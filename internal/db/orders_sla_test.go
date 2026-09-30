package db

import (
	"context"
	"testing"
)

// TestPendingShipPaidOrdersAfterCoversOrdersWithRuns 验证 SLA 扫描覆盖「已有运行但仍未发货」
// 的订单（旧兜底查询会漏掉），并正确过滤未付款、已发货与游标边界。
func TestPendingShipPaidOrdersAfterCoversOrdersWithRuns(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	// insertOrder 写入订单并按需设置付款时间。
	insertOrder := func(orderID, status, paidAt string) {
		t.Helper()
		// upsertErr 保存订单写入错误。
		upsertErr := s.Orders.Upsert(ctx, orderID, OrderUpsertOpts{
			ItemID: "item-1", BuyerID: "buyer-" + orderID, CookieID: cookieID, ChatID: "chat-" + orderID,
			OrderStatus: status, Quantity: "1", Amount: "9.90",
		})
		if upsertErr != nil {
			t.Fatalf("写入订单 %s 失败: %v", orderID, upsertErr)
		}
		if paidAt == "" {
			return
		}
		// updateErr 保存付款时间补写错误。
		_, updateErr := s.DB.ExecContext(ctx, `UPDATE orders SET paid_at=? WHERE order_id=?`, paidAt, orderID)
		if updateErr != nil {
			t.Fatalf("写入付款时间 %s 失败: %v", orderID, updateErr)
		}
	}
	insertOrder("o-1", "pending_ship", "2026-09-30T01:00:00Z")
	insertOrder("o-2", "pending_ship", "2026-09-30T02:00:00Z")
	insertOrder("o-3", "pending_ship", "")
	insertOrder("o-4", "shipped", "2026-09-30T03:00:00Z")
	// seedCatchupRule 给 o-1 写入启用规则，并补一条 order_paid 运行，模拟「已有运行仍未发货」。
	seedCatchupRule(t, s, userID, cookieID, "item-1", "发货规则", true,
		[]AutomationActionInput{{ActionType: "send_card", MessageTemplate: "card", Enabled: true}})
	// runInsertErr 保存模拟运行写入错误。
	_, runInsertErr := s.DB.ExecContext(ctx,
		`INSERT INTO automation_runs(rule_id,order_id,cookie_id,trigger_type,trigger_key,status,created_at,updated_at)
		 VALUES((SELECT id FROM automation_rules LIMIT 1),'o-1',?,'order_paid','o-1:order_paid','succeeded',datetime('now'),datetime('now'))`,
		cookieID)
	if runInsertErr != nil {
		t.Fatalf("写入模拟运行失败: %v", runInsertErr)
	}
	// all 保存整页扫描结果，应包含 o-1（有运行）与 o-2，排除未付款与已发货。
	all, err := s.Automation.PendingShipPaidOrdersAfter(ctx, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	// gotIDs 是扫描命中的订单号集合。
	gotIDs := map[string]bool{}
	// ord 是本轮校验的订单事实；SLA 依赖订单号、账号与付款/发货时间。
	for _, ord := range all {
		gotIDs[ord.OrderID] = true
		if ord.CookieID != cookieID || ord.PaidAt == "" || ord.ShippedAt != "" {
			t.Fatalf("SLA 事实列回填异常: %+v", ord)
		}
	}
	if !gotIDs["o-1"] || !gotIDs["o-2"] || gotIDs["o-3"] || gotIDs["o-4"] {
		t.Fatalf("SLA 扫描结果异常: %v", gotIDs)
	}
	// page 保存游标分页的第一页（上限 1）。
	page, err := s.Automation.PendingShipPaidOrdersAfter(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].OrderID != "o-1" {
		t.Fatalf("游标首页异常: %+v", page)
	}
	// next 保存游标后的下一页，应只含 o-2。
	next, err := s.Automation.PendingShipPaidOrdersAfter(ctx, "o-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || next[0].OrderID != "o-2" {
		t.Fatalf("游标翻页异常: %+v", next)
	}
	// empty 保存越过末尾后的空页。
	empty, err := s.Automation.PendingShipPaidOrdersAfter(ctx, "o-9", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("越过末尾应为空: %+v", empty)
	}
}
