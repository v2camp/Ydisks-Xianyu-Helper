// orders_fulfillment_test.go 锁定按会话查履约订单的行为，两种方言都执行。
//
// 该查询是 AI 回答「付款了没收到 / 链接失效」的事实入口，
// 与 FindLatestOpenByChat 的关键差别是**包含已发货与已完结订单**：
// 只查未终结状态会把「已经发过」误判成「查不到订单」，进而产生错误承诺。
package db

import (
	"context"
	"testing"
)

// TestMultiDB_FindLatestForFulfillment 覆盖取最新一笔、跨商品隔离、含已完结订单与无订单四种情形。
func TestMultiDB_FindLatestForFulfillment(t *testing.T) {
	// target 是独立迁移后的方言数据库，清理由该目标子测试负责。
	for _, target := range allTestTargets(t) {
		// t 接收当前数据库的断言。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 只用于当前测试的本地仓储调用。
			ctx := context.Background()
			// userID、cookieID 是订单与卡券所属的测试账号。
			_, cookieID := seedAccount(t, target.store)
			// seeds 是三类订单：较早的已完结单、较新的待发货单、其他商品单。
			seeds := []struct {
				// orderID 是订单业务标识。
				orderID string
				// opts 是订单写入字段。
				opts OrderUpsertOpts
			}{
				{orderID: "old-completed", opts: OrderUpsertOpts{CookieID: cookieID, ChatID: "chat-1", ItemID: "item-1",
					CreatedAt: "2024-01-01 00:00:00", OrderStatus: "completed"}},
				{orderID: "new-pending", opts: OrderUpsertOpts{CookieID: cookieID, ChatID: "chat-1", ItemID: "item-1",
					CreatedAt: "2024-03-01 00:00:00", OrderStatus: "pending_ship"}},
				{orderID: "other-item", opts: OrderUpsertOpts{CookieID: cookieID, ChatID: "chat-1", ItemID: "item-2",
					CreatedAt: "2024-02-01 00:00:00", OrderStatus: "shipped"}},
			}
			// seed 是当前遍历到的订单种子。
			for _, seed := range seeds {
				// seedErr 保存当前订单播种失败原因。
				if seedErr := target.store.Orders.Upsert(ctx, seed.orderID, seed.opts); seedErr != nil {
					t.Fatalf("播种订单 %s: %v", seed.orderID, seedErr)
				}
			}

			// latest 是同商品下最近一笔订单，应为 new-pending。
			latest, err := target.store.Orders.FindLatestForFulfillment(ctx, cookieID, "chat-1", "item-1")
			if err != nil {
				t.Fatalf("按会话查订单: %v", err)
			}
			if latest == nil || latest.OrderID != "new-pending" {
				t.Fatalf("应取同商品最近一笔订单，got=%+v", latest)
			}

			// byItem 是其他商品的最近订单，验证 itemID 串单防线。
			byItem, itemErr := target.store.Orders.FindLatestForFulfillment(ctx, cookieID, "chat-1", "item-2")
			if itemErr != nil {
				t.Fatalf("按商品隔离查订单: %v", itemErr)
			}
			if byItem == nil || byItem.OrderID != "other-item" {
				t.Fatalf("itemID 未生效串单防线，got=%+v", byItem)
			}

			// completedOnly 验证已完结订单仍可被查到——这正是与 FindLatestOpenByChat 的差别。
			completedOnly, completedErr := target.store.Orders.FindLatestForFulfillment(ctx, cookieID, "chat-1", "item-2")
			if completedErr != nil {
				t.Fatalf("查已发货订单: %v", completedErr)
			}
			if completedOnly == nil || completedOnly.OrderStatus != "shipped" {
				t.Fatalf("已发货订单不应被状态过滤掉，got=%+v", completedOnly)
			}

			// missing 是没有任何成交记录的会话，必须返回 nil 而不是错误。
			missing, missingErr := target.store.Orders.FindLatestForFulfillment(ctx, cookieID, "chat-none", "item-1")
			if missingErr != nil {
				t.Fatalf("查询无订单会话: %v", missingErr)
			}
			if missing != nil {
				t.Fatalf("无订单应返回 nil，got=%+v", missing)
			}

			// suffixed 验证历史带 @goofish 后缀的会话标识也能命中同一订单。
			suffixed, suffixedErr := target.store.Orders.FindLatestForFulfillment(ctx, cookieID, "chat-1@goofish", "item-1")
			if suffixedErr != nil {
				t.Fatalf("后缀会话查订单: %v", suffixedErr)
			}
			if suffixed == nil || suffixed.OrderID != "new-pending" {
				t.Fatalf("后缀会话未命中订单，got=%+v", suffixed)
			}
		})
	}
}
