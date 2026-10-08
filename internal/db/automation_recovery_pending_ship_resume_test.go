// automation_recovery_pending_ship_resume_test.go 覆盖「确认发货被静默跳过后仍留待发货」的订单如何重新回到自动发货链路。

package db

import (
	"context"
	"testing"
	"time"
)

// TestPendingShipResumableRunsAfterIncludesSuccessAndSkipsShipped 验证续跑候选集合的收口边界。
// 账号未开启自动确认发货时确认发货动作会被静默跳过，运行仍收口为 success 且动作游标走到末尾；
// 这类订单此后没有任何补触发入口，必须纳入候选。订单已经本地标记发货时则必须排除，避免重复调用平台确认发货。
func TestPendingShipResumableRunsAfterIncludesSuccessAndSkipsShipped(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	// fullRuleID 保存包含发卡与确认发货两类动作的规则主键。
	fullRuleID := seedCatchupRule(t, s, userID, cookieID, "item-1", "发货续跑规则", true,
		[]AutomationActionInput{{ActionType: "send_card", MessageTemplate: "card", Enabled: true}, {ActionType: "confirm_shipment", Enabled: true}})
	// 确认发货被跳过：订单仍待发货、运行成功、游标已走到计划末尾。
	seedCatchupOrder(t, s, "o-consign-skipped", cookieID, "item-1", "chat-consign-skipped", "pending_ship")
	// skippedRunID 保存确认发货被静默跳过的运行主键。
	skippedRunID := seedCatchupRun(t, s, fullRuleID, cookieID, "o-consign-skipped")
	// 已本地标记发货：订单事实已经完成，不得因为历史运行是 success 而被再次确认发货。
	seedCatchupOrder(t, s, "o-already-shipped", cookieID, "item-1", "chat-already-shipped", "pending_ship")
	// shippedRunID 保存已发货订单的历史运行主键。
	shippedRunID := seedCatchupRun(t, s, fullRuleID, cookieID, "o-already-shipped")
	// skipUpdateErr 保存「确认发货被跳过」运行的状态改写错误。
	if _, skipUpdateErr := s.DB.ExecContext(ctx,
		`UPDATE automation_runs SET status='success', attempt_count=1, action_cursor=2, action_started=0 WHERE id=?`, skippedRunID); skipUpdateErr != nil {
		t.Fatal(skipUpdateErr)
	}
	// shippedRunErr 保存已发货订单对应的运行改写错误。
	if _, shippedRunErr := s.DB.ExecContext(ctx,
		`UPDATE automation_runs SET status='success', attempt_count=1, action_cursor=2, action_started=0 WHERE id=?`, shippedRunID); shippedRunErr != nil {
		t.Fatal(shippedRunErr)
	}
	// shippedOrderErr 保存订单本地发货标记的改写错误。
	if _, shippedOrderErr := s.DB.ExecContext(ctx,
		`UPDATE orders SET system_shipped=1 WHERE order_id=?`, "o-already-shipped"); shippedOrderErr != nil {
		t.Fatal(shippedOrderErr)
	}
	// candidates 保存续跑扫描结果。
	candidates, err := s.Automation.PendingShipResumableRunsAfter(ctx, "", 5, 200)
	if err != nil {
		t.Fatal(err)
	}
	// gotIDs 收集候选订单，便于断言包含与排除。
	gotIDs := map[string]PendingShipResume{}
	// candidate 表示当前遍历过程中的续跑候选。
	for _, candidate := range candidates {
		gotIDs[candidate.Order.OrderID] = candidate
	}
	// skipped 保存确认发货被跳过的候选；不在集合中说明该订单永远无法自愈。
	skipped, found := gotIDs["o-consign-skipped"]
	if !found {
		t.Fatalf("确认发货被跳过的 success 运行必须进入续跑候选: %+v", candidates)
	}
	if skipped.RunID != skippedRunID || skipped.Status != "success" || skipped.ActionCursor != 2 || skipped.Attempt != 1 {
		t.Fatalf("success 候选回填异常: %+v", skipped)
	}
	// ok 保存已发货订单是否被错误放回候选集合。
	if _, ok := gotIDs["o-already-shipped"]; ok {
		t.Fatalf("已本地标记发货的订单不得进入续跑候选: %+v", gotIDs["o-already-shipped"])
	}
}

// TestReopenRunForRecoveryRollsBackCursorOnly 验证重开运行只允许把动作游标回退到幂等确认发货尾部。
// 回退是补确认发货的唯一入口；向前的游标会把尚未确认完成的发卡或消息动作跳过，必须被拒绝。
func TestReopenRunForRecoveryRollsBackCursorOnly(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号主键与账号标识。
	userID, cookieID := seedAccount(t, s)
	// fullRuleID 保存包含发卡与确认发货两类动作的规则主键。
	fullRuleID := seedCatchupRule(t, s, userID, cookieID, "item-1", "发货续跑规则", true,
		[]AutomationActionInput{{ActionType: "send_card", MessageTemplate: "card", Enabled: true}, {ActionType: "confirm_shipment", Enabled: true}})
	seedCatchupOrder(t, s, "o-rollback", cookieID, "item-1", "chat-rollback", "pending_ship")
	// runID 保存游标已走到计划末尾的运行主键。
	runID := seedCatchupRun(t, s, fullRuleID, cookieID, "o-rollback")
	// setupErr 保存把运行改写成「确认发货被跳过」状态的错误。
	if _, setupErr := s.DB.ExecContext(ctx,
		`UPDATE automation_runs SET status='success', attempt_count=1, action_cursor=2, action_started=0 WHERE id=?`, runID); setupErr != nil {
		t.Fatal(setupErr)
	}
	// leaseAt 是本次恢复分配的租约到期时间。
	leaseAt := time.Now().UTC().Add(5 * time.Minute).Unix()
	// reopened、reopenErr 保存回退游标到确认发货动作的重开结果。
	reopened, reopenErr := s.Automation.ReopenRunForRecovery(ctx, runID, 1, 1, leaseAt)
	if reopenErr != nil || !reopened {
		t.Fatalf("回退到确认发货尾部应成功: reopened=%v err=%v", reopened, reopenErr)
	}
	// run 保存重开后的运行记录，用于校验游标已回退且代次递增。
	run, getErr := s.Automation.GetRun(ctx, runID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if run.Status != "running" || run.ActionCursor != 1 || run.AttemptCount != 2 {
		t.Fatalf("重开后应回到确认发货动作并递增代次: %+v", run)
	}
	// forwarded、forwardErr 保存试图把游标继续向前推进的重开结果；必须被拒绝。
	forwarded, forwardErr := s.Automation.ReopenRunForRecovery(ctx, runID, 2, 2, leaseAt)
	if forwardErr != nil {
		t.Fatalf("前向游标应被安静拒绝而不是报错: %v", forwardErr)
	}
	if forwarded {
		t.Fatal("游标不得向前推进到未经确认完成的动作")
	}
	// negative、negativeErr 保存非法下标的重开结果；必须返回错误而不是写入数据库。
	negative, negativeErr := s.Automation.ReopenRunForRecovery(ctx, runID, 2, -1, leaseAt)
	if negativeErr == nil || negative {
		t.Fatalf("负数游标必须返回错误: reopened=%v err=%v", negative, negativeErr)
	}
}
