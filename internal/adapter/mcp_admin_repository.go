package adapter

import (
	"context"
	"time"

	mcpadminapp "xianyu-go/internal/application/mcpadmin"
	"xianyu-go/internal/db"
)

// mcpAdminRepository 把 MCP 持久化仓储投影为管理用例的最小仓储端口。
// 适配器只做模型转换与狭方法透传，不读取令牌哈希，也不缓存任何配置状态。
type mcpAdminRepository struct {
	// store 是 MCP 专用持久化仓储；nil 时所有方法返回未初始化错误。
	store *db.MCPAuthStore
}

// NewMCPAdminRepository 从数据库 Store 构造 MCP 管理仓储端口；Store 为空时返回 nil。
func NewMCPAdminRepository(store *db.Store) mcpadminapp.Repository {
	if store == nil {
		return nil
	}
	return mcpAdminRepository{store: store.MCP}
}

// Enabled 读取 MCP 服务启用状态。
func (r mcpAdminRepository) Enabled(ctx context.Context) (bool, error) {
	return r.store.Enabled(ctx)
}

// AllowNonLoopback 读取非本机来源放行策略。
func (r mcpAdminRepository) AllowNonLoopback(ctx context.Context) (bool, error) {
	return r.store.AllowNonLoopback(ctx)
}

// SetEnabled 保存 MCP 服务启用状态。
func (r mcpAdminRepository) SetEnabled(ctx context.Context, enabled bool) error {
	return r.store.SetEnabled(ctx, enabled)
}

// SetAllowNonLoopback 保存非本机来源放行策略。
func (r mcpAdminRepository) SetAllowNonLoopback(ctx context.Context, allowed bool) error {
	return r.store.SetAllowNonLoopback(ctx, allowed)
}

// TokenStatus 把数据库令牌配置态转换为管理用例模型。
func (r mcpAdminRepository) TokenStatus(ctx context.Context) (mcpadminapp.TokenStatus, error) {
	// status、err 是数据库令牌配置态及其读取错误。
	status, err := r.store.TokenStatus(ctx)
	if err != nil {
		return mcpadminapp.TokenStatus{}, err
	}
	return mcpadminapp.TokenStatus{
		HasCurrent:        status.HasCurrent,
		CurrentCreatedAt:  status.CurrentCreatedAt,
		CurrentLastUsedAt: status.CurrentLastUsedAt,
		HasPrevious:       status.HasPrevious,
		PreviousExpiresAt: status.PreviousExpiresAt,
	}, nil
}

// RotateToken 轮换持久化令牌并返回一次性明文；明文不落日志、不写审计。
func (r mcpAdminRepository) RotateToken(ctx context.Context, now time.Time) (string, error) {
	return r.store.RotateToken(ctx, now)
}

// RevokeTokens 立即吊销全部持久化令牌。
func (r mcpAdminRepository) RevokeTokens(ctx context.Context) error {
	return r.store.RevokeTokens(ctx)
}

// ListAudit 分页查询调用审计并转换为管理用例模型。
func (r mcpAdminRepository) ListAudit(ctx context.Context, filter mcpadminapp.AuditFilter) (mcpadminapp.AuditPage, error) {
	// dbFilter 是对应的数据库过滤条件。
	dbFilter := db.MCPAuditFilter{
		Limit: filter.Limit, Offset: filter.Offset, Category: filter.Category,
		Name: filter.Name, CookieID: filter.CookieID, SuccessState: filter.SuccessState,
	}
	// page、err 是数据库分页结果及其查询错误。
	page, err := r.store.ListMCPAudit(ctx, dbFilter)
	if err != nil {
		return mcpadminapp.AuditPage{}, err
	}
	// records 是管理用例视图切片。
	records := make([]mcpadminapp.AuditEntry, 0, len(page.Records))
	// row 是当前数据库审计行。
	for _, row := range page.Records {
		records = append(records, mcpadminapp.AuditEntry{
			ID: row.ID, CreatedAt: row.CreatedAt, UserID: row.UserID,
			TokenSource: row.TokenSource, Category: row.Category, Name: row.Name,
			CookieID: row.CookieID, Arguments: row.Arguments, Success: row.Success,
			ErrorClass: row.ErrorClass, DurationMS: row.DurationMS,
		})
	}
	return mcpadminapp.AuditPage{Total: page.Total, Records: records}, nil
}
