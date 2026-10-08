package adapter

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"xianyu-go/internal/backup"
	"xianyu-go/internal/db"
)

// backupIntervalSettingKey 是备份间隔在 system_settings 中的键名。
const backupIntervalSettingKey = "backup_interval_hours"

// BackupSource 把数据库仓储投影成定时备份服务所需的最小读取边界。
// 卡密 APIConfig 与规则 ConfigJSON 全程保持数据库原样，不经过解密，避免明文落盘。
type BackupSource struct {
	// store 是已迁移完成的数据库仓储入口；只读使用。
	store *db.Store
}

// NewBackupSource 构造备份读取投影；store 必须非空。
func NewBackupSource(store *db.Store) *BackupSource {
	return &BackupSource{store: store}
}

// Cards 导出全部卡密原始行并转换成备份记录；APIConfig 保持密文原样。
func (s *BackupSource) Cards(ctx context.Context) ([]backup.CardRecord, error) {
	// rows、err 是数据库原始行及其查询错误。
	rows, err := s.store.Cards.AllForBackup(ctx)
	if err != nil {
		return nil, err
	}
	// out 是转换后的备份记录；行数与原始行一一对应。
	out := make([]backup.CardRecord, 0, len(rows))
	// row 是当前遍历的卡密原始行。
	for _, row := range rows {
		out = append(out, backup.CardRecord{
			ID: row.ID, Name: row.Name, Type: row.Type, APIConfig: row.APIConfig,
			TextContent: row.TextContent, DataContent: row.DataContent, ImageURL: row.ImageURL,
			Description: row.Description, Enabled: row.Enabled, DelaySeconds: row.DelaySeconds,
			IsMultiSpec: row.IsMultiSpec, SpecName: row.SpecName, SpecValue: row.SpecValue, UserID: row.UserID,
		})
	}
	return out, nil
}

// AutomationRules 导出全部自动化规则（含动作）并转换成备份记录。
func (s *BackupSource) AutomationRules(ctx context.Context) ([]backup.RuleRecord, error) {
	// rules、err 是数据库规则及其动作；config_json 原样保留。
	rules, err := s.store.Automation.AllRulesForBackup(ctx)
	if err != nil {
		return nil, err
	}
	// out 是转换后的规则记录切片。
	out := make([]backup.RuleRecord, 0, len(rules))
	// rule 是当前遍历的规则原始行。
	for _, rule := range rules {
		// record 是当前规则的备份记录；Actions 逐条投影。
		record := backup.RuleRecord{
			ID: rule.ID, UserID: rule.UserID, CookieID: rule.CookieID, ItemID: rule.ItemID,
			Name: rule.Name, TriggerType: rule.TriggerType, Enabled: rule.Enabled, Priority: rule.Priority,
			ConfigJSON: rule.ConfigJSON, SKUMigrationStatus: rule.SKUMigrationStatus,
			CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
			Actions: make([]backup.ActionRecord, 0, len(rule.Actions)),
		}
		// act 是当前遍历的规则动作原始行。
		for _, act := range rule.Actions {
			record.Actions = append(record.Actions, backup.ActionRecord{
				ID: act.ID, ActionType: act.ActionType, CardID: act.CardID, CardName: act.CardName,
				DeliveryCount: act.DeliveryCount, MessageTemplate: act.MessageTemplate,
				DelaySeconds: act.DelaySeconds, ConfigJSON: act.ConfigJSON,
				Enabled: act.Enabled, SortOrder: act.SortOrder,
			})
		}
		out = append(out, record)
	}
	return out, nil
}

// IntervalHours 读取用户配置的备份间隔（小时）。
// 未配置或空白返回默认 24；0 是合法值（用户关闭定时备份）；其余非法值上抛错误，
// 由备份服务按默认间隔兜底，避免解析失败导致备份停摆。
func (s *BackupSource) IntervalHours(ctx context.Context) (int, error) {
	// raw 是设置表中的原始文本；键不存在时 Get 返回空串。
	raw, err := s.store.Settings.Get(ctx, backupIntervalSettingKey)
	if err != nil {
		return 0, err
	}
	// trimmed 是去除空白后的配置值。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 24, nil
	}
	// hours 是解析出的间隔小时数。
	hours, parseErr := strconv.Atoi(trimmed)
	if parseErr != nil || hours < 0 {
		return 0, fmt.Errorf("backup_interval_hours 配置 %q 不是合法的间隔小时数", raw)
	}
	return hours, nil
}

// 编译期保证 BackupSource 满足备份服务的读取接口。
var _ backup.Source = (*BackupSource)(nil)
