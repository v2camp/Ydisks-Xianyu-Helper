package adapter

import (
	"context"
	"errors"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// NotificationChannelRepository 将通知渠道数据库能力限制在应用层定义的端口内。
type NotificationChannelRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用；配置字段不会进入摘要模型。
	store *db.Store
}

// NewNotificationChannelRepository 构造通知渠道应用端口适配器；数据库依赖不完整时返回 nil。
func NewNotificationChannelRepository(store *db.Store) notificationsapp.ChannelRepository {
	if store == nil || store.Notifications == nil || store.Cookies == nil {
		return nil
	}
	return NotificationChannelRepository{store: store}
}

// ListChannels 查询渠道摘要并丢弃敏感配置。
func (r NotificationChannelRepository) ListChannels(ctx context.Context, userID int64) ([]notificationsapp.ChannelSummary, error) {
	// rows、err 保存数据库渠道非敏感摘要；列表路径不会读取或解密 Config。
	rows, err := r.store.Notifications.ListChannelSummariesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// summaries 保存不含渠道配置的应用摘要。
	summaries := make([]notificationsapp.ChannelSummary, 0, len(rows))
	// row 表示当前待转换的数据库渠道行。
	for _, row := range rows {
		summaries = append(summaries, notificationsapp.ChannelSummary{ID: row.ID, Name: row.Name, Type: row.Type, EventTypes: row.EventTypes, Enabled: row.Enabled, UserID: row.UserID})
	}
	return summaries, nil
}

// GetChannelForUpdate 查询归属渠道的完整记录，仅供应用层合并部分更新。
func (r NotificationChannelRepository) GetChannelForUpdate(ctx context.Context, channelID, userID int64) (*notificationsapp.ChannelRecord, error) {
	// row、err 保存带用户归属的数据库渠道记录。
	row, err := r.store.Notifications.GetChannelRowForUser(ctx, channelID, userID)
	if err != nil {
		return nil, normalizeNotificationRepositoryError(err)
	}
	if row == nil {
		return nil, nil
	}
	return &notificationsapp.ChannelRecord{ID: row.ID, Name: row.Name, Type: row.Type, Config: row.Config, EventTypes: row.EventTypes, Enabled: row.Enabled, UserID: row.UserID}, nil
}

// CreateChannel 将应用输入转换为数据库行并创建渠道。
func (r NotificationChannelRepository) CreateChannel(ctx context.Context, userID int64, input notificationsapp.ChannelInput) (int64, error) {
	// row 保存待加密写入数据库的渠道行；Config 不向 HTTP 返回。
	row := db.NotificationChannelRow{Name: input.Name, Type: input.Type, Config: input.Config, EventTypes: input.EventTypes, Enabled: input.Enabled, UserID: userID}
	return r.store.Notifications.CreateChannel(ctx, &row)
}

// UpdateChannel 将应用层完整渠道记录转换为数据库更新，并归一化资源错误。
func (r NotificationChannelRepository) UpdateChannel(ctx context.Context, userID int64, record notificationsapp.ChannelRecord) error {
	// row 保存待加密写入数据库的渠道行；调用方已完成归属和字段校验。
	row := db.NotificationChannelRow{ID: record.ID, Name: record.Name, Type: record.Type, Config: record.Config, EventTypes: record.EventTypes, Enabled: record.Enabled, UserID: userID}
	return normalizeNotificationRepositoryError(r.store.Notifications.UpdateChannelForUser(ctx, &row, userID))
}

// DeleteChannel 删除用户拥有的渠道并统一转换资源错误。
func (r NotificationChannelRepository) DeleteChannel(ctx context.Context, channelID, userID int64) error {
	return normalizeNotificationRepositoryError(r.store.Notifications.DeleteChannelForUser(ctx, channelID, userID))
}

// OwnsChannel 查询通知渠道归属，不读取配置内容。
func (r NotificationChannelRepository) OwnsChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	return r.store.Notifications.OwnsChannel(ctx, channelID, userID)
}

// normalizeNotificationRepositoryError 将数据库归属错误转换为应用错误，隐藏基础设施类型。
func normalizeNotificationRepositoryError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return notificationsapp.ErrChannelNotFound
	}
	if errors.Is(err, db.ErrForbidden) {
		return notificationsapp.ErrChannelForbidden
	}
	return err
}

// NotificationUncertainRepository 将通知不确定状态查询限制在非敏感摘要端口内。
type NotificationUncertainRepository struct {
	// store 保存数据库聚合入口，仅在脱敏摘要适配器内使用。
	store *db.Store
}

// NewNotificationUncertainRepository 创建通知不确定状态查询适配器；数据库依赖缺失时返回 nil。
func NewNotificationUncertainRepository(store *db.Store) notificationsapp.Repository {
	if store == nil || store.Notifications == nil {
		return nil
	}
	return NotificationUncertainRepository{store: store}
}

// ListUncertainForUser 查询指定用户的不确定通知摘要，并转换为应用模型。
func (r NotificationUncertainRepository) ListUncertainForUser(ctx context.Context, userID int64, limit int) ([]notificationsapp.UncertainSummary, error) {
	// rows、err 保存数据库摘要查询结果及错误。
	rows, err := r.store.Notifications.ListUncertainOutboxForUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	return newNotificationUncertainApplicationSummaries(rows), nil
}

// CountUncertainForUser 统计指定用户的不确定通知数量。
func (r NotificationUncertainRepository) CountUncertainForUser(ctx context.Context, userID int64) (int, error) {
	return r.store.Notifications.CountUncertainOutboxForUser(ctx, userID)
}

// ListUncertainForAdmin 查询全局不确定通知摘要，并转换为应用模型。
func (r NotificationUncertainRepository) ListUncertainForAdmin(ctx context.Context, limit int) ([]notificationsapp.UncertainSummary, error) {
	// rows、err 保存数据库全局摘要查询结果及错误。
	rows, err := r.store.Notifications.ListUncertainOutboxForAdmin(ctx, limit)
	if err != nil {
		return nil, err
	}
	return newNotificationUncertainApplicationSummaries(rows), nil
}

// CountUncertainForAdmin 统计全局不确定通知数量。
func (r NotificationUncertainRepository) CountUncertainForAdmin(ctx context.Context) (int, error) {
	return r.store.Notifications.CountUncertainOutboxForAdmin(ctx)
}

// newNotificationUncertainApplicationSummaries 将数据库摘要转换为不含正文的应用模型。
func newNotificationUncertainApplicationSummaries(rows []db.NotificationUncertainSummary) []notificationsapp.UncertainSummary {
	// summaries 保存脱离数据库模型的非敏感通知摘要。
	summaries := make([]notificationsapp.UncertainSummary, 0, len(rows))
	// row 表示当前待转换的数据库通知摘要。
	for _, row := range rows {
		summaries = append(summaries, notificationsapp.UncertainSummary{
			ID: row.ID, ChannelID: row.ChannelID, OwnerUserID: row.OwnerUserID,
			EventType: row.EventType, AttemptCount: row.AttemptCount,
			UncertainAt: row.UncertainAt, HasError: row.HasError,
		})
	}
	return summaries
}

// 编译期确认通知渠道适配器覆盖应用层定义的全部能力。
var _ notificationsapp.ChannelRepository = NotificationChannelRepository{}

// 编译期确认通知不确定状态适配器覆盖应用层定义的全部能力。
var _ notificationsapp.Repository = NotificationUncertainRepository{}
