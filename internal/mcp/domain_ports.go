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
	cardsapp "xianyu-go/internal/application/cards"
	itemapp "xianyu-go/internal/application/items"
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

// ItemPorts 聚合商品货架、同步、发布与批量发布用例。
// 本地商品写入方法的账号归属由 MCP 层先经 AccountPorts.RequireOwnership 复核。
type ItemPorts interface {
	// 本地商品目录。
	ListItems(ctx context.Context, userID int64, cookieID string) ([]itemapp.CatalogItem, error)
	GetItem(ctx context.Context, cookieID, itemID string) (itemapp.CatalogItem, error)
	CreateItem(ctx context.Context, cookieID string, input itemapp.CatalogWriteInput) error
	UpdateItem(ctx context.Context, cookieID, itemID string, patch itemapp.CatalogPatchInput) error
	DeleteItem(ctx context.Context, cookieID, itemID string) error
	SetItemMultiSpec(ctx context.Context, cookieID, itemID string, enabled bool) error
	SetItemMultiQuantity(ctx context.Context, cookieID, itemID string, enabled bool) error
	// 平台同步与发布。
	SyncItemsAll(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncAllResult, error)
	SyncItemsPage(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncPageResult, error)
	PublishSingle(ctx context.Context, input SinglePublishInput) (itemapp.PublishOutcome, error)
	RecommendCategory(ctx context.Context, userID int64, cookieID, keyword string) (itemapp.BatchPreviewCategory, error)
	// 批量发布预检与管理。
	PreviewBatch(ctx context.Context, input itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error)
	PersistBatch(ctx context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error)
	StartBatch(ctx context.Context, userID int64, batchID string) (string, error)
	ListBatches(ctx context.Context, userID int64, limit int) ([]itemapp.BatchInfo, error)
	GetBatch(ctx context.Context, userID int64, batchID string) (itemapp.BatchDetails, error)
	CancelBatch(ctx context.Context, userID int64, batchID string) (string, error)
	RetryBatch(ctx context.Context, userID int64, batchID string) (string, error)
	DeleteBatch(ctx context.Context, userID int64, batchID string) error
}

// SinglePublishInput 是 MCP 单商品发布输入；图片只接受公网 URL，由组合层下载并校验后交给应用层。
type SinglePublishInput struct {
	// UserID 是固定管理员用户标识。
	UserID int64
	// CookieID 是发布账号标识。
	CookieID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述。
	Description string
	// PriceCents 是售价（分）。
	PriceCents int64
	// OriginalPriceCents 是原价（分）。
	OriginalPriceCents int64
	// Quantity 是库存数量。
	Quantity int
	// PostageMode 是邮费模式 free/fixed。
	PostageMode string
	// PostageCents 是固定邮费（分）。
	PostageCents int64
	// ImageURLs 是商品图片公网 URL。
	ImageURLs []string
	// CatID 是可选指定闲鱼类目；为空走自动推荐。
	CatID string
}

// CardPorts 聚合卡券库存 CRUD、追加卡密与 API 配置连通性测试用例。
type CardPorts interface {
	// ListCards 返回用户全部卡券组（应用模型含明文，MCP 层负责裁剪）。
	ListCards(ctx context.Context, userID int64) ([]cardsapp.Card, error)
	// GetCard 返回单个归属卡券组。
	GetCard(ctx context.Context, userID, cardID int64) (cardsapp.Card, error)
	// CreateCard 创建卡券组并返回新标识。
	CreateCard(ctx context.Context, userID int64, draft cardsapp.Draft) (int64, error)
	// UpdateCard 更新卡券组；库存正文仅在 draft.DataContentSet 时覆盖。
	UpdateCard(ctx context.Context, userID, cardID int64, draft cardsapp.Draft) error
	// DeleteCard 删除卡券组。
	DeleteCard(ctx context.Context, userID, cardID int64) error
	// AppendCardData 向 data 卡券组追加逐行卡密，返回新增行数。
	AppendCardData(ctx context.Context, userID, cardID int64, content string) (int, error)
	// TestCardAPI 用已保存的完整配置对归属 API 卡券组发起一次受控连通性测试；
	// 完整请求模板由应用层内部读取，不经过 MCP 层，诊断结果只含非敏感字段。
	TestCardAPI(ctx context.Context, userID, cardID int64) (cardsapp.APIRequestTestResult, error)
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
	// Items 是商品货架与批量发布域端口。
	Items ItemPorts
	// Cards 是卡密库存域端口。
	Cards CardPorts
}
