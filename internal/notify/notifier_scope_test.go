package notify

import (
	"context"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// scopeRepositoryFake 为作用域路由测试提供可控的账号、系统渠道与站内事件记录。
type scopeRepositoryFake struct {
	// ownerUserID 是租户事件应解析到的账号所有者。
	ownerUserID int64
	// ownerErr 是账号所有者查询的预置故障。
	ownerErr error
	// ownerCalls 记录账号所有者查询次数。
	ownerCalls int
	// accountChannels 是租户事件可用的渠道。
	accountChannels []db.NotificationChannel
	// accountCalls 记录租户渠道查询次数。
	accountCalls int
	// systemChannels 是系统事件可用的管理员告警渠道。
	systemChannels []db.NotificationChannel
	// systemCalls 记录系统渠道查询次数。
	systemCalls int
	// recorded 保存站内事件写入参数，便于断言作用域与归属。
	recorded []db.NotificationEventInput
	// enqueued 保存外部投递入队参数，便于断言实际渠道。
	enqueued []db.NotificationOutboxInput
}

// AccountChannels 返回租户测试渠道并记录调用次数。
func (f *scopeRepositoryFake) AccountChannels(context.Context, string) ([]db.NotificationChannel, error) {
	f.accountCalls++
	return f.accountChannels, nil
}

// OwnerOfAccount 返回账号所有者并记录调用次数。
func (f *scopeRepositoryFake) OwnerOfAccount(context.Context, string) (int64, error) {
	f.ownerCalls++
	return f.ownerUserID, f.ownerErr
}

// RecordEvent 记录站内事件写入参数。
func (f *scopeRepositoryFake) RecordEvent(_ context.Context, event db.NotificationEventInput) error {
	f.recorded = append(f.recorded, event)
	return nil
}

// SystemAlertChannels 返回系统告警渠道并记录调用次数。
func (f *scopeRepositoryFake) SystemAlertChannels(context.Context) ([]db.NotificationChannel, error) {
	f.systemCalls++
	return f.systemChannels, nil
}

// EnqueueOutbox 记录外部投递入队参数。
func (f *scopeRepositoryFake) EnqueueOutbox(_ context.Context, messages []db.NotificationOutboxInput) error {
	f.enqueued = append(f.enqueued, messages...)
	return nil
}

// ClaimOutbox 返回空批次，作用域测试不驱动 worker。
func (f *scopeRepositoryFake) ClaimOutbox(context.Context, string, time.Time, int) ([]db.NotificationOutboxMessage, error) {
	return nil, nil
}

// GetChannel 返回空渠道，作用域测试不执行真实发送。
func (f *scopeRepositoryFake) GetChannel(context.Context, int64) (*db.NotificationChannel, error) {
	return nil, nil
}

// CompleteOutbox 返回未确认，作用域测试不驱动 worker。
func (f *scopeRepositoryFake) CompleteOutbox(context.Context, int64, string) (bool, error) {
	return false, nil
}

// MarkOutboxUncertain 返回未隔离，作用域测试不驱动 worker。
func (f *scopeRepositoryFake) MarkOutboxUncertain(context.Context, int64, string, string) (bool, error) {
	return false, nil
}

// RetryOutbox 返回未更新，作用域测试不驱动 worker。
func (f *scopeRepositoryFake) RetryOutbox(context.Context, int64, string, string, int64, bool) (bool, error) {
	return false, nil
}

// GetSetting 返回空系统设置，作用域测试不读取真实配置。
func (f *scopeRepositoryFake) GetSetting(context.Context, string) (string, error) {
	return "", nil
}

// TestNotifyEventTenantScope 验证租户事件写入所属用户的站内事件并投递到租户渠道。
func TestNotifyEventTenantScope(t *testing.T) {
	// fake 是本次测试使用的通知仓储替身。
	fake := &scopeRepositoryFake{
		ownerUserID:     7,
		accountChannels: []db.NotificationChannel{{ID: 11, Name: "租户渠道", Type: "webhook"}},
		systemChannels:  []db.NotificationChannel{{ID: 22, Name: "系统渠道", Type: "webhook"}},
	}
	// notifier 是使用替身仓储构造的通知器。
	notifier := NewWithRepository("", fake, nilLogger())
	notifier.notifyEvent(context.Background(), NotificationEvent{
		AccountID: "cid", Type: EventAccountOffline, Level: "warn", Title: "账号掉线", Body: "正文",
	}, "tenant-idem-1")
	if len(fake.recorded) != 1 {
		t.Fatalf("站内事件数量=%d", len(fake.recorded))
	}
	// event 是实际写入的站内事件。
	event := fake.recorded[0]
	if event.Scope != db.NotificationScopeTenant || event.OwnerUserID != 7 || event.AccountID != "cid" || event.EventType != EventAccountOffline {
		t.Fatalf("站内事件=%+v", event)
	}
	if len(fake.enqueued) != 1 || fake.enqueued[0].ChannelID != 11 {
		t.Fatalf("租户入队=%+v", fake.enqueued)
	}
	if fake.systemCalls != 0 {
		t.Fatalf("租户事件不应查询系统渠道，实际=%d", fake.systemCalls)
	}
}

// TestNotifyEventSystemScope 验证系统事件写入系统视图并只投递到系统告警渠道。
func TestNotifyEventSystemScope(t *testing.T) {
	// fake 是本次测试使用的通知仓储替身。
	fake := &scopeRepositoryFake{
		accountChannels: []db.NotificationChannel{{ID: 11, Name: "租户渠道", Type: "webhook"}},
		systemChannels:  []db.NotificationChannel{{ID: 22, Name: "系统渠道", Type: "webhook"}},
	}
	// notifier 是使用替身仓储构造的通知器。
	notifier := NewWithRepository("", fake, nilLogger())
	notifier.notifyEvent(context.Background(), NotificationEvent{
		Scope: ScopeSystem, Type: EventBusinessSilence, Level: "warn", Title: "业务静默", Body: "正文",
	}, "system-idem-1")
	if len(fake.recorded) != 1 {
		t.Fatalf("站内事件数量=%d", len(fake.recorded))
	}
	// event 是实际写入的系统站内事件。
	event := fake.recorded[0]
	if event.Scope != db.NotificationScopeSystem || event.OwnerUserID != 0 || event.AccountID != "" || event.EventType != EventBusinessSilence {
		t.Fatalf("系统站内事件=%+v", event)
	}
	if len(fake.enqueued) != 1 || fake.enqueued[0].ChannelID != 22 {
		t.Fatalf("系统入队=%+v", fake.enqueued)
	}
	if fake.accountCalls != 0 || fake.ownerCalls != 0 {
		t.Fatalf("系统事件不应查询租户账号，account=%d owner=%d", fake.accountCalls, fake.ownerCalls)
	}
}

// TestNotifyEventTenantMutedStillRecordsInbox 验证账号静音只抑制外部投递，站内事件仍保留。
func TestNotifyEventTenantMutedStillRecordsInbox(t *testing.T) {
	// fake 是只返回所有者、不返回渠道的替身，模拟账号静音或没有启用渠道。
	fake := &scopeRepositoryFake{ownerUserID: 9}
	// notifier 是使用替身仓储构造的通知器。
	notifier := NewWithRepository("", fake, nilLogger())
	notifier.notifyEvent(context.Background(), NotificationEvent{
		AccountID: "cid", Type: EventAccountOffline, Title: "账号掉线", Body: "正文",
	}, "muted-idem-1")
	if len(fake.recorded) != 1 || fake.recorded[0].OwnerUserID != 9 {
		t.Fatalf("静音站内事件=%+v", fake.recorded)
	}
	if len(fake.enqueued) != 0 {
		t.Fatalf("静音不应外部投递，实际=%+v", fake.enqueued)
	}
}

// TestNotifyEventRejectsInvalidScopeAndMissingAccount 验证未知作用域与缺失账号不会产生事件或投递。
func TestNotifyEventRejectsInvalidScopeAndMissingAccount(t *testing.T) {
	// fake 是本次测试使用的通知仓储替身。
	fake := &scopeRepositoryFake{ownerUserID: 7}
	// notifier 是使用替身仓储构造的通知器。
	notifier := NewWithRepository("", fake, nilLogger())
	notifier.notifyEvent(context.Background(), NotificationEvent{Scope: "unknown", Type: EventAccountOffline}, "")
	notifier.notifyEvent(context.Background(), NotificationEvent{AccountID: "", Type: EventAccountOffline}, "")
	if len(fake.recorded) != 0 || len(fake.enqueued) != 0 || fake.ownerCalls != 0 || fake.accountCalls != 0 {
		t.Fatalf("非法事件不应写入或投递: recorded=%d enqueued=%d owner=%d account=%d",
			len(fake.recorded), len(fake.enqueued), fake.ownerCalls, fake.accountCalls)
	}
}

// TestFormatEventScopeAndLabels 验证系统事件正文与事件标签不再使用租户账号语义。
func TestFormatEventScopeAndLabels(t *testing.T) {
	// systemText 是系统事件的格式化正文。
	systemText := formatEvent(NotificationEvent{Scope: ScopeSystem, Type: EventBusinessSilence, Level: "warn", Title: "业务静默", Body: "正文"})
	if !strings.Contains(systemText, "范围: 系统实例") || strings.Contains(systemText, "账号:") {
		t.Fatalf("系统正文=%q", systemText)
	}
	// tenantText 是租户事件的格式化正文，必须保留账号行。
	tenantText := formatEvent(NotificationEvent{AccountID: "cid", Type: EventAccountOffline, Level: "warn", Title: "掉线", Body: "正文"})
	if !strings.Contains(tenantText, "账号: cid") {
		t.Fatalf("租户正文=%q", tenantText)
	}
	if eventLabel(EventBusinessSilence) != "业务静默" || eventLabel(EventSystemError) != "账号异常（未分类）" {
		t.Fatalf("事件标签不匹配: silence=%q system=%q", eventLabel(EventBusinessSilence), eventLabel(EventSystemError))
	}
}
