package qqbot

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestParseCommandRecognizesAliases 验证四个命令及其别名都能被识别。
func TestParseCommandRecognizesAliases(t *testing.T) {
	// cases 是输入文本到期望命令的映射。
	cases := map[string]Command{
		"销量": CommandSales, "今日销量": CommandSales, "销售": CommandSales, "今日销售": CommandSales,
		"健康": CommandHealth, "健康度": CommandHealth, "系统健康": CommandHealth, "状态": CommandHealth,
		"会话": CommandChats, "会话审查": CommandChats, "最近会话": CommandChats, "对话": CommandChats,
		"帮助": CommandHelp, "HELP": CommandHelp, "?": CommandHelp, "？": CommandHelp, "菜单": CommandHelp,
	}
	// input 是当前待验证的输入文本。
	// want 是当前输入期望解析出的命令。
	for input, want := range cases {
		// got 是实际解析出的命令。
		if got := ParseCommand(input); got != want {
			t.Fatalf("ParseCommand(%q)=%q want %q", input, got, want)
		}
	}
}

// TestParseCommandUnknownInputs 验证空白与无关输入回退到未知命令。
func TestParseCommandUnknownInputs(t *testing.T) {
	// inputs 是应当判定为未知的输入集合。
	inputs := []string{"", "   ", "在吗", "帮我发货", "今天天气怎么样"}
	// input 是当前待验证的输入文本。
	for _, input := range inputs {
		// got 是实际解析出的命令。
		if got := ParseCommand(input); got != CommandUnknown {
			t.Fatalf("ParseCommand(%q)=%q want %q", input, got, CommandUnknown)
		}
	}
}

// TestNormalizeInboundTextStripsMentions 验证群聊 @ 提及前缀与空白被剥离。
func TestNormalizeInboundTextStripsMentions(t *testing.T) {
	// cases 是原始输入到期望归一化结果的映射。
	cases := map[string]string{
		"<@!1020> 销量":       "销量",
		"@bot 销量":           "销量",
		"  <@!1> <@!2> 健康 ": "健康",
		"会话":                "会话",
		"  ":                "",
		"<@!1020>":          "",
	}
	// input 是当前待验证的原始输入。
	// want 是当前输入期望的归一化结果。
	for input, want := range cases {
		// got 是实际归一化结果。
		if got := NormalizeInboundText(input); got != want {
			t.Fatalf("NormalizeInboundText(%q)=%q want %q", input, got, want)
		}
	}
}

// TestFormatSalesRendersPerAccountAndTotal 验证销量文案按账号分行并给出合计。
func TestFormatSalesRendersPerAccountAndTotal(t *testing.T) {
	// snapshot 是含两个账号的销量快照。
	snapshot := SalesSnapshot{
		Date: "2026-10-08",
		Accounts: []AccountSales{
			{AccountID: "acc-a", AccountName: "主力号", Orders: 3, AmountFen: 12800},
			{AccountID: "acc-b", AccountName: "小号", Orders: 1, AmountFen: 4500},
		},
		TotalOrders: 4,
		AmountFen:   17300,
	}
	// got 是渲染出的销量文案。
	got := FormatSales(snapshot)
	// wants 是必须出现的文案片段。
	wants := []string{"今日销量（2026-10-08）", "· 主力号：3 单 ¥128.00", "· 小号：1 单 ¥45.00", "合计：4 单 ¥173.00"}
	// want 是当前必须出现的片段。
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatSales 输出缺少 %q，实际=%q", want, got)
		}
	}
}

// TestFormatSalesEmpty 验证无成交时给出明确文案而不是空白。
func TestFormatSalesEmpty(t *testing.T) {
	// got 是空快照渲染出的文案。
	got := FormatSales(SalesSnapshot{Date: "2026-10-08"})
	if !strings.Contains(got, "暂无成交记录") {
		t.Fatalf("空销量应提示暂无成交，实际=%q", got)
	}
}

// TestFormatHealthRendersFourDimensions 验证健康文案覆盖数据库、账号、交易、业务四个维度。
func TestFormatHealthRendersFourDimensions(t *testing.T) {
	// snapshot 是含在线与异常账号的健康快照。
	snapshot := HealthSnapshot{
		DatabaseOK:       true,
		Accounts:         []AccountHealth{{AccountID: "acc-a", AccountName: "主力号", State: "online", Connected: true}, {AccountID: "acc-b", AccountName: "小号", State: "auth_expired", Failures: 3}},
		PendingIssues:    2,
		PendingDeferred:  1,
		SilenceMinutes:   3,
		SilenceThreshold: 180,
	}
	// now 是固定的观测时刻。
	now := time.Date(2026, 10, 8, 0, 55, 0, 0, time.Local)
	// got 是渲染出的健康文案。
	got := FormatHealth(snapshot, now)
	// wants 是必须出现的文案片段。
	wants := []string{"系统健康（10-08 00:55）", "数据库：正常", "账号：共 2 个，在线 1，异常 1", "· 小号 auth_expired 连续失败 3 次", "交易：待处理 2 起，死信 1 起", "业务：3 分钟前有活动（阈值 180 分钟）"}
	// want 是当前必须出现的片段。
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatHealth 输出缺少 %q，实际=%q", want, got)
		}
	}
}

// TestFormatHealthDegradedDatabaseAndNoActivity 验证数据库异常与无业务活动的渲染分支。
func TestFormatHealthDegradedDatabaseAndNoActivity(t *testing.T) {
	// snapshot 是数据库异常且无业务活动的快照。
	snapshot := HealthSnapshot{DatabaseOK: false, SilenceMinutes: -1, SilenceThreshold: 0}
	// got 是渲染出的健康文案。
	got := FormatHealth(snapshot, time.Now())
	// wants 是必须出现的文案片段。
	wants := []string{"数据库：异常", "业务：暂无业务活动记录", "账号：共 0 个，在线 0，异常 0"}
	// want 是当前必须出现的片段。
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatHealth 输出缺少 %q，实际=%q", want, got)
		}
	}
}

// TestFormatChatsGroupsByAccountAndClips 验证会话文案按账号分组且过长内容被裁剪。
func TestFormatChatsGroupsByAccountAndClips(t *testing.T) {
	// longContent 是超出单条发言展示上限的长文本。
	longContent := strings.Repeat("内容", 80)
	// digest 是含一个账号两个会话的摘要。
	digest := ChatDigest{Accounts: []AccountChats{{
		AccountID: "acc-a", AccountName: "主力号",
		Chats: []ChatBrief{
			{ChatID: "c1", PeerName: "买家张三", ItemTitle: "商品标题", Turns: []ChatTurn{{Direction: "incoming", Content: "在吗"}, {Direction: "outgoing", Content: longContent}}},
			{ChatID: "c2", PeerName: "李四", ItemTitle: "另一个商品", Turns: []ChatTurn{{Direction: "incoming", Content: "价格能少吗"}}},
		},
	}}}
	// got 是渲染出的会话文案。
	got := FormatChats(digest)
	// wants 是必须出现的文案片段。
	wants := []string{"【主力号】", "1. 买家张三 · 商品标题", "买：在吗", "机：", "2. 李四 · 另一个商品", "…"}
	// want 是当前必须出现的片段。
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatChats 输出缺少 %q，实际=%q", want, got)
		}
	}
}

// TestFormatChatsEmptyWhenNoTurns 验证没有会话内容时回退为空态文案。
func TestFormatChatsEmptyWhenNoTurns(t *testing.T) {
	// got 是空摘要渲染出的文案。
	if got := FormatChats(ChatDigest{}); !strings.Contains(got, "暂无会话记录") {
		t.Fatalf("空会话应提示暂无记录，实际=%q", got)
	}
	// gotOnlyTitle 是仅含无会话账号时的渲染结果。
	if gotOnlyTitle := FormatChats(ChatDigest{Accounts: []AccountChats{{AccountID: "a", AccountName: "空号"}}}); !strings.Contains(gotOnlyTitle, "暂无会话记录") {
		t.Fatalf("账号无会话应提示暂无记录，实际=%q", gotOnlyTitle)
	}
}

// TestFormatFenRendersYuan 验证分转元的金额格式化，含补位与负号。
func TestFormatFenRendersYuan(t *testing.T) {
	// cases 是输入分值与期望文本的映射。
	cases := map[int64]string{0: "¥0.00", 1: "¥0.01", 10: "¥0.10", 99: "¥0.99", 100: "¥1.00", 12800: "¥128.00", -4500: "¥-45.00"}
	// fen 是当前待验证的分值。
	// want 是当前分值期望的文本。
	for fen, want := range cases {
		// got 是实际格式化结果。
		if got := FormatFen(fen); got != want {
			t.Fatalf("FormatFen(%d)=%q want %q", fen, got, want)
		}
	}
}

// TestTruncateReplyKeepsWithinLimit 验证超长回复被裁剪到上限内并追加提示。
func TestTruncateReplyKeepsWithinLimit(t *testing.T) {
	// builder 用于构造超长的多行回复。
	var builder strings.Builder
	// index 是当前行序号。
	for index := 0; index < 500; index++ {
		builder.WriteString("这是一行会话内容用于触发裁剪逻辑\n")
	}
	// got 是裁剪后的文本。
	got := truncateReply(builder.String())
	// count 是裁剪结果的字符数。
	if count := utf8.RuneCountInString(got); count > maxReplyRunes {
		t.Fatalf("裁剪后字符数 %d 超过上限 %d", count, maxReplyRunes)
	}
	if !strings.HasSuffix(got, truncatedNotice) {
		t.Fatalf("裁剪结果应追加截断提示，实际尾部=%q", got)
	}
}

// TestTruncateReplyKeepsShortText 验证未超限时原文不变。
func TestTruncateReplyKeepsShortText(t *testing.T) {
	// text 是短文本。
	text := "销量\n合计：1 单 ¥1.00"
	// got 是裁剪后的文本。
	if got := truncateReply(text); got != text {
		t.Fatalf("短文本不应被裁剪，实际=%q", got)
	}
}
