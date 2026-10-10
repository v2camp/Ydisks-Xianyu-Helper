package runtime

// mcp_transport.go 把数据库仓储投影为 internal/mcp 自定义的最小消费者端口，
// 并装配 /mcp 的 Streamable HTTP 挂载条目。组合层只做透传与模型转换，不写 MCP 业务规则。

import (
	"context"
	"fmt"
	"time"

	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/db"
	"xianyu-go/internal/mcp"
	"xianyu-go/internal/server"
	appversion "xianyu-go/internal/version"
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
		ErrorClass:  entry.ErrorClass,
		DurationMS:  entry.DurationMS,
	}
	return a.store.AddMCPAudit(ctx, record)
}

// ListAudits 分页查询调用审计并把数据库行映射为 MCP 传输模型。
func (a mcpAuditAdapter) ListAudits(ctx context.Context, filter mcp.AuditFilter) (mcp.AuditPage, error) {
	// dbFilter 是对应的数据库过滤条件。
	dbFilter := db.MCPAuditFilter{
		Limit: filter.Limit, Offset: filter.Offset, Category: filter.Category,
		Name: filter.Name, CookieID: filter.CookieID, SuccessState: filter.SuccessState,
	}
	// page、err 是数据库分页结果。
	page, err := a.store.ListMCPAudit(ctx, dbFilter)
	if err != nil {
		return mcp.AuditPage{}, err
	}
	// records 是 MCP 视图切片。
	records := make([]mcp.AuditEntry, 0, len(page.Records))
	// row 是当前数据库审计行。
	for _, row := range page.Records {
		records = append(records, mcp.AuditEntry{
			UserID:        row.UserID,
			Source:        mcp.TokenSource(row.TokenSource),
			Category:      row.Category,
			Name:          row.Name,
			CookieID:      row.CookieID,
			ArgumentsJSON: row.Arguments,
			Success:       row.Success,
			ErrorClass:    row.ErrorClass,
			DurationMS:    row.DurationMS,
			CreatedAt:     row.CreatedAt,
		})
	}
	return mcp.AuditPage{Total: page.Total, Records: records}, nil
}

// BuildMCPEndpoint 装配 MCP 协议端点并返回对应的主路由挂载条目。
// environmentToken 为 cmd 从 XIANYU_MCP_TOKEN 读取的引导令牌，空白表示未配置。
// 此时只装配安全与协议骨架；业务工具、资源与提示在应用服务就绪后由注册函数补充。
func BuildMCPEndpoint(store *db.Store, environmentToken string) (*mcp.Endpoint, server.ExtraRoute, error) {
	if store == nil || store.MCP == nil || store.Users == nil {
		return nil, server.ExtraRoute{}, fmt.Errorf("MCP 端点缺少数据库仓储")
	}
	// audit 是同时支持写入与分页查询的审计适配器。
	audit := mcpAuditAdapter{store: store.MCP}
	// toolRisks、risksErr 是平台工具危险等级标注表及其构造错误；表本身是代码常量。
	toolRisks, risksErr := capability.PlatformToolRisks()
	if risksErr != nil {
		return nil, server.ExtraRoute{}, fmt.Errorf("构造平台工具危险等级表失败: %w", risksErr)
	}
	// endpoint、err 是协议端点及其构造错误。
	endpoint, err := mcp.NewEndpoint(mcp.EndpointConfig{
		Config:           mcpConfigAdapter{store: store.MCP},
		Identity:         &mcpIdentityAdapter{users: store.Users},
		Audit:            audit,
		AuditLister:      audit,
		ToolRisks:        toolRisks,
		SystemVersion:    appversion.Version,
		EnvironmentToken: environmentToken,
	})
	if err != nil {
		return nil, server.ExtraRoute{}, err
	}
	// 自检工具不依赖业务域，随端点立即可用。
	endpoint.RegisterSystemTools()
	// route 是挂进主 chi 路由树的条目；处理器已内含 Bearer 与 loopback 安全门。
	route := server.ExtraRoute{Pattern: MCPRoutePattern, Handler: endpoint.Handler()}
	return endpoint, route, nil
}

// RegisterMCPTools 在应用服务集合就绪后注册全部域工具；每域端口为 nil 时跳过该域。
// lifecycleContext 为需要脱离请求生命周期的后台 worker（如订单刷新任务）提供进程级 Context。
// backgroundTasks 返回进程后台任务快照，由 HTTP 服务在构造完成后提供；为空时总览返回空列表。
func RegisterMCPTools(endpoint *mcp.Endpoint, ports composition.TransportPorts, lifecycleContext func() context.Context, backgroundTasks func() []capability.BackgroundTask) {
	if endpoint == nil {
		return
	}
	// 账号域端口投影。
	endpoint.RegisterAccountTools(newMCPAccountPorts(ports))
	// 订单、分析与异常域端口投影；任一服务缺失时对应适配器为 nil，注册自动跳过。
	var analytics capability.AnalyticsPorts
	if ports.Analytics != nil {
		analytics = &mcpAnalyticsPorts{service: ports.Analytics}
	}
	// issues 是自动化异常处理端口；服务缺失时保持 nil，工具注册自动跳过。
	var issues capability.IssuePorts
	if ports.AutomationIssues != nil {
		issues = &mcpIssuePorts{service: ports.AutomationIssues}
	}
	endpoint.RegisterOrderTools(newMCPOrderPorts(ports, lifecycleContext), analytics, issues)
	// 商品域工具：账号端口同时用于本地商品写入前的归属复核。
	endpoint.RegisterItemTools(newMCPAccountPorts(ports), newMCPItemPorts(ports))
	// 卡密库存域工具：明文卡密与 API 模板只在应用层内流转，MCP 侧仅注册非敏感视图与只写入口。
	endpoint.RegisterCardTools(newMCPCardPorts(ports))
	// 自动化配置域工具：规则校验与模板变量契约完全复用应用服务，MCP 只做透传。
	endpoint.RegisterRuleTools(newMCPRulePorts(ports))
	endpoint.RegisterDeliveryTemplateTools(newMCPDeliveryTemplatePorts(ports))
	endpoint.RegisterDefaultReplyTools(newMCPDefaultReplyPorts(ports))
	endpoint.RegisterKeywordTools(newMCPKeywordPorts(ports))
	// 聊天域工具：发送与删除复用 chat 应用服务，图片下载复用组合层受控公网策略。
	endpoint.RegisterChatTools(newMCPChatPorts(ports))
	// 通知域工具：渠道配置只写不读，删除/清空/测试发送走 confirm 守卫。
	endpoint.RegisterNotificationTools(newMCPNotificationChannelPorts(ports), newMCPUncertainNotificationPorts(ports))
	// 设置与 AI 域工具：敏感值只写不读，MCP 自身状态键禁止经通用设置入口修改。
	endpoint.RegisterSettingsTools(newMCPSettingsPorts(ports))
	endpoint.RegisterAITools(newMCPSettingsPorts(ports))
	// 管理员全局域工具：删除用户复用 AdminService，后台任务总览只读当前进程。
	endpoint.RegisterAdminTools(newMCPAdminPorts(ports, backgroundTasks))
	// 只读资源复用各域只读用例，内容与对应工具 DTO 一致且同样脱敏；运维提示不依赖业务端口。
	endpoint.RegisterResources(newMCPResourcePorts(ports))
	endpoint.RegisterPrompts()
}
