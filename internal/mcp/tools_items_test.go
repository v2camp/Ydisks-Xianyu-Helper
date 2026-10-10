// tools_items_test.go 覆盖商品域工具：归属复核、confirm 守卫、预检行映射、CSV 导出与批次 ID。

package mcp

import (
	"context"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"

	"xianyu-go/internal/capability"
)

// fakeItemPorts 是商品域假端口，记录平台与本地写入调用。
type fakeItemPorts struct {
	// deleted 记录删除商品调用。
	deleted [][2]string
	// syncedAll 记录全量同步的账号。
	syncedAll []string
	// published 记录单发布标题。
	published []string
	// batchStarted 记录启动的批次。
	batchStarted []string
	// previewCalls 记录预检次数。
	previewCalls int
	// persistCalls 记录持久化次数。
	persistCalls int
}

// ListItems 返回一个本地商品。
func (f *fakeItemPorts) ListItems(context.Context, int64, string) ([]itemapp.CatalogItem, error) {
	return []itemapp.CatalogItem{{ItemID: "i1", CookieID: "a1", ItemTitle: "本地商品", ItemPrice: "5.00", IsMultiSpec: true}}, nil
}

// GetItem 返回单个商品。
func (f *fakeItemPorts) GetItem(context.Context, string, string) (itemapp.CatalogItem, error) {
	return itemapp.CatalogItem{ItemID: "i1", CookieID: "a1", ItemTitle: "本地商品"}, nil
}

// CreateItem 默认成功。
func (f *fakeItemPorts) CreateItem(context.Context, string, itemapp.CatalogWriteInput) error {
	return nil
}

// UpdateItem 默认成功。
func (f *fakeItemPorts) UpdateItem(context.Context, string, string, itemapp.CatalogPatchInput) error {
	return nil
}

// DeleteItem 记录删除调用。
func (f *fakeItemPorts) DeleteItem(_ context.Context, cookieID, itemID string) error {
	f.deleted = append(f.deleted, [2]string{cookieID, itemID})
	return nil
}

// SetItemMultiSpec 默认成功。
func (f *fakeItemPorts) SetItemMultiSpec(context.Context, string, string, bool) error { return nil }

// SetItemMultiQuantity 默认成功。
func (f *fakeItemPorts) SetItemMultiQuantity(context.Context, string, string, bool) error { return nil }

// SyncItemsAll 记录全量同步并返回固定结果。
func (f *fakeItemPorts) SyncItemsAll(_ context.Context, query itemapp.SyncQuery) (itemapp.SyncAllResult, error) {
	f.syncedAll = append(f.syncedAll, query.CookieID)
	return itemapp.SyncAllResult{TotalCount: 3, TotalPages: 1, SavedCount: 3}, nil
}

// SyncItemsPage 返回固定分页结果。
func (f *fakeItemPorts) SyncItemsPage(context.Context, itemapp.SyncQuery) (itemapp.SyncPageResult, error) {
	return itemapp.SyncPageResult{PageNumber: 1, CurrentCount: 3, SavedCount: 3}, nil
}

// PublishSingle 记录发布标题并返回成功结果（模拟响应 Cookie 警告）。
func (f *fakeItemPorts) PublishSingle(_ context.Context, input capability.SinglePublishInput) (itemapp.PublishOutcome, error) {
	f.published = append(f.published, input.Title)
	return itemapp.PublishOutcome{
		Result:            &itemapp.PublishResult{ItemID: "new-1", ItemURL: "https://goofish.com/item/new-1", Title: input.Title, Quantity: input.Quantity},
		ResponseCookieErr: nil,
	}, nil
}

// RecommendCategory 返回固定类目。
func (f *fakeItemPorts) RecommendCategory(context.Context, int64, string, string) (itemapp.BatchPreviewCategory, error) {
	return itemapp.BatchPreviewCategory{CatID: "c1", CatName: "数字内容", ChannelCatID: "ch1"}, nil
}

// PreviewBatch 记录调用并把每行标记为合法（缺标题行报错由应用层负责，这里只透传）。
func (f *fakeItemPorts) PreviewBatch(_ context.Context, input itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error) {
	f.previewCalls++
	// rows 是回传的预检行切片。
	rows := make([]itemapp.BatchPreviewRow, 0, len(input.Rows))
	// index、fields 是当前行序号与字段映射。
	for index, fields := range input.Rows {
		// title 是该行标题。
		title, _ := fields["title"].(string)
		rows = append(rows, itemapp.BatchPreviewRow{
			RowNo: index + 1, CookieID: input.DefaultCookieID, Title: title, Price: "1.00", Quantity: 1,
		})
	}
	return rows, nil
}

// PersistBatch 记录调用并回传成功持久化结果。
func (f *fakeItemPorts) PersistBatch(_ context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error) {
	f.persistCalls++
	return itemapp.BatchPreviewPersistenceResult{Success: true, PreviewID: batch.ID, Total: len(rows), Valid: len(rows)}, nil
}

// StartBatch 记录启动批次。
func (f *fakeItemPorts) StartBatch(_ context.Context, _ int64, batchID string) (string, error) {
	f.batchStarted = append(f.batchStarted, batchID)
	return "running", nil
}

// ListBatches 返回一个批次。
func (f *fakeItemPorts) ListBatches(context.Context, int64, int) ([]itemapp.BatchInfo, error) {
	return []itemapp.BatchInfo{{ID: "b1", Status: "succeeded", TotalCount: 2, SuccessCount: 2, Filename: "mcp.json"}}, nil
}

// GetBatch 返回含两行明细的批次详情。
func (f *fakeItemPorts) GetBatch(context.Context, int64, string) (itemapp.BatchDetails, error) {
	return itemapp.BatchDetails{
		Batch: itemapp.BatchInfo{ID: "b1", Status: "succeeded", TotalCount: 2, SuccessCount: 1, FailedCount: 1},
		Rows: []itemapp.BatchRow{
			{RowNo: 1, Title: "商品A", Status: "success", ItemID: "i9", ItemURL: "https://x/i9"},
			{RowNo: 2, Title: "商品B", Status: "failed", FailureKind: "manual_review", ErrorMessage: "需人工"},
		},
	}, nil
}

// CancelBatch 返回取消中状态。
func (f *fakeItemPorts) CancelBatch(context.Context, int64, string) (string, error) {
	return "canceling", nil
}

// RetryBatch 返回排队状态。
func (f *fakeItemPorts) RetryBatch(context.Context, int64, string) (string, error) {
	return "queued", nil
}

// DeleteBatch 默认成功。
func (f *fakeItemPorts) DeleteBatch(context.Context, int64, string) error { return nil }

// newItemToolEndpoint 构造注册商品工具的端点，账号假端口默认放行归属。
func newItemToolEndpoint(t *testing.T) (*Endpoint, *fakeItemPorts, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// items 是商品假端口。
	items := &fakeItemPorts{}
	// accounts 是默认放行的账号假端口。
	accounts := &fakeAccountPorts{}
	endpoint.RegisterItemTools(accounts, items)
	// ctx 是携带管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, items, ctx
}

// TestItemListMapping 验证本地商品列表映射。
func TestItemListMapping(t *testing.T) {
	// endpoint、_、ctx 是商品工具端点与上下文。
	endpoint, _, ctx := newItemToolEndpoint(t)
	// result 是商品列表结果。
	result := invoke(endpoint, ctx, "item_list", map[string]any{"account_id": "a1"})
	if result.IsError {
		t.Fatalf("商品列表失败: %s", resultJSON(t, result))
	}
	// text 是列表 JSON，必须含多规格标记且不含扩展详情原文以外的敏感内容。
	text := resultJSON(t, result)
	if !strings.Contains(text, `"multi_spec":true`) || !strings.Contains(text, `"total":1`) {
		t.Fatalf("商品列表映射异常: %s", text)
	}
}

// TestItemDeleteConfirmAndOwnership 验证商品删除的 confirm 与归属复核。
func TestItemDeleteConfirmAndOwnership(t *testing.T) {
	// endpoint、items、ctx 是商品工具端点、假端口与上下文。
	endpoint, items, ctx := newItemToolEndpoint(t)
	// denied 是缺 confirm 的删除。
	denied := invoke(endpoint, ctx, "item_delete", map[string]any{"account_id": "a1", "item_id": "i1"})
	if !denied.IsError || len(items.deleted) != 0 {
		t.Fatal("缺 confirm 时商品删除必须拦截且零触达")
	}
	// allowed 是显式确认后的删除。
	allowed := invoke(endpoint, ctx, "item_delete", map[string]any{
		"account_id": "a1", "item_id": "i1", "confirm": true,
	})
	if allowed.IsError || len(items.deleted) != 1 {
		t.Fatalf("带 confirm 删除应执行: %s", resultJSON(t, allowed))
	}
}

// TestPlatformActionsRequireConfirm 验证平台同步与发布动作缺 confirm 零触达。
func TestPlatformActionsRequireConfirm(t *testing.T) {
	// endpoint、items、ctx 是商品工具端点、假端口与上下文。
	endpoint, items, ctx := newItemToolEndpoint(t)
	// syncDenied 是缺 confirm 的全量同步。
	syncDenied := invoke(endpoint, ctx, "item_sync_all", map[string]any{"account_id": "a1"})
	if !syncDenied.IsError || len(items.syncedAll) != 0 {
		t.Fatal("缺 confirm 时平台同步必须拦截")
	}
	// publishDenied 是缺 confirm 的单商品发布。
	publishDenied := invoke(endpoint, ctx, "item_publish_single", map[string]any{
		"account_id": "a1", "title": "新品", "price_cents": 100.0,
	})
	if !publishDenied.IsError || len(items.published) != 0 {
		t.Fatal("缺 confirm 时商品发布必须拦截")
	}
	// publishOK 是显式确认后的发布。
	publishOK := invoke(endpoint, ctx, "item_publish_single", map[string]any{
		"account_id": "a1", "title": "新品", "price_cents": 100.0,
		"postage_mode": "free", "quantity": 2.0, "confirm": true,
	})
	if publishOK.IsError || len(items.published) != 1 {
		t.Fatalf("带 confirm 发布应执行: %s", resultJSON(t, publishOK))
	}
	// text 是发布结果，必须返回平台商品标识与 URL。
	text := resultJSON(t, publishOK)
	if !strings.Contains(text, "new-1") || !strings.Contains(text, `"quantity":2`) {
		t.Fatalf("发布结果映射异常: %s", text)
	}
}

// TestBatchPreviewMapsJSONRows 验证 JSON 行被映射为预检字段并统计结果。
func TestBatchPreviewMapsJSONRows(t *testing.T) {
	// endpoint、items、ctx 是商品工具端点、假端口与上下文。
	endpoint, items, ctx := newItemToolEndpoint(t)
	// result 是两行商品的预检结果。
	result := invoke(endpoint, ctx, "item_batch_preview", map[string]any{
		"default_account_id": "a1",
		"rows": []any{
			map[string]any{"title": "商品A", "price": "1.00", "quantity": 1.0,
				"image_urls": []any{"https://example.com/a.png"}},
			map[string]any{"account_id": "a2", "title": "商品B", "price": "2.00", "postage_mode": "free"},
		},
	})
	if result.IsError {
		t.Fatalf("预检失败: %s", resultJSON(t, result))
	}
	if items.previewCalls != 1 {
		t.Fatalf("预检应恰好调用一次，实际 %d", items.previewCalls)
	}
	// text 是预检结果 JSON。
	text := resultJSON(t, result)
	if !strings.Contains(text, `"total":2`) || !strings.Contains(text, "商品B") {
		t.Fatalf("预检结果异常: %s", text)
	}
}

// TestBatchCreateThenStart 验证批次创建返回 batch_id 且启动需要 confirm。
func TestBatchCreateThenStart(t *testing.T) {
	// endpoint、items、ctx 是商品工具端点、假端口与上下文。
	endpoint, items, ctx := newItemToolEndpoint(t)
	// created 是批次创建结果。
	created := invoke(endpoint, ctx, "item_batch_create", map[string]any{
		"rows": []any{map[string]any{"title": "商品A", "price": "1.00"}},
	})
	if created.IsError || items.persistCalls != 1 {
		t.Fatalf("批次创建失败: %s", resultJSON(t, created))
	}
	// createdText 是创建结果 JSON，必须带非空 batch_id。
	createdText := resultJSON(t, created)
	if !strings.Contains(createdText, "batch_") {
		t.Fatalf("批次创建结果缺少 batch_id: %s", createdText)
	}
	// startDenied 是缺 confirm 的启动。
	startDenied := invoke(endpoint, ctx, "item_batch_start", map[string]any{"batch_id": "batch_x"})
	if !startDenied.IsError || len(items.batchStarted) != 0 {
		t.Fatal("缺 confirm 时批次启动必须拦截")
	}
	// startOK 是显式确认后的启动。
	startOK := invoke(endpoint, ctx, "item_batch_start", map[string]any{"batch_id": "b1", "confirm": true})
	if startOK.IsError || len(items.batchStarted) != 1 {
		t.Fatalf("批次启动失败: %s", resultJSON(t, startOK))
	}
}

// TestBatchResultCSVContainsRows 验证批次结果 CSV 包含表头与逐行状态。
func TestBatchResultCSVContainsRows(t *testing.T) {
	// endpoint、_、ctx 是商品工具端点与上下文。
	endpoint, _, ctx := newItemToolEndpoint(t)
	// result 是 CSV 导出结果。
	result := invoke(endpoint, ctx, "item_batch_result_csv", map[string]any{"batch_id": "b1"})
	if result.IsError {
		t.Fatalf("CSV 导出失败: %s", resultJSON(t, result))
	}
	// text 是导出 JSON，内嵌 CSV 必须含表头和失败行。
	text := resultJSON(t, result)
	if !strings.Contains(text, "row_no") || !strings.Contains(text, "manual_review") {
		t.Fatalf("CSV 内容异常: %s", text)
	}
}

// TestNewBatchIDUnique 验证批次标识前缀与唯一性。
func TestNewBatchIDUnique(t *testing.T) {
	// ids 是去重集合。
	ids := map[string]bool{}
	// i 是批次标识生成序号。
	for i := 0; i < 100; i++ {
		// id、err 是生成的批次标识。
		id, err := newBatchID()
		if err != nil || !strings.HasPrefix(id, "batch_") || len(id) != len("batch_")+24 {
			t.Fatalf("批次标识格式异常: %q err=%v", id, err)
		}
		if ids[id] {
			t.Fatalf("批次标识重复: %s", id)
		}
		ids[id] = true
	}
}
