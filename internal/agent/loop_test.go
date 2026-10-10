// loop_test.go 覆盖客服 Agent 循环的编排与预算，以及适配器的回落行为。

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"xianyu-go/internal/capability"
	"xianyu-go/internal/engine"
)

// scriptedModel 是按调用顺序返回预设结果的模型端口替身。
type scriptedModel struct {
	// results 是第 N 次调用返回的结果；超出长度时返回空结果。
	results []StepResult
	// stepErr 是恒定返回的失败原因；非 nil 时每次都失败。
	stepErr error
	// requests 记录每次调用收到的请求。
	requests []StepRequest
}

// Step 记录请求并返回脚本中的对应结果。
func (m *scriptedModel) Step(_ context.Context, req StepRequest) (StepResult, error) {
	m.requests = append(m.requests, req)
	if m.stepErr != nil {
		return StepResult{}, m.stepErr
	}
	// index 是本次调用的序号。
	index := len(m.requests) - 1
	if index >= len(m.results) {
		return StepResult{}, nil
	}
	return m.results[index], nil
}

// fakeGenerator 是 engine.AIGenerator 替身，用于验证回落路径。
type fakeGenerator struct {
	// content 是回落时返回的正文。
	content string
	// genErr 是回落时返回的失败原因。
	genErr error
	// calls 是回落生成器被调用的次数。
	calls int
}

// Generate 记录调用次数并返回预设结果。
func (g *fakeGenerator) Generate(_ context.Context, _ engine.GenerateRequest) (string, error) {
	g.calls++
	return g.content, g.genErr
}

// request 构造一次最小生成请求。
func request() engine.GenerateRequest {
	return engine.GenerateRequest{
		APIKey: "sk-test", Model: "test-model", Temperature: 0.7,
		System: "系统提示", History: []engine.ConversationTurn{{Role: "user", Content: "历史"}},
		Current: "买家问题",
	}
}

// newTestLoop 构造带替身模型与工具集的循环。
func newTestLoop(t *testing.T, model ModelClient, budget Budget) (*Loop, *fakeOrderPorts) {
	t.Helper()
	// tools、orders、_ 分别是工具集与订单端口替身。
	tools, orders, _ := newTestTools(t)
	return NewLoop(model, tools, budget, nil), orders
}

// toolCallList 生成 count 个订单列表调用，用于验证单轮调用上限。
func toolCallList(count int) []ToolCall {
	// calls 是生成的工具调用列表。
	calls := make([]ToolCall, 0, count)
	// index 是当前生成的调用序号。
	for index := 0; index < count; index++ {
		calls = append(calls, ToolCall{ID: fmt.Sprintf("call-%d", index), Name: capability.CapOrderList})
	}
	return calls
}

// TestLoopAnswersDirectlyWhenModelRequestsNoTools 验证模型直接作答时循环只调用一次模型，
// 且把当前会话可见的工具声明下发给它。
func TestLoopAnswersDirectlyWhenModelRequestsNoTools(t *testing.T) {
	// model 是直接作答的模型替身。
	model := &scriptedModel{results: []StepResult{{Content: " 你好，在的 "}}}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{})

	// got、err 分别是循环输出与失败原因。
	got, err := loop.Run(context.Background(), sessionFor("cid-own", capability.PresetReadonly), request())
	if err != nil || got != "你好，在的" {
		t.Fatalf("循环应返回去空白后的正文: got=%q err=%v", got, err)
	}
	if len(model.requests) != 1 || len(model.requests[0].Tools) != 4 {
		t.Fatalf("应只调用一次模型且下发四个只读工具: %+v", model.requests)
	}
}

// TestLoopExecutesToolCallThenAnswers 验证工具回合被真正执行，且回执消息按调用标识回灌给模型。
func TestLoopExecutesToolCallThenAnswers(t *testing.T) {
	// model 是先请求工具再作答的模型替身。
	model := &scriptedModel{results: []StepResult{
		{ToolCalls: []ToolCall{{ID: "t1", Name: capability.CapOrderList, Args: map[string]any{"status": "paid"}}}},
		{Content: "已为你查到订单"},
	}}
	// loop、orders 分别是待测试循环与订单端口替身。
	loop, orders := newTestLoop(t, model, Budget{})

	// got、err 分别是循环输出与失败原因。
	got, err := loop.Run(context.Background(), sessionFor("cid-own", capability.PresetReadonly), request())
	if err != nil || got != "已为你查到订单" {
		t.Fatalf("循环应在工具回合后作答: got=%q err=%v", got, err)
	}
	if len(orders.listQueries) != 1 || orders.listQueries[0].CookieID != "cid-own" {
		t.Fatalf("工具应被真正执行且锁定会话账号: %+v", orders.listQueries)
	}
	// second 是第二次模型调用收到的请求。
	second := model.requests[1]
	// receipt 是消息列表末尾的工具回执。
	receipt := second.Messages[len(second.Messages)-1]
	if receipt.Role != roleTool || receipt.ToolCallID != "t1" || strings.TrimSpace(receipt.Content) == "" {
		t.Fatalf("工具回执必须按调用标识回灌: %+v", receipt)
	}
}

// TestLoopCapsCallsPerRound 验证单轮工具调用数超限时只执行上限内的调用，其余只回执不执行。
func TestLoopCapsCallsPerRound(t *testing.T) {
	// model 是一次请求八个工具调用的模型替身。
	model := &scriptedModel{results: []StepResult{
		{ToolCalls: toolCallList(8)},
		{Content: "完成"},
	}}
	// loop、orders 分别是待测试循环与订单端口替身。
	loop, orders := newTestLoop(t, model, Budget{MaxRounds: 2, MaxCallsPerRound: 6})

	// got、err 分别是循环输出与失败原因。
	got, err := loop.Run(context.Background(), sessionFor("cid-own", capability.PresetReadonly), request())
	if err != nil || got != "完成" {
		t.Fatalf("循环应在超限后继续作答: got=%q err=%v", got, err)
	}
	if len(orders.listQueries) != 6 {
		t.Fatalf("单轮最多执行 6 次工具调用: got=%d", len(orders.listQueries))
	}
	// second 是第二次模型调用收到的请求，其工具回执数必须与请求的调用数一致。
	second := model.requests[1]
	// receipts 是本次回合产生的全部工具回执。
	receipts := 0
	// message 是当前遍历到的消息。
	for _, message := range second.Messages {
		if message.Role == roleTool {
			receipts++
		}
	}
	if receipts != 8 {
		t.Fatalf("每个工具调用都必须有回执: got=%d", receipts)
	}
	if !strings.Contains(second.Messages[len(second.Messages)-1].Content, "已超过上限") {
		t.Fatalf("超限调用应回执上限提示: %q", second.Messages[len(second.Messages)-1].Content)
	}
}

// TestLoopReportsBudgetExhausted 验证模型反复请求工具时循环以预算耗尽收束。
func TestLoopReportsBudgetExhausted(t *testing.T) {
	// model 是每轮都请求工具的模型替身。
	model := &scriptedModel{}
	// round 是填充脚本用的序号；脚本长度取回合数上限加二，保证模型每轮都会请求工具。
	for round := 0; round <= 2; round++ {
		model.results = append(model.results, StepResult{ToolCalls: toolCallList(1)})
	}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{MaxRounds: 1, MaxCallsPerRound: 1})

	// got、err 分别是循环输出与失败原因。
	got, err := loop.Run(context.Background(), sessionFor("cid-own", capability.PresetReadonly), request())
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("预算耗尽必须返回哨兵错误: got=%q err=%v", got, err)
	}
}

// TestLoopSurfacesModelFailure 验证模型失败时上抛错误交由上层回落。
func TestLoopSurfacesModelFailure(t *testing.T) {
	// model 是恒定失败的模型替身。
	model := &scriptedModel{stepErr: errors.New("模型不可用")}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{})

	if // err 是模型失败时上抛的失败原因。
	_, err := loop.Run(context.Background(), sessionFor("cid-own", capability.PresetReadonly), request()); err == nil {
		t.Fatal("模型失败必须上抛")
	}
}

// TestLoopOmitsToolsWhenToolSourceMissing 验证工具层缺失时循环退化为单次问答且不下发工具声明。
func TestLoopOmitsToolsWhenToolSourceMissing(t *testing.T) {
	// model 是直接作答的模型替身。
	model := &scriptedModel{results: []StepResult{{Content: "纯问答"}}}
	// loop 是无工具层的循环。
	loop := NewLoop(model, nil, Budget{}, nil)

	// got、err 分别是循环输出与失败原因。
	got, err := loop.Run(context.Background(), sessionFor("cid-own", capability.PresetReadonly), request())
	if err != nil || got != "纯问答" {
		t.Fatalf("无工具层时仍应可作答: got=%q err=%v", got, err)
	}
	if len(model.requests) != 1 || len(model.requests[0].Tools) != 0 {
		t.Fatalf("无工具层时不得下发工具声明: %+v", model.requests)
	}
}

// TestReplierReturnsAgentAnswerOnSuccess 验证循环成功时适配器直接返回 Agent 答复。
func TestReplierReturnsAgentAnswerOnSuccess(t *testing.T) {
	// model 是直接作答的模型替身。
	model := &scriptedModel{results: []StepResult{{Content: "Agent 答复"}}}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{})
	// fallback 是回落生成器替身，本用例中不应被调用。
	fallback := &fakeGenerator{content: "回落答复"}
	// replier 是待测试适配器。
	replier := NewReplier(loop, sessionFor("cid-own", capability.PresetReadonly), fallback, nil)

	// got、err 分别是适配器输出与失败原因。
	got, err := replier.Generate(context.Background(), request())
	if err != nil || got != "Agent 答复" {
		t.Fatalf("循环成功时应返回 Agent 答复: got=%q err=%v", got, err)
	}
	if fallback.calls != 0 {
		t.Fatalf("循环成功时不应触发回落: %d", fallback.calls)
	}
}

// TestReplierFallsBackWhenLoopFails 验证预算耗尽与模型失败都回落到单次问答，买家不会静默无回复。
func TestReplierFallsBackWhenLoopFails(t *testing.T) {
	// cases 是两种必须回落的失败场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// model 是该场景使用的模型替身。
		model ModelClient
		// budget 是该场景使用的循环预算。
		budget Budget
	}{
		{name: "预算耗尽", model: &scriptedModel{results: []StepResult{
			{ToolCalls: toolCallList(1)}, {ToolCalls: toolCallList(1)},
		}}, budget: Budget{MaxRounds: 1, MaxCallsPerRound: 1}},
		{name: "模型失败", model: &scriptedModel{stepErr: errors.New("模型不可用")}, budget: Budget{}},
	}
	// current 是当前待验证的场景。
	for _, current := range cases {
		// loop、_ 分别是当前场景的循环与订单端口替身。
		loop, _ := newTestLoop(t, current.model, current.budget)
		// fallback 是回落生成器替身。
		fallback := &fakeGenerator{content: "回落答复"}
		// replier 是当前场景的适配器。
		replier := NewReplier(loop, sessionFor("cid-own", capability.PresetReadonly), fallback, nil)

		// got、err 分别是适配器输出与失败原因。
		got, err := replier.Generate(context.Background(), request())
		if err != nil || got != "回落答复" {
			t.Fatalf("场景[%s]应回落到单次问答: got=%q err=%v", current.name, got, err)
		}
		if fallback.calls != 1 {
			t.Fatalf("场景[%s]回落生成器应被调用一次: %d", current.name, fallback.calls)
		}
	}
}

// TestReplierUsesDefaultFallbackGenerator 验证未注入回落生成器时使用 engine 的默认单次问答实现。
func TestReplierUsesDefaultFallbackGenerator(t *testing.T) {
	// loop 是不参与本用例断言的循环。
	loop := NewLoop(&scriptedModel{}, nil, Budget{}, nil)
	// replier 是未注入回落生成器的适配器。
	replier := NewReplier(loop, sessionFor("cid-own", capability.PresetReadonly), nil, nil)
	if // isDefault 表示回落生成器是否为 engine 的默认单次问答实现。
	_, isDefault := replier.fallback.(engine.DefaultAIGenerator); !isDefault {
		t.Fatalf("未注入回落生成器时应使用 engine 默认实现: %T", replier.fallback)
	}
}

// TestBudgetNormalizedAppliesDefaults 验证预算零值回落到内置默认值，显式值被原样保留。
func TestBudgetNormalizedAppliesDefaults(t *testing.T) {
	// defaults 是零值预算补齐后的结果。
	defaults := Budget{}.normalized()
	if defaults.MaxRounds != defaultMaxRounds || defaults.MaxCallsPerRound != defaultMaxCallsPerRound ||
		defaults.TotalTimeout != defaultTotalTimeout || defaults.ToolTimeout != defaultToolTimeout {
		t.Fatalf("零值预算应补齐默认值: %+v", defaults)
	}
	// explicit 是显式给定全部字段的预算。
	explicit := Budget{MaxRounds: 7, MaxCallsPerRound: 3, TotalTimeout: 1, ToolTimeout: 2}.normalized()
	if explicit.MaxRounds != 7 || explicit.MaxCallsPerRound != 3 || explicit.TotalTimeout != 1 || explicit.ToolTimeout != 2 {
		t.Fatalf("显式预算不应被覆盖: %+v", explicit)
	}
}
