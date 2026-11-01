// agent_config.go 客服 Agent 账号级档位配置的持久化读写。
//
// 配置复用 ai_reply_settings 两列而不是新建表：该表已承载同一账号的模型凭证与业务
// 护栏，归属与敏感度一致，符合 AGENTS.md §1.5 对「敏感度或归属不同的类型禁止合并」
// 的反向判定。本文件只读写 agent_preset，不读取 api_key，也不读写 AI 开关键。
//
// 与 AI 开关的正交性：写入本文件的配置行时把 ai_enabled 写成 1（= 跟随总闸在总闸开启下
// 的等价显式值），且此后不再改动该列。回复链路的口径是 COALESCE(ai_enabled, 1)，
// 因此「配一次 Agent 档位」既不会把默认开启的账号静默关闭，也不会覆盖用户已有的显式关闭
// （E1 语义反转后，落到列默认值 0 就等于静默关闭）。
// 历史遗留：列 agent_enabled 由迁移 00059 建立，本文件已停止读写，待后续迁移清理。

package db

import (
	"context"
	"database/sql"
	"errors"
)

// AgentAccountConfig 是账号级客服 Agent 档位配置的持久化视图，不含任何模型凭证与开关。
//
// 档位为 nil 表示该账号完全继承租户默认档位。是否运行客服 Agent 不再是本结构的维度：
// 它由统一 AI 开关决定，与档位配置互不影响。
type AgentAccountConfig struct {
	// CookieID 是配置归属账号。
	CookieID string
	// Preset 为 nil 表示账号未覆盖档位，完全继承租户默认。
	Preset *string
}

// GetAgentConfig 读取账号级客服 Agent 档位配置。
//
// 以 cookies 为主表左连接配置表，因此两类情形被区分开：账号存在但从未写过配置时返回
// 全继承的空配置；账号不存在或不属于该租户时返回 found=false。调用方对 found=false
// 必须按未授权处理，禁止降级为继承值继续执行。
func (a *AIReply) GetAgentConfig(ctx context.Context, userID int64, cookieID string) (*AgentAccountConfig, bool, error) {
	// preset 是档位列；NULL 表示该账号未覆盖档位。
	var preset sql.NullString
	// err 是账号配置查询错误；ErrNoRows 表示账号不存在或跨租户。
	err := a.DB.QueryRowContext(ctx, `
		SELECT r.agent_preset
		  FROM cookies c LEFT JOIN ai_reply_settings r ON r.cookie_id = c.id
		 WHERE c.id = ? AND c.user_id = ?`, cookieID, userID).Scan(&preset)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// config 是读取结果，默认表示继承租户默认档位。
	config := &AgentAccountConfig{CookieID: cookieID}
	if preset.Valid {
		// presetValue 是档位列的有效原文，合法性由应用层收敛。
		presetValue := preset.String
		config.Preset = &presetValue
	}
	return config, true, nil
}

// SetAgentConfig 保存账号级客服 Agent 档位配置；Preset 为 nil 表示恢复继承租户默认。
//
// 与 AI 开关正交是本方法的核心约束：ai_enabled 在首次插入时写入 1（「跟随总闸」在总闸
// 开启下的等价显式值），冲突时保持数据库原值不变。这样写 Agent 档位既不会把默认开启的
// 账号写成显式关闭，也不会把用户显式关闭的账号重新打开——开关的表态只属于 AI 设置通路。
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
	// presetValue 是档位列写入值；nil 落库为 NULL 表示继承租户默认。
	var presetValue any
	if config.Preset != nil {
		presetValue = *config.Preset
	}
	// err 是账号级配置 upsert 错误。
	//
	// SQL 形状的两条约束不可省，口径与 UpsertSettings 的三态约定保持一致：
	//  1. INSERT 列表里显式写 ai_enabled=1。省略该列会落到表定义的 DEFAULT 0
	//     （00001 建表 sqlite 为 FALSE、postgres 为 0），在「无行即跟随总闸」的语义下
	//     等于把默认开启的账号静默写成显式关闭。
	//  2. 冲突时只更新 agent_preset 与 updated_at，绝不更新 ai_enabled：0 或 1 都属于
	//     AI 设置通路的表态，本通路无权改写，否则用户「关掉 AI」的决定会随一次档位保存失效。
	_, err = a.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings
		(cookie_id, ai_enabled, agent_preset, updated_at)
		VALUES (?,1,?,CURRENT_TIMESTAMP)`+dialectUpsert(a.Dialect, []string{"cookie_id"}, map[string]string{
		"agent_preset": "EXCLUDED.agent_preset",
		"updated_at":   "CURRENT_TIMESTAMP",
	}), cookieID, presetValue)
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
