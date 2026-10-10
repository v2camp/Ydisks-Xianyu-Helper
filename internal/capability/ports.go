// ports.go 汇总跨消费方共用的能力用例契约。
//
// 这些接口由能力消费者定义：多个消费方（MCP 传输层承载的外部 Harness、internal/agent
// 承载的客服 Agent）需要同一份用例语义时，契约放在能力内核，避免各自定义导致签名漂移。
// 接口只引用 internal/application/* 的应用模型，实现由 internal/composition 投影提供；
// 本包只声明契约，不实现、不调用、不触达业务数据。

package capability

import (
	"context"

	itemapp "xianyu-go/internal/application/items"
	orderapp "xianyu-go/internal/application/orders"
)

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

// ItemPorts 聚合商品货架、同步、发布与批量发布用例。
// 本地商品写入方法的账号归属由调用方经账号归属用例复核后调用。
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

// SinglePublishInput 是单商品发布输入；图片只接受公网 URL，由组合层下载并校验后交给应用层。
type SinglePublishInput struct {
	// UserID 是发起发布的用户标识。
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
