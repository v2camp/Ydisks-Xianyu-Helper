// replier_test.go 覆盖客服 Agent 生成器适配器的选路：解析失败与未启用回落、
// 循环成功直返、循环失败回落，以及「每次补全都重新解析」这一接线前提。

package agent

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/capability"
	"xianyu-go/internal/engine"
)

// scriptedSessions 是恒定返回预设结果的账号级会话解析端口替身。
type scriptedSessions struct {
	// session 是解析成功时返回的会话配置。
	session Session
	// enabled 是解析成功时返回的启用状态。
	enabled bool
	// resolveErr 是恒定返回的解析失败原因；非 nil 时每次都失败。
	resolveErr error
	// calls 是解析被调用的次数。
	calls int
}

// ResolveSession 记录调用次数并返回预设结果。
func (s *scriptedSessions) ResolveSession(_ context.Context, _ string) (Session, bool, error) {
	s.calls++
	return s.session, s.enabled, s.resolveErr
}

// mustReplier 构造注入了指定解析端口的适配器。
func mustReplier(t *testing.T, loop *Loop, sessions SessionResolver, fallback engine.AIGenerator) *Replier {
	t.Helper()
	// replier、err 分别是待测试适配器与其构造失败原因。
	replier, err := NewReplier(ReplierConfig{Loop: loop, Sessions: sessions, CookieID: "cid-own", Fallback: fallback})
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	return replier
}

// enabledSessions 返回固定为「已启用且只读档」的解析端口替身。
func enabledSessions() *scriptedSessions {
	return &scriptedSessions{session: sessionFor("cid-own", capability.PresetReadonly), enabled: true}
}

// TestNewReplierRequiresDependencies 验证必需依赖缺失时构造期即失败，而不是留到首次补全。
func TestNewReplierRequiresDependencies(t *testing.T) {
	// loop 是不参与本用例断言的循环。
	loop := NewLoop(&scriptedModel{}, nil, Budget{}, nil)
	// cases 是两种必需依赖分别缺失的场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// cfg 是该场景的构造参数。
		cfg ReplierConfig
	}{
		{name: "缺循环", cfg: ReplierConfig{Sessions: enabledSessions(), CookieID: "cid-own"}},
		{name: "缺会话解析端口", cfg: ReplierConfig{Loop: loop, CookieID: "cid-own"}},
	}
	// current 是当前待验证的场景。
	for _, current := range cases {
		// replier、err 分别是当前场景的构造结果与失败原因。
		replier, err := NewReplier(current.cfg)
		if err == nil || replier != nil {
			t.Fatalf("场景[%s]必需依赖缺失时必须构造失败: replier=%v err=%v", current.name, replier, err)
		}
	}
}

// TestReplierReturnsAgentAnswerOnSuccess 验证循环成功时适配器直接返回 Agent 答复。
func TestReplierReturnsAgentAnswerOnSuccess(t *testing.T) {
	// model 是直接作答的模型替身。
	model := &scriptedModel{results: []StepResult{{Content: "Agent 答复"}}}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{})
	// sessions 是已启用的解析端口替身。
	sessions := enabledSessions()
	// fallback 是回落生成器替身，本用例中不应被调用。
	fallback := &fakeGenerator{content: "回落答复"}
	// replier 是待测试适配器。
	replier := mustReplier(t, loop, sessions, fallback)

	// got、err 分别是适配器输出与失败原因。
	got, err := replier.Generate(context.Background(), request())
	if err != nil || got != "Agent 答复" {
		t.Fatalf("循环成功时应返回 Agent 答复: got=%q err=%v", got, err)
	}
	if fallback.calls != 0 {
		t.Fatalf("循环成功时不应触发回落: %d", fallback.calls)
	}
	if len(model.requests) != 1 || len(model.requests[0].Tools) == 0 {
		t.Fatalf("已启用时必须把工具声明下发给模型: %+v", model.requests)
	}
}

// TestReplierFallsBackWhenDisabled 验证账号未启用客服 Agent 时不运行循环，直接走单次问答。
func TestReplierFallsBackWhenDisabled(t *testing.T) {
	// model 是不应被调用的模型替身。
	model := &scriptedModel{results: []StepResult{{Content: "Agent 答复"}}}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{})
	// sessions 是未启用的解析端口替身。
	sessions := enabledSessions()
	sessions.enabled = false
	// fallback 是回落生成器替身。
	fallback := &fakeGenerator{content: "回落答复"}
	// replier 是待测试适配器。
	replier := mustReplier(t, loop, sessions, fallback)

	// got、err 分别是适配器输出与失败原因。
	got, err := replier.Generate(context.Background(), request())
	if err != nil || got != "回落答复" {
		t.Fatalf("未启用时应回落到单次问答: got=%q err=%v", got, err)
	}
	if len(model.requests) != 0 || fallback.calls != 1 {
		t.Fatalf("未启用时不得运行循环: 模型调用=%d 回落调用=%d", len(model.requests), fallback.calls)
	}
}

// TestReplierFallsBackWhenResolveFails 验证配置不可解析时按未启用处理，不带工具答复。
func TestReplierFallsBackWhenResolveFails(t *testing.T) {
	// model 是不应被调用的模型替身。
	model := &scriptedModel{results: []StepResult{{Content: "Agent 答复"}}}
	// loop、_ 分别是待测试循环与订单端口替身。
	loop, _ := newTestLoop(t, model, Budget{})
	// sessions 是恒定失败的解析端口替身；enabled 为真用于证明失败优先于启用。
	sessions := enabledSessions()
	sessions.resolveErr = errors.New("配置读取失败")
	// fallback 是回落生成器替身。
	fallback := &fakeGenerator{content: "回落答复"}
	// replier 是待测试适配器。
	replier := mustReplier(t, loop, sessions, fallback)

	// got、err 分别是适配器输出与失败原因。
	got, err := replier.Generate(context.Background(), request())
	if err != nil || got != "回落答复" {
		t.Fatalf("解析失败时应回落到单次问答: got=%q err=%v", got, err)
	}
	if len(model.requests) != 0 {
		t.Fatalf("解析失败时不得运行循环: %d", len(model.requests))
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
		replier := mustReplier(t, loop, enabledSessions(), fallback)

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

// TestReplierResolvesSessionPerCall 验证每次补全都重新解析账号配置。
// 这是接线前提：管理界面的开关与档位必须在不重启账号的情况下生效。
func TestReplierResolvesSessionPerCall(t *testing.T) {
	// loop 是恒定直接作答的循环。
	loop := NewLoop(&scriptedModel{results: []StepResult{{Content: "答复"}, {Content: "答复"}}}, nil, Budget{}, nil)
	// sessions 是被观察调用次数的解析端口替身。
	sessions := enabledSessions()
	// replier 是待测试适配器。
	replier := mustReplier(t, loop, sessions, &fakeGenerator{content: "回落答复"})

	// index 是当前补全的序号，两次调用之间不重启任何东西。
	for index := 0; index < 2; index++ {
		// got、err 分别是本次补全的输出与失败原因。
		got, err := replier.Generate(context.Background(), request())
		if err != nil || got != "答复" {
			t.Fatalf("第 %d 次补全失败: got=%q err=%v", index+1, got, err)
		}
	}
	if sessions.calls != 2 {
		t.Fatalf("每次补全都应重新解析账号配置: %d", sessions.calls)
	}
}

// TestReplierUsesDefaultFallbackGenerator 验证未注入回落生成器时使用 engine 的默认单次问答实现。
func TestReplierUsesDefaultFallbackGenerator(t *testing.T) {
	// loop 是不参与本用例断言的循环。
	loop := NewLoop(&scriptedModel{}, nil, Budget{}, nil)
	// replier 是未注入回落生成器的适配器。
	replier := mustReplier(t, loop, enabledSessions(), nil)
	if // isDefault 表示回落生成器是否为 engine 的默认单次问答实现。
	_, isDefault := replier.fallback.(engine.DefaultAIGenerator); !isDefault {
		t.Fatalf("未注入回落生成器时应使用 engine 默认实现: %T", replier.fallback)
	}
}
