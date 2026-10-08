package db

import (
	"context"
	"database/sql"
)

// CardBackupRow 是卡密定时备份使用的原始行视图。
// APIConfig 保留数据库中的密文原样：备份文件不落任何明文凭证，
// 恢复时可直接把密文写回数据库，恢复路径不依赖数据密钥。
type CardBackupRow struct {
	ID           int64
	Name         string
	Type         string
	APIConfig    string
	TextContent  string
	DataContent  string
	ImageURL     string
	Description  string
	Enabled      bool
	DelaySeconds int
	IsMultiSpec  bool
	SpecName     string
	SpecValue    string
	UserID       int64
}

// AllForBackup 导出全部卡密的原始行（含 APIConfig 密文原样），供定时备份落盘。
// 返回顺序按 id 升序，保证同一份数据多次导出内容稳定；查询错误原样上抛。
func (c *Cards) AllForBackup(ctx context.Context) ([]CardBackupRow, error) {
	// rows、err 是卡密原始行游标及其查询错误；api_config 保持密文，不做解密读取。
	rows, err := c.DB.QueryContext(ctx,
		`SELECT id, name, type, api_config, text_content, data_content, image_url, description,
		        enabled, delay_seconds, is_multi_spec, spec_name, spec_value, user_id
		 FROM cards ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 是累计的备份行切片；空表时返回 nil 由上层序列化为 null 或空数组。
	var out []CardBackupRow
	for rows.Next() {
		// row、enabled、isMultiSpec、apiCfg 等是当前行的扫描目标；可空文本列统一用 NullString。
		var row CardBackupRow
		// enabled、isMultiSpec 是数据库中的整数开关标记。
		var enabled, isMultiSpec int
		// apiCfg、textContent、dataContent、imageURL、specName、specValue、desc 是可空文本列的扫描缓冲。
		var apiCfg, textContent, dataContent, imageURL, specName, specValue, desc sql.NullString
		// scanErr 是当前行扫描错误；任一行失败即整体失败，避免产出不完整备份。
		if scanErr := rows.Scan(&row.ID, &row.Name, &row.Type, &apiCfg, &textContent, &dataContent, &imageURL, &desc,
			&enabled, &row.DelaySeconds, &isMultiSpec, &specName, &specValue, &row.UserID); scanErr != nil {
			return nil, scanErr
		}
		row.APIConfig = apiCfg.String
		row.TextContent = textContent.String
		row.DataContent = dataContent.String
		row.ImageURL = imageURL.String
		row.Description = desc.String
		row.Enabled = enabled != 0
		row.IsMultiSpec = isMultiSpec != 0
		row.SpecName = specName.String
		row.SpecValue = specValue.String
		out = append(out, row)
	}
	return out, rows.Err()
}

// AllRulesForBackup 导出全部自动化规则及其动作，供定时备份落盘。
// 规则的 config_json 与动作的 ConfigJSON 原样导出；顺序按规则 id 升序，保证导出内容稳定。
func (a *AutomationRules) AllRulesForBackup(ctx context.Context) ([]AutomationRule, error) {
	// rows、err 是规则基础字段游标及其查询错误；不关联 item_info，备份只需要规则自身字段。
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id, r.user_id, r.cookie_id, r.item_id, r.name, r.trigger_type, r.enabled,
       r.priority, r.config_json, r.sku_migration_status, r.created_at, r.updated_at
  FROM automation_rules r
 ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	// out 是游标关闭后再补加载动作的规则切片。
	out := []AutomationRule{}
	for rows.Next() {
		// rule、enabled 是当前行扫描目标与整数启用标记。
		var rule AutomationRule
		// enabled 是数据库中的整数启用标记。
		var enabled int
		// scanErr 是当前规则基础字段扫描错误；任一行失败即整体失败。
		if scanErr := rows.Scan(&rule.ID, &rule.UserID, &rule.CookieID, &rule.ItemID, &rule.Name, &rule.TriggerType,
			&enabled, &rule.Priority, &rule.ConfigJSON, &rule.SKUMigrationStatus, &rule.CreatedAt, &rule.UpdatedAt); scanErr != nil {
			// 关闭游标再返回，避免连接池被失败路径占用。
			closeErr := rows.Close()
			if closeErr != nil {
				return nil, closeErr
			}
			return nil, scanErr
		}
		rule.Enabled = enabled != 0
		out = append(out, rule)
	}
	// rowsErr 是规则游标遍历错误。
	rowsErr := rows.Err()
	// closeErr 是规则游标关闭错误；两者任一失败都不得吞掉。
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	// 动作在游标关闭后逐规则加载，避免小连接池下嵌套查询死锁（与 ListPageForUser 同策略）。
	for index := range out {
		// acts、actErr 是当前规则的动作列表及其加载错误。
		acts, actErr := a.Actions(ctx, out[index].ID)
		if actErr != nil {
			return nil, actErr
		}
		out[index].Actions = acts
	}
	return out, nil
}
