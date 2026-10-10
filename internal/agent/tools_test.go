// tools_test.go 覆盖客服 Agent 的能力工具层：能力下发范围、作用域断言与派发。
//
// 三条必须成立的性质：
//  1. 只有 Decision 为 allow 的能力会出现在模型可见的工具声明里；
//  2. 模型填写的 account_id 不被信任，跨账号调用一律拒绝且不触达用例端口；
//  3. 缺参、未登记能力与端口缺失都返回错误，不静默成功。

package agent

import (
	"context"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	orderapp "xianyu-go/internal/application/orders"

	"xianyu-go/internal/capability"
)

// fakeOrderPorts 是订单用例端口替身，记录调用入参并返回预设结果。
type fakeOrderPorts struct {
	// listQueries 记录 List 收到的查询条件。
	listQueries []orderapp.ListQuery
	// getCalls 记录 Get 收到的订单标识。
	getCalls []string
	// listResult 是 List 返回的分页结果。
	listResult orderapp.ListResult
	// getResult 是 Get 返回的订单详情。
	getResult *orderapp.Order
	// listErr 是 List 预设的失败原因。
	listErr error
	// getErr 是 Get 预设的失败原因。
	getErr error
}

// List 记录查询条件并返回预设结果。
func (f *fakeOrderPorts) List(_ context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	f.listQueries = append(f.listQueries, query)
	return f.listResult, f.listErr
}

// Get 记录订单标识并返回预设结果。
func (f *fakeOrderPorts) Get(_ context.Context, _ int64, orderID string) (*orderapp.Order, error) {
	f.getCalls = append(f.getCalls, orderID)
	return f.getResult, f.getErr
}

// RefreshSingle 是本替身未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeOrderPorts) RefreshSingle(context.Context, int64, string) (orderapp.SingleRefreshResult, error) {
	panic("工具层不应触达订单刷新用例")
}

// Refresh 是本替身未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeOrderPorts) Refresh(context.Context, int64, string, string) (orderapp.RefreshResult, error) {
	panic("工具层不应触达订单刷新用例")
}

// ManualShip 是本替身未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeOrderPorts) ManualShip(context.Context, orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
	panic("工具层不应触达手动发货用例")
}

// CreateRefreshJob 是本替身未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeOrderPorts) CreateRefreshJob(context.Context, int64, string, string) (orderapp.RefreshJobStartResult, error) {
	panic("工具层不应触达刷新任务用例")
}

// GetRefreshJob 是本替身未覆盖的读取用例，被调用即视为测试失败。
func (f *fakeOrderPorts) GetRefreshJob(context.Context, int64, string) (*orderapp.RefreshJob, error) {
	panic("工具层不应触达刷新任务用例")
}

// CancelRefreshJob 是本替身未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeOrderPorts) CancelRefreshJob(context.Context, int64, string) (orderapp.RefreshJobCancelResult, error) {
	panic("工具层不应触达刷新任务用例")
}

// fakeItemPorts 是商品用例端口替身，记录调用入参并返回预设结果。
type fakeItemPorts struct {
	// listCookies 记录 ListItems 收到的账号标识。
	listCookies []string
	// getCookies 记录 GetItem 收到的账号标识。
	getCookies []string
	// getItemIDs 记录 GetItem 收到的商品标识。
	getItemIDs []string
	// items 是 ListItems 返回的货架商品。
	items []itemapp.CatalogItem
	// item 是 GetItem 返回的单个商品。
	item itemapp.CatalogItem
	// listErr 是 ListItems 预设的失败原因。
	listErr error
}

// ListItems 记录账号标识并返回预设货架。
func (f *fakeItemPorts) ListItems(_ context.Context, _ int64, cookieID string) ([]itemapp.CatalogItem, error) {
	f.listCookies = append(f.listCookies, cookieID)
	return f.items, f.listErr
}

// GetItem 记录账号与商品标识并返回预设商品。
func (f *fakeItemPorts) GetItem(_ context.Context, cookieID, itemID string) (itemapp.CatalogItem, error) {
	f.getCookies = append(f.getCookies, cookieID)
	f.getItemIDs = append(f.getItemIDs, itemID)
	return f.item, nil
}

// CreateItem 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) CreateItem(context.Context, string, itemapp.CatalogWriteInput) error {
	panic("工具层不应触达商品写入用例")
}

// UpdateItem 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) UpdateItem(context.Context, string, string, itemapp.CatalogPatchInput) error {
	panic("工具层不应触达商品写入用例")
}

// DeleteItem 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) DeleteItem(context.Context, string, string) error {
	panic("工具层不应触达商品写入用例")
}

// SetItemMultiSpec 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) SetItemMultiSpec(context.Context, string, string, bool) error {
	panic("工具层不应触达商品写入用例")
}

// SetItemMultiQuantity 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) SetItemMultiQuantity(context.Context, string, string, bool) error {
	panic("工具层不应触达商品写入用例")
}

// SyncItemsAll 是未覆盖的同步用例，被调用即视为测试失败。
func (f *fakeItemPorts) SyncItemsAll(context.Context, itemapp.SyncQuery) (itemapp.SyncAllResult, error) {
	panic("工具层不应触达商品同步用例")
}

// SyncItemsPage 是未覆盖的同步用例，被调用即视为测试失败。
func (f *fakeItemPorts) SyncItemsPage(context.Context, itemapp.SyncQuery) (itemapp.SyncPageResult, error) {
	panic("工具层不应触达商品同步用例")
}

// PublishSingle 是未覆盖的发布用例，被调用即视为测试失败。
func (f *fakeItemPorts) PublishSingle(context.Context, capability.SinglePublishInput) (itemapp.PublishOutcome, error) {
	panic("工具层不应触达商品发布用例")
}

// RecommendCategory 是未覆盖的读取用例，被调用即视为测试失败。
func (f *fakeItemPorts) RecommendCategory(context.Context, int64, string, string) (itemapp.BatchPreviewCategory, error) {
	panic("工具层不应触达类目推荐用例")
}

// PreviewBatch 是未覆盖的预检用例，被调用即视为测试失败。
func (f *fakeItemPorts) PreviewBatch(context.Context, itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error) {
	panic("工具层不应触达批量预检用例")
}

// PersistBatch 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) PersistBatch(context.Context, itemapp.BatchPreviewPersistenceBatch, []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error) {
	panic("工具层不应触达批量写入用例")
}

// StartBatch 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) StartBatch(context.Context, int64, string) (string, error) {
	panic("工具层不应触达批量发布用例")
}

// ListBatches 是未覆盖的读取用例，被调用即视为测试失败。
func (f *fakeItemPorts) ListBatches(context.Context, int64, int) ([]itemapp.BatchInfo, error) {
	panic("工具层不应触达批量列表用例")
}

// GetBatch 是未覆盖的读取用例，被调用即视为测试失败。
func (f *fakeItemPorts) GetBatch(context.Context, int64, string) (itemapp.BatchDetails, error) {
	panic("工具层不应触达批量详情用例")
}

// CancelBatch 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) CancelBatch(context.Context, int64, string) (string, error) {
	panic("工具层不应触达批量取消用例")
}

// RetryBatch 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) RetryBatch(context.Context, int64, string) (string, error) {
	panic("工具层不应触达批量重试用例")
}

// DeleteBatch 是未覆盖的写入用例，被调用即视为测试失败。
func (f *fakeItemPorts) DeleteBatch(context.Context, int64, string) error {
	panic("工具层不应触达批量删除用例")
}

// newTestTools 用平台能力目录与两个替身端口构造工具集。
func newTestTools(t *testing.T) (*Tools, *fakeOrderPorts, *fakeItemPorts) {
	t.Helper()
	// catalog、err 分别是平台能力目录及其构造失败原因。
	catalog, err := capability.PlatformCatalog()
	if err != nil {
		t.Fatalf("构造平台能力目录失败: %v", err)
	}
	// orders 是订单端口替身。
	orders := &fakeOrderPorts{}
	// items 是商品端口替身。
	items := &fakeItemPorts{}
	return NewTools(catalog, orders, items), orders, items
}

// sessionFor 构造指定账号与档位的会话。
func sessionFor(cookieID string, preset capability.Preset) Session {
	return Session{UserID: 42, CookieID: cookieID, Preset: preset}
}

// TestToolsSchemasExposeOnlySupportAvailableCapabilities 验证只读档下只有账号内只读能力被下发，
// 运营汇总能力与未登记声明的能力都不出现在模型可见的工具声明里。
func TestToolsSchemasExposeOnlySupportAvailableCapabilities(t *testing.T) {
	// tools 是待测试的工具集。
	tools, _, _ := newTestTools(t)
	// got 是只读档下被放行的工具声明。
	got := tools.Schemas(context.Background(), sessionFor("cid-own", capability.PresetReadonly))
	// names 是实际下发的工具名集合。
	names := make([]string, 0, len(got))
	// schema 是当前遍历到的工具声明。
	for _, schema := range got {
		names = append(names, schema.Name)
	}
	// want 是本期只读档必须下发的四个能力，按目录字典序。
	want := []string{capability.CapItemGet, capability.CapItemList, capability.CapOrderGet, capability.CapOrderList}
	if len(names) != len(want) {
		t.Fatalf("下发工具数量不符：got=%v want=%v", names, want)
	}
	// index、name 分别是下标与期望工具名。
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("第 %d 个工具不符：got=%q want=%q", index, names[index], name)
		}
	}
}

// TestToolsSchemasHideCapabilityWhenPresetInsufficient 验证档位不足时能力不下发给模型。
func TestToolsSchemasHideCapabilityWhenPresetInsufficient(t *testing.T) {
	// catalog、err 分别是把订单列表要求提到 standard 档的测试目录及其构造失败原因。
	catalog, err := capability.NewCatalog(capability.Spec{
		Name: capability.CapOrderList, Risk: capability.RiskRead,
		Scope: capability.ScopeAccount, MinPreset: capability.PresetStandard,
	})
	if err != nil {
		t.Fatalf("构造测试目录失败: %v", err)
	}
	// tools 是基于测试目录的工具集。
	tools := NewTools(catalog, &fakeOrderPorts{}, &fakeItemPorts{})
	if // readonlyTools 是只读档下被放行的工具声明。
	readonlyTools := tools.Schemas(context.Background(), sessionFor("cid-own", capability.PresetReadonly)); len(readonlyTools) != 0 {
		t.Fatalf("只读档不应下发 standard 档能力: %+v", readonlyTools)
	}
	if // standardTools 是 standard 档下被放行的工具声明。
	standardTools := tools.Schemas(context.Background(), sessionFor("cid-own", capability.PresetStandard)); len(standardTools) != 1 {
		t.Fatalf("standard 档应下发该能力: %+v", standardTools)
	}
}

// TestToolsCallRejectsCrossAccountRequest 验证模型给出的异账号参数被拒绝且不触达用例端口。
func TestToolsCallRejectsCrossAccountRequest(t *testing.T) {
	// tools、orders、items 分别是待测试工具集与两个端口替身。
	tools, orders, items := newTestTools(t)
	// ctx 是本次调用上下文。
	ctx := context.Background()
	// session 是账号 cid-own 的只读会话。
	session := sessionFor("cid-own", capability.PresetReadonly)

	// _, err 分别是跨账号调用返回的结果与失败原因。
	if _, err := tools.Call(ctx, session, ToolCall{
		ID: "c1", Name: capability.CapOrderList, Args: map[string]any{"account_id": "cid-other"},
	}); err == nil {
		t.Fatal("异账号参数必须被拒绝")
	}
	// _, err 复用以验证异账号商品读取同样被拒。
	if _, err := tools.Call(ctx, session, ToolCall{
		ID: "c2", Name: capability.CapItemGet, Args: map[string]any{"account_id": "cid-other", "item_id": "i1"},
	}); err == nil {
		t.Fatal("异账号商品读取必须被拒绝")
	}
	if len(orders.listQueries) != 0 || len(items.getCookies) != 0 {
		t.Fatalf("被拒调用不得触达用例端口: orders=%+v items=%+v", orders.listQueries, items.getCookies)
	}
}

// TestToolsCallUsesSessionScopeInsteadOfModelArgs 验证未提供账号时使用会话账号，
// 且查询条件里的归属与账号都来自会话而非模型。
func TestToolsCallUsesSessionScopeInsteadOfModelArgs(t *testing.T) {
	// tools、orders 分别是待测试工具集与订单端口替身。
	tools, orders, _ := newTestTools(t)
	// session 是账号 cid-own 的只读会话。
	session := sessionFor("cid-own", capability.PresetReadonly)

	// _, err 分别是订单列表调用结果与失败原因。
	if _, err := tools.Call(context.Background(), session, ToolCall{
		ID: "c1", Name: capability.CapOrderList,
		Args: map[string]any{"status": "paid", "page": float64(2), "page_size": float64(999)},
	}); err != nil {
		t.Fatalf("只读档的订单列表调用不应失败: %v", err)
	}
	if len(orders.listQueries) != 1 {
		t.Fatalf("订单列表应被调用一次: %+v", orders.listQueries)
	}
	// query 是端口实际收到的查询条件。
	query := orders.listQueries[0]
	if query.UserID != 42 || query.CookieID != "cid-own" {
		t.Fatalf("归属与账号必须来自会话: %+v", query)
	}
	if query.Status != "paid" || query.Page != 2 || query.PageSize != maxOrderPageSize {
		t.Fatalf("过滤与分页归一不符: %+v", query)
	}
}

// TestToolsCallRejectsUngrantedAndUnknownCapabilities 验证未放行与未登记的能力都被拒绝。
func TestToolsCallRejectsUngrantedAndUnknownCapabilities(t *testing.T) {
	// tools、orders 分别是待测试工具集与订单端口替身。
	tools, orders, _ := newTestTools(t)
	// session 是账号 cid-own 的只读会话。
	session := sessionFor("cid-own", capability.PresetReadonly)
	// name 是当前待验证的不可调用能力名。
	for _, name := range []string{capability.CapOpsSalesSnapshot, "not_registered"} {
		if // err 是该能力被拒绝时返回的失败原因。
		_, err := tools.Call(context.Background(), session, ToolCall{ID: "c1", Name: name}); err == nil {
			t.Fatalf("能力 %s 不应被客服 Agent 调用", name)
		}
	}
	if len(orders.listQueries) != 0 {
		t.Fatal("被拒调用不得触达用例端口")
	}
}

// TestToolsCallDispatchesReadOnlyCapabilities 验证四个只读能力各自派发到正确的用例与入参。
func TestToolsCallDispatchesReadOnlyCapabilities(t *testing.T) {
	// tools、orders、items 分别是待测试工具集与两个端口替身。
	tools, orders, items := newTestTools(t)
	// session 是账号 cid-own 的只读会话。
	session := sessionFor("cid-own", capability.PresetReadonly)
	// ctx 是本次调用上下文。
	ctx := context.Background()
	orders.getResult = &orderapp.Order{OrderID: "o1"}
	items.item = itemapp.CatalogItem{ItemID: "i1"}

	// _, err 分别是订单详情调用结果与失败原因。
	if _, err := tools.Call(ctx, session, ToolCall{ID: "c1", Name: capability.CapOrderGet, Args: map[string]any{"order_id": "o1"}}); err != nil {
		t.Fatalf("订单详情调用不应失败: %v", err)
	}
	// _, err 复用以验证商品列表调用。
	if _, err := tools.Call(ctx, session, ToolCall{ID: "c2", Name: capability.CapItemList}); err != nil {
		t.Fatalf("商品列表调用不应失败: %v", err)
	}
	// _, err 复用以验证商品详情调用。
	if _, err := tools.Call(ctx, session, ToolCall{ID: "c3", Name: capability.CapItemGet, Args: map[string]any{"item_id": "i1"}}); err != nil {
		t.Fatalf("商品详情调用不应失败: %v", err)
	}
	if len(orders.getCalls) != 1 || orders.getCalls[0] != "o1" {
		t.Fatalf("订单详情入参不符: %+v", orders.getCalls)
	}
	if len(items.listCookies) != 1 || items.listCookies[0] != "cid-own" {
		t.Fatalf("商品列表账号不符: %+v", items.listCookies)
	}
	if len(items.getCookies) != 1 || items.getCookies[0] != "cid-own" || items.getItemIDs[0] != "i1" {
		t.Fatalf("商品详情入参不符: cookies=%+v ids=%+v", items.getCookies, items.getItemIDs)
	}
}

// TestToolsCallRejectsMissingRequiredArgument 验证必填入参缺失时返回错误而不是拿空标识去查询。
func TestToolsCallRejectsMissingRequiredArgument(t *testing.T) {
	// tools、orders 分别是待测试工具集与订单端口替身。
	tools, orders, _ := newTestTools(t)
	// session 是账号 cid-own 的只读会话。
	session := sessionFor("cid-own", capability.PresetReadonly)
	// call 是两种缺参调用的构造方式。
	calls := []ToolCall{
		{ID: "c1", Name: capability.CapOrderGet},
		{ID: "c2", Name: capability.CapItemGet, Args: map[string]any{"item_id": "  "}},
	}
	// call 是当前待验证的缺参调用。
	for _, call := range calls {
		if // err 是缺参调用被拒绝时返回的失败原因。
		_, err := tools.Call(context.Background(), session, call); err == nil {
			t.Fatalf("能力 %s 缺参必须报错", call.Name)
		}
	}
	if len(orders.getCalls) != 0 {
		t.Fatal("缺参调用不得触达用例端口")
	}
}

// TestToolsCallTruncatesOversizedResult 验证超大工具结果被截断，避免上下文被单条结果挤爆。
func TestToolsCallTruncatesOversizedResult(t *testing.T) {
	// tools、_、items 分别是待测试工具集与商品端口替身。
	tools, _, items := newTestTools(t)
	items.items = []itemapp.CatalogItem{{ItemDescription: strings.Repeat("长", maxToolResultRunes+100)}}

	// result、err 分别是商品列表调用结果与失败原因。
	result, err := tools.Call(context.Background(), sessionFor("cid-own", capability.PresetReadonly),
		ToolCall{ID: "c1", Name: capability.CapItemList})
	if err != nil {
		t.Fatalf("商品列表调用不应失败: %v", err)
	}
	if !strings.HasSuffix(result, "（结果已截断）") {
		t.Fatalf("超大结果必须被截断并标注: 长度=%d", len([]rune(result)))
	}
}
