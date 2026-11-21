package adapter

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/notify"
)

// TestBusinessSilenceAlerterUsesSystemScope 验证业务静默告警进入系统视图，不再借用租户账号。
func TestBusinessSilenceAlerterUsesSystemScope(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// logger 丢弃测试日志，避免噪声。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// alerter 是使用真实通知器与数据库构造的静默告警出口。
	alerter := &businessSilenceAlerter{notifier: notify.New("", store, logger), logger: logger}
	alerter.NotifyBusinessSilence(context.Background(), "业务静默", "长时间没有业务事件")
	// events、err 保存系统视图事件与读取错误。
	events, err := store.Notifications.ListSystemEvents(context.Background(), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("系统事件=%+v err=%v", events, err)
	}
	if events[0].Scope != db.NotificationScopeSystem || events[0].EventType != notify.EventBusinessSilence || events[0].Body != "长时间没有业务事件" {
		t.Fatalf("系统事件=%+v", events[0])
	}
	// tenantEvents、tenantErr 保存租户视图事件；系统告警不得落入任何租户视图。
	tenantEvents, tenantErr := store.Notifications.ListEventsForUser(context.Background(), 1, 10)
	if tenantErr != nil || len(tenantEvents) != 0 {
		t.Fatalf("租户事件=%+v err=%v", tenantEvents, tenantErr)
	}
}

// TestBusinessSilenceAlerterNilSafe 验证告警出口未装配时安全跳过。
func TestBusinessSilenceAlerterNilSafe(t *testing.T) {
	// nilAlerter 验证空接收者不会触发 panic。
	var nilAlerter *businessSilenceAlerter
	nilAlerter.NotifyBusinessSilence(context.Background(), "标题", "正文")
	// emptyAlerter 验证通知器缺失时同样安全跳过。
	emptyAlerter := &businessSilenceAlerter{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	emptyAlerter.NotifyBusinessSilence(context.Background(), "标题", "正文")
}
