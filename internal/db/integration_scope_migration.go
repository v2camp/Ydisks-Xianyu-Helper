package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// integrationScopeMigrationName 是一次性数据迁移的稳定标记名。
const integrationScopeMigrationName = "integration_scope_v1"

// IntegrationMigrationReport 是全局与渠道级集成凭据折叠到用户级的迁移结果。
type IntegrationMigrationReport struct {
	// Skipped 表示迁移此前已完成，本次未做任何写入。
	Skipped bool
	// AdminUserID 是接收旧全局配置的管理员用户；0 表示没有可迁移配置。
	AdminUserID int64
	// GlobalKeys 是实际迁移并删除的全局设置键数量。
	GlobalKeys int
	// Channels 是实际折叠凭据的通知渠道数量。
	Channels int
	// Conflicts 是用户级已有不同值而未覆盖的键数量。
	Conflicts int
}

// integrationGlobalKeys 是需要从系统设置迁移到管理员用户的键。
var integrationGlobalKeys = []string{
	"ai_api_url", "ai_base_url", "ai_model", "ai_api_key",
	"smtp_server", "smtp_port", "smtp_user", "smtp_password",
	"smtp_from_name", "smtp_from_address", "smtp_use_tls", "smtp_use_ssl",
	"qqbot.app_id", "qqbot.app_secret", "qqbot.commands_enabled", "qqbot.command_openids",
}

// channelCredentialKeys 是渠道配置中需要折叠到用户级的键。
var channelCredentialKeys = map[string]string{
	"app_id":            "qqbot.app_id",
	"app_secret":        "qqbot.app_secret",
	"smtp_server":       "smtp_server",
	"smtp_port":         "smtp_port",
	"smtp_user":         "smtp_user",
	"smtp_password":     "smtp_password",
	"smtp_from_name":    "smtp_from_name",
	"smtp_from_address": "smtp_from_address",
	"smtp_use_tls":      "smtp_use_tls",
	"smtp_use_ssl":      "smtp_use_ssl",
}

// MigrateIntegrationSettingsV1 把全局与渠道级集成凭据折叠到管理员用户，并写入一次性迁移标记。
// 迁移失败时不得标记完成；调用方应让进程启动失败，避免继续使用半迁移状态。
func (s *Store) MigrateIntegrationSettingsV1(ctx context.Context) (IntegrationMigrationReport, error) {
	// report 保存本次迁移的计数，便于启动日志输出。
	var report IntegrationMigrationReport
	if s == nil || s.DB == nil || s.Settings == nil || s.UserSettings == nil {
		return report, errors.New("集成配置迁移依赖未初始化")
	}
	// applied 表示迁移标记是否已存在。
	var applied bool
	// err 表示读取一次性迁移标记失败。
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM data_migrations WHERE name=?)`, integrationScopeMigrationName).Scan(&applied); err != nil {
		return report, fmt.Errorf("读取集成配置迁移标记失败: %w", err)
	}
	if applied {
		report.Skipped = true
		return report, nil
	}
	// adminUserID 是旧全局配置的接收者；没有可迁移配置时允许为 0。
	adminUserID, adminErr := s.integrationMigrationAdminID(ctx)
	if adminErr != nil {
		return report, adminErr
	}
	report.AdminUserID = adminUserID
	// hasLegacy 表示是否存在需要迁移的全局键或渠道级凭据。
	hasLegacy, legacyErr := s.hasLegacyIntegrationSettings(ctx)
	if legacyErr != nil {
		return report, legacyErr
	}
	if adminUserID <= 0 {
		// userCount 是当前数据库中的用户数量；全新数据库允许直接丢弃出厂默认全局键。
		var userCount int
		// err 表示统计用户数量失败。
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
			return report, fmt.Errorf("统计用户数量失败: %w", err)
		}
		if userCount == 0 {
			// err 表示删除出厂默认全局键失败。
			if err := s.deleteLegacyGlobalKeys(ctx); err != nil {
				return report, err
			}
			// err 表示写入迁移完成标记失败。
			if err := s.markIntegrationMigrationDone(ctx); err != nil {
				return report, err
			}
			report.Skipped = true
			return report, nil
		}
		if hasLegacy {
			return report, errors.New("存在旧集成配置但没有可用管理员用户，无法迁移")
		}
		// err 表示写入迁移完成标记失败。
		if err := s.markIntegrationMigrationDone(ctx); err != nil {
			return report, err
		}
		report.Skipped = true
		return report, nil
	}
	// migrate 全局键：先读后写，成功后再删除全局键。
	// key 是当前待迁移的全局集成配置键。
	for _, key := range integrationGlobalKeys {
		// value、readErr 是旧全局集成配置值与读取错误。
		value, readErr := s.Settings.Get(ctx, key)
		if readErr != nil {
			return report, fmt.Errorf("读取旧集成配置 %s 失败: %w", key, readErr)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		// existing 是管理员用户级已有的值；非空时不覆盖，避免迁移冲掉新配置。
		existing, existingErr := s.UserSettings.GetForUser(ctx, adminUserID, key)
		if existingErr != nil {
			return report, fmt.Errorf("读取用户集成配置 %s 失败: %w", key, existingErr)
		}
		if strings.TrimSpace(existing) == "" {
			// err 表示把旧值写入管理员用户级配置失败。
			if err := s.UserSettings.SetForUser(ctx, adminUserID, key, value); err != nil {
				return report, fmt.Errorf("迁移旧集成配置 %s 失败: %w", key, err)
			}
		} else if existing != value {
			report.Conflicts++
		}
		// err 表示删除已迁移的全局设置失败。
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM system_settings WHERE `+dialectQuote(s.Dialect, "key")+`=?`, key); err != nil {
			return report, fmt.Errorf("删除旧集成配置 %s 失败: %w", key, err)
		}
		report.GlobalKeys++
	}
	// folded、conflicts 是渠道级凭据折叠结果。
	folded, conflicts, foldErr := s.foldChannelCredentials(ctx, adminUserID)
	if foldErr != nil {
		return report, foldErr
	}
	report.Channels = folded
	report.Conflicts += conflicts
	// err 表示写入迁移完成标记失败。
	if err := s.markIntegrationMigrationDone(ctx); err != nil {
		return report, err
	}
	return report, nil
}

// integrationMigrationAdminID 返回接收旧全局配置的管理员用户 ID。
func (s *Store) integrationMigrationAdminID(ctx context.Context) (int64, error) {
	// adminExpr、activeExpr 按方言归一布尔列比较，避免 PostgreSQL 的 boolean/integer 不匹配。
	adminExpr, activeExpr := "is_admin = 1", "COALESCE(is_active,1) = 1"
	if s.Dialect == DialectPostgres {
		adminExpr, activeExpr = "is_admin = TRUE", "COALESCE(is_active,TRUE) = TRUE"
	}
	// adminUserID 是接收旧全局配置的管理员用户主键。
	var adminUserID int64
	// err 表示查询管理员用户失败。
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE `+adminExpr+` AND `+activeExpr+` ORDER BY id LIMIT 1`).Scan(&adminUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("查询管理员用户失败: %w", err)
	}
	return adminUserID, nil
}

// hasLegacyIntegrationSettings 判断是否仍有全局集成键或渠道级凭据需要迁移。
func (s *Store) hasLegacyIntegrationSettings(ctx context.Context) (bool, error) {
	// key 是当前待检查的全局集成配置键。
	for _, key := range integrationGlobalKeys {
		// value、err 是当前全局键的值与读取错误。
		value, err := s.Settings.Get(ctx, key)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(value) != "" {
			return true, nil
		}
	}
	// count 是带有配置 JSON 的通知渠道数量；渠道级凭据在折叠阶段再精确判断。
	var count int
	// err 表示统计待检查通知渠道失败。
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_channels WHERE config <> ''`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// foldChannelCredentials 把渠道配置中的凭据复制到渠道所有者用户，并从配置中移除。
func (s *Store) foldChannelCredentials(ctx context.Context, adminUserID int64) (int, int, error) {
	// rows 是所有通知渠道的配置快照。
	rows, err := s.DB.QueryContext(ctx, `SELECT id, COALESCE(user_id,0), config FROM notification_channels ORDER BY id`)
	if err != nil {
		return 0, 0, fmt.Errorf("读取通知渠道配置失败: %w", err)
	}
	defer rows.Close()
	// folded、conflicts 是实际折叠的渠道数与冲突数。
	folded, conflicts := 0, 0
	for rows.Next() {
		// channelID、ownerID、rawConfig 是当前渠道的主键、所有者与密文配置。
		var channelID, ownerID int64
		// channelID、ownerID、rawConfig 是渠道主键、所有者与密文配置。
		var rawConfig string
		// err 表示扫描当前渠道记录失败。
		if err := rows.Scan(&channelID, &ownerID, &rawConfig); err != nil {
			return 0, 0, err
		}
		if ownerID <= 0 {
			ownerID = adminUserID
		}
		if strings.TrimSpace(rawConfig) == "" {
			continue
		}
		// config 是解密后的渠道配置 JSON。
		config, decryptErr := s.Notifications.codec.decrypt("notification-config", strconv.FormatInt(ownerID, 10), rawConfig)
		if decryptErr != nil {
			return 0, 0, fmt.Errorf("解密通知渠道 %d 配置失败: %w", channelID, decryptErr)
		}
		// values 是渠道配置中可折叠到用户级的凭据。
		var parsed map[string]any
		// err 表示解析渠道配置 JSON 失败；非 JSON 历史配置保持原样。
		if err := json.Unmarshal([]byte(config), &parsed); err != nil {
			// 非 JSON 历史配置不阻断迁移；保持原样。
			continue
		}
		// hasCredential 表示当前渠道是否存在需要折叠的凭据。
		hasCredential := false
		// configKey、settingKey 是渠道配置键和对应的用户级设置键。
		for configKey, settingKey := range channelCredentialKeys {
			// rawValue、exists 是当前渠道配置值与是否存在。
			rawValue, exists := parsed[configKey]
			if !exists {
				continue
			}
			// text 是渠道配置值去空白后的文本。
			text := strings.TrimSpace(fmt.Sprintf("%v", rawValue))
			if text == "" {
				delete(parsed, configKey)
				continue
			}
			// existing 是用户级已有的值；非空且不同则保留用户级并记录冲突。
			existing, existingErr := s.UserSettings.GetForUser(ctx, ownerID, settingKey)
			if existingErr != nil {
				return 0, 0, existingErr
			}
			if strings.TrimSpace(existing) == "" {
				// err 表示把渠道级凭据写入用户级设置失败。
				if err := s.UserSettings.SetForUser(ctx, ownerID, settingKey, text); err != nil {
					return 0, 0, err
				}
			} else if existing != text {
				conflicts++
			}
			delete(parsed, configKey)
			hasCredential = true
		}
		if !hasCredential {
			continue
		}
		// 邮件渠道折叠后统一改为继承用户级 SMTP。
		parsed["use_custom_smtp"] = false
		// nextConfig 是移除凭据后的渠道配置。
		nextConfig, marshalErr := json.Marshal(parsed)
		if marshalErr != nil {
			return 0, 0, marshalErr
		}
		// encrypted 是重新加密后的渠道配置。
		encrypted, encryptErr := s.Notifications.codec.encrypt("notification-config", strconv.FormatInt(ownerID, 10), string(nextConfig))
		if encryptErr != nil {
			return 0, 0, encryptErr
		}
		// err 表示保存移除凭据后的渠道配置失败。
		if _, err := s.DB.ExecContext(ctx, `UPDATE notification_channels SET config=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, encrypted, channelID); err != nil {
			return 0, 0, err
		}
		folded++
	}
	// err 表示遍历通知渠道结果集失败。
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return folded, conflicts, nil
}

// deleteLegacyGlobalKeys 删除没有管理员可接收的出厂默认全局集成键。
func (s *Store) deleteLegacyGlobalKeys(ctx context.Context) error {
	// key 是当前待删除的旧全局集成配置键。
	for _, key := range integrationGlobalKeys {
		// err 表示删除当前旧全局键失败。
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM system_settings WHERE `+dialectQuote(s.Dialect, "key")+`=?`, key); err != nil {
			return fmt.Errorf("删除旧集成配置 %s 失败: %w", key, err)
		}
	}
	return nil
}

// markIntegrationMigrationDone 写入一次性迁移标记。
func (s *Store) markIntegrationMigrationDone(ctx context.Context) error {
	// err 表示写入一次性迁移标记失败。
	_, err := s.DB.ExecContext(ctx, `INSERT INTO data_migrations (name) SELECT ? WHERE NOT EXISTS (SELECT 1 FROM data_migrations WHERE name=?)`, integrationScopeMigrationName, integrationScopeMigrationName)
	return err
}
