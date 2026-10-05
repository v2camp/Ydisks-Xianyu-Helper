// domain_ports.go 汇总 internal/mcp 各域工具消费的最小应用端口。
// 这些接口由 internal/mcp 按用例需要自定义，由 internal/composition/runtime 用
// 既有应用服务投影实现；接口签名只引用 internal/application/* 的应用模型，
// 不引用 server、db 或任何平台实现包。

package mcp

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	orderapp "xianyu-go/internal/application/orders"
)

// AccountPorts 聚合账号域工具所需的账号用例集合。
type AccountPorts interface {
	// 摘要与归属。
	ListAdminSummaries(ctx context.Context) ([]accountapp.AdminAccountSummary, error)
	ListSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error)
	GetOwnedSummary(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error)
	RequireOwnership(ctx context.Context, userID int64, cookieID string) error
	// 本地配置写入。
	UpdateAccountSettings(ctx context.Context, input accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error)
	SetAccountStatus(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.StatusResult, error)
	SetAccountPause(ctx context.Context, userID int64, cookieID string, minutes int) (accountapp.SettingsResult, error)
	GetAccountPause(ctx context.Context, userID int64, cookieID string) (accountapp.PauseState, error)
	SetAutoConfirm(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetAutoConsign(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetAutoBargain(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetRemark(ctx context.Context, userID int64, cookieID, remark string) (accountapp.SettingsResult, error)
	// 平台触达与运行时。
	QueryLongLogin(ctx context.Context, userID int64, cookieID string) (accountapp.LongLoginResult, error)
	SetLongLogin(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.LongLoginResult, error)
	RuntimeStatuses(ctx context.Context) (map[string]accountapp.RuntimeStatus, error)
	RestartAccount(ctx context.Context, cookieID string) error
	RefreshProfile(ctx context.Context, userID int64, cookieID string) (accountapp.ProfileResult, error)
	DeleteAccount(ctx context.Context, userID int64, cookieID string) error
	// 自动评价/擦亮任务。
	GetAccountTaskSettings(ctx context.Context, cookieID string) (automationapp.AccountTaskSettings, error)
	UpdateAccountTaskSettings(ctx context.Context, settings automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error)
	ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]automationapp.AccountTaskRun, error)
	RunAccountTask(ctx context.Context, cookieID, taskType string) (automationapp.TaskSummary, error)
}

// OrderPorts 聚合订单查询、刷新、手动发货与刷新任务用例。
type OrderPorts interface {
	// List 按条件分页查询订单。
	List(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error)
	// Get 读取单个归属订单详情。
	Get(ctx context.Context, userID int64, orderID string) (*orderapp.Order, error)
	// RefreshSingle 向平台刷新单个订单。
	RefreshSingle(ctx context.Context, userID int64, orderID string) (orderapp.SingleRefreshResult, error)
	// Refresh 按账号或状态批量向平台刷新订单。
	Refresh(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshResult, error)
	// ManualShip 执行手动发货；只改本地状态或执行完整发货链路由 shipMode 决定。
	ManualShip(ctx context.Context, request orderapp.ManualShipRequest) (orderapp.ManualShipResult, error)
	// CreateRefreshJob 创建并启动后台批量刷新任务。
	CreateRefreshJob(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshJobStartResult, error)
	// GetRefreshJob 查询刷新任务快照。
	GetRefreshJob(ctx context.Context, userID int64, jobID string) (*orderapp.RefreshJob, error)
	// CancelRefreshJob 取消排队或运行中的刷新任务。
	CancelRefreshJob(ctx context.Context, userID int64, jobID string) (orderapp.RefreshJobCancelResult, error)
}

// AnalyticsPorts 聚合仪表盘与订单分析用例。
type AnalyticsPorts interface {
	// DashboardStats 返回首页非敏感计数；不接受日期范围。
	DashboardStats(ctx context.Context, userID int64) (analyticsapp.DashboardStats, error)
	// OrderAnalytics 返回收益、按日与按状态聚合。
	OrderAnalytics(ctx context.Context, query analyticsapp.Query) (analyticsapp.OrderAnalytics, error)
	// ValidOrders 分页返回参与分析的有效订单明细。
	ValidOrders(ctx context.Context, query analyticsapp.Query, page, pageSize int) (analyticsapp.ValidOrders, error)
}

// IssuePorts 聚合自动化异常与死信延期任务的查询和人工处理用例。
type IssuePorts interface {
	// ListIssues 返回待人工处理的运行异常与死信任务。
	ListIssues(ctx context.Context, userID int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error)
	// ResolveRunIssue 对异常运行执行人工处理动作。
	ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error
	// ResolveDeferredIssue 对死信延期任务执行 retry 或 dismiss。
	ResolveDeferredIssue(ctx context.Context, userID, taskID int64, resolution string) error
}

// DomainPorts 是全部域工具端口的聚合；某域为 nil 时该域工具不注册。
type DomainPorts struct {
	// Account 是账号域端口。
	Account AccountPorts
	// Orders 是订单履约域端口。
	Orders OrderPorts
	// Analytics 是仪表盘分析域端口。
	Analytics AnalyticsPorts
	// Issues 是自动化异常处理域端口。
	Issues IssuePorts
}
