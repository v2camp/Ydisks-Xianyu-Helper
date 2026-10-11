// ai_decision_log_test.go 验证接管决策归因埋点。
//
// 现状缺陷背景：接管判定为 false 时 Reply 直接返回，既不落库也不记日志，
// 导致评测发现回复不合格后无法回答「这条消息当时为什么被放行或拦下」。
// 本文件锁定「接管与不接管都要留痕」以及原因简码的正确性。
package engine

import (
	"context"
	"testing"
)

// TestRecordDecisionLogsBothOutcomes 接管与不接管都必须写入归因记录。
func TestRecordDecisionLogsBothOutcomes(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// ctx 是夹具上下文。
	ctx := context.Background()

	// 被接管的消息：命中发货意图，未命中负向词。
	a.recordDecision(ctx, ChatMessage{ChatID: "chat-taken", Text: "有度盘的吗"},
		"fp-1", []string{IntentOrder}, false, false, true, IntentOrder)
	// 被负向词拦下的消息：这条过去完全没有痕迹。
	a.recordDecision(ctx, ChatMessage{ChatID: "chat-blocked", Text: "我要退款"},
		"fp-1", nil, true, false, false, IntentComplaint)
	// 被扩展词拦下的消息。
	a.recordDecision(ctx, ChatMessage{ChatID: "chat-extra", Text: "加个微信"},
		"fp-1", nil, false, true, false, IntentComplaint)

	// total 是归因记录总条数。
	var total int
	// err 是统计失败原因。
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_decision_log`).Scan(&total); err != nil {
		t.Fatalf("统计归因记录失败: %v", err)
	}
	if total != 3 {
		t.Fatalf("接管与不接管都应留痕，实际 %d 条", total)
	}

	// cases 是逐条校验的期望：会话标识到原因简码。
	cases := map[string]string{
		"chat-taken":   "taken",
		"chat-blocked": "negative",
		"chat-extra":   "extra-negative",
	}
	// chatID、want 是当前遍历到的会话与期望原因。
	for chatID, want := range cases {
		// reason、takeover、scanErr 是该条记录的原因、接管状态与读取失败原因。
		var (
			reason   string
			takeover int
			scanErr  error
		)
		if scanErr = s.DB.QueryRowContext(ctx,
			`SELECT reason, takeover FROM ai_decision_log WHERE chat_id = ?`, chatID).
			Scan(&reason, &takeover); scanErr != nil {
			t.Fatalf("读取 %s 归因失败: %v", chatID, scanErr)
		}
		if reason != want {
			t.Errorf("%s 原因简码错误：got %q want %q", chatID, reason, want)
		}
		// wantTakeover 是该条记录期望的接管状态，与库中整型取值对齐。
		wantTakeover := 0
		if want == "taken" {
			wantTakeover = 1
		}
		if takeover != wantTakeover {
			t.Errorf("%s 接管状态与原因不一致", chatID)
		}
	}
}

// TestRecordDecisionStoresFingerprintAndIntent 归因记录必须带上配置指纹与命中意图，
// 否则无法区分「模型波动」与「配置变更」。
func TestRecordDecisionStoresFingerprintAndIntent(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// ctx 是夹具上下文。
	ctx := context.Background()

	a.recordDecision(ctx, ChatMessage{ChatID: "chat-fp", Text: "怎么发货"},
		"fp-abc", []string{IntentOrder, IntentStock}, false, false, true, IntentOrder)

	// fingerprint、intents、label 是该条记录的指纹、命中意图与标签。
	var fingerprint, intents, label string
	// err 是读取失败原因。
	if err := s.DB.QueryRowContext(ctx,
		`SELECT config_fingerprint, hit_intents, intent_label FROM ai_decision_log WHERE chat_id = 'chat-fp'`).
		Scan(&fingerprint, &intents, &label); err != nil {
		t.Fatalf("读取归因记录失败: %v", err)
	}
	if fingerprint != "fp-abc" {
		t.Errorf("配置指纹未落库: %q", fingerprint)
	}
	if intents != IntentOrder+","+IntentStock {
		t.Errorf("命中意图未按序落库: %q", intents)
	}
	if label != IntentOrder {
		t.Errorf("意图标签未落库: %q", label)
	}
}

// TestScopeFingerprintChangesWithConfig 配置变更必须改变指纹。
func TestScopeFingerprintChangesWithConfig(t *testing.T) {
	// base 是基准配置。
	base := aiScopeConfig{Negative: "退款", Intents: []aiIntentConfig{{ID: "delivery", Enabled: true, AI: true, Match: "网盘"}}}
	// toggled 是把允许接管翻转为不允许的同形配置。
	toggled := aiScopeConfig{Negative: "退款", Intents: []aiIntentConfig{{ID: "delivery", Enabled: true, AI: false, Match: "网盘"}}}
	// widened 是把负向词放宽的配置。
	widened := aiScopeConfig{Negative: "", Intents: []aiIntentConfig{{ID: "delivery", Enabled: true, AI: true, Match: "网盘"}}}

	if scopeFingerprint("raw", base) == scopeFingerprint("raw", toggled) {
		t.Error("接管开关变化必须改变指纹")
	}
	if scopeFingerprint("raw", base) == scopeFingerprint("raw", widened) {
		t.Error("负向词变化必须改变指纹")
	}
	if scopeFingerprint("raw", base) != scopeFingerprint("raw", base) {
		t.Error("相同配置的指纹必须稳定")
	}
}
