// tools_orders_test.go 覆盖订单/分析/异常域工具的映射、分页、confirm 守卫与凭证剔除。

package mcp

import (
	"context"
	"strings"
	"testing"

	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	orderapp "xianyu-go/internal/application/orders"
)

// fakeOrderPorts 是订单域假端口，记录平台动作调用。
type fakeOrderPorts struct {
	// shipModes 记录手动发货的模式与订单。
	shipModes []shipCall
	// refreshSingles 记录单订单刷新的订单号。
	refreshSingles []string
	// createdJobs 记录创建的刷新任务。
	createdJobs [][2]string
	// listUser 记录列表查询携带的用户标识。
	listUser int64
}

// shipCall 是一次手动发货调用记录。
type shipCall struct {
	// mode 是发货模式。
	mode string
	// ids 是订单标识数组。
	ids []string
}

// List 返回一条固定订单行。
func (f *fakeOrderPorts) List(_ context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	f.listUser = query.UserID
	return orderapp.ListResult{
		Total: 1, Page: query.Page, PageSize: query.PageSize, TotalPages: 1,
		Rows: []orderapp.OrderRow{{OrderID: "o1", ItemID: "i1", ItemTitle: "商品A", OrderStatus: "paid",
			CookieID: "a1", Amount: "9.90", Quantity: "1", SystemShipped: true}},
	}, nil
}

// Get 返回含收货信息的订单实体。
func (f *fakeOrderPorts) Get(context.Context, int64, string) (*orderapp.Order, error) {
	return &orderapp.Order{OrderID: "o1", ItemID: "i1", OrderStatus: "paid", CookieID: "a1",
		ReceiverName: "张三", ReceiverPhone: "13800000000", ReceiverAddress: "测试地址", Version: 3}, nil
}

// RefreshSingle 返回带 UpdatedCookies 的刷新结果，用于验证凭证不泄漏。
func (f *fakeOrderPorts) RefreshSingle(_ context.Context, _ int64, orderID string) (orderapp.SingleRefreshResult, error) {
	f.refreshSingles = append(f.refreshSingles, orderID)
	return orderapp.SingleRefreshResult{Success: true, Message: "ok",
		Detail: orderapp.RefreshDetail{OrderStatus: "shipped", Amount: "19.90", UpdatedCookies: "SECRET_COOKIE=1"}}, nil
}

// Refresh 返回成功批量刷新汇总。
func (f *fakeOrderPorts) Refresh(context.Context, int64, string, string) (orderapp.RefreshResult, error) {
	return orderapp.RefreshResult{Message: "done", Summary: orderapp.RefreshSummary{Discovered: 2, ListUpdated: 2}}, nil
}

// ManualShip 记录发货调用并返回一成功一失败。
func (f *fakeOrderPorts) ManualShip(_ context.Context, request orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
	f.shipModes = append(f.shipModes, shipCall{mode: request.ShipMode, ids: request.OrderIDs})
	return orderapp.ManualShipResult{SuccessCount: 1, FailedCount: 1, Results: []orderapp.ManualShipItemResult{
		{OrderID: request.OrderIDs[0], Status: "succeeded", Success: true, Message: "已发货"},
		{OrderID: request.OrderIDs[1], Status: "failed", Success: false, Message: "需人工核对"},
	}}, nil
}

// CreateRefreshJob 记录任务创建并返回任务快照。
func (f *fakeOrderPorts) CreateRefreshJob(_ context.Context, _ int64, cookieID, status string) (orderapp.RefreshJobStartResult, error) {
	f.createdJobs = append(f.createdJobs, [2]string{cookieID, status})
	return orderapp.RefreshJobStartResult{Job: &orderapp.RefreshJob{ID: "j1", CookieID: cookieID, Status: "queued", FilterStatus: status}}, nil
}

// GetRefreshJob 返回固定任务快照。
func (f *fakeOrderPorts) GetRefreshJob(context.Context, int64, string) (*orderapp.RefreshJob, error) {
	return &orderapp.RefreshJob{ID: "j1", Status: "running"}, nil
}

// CancelRefreshJob 返回取消成功结果。
func (f *fakeOrderPorts) CancelRefreshJob(context.Context, int64, string) (orderapp.RefreshJobCancelResult, error) {
	return orderapp.RefreshJobCancelResult{Job: &orderapp.RefreshJob{ID: "j1", Status: "cancelled"}, Cancelled: true}, nil
}

// fakeAnalyticsPorts 是分析域假端口。
type fakeAnalyticsPorts struct{}

// DashboardStats 返回固定计数。
func (fakeAnalyticsPorts) DashboardStats(context.Context, int64) (analyticsapp.DashboardStats, error) {
	return analyticsapp.DashboardStats{TotalOrders: 5, TotalCookies: 2, ActiveCookies: 1, TotalCards: 3}, nil
}

// OrderAnalytics 返回固定聚合。
func (fakeAnalyticsPorts) OrderAnalytics(context.Context, analyticsapp.Query) (analyticsapp.OrderAnalytics, error) {
	return analyticsapp.OrderAnalytics{
		RevenueStats: analyticsapp.RevenueStats{TotalOrders: 5, TotalAmount: 49.5},
		DailyStats:   []analyticsapp.DailyStats{{Date: "2026-10-01", OrderCount: 5, Amount: 49.5}},
	}, nil
}

// ValidOrders 返回单条有效订单。
func (fakeAnalyticsPorts) ValidOrders(context.Context, analyticsapp.Query, int, int) (analyticsapp.ValidOrders, error) {
	return analyticsapp.ValidOrders{Total: 1, Orders: []analyticsapp.ValidOrder{{OrderID: "o1", Amount: "9.9"}}, Page: 1, PageSize: 20}, nil
}

// fakeIssuePorts 记录异常处理调用。
type fakeIssuePorts struct {
	// runResolutions 记录运行异常处理的 runID 与决议。
	runResolutions map[int64]string
	// deferredResolutions 记录死信处理的 taskID 与决议。
	deferredResolutions map[int64]string
}

// ListIssues 返回一条运行异常与一条死信。
func (*fakeIssuePorts) ListIssues(context.Context, int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error) {
	return []automationapp.RunIssue{{ID: 9, CookieID: "a1", OrderID: "o1", IssueKind: "manual_review"}},
		[]automationapp.DeferredIssue{{ID: 3, CookieID: "a1", AttemptCount: 5}}, nil
}

// ResolveRunIssue 记录运行异常决议。
func (f *fakeIssuePorts) ResolveRunIssue(_ context.Context, _ int64, runID int64, resolution string) error {
	f.runResolutions[runID] = resolution
	return nil
}

// ResolveDeferredIssue 记录死信决议。
func (f *fakeIssuePorts) ResolveDeferredIssue(_ context.Context, _ int64, taskID int64, resolution string) error {
	f.deferredResolutions[taskID] = resolution
	return nil
}

// newOrderToolEndpoint 构造注册了订单/分析/异常工具的端点。
func newOrderToolEndpoint(t *testing.T) (*Endpoint, *fakeOrderPorts, *fakeIssuePorts, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// orders 是订单域假端口。
	orders := &fakeOrderPorts{}
	// issues 是异常域假端口。
	issues := &fakeIssuePorts{runResolutions: map[int64]string{}, deferredResolutions: map[int64]string{}}
	endpoint.RegisterOrderTools(orders, fakeAnalyticsPorts{}, issues)
	// ctx 是携带管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, orders, issues, ctx
}

// TestOrderListPagination 验证订单列表分页参数透传与 DTO 映射。
func TestOrderListPagination(t *testing.T) {
	// endpoint、orders、_、ctx 是订单工具端点与上下文。
	endpoint, orders, _, ctx := newOrderToolEndpoint(t)
	// result 是第二页请求。
	result := invoke(endpoint, ctx, "order_list", map[string]any{"page": 1.0, "page_size": 20.0, "status": "paid"})
	if result.IsError {
		t.Fatalf("订单列表失败: %s", resultJSON(t, result))
	}
	// text 是列表 JSON。
	text := resultJSON(t, result)
	if !strings.Contains(text, `"total":1`) || !strings.Contains(text, `"page":1`) {
		t.Fatalf("分页结果异常: %s", text)
	}
	if orders.listUser != 1 {
		t.Fatalf("列表查询必须透传管理员用户 1，实际 %d", orders.listUser)
	}
}

// TestManualShipRequiresConfirmAndMode 验证手动发货确认守卫、模式透传与结果映射。
func TestManualShipRequiresConfirmAndMode(t *testing.T) {
	// endpoint、orders、_、ctx 是订单工具端点与上下文。
	endpoint, orders, _, ctx := newOrderToolEndpoint(t)
	// denied 是缺 confirm 的发货调用。
	denied := invoke(endpoint, ctx, "order_manual_ship", map[string]any{
		"order_ids": []any{"o1", "o2"}, "ship_mode": "full_delivery",
	})
	if !denied.IsError || len(orders.shipModes) != 0 {
		t.Fatal("缺 confirm 时手动发货必须被拦截且零触达")
	}
	// allowed 是显式确认后的发货调用。
	allowed := invoke(endpoint, ctx, "order_manual_ship", map[string]any{
		"order_ids": []any{"o1", "o2"}, "ship_mode": "full_delivery", "confirm": true,
	})
	if allowed.IsError {
		t.Fatalf("带 confirm 发货应成功: %s", resultJSON(t, allowed))
	}
	// text 是发货结果 JSON。
	text := resultJSON(t, allowed)
	if !strings.Contains(text, `"success_count":1`) || !strings.Contains(text, `"failed_count":1`) {
		t.Fatalf("发货汇总异常: %s", text)
	}
	if len(orders.shipModes) != 1 || orders.shipModes[0].mode != "full_delivery" {
		t.Fatalf("发货模式透传异常: %+v", orders.shipModes)
	}
}

// TestRefreshSingleDoesNotLeakCookies 验证刷新详情 DTO 不携带 UpdatedCookies 凭证明文。
func TestRefreshSingleDoesNotLeakCookies(t *testing.T) {
	// endpoint、_、_、ctx 是订单工具端点与上下文。
	endpoint, _, _, ctx := newOrderToolEndpoint(t)
	// result 是单订单刷新结果。
	result := invoke(endpoint, ctx, "order_refresh_single", map[string]any{"order_id": "o1", "confirm": true})
	if result.IsError {
		t.Fatalf("刷新调用失败: %s", resultJSON(t, result))
	}
	// text 是结果 JSON，绝不能出现平台 Cookie。
	text := resultJSON(t, result)
	if strings.Contains(strings.ToLower(text), "cookie") || strings.Contains(text, "SECret") {
		t.Fatalf("刷新结果疑似泄漏凭证: %s", text)
	}
}

// TestIssueResolvePropagation 验证异常处理工具的 confirm 与决议透传。
func TestIssueResolvePropagation(t *testing.T) {
	// endpoint、_、issues、ctx 是订单工具端点、异常假端口与上下文。
	endpoint, _, issues, ctx := newOrderToolEndpoint(t)
	// denied 是缺 confirm 的死信处理调用。
	denied := invoke(endpoint, ctx, "issue_resolve_deferred", map[string]any{"task_id": 3.0, "resolution": "retry"})
	if !denied.IsError {
		t.Fatal("死信处理缺 confirm 必须拦截")
	}
	// allowed 是显式确认后的 retry 调用。
	allowed := invoke(endpoint, ctx, "issue_resolve_deferred", map[string]any{
		"task_id": 3.0, "resolution": "retry", "confirm": true,
	})
	if allowed.IsError || issues.deferredResolutions[3] != "retry" {
		t.Fatalf("死信 retry 决议必须透传: %s", resultJSON(t, allowed))
	}
	// listResult 是异常列表，应包含运行与死信两条。
	listResult := invoke(endpoint, ctx, "issue_list", nil)
	// text 是异常列表 JSON。
	text := resultJSON(t, listResult)
	if !strings.Contains(text, "manual_review") || !strings.Contains(text, `"attempt_count":5`) {
		t.Fatalf("异常列表内容异常: %s", text)
	}
}

// TestAnalyticsDashboardMapping 验证仪表盘计数 DTO 映射。
func TestAnalyticsDashboardMapping(t *testing.T) {
	// endpoint、_、_、ctx 是订单工具端点与上下文。
	endpoint, _, _, ctx := newOrderToolEndpoint(t)
	// result 是仪表盘结果。
	result := invoke(endpoint, ctx, "analytics_dashboard", nil)
	if result.IsError {
		t.Fatalf("仪表盘查询失败: %s", resultJSON(t, result))
	}
	// text 是仪表盘 JSON。
	text := resultJSON(t, result)
	if !strings.Contains(text, `"total_orders":5`) || !strings.Contains(text, `"available_card_stock":0`) {
		t.Fatalf("仪表盘映射异常: %s", text)
	}
}
