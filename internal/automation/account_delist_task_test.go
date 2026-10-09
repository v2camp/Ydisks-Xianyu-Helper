package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestDelistDueCoversScheduleWindows 验证每日下架到期判断覆盖未来、到点和当日已执行。
func TestDelistDueCoversScheduleWindows(t *testing.T) {
	// now 是北京时间当天 08:30，用于比较不同配置的下架时间。
	now := time.Date(2026, 10, 9, 8, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	// cases 保存不同下架时间配置与期望到期结果。
	cases := []struct {
		// name 是当前分支名称。
		name string
		// settings 是待判断的账号任务设置。
		settings db.AccountTaskSettings
		// want 是期望的到期结果。
		want bool
	}{
		{name: "尚未到点", settings: db.AccountTaskSettings{DelistTime: "09:00"}, want: false},
		{name: "恰好到点", settings: db.AccountTaskSettings{DelistTime: "08:30"}, want: true},
		{name: "已经过点", settings: db.AccountTaskSettings{DelistTime: "01:00"}, want: true},
		{name: "当日已完成", settings: db.AccountTaskSettings{DelistTime: "01:00", LastDelistDate: "2026-10-09"}, want: false},
		{name: "非法时间回落默认", settings: db.AccountTaskSettings{DelistTime: "9:0"}, want: false},
	}
	// testCase 是当前待验证的下架到期分支。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 保存当前配置下的下架到期判断结果。
			if got := delistDue(testCase.settings, now); got != testCase.want {
				t.Fatalf("delistDue=%v，期望 %v", got, testCase.want)
			}
		})
	}
}

// TestRunAutoDelistOnlyProcessesWhitelist 验证定时下架只处理白名单商品，并记录当日完成日期。
func TestRunAutoDelistOnlyProcessesWhitelist(t *testing.T) {
	// repository 保存可控内存仓储，凭证版本与非空 Cookie 用于打开会话。
	repository := &accountTaskFlowRepository{value: "cookie", claimed: true}
	// client 保存记录下架请求的商品客户端替身。
	client := &accountTaskFlowClient{}
	// coordinator 是绑定内存仓储和客户端的账号任务协调器。
	coordinator := newAccountTaskFlowCoordinator(repository, client)
	// settings 是包含两个白名单商品的账号任务设置。
	settings := db.AccountTaskSettings{CookieID: "account", AutoDelistEnabled: true, DelistTime: "09:00", DelistItemIDs: []string{"item-1", "item-2"}}
	// now 是触发下架的北京时间。
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	// summary、err 保存本轮下架结果及错误。
	summary, err := coordinator.runAutoDelist(context.Background(), settings, now, false)
	if err != nil || summary.Found != 2 || summary.Success != 2 || summary.Failed != 0 {
		t.Fatalf("下架结果错误: summary=%+v err=%v", summary, err)
	}
	if client.downshelfCalls != 2 {
		t.Fatalf("下架调用次数=%d，期望 2", client.downshelfCalls)
	}
	if len(client.downshelfItemIDs) != 2 || client.downshelfItemIDs[0] != "item-1" || client.downshelfItemIDs[1] != "item-2" {
		t.Fatalf("下架商品与白名单不一致: %+v", client.downshelfItemIDs)
	}
	if repository.markDelistCalls != 1 || repository.markDelistDate != "2026-10-09" {
		t.Fatalf("下架完成标记错误: calls=%d date=%q", repository.markDelistCalls, repository.markDelistDate)
	}
}

// TestRunAutoDelistSkipsEmptyWhitelist 验证空白名单不会触达平台并仍标记当日完成。
func TestRunAutoDelistSkipsEmptyWhitelist(t *testing.T) {
	// repository 保存可控内存仓储。
	repository := &accountTaskFlowRepository{value: "cookie", claimed: true}
	// client 保存商品客户端替身；空白名单下不应被调用。
	client := &accountTaskFlowClient{}
	// coordinator 是绑定内存仓储和客户端的账号任务协调器。
	coordinator := newAccountTaskFlowCoordinator(repository, client)
	// summary、err 保存空白名单下的下架结果及错误。
	summary, err := coordinator.runAutoDelist(context.Background(), db.AccountTaskSettings{CookieID: "account", AutoDelistEnabled: true, DelistTime: "09:00"}, beijingNow(), false)
	if err != nil || summary.Found != 0 || summary.Success != 0 {
		t.Fatalf("空白名单下架结果错误: summary=%+v err=%v", summary, err)
	}
	if client.downshelfCalls != 0 {
		t.Fatalf("空白名单不应调用下架接口: %d", client.downshelfCalls)
	}
	if repository.markDelistCalls != 1 {
		t.Fatalf("空白名单仍应标记当日完成: %d", repository.markDelistCalls)
	}
}

// TestRunAutoDelistReportsClaimSkipAndFailures 验证抢占冲突、平台失败和会话失效的下架分支。
func TestRunAutoDelistReportsClaimSkipAndFailures(t *testing.T) {
	// skippedRepository 保存抢占失败的仓储，用于验证当日去重。
	skippedRepository := &accountTaskFlowRepository{value: "cookie", claimed: false}
	// skippedCoordinator 是抢占失败的账号任务协调器。
	skippedCoordinator := newAccountTaskFlowCoordinator(skippedRepository, &accountTaskFlowClient{})
	// skippedSummary、skippedErr 保存抢占失败结果。
	skippedSummary, skippedErr := skippedCoordinator.runAutoDelist(context.Background(), db.AccountTaskSettings{CookieID: "account", DelistItemIDs: []string{"item-1"}}, beijingNow(), false)
	if skippedErr != nil || skippedSummary.Skipped != 1 || skippedSummary.Message == "" {
		t.Fatalf("抢占失败分支错误: summary=%+v err=%v", skippedSummary, skippedErr)
	}

	// failedRepository 保存抢占成功的仓储。
	failedRepository := &accountTaskFlowRepository{value: "cookie", claimed: true}
	// failedClient 是返回平台错误的下架客户端替身。
	failedClient := &accountTaskFlowClient{downshelfErr: errors.New("downshelf rejected")}
	// failedCoordinator 是平台失败场景的账号任务协调器。
	failedCoordinator := newAccountTaskFlowCoordinator(failedRepository, failedClient)
	// failedSummary、failedErr 保存平台失败结果。
	failedSummary, failedErr := failedCoordinator.runAutoDelist(context.Background(), db.AccountTaskSettings{CookieID: "account", DelistItemIDs: []string{"item-1"}}, beijingNow(), false)
	if failedErr == nil || failedSummary.Failed != 1 || failedSummary.Success != 0 {
		t.Fatalf("平台失败分支错误: summary=%+v err=%v", failedSummary, failedErr)
	}
	if failedRepository.markDelistCalls != 0 {
		t.Fatalf("存在失败商品时不应标记当日完成: %d", failedRepository.markDelistCalls)
	}

	// sessionRepository 保存抢占成功的仓储。
	sessionRepository := &accountTaskFlowRepository{value: "cookie", claimed: true}
	// sessionClient 是返回会话失效错误的下架客户端替身。
	sessionClient := &accountTaskFlowClient{downshelfErr: &mtop.SessionExpiredError{API: "下架接口", Ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}}}
	// sessionCoordinator 是会话失效场景的账号任务协调器。
	sessionCoordinator := newAccountTaskFlowCoordinator(sessionRepository, sessionClient)
	// sessionSummary、sessionErr 保存会话失效结果；该错误必须保留会话失效分类以供上层续期。
	sessionSummary, sessionErr := sessionCoordinator.runAutoDelist(context.Background(), db.AccountTaskSettings{CookieID: "account", DelistItemIDs: []string{"item-1"}}, beijingNow(), false)
	if sessionErr == nil || !mtop.IsSessionExpiredErr(sessionErr) || sessionSummary.Failed != 1 {
		t.Fatalf("会话失效分支错误: summary=%+v err=%v", sessionSummary, sessionErr)
	}
}
