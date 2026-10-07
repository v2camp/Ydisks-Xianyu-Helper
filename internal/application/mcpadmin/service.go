// Package mcpadmin 提供对外开放 MCP 服务的管理用例：
// 服务状态查询、启用开关与网络策略更新、持久化令牌生成/轮换/吊销，以及调用审计分页查询。
//
// 本包只依赖由消费者定义的最小仓储端口，不接触 HTTP、数据库模型、协议细节或凭证明文；
// 令牌明文只在生成/轮换调用的返回值中出现一次，调用方必须立即安全转交管理员。

package mcpadmin

import (
	"context"
	"errors"
	"time"
)

// ErrNotConfigured 表示 MCP 管理仓储未装配，管理用例不可用。
var ErrNotConfigured = errors.New("MCP 管理仓储未初始化")

// TokenStatus 是持久化令牌的非敏感配置态视图，不含任何哈希或明文。
type TokenStatus struct {
	// HasCurrent 表示当前是否存在生效中的持久化令牌。
	HasCurrent bool
	// CurrentCreatedAt 是当前令牌创建时间（Unix 秒），无令牌时为 0。
	CurrentCreatedAt int64
	// CurrentLastUsedAt 是当前令牌最后一次鉴权成功时间（Unix 秒），从未使用为 0。
	CurrentLastUsedAt int64
	// HasPrevious 表示是否存在宽限期内的旧令牌。
	HasPrevious bool
	// PreviousExpiresAt 是宽限期旧令牌的失效时刻（Unix 秒），无旧令牌时为 0。
	PreviousExpiresAt int64
}

// Status 是 MCP 服务管理状态的非敏感聚合视图。
type Status struct {
	// Enabled 表示 MCP 服务是否已启用。
	Enabled bool
	// AllowNonLoopback 表示是否显式放行非本机来源访问 /mcp。
	AllowNonLoopback bool
	// Token 是持久化令牌的非敏感配置态。
	Token TokenStatus
}

// SettingsUpdate 是启用开关与网络策略的一次性更新请求。
type SettingsUpdate struct {
	// Enabled 是更新后的启用状态。
	Enabled bool
	// AllowNonLoopback 是更新后的非本机访问策略。
	AllowNonLoopback bool
}

// AuditEntry 是一条调用审计的非敏感字段视图；参数摘要已由持久化层按白名单脱敏。
type AuditEntry struct {
	// ID 是审计行主键。
	ID int64
	// CreatedAt 是调用发生时间（Unix 秒）。
	CreatedAt int64
	// UserID 是执行调用的固定管理员本地用户标识。
	UserID int64
	// TokenSource 是本次调用的令牌来源（persisted/environment）。
	TokenSource string
	// Category 是调用类别：tool、resource 或 prompt。
	Category string
	// Name 是被调用的工具、资源或提示名称。
	Name string
	// CookieID 是调用目标账号标识；无账号归属时为空串。
	CookieID string
	// Arguments 是白名单键级脱敏后的参数 JSON 摘要。
	Arguments string
	// Success 表示调用是否成功完成。
	Success bool
	// ErrorClass 是失败类别的稳定标识；成功时为空串。
	ErrorClass string
	// DurationMS 是调用耗时（毫秒）。
	DurationMS int64
}

// AuditFilter 是审计分页查询条件；零值字段不参与过滤。
type AuditFilter struct {
	// Limit 是单页条数；非正时由仓储回落到默认值。
	Limit int
	// Offset 是跳过的记录数。
	Offset int
	// Category 非空时按调用类别过滤。
	Category string
	// Name 非空时按名称精确过滤。
	Name string
	// CookieID 非空时按账号归属过滤。
	CookieID string
	// SuccessState 取 0 时不过滤；1 只看成功；-1 只看失败。
	SuccessState int
}

// AuditPage 是审计分页查询结果。
type AuditPage struct {
	// Total 是满足过滤条件的总记录数。
	Total int
	// Records 是当前页非敏感审计记录。
	Records []AuditEntry
}

// Repository 定义 MCP 管理用例所需的最小持久化能力。
// 实现不得向调用方返回令牌哈希、明文或任何凭证明文。
type Repository interface {
	// Enabled 读取 MCP 服务启用状态。
	Enabled(context.Context) (bool, error)
	// AllowNonLoopback 读取非本机来源放行策略。
	AllowNonLoopback(context.Context) (bool, error)
	// SetEnabled 保存 MCP 服务启用状态。
	SetEnabled(context.Context, bool) error
	// SetAllowNonLoopback 保存非本机来源放行策略。
	SetAllowNonLoopback(context.Context, bool) error
	// TokenStatus 返回持久化令牌的非敏感配置态。
	TokenStatus(context.Context) (TokenStatus, error)
	// RotateToken 轮换持久化令牌并返回一次性明文。
	RotateToken(context.Context, time.Time) (string, error)
	// RevokeTokens 立即吊销全部持久化令牌。
	RevokeTokens(context.Context) error
	// ListAudit 按条件分页返回非敏感审计记录。
	ListAudit(context.Context, AuditFilter) (AuditPage, error)
}

// Invalidator 在开关或令牌变更后使 /mcp 安全守卫的配置快照立即失效。
// 实现可为空，表示不需要主动失效缓存（例如离线装配或测试）。
type Invalidator interface {
	// Invalidate 丢弃已缓存的配置快照，使下一次请求重新读取配置。
	Invalidate()
}

// Service 编排 MCP 管理用例。
type Service struct {
	// repository 是 MCP 管理持久化端口。
	repository Repository
	// invalidator 是配置变更后的缓存失效端口；为空时跳过失效。
	invalidator Invalidator
	// now 是统一时钟，供令牌轮换计算创建与宽限时间。
	now func() time.Time
}

// NewService 构造 MCP 管理应用服务，使用进程墙钟。
func NewService(repository Repository, invalidator Invalidator) *Service {
	return NewServiceWithClock(repository, invalidator, time.Now)
}

// NewServiceWithClock 构造 MCP 管理应用服务并注入可关闭的时钟，便于确定性测试。
func NewServiceWithClock(repository Repository, invalidator Invalidator, now func() time.Time) *Service {
	// clock 是缺省时回落到墙钟的统一时钟。
	clock := now
	if clock == nil {
		clock = time.Now
	}
	return &Service{repository: repository, invalidator: invalidator, now: clock}
}

// Status 返回 MCP 服务当前的非敏感管理状态。
func (s *Service) Status(ctx context.Context) (Status, error) {
	if s == nil || s.repository == nil {
		return Status{}, ErrNotConfigured
	}
	// enabled、err 是启用状态及其读取错误。
	enabled, err := s.repository.Enabled(ctx)
	if err != nil {
		return Status{}, err
	}
	// allowNonLoopback、err 是网络策略及其读取错误。
	allowNonLoopback, err := s.repository.AllowNonLoopback(ctx)
	if err != nil {
		return Status{}, err
	}
	// token、err 是持久化令牌配置态及其读取错误。
	token, err := s.repository.TokenStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{Enabled: enabled, AllowNonLoopback: allowNonLoopback, Token: token}, nil
}

// UpdateSettings 保存启用开关与网络策略，并立即使安全守卫缓存失效。
func (s *Service) UpdateSettings(ctx context.Context, update SettingsUpdate) error {
	if s == nil || s.repository == nil {
		return ErrNotConfigured
	}
	// 先保存启用状态；失败立即返回，避免只写入一半配置。
	if err := s.repository.SetEnabled(ctx, update.Enabled); err != nil {
		return err
	}
	// 再保存网络策略；失败立即返回并由调用方按普通错误处理。
	if err := s.repository.SetAllowNonLoopback(ctx, update.AllowNonLoopback); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// RotateToken 生成并轮换持久化令牌，返回只在本次调用出现一次的令牌明文。
func (s *Service) RotateToken(ctx context.Context) (string, error) {
	if s == nil || s.repository == nil {
		return "", ErrNotConfigured
	}
	// plaintext、err 是新令牌明文及其生成/落库错误；明文不得写入日志或审计。
	plaintext, err := s.repository.RotateToken(ctx, s.now())
	if err != nil {
		return "", err
	}
	s.invalidate()
	return plaintext, nil
}

// RevokeToken 立即吊销全部持久化令牌，并立即使安全守卫缓存失效。
func (s *Service) RevokeToken(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return ErrNotConfigured
	}
	// err 是吊销持久化令牌的错误。
	if err := s.repository.RevokeTokens(ctx); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// ListAudit 分页查询调用审计。
func (s *Service) ListAudit(ctx context.Context, filter AuditFilter) (AuditPage, error) {
	if s == nil || s.repository == nil {
		return AuditPage{}, ErrNotConfigured
	}
	return s.repository.ListAudit(ctx, filter)
}

// invalidate 在配置变更后失效安全守卫缓存；未注入失效端口时忽略。
func (s *Service) invalidate() {
	if s.invalidator != nil {
		s.invalidator.Invalidate()
	}
}

const (
	// defaultPageSize 是审计分页未指定页大小时的默认条数。
	defaultPageSize = 20
	// maxPageSize 是审计分页单页允许的最大条数。
	maxPageSize = 200
)

// PageBounds 归一化 HTTP 分页参数为页码、页大小与仓储偏移量，供管理接口适配层复用。
func PageBounds(page, pageSize int) (normalizedPage, normalizedSize, offset int) {
	// normalizedPage 是从 1 开始的有效页码。
	normalizedPage = page
	if normalizedPage < 1 {
		normalizedPage = 1
	}
	// normalizedSize 是收敛到上下限内的页大小。
	normalizedSize = pageSize
	if normalizedSize <= 0 {
		normalizedSize = defaultPageSize
	}
	if normalizedSize > maxPageSize {
		normalizedSize = maxPageSize
	}
	return normalizedPage, normalizedSize, (normalizedPage - 1) * normalizedSize
}

// TotalPages 按总数与页大小计算总页数；页大小非正时返回 0。
func TotalPages(total, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}
