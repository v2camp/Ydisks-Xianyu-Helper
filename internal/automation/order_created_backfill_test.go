package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestUnknownWebSocketOrderCreatedBackfillsFromOpenChatOrder 验证简化拍下事件在缺订单号与商品号时，
// 可从本账号同一会话的待付款（未终结）订单回填事实，从而通过卖家核验而不进入延期/死信队列。
// 该用例复现生产缺陷：拍下类事件被跳过会话回填，卖家核验永远缺商品事实，最终死信。
func TestUnknownWebSocketOrderCreatedBackfillsFromOpenChatOrder(t *testing.T) {
	// store、cleanup 提供隔离数据库及测试结束后的连接释放责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 限制本测试的事件处理、订单事实与延期队列生命周期。
	ctx := context.Background()
	// itemErr 保存本账号历史商品事实，作为拍下事件的卖家归属证据。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "open-item", ItemTitle: "拍下商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// orderErr 保存待付款订单种子，模拟闲鱼拍下提醒抵达时本地订单尚未付款的真实状态。
	if orderErr := store.Orders.Upsert(ctx, "open-order", db.OrderUpsertOpts{
		CookieID: "cid", ChatID: "open-chat", BuyerID: "open-buyer", ItemID: "open-item", OrderStatus: "processing",
	}); orderErr != nil {
		t.Fatal(orderErr)
	}
	// sender 记录任何不应发生的消息发送副作用。
	sender := &testSender{}
	// center 使用真实事实记录器，确保门禁位于所有持久化副作用之前。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// task 模拟简化红色提醒拍下事件：只有会话号，没有订单号、商品号与角色。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderCreated, ChatID: "open-chat"}
	// handleErr 保存拍下事件经过会话回填与卖家核验后的处理错误。
	if handleErr := center.HandleTask(ctx, task); handleErr != nil {
		t.Fatalf("拍下事件处理失败: %v", handleErr)
	}
	// pending 保存延期队列中的待核验任务数量，用于验证拍下事件不再被延期。
	var pending int
	// pendingErr 保存延期队列计数查询错误。
	if pendingErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_pending_tasks`).Scan(&pending); pendingErr != nil {
		t.Fatal(pendingErr)
	}
	if pending != 0 {
		t.Fatalf("拍下事件不应进入延期队列: pending=%d", pending)
	}
	// order、orderErr 验证会话回填出的订单事实归属本账号，卖家核验已通过。
	order, orderErr := store.Orders.Get(ctx, "open-order")
	if orderErr != nil || order == nil || order.CookieID != "cid" {
		t.Fatalf("拍下订单事实未记录: order=%+v err=%v", order, orderErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("拍下事件不应发送发货消息: %v", sender.texts)
	}
}
