// ai_generation_log_test.go 验证生成归因埋点能实锤「谁答的」与「依据什么」。
//
// 这是回答业务方两个追问的唯一依据：答案来自策略话术还是模型；
// 发货渠道来自卡密事实还是文本规则探测。两项此前都无法回答。
package engine

import (
	"context"
	"testing"
	"time"
)

// TestRecordGenerationProvenance 验证生成归因记录两个正交维度都正确落库。
func TestRecordGenerationProvenance(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// ctx 是夹具上下文。
	ctx := context.Background()

	// modelCase 是「模型作答 + 卡密事实」这一理想组合。
	a.recordGeneration(ctx, ChatMessage{ChatID: "g1", Text: "有度盘吗"},
		"flash", "sys-prompt-1", answerSourceLLM, "",
		liveKnowledge{DeliverySource: deliverySourceCard, DeliveryChannels: []string{"百度网盘", "夸克网盘"}},
		"这款发百度网盘和夸克网盘～", 120*time.Millisecond)

	// policyCase 是「策略话术 + 描述关键词探测」这一最需要被区分出来的组合。
	a.recordGeneration(ctx, ChatMessage{ChatID: "g2", Text: "太贵了少点"},
		"flash", "sys-prompt-2", answerSourceMinPrice, "offer-below-minimum",
		liveKnowledge{DeliverySource: deliverySourceItemText, DeliveryChannels: []string{"夸克网盘"}},
		"可以优惠的最低价格是 5.00 元", 80*time.Millisecond)

	// query 是按会话读取归因记录的查询语句。
	const query = `SELECT answer_source, guarded, guard_reason, grounding_source, grounding_detail, latency_ms FROM ai_generation_log WHERE chat_id = ?`

	// answer、guarded、reason、grounding、detail、latency、readErr 是模型作答那条的记录与读取失败原因。
	var (
		answer    string
		reason    string
		grounding string
		detail    string
		latency   int64
		guarded   int
		readErr   error
	)
	if readErr = s.DB.QueryRowContext(ctx, query, "g1").Scan(&answer, &guarded, &reason, &grounding, &detail, &latency); readErr != nil {
		t.Fatalf("读取生成归因失败: %v", readErr)
	}
	if answer != answerSourceLLM || guarded != 0 {
		t.Errorf("模型作答不应被记为策略接管: answer=%s guarded=%d", answer, guarded)
	}
	if grounding != deliverySourceCard {
		t.Errorf("卡密渠道应记为事实来源: %s", grounding)
	}
	if detail != "百度网盘、夸克网盘" {
		t.Errorf("具体依据应落库: %s", detail)
	}
	if latency != 120 {
		t.Errorf("耗时应落库: %d", latency)
	}

	// policyAnswer、policyReason、policyGrounding、policyDetail、policyLatency、
	// policyGuarded、policyErr 是策略那条记录的各字段与读取失败原因。
	var (
		policyAnswer    string
		policyReason    string
		policyGrounding string
		policyDetail    string
		policyLatency   int64
		policyGuarded   int
		policyErr       error
	)
	if policyErr = s.DB.QueryRowContext(ctx, query, "g2").
		Scan(&policyAnswer, &policyGuarded, &policyReason, &policyGrounding, &policyDetail, &policyLatency); policyErr != nil {
		t.Fatalf("读取策略归因失败: %v", policyErr)
	}
	if policyAnswer != answerSourceMinPrice || policyGuarded != 1 {
		t.Errorf("策略话术必须与模型作答区分: answer=%s guarded=%d", policyAnswer, policyGuarded)
	}
	if policyReason != "offer-below-minimum" {
		t.Errorf("拦截原因应落库: %s", policyReason)
	}
	if policyGrounding != deliverySourceItemText {
		t.Errorf("描述关键词应记为文本规则来源: %s", policyGrounding)
	}
}

// TestRecordGenerationEmptySourceFallback 动态查询失败导致来源为空时统一归为 none，
// 避免出现空字符串让分布统计失真。
func TestRecordGenerationEmptySourceFallback(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// ctx 是夹具上下文。
	ctx := context.Background()

	a.recordGeneration(ctx, ChatMessage{ChatID: "g3", Text: "在吗"}, "flash", "p",
		answerSourceEmpty, "", liveKnowledge{}, "", 5*time.Millisecond)

	// grounding、answer 是该条记录的来源与答案来源。
	var grounding, answer string
	// err 是读取失败原因。
	if err := s.DB.QueryRowContext(ctx,
		`SELECT grounding_source, answer_source FROM ai_generation_log WHERE chat_id = 'g3'`).
		Scan(&grounding, &answer); err != nil {
		t.Fatalf("读取生成归因失败: %v", err)
	}
	if grounding != deliverySourceNone {
		t.Errorf("空来源应归为 none: %s", grounding)
	}
	if answer != answerSourceEmpty {
		t.Errorf("空回复应可归因: %s", answer)
	}
}