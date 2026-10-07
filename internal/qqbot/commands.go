package qqbot

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Command 是识别出的入站命令标识。
type Command string

const (
	// CommandUnknown 表示无法识别的输入，回复帮助文案。
	CommandUnknown Command = ""
	// CommandHelp 表示查询命令清单。
	CommandHelp Command = "help"
	// CommandSales 表示查询当日多账号销量。
	CommandSales Command = "sales"
	// CommandHealth 表示查询系统健康度。
	CommandHealth Command = "health"
	// CommandChats 表示提取近期会话用于审查。
	CommandChats Command = "chats"
)

// maxReplyRunes 是单条 QQ 文本回复允许的最大字符数，留出平台限制余量。
const maxReplyRunes = 1500

// maxTurnRunes 是会话审查中单条发言展示的最大字符数。
const maxTurnRunes = 60

// maxNameRunes 是会话审查中对端名称展示的最大字符数，避免过长昵称挤占版面。
const maxNameRunes = 12

// maxTitleRunes 是会话审查中商品标题展示的最大字符数。
const maxTitleRunes = 20

// commandAliases 是命令标识到可识别输入文本的映射，键为归一化后的用户输入。
var commandAliases = map[string]Command{
	"销量": CommandSales, "今日销量": CommandSales, "销售": CommandSales, "今日销售": CommandSales,
	"健康": CommandHealth, "健康度": CommandHealth, "系统健康": CommandHealth, "状态": CommandHealth,
	"会话": CommandChats, "会话审查": CommandChats, "最近会话": CommandChats, "对话": CommandChats,
	"帮助": CommandHelp, "help": CommandHelp, "?": CommandHelp, "？": CommandHelp, "菜单": CommandHelp,
}

// NormalizeInboundText 清理入站消息文本：去除首尾空白、平台 @ 提及前缀与零宽字符。
// 群聊场景下消息正文常带 `<@!bot>` 之类的提及前缀，命令匹配前必须剥离。
func NormalizeInboundText(raw string) string {
	// trimmed 是去除首尾空白后的输入。
	trimmed := strings.TrimSpace(raw)
	// fields 是按空白切分后的词元，用于剥离开头的提及前缀。
	fields := strings.Fields(trimmed)
	for len(fields) > 0 && (strings.HasPrefix(fields[0], "<@") || strings.HasPrefix(fields[0], "@")) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(fields, " "))
}

// ParseCommand 把归一化后的入站文本解析为命令标识；无法识别时返回 CommandUnknown。
func ParseCommand(raw string) Command {
	// normalized 是清理提及前缀与空白后的文本。
	normalized := NormalizeInboundText(raw)
	if normalized == "" {
		return CommandUnknown
	}
	// lower 是小写形式，用于兼容 help 之类的英文命令。
	lower := strings.ToLower(normalized)
	if // matched 是别名表命中且非未知的命令。
	matched := commandAliases[lower]; matched != CommandUnknown {
		return matched
	}
	return CommandUnknown
}

// FormatCommandHelp 渲染命令清单文案。
func FormatCommandHelp() string {
	// lines 是命令清单的逐行文案。
	lines := []string{
		"可用命令（发送关键词即可）：",
		"销量 / 今日销量 —— 当日按账号的成交单数与金额",
		"健康 / 健康度 —— 数据库、账号、交易、业务四维快照",
		"会话 / 会话审查 —— 按账号分组的近期会话片段",
		"帮助 —— 显示本清单",
	}
	return strings.Join(lines, "\n")
}

// FormatSales 把当日销量快照渲染为单条中文文本；无成交时给出明确说明。
func FormatSales(snapshot SalesSnapshot) string {
	if len(snapshot.Accounts) == 0 {
		return fmt.Sprintf("今日销量（%s）\n暂无成交记录。", orUnknownDate(snapshot.Date))
	}
	// lines 是输出行，首行为标题。
	lines := []string{fmt.Sprintf("今日销量（%s）", orUnknownDate(snapshot.Date))}
	// account 是当前遍历到的账号销量。
	for _, account := range snapshot.Accounts {
		lines = append(lines, fmt.Sprintf("· %s：%d 单 %s", displayAccountName(account), account.Orders, FormatFen(account.AmountFen)))
	}
	lines = append(lines, fmt.Sprintf("合计：%d 单 %s", snapshot.TotalOrders, FormatFen(snapshot.AmountFen)))
	return strings.Join(lines, "\n")
}

// FormatHealth 把四维健康快照渲染为单条中文文本；now 用于输出观测时刻。
func FormatHealth(snapshot HealthSnapshot, now time.Time) string {
	// lines 是输出行，首行为标题与观测时刻。
	lines := []string{fmt.Sprintf("系统健康（%s）", now.Format("01-02 15:04"))}
	// dbState 是数据库连通性的中文结论。
	dbState := "正常"
	if !snapshot.DatabaseOK {
		dbState = "异常"
	}
	lines = append(lines, fmt.Sprintf("数据库：%s", dbState))
	// online 是在线账号数量，abnormal 是非在线账号数量。
	online, abnormal := 0, 0
	// abnormalAccounts 是需要逐条列出的异常账号说明。
	abnormalAccounts := make([]string, 0, len(snapshot.Accounts))
	// account 是当前遍历到的账号健康状态。
	for _, account := range snapshot.Accounts {
		if account.Connected {
			online++
			continue
		}
		abnormal++
		// detail 是单个异常账号的说明，含状态与连续失败次数。
		detail := fmt.Sprintf("· %s %s", displayAccountHealthName(account), orUnknownState(account.State))
		if account.Failures > 0 {
			detail += fmt.Sprintf(" 连续失败 %d 次", account.Failures)
		}
		abnormalAccounts = append(abnormalAccounts, detail)
	}
	lines = append(lines, fmt.Sprintf("账号：共 %d 个，在线 %d，异常 %d", len(snapshot.Accounts), online, abnormal))
	lines = append(lines, abnormalAccounts...)
	lines = append(lines, fmt.Sprintf("交易：待处理 %d 起，死信 %d 起", snapshot.PendingIssues, snapshot.PendingDeferred))
	lines = append(lines, formatSilenceLine(snapshot))
	return strings.Join(lines, "\n")
}

// FormatChats 把分组会话摘要渲染为单条中文文本，用于审查机器人回复是否合理。
func FormatChats(digest ChatDigest) string {
	if len(digest.Accounts) == 0 {
		return "近期会话\n暂无会话记录。"
	}
	// lines 是输出行，首行为标题。
	lines := []string{"近期会话（按账号分组，最近优先）"}
	// account 是当前遍历到的账号会话集合。
	for _, account := range digest.Accounts {
		if len(account.Chats) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("【%s】", displayAccountChatsName(account)))
		// index 是当前会话在本账号内的序号。
		// brief 是当前遍历到的会话摘要。
		for index, brief := range account.Chats {
			lines = append(lines, fmt.Sprintf("%d. %s · %s", index+1, clipRunes(brief.PeerName, maxNameRunes), clipRunes(brief.ItemTitle, maxTitleRunes)))
			// turn 是当前会话内的一条发言。
			for _, turn := range brief.Turns {
				lines = append(lines, fmt.Sprintf("    %s：%s", turnSpeaker(turn.Direction), clipRunes(turn.Content, maxTurnRunes)))
			}
		}
	}
	if len(lines) == 1 {
		return "近期会话\n暂无会话记录。"
	}
	return strings.Join(lines, "\n")
}

// FormatFen 把以分为单位的金额渲染为带人民币符号的中文金额文本。
func FormatFen(fen int64) string {
	// negative 表示金额为负，需要保留负号。
	negative := fen < 0
	// absolute 是金额绝对值。
	absolute := fen
	if negative {
		absolute = -absolute
	}
	// text 是拼接后的金额文本，小数部分固定两位。
	text := fmt.Sprintf("%s%d.%s", boolPrefix(negative), absolute/100, pad2(absolute%100))
	return "¥" + text
}

// pad2 把小于 100 的数值补齐为两位文本。
func pad2(value int64) string {
	// text 是数值的十进制文本。
	text := strconv.FormatInt(value, 10)
	if len(text) < 2 {
		return "0" + text
	}
	return text
}

// boolPrefix 在金额为负时返回负号前缀，否则返回空串。
func boolPrefix(negative bool) string {
	if negative {
		return "-"
	}
	return ""
}

// turnSpeaker 把消息方向渲染为单字发言方标识。
func turnSpeaker(direction string) string {
	if strings.EqualFold(strings.TrimSpace(direction), "outgoing") {
		return "机"
	}
	return "买"
}

// formatSilenceLine 渲染业务静默维度的一行文案。
func formatSilenceLine(snapshot HealthSnapshot) string {
	if snapshot.SilenceMinutes < 0 {
		return "业务：暂无业务活动记录"
	}
	// suffix 是阈值说明；阈值为 0 表示看门狗关闭。
	suffix := fmt.Sprintf("（阈值 %d 分钟）", snapshot.SilenceThreshold)
	if snapshot.SilenceThreshold <= 0 {
		suffix = "（看门狗已关闭）"
	}
	return fmt.Sprintf("业务：%d 分钟前有活动%s", snapshot.SilenceMinutes, suffix)
}

// orUnknownDate 在日期缺失时返回占位文本。
func orUnknownDate(date string) string {
	if strings.TrimSpace(date) == "" {
		return "日期未知"
	}
	return strings.TrimSpace(date)
}

// orUnknownState 在运行时状态缺失时返回占位文本。
func orUnknownState(state string) string {
	if strings.TrimSpace(state) == "" {
		return "状态未知"
	}
	return strings.TrimSpace(state)
}

// displayAccountName 返回账号销量的展示名称，缺失名称时回退标识前缀。
func displayAccountName(account AccountSales) string {
	if strings.TrimSpace(account.AccountName) != "" {
		return strings.TrimSpace(account.AccountName)
	}
	return clipRunes(account.AccountID, maxNameRunes)
}

// displayAccountHealthName 返回账号健康状态的展示名称。
func displayAccountHealthName(account AccountHealth) string {
	if strings.TrimSpace(account.AccountName) != "" {
		return strings.TrimSpace(account.AccountName)
	}
	return clipRunes(account.AccountID, maxNameRunes)
}

// displayAccountChatsName 返回账号会话集合的展示名称。
func displayAccountChatsName(account AccountChats) string {
	if strings.TrimSpace(account.AccountName) != "" {
		return strings.TrimSpace(account.AccountName)
	}
	return clipRunes(account.AccountID, maxNameRunes)
}

// clipRunes 把文本裁剪到指定字符数，超出部分以省略号代替。
func clipRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	// normalized 是去除首尾空白后的文本。
	normalized := strings.TrimSpace(text)
	if utf8.RuneCountInString(normalized) <= limit {
		return normalized
	}
	// runes 是按字符切分后的序列。
	runes := []rune(normalized)
	return string(runes[:limit]) + "…"
}

// truncateReply 按字符数裁剪回复文本，超出时按行保留并追加截断提示。
func truncateReply(text string) string {
	if utf8.RuneCountInString(text) <= maxReplyRunes {
		return text
	}
	// lines 是按行切分后的回复文本。
	lines := strings.Split(text, "\n")
	// kept 是当前累计保留的行。
	kept := make([]string, 0, len(lines))
	// used 是当前累计字符数。
	used := 0
	// line 是当前遍历到的行。
	for _, line := range lines {
		// cost 是本行计入后的字符数，含换行符。
		cost := utf8.RuneCountInString(line) + 1
		if used+cost > maxReplyRunes-len(truncatedNotice)-1 {
			break
		}
		kept = append(kept, line)
		used += cost
	}
	if len(kept) == 0 {
		// runes 是按字符切分后的完整文本。
		runes := []rune(text)
		// budget 是扣除提示后的可用字符数。
		budget := maxReplyRunes - len(truncatedNotice) - 1
		if budget < 0 {
			budget = 0
		}
		return string(runes[:budget]) + truncatedNotice
	}
	return strings.Join(kept, "\n") + truncatedNotice
}

// truncatedNotice 是回复被裁剪时追加的提示文本。
const truncatedNotice = "\n…（内容过长已截断）"
