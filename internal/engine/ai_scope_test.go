// ai_scope_test.go AI 客服三层可配置体系的聚焦测试：边界（意图白名单与负向组合词）、
// 语料（FAQ 与在售清单）、策略（报价兜底话术），及与 AI 回复流程的端到端集成。
package engine

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// scopeStore 构造带边界/语料/策略配置的测试 store，返回清理函数。
func scopeStore(t *testing.T, scopeJSON, knowledgeJSON, policyJSON string) (*db.Store, func()) {
	t.Helper()
	// d、err 打开临时 SQLite 数据库。
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// s 是测试 store。
	s := db.NewStore(d, db.DialectSQLite)
	// ctx 用于配置写入。
	ctx := context.Background()
	if scopeJSON != "" {
		if // err 是写入边界配置的数据库错误。
		err := s.Settings.Set(ctx, settingAIScopeKey, scopeJSON); err != nil {
			t.Fatalf("写入边界配置失败: %v", err)
		}
	}
	if knowledgeJSON != "" {
		if // err 是写入语料配置的数据库错误。
		err := s.Settings.Set(ctx, settingAIKnowledgeKey, knowledgeJSON); err != nil {
			t.Fatalf("写入语料配置失败: %v", err)
		}
	}
	if policyJSON != "" {
		if // err 是写入策略配置的数据库错误。
		err := s.Settings.Set(ctx, settingAIPolicyKey, policyJSON); err != nil {
			t.Fatalf("写入策略配置失败: %v", err)
		}
	}
	return s, func() { d.Close() }
}

// TestLoadScopeFallback 未配置边界时回落内置：砍价接管，非砍价不接管。
func TestLoadScopeFallback(t *testing.T) {
	// s、cleanup 是空配置的测试 store。
	s, cleanup := scopeStore(t, "", "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// scope、err 是读取到的边界。
	scope, err := a.loadScope(context.Background())
	if err != nil {
		t.Fatalf("loadScope: %v", err)
	}
	// take、hits 为砍价文本判定结果。
	take, hits := scope.decide("能便宜点吗")
	if !take || len(hits) != 1 || hits[0] != IntentBargain {
		t.Fatalf("砍价应接管: take=%v hits=%v", take, hits)
	}
	// 询价文本不应接管（但参与命中标注）。
	take, hits = scope.decide("多少钱")
	if take {
		t.Fatalf("询价不应接管: hits=%v", hits)
	}
	if len(hits) != 1 || hits[0] != IntentInquiry {
		t.Fatalf("询价应被标注为 inquiry: hits=%v", hits)
	}
}

// TestLoadScopeConfigIntents 配置的意图白名单可扩缩接管范围，且支持非接管意图。
func TestLoadScopeConfigIntents(t *testing.T) {
	// cfg 是自定义边界：砍价 + 发货咨询接管，询价仅标注。
	cfg := `{"intents":[
		{"id":"bargain","enabled":true,"ai":true,"match":"便宜|砍价"},
		{"id":"delivery","enabled":true,"ai":true,"match":"什么盘|网盘发货"},
		{"id":"inquiry","enabled":true,"ai":false,"match":"多少钱"}
	]}`
	// s、cleanup 是带边界配置的 store。
	s, cleanup := scopeStore(t, cfg, "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// scope、err 是解析后的边界。
	scope, err := a.loadScope(context.Background())
	if err != nil {
		t.Fatalf("loadScope: %v", err)
	}
	// 发货咨询应被接管。
	take, hits := scope.decide("网盘发货吗")
	if !take || len(hits) != 1 || hits[0] != "delivery" {
		t.Fatalf("发货咨询应接管: take=%v hits=%v", take, hits)
	}
	// 询价命中了启用意图但允许接管为假 → 不接管。
	take, _ = scope.decide("多少钱")
	if take {
		t.Fatal("ai=false 的意图不应接管")
	}
	// 未配置意图的文本不接管。
	take, _ = scope.decide("你好在吗")
	if take {
		t.Fatal("未配置意图不应接管")
	}
}

// TestLoadScopeNegativeVeto 配置的负向组合词一票否决。
func TestLoadScopeNegativeVeto(t *testing.T) {
	// cfg 的负向词包含「退款」，砍价表述与其同时出现时不得接管。
	cfg := `{"intents":[{"id":"bargain","enabled":true,"ai":true,"match":"便宜"}],
		"negative":"退款|违规|扣分|封号"}`
	// s、cleanup 是带配置的 store。
	s, cleanup := scopeStore(t, cfg, "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// scope、err 是解析后的边界。
	scope, err := a.loadScope(context.Background())
	if err != nil {
		t.Fatalf("loadScope: %v", err)
	}
	// 纯砍价正常接管。
	take, _ := scope.decide("能便宜点吗")
	if !take {
		t.Fatal("纯砍价应接管")
	}
	// 砍价+退款不接管。
	take, hits := scope.decide("便宜点，不行我退款")
	if take || len(hits) != 0 {
		t.Fatalf("负向命中应一票否决: take=%v hits=%v", take, hits)
	}
}

// TestLoadScopeMalformedFallback 配置 JSON 非法或无可用意图时回落内置。
func TestLoadScopeMalformedFallback(t *testing.T) {
	// badJSON 是语法非法的配置 JSON。
	badJSON := `{"intents": [{"id":"bargain","match":"便宜"}], "negative": }`
	// s、cleanup 是非法配置的 store。
	s, cleanup := scopeStore(t, badJSON, "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// scope、err 是非法配置下的读取结果，应回落内置。
	scope, err := a.loadScope(context.Background())
	// take 是砍价文本的判定结果，内置边界应接管。
	if take, _ := scope.decide("能便宜点吗"); !take {
		t.Fatal("内置应接管砍价")
	}

	// emptyIntents 是全部禁用的意图配置，也应回落内置。
	emptyIntents := `{"intents":[{"id":"x","enabled":false,"match":"a"}]}`
	// s2、cleanup2 是无可用意图的 store。
	s2, cleanup2 := scopeStore(t, emptyIntents, "", "")
	defer cleanup2()
	// a2 用于无可用意图回退验证。
	a2 := NewAIReplier("cid", s2, nil)
	// scope2 是无可用意图时读取的边界；err 是读取错误。
	scope2, err := a2.loadScope(context.Background())
	if err != nil {
		t.Fatalf("loadScope: %v", err)
	}
	// take 是砍价文本的判定结果，无可用意图时应回落内置接管。
	if take, _ := scope2.decide("能便宜点吗"); !take {
		t.Fatal("无可用意图应回落内置并接管砍价")
	}
	_ = scope
}

// TestKnowledgeMatchedFAQAndContext 验证 FAQ 命中与在售清单拼装知识上下文。
func TestKnowledgeMatchedFAQAndContext(t *testing.T) {
	// cfg 包含一条发货 FAQ 与两条在售资源。
	cfg := `{"faq":[{"category":"发货","match":"什么盘|网盘发货","answer":"支持百度网盘与夸克发货。"}],
		"catalog":[{"title":"糯糯下山","detail":"1-3季全"},{"title":"梦遇崔郎","detail":"92集完整版"}]}`
	// s、cleanup 是带语料的 store。
	s, cleanup := scopeStore(t, "", cfg, "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// k、err 是读取到的语料。
	k, err := a.loadKnowledge(context.Background())
	if err != nil {
		t.Fatalf("loadKnowledge: %v", err)
	}
	// hits 应命中发货 FAQ。
	hits := k.matchedFAQ("是百度网盘发货吗", 3)
	if len(hits) != 1 || hits[0].Category != "发货" {
		t.Fatalf("FAQ 应命中: %+v", hits)
	}
	// ctx 应同时包含 FAQ 答案与在售清单。
	ctx := k.buildKnowledgeContext("是百度网盘发货吗")
	if !strings.Contains(ctx, "支持百度网盘与夸克发货") || !strings.Contains(ctx, "糯糯下山") || !strings.Contains(ctx, "92集完整版") {
		t.Fatalf("知识上下文缺失: %s", ctx)
	}
	// 未命中 FAQ 的问题不应输出问答段落。
	if c2 := k.buildKnowledgeContext("什么时候到货"); strings.Contains(c2, "店铺知识") {
		t.Fatalf("无命中时不应输出 FAQ 段落: %s", c2)
	}
}

// TestLoadKnowledgeEmpty 未配置语料时返回空语料而不是错误。
func TestLoadKnowledgeEmpty(t *testing.T) {
	// s、cleanup 是空语料 store。
	s, cleanup := scopeStore(t, "", "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// k、err 是读取到的空语料。
	k, err := a.loadKnowledge(context.Background())
	if err != nil || k == nil {
		t.Fatalf("空语料应返回空结构: %v", err)
	}
	if len(k.FAQ) != 0 || len(k.Catalog) != 0 {
		t.Fatalf("空语料应无条目: %+v", k)
	}
}

// TestLoadPolicyCustom 策略配置覆盖内置兜底话术；未配置时回落内置。
func TestLoadPolicyCustom(t *testing.T) {
	// s、cleanup 是带策略配置的 store。
	s, cleanup := scopeStore(t, "", "", `{"min_price_reply":"最低 {amount} 元哦","no_discount_reply":"不能再低了亲"}`)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// p、err 是读取到的策略。
	p, err := a.loadPolicy(context.Background())
	if err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}
	if p.NoDiscountReply != "不能再低了亲" || !strings.Contains(p.MinPriceReply, "{amount}") {
		t.Fatalf("策略未生效: %+v", p)
	}

	// s2、cleanup2 是空策略 store。
	s2, cleanup2 := scopeStore(t, "", "", "")
	defer cleanup2()
	// a2 是空策略 store 上构造的 AI 实现。
	a2 := NewAIReplier("cid", s2, nil)
	// p2 是读取到的兜底策略；err 是读取错误。
	p2, err := a2.loadPolicy(context.Background())
	if err != nil || !strings.Contains(p2.MinPriceReply, "{amount}") || p2.NoDiscountReply == "" {
		t.Fatalf("空策略应回落内置: %+v err=%v", p2, err)
	}
}

// TestIntentBlockedByExtra 扩展投诉词表中的任一命中即阻止接管。
func TestIntentBlockedByExtra(t *testing.T) {
	// 空扩展不拦截。
	if intentBlockedByExtra(nil, "能便宜点吗") {
		t.Fatal("空扩展不应拦截")
	}
	// extras 是含「封号」「违规」的扩展负向词表。
	extras := parseComplaintTestKeywords(t, "封号|违规")
	if !intentBlockedByExtra(extras, "会封号吗") {
		t.Fatal("扩展词表应拦截封号文本")
	}
	if intentBlockedByExtra(extras, "能便宜点吗") {
		t.Fatal("扩展词表不应拦截无关键文本")
	}
}

// parseComplaintTestKeywords 把组合词语法下的扩展词表解析为正则列表，仅用于测试断言。
func parseComplaintTestKeywords(t *testing.T, expr string) []*regexp.Regexp {
	t.Helper()
	// re 是编译（?i）大小写不敏感的整串匹配表达式。
	re, err := regexp.Compile("(?i)" + expr)
	if err != nil {
		t.Fatalf("编译扩展词表失败: %v", err)
	}
	return []*regexp.Regexp{re}
}

// TestAIReplyScopeKnowledgeIntegration 端到端验证：配置的边界与语料参与 Reply 流程。
func TestAIReplyScopeKnowledgeIntegration(t *testing.T) {
	// cfg 接管砍价与发货咨询。
	cfg := `{"intents":[
		{"id":"bargain","enabled":true,"ai":true,"match":"便宜|砍价"},
		{"id":"delivery","enabled":true,"ai":true,"match":"什么盘|网盘发货|资源码"}
	],"negative":"退款|违规|封号"}`
	// knowledge 带一条发货 FAQ 与在售清单。
	knowledge := `{"faq":[{"category":"发货","match":"什么盘|网盘发货","answer":"本店支持百度网盘、夸克、迅雷发货。"}],
		"catalog":[{"title":"糯糯下山","detail":"1-3季全"}]}`
	// s、cleanup 是带 cookie 的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// srv 是返回固定文本的模型桩。
	srv := mockOpenAIServer(t, 0, "本店支持百度网盘发货呢")
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, settingAIScopeKey, cfg)
	s.Settings.Set(ctx, settingAIKnowledgeKey, knowledge)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)

	// 发货咨询被配置接管并返回模型内容。
	res, err := a.Reply(ctx, chatMsg("是百度网盘发货吗", "item1", "chat1"))
	if err != nil || res == nil {
		t.Fatalf("发货咨询应被 AI 接管: res=%v err=%v", res, err)
	}
	if !strings.Contains(res.Text, "百度网盘") {
		t.Fatalf("回复应为模型内容: %q", res.Text)
	}

	// 负向词命中（退款）时不接管。
	res, err = a.Reply(ctx, chatMsg("便宜点吧，不然我退款", "item1", "chat2"))
	if err != nil || res != nil {
		t.Fatalf("负向命中不应接管: res=%v err=%v", res, err)
	}

	// 未配置意图（闲聊）不接管。
	res, err = a.Reply(ctx, chatMsg("在吗", "item1", "chat3"))
	if err != nil || res != nil {
		t.Fatalf("闲聊不应接管: res=%+v err=%v", res, err)
	}

	// 接管写入的意图标签应为 delivery。
	history, herr := s.AIReply.ConversationHistory(ctx, "cid", "chat1", "item1", 10)
	if herr != nil || len(history) < 2 || history[0].Intent != "delivery" {
		t.Fatalf("历史意图应为 delivery: %+v err=%v", history, herr)
	}
}
