// evalsynth 把仓库内的场景定义合成为可回放的评测样本。
//
// 用途：生产事故样本来自线上，天然稀疏且不可控；本命令把「按维度系统分解」得到的
// 场景清单合成为同格式样本，使评测集同时覆盖真实事故与已知风险面。
//
// 用法：
//
//	go run ./cmd/evalsynth -root <项目根> -scenarios testdata/eval-scenarios.json
//
// 合成样本与 cmd/evalexport 产出的生产样本格式一致，可直接混合评测：
// 生产样本回答「实际出过什么问题」，合成样本回答「哪些地方还可能出问题」。
//
// 本命令只读：打开数据库时使用只读入口，不执行迁移，不写任何业务表。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"xianyu-go/internal/evalharness"
	"xianyu-go/internal/logsafe"
)

// sourceLabel 是合成样本的来源标记，报告中用于与生产样本区分。
const sourceLabel = "synthetic"

// scenarioFile 是场景定义文件的顶层结构。
type scenarioFile struct {
	// Scenarios 是场景列表。
	Scenarios []scenarioSpec `json:"scenarios"`
}

// scenarioSpec 是一条场景定义。
type scenarioSpec struct {
	// ID 是场景稳定标识，用于生成稳定的样本键，必须全局唯一。
	ID string `json:"id"`
	// Group 是场景分组，用于报告归类，如 delivery、compliance。
	Group string `json:"group"`
	// Account 是账号标识；留空则使用命令行的默认账号。
	Account string `json:"account,omitempty"`
	// ItemID 是商品标识；留空则使用默认商品。
	ItemID string `json:"item_id,omitempty"`
	// BuyerText 是买家发出的消息原文。
	BuyerText string `json:"buyer_text"`
	// Verdicts 是该场景对应的标签；留空时使用 Group。
	Verdicts []string `json:"verdicts,omitempty"`
	// Expectation 是判定期望；留空表示完全交给判定模型。
	Expectation *evalharness.Expectation `json:"expectation,omitempty"`
	// Note 是场景说明，仅用于人读。
	Note string `json:"note,omitempty"`
}

// main 是合成 CLI 的进程入口。
func main() {
	// root 是项目根目录，场景文件与产物路径相对它解析。
	root := flag.String("root", ".", "项目根目录（含 .env）")
	// scenariosPath 是场景定义文件路径。
	scenariosPath := flag.String("scenarios", "testdata/eval-scenarios.json", "场景定义文件")
	// outPath 是合成样本输出路径。
	outPath := flag.String("out", "", "合成样本输出路径（默认 data/evalexport/samples-synthetic.jsonl）")
	// account 是账号缺省值。
	account := flag.String("account", "", "账号标识；场景未指定时使用")
	// itemID 是商品缺省值。
	itemID := flag.String("item", "", "商品标识；场景未指定时使用")
	flag.Parse()

	// code 是退出码，由 run 决定。
	code := run(*root, *scenariosPath, *outPath, *account, *itemID)
	os.Exit(code)
}

// run 执行场景合成，返回退出码。
func run(root, scenariosPath, outPath, account, itemID string) int {
	// ctx 是总超时上下文；每条场景都要读一次配置与商品。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// resolvedOut 是输出路径，未指定时落到被忽略的数据目录。
	resolvedOut := outPath
	if strings.TrimSpace(resolvedOut) == "" {
		resolvedOut = filepath.Join("data", "evalexport", "samples-synthetic.jsonl")
	}
	// absScenarios 是解析为绝对路径后的场景定义文件位置。
	absScenarios := resolvePath(root, scenariosPath)
	// absOut 是解析为绝对路径后的输出文件位置。
	absOut := resolvePath(root, resolvedOut)

	// specs、err 是场景定义与其解析失败原因。
	specs, err := loadScenarios(absScenarios)
	if err != nil {
		fmt.Printf("读取场景定义失败: %s\n", logsafe.Error(err))
		return 2
	}

	// values 是项目 .env 的解析结果。
	values, err := evalharness.LoadDotenv(root)
	if err != nil {
		fmt.Printf("读取 .env 失败: %s\n", logsafe.Error(err))
		return 2
	}
	applyDotenv(values)

	// dsn 是数据库连接串。
	dsn := evalharness.EnvValue(values, "DATABASE_URL")
	if dsn == "" {
		fmt.Println("未配置 DATABASE_URL，无法重建系统提示")
		return 2
	}
	// 只读打开：合成工具不得在生产库上触发迁移。
	database, dialect, err := db.OpenReadOnly(ctx, dsn)
	if err != nil {
		fmt.Printf("打开数据库失败: %s\n", logsafe.Error(err))
		return 2
	}
	defer database.Close()
	// store 是仓储集合，供引擎读取配置与商品。
	store := db.NewStore(database, dialect)

	// defaultAccount 是场景未指定账号时使用的账号。
	defaultAccount := strings.TrimSpace(account)
	if defaultAccount == "" {
		defaultAccount, err = firstAIAccount(ctx, database)
		if err != nil {
			fmt.Printf("解析默认账号失败: %s\n", logsafe.Error(err))
			return 2
		}
	}
	// defaultItem 是场景未指定商品时使用的商品。
	defaultItem := strings.TrimSpace(itemID)
	if defaultItem == "" {
		defaultItem, err = firstItem(ctx, database, defaultAccount)
		if err != nil {
			fmt.Printf("解析默认商品失败: %s\n", logsafe.Error(err))
			return 2
		}
	}

	// samples 是合成出的样本列表。
	samples := make([]evalharness.Sample, 0, len(specs))
	// failed 是重建失败并跳过的场景数。
	failed := 0
	// spec 是当前遍历到的场景定义。
	for _, spec := range specs {
		// sample、buildErr 是该场景合成出的样本与失败原因。
		sample, buildErr := buildSample(ctx, store, spec, defaultAccount, defaultItem)
		if buildErr != nil {
			failed++
			fmt.Printf("场景 %s 合成失败: %s\n", spec.ID, logsafe.Error(buildErr))
			continue
		}
		samples = append(samples, *sample)
	}

	// writeErr 是写出合成样本的失败原因。
	if writeErr := writeSamples(absOut, samples); writeErr != nil {
		fmt.Printf("写出合成样本失败: %s\n", logsafe.Error(writeErr))
		return 2
	}
	fmt.Printf("合成 %d 条场景样本（失败 %d 条），默认账号 %s，默认商品 %s\n",
		len(samples), failed, logsafe.ID(defaultAccount), logsafe.ID(defaultItem))
	fmt.Printf("输出: %s\n", absOut)
	return 0
}

// buildSample 用当前代码为一条场景重建系统提示并组装样本。
// spec 是场景定义；defaultAccount 与 defaultItem 是缺省账号与商品。
func buildSample(ctx context.Context, store *db.Store, spec scenarioSpec, defaultAccount, defaultItem string) (*evalharness.Sample, error) {
	// account、itemID 是本次实际使用的账号与商品。
	account, itemID := defaultAccount, defaultItem
	if strings.TrimSpace(spec.Account) != "" {
		account = spec.Account
	}
	if strings.TrimSpace(spec.ItemID) != "" {
		itemID = spec.ItemID
	}
	// logger 丢弃明细日志，避免污染命令输出。
	logger := slogDiscard()
	// replier 是复用配置读取与提示拼装的回复器实例。
	replier := engine.NewAIReplier(account, store, logger)
	// prompt、err 是重建出的提示快照与失败原因。
	prompt, err := replier.ExportPromptSnapshot(ctx, engine.ChatMessage{
		AccountID: account,
		ChatID:    spec.ID,
		ItemID:    itemID,
		Text:      spec.BuyerText,
	})
	if err != nil {
		return nil, err
	}
	// history 是把引擎侧历史转换为样本结构的结果。
	history := make([]evalharness.ConversationTurn, 0, len(prompt.History))
	// turn 是当前遍历到的历史轮次。
	for _, turn := range prompt.History {
		history = append(history, evalharness.ConversationTurn{Role: turn.Role, Content: turn.Content})
	}
	// verdicts 是该场景的标签集合，未显式指定时回落到分组名。
	verdicts := spec.Verdicts
	if len(verdicts) == 0 && strings.TrimSpace(spec.Group) != "" {
		verdicts = []string{spec.Group}
	}
	return &evalharness.Sample{
		Account: logsafe.ID(account),
		Chat:    logsafe.ID(spec.ID),
		Item:    logsafe.ID(itemID),
		// 合成的买家消息不来自线上，保留原文以便复核。
		Messages: []evalharness.Message{{
			Direction:   "incoming",
			MessageType: "text",
			Content:     spec.BuyerText,
		}},
		Verdicts: verdicts,
		Snapshot: &evalharness.Snapshot{
			Takeover:         prompt.Takeover,
			Intent:           prompt.Intent,
			Model:            prompt.Model,
			SystemPrompt:     prompt.SystemPrompt,
			KnowledgeContext: prompt.KnowledgeContext,
			History:          history,
			MessageText:      prompt.MessageText,
			ItemPrice:        prompt.ItemPrice,
			MinimumPrice:     prompt.MinimumPrice,
		},
		Expectation: spec.Expectation,
		Source:      sourceLabel,
		ScenarioID:  spec.ID,
	}, nil
}

// loadScenarios 读取并校验场景定义文件。
func loadScenarios(path string) ([]scenarioSpec, error) {
	// data、err 是文件内容与读取失败原因。
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取场景文件失败: %w", err)
	}
	// file 是解析出的场景定义。
	var file scenarioFile
	// unmarshalErr 是场景文件的 JSON 解析失败原因。
	if unmarshalErr := json.Unmarshal(data, &file); unmarshalErr != nil {
		return nil, fmt.Errorf("解析场景文件失败: %w", unmarshalErr)
	}
	if len(file.Scenarios) == 0 {
		return nil, errors.New("场景文件没有任何场景")
	}
	// seen 用于检测重复的场景标识。
	seen := make(map[string]bool, len(file.Scenarios))
	// spec 是当前遍历到的场景定义。
	for _, spec := range file.Scenarios {
		// id 是去掉首尾空白后的场景标识。
		id := strings.TrimSpace(spec.ID)
		if id == "" {
			return nil, errors.New("存在缺少 id 的场景")
		}
		if seen[id] {
			return nil, fmt.Errorf("场景 id 重复: %s", id)
		}
		seen[id] = true
		if strings.TrimSpace(spec.BuyerText) == "" {
			return nil, fmt.Errorf("场景 %s 缺少 buyer_text", id)
		}
	}
	return file.Scenarios, nil
}

// writeSamples 把合成样本逐行写出为 JSONL。
func writeSamples(path string, samples []evalharness.Sample) error {
	// dir 是输出文件的父目录。
	dir := filepath.Dir(path)
	// mkdirErr 是创建输出目录的失败原因。
	if mkdirErr := os.MkdirAll(dir, 0o755); mkdirErr != nil {
		return fmt.Errorf("创建输出目录失败: %w", mkdirErr)
	}
	// f 是输出文件句柄。
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建合成样本失败: %w", err)
	}
	defer f.Close()
	// sample 是当前遍历到的样本。
	for _, sample := range samples {
		// blob、err 是序列化结果与失败原因。
		blob, err := json.Marshal(sample)
		if err != nil {
			return fmt.Errorf("序列化样本失败: %w", err)
		}
		fmt.Fprintf(f, "%s\n", blob)
	}
	return nil
}

// firstAIAccount 取任意一个已启用 AI 回复的账号；只读账号主键，不涉及凭证。
func firstAIAccount(ctx context.Context, database *sql.DB) (string, error) {
	// query 取启用 AI 的账号，顺序固定便于复现。
	query := `SELECT cookie_id FROM ai_reply_settings WHERE ai_enabled = 1 ORDER BY cookie_id LIMIT 1`
	// cookieID 是查询结果。
	var cookieID string
	// err 是查询失败原因。
	err := database.QueryRowContext(ctx, query).Scan(&cookieID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("数据库中没有启用 AI 回复的账号")
	}
	if err != nil {
		return "", err
	}
	return cookieID, nil
}

// firstItem 取指定账号下第一个未删除商品；只读商品主键。
func firstItem(ctx context.Context, database *sql.DB, account string) (string, error) {
	// query 取未删除商品，顺序固定便于复现。
	query := `SELECT item_id FROM item_info WHERE cookie_id = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1`
	// itemID 是查询结果。
	var itemID string
	// err 是查询失败原因。
	err := database.QueryRowContext(ctx, query, account).Scan(&itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("该账号下没有未删除的商品")
	}
	if err != nil {
		return "", err
	}
	return itemID, nil
}

// resolvePath 把相对路径按项目根目录展开；已是绝对路径时原样返回。
func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

// slogDiscard 返回丢弃全部输出的日志器，避免合成过程污染命令输出。
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// applyDotenv 把白名单键注入进程环境，数据库解密密钥必须经此注入。
func applyDotenv(values map[string]string) {
	// key 是当前遍历到的键名。
	for _, key := range []string{"DATABASE_URL", "XIANYU_DATA_KEY"} {
		if os.Getenv(key) != "" {
			continue
		}
		// value 是该键在 .env 中的取值。
		if value := values[key]; value != "" {
			_ = os.Setenv(key, value)
		}
	}
}
