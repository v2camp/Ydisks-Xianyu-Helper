package runtime

// mcp_transport.go 把数据库仓储投影为 internal/mcp 自定义的最小消费者端口，
// 并装配 /mcp 的 Streamable HTTP 挂载条目。组合层只做透传与模型转换，不写 MCP 业务规则。

import (
	"context"
	"fmt"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/mcp"
	"xianyu-go/internal/server"
)

// MCPRoutePattern 是 MCP 端点在主路由树上的精确挂载路径。
const MCPRoutePattern = "/mcp"

// mcpConfigAdapter 把 MCP 令牌仓储投影为传输层配置端口；不暴露任何哈希或明文。
type mcpConfigAdapter struct {
	// store 是 MCP 专用仓储；适配器只调用其窄方法。
	store *db.MCPAuthStore
}

// Enabled 返回 MCP 服务启用状态。
func (a mcpConfigAdapter) Enabled(ctx context.Context) (bool, error) {
	return a.store.Enabled(ctx)
}

// AllowNonLoopback 返回是否放行非本机来源。
func (a mcpConfigAdapter) AllowNonLoopback(ctx context.Context) (bool, error) {
	return a.store.AllowNonLoopback(ctx)
}

// HasPersistedToken 报告当前是否存在当前或宽限期内的持久化令牌。
func (a mcpConfigAdapter) HasPersistedToken(ctx context.Context) (bool, error) {
	// status 是不含哈希与明文的令牌配置态。
	status, err := a.store.TokenStatus(ctx)
	if err != nil {
		return false, err
	}
	return status.HasCurrent || status.HasPrevious, nil
}

// VerifyPersistedToken 使用进程墙钟校验持久化令牌；宽限到期清理也按同一时刻执行。
func (a mcpConfigAdapter) VerifyPersistedToken(ctx context.Context, token string) (bool, error) {
	return a.store.VerifyPersistedToken(ctx, token, time.Now())
}

// mcpIdentityAdapter 解析 MCP 固定管理员身份；只读取非敏感用户标识。
type mcpIdentityAdapter struct {
	// users 是用户仓储；只调用 GetAdmin 取管理员主键。
	users *db.Users
}

// AdminUserID 返回管理员用户主键；管理员不存在时返回错误，拒绝匿名协议服务。
func (a mcpIdentityAdapter) AdminUserID(ctx context.Context) (int64, error) {
	// admin、err 是管理员用户行及其查询错误；不触碰密码字段。
	admin, err := a.users.GetAdmin(ctx)
	if err != nil {
		return 0, fmt.Errorf("解析 MCP 管理员身份失败: %w", err)
	}
	if admin == nil || admin.ID <= 0 {
		return 0, fmt.Errorf("管理员账号不存在或未激活")
	}
	return admin.ID, nil
}

// mcpAuditAdapter 把 MCP 审计条目投影为数据库审计记录；参数脱敏由仓储强制完成。
type mcpAuditAdapter struct {
	// store 是 MCP 专用仓储。
	store *db.MCPAuthStore
}

// AddAudit 写入一条调用审计；写入时刻由适配器取墙钟，传输层无需关心时间来源。
func (a mcpAuditAdapter) AddAudit(ctx context.Context, entry mcp.AuditEntry) error {
	// record 是与传输模型解耦的数据库审计行。
	record := db.MCPAuditRecord{
		CreatedAt:   time.Now().Unix(),
		UserID:      entry.UserID,
		TokenSource: string(entry.Source),
		Category:    entry.Category,
		Name:        entry.Name,
		CookieID:    entry.CookieID,
		Arguments:   entry.ArgumentsJSON,
		Success:     entry.Success,
		ErrorClass:  string(entry.ErrorClass),
		DurationMS:  entry.DurationMS,
	}
	return a.store.AddMCPAudit(ctx, record)
}

// BuildMCPEndpoint 装配 MCP 协议端点并返回对应的主路由挂载条目。
// environmentToken 为 cmd 从 XIANYU_MCP_TOKEN 读取的引导令牌，空白表示未配置。
// 此时只装配安全与协议骨架；业务工具、资源与提示在应用服务就绪后由注册函数补充。
func BuildMCPEndpoint(store *db.Store, environmentToken string) (*mcp.Endpoint, server.ExtraRoute, error) {
	if store == nil || store.MCP == nil || store.Users == nil {
		return nil, server.ExtraRoute{}, fmt.Errorf("MCP 端点缺少数据库仓储")
	}
	// endpoint、err 是协议端点及其构造错误。
	endpoint, err := mcp.NewEndpoint(mcp.EndpointConfig{
		Config:           mcpConfigAdapter{store: store.MCP},
		Identity:         &mcpIdentityAdapter{users: store.Users},
		Audit:            mcpAuditAdapter{store: store.MCP},
		EnvironmentToken: environmentToken,
	})
	if err != nil {
		return nil, server.ExtraRoute{}, err
	}
	// route 是挂进主 chi 路由树的条目；处理器已内含 Bearer 与 loopback 安全门。
	route := server.ExtraRoute{Pattern: MCPRoutePattern, Handler: endpoint.Handler()}
	return endpoint, route, nil
}
