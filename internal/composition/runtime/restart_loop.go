package runtime

import (
	"context"
	"log/slog"
	"time"
)

const (
	// restartInitialDelay 是后台组件退出后的首次重连等待时间。
	restartInitialDelay = 5 * time.Second
	// restartMaxDelay 是重连等待时间的上限，避免长期失败时退避无界增长。
	restartMaxDelay = 5 * time.Minute
	// restartHealthyRun 是一次运行被视为「已稳定」的最短时长，超过则重置退避。
	restartHealthyRun = 60 * time.Second
)

// restartableRunner 是需要被守护的后台运行体；Run 返回即表示本次运行结束。
// 满足该接口的组件必须自身可重复启动，不得遗留上一次运行的状态。
type restartableRunner interface {
	// Run 阻塞运行，直到本次运行失败或 ctx 被取消。
	Run(ctx context.Context) error
}

// restartSleep 是退避等待函数，抽象为包级变量便于测试注入即时返回。
// 返回 false 表示 ctx 已取消，调用方必须立即停止重连。
var restartSleep = func(ctx context.Context, delay time.Duration) bool {
	// timer 是本次等待使用的定时器，提前退出时释放其内部资源。
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// restartNow 返回当前时刻，抽象为包级变量便于测试推进「已稳定运行」的判定。
var restartNow = time.Now

// serveWithRestart 持续守护后台组件：每次退出后按指数退避重启，直到 ctx 取消。
// 组件单次运行超过 restartHealthyRun 即视为已稳定，下一次退避重新从初始值开始。
// name 只用于日志标识；runner 是可重复启动的运行体；logger 为 nil 时静默重试。
// ctx 必须是进程生命周期上下文，禁止传入裸 Background。
func serveWithRestart(ctx context.Context, name string, runner restartableRunner, logger *slog.Logger) {
	if ctx == nil || runner == nil {
		return
	}
	// delay 是当前退避等待时间，首次退出使用初始值。
	delay := restartInitialDelay
	for {
		if ctx.Err() != nil {
			return
		}
		// startedAt 是本次运行的开始时刻，用于判断组件是否已稳定运行。
		startedAt := restartNow()
		// runErr 是本次运行的退出原因；ctx 取消属正常收束，不再重连。
		runErr := runner.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		if restartNow().Sub(startedAt) >= restartHealthyRun {
			delay = restartInitialDelay
		}
		logRestartAttempt(logger, name, runErr, delay)
		if !restartSleep(ctx, delay) {
			return
		}
		delay = nextRestartDelay(delay)
	}
}

// logRestartAttempt 记录后台组件的一次异常退出及其后的重连间隔。
// logger 为 nil 时不做任何输出；runErr 允许为 nil，表示组件自行结束而非报错。
func logRestartAttempt(logger *slog.Logger, name string, runErr error, delay time.Duration) {
	if logger == nil {
		return
	}
	logger.Warn(name+"异常退出，将按退避重连", "err", runErr, "retry_in", delay.String())
}

// nextRestartDelay 计算下一次退避等待时间：翻倍增长并收敛到上限。
func nextRestartDelay(current time.Duration) time.Duration {
	// doubled 是当前退避时间翻倍后的结果；溢出或超限时统一取上限。
	doubled := current * 2
	if doubled <= 0 || doubled > restartMaxDelay {
		return restartMaxDelay
	}
	return doubled
}
