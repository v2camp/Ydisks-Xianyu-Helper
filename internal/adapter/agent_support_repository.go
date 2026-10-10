// agent_support_repository.go 把用户设置与账号 AI 设置仓储投影为客服 Agent 配置端口。
//
// 投影只做类型转换，不做策略判断：档位合法性、归属校验与读写失败策略都由
// internal/application/agentadmin 决定。账号级配置与模型凭证同存一张表，本通路只取
// agent_enabled 与 agent_preset 两列，api_key 不进入这条路径（AGENTS.md §1.7）。

package adapter

import (
	"context"

	agentadminapp "xianyu-go/internal/application/agentadmin"
	"xianyu-go/internal/db"
)

// agentSupportRepository 把租户级与账号级配置仓储投影为客服 Agent 配置端口。
type agentSupportRepository struct {
	// users 提供租户级 user_settings 的读写。
	users *db.UserSettings
	// replies 提供账号级 Agent 配置的读写。
	replies *db.AIReply
}

// 编译期断言投影结果满足客服 Agent 配置端口。
var _ agentadminapp.Repository = (*agentSupportRepository)(nil)

// TenantValues 返回租户全部设置原文；缺失的键不出现在结果中。
func (r *agentSupportRepository) TenantValues(ctx context.Context, userID int64) (map[string]string, error) {
	return r.users.AllForUser(ctx, userID)
}

// SetTenantValues 在单个事务内保存租户级配置项。
func (r *agentSupportRepository) SetTenantValues(ctx context.Context, userID int64, values map[string]string) error {
	return r.users.SetManyForUser(ctx, userID, values)
}

// AccountOverride 读取账号级覆盖；账号不存在或不属于该租户时 found 为 false。
func (r *agentSupportRepository) AccountOverride(ctx context.Context, userID int64, cookieID string) (agentadminapp.AccountOverride, bool, error) {
	// config、found、err 是账号级配置、归属判定与读取错误。
	config, found, err := r.replies.GetAgentConfig(ctx, userID, cookieID)
	if err != nil || !found {
		return agentadminapp.AccountOverride{}, found, err
	}
	return agentadminapp.AccountOverride{Enabled: config.Enabled, Preset: config.Preset}, true, nil
}

// SetAccountOverride 写入账号级覆盖；账号不属于该租户时不落库并返回 found=false。
func (r *agentSupportRepository) SetAccountOverride(ctx context.Context, userID int64, cookieID string, override agentadminapp.AccountOverride) (bool, error) {
	return r.replies.SetAgentConfig(ctx, userID, cookieID, db.AgentAccountConfig{
		CookieID: cookieID, Enabled: override.Enabled, Preset: override.Preset,
	})
}

// NewAgentSupportRepository 从数据库 Store 构造客服 Agent 配置端口。
// Store 或其成员仓储缺失时返回 nil，由构造方按未装配处理。
func NewAgentSupportRepository(store *db.Store) agentadminapp.Repository {
	if store == nil || store.UserSettings == nil || store.AIReply == nil {
		return nil
	}
	return &agentSupportRepository{users: store.UserSettings, replies: store.AIReply}
}
