package runtime

// mcp_item_adapter.go 把商品域应用服务投影为 internal/mcp 的 ItemPorts。
// 单商品发布的图片公网 URL 在本层下载并做大小/媒体类型限制，复用组合层既有下载回调。

import (
	"context"
	"path/filepath"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
)

// mcpBatchRetryLease 是 MCP 重试失败批次时声明的 worker 租约时长。
const mcpBatchRetryLease = 5 * time.Minute

// mcpItemPorts 聚合商品域全部应用服务。
type mcpItemPorts struct {
	// catalog 是本地商品读取服务。
	catalog *itemapp.CatalogService
	// mutation 是本地商品写入服务。
	mutation *itemapp.CatalogMutationService
	// sync 是平台商品同步服务。
	sync *itemapp.SyncService
	// singlePublish 是单商品平台发布服务。
	singlePublish *itemapp.Service
	// recommend 是类目推荐服务。
	recommend *itemapp.CategoryRecommendationService
	// preview 是批量发布预检服务。
	preview *itemapp.BatchPreviewService
	// persistence 是预检结果持久化服务。
	persistence *itemapp.BatchPreviewPersistenceService
	// management 是批次管理服务。
	management *itemapp.BatchManagementService
}

// 编译期断言适配器满足 MCP 商品端口。
var _ capability.ItemPorts = (*mcpItemPorts)(nil)

// ListItems 透传归属商品列表用例。
func (a *mcpItemPorts) ListItems(ctx context.Context, userID int64, cookieID string) ([]itemapp.CatalogItem, error) {
	return a.catalog.ListForUser(ctx, userID, cookieID)
}

// GetItem 透传单商品读取用例。
func (a *mcpItemPorts) GetItem(ctx context.Context, cookieID, itemID string) (itemapp.CatalogItem, error) {
	return a.catalog.Get(ctx, cookieID, itemID)
}

// CreateItem 透传本地商品创建用例（归属已在 MCP 层复核）。
func (a *mcpItemPorts) CreateItem(ctx context.Context, cookieID string, input itemapp.CatalogWriteInput) error {
	return a.mutation.Create(ctx, cookieID, input)
}

// UpdateItem 透传本地商品局部更新用例。
func (a *mcpItemPorts) UpdateItem(ctx context.Context, cookieID, itemID string, patch itemapp.CatalogPatchInput) error {
	return a.mutation.Update(ctx, cookieID, itemID, patch)
}

// DeleteItem 透传本地商品删除用例。
func (a *mcpItemPorts) DeleteItem(ctx context.Context, cookieID, itemID string) error {
	return a.mutation.Delete(ctx, cookieID, itemID)
}

// SetItemMultiSpec 透传多规格标记用例。
func (a *mcpItemPorts) SetItemMultiSpec(ctx context.Context, cookieID, itemID string, enabled bool) error {
	return a.mutation.SetMultiSpec(ctx, cookieID, itemID, enabled)
}

// SetItemMultiQuantity 透传多数量交付标记用例。
func (a *mcpItemPorts) SetItemMultiQuantity(ctx context.Context, cookieID, itemID string, enabled bool) error {
	return a.mutation.SetMultiQuantity(ctx, cookieID, itemID, enabled)
}

// SyncItemsAll 透传全量平台同步用例。
func (a *mcpItemPorts) SyncItemsAll(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncAllResult, error) {
	return a.sync.SyncAll(ctx, query)
}

// SyncItemsPage 透传分页平台同步用例。
func (a *mcpItemPorts) SyncItemsPage(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncPageResult, error) {
	return a.sync.SyncPage(ctx, query)
}

// PublishSingle 下载公网图片后透传单商品发布用例；二进制图片不经过 MCP 传输层。
func (a *mcpItemPorts) PublishSingle(ctx context.Context, input capability.SinglePublishInput) (itemapp.PublishOutcome, error) {
	// images 是下载后的应用层图片切片。
	images := make([]itemapp.Image, 0, len(input.ImageURLs))
	// rawURL 是当前待下载的图片公网地址。
	for _, rawURL := range input.ImageURLs {
		// data、contentType、downloadErr 分别是图片字节、MIME 类型与下载错误。
		data, contentType, downloadErr := composition.DownloadPublishImageURL(ctx, rawURL)
		if downloadErr != nil {
			return itemapp.PublishOutcome{}, downloadErr
		}
		images = append(images, itemapp.Image{
			Filename:    filepath.Base(rawURL),
			ContentType: contentType,
			Data:        data,
		})
	}
	// appInput 是应用层发布入参。
	appInput := itemapp.PublishInput{
		UserID: input.UserID, CookieID: input.CookieID, Title: input.Title,
		Description: input.Description, PriceCents: input.PriceCents,
		OriginalPriceCents: input.OriginalPriceCents, Quantity: input.Quantity,
		PostageMode: input.PostageMode, PostageCents: input.PostageCents,
		Images: images,
	}
	if input.CatID != "" {
		appInput.Category = &itemapp.PublishCategory{CatID: input.CatID}
	}
	return a.singlePublish.PublishSingle(ctx, appInput)
}

// RecommendCategory 透传类目推荐用例。
func (a *mcpItemPorts) RecommendCategory(ctx context.Context, userID int64, cookieID, keyword string) (itemapp.BatchPreviewCategory, error) {
	return a.recommend.Recommend(ctx, userID, cookieID, keyword)
}

// PreviewBatch 透传批量预检用例。
func (a *mcpItemPorts) PreviewBatch(ctx context.Context, input itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error) {
	return a.preview.Preview(ctx, input)
}

// PersistBatch 透传预检结果持久化用例。
func (a *mcpItemPorts) PersistBatch(ctx context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error) {
	return a.persistence.Persist(ctx, batch, rows)
}

// StartBatch 透传批次启动用例，使用 MCP 固定重试租约。
func (a *mcpItemPorts) StartBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	return a.management.StartBatch(ctx, userID, batchID, 0)
}

// ListBatches 透传批次列表用例。
func (a *mcpItemPorts) ListBatches(ctx context.Context, userID int64, limit int) ([]itemapp.BatchInfo, error) {
	return a.management.ListBatches(ctx, userID, limit)
}

// GetBatch 透传批次详情用例。
func (a *mcpItemPorts) GetBatch(ctx context.Context, userID int64, batchID string) (itemapp.BatchDetails, error) {
	return a.management.GetBatch(ctx, userID, batchID)
}

// CancelBatch 透传批次取消用例。
func (a *mcpItemPorts) CancelBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	return a.management.CancelBatch(ctx, userID, batchID)
}

// RetryBatch 透传失败行重试用例。
func (a *mcpItemPorts) RetryBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	return a.management.RetryFailedBatch(ctx, userID, batchID, mcpBatchRetryLease)
}

// DeleteBatch 透传批次删除用例。
func (a *mcpItemPorts) DeleteBatch(ctx context.Context, userID int64, batchID string) error {
	return a.management.DeleteBatch(ctx, userID, batchID)
}

// newMCPItemPorts 构造 MCP 商品端口；任一必需服务缺失时返回 nil，对应工具自动不注册。
func newMCPItemPorts(ports composition.TransportPorts) *mcpItemPorts {
	if ports.ItemCatalog == nil || ports.ItemCatalogMutation == nil || ports.ItemSync == nil ||
		ports.ItemSinglePublish == nil || ports.ItemCategoryRecommendation == nil ||
		ports.ItemBatchPreview == nil || ports.ItemBatchPreviewPersistence == nil || ports.ItemBatchManagement == nil {
		return nil
	}
	return &mcpItemPorts{
		catalog:       ports.ItemCatalog,
		mutation:      ports.ItemCatalogMutation,
		sync:          ports.ItemSync,
		singlePublish: ports.ItemSinglePublish,
		recommend:     ports.ItemCategoryRecommendation,
		preview:       ports.ItemBatchPreview,
		persistence:   ports.ItemBatchPreviewPersistence,
		management:    ports.ItemBatchManagement,
	}
}
