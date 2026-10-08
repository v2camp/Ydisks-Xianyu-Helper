// role_gate_buyer_evidence_test.go 覆盖「旁观视角事件」的收口判据：本机既不是卖家时，买家侧事件必须停止延期。

package automation

import (
	"context"
	"testing"
)

// insertChatRoleOrder 写入指定会话与归属的订单夹具；orders.cookie_id 有外键约束，缺少账号会导致插入失败。
func insertChatRoleOrder(t *testing.T, center *Center, orderID, cookieID, buyerID, chatID, status string) {
	t.Helper()
	// saveErr 保存账号夹具写入错误。
	if saveErr := center.store.Cookies.Save(context.Background(), cookieID, "unb="+cookieID, 1); saveErr != nil {
		t.Fatalf("写入账号夹具失败: %v", saveErr)
	}
	// insertErr 保存订单夹具写入错误。
	if _, insertErr := center.store.DB.ExecContext(context.Background(),
		`INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id, quantity, amount)
		 VALUES (?,?,?,?,?,?,'1','9.90')`, orderID, "item-role", buyerID, status, cookieID, chatID); insertErr != nil {
		t.Fatalf("写入订单夹具失败: %v", insertErr)
	}
}

// TestResolveBuyerRoleByForeignChatOrderEndsBuyerSideEvents 验证买家侧事件会被立刻判定为买家身份。
// 这类事件在会话里永远等不到属于本账号的卖家订单事实；若继续延期，退避队列会反复重放并最终按重试上限
// 发出「需要人工处理」告警，而买家侧根本没有发货义务。
func TestResolveBuyerRoleByForeignChatOrderEndsBuyerSideEvents(t *testing.T) {
	// store、cleanup 保存测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 是带真实存储的自动化中心。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// sellerID 是会话内真正的卖家账号标识。
	sellerID := "seller-acc"
	// buyerID 是同一会话里的买家账号标识。
	buyerID := "buyer-acc"
	insertChatRoleOrder(t, center, "o-role", sellerID, buyerID, "chat-buyer-side", "pending_ship")
	// cases 覆盖买家、无关账号、有归属订单、空会话与无关会话五类输入。
	cases := []struct {
		name   string
		task   Task
		reason string
	}{
		{name: "本机是订单买家", task: Task{AccountID: buyerID, ChatID: "chat-buyer-side"}, reason: "explicit_buyer_role"},
		{name: "本机与该订单无关", task: Task{AccountID: "other-acc", ChatID: "chat-buyer-side"}, reason: "order_account_mismatch"},
		{name: "会话内存在归属订单时保留判断", task: Task{AccountID: sellerID, ChatID: "chat-buyer-side"}, reason: ""},
		{name: "会话没有任何订单时保留判断", task: Task{AccountID: buyerID, ChatID: "chat-empty"}, reason: ""},
		{name: "缺少会话标识时保留判断", task: Task{AccountID: buyerID, ChatID: ""}, reason: ""},
	}
	// tc 是当前待验证的用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 保存本次角色的判定结论。
			got := center.resolveBuyerRoleByForeignChatOrder(ctx, tc.task)
			if got != tc.reason {
				t.Fatalf("resolveBuyerRoleByForeignChatOrder = %q, want %q", got, tc.reason)
			}
		})
	}
}

// TestBuyerRoleReasonsAreNotRetryable 验证这两个拒绝原因都不在可重试集合内。
// 一旦它们被误加进白名单，买家侧事件会重新变成退避队列里的死任务，修复 B 的目的随之失效。
func TestBuyerRoleReasonsAreNotRetryable(t *testing.T) {
	// reasons 是本次需要锁死的收口原因。
	reasons := []string{"explicit_buyer_role", "order_account_mismatch"}
	// reason 表示当前遍历过程中的拒绝原因。
	for _, reason := range reasons {
		if roleVerificationRetryable(reason) {
			t.Fatalf("%s 不得进入待核验重试白名单", reason)
		}
	}
}
