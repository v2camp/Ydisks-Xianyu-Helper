package engine

import (
	"context"
	"testing"
	"time"
)

// TestAIReadinessIntervalFromEnv 周期解析：空值与非法值回落默认，0 关闭，负数回落，过短抬到下限。
func TestAIReadinessIntervalFromEnv(t *testing.T) {
	// cases 是环境变量原文与期望周期。
	cases := []struct {
		// raw 是环境变量原文。
		raw string
		// want 是期望周期；0 表示关闭。
		want time.Duration
	}{
		{raw: "", want: defaultAIReadinessInterval},
		{raw: "abc", want: defaultAIReadinessInterval},
		{raw: "0", want: 0},
		{raw: "-5", want: defaultAIReadinessInterval},
		{raw: "5", want: minAIReadinessInterval},
		{raw: "7200", want: 2 * time.Hour},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		// got 是解析出的周期。
		got := aiReadinessIntervalFromEnv(func(string) string { return c.raw })
		if got != c.want {
			t.Fatalf("raw=%q got=%v want=%v", c.raw, got, c.want)
		}
	}
}

// TestCheckAIReadiness_NoEnabledAccount 没有账号启用 AI 时必须给出阻断说明，
// 而不是静默返回「一切正常」——这正是三周以来「以为在跑其实没跑」的形态。
func TestCheckAIReadiness_NoEnabledAccount(t *testing.T) {
	// s、cleanup 是带一个账号但没有 AI 配置的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// rep、err 是本次自检结果与失败原因。
	rep, err := CheckAIReadiness(context.Background(), s)
	if err != nil {
		t.Fatalf("自检不应报错: %v", err)
	}
	if rep.Ready() {
		t.Fatalf("没有启用账号时不得判定就绪")
	}
	if len(rep.Problems()) == 0 {
		t.Fatalf("没有启用账号时必须给出阻断说明")
	}
}

// TestCheckAIReadiness_EnabledButMissingKey 启用但缺凭证是 G4 阻断，必须单列到凭据问题里。
func TestCheckAIReadiness_EnabledButMissingKey(t *testing.T) {
	// s、cleanup 是测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// 插入启用行，但不配置任何密钥。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`); err != nil {
		t.Fatalf("插入启用行: %v", err)
	}
	// rep、err 是本次自检结果与失败原因。
	rep, err := CheckAIReadiness(ctx, s)
	if err != nil {
		t.Fatalf("自检不应报错: %v", err)
	}
	if rep.Ready() {
		t.Fatalf("缺密钥时不得判定就绪")
	}
	if len(rep.CredentialIssues) != 1 {
		t.Fatalf("应恰好报告一条凭据问题，got=%v", rep.CredentialIssues)
	}
}

// TestCheckAIReadiness_ReadyWithKeyAndScope 开关、密钥、边界都具备时应判定就绪。
func TestCheckAIReadiness_ReadyWithKeyAndScope(t *testing.T) {
	// s、cleanup 是测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 是启用行写入失败原因；写不进去就无从验证就绪判定。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`); err != nil {
		t.Fatalf("插入启用行: %v", err)
	}
	// err 是密钥写入失败原因；缺密钥会走 G4 阻断分支。
	if err := s.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatalf("写入密钥: %v", err)
	}
	// err 是边界配置写入失败原因；写入后 ScopeConfigured 应为真。
	if err := s.Settings.Set(ctx, settingAIScopeKey, `{"intents":[]}`); err != nil {
		t.Fatalf("写入边界配置: %v", err)
	}
	// rep、err 是本次自检结果与失败原因。
	rep, err := CheckAIReadiness(ctx, s)
	if err != nil {
		t.Fatalf("自检不应报错: %v", err)
	}
	if !rep.Ready() {
		t.Fatalf("应判定就绪，problems=%v", rep.Problems())
	}
	if !rep.ScopeConfigured {
		t.Fatalf("边界配置已写入时应标记为已配置")
	}
	if len(rep.EnabledCookieIDs) != 1 || rep.EnabledCookieIDs[0] != "cid" {
		t.Fatalf("启用账号清单不符：%v", rep.EnabledCookieIDs)
	}
}

// TestAIReadinessProbe_StopsOnCancel 探针必须在 ctx 取消后退出，关闭流程才能 Join。
func TestAIReadinessProbe_StopsOnCancel(t *testing.T) {
	// 周期设成下限，避免测试依赖真实等待。
	t.Setenv(aiReadinessIntervalEnv, "60")
	// s、cleanup 是测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// p 是待测探针。
	p := NewAIReadinessProbe(s, nil)
	if p == nil {
		t.Fatalf("store 非空时应构造出探针")
	}
	// ctx、cancel 控制探针生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)
	cancel()
	// err 是等待探针退出失败原因；取消后必须无阻塞返回。
	if err := p.WaitContext(context.Background()); err != nil {
		t.Fatalf("取消后应正常退出: %v", err)
	}
}

// TestNewAIReadinessProbe_DisabledWhenZero 周期配置为 0 时显式关闭，返回 nil 让调用方跳过登记。
func TestNewAIReadinessProbe_DisabledWhenZero(t *testing.T) {
	t.Setenv(aiReadinessIntervalEnv, "0")
	// s、cleanup 是测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// p 是构造出的探针；周期为 0 时必须是 nil。
	if p := NewAIReadinessProbe(s, nil); p != nil {
		t.Fatalf("周期为 0 时应返回 nil")
	}
}
