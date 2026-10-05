// Package mcp 是与 internal/server 平级的第二传输层：对外提供 MCP Streamable HTTP 服务。
//
// 本包只允许导入标准库、github.com/mark3labs/mcp-go 与 internal/application/* 的应用模型；
// 鉴权、启用状态、固定管理员身份与调用审计都通过包内自定义的最小端口获取，
// 由 internal/composition/runtime 投影具体实现，禁止导入 db、server、平台与自动化实现包。
package mcp

import "context"

// TokenSource 标识鉴权成功的令牌来源，写入审计时用于区分持久化令牌与环境引导令牌。
type TokenSource string

const (
	// SourcePersisted 表示调用方使用数据库持久化的 MCP 令牌。
	SourcePersisted TokenSource = "persisted"
	// SourceEnvironment 表示调用方使用 XIANYU_MCP_TOKEN 环境引导令牌。
	SourceEnvironment TokenSource = "environment"
)

// ConfigPort 定义 MCP 端点启用门与令牌校验所需的最小配置能力。
// 实现必须保证校验不返回令牌哈希、明文或任何凭证元数据。
type ConfigPort interface {
	// Enabled 返回 MCP 服务当前是否启用；读取失败按错误上抛，由调用方按关闭处理或记录诊断。
	Enabled(ctx context.Context) (bool, error)
	// AllowNonLoopback 返回是否显式放行非本机来源；默认必须为 false。
	AllowNonLoopback(ctx context.Context) (bool, error)
	// HasPersistedToken 返回当前是否存在未吊销的持久化令牌（含宽限期令牌）。
	HasPersistedToken(ctx context.Context) (bool, error)
	// VerifyPersistedToken 以常量时间方式校验明文持久化令牌，命中返回 true。
	VerifyPersistedToken(ctx context.Context, token string) (bool, error)
}

// IdentityPort 解析 MCP 调用使用的固定管理员本地用户身份；只返回非敏感用户标识。
type IdentityPort interface {
	// AdminUserID 返回管理员用户的稳定数据库标识，供应用层归属校验与审计使用。
	AdminUserID(ctx context.Context) (int64, error)
}

// AuditEntry 是一次 MCP 调用写入审计的非敏感字段；参数 JSON 在持久化层强制脱敏。
type AuditEntry struct {
	// Category 是调用类别：tool、resource 或 prompt。
	Category string
	// Name 是被调用的工具、资源或提示名称。
	Name string
	// CookieID 是调用目标账号标识；无账号归属时为空串。
	CookieID string
	// ArgumentsJSON 是原始参数 JSON，落库前由持久化层按白名单脱敏，调用方不得预先加密。
	ArgumentsJSON string
	// Success 表示调用是否成功完成。
	Success bool
	// ErrorClass 是失败类别的稳定标识；成功时为空串。
	ErrorClass string
	// DurationMS 是调用耗时（毫秒）。
	DurationMS int64
}

// AuditPort 定义调用审计写入能力；实现不得因审计失败而重放业务动作。
type AuditPort interface {
	// AddAudit 在调用方 Context 内写入一条非敏感审计。
	AddAudit(ctx context.Context, entry AuditEntry) error
}

// CallIdentity 是鉴权成功后注入请求上下文的调用者身份视图。
type CallIdentity struct {
	// UserID 是固定管理员本地用户标识，所有工具用例都以该身份执行。
	UserID int64
	// Source 是本次请求使用的令牌来源。
	Source TokenSource
}

// identityContextKey 是调用身份在 context 中的键类型，避免与其他包冲突。
type identityContextKey struct{}

// withIdentity 返回携带调用身份指针的派生 Context；指针形态与读取断言保持一致。
func withIdentity(ctx context.Context, identity *CallIdentity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, identity)
}

// IdentityFromContext 从请求 Context 提取调用身份；未经鉴权的请求返回 nil。
func IdentityFromContext(ctx context.Context) *CallIdentity {
	// identity 是鉴权中间件注入的调用身份，缺失时返回 nil。
	identity, _ := ctx.Value(identityContextKey{}).(*CallIdentity)
	return identity
}
