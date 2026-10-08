// orders_chat_role_evidence_test.go 覆盖「同一会话内订单归属」这类身份证据查询：它决定买家侧事件能否被直接收口。

package db

import (
	"context"
	"testing"
)

// TestExistsOpenSellerOrderByChat 验证归属查询同时覆盖带协议后缀与裸值的会话标识，且不区分订单状态。
// 待付款与待发货订单都能证明本机在这个会话里承担卖家义务，漏掉任何一种都会让真实发货被误判成买家视角。
func TestExistsOpenSellerOrderByChat(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	_, _ = userID, cookieID
	// pendingOrderErr 保存待发货订单夹具写入错误。
	if _, pendingOrderErr := s.DB.ExecContext(ctx,
		`INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id, quantity, amount)
		 VALUES ('o-chat-pend','item-1','buyer-1','pending_ship',?,'chat-role','1','9.90')`, cookieID); pendingOrderErr != nil {
		t.Fatal(pendingOrderErr)
	}
	// exists、err 保存带协议后缀会话标识的归属查询结果。
	exists, err := s.Orders.ExistsOpenSellerOrderByChat(ctx, "chat-role@goofish", cookieID)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("会话内归属订单应被命中")
	}
	// missing、missingErr 保存无归属订单账号的查询结果。
	missing, missingErr := s.Orders.ExistsOpenSellerOrderByChat(ctx, "chat-role", "other-acc")
	if missingErr != nil {
		t.Fatal(missingErr)
	}
	if missing {
		t.Fatal("会话内没有归属订单时必须返回 false，避免误判卖家身份")
	}
	// softDeletedErr 保存软删除该订单的改写错误。
	if _, softDeletedErr := s.DB.ExecContext(ctx,
		`UPDATE orders SET deleted_at=CURRENT_TIMESTAMP WHERE order_id='o-chat-pend'`); softDeletedErr != nil {
		t.Fatal(softDeletedErr)
	}
	// afterDelete、afterDeleteErr 保存软删除后的归属查询结果。
	afterDelete, afterDeleteErr := s.Orders.ExistsOpenSellerOrderByChat(ctx, "chat-role", cookieID)
	if afterDeleteErr != nil {
		t.Fatal(afterDeleteErr)
	}
	if afterDelete {
		t.Fatal("软删除订单不得再作为卖家身份证据")
	}
	// empty 保存空会话标识的查询结果，必须安全返回 false。
	empty, emptyErr := s.Orders.ExistsOpenSellerOrderByChat(ctx, "", cookieID)
	if emptyErr != nil || empty {
		t.Fatalf("空会话标识应安全返回 false: exists=%v err=%v", empty, emptyErr)
	}
}

// TestFindLatestForeignOrderByChat 验证会话内最近一笔非归属订单的查询边界。
// 该订单是判定「本机既不是卖家也不是买家」或「本机就是买家」的唯一依据，必须按更新时间取最近且排除归属订单。
func TestFindLatestForeignOrderByChat(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	// sellerID 保存会话内真正的卖家账号标识；orders.cookie_id 有外键约束，必须先落库。
	sellerID := "seller-acc"
	// sellerSaveErr 保存卖家账号写入错误。
	if sellerSaveErr := s.Cookies.Save(ctx, sellerID, "cv=seller", userID); sellerSaveErr != nil {
		t.Fatal(sellerSaveErr)
	}
	// olderErr 保存较早的卖家订单夹具写入错误。
	if _, olderErr := s.DB.ExecContext(ctx,
		`INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id, quantity, amount, updated_at)
		 VALUES ('o-old','item-1','buyer-1','pending_ship',?,'chat-role','1','9.90','2026-10-01 00:00:00')`, sellerID); olderErr != nil {
		t.Fatal(olderErr)
	}
	// newerErr 保存较新的卖家订单夹具写入错误。
	if _, newerErr := s.DB.ExecContext(ctx,
		`INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id, quantity, amount, updated_at)
		 VALUES ('o-new','item-1','buyer-1','shipped',?,'chat-role','1','19.90','2026-10-02 00:00:00')`, sellerID); newerErr != nil {
		t.Fatal(newerErr)
	}
	// ownErr 保存归属本账号的订单夹具写入错误；存在它时必须优先认为本机可能是卖家。
	if _, ownErr := s.DB.ExecContext(ctx,
		`INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id, quantity, amount, updated_at)
		 VALUES ('o-own','item-9','buyer-9','pending_ship',?,'chat-role','1','1.00','2026-10-03 00:00:00')`, cookieID); ownErr != nil {
		t.Fatal(ownErr)
	}
	// order、err 保存会话内最近一笔非归属订单。
	order, err := s.Orders.FindLatestForeignOrderByChat(ctx, "chat-role@goofish", cookieID)
	if err != nil {
		t.Fatal(err)
	}
	if order == nil || order.OrderID != "o-new" {
		t.Fatalf("应返回会话内最近的非归属订单 o-new: %+v", order)
	}
	if order.CookieID != sellerID {
		t.Fatalf("非归属订单不得返回本账号订单: %+v", order)
	}
	// none、noneErr 保存只存在归属订单时的查询结果；必须为空，表示没有否定卖家身份的依据。
	none, noneErr := s.Orders.FindLatestForeignOrderByChat(ctx, "chat-none", cookieID)
	if noneErr != nil {
		t.Fatal(noneErr)
	}
	if none != nil {
		t.Fatalf("会话内没有非归属订单时必须返回 nil: %+v", none)
	}
}
