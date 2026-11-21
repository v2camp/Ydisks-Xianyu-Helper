package adapter

import (
	"context"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// SystemAlertChannelRepository 把系统告警渠道的数据库能力限制在应用层定义的端口内。
type SystemAlertChannelRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// NewSystemAlertChannelRepository 构造系统告警渠道适配器；数据库依赖不完整时返回 nil。
func NewSystemAlertChannelRepository(store *db.Store) notificationsapp.SystemAlertChannelRepository {
	if store == nil || store.Notifications == nil {
		return nil
	}
	return SystemAlertChannelRepository{store: store}
}

// ListSystemChannels 查询系统告警渠道摘要，不读取或解密配置。
func (r SystemAlertChannelRepository) ListSystemChannels(ctx context.Context) ([]notificationsapp.ChannelSummary, error) {
	// rows、err 保存系统渠道非敏感摘要与查询错误。
	rows, err := r.store.Notifications.ListSystemChannelSummaries(ctx)
	if err != nil {
		return nil, err
	}
	// summaries 保存脱离数据库模型的系统渠道摘要。
	summaries := make([]notificationsapp.ChannelSummary, 0, len(rows))
	// row 是当前待转换的数据库系统渠道摘要。
	for _, row := range rows {
		summaries = append(summaries, notificationsapp.ChannelSummary{
			ID: row.ID, Name: row.Name, Type: row.Type, EventTypes: row.EventTypes,
			Enabled: row.Enabled, UserID: row.UserID,
		})
	}
	return summaries, nil
}

// GetSystemChannel 查询单条系统告警渠道完整记录，供应用层合并更新与编辑器回显。
func (r SystemAlertChannelRepository) GetSystemChannel(ctx context.Context, channelID int64) (*notificationsapp.ChannelRecord, error) {
	// row、err 保存系统渠道完整记录与查询错误。
	row, err := r.store.Notifications.GetSystemChannelRow(ctx, channelID)
	if err != nil {
		return nil, normalizeNotificationRepositoryError(err)
	}
	if row == nil {
		return nil, nil
	}
	return &notificationsapp.ChannelRecord{
		ID: row.ID, Name: row.Name, Type: row.Type, Config: row.Config,
		EventTypes: row.EventTypes, Enabled: row.Enabled, UserID: row.UserID,
	}, nil
}

// CreateSystemChannel 将应用输入转换为数据库行并创建系统告警渠道。
func (r SystemAlertChannelRepository) CreateSystemChannel(ctx context.Context, adminID int64, input notificationsapp.ChannelInput) (int64, error) {
	// row 保存待加密写入的系统渠道行；Config 不向 HTTP 返回。
	row := db.NotificationChannelRow{Name: input.Name, Type: input.Type, Config: input.Config, EventTypes: input.EventTypes, Enabled: input.Enabled}
	return r.store.Notifications.CreateSystemChannel(ctx, adminID, &row)
}

// UpdateSystemChannel 保存系统告警渠道；adminID 仅用于审计语义，数据库更新按渠道主键执行。
func (r SystemAlertChannelRepository) UpdateSystemChannel(ctx context.Context, _ int64, record notificationsapp.ChannelRecord) error {
	// row 保存待重新加密写入的系统渠道行。
	row := db.NotificationChannelRow{ID: record.ID, Name: record.Name, Type: record.Type, Config: record.Config, EventTypes: record.EventTypes, Enabled: record.Enabled, UserID: record.UserID}
	return normalizeNotificationRepositoryError(r.store.Notifications.UpdateSystemChannel(ctx, &row))
}

// DeleteSystemChannel 删除系统告警渠道并统一转换资源错误。
func (r SystemAlertChannelRepository) DeleteSystemChannel(ctx context.Context, channelID int64) error {
	return normalizeNotificationRepositoryError(r.store.Notifications.DeleteSystemChannel(ctx, channelID))
}

// 编译期确认系统告警渠道适配器覆盖应用层定义的全部能力。
var _ notificationsapp.SystemAlertChannelRepository = SystemAlertChannelRepository{}
