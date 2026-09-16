// 本文件实现账号实例看门狗：周期性巡检已登记的运行实例，发现实例协程已退出且
// 无需人工介入（未处于待扫码 / 待安全验证）时自动重启，避免账号因瞬时故障永久停摆。
//
// 退避说明：只要观察到实例处于「已退出」状态就推进退避窗口（无论本次重启是否报错），
// 下一轮观察到账号持续运行才清零。这样即便「重启成功但实例随即又退出」也会被挡住，
// 不会退化成每分钟一次的重启风暴去刺激平台风控。
package account

import (
	"context"
	"errors"
	"sync"
	"time"

	"xianyu-go/internal/engine"
)

// accountWatchdogInterval 是看门狗巡检周期；账号退出后最迟一个周期内被发现。
const accountWatchdogInterval = 60 * time.Second

// accountWatchdogRestartTimeout 是单次重启的超时预算，避免个别账号卡死拖住整轮巡检。
const accountWatchdogRestartTimeout = 45 * time.Second

// accountWatchdogBackoffSteps 是连续重启失败后的退避阶梯，超出阶梯后固定取最后一档。
var accountWatchdogBackoffSteps = []time.Duration{
	1 * time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute,
}

// accountWatchdog 保存看门狗按账号维度的退避状态与协程收束信号。
// 状态按 cookieID 保存而非挂在运行实例上，因此实例被替换后退避记录仍然有效。
type accountWatchdog struct {
	// mu 保护 failures 与 nextAttempt 的并发读写。
	mu sync.Mutex
	// failures 记录每个账号的连续重启失败次数，用于选择退避档位。
	failures map[string]int
	// nextAttempt 记录每个账号下次允许重启的最早时间。
	nextAttempt map[string]time.Time
	// done 在看门狗协程退出后关闭，供生命周期关闭阶段等待收束。
	done chan struct{}
	// once 保证 done 只被关闭一次，重复启动看门狗不会 panic。
	once sync.Once
}

// newAccountWatchdog 构造空的看门狗状态。
func newAccountWatchdog() *accountWatchdog {
	return &accountWatchdog{
		failures:    make(map[string]int),
		nextAttempt: make(map[string]time.Time),
		done:        make(chan struct{}),
	}
}

// nextDelay 返回第 failures 次连续失败之后应等待的时长，超出阶梯取最后一档。
func (wd *accountWatchdog) nextDelay(failures int) time.Duration {
	// level 是转换后的阶梯下标，最小为 0。
	level := failures - 1
	if level < 0 {
		level = 0
	}
	if level >= len(accountWatchdogBackoffSteps) {
		level = len(accountWatchdogBackoffSteps) - 1
	}
	return accountWatchdogBackoffSteps[level]
}

// attemptAllowed 判断 now 时刻是否允许重启账号；允许时递增失败计数并推进下次允许时间。
// 无论本次重启最终是否成功都先推进窗口，若下一轮观察到账号持续运行则由 markRunning 清零。
func (wd *accountWatchdog) attemptAllowed(cookieID string, now time.Time) bool {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	// until 是该账号此前登记的最早可重试时间。
	if until, ok := wd.nextAttempt[cookieID]; ok && now.Before(until) {
		return false
	}
	wd.failures[cookieID]++
	wd.nextAttempt[cookieID] = now.Add(wd.nextDelay(wd.failures[cookieID]))
	return true
}

// markRunning 在观察到账号处于运行状态时清零其退避状态，保证偶发故障仍能被快速恢复。
func (wd *accountWatchdog) markRunning(cookieID string) {
	wd.mu.Lock()
	delete(wd.failures, cookieID)
	delete(wd.nextAttempt, cookieID)
	wd.mu.Unlock()
}

// RunWatchdog 周期性巡检账号运行实例，直到 ctx 结束。
// 该方法只应由生命周期组件调用一次；重复调用时只有首个协程持有收束信号。
func (m *Manager) RunWatchdog(ctx context.Context) {
	if m == nil || m.watchdog == nil {
		return
	}
	defer m.watchdog.once.Do(func() { close(m.watchdog.done) })
	// 缺少生命周期 Context 或仓储时无法重启账号，直接收束避免空转。
	if ctx == nil || m.store == nil {
		return
	}
	// ticker 是巡检心跳，按固定周期触发一轮退出实例扫描。
	ticker := time.NewTicker(accountWatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepExitedAccounts(ctx)
		}
	}
}

// WaitWatchdog 等待看门狗协程收束，供生命周期关闭阶段调用。
func (m *Manager) WaitWatchdog(ctx context.Context) error {
	if m == nil || m.watchdog == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("等待账号看门狗收束需要关闭 Context")
	}
	select {
	case <-m.watchdog.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sweepExitedAccounts 扫描一轮账号实例，重启已退出且无需人工介入的实例。
func (m *Manager) sweepExitedAccounts(ctx context.Context) {
	// ids 是本轮需要重启的账号 ID 列表，在锁外逐个重启避免持锁等待协程。
	ids := m.exitedAccountIDs()
	// id 表示当前正在处理的账号标识。
	for _, id := range ids {
		if !m.watchdog.attemptAllowed(id, time.Now()) {
			continue
		}
		// restartCtx、cancel 为单次重启设置超时预算，避免个别账号卡死拖住整轮巡检。
		restartCtx, cancel := context.WithTimeout(ctx, accountWatchdogRestartTimeout)
		// restartErr 是本次重启失败原因；重启成功但实例随后再次退出时由下一轮观察退避。
		restartErr := m.Restart(restartCtx, id)
		cancel()
		if restartErr != nil {
			m.logger.Warn("账号看门狗重启失败", "account", id, "err", restartErr)
			continue
		}
		m.logger.Warn("账号看门狗已重启已退出的账号实例", "account", id)
	}
}

// exitedAccountIDs 收集本轮处于「已退出且可自动恢复」状态的账号 ID，
// 同时为仍在运行的账号清零退避记录。
func (m *Manager) exitedAccountIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// ids 是本轮收集到的待重启账号 ID。
	ids := make([]string, 0, len(m.accounts))
	// id、ma 表示当前遍历过程中的账号标识与运行实例。
	for id, ma := range m.accounts {
		// fencing 表示账号正处于删除或停止 fencing，不得被看门狗重启。
		if _, fencing := m.stopping[id]; fencing {
			continue
		}
		// 正在停止或全局关闭中的实例交给正常停止流程收束，看门狗不介入。
		if ma.stopping || m.stoppingAll {
			continue
		}
		// 实例协程仍在运行即视为健康，清零退避后跳过；已退出则继续判断是否可自动恢复。
		select {
		case <-ma.done:
		default:
			m.watchdog.markRunning(id)
			continue
		}
		// state 是账号运行时状态；处于待登录或待安全验证时重启无意义且会加重风控，留给人工处理。
		state := ma.acc.RuntimeStatus().State
		if state == engine.RuntimeAuthExpired || state == engine.RuntimeVerificationRequired {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
