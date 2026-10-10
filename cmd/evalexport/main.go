// evalexport 只读导出客服 Agent 与自动发货的事故样本全记录，供离线评测建设样本库。
//
// 用法：
//
//	go run ./cmd/evalexport -dsn "postgres://user:pass@host:5432/db?sslmode=disable" -out /tmp/evalexport
//	go run ./cmd/evalexport -dsn "sqlite://data/xianyu_data.db" -out /tmp/evalexport
//
// 输出两个文件：
//   - report.md：人类可读的逐会话全记录，含按生产链路重建的系统提示
//   - samples.jsonl：结构化样本，账号与会话标识已哈希，供后续评测 runner 消费
//
// 本命令只读：不写业务表、不调用模型、不修改任何生产数据。
// 连接串中的凭证与查询参数经 logsafe 收敛后再打印。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/logsafe"
)

// 事故判定标签。判定为启发式，只做初筛，进入样本库前必须人工确认。
const (
	// verdictAutoFailed 是自动发货失败：运行记录为 failed 或 needs_review。
	verdictAutoFailed = "auto-failed"
	// verdictHumanCorrection 是人工纠正：商家明确否定 AI 回复，或人工接手重答。
	// 这是置信度最高的失误标签，等价于商家手工标注。
	verdictHumanCorrection = "human-correction"
	// verdictDeliveryDeflection 是发货回避：买家反复追问渠道且回复使用统一回避话术。
	verdictDeliveryDeflection = "delivery-deflection"
	// verdictRepeatAsk 是重复追问：同一会话内买家多次问同类问题，疑似未答到点。
	verdictRepeatAsk = "repeat-ask"
	// verdictBaseline 是对照样本：未命中任何事故信号，用于防止样本库只有失败案例。
	verdictBaseline = "baseline"
)

// robotDenialMarkers 是商家明确否定机器人回复的表述，命中即为高置信人工纠正。
var robotDenialMarkers = []string{
	"机器人瞎回复", "瞎回复", "机器人乱", "乱回复", "机器人回错", "机器人说错", "不是这么回", "别乱说",
}

// deflectionMarkers 是发货类回复中的统一回避话术标记，命中说明模型没有拿到确定渠道线索。
var deflectionMarkers = []string{"以商品详情", "拍下后的发货消息", "具体以"}

// deliveryAskingWords 是买家询问发货渠道时的常见词，用于统计追问次数。
var deliveryAskingWords = []string{"网盘", "夸克", "度盘", "百度", "盘", "怎么发", "怎么发货", "发货"}

// chatMessageRow 是一条聊天记录行，对应 chat_messages 的只读投影。
type chatMessageRow struct {
	// Direction 是消息方向：incoming 表示买家，outgoing 表示卖家。
	Direction string `json:"direction"`
	// MessageType 是消息类型：text、image、system 等。
	MessageType string `json:"message_type"`
	// Content 是消息文本或媒体地址。
	Content string `json:"content"`
	// SentAt 是发送时间（Unix 毫秒）。
	SentAt int64 `json:"sent_at"`
}

// aiTurnRow 是一条 AI 对话记录行，对应 ai_conversations 的只读投影。
type aiTurnRow struct {
	// Role 是角色：user 表示买家，assistant 表示 AI 回复。
	Role string `json:"role"`
	// Content 是消息文本。
	Content string `json:"content"`
	// Intent 是写入历史的意图标签。
	Intent string `json:"intent"`
	// BargainCount 是该轮的砍价计数。
	BargainCount int `json:"bargain_count"`
	// CreatedAt 是落库时间。
	CreatedAt sql.NullTime `json:"created_at"`
}

// autoRunRow 是一条自动化运行记录行，对应 automation_runs 的只读投影。
type autoRunRow struct {
	// ID 是运行记录主键。
	ID int64 `json:"id"`
	// TriggerType 是触发类型，如 order_paid、buyer_reviewed。
	TriggerType string `json:"trigger_type"`
	// Status 是运行状态：success、failed、needs_review、canceled。
	Status string `json:"status"`
	// ErrorMessage 是失败原因，成功时为空。
	ErrorMessage string `json:"error_message"`
	// OrderID 是关联订单号。
	OrderID string `json:"order_id"`
	// AttemptCount 是已尝试次数。
	AttemptCount int `json:"attempt_count"`
}

// exportedSession 是一个待导出的会话，聚合现场、AI 记录与判定结果。
type exportedSession struct {
	// CookieID 是账号标识。
	CookieID string
	// ChatID 是会话标识。
	ChatID string
	// ItemID 是会话关联的商品标识。
	ItemID string
	// Messages 是该会话的完整聊天记录，按时间正序。
	Messages []chatMessageRow
	// AITurns 是该会话的 AI 对话记录。
	AITurns []aiTurnRow
	// AutoRuns 是该会话关联的自动化运行记录。
	AutoRuns []autoRunRow
	// Verdicts 是命中的事故标签集合。
	Verdicts []string
	// Snapshot 是按生产链路重建的提示快照；无法重建时为 nil。
	Snapshot *engine.PromptSnapshot
	// SnapshotErr 是重建失败原因。
	SnapshotErr string
}

// main 是导出 CLI 的进程入口。
func main() {
	// dsn 是数据库连接串，需带 scheme（postgres:// 或 sqlite://）。
	// 命令行未给时回落到环境变量 EVALEXPORT_DSN，便于用 --env-file 传递而不让凭证进入进程参数。
	dsn := flag.String("dsn", "", "数据库连接串，如 postgres://user:pass@host:5432/db?sslmode=disable")
	// outDir 是导出物目录；不存在时自动创建。
	outDir := flag.String("out", "/tmp/evalexport", "输出目录")
	// limit 是单个导出的会话数上限，用于控制报告体积。
	limit := flag.Int("limit", 50, "最多导出的会话数")
	flag.Parse()

	// effectiveDSN 是最终生效的连接串：命令行优先，其次环境变量。
	effectiveDSN := strings.TrimSpace(*dsn)
	if effectiveDSN == "" {
		effectiveDSN = strings.TrimSpace(os.Getenv("EVALEXPORT_DSN"))
	}
	if effectiveDSN == "" {
		fmt.Println("用法: evalexport -dsn <database-url> [-out <dir>]")
		os.Exit(2)
	}
	// err 是导出主流程的失败原因；非 nil 时打印脱敏文本并按非零码退出。
	if err := run(effectiveDSN, *outDir, *limit); err != nil {
		fmt.Printf("导出失败: %s\n", logsafe.Error(err))
		os.Exit(1)
	}
}

// run 执行导出主流程，返回的错误文本已经过脱敏处理。
// dsn 是连接串，outDir 是输出目录，limit 是会话数上限。
func run(dsn, outDir string, limit int) error {
	// ctx 是导出的总超时上下文，避免在慢查询上无限等待。
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fmt.Printf("连接 %s ...\n", maskDSN(dsn))
	// database、dialect、err 是打开的连接池、方言与失败原因。
	// 使用只读打开：本工具不得在生产库上触发迁移。
	database, dialect, err := db.OpenReadOnly(ctx, dsn)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()

	// store 是仓储集合，供 engine 复用的配置与商品读取路径使用。
	store := db.NewStore(database, dialect)

	// sessions 是本次导出的会话集合，按事故置信度排序后写出。
	sessions, err := collectSessions(ctx, database, dialect, store, limit)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("未发现可导出的会话：该库既无 AI 对话记录，也无自动化失败记录。")
		return nil
	}

	// mkdirErr 是创建输出目录的失败原因。
	if mkdirErr := os.MkdirAll(outDir, 0o755); mkdirErr != nil {
		return fmt.Errorf("创建输出目录失败: %w", mkdirErr)
	}
	// reportPath 是 Markdown 报告的落盘路径。
	reportPath := filepath.Join(outDir, "report.md")
	// samplesPath 是结构化样本的落盘路径。
	samplesPath := filepath.Join(outDir, "samples.jsonl")

	// reportErr 是报告写出的失败原因。
	if reportErr := writeReport(reportPath, maskDSN(dsn), sessions); reportErr != nil {
		return reportErr
	}
	// samplesErr 是样本写出的失败原因。
	if samplesErr := writeSamples(samplesPath, sessions); samplesErr != nil {
		return samplesErr
	}

	// counts 是各判定标签的命中统计，用于终端摘要。
	counts := map[string]int{}
	// session 是当前遍历到的导出会话。
	for _, session := range sessions {
		// verdict 是当前会话命中的判定标签。
		for _, verdict := range session.Verdicts {
			counts[verdict]++
		}
	}
	fmt.Printf("导出 %d 个会话\n", len(sessions))
	// verdict、n 是遍历到的标签与命中数。
	for verdict, n := range counts {
		fmt.Printf("  %-22s %d\n", verdict, n)
	}
	fmt.Printf("报告: %s\n样本: %s\n", reportPath, samplesPath)
	return nil
}

// collectSessions 收集候选会话并逐个重建提示快照。
// database 是连接池，dialect 决定占位符风格，store 供 engine 读取配置与商品。
func collectSessions(ctx context.Context, database *sql.DB, dialect db.Dialect, store *db.Store, limit int) ([]*exportedSession, error) {
	// keys 是有 AI 介入的会话标识到会话对象的映射。
	keys := map[string]*exportedSession{}
	// order 保持查询顺序，保证报告可复现。
	var order []string

	// queryAIConversations 列出所有出现过的 AI 会话。
	queryAIConversations := `SELECT DISTINCT cookie_id, chat_id, item_id FROM ai_conversations ORDER BY cookie_id, chat_id`
	// rows、err 是会话查询结果集与失败原因。
	rows, err := database.QueryContext(ctx, queryAIConversations)
	if err != nil {
		return nil, fmt.Errorf("查询 AI 会话失败: %w", err)
	}
	// 逐行装载会话键，扫描失败时先关闭结果集再返回。
	for rows.Next() {
		// cookieID、chatID、itemID 是当前行的账号、会话与商品标识。
		var cookieID, chatID, itemID string
		// scanErr 是当前行的扫描失败原因。
		if scanErr := rows.Scan(&cookieID, &chatID, &itemID); scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("解析 AI 会话失败: %w", scanErr)
		}
		// key 是会话唯一键。
		key := cookieID + "\x00" + chatID
		// seen 表示该会话是否已被登记，未登记时补充到顺序表。
		if _, seen := keys[key]; !seen {
			keys[key] = &exportedSession{CookieID: cookieID, ChatID: chatID, ItemID: itemID}
			order = append(order, key)
		}
	}
	rows.Close()
	// rowsErr 是遍历结果集时的累计失败原因。
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("读取 AI 会话失败: %w", rowsErr)
	}

	// 装载自动化失败记录；失败会话即使没有 AI 介入也必须导出。
	if autoErr := loadAutoRuns(ctx, database, keys, &order); autoErr != nil {
		return nil, autoErr
	}

	// sessions 是按序展开的会话切片。
	sessions := make([]*exportedSession, 0, len(order))
	// maxSessions 是本次实际导出的上限。
	maxSessions := limit
	if maxSessions <= 0 || maxSessions > len(order) {
		maxSessions = len(order)
	}
	// key 是当前遍历到的会话键。
	for _, key := range order[:maxSessions] {
		// session 是当前会话，装载现场与 AI 记录后重建快照。
		session := keys[key]
		// msgErr 是装载聊天记录的失败原因。
		if msgErr := loadMessages(ctx, database, dialect, session); msgErr != nil {
			return nil, msgErr
		}
		// turnErr 是装载 AI 记录的失败原因。
		if turnErr := loadAITurns(ctx, database, dialect, session); turnErr != nil {
			return nil, turnErr
		}
		session.Verdicts = classify(session)
		buildSnapshot(ctx, store, session)
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// loadAutoRuns 把自动化失败记录并入会话集合，必要时新建会话条目。
// 只有 failed 与 needs_review 被视为事故，canceled 与 success 不导出。
func loadAutoRuns(ctx context.Context, database *sql.DB, keys map[string]*exportedSession, order *[]string) error {
	// query 只取失败态运行；顺序固定便于复现。
	query := `SELECT id, cookie_id, chat_id, order_id, trigger_type, status, error_message, attempt_count
FROM automation_runs WHERE status IN ('failed','needs_review') ORDER BY id`
	// rows、err 是失败运行查询结果集与失败原因。
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("查询自动化失败记录失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		// run 是当前行的只读投影。
		var run autoRunRow
		// cookieID、chatID 是该行归属的账号与会话标识。
		var cookieID, chatID string
		// scanErr 是当前行的扫描失败原因。
		if scanErr := rows.Scan(&run.ID, &cookieID, &chatID, &run.OrderID, &run.TriggerType, &run.Status, &run.ErrorMessage, &run.AttemptCount); scanErr != nil {
			return fmt.Errorf("解析自动化失败记录失败: %w", scanErr)
		}
		// key 是会话唯一键。
		key := cookieID + "\x00" + chatID
		// session、exists 是该失败记录归属的会话，以及该会话是否已登记。
		session, exists := keys[key]
		if !exists {
			session = &exportedSession{CookieID: cookieID, ChatID: chatID}
			keys[key] = session
			*order = append(*order, key)
		}
		session.AutoRuns = append(session.AutoRuns, run)
	}
	// rowsErr 是遍历结果集时的累计失败原因。
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("读取自动化失败记录失败: %w", rowsErr)
	}
	return nil
}

// loadMessages 装载一个会话的完整聊天记录。
func loadMessages(ctx context.Context, database *sql.DB, dialect db.Dialect, session *exportedSession) error {
	// query 按发送时间正序取全量消息；占位符随方言变化。
	query := `SELECT direction, message_type, content, sent_at FROM chat_messages
WHERE cookie_id = ` + placeholder(dialect, 1) + ` AND chat_id = ` + placeholder(dialect, 2) + `
ORDER BY sent_at, id`
	// rows、err 是消息查询结果集与失败原因。
	rows, err := database.QueryContext(ctx, query, session.CookieID, session.ChatID)
	if err != nil {
		return fmt.Errorf("查询聊天记录失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		// item 是当前行的只读投影。
		var item chatMessageRow
		// scanErr 是当前行的扫描失败原因。
		if scanErr := rows.Scan(&item.Direction, &item.MessageType, &item.Content, &item.SentAt); scanErr != nil {
			return fmt.Errorf("解析聊天记录失败: %w", scanErr)
		}
		session.Messages = append(session.Messages, item)
	}
	// rowsErr 是遍历结果集时的累计失败原因。
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("读取聊天记录失败: %w", rowsErr)
	}
	return nil
}

// loadAITurns 装载一个会话的 AI 对话记录。
func loadAITurns(ctx context.Context, database *sql.DB, dialect db.Dialect, session *exportedSession) error {
	// query 按主键正序取该会话的 AI 记录。
	query := `SELECT role, content, COALESCE(intent,''), COALESCE(bargain_count,0), created_at FROM ai_conversations
WHERE cookie_id = ` + placeholder(dialect, 1) + ` AND chat_id = ` + placeholder(dialect, 2) + ` ORDER BY id`
	// rows、err 是结果集与失败原因。
	rows, err := database.QueryContext(ctx, query, session.CookieID, session.ChatID)
	if err != nil {
		return fmt.Errorf("查询 AI 对话失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		// turn 是当前行的只读投影。
		var turn aiTurnRow
		// scanErr 是当前行的扫描失败原因。
		if scanErr := rows.Scan(&turn.Role, &turn.Content, &turn.Intent, &turn.BargainCount, &turn.CreatedAt); scanErr != nil {
			return fmt.Errorf("解析 AI 对话失败: %w", scanErr)
		}
		session.AITurns = append(session.AITurns, turn)
	}
	// rowsErr 是遍历结果集时的累计失败原因。
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("读取 AI 对话失败: %w", rowsErr)
	}
	return nil
}

// buildSnapshot 用会话最后一条买家文本消息重建系统提示；失败只记录原因，不中断导出。
func buildSnapshot(ctx context.Context, store *db.Store, session *exportedSession) {
	// lastBuyerText 是该会话最后一条买家文本消息，作为重建提示的触发消息。
	lastBuyerText := ""
	// item 是当前遍历到的会话消息。
	for _, item := range session.Messages {
		if item.Direction == "incoming" && item.MessageType == "text" && strings.TrimSpace(item.Content) != "" {
			lastBuyerText = item.Content
		}
	}
	if lastBuyerText == "" {
		session.SnapshotErr = "该会话没有可用的买家文本消息，无法重建提示"
		return
	}
	// logger 丢弃明细日志，避免导出过程污染终端输出。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// replier 是复用的 AI 回复器实例，仅用于读取配置与拼装提示。
	replier := engine.NewAIReplier(session.CookieID, store, logger)
	// snap、err 是重建出的提示快照与失败原因。
	snap, err := replier.ExportPromptSnapshot(ctx, engine.ChatMessage{
		AccountID: session.CookieID,
		ChatID:    session.ChatID,
		ItemID:    session.ItemID,
		Text:      lastBuyerText,
	})
	if err != nil {
		session.SnapshotErr = logsafe.Error(err)
		return
	}
	session.Snapshot = snap
}

// classify 按启发式规则给出会话的事故标签，结果只作初筛，必须人工确认。
// 优先级：自动发货失败 > 人工纠正 > 发货回避 > 重复追问 > 对照样本。
func classify(session *exportedSession) []string {
	// verdicts 是命中的标签集合。
	var verdicts []string
	if len(session.AutoRuns) > 0 {
		verdicts = append(verdicts, verdictAutoFailed)
	}

	// askingCount 是买家询问发货渠道的消息条数。
	askingCount := 0
	// buyerItem 是当前遍历到的会话消息。
	for _, buyerItem := range session.Messages {
		if buyerItem.Direction != "incoming" || buyerItem.MessageType != "text" {
			continue
		}
		// word 是当前遍历到的发货问法关键词。
		for _, word := range deliveryAskingWords {
			if strings.Contains(buyerItem.Content, word) {
				askingCount++
				break
			}
		}
	}

	// sellerText 是该会话卖家回复的拼接文本，用于统计回避话术与人工纠正。
	sellerText := strings.Builder{}
	// sellerCount 是卖家文本消息条数，用于推算人工介入次数。
	sellerCount := 0
	// sellerItem 是当前遍历到的会话消息。
	for _, sellerItem := range session.Messages {
		if sellerItem.Direction != "outgoing" || sellerItem.MessageType != "text" {
			continue
		}
		sellerCount++
		sellerText.WriteString(sellerItem.Content)
		sellerText.WriteString("\n")
	}
	// combinedSeller 是卖家文本的合并结果。
	combinedSeller := sellerText.String()

	// hitMarkers 是命中的回避话术数量。
	hitMarkers := 0
	// marker 是当前遍历到的回避话术标记。
	for _, marker := range deflectionMarkers {
		if strings.Contains(combinedSeller, marker) {
			hitMarkers++
		}
	}

	// denialHit 表示商家在会话中明确否定机器人回复。
	denialHit := false
	// denialMarker 是当前遍历到的否定表述。
	for _, denialMarker := range robotDenialMarkers {
		if strings.Contains(combinedSeller, denialMarker) {
			denialHit = true
			break
		}
	}

	// aiReplies 是 AI 实际发出的回复条数。
	aiReplies := 0
	// turn 是当前遍历到的 AI 记录。
	for _, turn := range session.AITurns {
		if turn.Role == "assistant" {
			aiReplies++
		}
	}
	// manualReplies 是排除 AI 回复后的卖家人工消息条数，大于零即说明人工已介入。
	manualReplies := sellerCount - aiReplies

	switch {
	case denialHit || manualReplies >= 2:
		verdicts = append(verdicts, verdictHumanCorrection)
	case askingCount >= 2 && hitMarkers >= 2:
		verdicts = append(verdicts, verdictDeliveryDeflection)
	case askingCount >= 2:
		verdicts = append(verdicts, verdictRepeatAsk)
	}
	if len(verdicts) == 0 {
		verdicts = append(verdicts, verdictBaseline)
	}
	return verdicts
}

// writeReport 写出人类可读的逐会话全记录，含重建的系统提示。
func writeReport(path, maskedDSN string, sessions []*exportedSession) error {
	// f 是报告文件句柄。
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建报告文件失败: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "# 客服 Agent 事故样本全记录\n\n")
	fmt.Fprintf(f, "> 数据库：`%s`\n", maskedDSN)
	fmt.Fprintf(f, "> 导出时间：%s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "> 会话数：%d\n", len(sessions))
	fmt.Fprintf(f, "\n> **合规警示**：本文件含生产对话原文，属敏感数据，禁止提交进版本库或外发。\n")
	fmt.Fprintf(f, "> 事故标签为启发式初筛，进入样本库前必须人工确认。\n\n")
	fmt.Fprintf(f, "---\n\n")

	// idx、session 是样本序号与当前会话。
	for idx, session := range sessions {
		writeSessionReport(f, idx+1, session)
	}
	return nil
}

// writeSessionReport 写出单个会话的完整记录块。
func writeSessionReport(f *os.File, index int, session *exportedSession) {
	fmt.Fprintf(f, "## 样本 %03d · %s\n\n", index, strings.Join(session.Verdicts, ", "))
	fmt.Fprintf(f, "| 项 | 值 |\n|---|---|\n")
	fmt.Fprintf(f, "| 账号 | `%s` |\n", session.CookieID)
	fmt.Fprintf(f, "| 会话 | `%s` |\n", session.ChatID)
	fmt.Fprintf(f, "| 商品 | `%s` |\n", orNone(session.ItemID))
	fmt.Fprintf(f, "| 消息数 | %d |\n", len(session.Messages))
	fmt.Fprintf(f, "| AI 轮次 | %d |\n", len(session.AITurns))
	fmt.Fprintf(f, "\n")

	if len(session.AutoRuns) > 0 {
		fmt.Fprintf(f, "### 自动化运行\n\n")
		fmt.Fprintf(f, "| 运行 | 触发 | 状态 | 尝试 | 订单 | 失败原因 |\n|---|---|---|---|---|---|\n")
		// run 是当前遍历到的运行记录。
		for _, run := range session.AutoRuns {
			fmt.Fprintf(f, "| %d | %s | %s | %d | `%s` | %s |\n",
				run.ID, run.TriggerType, run.Status, run.AttemptCount, orNone(run.OrderID), orNone(run.ErrorMessage))
		}
		fmt.Fprintf(f, "\n")
	}

	fmt.Fprintf(f, "### 对话全记录\n\n")
	if len(session.Messages) == 0 {
		fmt.Fprintf(f, "（该会话无聊天记录落库）\n\n")
	}
	// i、item 是消息序号与会话消息。
	for i, item := range session.Messages {
		// who 是消息来源的中文标签。
		who := "买家"
		if item.Direction == "outgoing" {
			who = "卖家"
		}
		fmt.Fprintf(f, "**%d. %s** · %s · %s\n\n", i+1, who, item.MessageType, formatMillis(item.SentAt))
		fmt.Fprintf(f, "> %s\n\n", indentQuote(item.Content))
	}

	fmt.Fprintf(f, "### 系统提示（按生产链路重建）\n\n")
	if session.Snapshot == nil {
		fmt.Fprintf(f, "无法重建：%s\n\n", orNone(session.SnapshotErr))
		fmt.Fprintf(f, "---\n\n")
		return
	}
	// snap 是重建出的提示快照。
	snap := session.Snapshot
	fmt.Fprintf(f, "| 项 | 值 |\n|---|---|\n")
	fmt.Fprintf(f, "| 模型 | `%s` |\n", snap.Model)
	fmt.Fprintf(f, "| 是否接管 | %v |\n", snap.Takeover)
	fmt.Fprintf(f, "| 命中意图 | %s |\n", orNone(strings.Join(snap.HitIntents, ", ")))
	fmt.Fprintf(f, "| 意图标签 | `%s` |\n", snap.Intent)
	fmt.Fprintf(f, "| 商品标价 | %.2f 元 |\n", snap.ItemPrice)
	fmt.Fprintf(f, "| 最低可成交价 | %.2f 元 |\n", snap.MinimumPrice)
	fmt.Fprintf(f, "| 砍价轮次 | %d（在限内=%v） |\n", snap.BargainCount, snap.WithinBargainLimit)
	fmt.Fprintf(f, "| 历史轮次 | %d |\n", len(snap.History))
	if len(snap.Warnings) > 0 {
		fmt.Fprintf(f, "| 读取告警 | %s |\n", strings.Join(snap.Warnings, "；"))
	}
	fmt.Fprintf(f, "\n触发消息（重建所用）：\n\n> %s\n\n", indentQuote(snap.MessageText))

	fmt.Fprintf(f, "完整 system 提示：\n\n```text\n%s\n```\n\n", snap.SystemPrompt)

	if snap.KnowledgeContext != "" {
		fmt.Fprintf(f, "本次注入的知识上下文：\n\n```text\n%s\n```\n\n", snap.KnowledgeContext)
	}
	fmt.Fprintf(f, "---\n\n")
}

// writeSamples 写出结构化样本；账号与会话标识经哈希脱敏，正文保留以维持语义完整。
func writeSamples(path string, sessions []*exportedSession) error {
	// f 是样本文件句柄。
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建样本文件失败: %w", err)
	}
	defer f.Close()

	// session 是当前遍历到的会话。
	for _, session := range sessions {
		// record 是单条样本的序列化结构。
		record := map[string]any{
			"account":   logsafe.ID(session.CookieID),
			"chat":      logsafe.ID(session.ChatID),
			"item":      logsafe.ID(session.ItemID),
			"verdicts":  session.Verdicts,
			"messages":  session.Messages,
			"ai_turns":  session.AITurns,
			"auto_runs": session.AutoRuns,
		}
		if session.Snapshot != nil {
			record["snapshot"] = session.Snapshot
		}
		if session.SnapshotErr != "" {
			record["snapshot_error"] = session.SnapshotErr
		}
		// blob、err 是序列化结果与失败原因。
		blob, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("序列化样本失败: %w", err)
		}
		fmt.Fprintf(f, "%s\n", blob)
	}
	return nil
}

// placeholder 按方言返回第 n 个（从 1 开始计数）查询参数占位符。
func placeholder(dialect db.Dialect, n int) string {
	if dialect == db.DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// maskDSN 收敛连接串中的凭证与查询参数，用于安全打印。
func maskDSN(dsn string) string {
	if !strings.Contains(dsn, "://") {
		return dsn
	}
	return logsafe.URL(dsn)
}

// orNone 在值为空时返回占位符，避免报告出现空白单元格。
func orNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// indentQuote 把多行文本转为 Markdown 引用块，保留换行结构。
func indentQuote(text string) string {
	// lines 是文本按行切分的结果。
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	// out 是加好引用前缀的文本。
	out := make([]string, 0, len(lines))
	// line 是当前遍历到的行。
	for _, line := range lines {
		out = append(out, strings.TrimRight(line, "\r"))
	}
	return strings.Join(out, "\n> ")
}

// formatMillis 把 Unix 毫秒或秒时间戳格式化为本地可读时间；非正数返回空串。
func formatMillis(value int64) string {
	if value <= 0 {
		return ""
	}
	if value < 1_000_000_000_000 {
		return time.Unix(value, 0).Format("2006-01-02 15:04:05")
	}
	return time.UnixMilli(value).Format("2006-01-02 15:04:05")
}
