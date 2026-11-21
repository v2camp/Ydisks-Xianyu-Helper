package server

import (
	"context"
	"time"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// UncertainNotificationsPort 定义通知不确定状态的查询能力。
type UncertainNotificationsPort interface {
	ListForUser(context.Context, int64, int) ([]notificationsapp.UncertainSummary, int, error)
	ListForAdmin(context.Context, int) ([]notificationsapp.UncertainSummary, int, error)
}

// NotificationChannelsPort 定义通知渠道的管理能力。
type NotificationChannelsPort interface {
	ListChannels(context.Context, int64) ([]notificationsapp.ChannelSummary, error)
	GetChannelEditor(context.Context, int64, int64) (notificationsapp.ChannelEditor, error)
	CreateChannel(context.Context, int64, notificationsapp.ChannelInput) (int64, error)
	UpdateChannel(context.Context, int64, int64, notificationsapp.ChannelPatch) error
	DeleteChannel(context.Context, int64, int64) error
	TestChannel(context.Context, int64, int64, time.Time) error
}

// NotificationEventsPort 定义租户与系统站内通知事件的查询和已读能力。
type NotificationEventsPort interface {
	ListForUser(context.Context, int64, int) ([]notificationsapp.EventSummary, int, error)
	MarkReadForUser(context.Context, int64, int64) error
	MarkAllReadForUser(context.Context, int64) (int64, error)
	ListForAdmin(context.Context, int) ([]notificationsapp.EventSummary, int, error)
	MarkReadForAdmin(context.Context, int64) error
	MarkAllReadForAdmin(context.Context) (int64, error)
}

// SystemAlertChannelsPort 定义管理员系统告警渠道的管理能力。
type SystemAlertChannelsPort interface {
	List(context.Context) ([]notificationsapp.ChannelSummary, error)
	GetEditor(context.Context, int64) (notificationsapp.ChannelEditor, error)
	Create(context.Context, int64, notificationsapp.ChannelInput) (int64, error)
	Update(context.Context, int64, int64, notificationsapp.ChannelPatch) error
	Delete(context.Context, int64) error
	Test(context.Context, int64, time.Time) error
}
