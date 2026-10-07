package automation

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// newPaidPreActionFixture 构造付款后发卡规则与指定状态的订单，返回账号、规则主键、存储与清理函数。
// orderStatus 是订单创建时写入的本地状态；卡密内容固定，便于断言是否真的发出。
func newPaidPreActionFixture(t *testing.T, orderStatus string) (string, int64, *db.Store, func()) {
	t.Helper()
	// store、cleanup 保存隔离测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	// ctx 是夹具写入使用的上下文。
	ctx := context.Background()
	// admin、adminErr 保存规则所属管理员及查询错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		cleanup()
		t.Fatal(adminErr)
	}
	// cardID、cardErr 保存一旦门禁失效就会被发送的测试卡密组。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{
		Name: "动作前门禁卡", Type: "text", TextContent: "PRE-ACTION-GUARD", Enabled: true, UserID: admin.ID,
	})
	if cardErr != nil {
		cleanup()
		t.Fatal(cardErr)
	}
	// cookieID 是订单与规则所属账号。
	cookieID := "preaction-acc"
	// saveErr 保存账号夹具写入错误；账号缺失时自动化门禁会直接拒绝。
	if saveErr := store.Cookies.Save(ctx, cookieID, "unb=1", admin.ID); saveErr != nil {
		cleanup()
		t.Fatal(saveErr)
	}
	// ruleID、ruleErr 保存付款后发卡规则主键与创建错误；空规格动作匹配无规格订单。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: cookieID, ItemID: "preaction-item", Name: "动作前门禁",
		TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`, Enabled: true}},
	})
	if ruleErr != nil {
		cleanup()
		t.Fatal(ruleErr)
	}
	// upsertErr 保存订单夹具写入错误；订单必须带 chat_id 才能进入发送路径。
	if upsertErr := store.Orders.Upsert(ctx, "preaction-order", db.OrderUpsertOpts{
		CookieID: cookieID, ItemID: "preaction-item", BuyerID: "buyer", ChatID: "chat",
		Quantity: "1", Amount: "9.90", OrderStatus: orderStatus,
	}); upsertErr != nil {
		cleanup()
		t.Fatal(upsertErr)
	}
	return cookieID, ruleID, store, cleanup
}

// runPaidTaskForGuard 用动作前门禁夹具执行一次付款事件，返回发送记录、运行状态与读取错误。
// orderStatus 是订单写入的本地状态；它同时写入任务快照，模拟 WebSocket 事件携带的原始状态。
func runPaidTaskForGuard(t *testing.T, orderStatus string) ([]string, string) {
	t.Helper()
	// cookieID、ruleID、store、cleanup 保存夹具账号、规则主键、存储与清理函数。
	cookieID, ruleID, store, cleanup := newPaidPreActionFixture(t, orderStatus)
	defer cleanup()
	// ctx 是执行与查询共用的取消边界。
	ctx := context.Background()
	// rule、ruleErr 保存执行所需的规则快照与读取错误。
	rule, ruleErr := store.Automation.Get(ctx, ruleID)
	if ruleErr != nil || rule == nil {
		t.Fatalf("读取规则失败: %v", ruleErr)
	}
	// task 是模拟 WebSocket 付款发货事件的任务快照。
	task := Task{
		Source: "ws", AccountID: cookieID, OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid,
		OrderID: "preaction-order", ItemID: "preaction-item", BuyerID: "buyer", ChatID: "chat",
		Quantity: "1", Amount: "9.90", OrderStatus: orderStatus, Text: "[我已付款，等待你发货]",
	}
	// sender 记录门禁放行时是否真的发出卡密。
	sender := &testSender{}
	// center 是使用在线发送器的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	if // runErr 保存本次执行的错误；已失效订单被取消时不得返回错误。
	runErr := center.executeRule(ctx, task, *rule); runErr != nil {
		t.Fatalf("执行付款任务失败: %v", runErr)
	}
	// status 是本次运行写入的终态。
	var status string
	if // scanErr 保存运行状态读取错误。
	scanErr := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id=?`, "preaction-order").Scan(&status); scanErr != nil {
		t.Fatalf("读取运行状态失败: %v", scanErr)
	}
	return sender.texts, status
}

// TestPaidRunSkipsDeliveryWhenOrderAlreadyCancelled 验证动作执行前订单已取消时不再发卡。
// 复现线上事故：买家已申请退款成功，运行仍按冻结动作计划发出卡密并随后确认发货失败。
func TestPaidRunSkipsDeliveryWhenOrderAlreadyCancelled(t *testing.T) {
	// texts、status 保存已取消订单运行的发送记录与终态。
	texts, status := runPaidTaskForGuard(t, "12")
	if len(texts) != 0 {
		t.Fatalf("订单已取消仍发出卡密: %v", texts)
	}
	if status != "canceled" {
		t.Fatalf("已取消订单运行终态=%q want canceled", status)
	}
}

// TestPaidRunStillDeliversForPendingShipOrder 验证订单仍待发货时门禁不误伤正常发货。
func TestPaidRunStillDeliversForPendingShipOrder(t *testing.T) {
	// texts、status 保存正常订单运行的发送记录与终态。
	texts, status := runPaidTaskForGuard(t, "pending_ship")
	if len(texts) == 0 {
		t.Fatalf("待发货订单未发出卡密: status=%q", status)
	}
	if status != "success" {
		t.Fatalf("待发货订单运行终态=%q want success", status)
	}
	if texts[0] != "PRE-ACTION-GUARD" {
		t.Fatalf("待发货订单卡密内容=%q", texts[0])
	}
}

// TestPaidRunStillDeliversForUnknownOrderStatus 验证本地状态未映射时按事实不足放行，避免漏发。
func TestPaidRunStillDeliversForUnknownOrderStatus(t *testing.T) {
	// texts、status 保存状态未知订单运行的发送记录与终态。
	texts, status := runPaidTaskForGuard(t, "unknown")
	if len(texts) == 0 {
		t.Fatalf("状态未知订单被误判为失效而未发货: status=%q", status)
	}
	if status != "success" {
		t.Fatalf("状态未知订单运行终态=%q want success", status)
	}
}

// TestOrderTerminalWithoutDeliveryClassification 验证终态判定只承认明确不需要发货的状态。
func TestOrderTerminalWithoutDeliveryClassification(t *testing.T) {
	// cases 覆盖取消、退款、完成、已发货与事实不足状态的判定。
	cases := []struct {
		// status 是待判定的原始订单状态。
		status string
		// want 是该状态是否属于明确终态。
		want bool
	}{
		{status: "12", want: true},
		{status: "cancelled", want: true},
		{status: "refunding", want: true},
		{status: "4", want: true},
		{status: "completed", want: true},
		{status: "pending_ship", want: false},
		{status: "2", want: false},
		{status: "partial_success", want: false},
		{status: "unknown", want: false},
		{status: "", want: false},
	}
	// testCase 表示当前待验证的终态判定样例。
	for _, testCase := range cases {
		// got 是当前状态的终态判定结果。
		got := isOrderTerminalWithoutDelivery(testCase.status)
		if got != testCase.want {
			t.Fatalf("状态 %q 终态判定=%v want %v", testCase.status, got, testCase.want)
		}
	}
}

// TestPaidRunGuardCoversOrderFactBoundaries 验证门禁对已发货、归属不符与读取失败三类事实边界的处理。
func TestPaidRunGuardCoversOrderFactBoundaries(t *testing.T) {
	// store、cleanup 保存隔离测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是夹具写入与判定共用的取消边界。
	ctx := context.Background()
	// coordinator 是 Center 实际装配的运行协调器。
	coordinator := New(store, testSenderProvider{sender: &testSender{}}, nil).runs
	// admin、adminErr 保存订单夹具所属管理员及查询错误；订单外键要求账号行已存在。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// saveErr 保存账号夹具写入错误。
	if saveErr := store.Cookies.Save(ctx, "guard-acc", "unb=2", admin.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// shipped 标记订单已由本系统确认发货。
	shipped := true
	// shippedErr 保存已发货订单夹具写入错误。
	if shippedErr := store.Orders.Upsert(ctx, "guard-shipped", db.OrderUpsertOpts{
		CookieID: "guard-acc", ItemID: "guard-item", BuyerID: "buyer", ChatID: "chat",
		Quantity: "1", Amount: "9.90", OrderStatus: "shipped", SystemShipped: &shipped,
	}); shippedErr != nil {
		t.Fatal(shippedErr)
	}
	// shippedReason 是已发货订单的门禁原因；必须给出不再发货的说明。
	shippedReason := coordinator.paidRunObsoleteReason(ctx, Task{TriggerType: TriggerOrderPaid, AccountID: "guard-acc", OrderID: "guard-shipped"})
	if !strings.Contains(shippedReason, "已发货") {
		t.Fatalf("已发货订单门禁原因=%q", shippedReason)
	}
	// mismatchReason 是归属不符时的门禁判定；不得借用其他账号的订单事实，必须为空。
	mismatchReason := coordinator.paidRunObsoleteReason(ctx, Task{TriggerType: TriggerOrderPaid, AccountID: "other-acc", OrderID: "guard-shipped"})
	if mismatchReason != "" {
		t.Fatalf("归属不符时不应判定失效: %q", mismatchReason)
	}
	// closedErr 保存关闭数据库的结果；关闭后读取必须走真实错误分支而不是 ErrNotFound。
	if closedErr := store.DB.Close(); closedErr != nil {
		t.Fatal(closedErr)
	}
	// readErrReason 是数据库不可读时的门禁判定；必须降级放行且不 panic。
	readErrReason := coordinator.paidRunObsoleteReason(ctx, Task{TriggerType: TriggerOrderPaid, AccountID: "guard-acc", OrderID: "guard-shipped"})
	if readErrReason != "" {
		t.Fatalf("订单读取失败时不应判定失效: %q", readErrReason)
	}
}

// TestPaidRunGuardIgnoresNonPaidTriggers 验证门禁只作用于付款发货触发，不影响其他任务类型。
func TestPaidRunGuardIgnoresNonPaidTriggers(t *testing.T) {
	// store、cleanup 保存隔离测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// coordinator 是 Center 实际装配的运行协调器，避免测试构造半初始化依赖。
	coordinator := New(store, testSenderProvider{sender: &testSender{}}, nil).runs
	// cancelledOrder 是已取消状态的付款任务。
	cancelledOrder := Task{TriggerType: TriggerOrderPaid, AccountID: "cid", OrderID: "order-1"}
	// missingReason 是订单事实缺失时的门禁判定；属于事实不足，必须为空。
	missingReason := coordinator.paidRunObsoleteReason(context.Background(), cancelledOrder)
	if missingReason != "" {
		t.Fatalf("订单事实缺失时不应判定失效: %q", missingReason)
	}
	// reviewTask 是求评价任务，即使订单已取消也不由本门禁处理。
	reviewTask := Task{TriggerType: TriggerReviewMissingTimeout, AccountID: "cid", OrderID: "order-1"}
	// reviewReason 是非付款触发被门禁拦截的原因；必须为空。
	reviewReason := coordinator.paidRunObsoleteReason(context.Background(), reviewTask)
	if reviewReason != "" {
		t.Fatalf("非付款触发被门禁拦截: %q", reviewReason)
	}
	// noOrderTask 是缺少订单号的付款任务，必须放行交给既有校验。
	noOrderTask := Task{TriggerType: TriggerOrderPaid, AccountID: "cid"}
	// noOrderReason 是缺少订单号时的门禁判定；必须为空。
	noOrderReason := coordinator.paidRunObsoleteReason(context.Background(), noOrderTask)
	if noOrderReason != "" {
		t.Fatalf("缺少订单号时不应判定失效: %q", noOrderReason)
	}
}