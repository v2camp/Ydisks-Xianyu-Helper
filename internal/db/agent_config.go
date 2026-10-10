// agent_config.go 客服 Agent 账号级配置的持久化读写。
//
// 配置复用 ai_reply_settings 两列而不是新建表：该表已承载同一账号的模型凭证与业务
// 护栏，归属与敏感度一致，符合 AGENTS.md §1.5 对「敏感度或归属不同的类型禁止合并」
// 的反向判定。本文件只投影 agent_enabled 与 agent_preset，不读取 api_key。

package db

import (
	"context"
	"database/sql"
	"errors"
)

// AgentAccountConfig 是账号级客服 Agent 配置的持久化视图，不含任何模型凭证。
//
// 字段为 nil 表示该维度继承租户默认；两个字段同时为 nil 表示账号完全跟随租户。
type AgentAccountConfig struct {
	// CookieID 是配置归属账号。
	CookieID string
	// Enabled 为 nil 表示账号未覆盖开关。
	Enabled *bool
	// Preset 为 nil 表示账号未覆盖档位。
	Preset *string
}

// GetAgentConfig 读取账号级客服 Agent 配置。
//
// 以 cookies 为主表左连接配置表，因此两类情形被区分开：账号存在但从未写过配置时返回
// 全继承的空配置；账号不存在或不属于该租户时返回 found=false。调用方对 found=false
// 必须按未授权处理，禁止降级为继承值继续执行。
func (a *AIReply) GetAgentConfig(ctx context.Context, userID int64, cookieID string) (*AgentAccountConfig, bool, error) {
	// enabled 是开关列；NULL 表示该维度未覆盖。
	var enabled sql.NullInt64
	// preset 是档位列；NULL 表示该维度未覆盖。
	var preset sql.NullString
	// err 是账号配置查询错误；ErrNoRows 表示账号不存在或跨租户。
	err := a.DB.QueryRowContext(ctx, `
		SELECT r.agent_enabled, r.agent_preset
		  FROM cookies c LEFT JOIN ai_reply_settings r ON r.cookie_id = c.id
		 WHERE c.id = ? AND c.user_id = ?`, cookieID, userID).Scan(&enabled, &preset)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// config 是读取结果，两个维度默认都表示继承。
	config := &AgentAccountConfig{CookieID: cookieID}
	if enabled.Valid {
		// enabledValue 是开关列的有效取值，非零即开启。
		enabledValue := enabled.Int64 != 0
		config.Enabled = &enabledValue
	}
	if preset.Valid {
		// presetValue 是档位列的有效原文，合法性由应用层收敛。
		presetValue := preset.String
		config.Preset = &presetValue
	}
	return config, true, nil
}

// SetAgentConfig 保存账号级客服 Agent 配置；两个维度都为空表示恢复完全继承租户默认。
//
// 归属校验与写入分两步执行：账号在两步之间被删除时，由 ai_reply_settings 对 cookies
// 的外键约束兜底拒绝，不会留下孤儿配置。
func (a *AIReply) SetAgentConfig(ctx context.Context, userID int64, cookieID string, config AgentAccountConfig) (bool, error) {
	// owned、err 是归属校验结果与错误。
	owned, err := a.agentConfigAccountOwned(ctx, userID, cookieID)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}
	// enabledValue 是开关列写入值；nil 落库为 NULL 表示继承租户默认。
	var enabledValue any
	if config.Enabled != nil {
		enabledValue = boolToInt(*config.Enabled)
	}
	// presetValue 是档位列写入值；nil 落库为 NULL 表示继承租户默认。
	var presetValue any
	if config.Preset != nil {
		presetValue = *config.Preset
	}
	// err 是账号级配置 upsert 错误。
	_, err = a.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings
		(cookie_id, agent_enabled, agent_preset, updated_at)
		VALUES (?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(a.Dialect, []string{"cookie_id"}, map[string]string{
		"agent_enabled": "EXCLUDED.agent_enabled",
		"agent_preset":  "EXCLUDED.agent_preset",
		"updated_at":    "CURRENT_TIMESTAMP",
	}), cookieID, enabledValue, presetValue)
	return true, err
}

// agentConfigAccountOwned 校验账号存在且属于该租户。
func (a *AIReply) agentConfigAccountOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	// matched 是匹配到的账号数量；0 表示账号不存在或跨租户。
	var matched int
	// err 是归属查询错误。
	err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cookies WHERE id=? AND user_id=?`, cookieID, userID).Scan(&matched)
	if err != nil {
		return false, err
	}
	return matched > 0, nil
}
