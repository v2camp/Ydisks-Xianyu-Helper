// ai_shipping_test.go 覆盖运费咨询的接管判定、事实注入与回答约束。
//
// 背景（2026-10-21 决策）：买家问「运费多少 / 能邮寄吗 / 包邮吗」时，原意图白名单
// 一条都不覆盖，消息恒判 no-intent 直接不回复，是当时唯一持续漏单的路径。
// 放开接管的前提是先有运费事实：平台标注（包邮/不包邮/仅自提）必须直答，
// 未标注时必须答「需另议」且不得承诺包邮，否则会把回避回答换成编造承诺，风险更大。

package engine

import (
	"context"
	"strings"
	"testing"
)

// TestFormatShippingFact 验证运费事实在四种平台标注下的输出口径。
func TestFormatShippingFact(t *testing.T) {
	// cases 是平台标注到期望事实文案的映射样例。
	cases := []struct {
		// name 是用例名。
		name string
		// postInfo 是平台标注原文；空串表示未标注。
		postInfo string
		// itemExists 表示商品是否在本地库。
		itemExists bool
		// want 是期望输出的关键片段。
		want string
		// wantEmpty 表示期望返回空串。
		wantEmpty bool
	}{
		{name: "包邮", postInfo: "包邮", itemExists: true, want: "包邮，运费由卖家承担"},
		{name: "不包邮", postInfo: "不包邮", itemExists: true, want: "运费由买家承担"},
		{name: "仅自提", postInfo: "仅自提", itemExists: true, want: "仅支持自提，不发快递"},
		{name: "未标注", postInfo: "", itemExists: true, want: "运费需与卖家另行商议"},
		{name: "商品不存在", postInfo: "", itemExists: false, wantEmpty: true},
		{name: "未知标注按未标注处理", postInfo: "顺丰到付", itemExists: true, want: "运费需与卖家另行商议"},
	}
	// c 表示当前遍历到的用例。
	for _, c := range cases {
		// got 是实际输出的运费事实文案。
		got := formatShippingFact(c.postInfo, c.itemExists)
		if c.wantEmpty {
			if got != "" {
				t.Fatalf("%s：商品不存在时必须返回空串，实际=%q", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Fatalf("%s：期望包含 %q，实际=%q", c.name, c.want, got)
		}
	}
}

// TestShippingIntentAllowsTakeover 验证运费类问法在内置边界下允许 AI 接管。
// 同时锁定反向语义：退款动作仍必须被负向词拦下，不能因为放开运费而顺带放行售后纠纷。
func TestShippingIntentAllowsTakeover(t *testing.T) {
	// wantTakeover 的用例是 2026-10-18~19 四条真实生产消息及常见变体。
	wantTakeover := []string{"运费多少", "能邮寄吗", "还是你包邮过来吧", "南京运费多少", "快递费怎么算", "可以自提吗"}
	// text 表示当前遍历到的买家问法。
	for _, text := range wantTakeover {
		// takeover、hits 是接管结论与命中的意图集合。
		takeover, hits := defaultScope.decide(text)
		if !takeover {
			t.Fatalf("%q 应允许 AI 接管，实际接管=%t 命中=%v", text, takeover, hits)
		}
		if !containsIntent(hits, IntentShipping) {
			t.Fatalf("%q 应命中 shipping 意图，实际命中=%v", text, hits)
		}
	}
	// blocked 的用例必须保持不接管：负向词优先于意图，售后纠纷不能交给 AI。
	blocked := []string{"我要退款", "退货怎么弄", "我要投诉你"}
	// text 表示当前遍历到的纠纷表达。
	for _, text := range blocked {
		// takeover、hits 是接管结论与命中的意图集合。
		takeover, hits := defaultScope.decide(text)
		if takeover {
			t.Fatalf("%q 属售后纠纷，必须拒绝接管，实际命中=%v", text, hits)
		}
	}
}

// containsIntent 判断命中集合里是否包含指定意图名。
func containsIntent(hits []string, want string) bool {
	// hit 表示当前遍历到的命中意图名。
	for _, hit := range hits {
		if hit == want {
			return true
		}
	}
	return false
}

// TestBuildKnowledgeContextInjectsShippingFact 验证运费事实被注入且带独立指令。
func TestBuildKnowledgeContextInjectsShippingFact(t *testing.T) {
	// knowledge 是空语料配置，确保输出只来自动态事实。
	knowledge := &aiKnowledgeConfig{}
	// ctxText 是 buildKnowledgeContext 的产物。
	ctxText := knowledge.buildKnowledgeContext("运费多少", liveKnowledge{
		ShippingFact: shippingFactNoAnnotation,
	})
	if !strings.Contains(ctxText, shippingFactInstruction) {
		t.Fatalf("运费事实段缺少指令文案，实际=%q", ctxText)
	}
	if !strings.Contains(ctxText, shippingFactNoAnnotation) {
		t.Fatalf("运费事实段缺少事实文案，实际=%q", ctxText)
	}
	// 没有运费事实时不得凭空注入该段，避免把「查不到商品」说成「未标注」。
	emptyText := knowledge.buildKnowledgeContext("运费多少", liveKnowledge{})
	if strings.Contains(emptyText, shippingFactInstruction) {
		t.Fatalf("无运费事实时不应注入运费段，实际=%q", emptyText)
	}
}

// TestReplyInjectsShippingConstraintWhenItemUnannotated 验证端到端行为：
// 商品未标注运费时，system prompt 必须同时给出「需另议」事实与「禁止承诺包邮」约束。
// 用注入生成器捕获 system，避免依赖真实模型。
func TestReplyInjectsShippingConstraintWhenItemUnannotated(t *testing.T) {
	// store、cleanup 是接缝测试仓储及清理函数；商品 item-seam 没有 item_detail。
	store, cleanup := newSeamAIStore(t, 10, 20, 0)
	defer cleanup()
	// stub 是替代真实模型调用的生成器替身，回复内容本身不参与断言。
	stub := &recordingAIGenerator{content: "运费需要和您单独确认，以拍下时页面显示为准。"}
	// result、err 是本次回复结果与调用错误。
	result, err := NewAIReplier("cid", store, nil, stub).Reply(context.Background(), chatMsg("运费多少", "item-seam", "chat-seam"))
	if err != nil {
		t.Fatalf("运费咨询回复失败: %v", err)
	}
	if result == nil {
		t.Fatal("运费咨询应被接管并返回回复")
	}
	// system 是本次实际发给模型的系统提示。
	system := stub.lastReq.System
	if !strings.Contains(system, shippingFactNoAnnotation) {
		t.Fatalf("system 缺少未标注运费的事实，实际=%q", system)
	}
	if !strings.Contains(system, "禁止承诺包邮") {
		t.Fatalf("system 缺少禁止承诺包邮的约束，实际=%q", system)
	}
}
