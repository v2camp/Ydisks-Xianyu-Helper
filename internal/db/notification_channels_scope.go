package db

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// notificationConfigOwner 返回通知渠道配置加解密使用的归属方。
// 系统渠道固定使用 "system"，避免多个管理员先后编辑时因重加密 owner 变化而无法解密。
func notificationConfigOwner(scope string, userID int64) string {
	if scope == NotificationScopeSystem {
		return NotificationScopeSystem
	}
	return strconv.FormatInt(userID, 10)
}

// ListSystemAlertChannels 读取全部启用的系统告警渠道，并解密配置供通知器直接发送。
func (n *Notifications) ListSystemAlertChannels(ctx context.Context) ([]NotificationChannel, error) {
	// rows、err 保存系统渠道查询结果与数据库错误。
	rows, err := n.DB.QueryContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), COALESCE(user_id,1)
		   FROM notification_channels
		  WHERE scope=? AND enabled=1
		  ORDER BY id`, NotificationScopeSystem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// channels 保存解密后的系统告警渠道。
	var channels []NotificationChannel
	for rows.Next() {
		// channel 是当前遍历到的系统渠道；userID 是创建管理员，仅用于审计。
		var channel NotificationChannel
		// userID 是创建该渠道的管理员主键，不参与系统渠道配置解密。
		var userID int64
		// scanErr 表示当前系统渠道行读取失败。
		if scanErr := rows.Scan(&channel.ID, &channel.Name, &channel.Type, &channel.Config,
			&channel.EventTypes, &userID); scanErr != nil {
			return nil, scanErr
		}
		channel.UserID = userID
		channel.Scope = NotificationScopeSystem
		// config、decryptErr 保存解密后的渠道配置与解密错误。
		config, decryptErr := n.codec.decrypt("notification-config", NotificationScopeSystem, channel.Config)
		if decryptErr != nil {
			return nil, decryptErr
		}
		channel.Config = config
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

// ListSystemChannelSummaries 查询系统告警渠道的非敏感摘要，不读取或解密配置。
func (n *Notifications) ListSystemChannelSummaries(ctx context.Context) ([]NotificationChannelSummaryRow, error) {
	// rows、err 保存系统渠道摘要查询结果与数据库错误。
	rows, err := n.DB.QueryContext(ctx,
		`SELECT id, name, type, COALESCE(event_types,''), enabled, COALESCE(user_id,1)
		   FROM notification_channels
		  WHERE scope=?
		  ORDER BY id DESC`, NotificationScopeSystem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// summaries 保存不含渠道配置的系统渠道摘要。
	summaries := make([]NotificationChannelSummaryRow, 0)
	for rows.Next() {
		// summary 是当前遍历到的系统渠道摘要；enabledValue 是数据库布尔值。
		var summary NotificationChannelSummaryRow
		// enabledValue 保存数据库中启用标记的整数表示。
		var enabledValue int
		// scanErr 表示当前摘要行读取失败。
		if scanErr := rows.Scan(&summary.ID, &summary.Name, &summary.Type, &summary.EventTypes,
			&enabledValue, &summary.UserID); scanErr != nil {
			return nil, scanErr
		}
		summary.Enabled = enabledValue != 0
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// GetSystemChannelRow 按主键读取系统告警渠道的完整记录；未找到返回 nil。
func (n *Notifications) GetSystemChannelRow(ctx context.Context, channelID int64) (*NotificationChannelRow, error) {
	// row 保存系统渠道完整记录查询结果。
	row := n.DB.QueryRowContext(ctx,
		`SELECT id, name, type, config, COALESCE(event_types,''), enabled, COALESCE(user_id,1)
		   FROM notification_channels
		  WHERE id=? AND scope=?`, channelID, NotificationScopeSystem)
	// channel 是当前系统渠道记录；enabledValue 是数据库布尔值。
	var channel NotificationChannelRow
	// enabledValue 保存数据库中启用标记的整数表示。
	var enabledValue int
	// err 表示读取系统渠道失败；未找到返回 nil。
	if err := row.Scan(&channel.ID, &channel.Name, &channel.Type, &channel.Config,
		&channel.EventTypes, &enabledValue, &channel.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	channel.Enabled = enabledValue != 0
	// config、decryptErr 保存解密后的系统渠道配置与解密错误。
	config, decryptErr := n.codec.decrypt("notification-config", NotificationScopeSystem, channel.Config)
	if decryptErr != nil {
		return nil, decryptErr
	}
	channel.Config = config
	return &channel, nil
}

// CreateSystemChannel 创建系统告警渠道；adminID 记入 user_id 作为创建者审计，加密归属固定为 system。
func (n *Notifications) CreateSystemChannel(ctx context.Context, adminID int64, channel *NotificationChannelRow) (int64, error) {
	// config、err 保存使用系统归属加密后的渠道配置与加密错误。
	config, err := n.codec.encrypt("notification-config", NotificationScopeSystem, channel.Config)
	if err != nil {
		return 0, err
	}
	return insertReturningID(ctx, n.DB, n.Dialect,
		`INSERT INTO notification_channels (name, type, config, event_types, enabled, user_id, scope)
		 VALUES (?,?,?,?,?,?,?)`,
		channel.Name, channel.Type, config, channel.EventTypes, boolToInt(channel.Enabled), adminID, NotificationScopeSystem)
}

// UpdateSystemChannel 更新系统告警渠道，保留原创建者 user_id 与 system 加密归属。
func (n *Notifications) UpdateSystemChannel(ctx context.Context, channel *NotificationChannelRow) error {
	// config、err 保存使用系统归属重新加密后的渠道配置与加密错误。
	config, err := n.codec.encrypt("notification-config", NotificationScopeSystem, channel.Config)
	if err != nil {
		return err
	}
	// res、err 保存更新结果与数据库错误。
	res, err := n.DB.ExecContext(ctx,
		`UPDATE notification_channels
		    SET name=?, type=?, config=?, event_types=?, enabled=?, updated_at=CURRENT_TIMESTAMP
		  WHERE id=? AND scope=?`,
		channel.Name, channel.Type, config, channel.EventTypes, boolToInt(channel.Enabled), channel.ID, NotificationScopeSystem)
	if err != nil {
		return err
	}
	// affected 是命中并更新的系统渠道数量。
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteSystemChannel 删除系统告警渠道；未命中时返回统一未找到错误。
func (n *Notifications) DeleteSystemChannel(ctx context.Context, channelID int64) error {
	// res、err 保存删除结果与数据库错误。
	res, err := n.DB.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=? AND scope=?`, channelID, NotificationScopeSystem)
	if err != nil {
		return err
	}
	// affected 是实际删除的系统渠道数量。
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return err
}
