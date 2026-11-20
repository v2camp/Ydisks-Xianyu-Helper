package qqbot

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ManagedUserConfig 是一个本地用户的 QQ 入站网关期望状态。
type ManagedUserConfig struct {
	// UserID 是网关所属本地用户。
	UserID int64
	// AppID 是机器人接入标识，非敏感。
	AppID string
	// AppSecret 是机器人接入密钥，只用于构造网关，禁止写入日志。
	AppSecret string
	// Fingerprint 是配置指纹；变化时管理器会重建该用户的网关。
	Fingerprint string
}

// ConfigSource 返回当前应运行的所有用户网关期望状态。
type ConfigSource interface {
	// ListManagedUsers 只返回已启用且凭据完整的用户；读取失败应返回错误。
	ListManagedUsers(ctx context.Context) ([]ManagedUserConfig, error)
}

// GatewayBuilder 根据用户配置构造一个尚未运行的网关。
type GatewayBuilder func(ctx context.Context, cfg ManagedUserConfig) (*Gateway, error)

// managedGateway 是一个已启动的用户网关及其取消与完成信号。
type managedGateway struct {
	// fingerprint 是该网关启动时的配置指纹。
	fingerprint string
	// cancel 停止该网关的运行上下文。
	cancel context.CancelFunc
	// done 在网关运行协程退出后关闭。
	done chan struct{}
}

// Manager 管理所有本地用户的 QQ 入站网关，按配置指纹启停与重建。
// 并发约束：mu 只保护 gateways 映射与关停状态；构造网关和网络 I/O 都在锁外执行。
type Manager struct {
	// source 提供期望状态。
	source ConfigSource
	// build 构造单个用户网关。
	build GatewayBuilder
	// logger 记录启停失败，不输出任何凭据。
	logger *slog.Logger
	// interval 是定期兜底对齐间隔；<=0 时只做启动对齐。
	interval time.Duration
	// mu 保护 gateways 与 stopped。
	mu sync.Mutex
	// gateways 是当前运行中的用户网关，键为用户 ID。
	gateways map[int64]*managedGateway
	// stopped 表示管理器已停止，禁止再启动新网关。
	stopped bool
	// cancel 取消管理器自身的对齐循环。
	cancel context.CancelFunc
	// wg 等待对齐循环退出。
	wg sync.WaitGroup
}

// NewManager 构造用户网关管理器；interval<=0 表示关闭定期对齐。
func NewManager(source ConfigSource, build GatewayBuilder, logger *slog.Logger, interval time.Duration) *Manager {
	return &Manager{source: source, build: build, logger: logger, interval: interval, gateways: make(map[int64]*managedGateway)}
}

// Start 启动管理器：先做一次对齐，再按间隔兜底重试。
func (m *Manager) Start(ctx context.Context) error {
	if m == nil || m.source == nil || m.build == nil {
		return errors.New("QQ 网关管理器依赖未初始化")
	}
	if ctx == nil {
		return errors.New("QQ 网关管理器缺少生命周期 Context")
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return errors.New("QQ 网关管理器已停止")
	}
	// runCtx、cancel 是管理器运行上下文与取消函数。
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()
	// err 表示启动时对齐失败；仅记录日志并等待兜底重试。
	if err := m.Reconcile(runCtx); err != nil && m.logger != nil {
		m.logger.Warn("QQ 用户网关首次对齐失败", "err", err)
	}
	if m.interval <= 0 {
		return nil
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// ticker 定期兜底对齐，防止设置写入监听遗漏。
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				// err 表示周期兜底对齐失败；仅记录日志并继续下一轮。
				if err := m.Reconcile(runCtx); err != nil && m.logger != nil {
					m.logger.Warn("QQ 用户网关对齐失败", "err", err)
				}
			}
		}
	}()
	return nil
}

// Reconcile 读取期望状态并启停差异网关；配置指纹变化时重建。
func (m *Manager) Reconcile(ctx context.Context) error {
	if m == nil || m.source == nil || m.build == nil {
		return errors.New("QQ 网关管理器依赖未初始化")
	}
	// configs、err 是期望的用户网关配置与读取错误。
	configs, err := m.source.ListManagedUsers(ctx)
	if err != nil {
		return err
	}
	// desired 保存按用户主键索引的期望配置。
	desired := make(map[int64]ManagedUserConfig, len(configs))
	// cfg 是当前待索引的用户网关配置。
	for _, cfg := range configs {
		desired[cfg.UserID] = cfg
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return nil
	}
	// 停止不再需要或指纹已变化的网关。
	for userID, current := range m.gateways {
		// cfg、keep 是当前用户的最新期望配置与是否保留。
		cfg, keep := desired[userID]
		if keep && cfg.Fingerprint == current.fingerprint {
			continue
		}
		delete(m.gateways, userID)
		current.cancel()
	}
	m.mu.Unlock()
	// 启动新增或重建的网关。
	for userID, cfg := range desired {
		m.mu.Lock()
		// running 表示当前用户是否已有运行中的网关。
		_, running := m.gateways[userID]
		// stopped 表示该用户网关是否因校验被停止。
		stopped := m.stopped
		m.mu.Unlock()
		if running || stopped {
			continue
		}
		// gatewayCtx、cancel 是单个网关的上下文与取消函数。
		gatewayCtx, cancel := context.WithCancel(ctx)
		// gateway、buildErr 是构建出的网关与构建错误。
		gateway, buildErr := m.build(gatewayCtx, cfg)
		if buildErr != nil {
			cancel()
			if m.logger != nil {
				m.logger.Warn("QQ 用户网关构造失败", "user_id", userID, "err", buildErr)
			}
			continue
		}
		if gateway == nil {
			cancel()
			continue
		}
		// managed 是登记到管理器表中的运行网关句柄。
		managed := &managedGateway{fingerprint: cfg.Fingerprint, cancel: cancel, done: make(chan struct{})}
		m.mu.Lock()
		if m.stopped {
			m.mu.Unlock()
			cancel()
			continue
		}
		m.gateways[userID] = managed
		m.mu.Unlock()
		go func(userID int64, gateway *Gateway, done chan struct{}, runCtx context.Context) {
			defer close(done)
			// runErr 是网关退出原因；正常取消不记录错误。
			if runErr := gateway.Run(runCtx); runErr != nil && runCtx.Err() == nil && m.logger != nil {
				m.logger.Warn("QQ 用户网关退出", "user_id", userID, "err", runErr)
			}
		}(userID, gateway, managed.done, gatewayCtx)
	}
	return nil
}

// Stop 停止所有用户网关并等待运行协程退出；可重复调用。
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	if m.cancel != nil {
		m.cancel()
	}
	// gateways 保存当前全部运行网关的快照。
	gateways := make([]*managedGateway, 0, len(m.gateways))
	// gateway 是当前待取消的运行网关。
	for _, gateway := range m.gateways {
		gateways = append(gateways, gateway)
	}
	m.gateways = make(map[int64]*managedGateway)
	m.mu.Unlock()
	// gateway 是当前待等待退出完成的运行网关。
	for _, gateway := range gateways {
		gateway.cancel()
	}
	// gateway 是当前待等待关闭完成的运行网关。
	for _, gateway := range gateways {
		<-gateway.done
	}
	m.wg.Wait()
}
