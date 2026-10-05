package runtime

// mcp_order_adapter.go 把订单服务集合、刷新任务服务、分析服务与异常服务投影为
// internal/mcp 的 OrderPorts/AnalyticsPorts/IssuePorts。

import (
	"context"

	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	orderapp "xianyu-go/internal/application/orders"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/mcp"
)

// mcpOrderPorts 聚合订单查询/履约端口与后台刷新任务端口。
type mcpOrderPorts struct {
	// set 是订单应用服务集合。
	set *orderapp.ServiceSet
	// jobs 是订单刷新任务应用服务。
	jobs *orderapp.RefreshJobService
	// lifecycleContext 返回应用协调器拥有的 worker 根 Context。
	lifecycleContext func() context.Context
}

// 编译期断言适配器满足 MCP 订单端口。
var _ mcp.OrderPorts = (*mcpOrderPorts)(nil)

// List 透传订单分页查询用例。
func (a *mcpOrderPorts) List(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	return a.set.List.List(ctx, query)
}

// Get 透传单订单详情用例。
func (a *mcpOrderPorts) Get(ctx context.Context, userID int64, orderID string) (*orderapp.Order, error) {
	return a.set.Detail.Get(ctx, userID, orderID)
}

// RefreshSingle 透传单订单平台刷新用例。
func (a *mcpOrderPorts) RefreshSingle(ctx context.Context, userID int64, orderID string) (orderapp.SingleRefreshResult, error) {
	return a.set.Refresh.RefreshSingle(ctx, userID, orderID)
}

// Refresh 透传批量订单平台刷新用例。
func (a *mcpOrderPorts) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshResult, error) {
	return a.set.Refresh.Refresh(ctx, userID, cookieID, status)
}

// ManualShip 透传手动发货用例。
func (a *mcpOrderPorts) ManualShip(ctx context.Context, request orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
	return a.set.ManualShip.ManualShip(ctx, request)
}

// CreateRefreshJob 透传刷新任务创建启动用例；worker 绑定应用生命周期 Context，请求 Context 只用于归属与落库。
func (a *mcpOrderPorts) CreateRefreshJob(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshJobStartResult, error) {
	return a.jobs.CreateAndStart(ctx, a.lifecycleContext(), userID, cookieID, status)
}

// GetRefreshJob 透传刷新任务查询用例。
func (a *mcpOrderPorts) GetRefreshJob(ctx context.Context, userID int64, jobID string) (*orderapp.RefreshJob, error) {
	return a.jobs.GetJob(ctx, userID, jobID)
}

// CancelRefreshJob 透传刷新任务取消用例。
func (a *mcpOrderPorts) CancelRefreshJob(ctx context.Context, userID int64, jobID string) (orderapp.RefreshJobCancelResult, error) {
	return a.jobs.CancelForUser(ctx, userID, jobID)
}

// mcpAnalyticsPorts 是分析服务的薄包装类型。
type mcpAnalyticsPorts struct {
	// service 是订单分析应用服务。
	service *analyticsapp.Service
}

// 编译期断言适配器满足 MCP 分析端口。
var _ mcp.AnalyticsPorts = (*mcpAnalyticsPorts)(nil)

// DashboardStats 透传仪表盘计数用例。
func (a *mcpAnalyticsPorts) DashboardStats(ctx context.Context, userID int64) (analyticsapp.DashboardStats, error) {
	return a.service.DashboardStats(ctx, userID)
}

// OrderAnalytics 透传订单分析聚合用例。
func (a *mcpAnalyticsPorts) OrderAnalytics(ctx context.Context, query analyticsapp.Query) (analyticsapp.OrderAnalytics, error) {
	return a.service.OrderAnalytics(ctx, query)
}

// ValidOrders 透传有效订单分页用例。
func (a *mcpAnalyticsPorts) ValidOrders(ctx context.Context, query analyticsapp.Query, page, pageSize int) (analyticsapp.ValidOrders, error) {
	return a.service.ValidOrders(ctx, query, page, pageSize)
}

// mcpIssuePorts 是自动化异常服务的薄包装类型。
type mcpIssuePorts struct {
	// service 是自动化异常应用服务。
	service *automationapp.IssueService
}

// 编译期断言适配器满足 MCP 异常处理端口。
var _ mcp.IssuePorts = (*mcpIssuePorts)(nil)

// ListIssues 透传异常与死信列表用例。
func (a *mcpIssuePorts) ListIssues(ctx context.Context, userID int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error) {
	return a.service.ListIssues(ctx, userID)
}

// ResolveRunIssue 透传运行异常人工处理用例。
func (a *mcpIssuePorts) ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error {
	return a.service.ResolveRunIssue(ctx, userID, runID, resolution)
}

// ResolveDeferredIssue 透传死信任务人工处理用例。
func (a *mcpIssuePorts) ResolveDeferredIssue(ctx context.Context, userID, taskID int64, resolution string) error {
	return a.service.ResolveDeferredIssue(ctx, userID, taskID, resolution)
}

// newMCPOrderPorts 构造 MCP 订单端口；lifecycleContext 为刷新任务 worker 提供进程级 Context。
func newMCPOrderPorts(ports composition.TransportPorts, lifecycleContext func() context.Context) *mcpOrderPorts {
	if ports.Orders == nil || ports.OrderRefreshJobs == nil || lifecycleContext == nil {
		return nil
	}
	return &mcpOrderPorts{set: ports.Orders, jobs: ports.OrderRefreshJobs, lifecycleContext: lifecycleContext}
}
