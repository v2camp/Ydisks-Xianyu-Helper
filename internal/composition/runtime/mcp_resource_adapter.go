package runtime

// mcp_resource_adapter.go 把只读资源所需的同域查询用例投影为 internal/mcp 的 ResourcePorts。
// 适配器只做透传：资源内容与对应工具使用同一应用服务，脱敏规则完全一致。

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	orderapp "xianyu-go/internal/application/orders"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/mcp"
)

// mcpResourcePorts 聚合各域只读查询用例。
type mcpResourcePorts struct {
	// analytics 是仪表盘计数用例。
	analytics *analyticsapp.Service
	// accounts 是账号摘要用例。
	accounts *accountapp.SummaryService
	// orders 是订单列表用例。
	orders *orderapp.ServiceSet
	// issues 是自动化异常用例。
	issues *automationapp.IssueService
	// cards 是卡券库存用例。
	cards *cardsapp.Service
}

// 编译期断言适配器满足 MCP 只读资源端口。
var _ mcp.ResourcePorts = (*mcpResourcePorts)(nil)

// DashboardStats 透传仪表盘计数用例。
func (a *mcpResourcePorts) DashboardStats(ctx context.Context, userID int64) (analyticsapp.DashboardStats, error) {
	return a.analytics.DashboardStats(ctx, userID)
}

// ListAccountSummaries 透传账号摘要列表用例。
func (a *mcpResourcePorts) ListAccountSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error) {
	return a.accounts.ListSummaries(ctx, userID)
}

// GetAccountSummary 透传单个归属账号摘要用例。
func (a *mcpResourcePorts) GetAccountSummary(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error) {
	return a.accounts.GetOwnedSummary(ctx, userID, cookieID)
}

// ListOrders 透传订单分页查询用例。
func (a *mcpResourcePorts) ListOrders(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	return a.orders.List.List(ctx, query)
}

// ListAutomationIssues 透传自动化异常查询用例。
func (a *mcpResourcePorts) ListAutomationIssues(ctx context.Context, userID int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error) {
	return a.issues.ListIssues(ctx, userID)
}

// ListCards 透传卡券组列表用例；库存正文只在 MCP 层用于计数，不进入资源输出。
func (a *mcpResourcePorts) ListCards(ctx context.Context, userID int64) ([]cardsapp.Card, error) {
	return a.cards.List(ctx, userID)
}

// newMCPResourcePorts 构造 MCP 只读资源端口；任一必需服务缺失时返回 nil，资源不注册。
func newMCPResourcePorts(ports composition.TransportPorts) *mcpResourcePorts {
	if ports.Analytics == nil || ports.AccountSummaries == nil || ports.Orders == nil ||
		ports.AutomationIssues == nil || ports.Cards == nil {
		return nil
	}
	return &mcpResourcePorts{
		analytics: ports.Analytics,
		accounts:  ports.AccountSummaries,
		orders:    ports.Orders,
		issues:    ports.AutomationIssues,
		cards:     ports.Cards,
	}
}
