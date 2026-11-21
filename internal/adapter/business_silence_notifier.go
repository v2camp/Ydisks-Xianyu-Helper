package adapter

import (
	"context"
	"log/slog"

	"xianyu-go/internal/notify"
)

// businessSilenceAlerter 把进程级业务静默告警交给系统告警渠道。
//
// 业务静默是实例级事件，不属于任何租户账号；这里不再选择「宿主账号」，
// 而是由通知器按 system 作用域投递给管理员配置的系统告警渠道，避免实例告警落入某个租户。
type businessSilenceAlerter struct {
	// notifier 是进程启动时装配完成的通知出口；为 nil 时跳过发送。
	notifier *notify.Notifier
	// logger 记录告警提交结果，不包含渠道配置等敏感内容。
	logger *slog.Logger
}

// NotifyBusinessSilence 发送一条系统级业务静默告警；通知器不可用时只记日志，
// 不向看门狗返回错误，保证告警路由故障不影响业务扫描循环。
func (a *businessSilenceAlerter) NotifyBusinessSilence(ctx context.Context, title, body string) {
	if a == nil || a.notifier == nil {
		if a != nil && a.logger != nil {
			a.logger.Warn("业务静默告警未发送：通知器未装配")
		}
		return
	}
	a.notifier.NotifySystemEvent(ctx, notify.EventBusinessSilence, "warn", title, body)
}
