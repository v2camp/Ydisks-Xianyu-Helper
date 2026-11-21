package adapter

import (
	"context"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// NotificationEventRepository 把站内通知事件的数据库能力限制在应用层定义的端口内。
type NotificationEventRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// NewNotificationEventRepository 构造站内通知事件适配器；数据库依赖不完整时返回 nil。
func NewNotificationEventRepository(store *db.Store) notificationsapp.EventRepository {
	if store == nil || store.Notifications == nil {
		return nil
	}
	return NotificationEventRepository{store: store}
}

// ListForUser 返回当前用户的租户事件。
func (r NotificationEventRepository) ListForUser(ctx context.Context, userID int64, limit int) ([]notificationsapp.EventSummary, error) {
	// rows、err 保存数据库事件摘要与查询错误。
	rows, err := r.store.Notifications.ListEventsForUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	return newNotificationEventSummaries(rows), nil
}

// CountUnreadForUser 统计当前用户的未读租户事件。
func (r NotificationEventRepository) CountUnreadForUser(ctx context.Context, userID int64) (int, error) {
	return r.store.Notifications.CountUnreadEventsForUser(ctx, userID)
}

// MarkReadForUser 标记当前用户的单条租户事件已读。
func (r NotificationEventRepository) MarkReadForUser(ctx context.Context, userID, eventID int64) (bool, error) {
	return r.store.Notifications.MarkEventReadForUser(ctx, userID, eventID)
}

// MarkAllReadForUser 标记当前用户全部租户事件已读。
func (r NotificationEventRepository) MarkAllReadForUser(ctx context.Context, userID int64) (int64, error) {
	return r.store.Notifications.MarkAllEventsReadForUser(ctx, userID)
}

// ListForAdmin 返回系统级事件。
func (r NotificationEventRepository) ListForAdmin(ctx context.Context, limit int) ([]notificationsapp.EventSummary, error) {
	// rows、err 保存数据库系统事件摘要与查询错误。
	rows, err := r.store.Notifications.ListSystemEvents(ctx, limit)
	if err != nil {
		return nil, err
	}
	return newNotificationEventSummaries(rows), nil
}

// CountUnreadForAdmin 统计未读系统级事件。
func (r NotificationEventRepository) CountUnreadForAdmin(ctx context.Context) (int, error) {
	return r.store.Notifications.CountUnreadSystemEvents(ctx)
}

// MarkReadForAdmin 标记单条系统级事件已读。
func (r NotificationEventRepository) MarkReadForAdmin(ctx context.Context, eventID int64) (bool, error) {
	return r.store.Notifications.MarkSystemEventRead(ctx, eventID)
}

// MarkAllReadForAdmin 标记全部系统级事件已读。
func (r NotificationEventRepository) MarkAllReadForAdmin(ctx context.Context) (int64, error) {
	return r.store.Notifications.MarkAllSystemEventsRead(ctx)
}

// newNotificationEventSummaries 把数据库事件摘要转换为脱离数据库模型的应用摘要。
func newNotificationEventSummaries(rows []db.NotificationEventSummary) []notificationsapp.EventSummary {
	// summaries 保存应用层事件摘要。
	summaries := make([]notificationsapp.EventSummary, 0, len(rows))
	// row 是当前待转换的数据库事件摘要。
	for _, row := range rows {
		summaries = append(summaries, notificationsapp.EventSummary{
			ID: row.ID, Scope: row.Scope, AccountID: row.AccountID, EventType: row.EventType,
			Level: row.Level, Title: row.Title, Body: row.Body, Read: row.Read, CreatedAt: row.CreatedAt,
		})
	}
	return summaries
}

// 编译期确认站内事件适配器覆盖应用层定义的全部能力。
var _ notificationsapp.EventRepository = NotificationEventRepository{}
