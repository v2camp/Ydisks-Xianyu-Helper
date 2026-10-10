// ai_generator_seam_test.go 覆盖模型生成接缝。
//
// 断言两件事：注入生成器后不再发起 HTTP 模型调用；生成器返回的文本仍要过完价格守卫与
// 对话落库。后者是接缝的核心约束——客服 Agent Loop 只能替换「怎么问模型」，
// 不能替换「怎么校验模型说了什么」。

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// recordingAIGenerator 是记录调用参数的生成器替身。
type recordingAIGenerator struct {
	// content 是替身返回的候选文本。
	content string
	// genErr 是替身返回的错误；非 nil 时调用方不得使用 content。
	genErr error
	// calls 是替身被调用的次数。
	calls int
	// lastReq 是最近一次收到的生成请求。
	lastReq GenerateRequest
}

// Generate 实现 AIGenerator：记录请求并返回预设结果。
func (r *recordingAIGenerator) Generate(_ context.Context, req GenerateRequest) (string, error) {
	r.calls++
	r.lastReq = req
	return r.content, r.genErr
}

// newSeamAIStore 构造启用 AI、配好密钥并带一件 100 元商品的测试仓储。
// discountPercent、discountAmount 是该账号的折扣上限；autoAdjust 控制是否开启真实改价。
func newSeamAIStore(t *testing.T, discountPercent, discountAmount, autoAdjust int) (*db.Store, func()) {
	t.Helper()
	// store、cleanup 是共享的 AI 测试仓储及清理函数。
	store, cleanup := newAIStore(t)
	// ctx 是准备测试数据使用的上下文。
	ctx := context.Background()
	// insertErr 表示写入账号 AI 设置时不应出现的数据库错误。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings
		(cookie_id,ai_enabled,auto_adjust_price_enabled,max_discount_percent,max_discount_amount,max_bargain_rounds,custom_prompts)
		VALUES ('cid',1,?,?,?,3,'')`, autoAdjust, discountPercent, discountAmount); insertErr != nil {
		t.Fatal(insertErr)
	}
	// itemErr 表示写入测试商品时不应出现的数据库错误。
	if _, itemErr := store.DB.ExecContext(ctx, `INSERT INTO item_info
		(cookie_id,item_id,item_title,item_price,item_description) VALUES ('cid','item-seam','商品','100','描述')`); itemErr != nil {
		t.Fatal(itemErr)
	}
	// keyErr 表示写入测试密钥时不应出现的数据库错误。
	if keyErr := store.Settings.Set(ctx, "ai_api_key", "sk-seam"); keyErr != nil {
		t.Fatal(keyErr)
	}
	// urlErr 表示写入不可达模型地址时不应出现的数据库错误；该地址必须永不被访问。
	if urlErr := store.Settings.Set(ctx, "ai_api_url", "http://127.0.0.1:1"); urlErr != nil {
		t.Fatal(urlErr)
	}
	return store, cleanup
}

// TestInjectedGeneratorReplacesHTTPModelCall 验证注入生成器后模型调用完全由替身承担。
// 模型地址被指向不可达端口：若接缝未生效，该用例会因 HTTP 失败而报错。
func TestInjectedGeneratorReplacesHTTPModelCall(t *testing.T) {
	// store、cleanup 是接缝测试仓储及清理函数。
	store, cleanup := newSeamAIStore(t, 10, 20, 0)
	defer cleanup()
	// ctx 是本次回复调用的上下文。
	ctx := context.Background()
	// stub 是替代真实模型调用的生成器替身。
	stub := &recordingAIGenerator{content: "你好，在的哦"}

	// result、err 分别是注入生成器后的回复结果与调用错误。
	result, err := NewAIReplier("cid", store, nil, stub).Reply(ctx, chatMsg("能便宜点吗", "item-seam", "chat-seam"))
	if err != nil {
		t.Fatalf("注入生成器后不应因 HTTP 失败报错: %v", err)
	}
	if result == nil || result.Text != "你好，在的哦" {
		t.Fatalf("应返回生成器文本: %+v", result)
	}
	if stub.calls != 1 {
		t.Fatalf("生成器应被调用一次: calls=%d", stub.calls)
	}
	// req 是替身最近一次收到的请求，用于断言调用方注入了完整上下文。
	req := stub.lastReq
	if req.APIKey != "sk-seam" || req.Model != defaultAIModel || req.BaseURL != "http://127.0.0.1:1" {
		t.Fatalf("生成请求未注入账号配置: %+v", req)
	}
	if req.Temperature != defaultAIGeneratorTemperature || req.Current != "能便宜点吗" {
		t.Fatalf("生成请求缺少温度或当前输入: %+v", req)
	}
	if !strings.Contains(req.System, "不可覆盖的价格安全规则") {
		t.Fatalf("生成请求缺少后端价格护栏: %q", req.System)
	}
}

// TestInjectedGeneratorHistoryIsForwardedInOrder 验证既有对话历史按时间正序传给生成器。
func TestInjectedGeneratorHistoryIsForwardedInOrder(t *testing.T) {
	// store、cleanup 是接缝测试仓储及清理函数。
	store, cleanup := newSeamAIStore(t, 10, 20, 0)
	defer cleanup()
	// ctx 是写入历史与本次回复共用上下文。
	ctx := context.Background()
	// historyErr 表示写入既有对话历史时不应出现的数据库错误。
	if historyErr := store.AIReply.AddConversationExchange(ctx, "cid", "chat-seam", "buyer-1", "item-seam",
		db.AIConversationMessage{Role: "user", Content: "第一条买家消息", Intent: "bargain", BargainCount: 0},
		db.AIConversationMessage{Role: "assistant", Content: "第一条卖家回复", Intent: "reply", BargainCount: 0},
	); historyErr != nil {
		t.Fatal(historyErr)
	}
	// stub 是记录历史传参的生成器替身。
	stub := &recordingAIGenerator{content: "好的"}

	// _, err 分别是注入生成器后的回复结果与调用错误。
	if _, err := NewAIReplier("cid", store, nil, stub).Reply(ctx, chatMsg("能便宜点吗", "item-seam", "chat-seam")); err != nil {
		t.Fatalf("注入生成器后不应报错: %v", err)
	}
	// turns 是替身收到的历史回合。
	turns := stub.lastReq.History
	if len(turns) != 2 || turns[0].Role != "user" || turns[0].Content != "第一条买家消息" || turns[1].Role != "assistant" {
		t.Fatalf("历史未按时间正序传出: %+v", turns)
	}
}

// TestInjectedGeneratorOutputStillPassesPriceGuard 验证生成器文本仍受价格守卫约束。
// 替身故意报出 10 元，最低价为 90 元，结果必须被改写成最低价话术并给出 90 元报价。
func TestInjectedGeneratorOutputStillPassesPriceGuard(t *testing.T) {
	// store、cleanup 是接缝测试仓储及清理函数。
	store, cleanup := newSeamAIStore(t, 10, 20, 1)
	defer cleanup()
	// ctx 是本次回复调用的上下文。
	ctx := context.Background()
	// stub 是故意越界报价的生成器替身。
	stub := &recordingAIGenerator{content: "可以，10.00 元成交。 [[AUTO_PRICE:10.00]]"}

	// result、err 分别是越界报价被守卫改写后的回复结果与调用错误。
	result, err := NewAIReplier("cid", store, nil, stub).Reply(ctx, chatMsg("能便宜点吗", "item-seam", "chat-seam"))
	if err != nil {
		t.Fatalf("越界报价应被守卫改写而非报错: %v", err)
	}
	if result == nil {
		t.Fatal("越界报价应返回安全回复")
	}
	if result.Text == "可以，10.00 元成交。" || strings.Contains(result.Text, "10.00") {
		t.Fatalf("越界报价必须被改写: %q", result.Text)
	}
	if !strings.Contains(result.Text, "90.00") {
		t.Fatalf("应改用最低价话术: %q", result.Text)
	}
	if result.AutoPriceQuote == nil || result.AutoPriceQuote.PriceCents != 9000 {
		t.Fatalf("报价必须由守卫按最低价推导: %+v", result.AutoPriceQuote)
	}
}

// TestInjectedGeneratorErrorPropagatesWithoutHistory 验证生成器报错时上抛且不写入半轮历史。
func TestInjectedGeneratorErrorPropagatesWithoutHistory(t *testing.T) {
	// store、cleanup 是接缝测试仓储及清理函数。
	store, cleanup := newSeamAIStore(t, 10, 20, 0)
	defer cleanup()
	// ctx 是本次回复调用的上下文。
	ctx := context.Background()
	// stub 是恒定报错的生成器替身。
	stub := &recordingAIGenerator{genErr: errors.New("模型不可用")}

	// result、err 分别是生成失败时的回复结果与调用错误。
	result, err := NewAIReplier("cid", store, nil, stub).Reply(ctx, chatMsg("能便宜点吗", "item-seam", "chat-seam"))
	if err == nil || result != nil {
		t.Fatalf("生成失败必须上抛且不返回结果: result=%+v err=%v", result, err)
	}
	// history、historyErr 分别是失败后被写入的对话历史及其读取错误。
	history, historyErr := store.AIReply.ConversationHistory(ctx, "cid", "chat-seam", "item-seam", 10)
	if historyErr != nil || len(history) != 0 {
		t.Fatalf("生成失败不应写入半轮历史: history=%+v err=%v", history, historyErr)
	}
}

// TestNewAIReplierGeneratorSelection 验证变参生成器的选取规则与默认回落。
func TestNewAIReplierGeneratorSelection(t *testing.T) {
	// store、cleanup 是接缝测试仓储及清理函数。
	store, cleanup := newSeamAIStore(t, 10, 20, 0)
	defer cleanup()
	// defaultReplier 是不传生成器时的实现，其接缝必须是默认单次补全。
	defaultReplier := NewAIReplier("cid", store, nil)
	// defaultOK 表示不传生成器时是否回落到默认实现。
	if _, defaultOK := defaultReplier.gen.(DefaultAIGenerator); !defaultOK {
		t.Fatalf("不传生成器时应回落默认实现: %T", defaultReplier.gen)
	}
	// nilReplier 是显式传 nil 生成器时的实现，同样必须回落默认实现。
	nilReplier := NewAIReplier("cid", store, nil, nil)
	// nilOK 表示显式传 nil 时是否回落到默认实现。
	if _, nilOK := nilReplier.gen.(DefaultAIGenerator); !nilOK {
		t.Fatalf("传 nil 生成器时应回落默认实现: %T", nilReplier.gen)
	}
	// first 是变参中的首个生成器，按契约只有它生效。
	first := &recordingAIGenerator{content: "首个"}
	// second 是变参中的第二个生成器，必须被忽略。
	second := &recordingAIGenerator{content: "第二个"}
	if NewAIReplier("cid", store, nil, first, second).gen != first {
		t.Fatal("变参生成器应只取第一个")
	}
}
