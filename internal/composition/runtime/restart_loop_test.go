package runtime

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// restartRunnerFunc 把函数适配为可重复启动的后台运行体，便于测试编排每次运行的退出方式。
type restartRunnerFunc func(ctx context.Context) error

// Run 执行测试编排的运行逻辑。
func (f restartRunnerFunc) Run(ctx context.Context) error {
	return f(ctx)
}

// stubRestartSleep 替换真实退避等待，记录每次等待时长并保证测试即时完成。
// delays 收集按顺序发生的退避时长；返回值恒为 true 表示 ctx 始终有效。
func stubRestartSleep(t *testing.T, delays *[]time.Duration) {
	t.Helper()
	// previous 是替换前的退避等待实现，用例结束后还原。
	previous := restartSleep
	restartSleep = func(_ context.Context, delay time.Duration) bool {
		*delays = append(*delays, delay)
		return true
	}
	t.Cleanup(func() { restartSleep = previous })
}

// stubRestartClock 替换时间源，让每次运行的耗时可控。
// step 是每次读取时间时前进的增量；用例结束后还原真实时间源。
func stubRestartClock(t *testing.T, step time.Duration) {
	t.Helper()
	// previous 是替换前的时间源，用例结束后还原。
	previous := restartNow
	// current 是虚拟时钟的当前时刻。
	current := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	restartNow = func() time.Time {
		current = current.Add(step)
		return current
	}
	t.Cleanup(func() { restartNow = previous })
}

// TestServeWithRestartRetriesWithBackoff 验证组件异常退出后会按指数退避重启，并在进程关闭时停止。
func TestServeWithRestartRetriesWithBackoff(t *testing.T) {
	// delays 收集本次重连实际使用的退避时长。
	var delays []time.Duration
	stubRestartSleep(t, &delays)
	// 每次运行只推进 1 秒，低于稳定阈值，因此退避必须逐次翻倍。
	stubRestartClock(t, time.Second)
	// ctx、cancel 为被测守护循环提供可控的进程生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// calls 记录运行体被启动的次数。
	calls := 0
	// runner 在前两次返回错误模拟连接失败，第三次返回前主动取消上下文模拟进程关闭。
	runner := restartRunnerFunc(func(runCtx context.Context) error {
		calls++
		if calls >= 3 {
			cancel()
			return runCtx.Err()
		}
		return errors.New("获取 QQ 网关地址失败")
	})
	serveWithRestart(ctx, "测试网关", runner, slog.New(slog.DiscardHandler))
	if calls != 3 {
		t.Fatalf("守护循环启动次数 = %d，期望 3", calls)
	}
	if len(delays) != 2 {
		t.Fatalf("退避次数 = %d，期望 2", len(delays))
	}
	if delays[0] != restartInitialDelay || delays[1] != 2*restartInitialDelay {
		t.Fatalf("退避序列 = %v，期望 [%v %v]", delays, restartInitialDelay, 2*restartInitialDelay)
	}
}

// TestServeWithRestartResetsBackoffAfterHealthyRun 验证稳定运行过的组件重新从初始退避开始。
func TestServeWithRestartResetsBackoffAfterHealthyRun(t *testing.T) {
	// delays 收集本次重连实际使用的退避时长。
	var delays []time.Duration
	stubRestartSleep(t, &delays)
	// 每次运行都推进稳定阈值以上，模拟组件已长期正常运行后才断开。
	stubRestartClock(t, 2*restartHealthyRun)
	// ctx、cancel 为被测守护循环提供可控的进程生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// calls 记录运行体被启动的次数。
	calls := 0
	// runner 每次运行都返回连接错误，第三次返回前取消上下文结束循环。
	runner := restartRunnerFunc(func(context.Context) error {
		calls++
		if calls >= 3 {
			cancel()
		}
		return errors.New("连接被对端关闭")
	})
	serveWithRestart(ctx, "测试网关", runner, nil)
	if len(delays) != 2 {
		t.Fatalf("退避次数 = %d，期望 2", len(delays))
	}
	// index、delay 分别是退避序号与该次实际等待时长。
	for index, delay := range delays {
		if delay != restartInitialDelay {
			t.Fatalf("第 %d 次退避 = %v，稳定运行后应回到初始值", index+1, delay)
		}
	}
}

// TestServeWithRestartStopsWhenContextAlreadyCancelled 验证上下文已取消时不再启动组件。
func TestServeWithRestartStopsWhenContextAlreadyCancelled(t *testing.T) {
	// ctx、cancel 模拟进程已进入关闭阶段。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// calls 记录运行体被启动的次数，必须保持为零。
	calls := 0
	serveWithRestart(ctx, "测试网关", restartRunnerFunc(func(context.Context) error {
		calls++
		return nil
	}), nil)
	if calls != 0 {
		t.Fatalf("上下文已取消时仍启动了 %d 次", calls)
	}
}

// TestServeWithRestartIgnoresMissingDependencies 验证缺少上下文或运行体时静默返回。
func TestServeWithRestartIgnoresMissingDependencies(t *testing.T) {
	// calls 记录运行体被启动的次数，必须保持为零。
	calls := 0
	// runner 是被传入但不应被启动的运行体。
	runner := restartRunnerFunc(func(context.Context) error {
		calls++
		return nil
	})
	// missingCtx 是显式声明的空上下文，用于覆盖守护循环的入参校验分支。
	var missingCtx context.Context
	serveWithRestart(missingCtx, "测试网关", runner, nil)
	serveWithRestart(context.Background(), "测试网关", nil, nil)
	if calls != 0 {
		t.Fatalf("依赖缺失时仍启动了 %d 次", calls)
	}
}

// TestNextRestartDelayCapsAtMaximum 验证退避时长翻倍增长并收敛到上限。
func TestNextRestartDelayCapsAtMaximum(t *testing.T) {
	// halfDelay 是接近上限的退避时长，翻倍后必须被截断到上限。
	halfDelay := nextRestartDelay(restartMaxDelay / 2)
	if halfDelay != restartMaxDelay {
		t.Fatalf("未收敛到上限: %v", halfDelay)
	}
	// cappedDelay 是已在上限的退避时长，继续翻倍必须保持上限。
	cappedDelay := nextRestartDelay(restartMaxDelay)
	if cappedDelay != restartMaxDelay {
		t.Fatalf("超过上限应保持上限: %v", cappedDelay)
	}
	// zeroDelay 是非法零值，必须回落到上限而不是退化成无间隔重连。
	zeroDelay := nextRestartDelay(0)
	if zeroDelay != restartMaxDelay {
		t.Fatalf("非法时长应回落到上限: %v", zeroDelay)
	}
	// doubledDelay 是正常退避时长的翻倍结果。
	doubledDelay := nextRestartDelay(time.Second)
	if doubledDelay != 2*time.Second {
		t.Fatalf("正常翻倍失败: %v", doubledDelay)
	}
}
