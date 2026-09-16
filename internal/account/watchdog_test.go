package account

import (
	"testing"
	"time"

	"xianyu-go/internal/engine"
)

// TestAccountWatchdogBackoffProgressiveDelay 验证连续重启失败后退避逐档拉长，
// 且观察到账号恢复运行后退避被清零回到首档。
func TestAccountWatchdogBackoffProgressiveDelay(t *testing.T) {
	// wd 是独立的看门狗退避状态，不依赖 Manager 与数据库。
	wd := newAccountWatchdog()
	// now 是本用例的时间基准，所有采样都相对它推进。
	now := time.Now()
	if !wd.attemptAllowed("acct-1", now) {
		t.Fatal("首次发现账号退出应当立即允许重启")
	}
	if wd.attemptAllowed("acct-1", now.Add(30*time.Second)) {
		t.Fatal("首次重启后 1 分钟窗口内不应再次重启")
	}
	if !wd.attemptAllowed("acct-1", now.Add(time.Minute)) {
		t.Fatal("满 1 分钟后应当允许第二次重启")
	}
	if wd.attemptAllowed("acct-1", now.Add(90*time.Second)) {
		t.Fatal("第二次失败后应退避 2 分钟，90 秒时不该放行")
	}
	if !wd.attemptAllowed("acct-1", now.Add(3*time.Minute)) {
		t.Fatal("第二次重启满 2 分钟后应当允许第三次重启")
	}
	// 模拟账号恢复运行：退避清零后应重新从 1 分钟首档开始。
	wd.markRunning("acct-1")
	if !wd.attemptAllowed("acct-1", now.Add(3*time.Minute)) {
		t.Fatal("退避清零后应当立即允许重启")
	}
	if wd.attemptAllowed("acct-1", now.Add(3*time.Minute+30*time.Second)) {
		t.Fatal("退避清零后应回到 1 分钟首档，30 秒时不该放行")
	}
}

// TestAccountWatchdogNextDelayClampsToLastStep 验证退避档位在越界时收敛到首尾档。
func TestAccountWatchdogNextDelayClampsToLastStep(t *testing.T) {
	// wd 是独立的看门狗退避状态。
	wd := newAccountWatchdog()
	// cases 是 failures 与期望退避时长的对照表。
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{failures: 0, want: time.Minute},
		{failures: 1, want: time.Minute},
		{failures: 2, want: 2 * time.Minute},
		{failures: 3, want: 5 * time.Minute},
		{failures: 99, want: 15 * time.Minute},
	}
	// item 表示当前遍历过程中的一组入参与期望值。
	for _, item := range cases {
		// got 是实际计算出的退避时长。
		got := wd.nextDelay(item.failures)
		if got != item.want {
			t.Fatalf("failures=%d 退避期望 %v，实际 %v", item.failures, item.want, got)
		}
	}
}

// TestExitedAccountIDsOnlyCollectsRecoverable 验证只有「已退出且未被停止流程接管」的
// 账号会被收集，正在停止、处于删除 fencing 或全局关闭中的实例都不会被看门狗重启。
func TestExitedAccountIDsOnlyCollectsRecoverable(t *testing.T) {
	// manager 是不带仓储的管理器，仅用于验证实例筛选逻辑。
	manager := NewManager(nil, noopHandler{}, nil)
	// exitedAcc 是模拟已退出协程的账号实例。
	exitedAcc := engine.New(engine.Config{CookieID: "exited-account", CookieStr: "unb=1"})
	// exitedDone 是已关闭的运行结束信号，代表账号协程已退出。
	exitedDone := make(chan struct{})
	close(exitedDone)
	manager.accounts["exited-account"] = &managedAccount{cookieID: "exited-account", acc: exitedAcc, cancel: func() {}, done: exitedDone}

	// runningAcc 是模拟仍在运行的账号实例，不应被收集。
	runningAcc := engine.New(engine.Config{CookieID: "running-account", CookieStr: "unb=1"})
	manager.accounts["running-account"] = &managedAccount{cookieID: "running-account", acc: runningAcc, cancel: func() {}, done: make(chan struct{})}

	// stoppingAcc 是模拟正在停止的账号实例，即使已退出也不应被看门狗重启。
	stoppingAcc := engine.New(engine.Config{CookieID: "stopping-account", CookieStr: "unb=1"})
	// stoppingDone 是已关闭的运行结束信号。
	stoppingDone := make(chan struct{})
	close(stoppingDone)
	manager.accounts["stopping-account"] = &managedAccount{cookieID: "stopping-account", acc: stoppingAcc, cancel: func() {}, done: stoppingDone, stopping: true}

	// fencedAcc 是模拟处于删除 fencing 的账号实例。
	fencedAcc := engine.New(engine.Config{CookieID: "fenced-account", CookieStr: "unb=1"})
	// fencedDone 是已关闭的运行结束信号。
	fencedDone := make(chan struct{})
	close(fencedDone)
	manager.accounts["fenced-account"] = &managedAccount{cookieID: "fenced-account", acc: fencedAcc, cancel: func() {}, done: fencedDone}
	manager.stopping["fenced-account"] = struct{}{}

	// ids 是本轮收集到的待重启账号 ID。
	ids := manager.exitedAccountIDs()
	if len(ids) != 1 || ids[0] != "exited-account" {
		t.Fatalf("期望只收集 exited-account，实际 %v", ids)
	}
	// 全局关闭期间看门狗必须完全让位给停止流程。
	manager.stoppingAll = true
	// got 是全局关闭期间收集到的账号 ID，期望为空。
	got := manager.exitedAccountIDs()
	if len(got) != 0 {
		t.Fatalf("全局关闭期间不应收集任何账号，实际 %v", got)
	}
}
