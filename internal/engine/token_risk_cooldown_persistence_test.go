package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestPersistedTokenRiskCooldownSurvivesAccountRestart 验证风控失败冷却落库后，
// 新账号实例（模拟进程重启或扫码重登）仍尊重冷却，且冷却期间不发起 token 平台请求。
func TestPersistedTokenRiskCooldownSurvivesAccountRestart(t *testing.T) {
	// first、store、cleanup 是首次发生风控失败的账号实例、共享存储与清理函数。
	first, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是冷却读写使用的取消边界。
	ctx := context.Background()
	// 第一次实例记录风控失败，冷却应同步落库。
	first.credentials.markTokenCaptchaFailure()
	// markedAt、getErr 是数据库中的风控失败时刻与读取错误，必须刚刚写入。
	markedAt, getErr := store.Renewal.GetCooldown(ctx, "cid", cooldownKindTokenRisk)
	if getErr != nil || markedAt.IsZero() {
		t.Fatalf("风控冷却未持久化 markedAt=%v err=%v", markedAt, getErr)
	}
	// second 用同一份存储新建账号实例，模拟重启后内存风控状态为空。
	second := New(Config{
		CookieID:  "cid",
		CookieStr: "unb=123; _m_h5_tk=tk_1;",
		Store:     store,
		MTop:      &countingMtop{fakeRunMtop: fakeRunMtop{token: "restart-token"}},
	})
	// remaining 是新实例从持久化冷却读到的剩余时间，必须为正。
	remaining := second.credentials.persistedTokenRiskCooldownRemaining(ctx)
	if remaining <= 0 {
		t.Fatalf("新实例未读到持久化风控冷却 remaining=%s", remaining)
	}
	// refreshErr 是新实例在冷却期内刷新 token 的错误，必须命中冷却哨兵错误。
	_, _, refreshErr := second.refreshToken(ctx)
	if !errors.Is(refreshErr, errTokenCaptchaCooldown) {
		t.Fatalf("冷却期刷新错误=%v want errTokenCaptchaCooldown", refreshErr)
	}
	// counting 是新实例注入的可计数 MTOP 客户端。
	counting := second.mtop.(*countingMtop)
	// tokenCalls 是冷却期内实际发起的平台 token 请求次数，必须为零。
	tokenCalls := atomic.LoadInt32(&counting.calls)
	if tokenCalls != 0 {
		t.Fatalf("冷却期发起了 %d 次 token 平台请求", tokenCalls)
	}
}

// TestPersistedTokenRiskCooldownClearedOnExpiry 验证超过冷却窗口后读取返回零并清理记录。
func TestPersistedTokenRiskCooldownClearedOnExpiry(t *testing.T) {
	// acc、store、cleanup 是被测账号、共享存储与清理函数。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是冷却读写使用的取消边界。
	ctx := context.Background()
	// expiredAt 是早已超过基础冷却窗口的失败时刻。
	expiredAt := time.Now().Add(-TokenCaptchaFailureCooldown - time.Minute)
	if // markErr 是写入过期冷却的错误，必须成功。
	markErr := store.Renewal.MarkCooldown(ctx, "cid", cooldownKindTokenRisk, expiredAt); markErr != nil {
		t.Fatalf("写入过期冷却失败: %v", markErr)
	}
	if // remaining 是过期冷却的剩余时间，必须为零。
	remaining := acc.credentials.persistedTokenRiskCooldownRemaining(ctx); remaining != 0 {
		t.Fatalf("过期冷却剩余=%s want 0", remaining)
	}
	// cleanedAt、getErr 用于确认过期记录已被顺手清理。
	cleanedAt, getErr := store.Renewal.GetCooldown(ctx, "cid", cooldownKindTokenRisk)
	if getErr != nil || !cleanedAt.IsZero() {
		t.Fatalf("过期冷却未清理 markedAt=%v err=%v", cleanedAt, getErr)
	}
}

// TestClearPersistedTokenRiskCooldownOnSuccessPath 验证成功路径会删除持久化风控冷却。
func TestClearPersistedTokenRiskCooldownOnSuccessPath(t *testing.T) {
	// acc、store、cleanup 是被测账号、共享存储与清理函数。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是冷却读写使用的取消边界。
	ctx := context.Background()
	acc.credentials.markTokenCaptchaFailure()
	if // markedAt 是前置冷却记录；缺失说明测试夹具失效。
	markedAt, _ := store.Renewal.GetCooldown(ctx, "cid", cooldownKindTokenRisk); markedAt.IsZero() {
		t.Fatal("前置冷却记录缺失")
	}
	// 只有真实拿到 token 的成功路径才解除冷却，这里直接验证清除函数语义。
	acc.credentials.clearPersistedTokenRiskCooldown(ctx)
	if // afterClear 是清除后的冷却记录；非零说明未真正解除。
	afterClear, _ := store.Renewal.GetCooldown(ctx, "cid", cooldownKindTokenRisk); !afterClear.IsZero() {
		t.Fatalf("成功路径未清除风控冷却 markedAt=%v", afterClear)
	}
}