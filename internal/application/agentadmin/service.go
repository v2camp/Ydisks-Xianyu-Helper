// Package agentadmin 提供客服 Agent 的配置用例：租户默认与账号级覆盖的读取、
// 校验、合并与写入。
//
// 该配置只回答两个问题——「这个账号跑不跑客服 Agent」与「跑在哪一档」。平台级定义
// （有哪些能力、各属哪一档）不在这里，它是 internal/capability 的代码常量，用户不可
// 增删；本包只负责在租户默认与账号覆盖之间选出最终生效值。
//
// 账号级配置与模型凭证同存于 ai_reply_settings，但本包只投影 agent_enabled 与
// agent_preset 两列：含 api_key 的持久化模型禁止流向调用方（AGENTS.md §1.7）。
// 档位合法性由 capability.Preset 单点判定，本包不自行列举合法取值。
package agentadmin

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"xianyu-go/internal/capability"
)

// ErrNotConfigured 表示客服 Agent 配置仓储未装配，配置用例不可用。
var ErrNotConfigured = errors.New("客服 Agent 配置仓储未初始化")

// ErrInvalidPreset 表示待写入的档位不是平台定义的合法档位。
var ErrInvalidPreset = errors.New("客服 Agent 档位非法")

// ErrAccountNotOwned 表示目标账号不存在或不属于当前租户。
//
// 两种情形共用同一个错误：区分它们会让调用方接口变成账号枚举器。
var ErrAccountNotOwned = errors.New("账号不存在或不属于当前租户")

// 租户级客服 Agent 配置在 user_settings 中的键名。
const (
	// KeySupportEnabled 是租户级默认开关的键。
	KeySupportEnabled = "agent.support.enabled"
	// KeySupportPreset 是租户级默认档位的键。
	KeySupportPreset = "agent.support.preset"
)

// TenantConfig 是租户级客服 Agent 默认配置。
type TenantConfig struct {
	// Enabled 表示该租户的账号默认是否运行客服 Agent。
	Enabled bool
	// Preset 是账号未覆盖时使用的档位。
	Preset capability.Preset
}

// AccountOverride 是账号级覆盖。字段为 nil 表示该维度继承租户默认，
// 两个字段同时为 nil 表示这个账号完全跟随租户。
//
// Preset 声明为字符串而不是 capability.Preset，是为了区分「未覆盖」与「覆盖成空值」，
// 同时把「什么算合法档位」的把关收在写入路径上。
type AccountOverride struct {
	// Enabled 非 nil 时覆盖租户开关。
	Enabled *bool
	// Preset 非 nil 时覆盖租户档位。
	Preset *string
}

// EffectiveConfig 是某账号最终生效的客服 Agent 配置。
type EffectiveConfig struct {
	// CookieID 是配置归属的账号标识。
	CookieID string
	// Enabled 是该账号最终生效的开关。
	Enabled bool
	// Preset 是该账号最终生效的档位。
	Preset capability.Preset
	// AccountEnabled 非 nil 表示开关来自账号级覆盖，nil 表示继承租户默认。
	AccountEnabled *bool
	// AccountPreset 非 nil 表示档位来自账号级覆盖，nil 表示继承租户默认。
	AccountPreset *capability.Preset
}

// Repository 定义客服 Agent 配置用例所需的最小持久化能力。
//
// 实现负责归属校验，并且只读写配置本身：不解释档位语义，也不得把含模型凭证的
// 持久化模型交给调用方。
type Repository interface {
	// TenantValues 读取租户的全部 user_settings 原文；缺失的键不出现在结果中。
	TenantValues(ctx context.Context, userID int64) (map[string]string, error)
	// SetTenantValues 写入租户级配置项；任意一项失败时调用方按整体失败处理。
	SetTenantValues(ctx context.Context, userID int64, values map[string]string) error
	// AccountOverride 读取账号级覆盖；账号不存在或不属于该租户时 found 为 false。
	AccountOverride(ctx context.Context, userID int64, cookieID string) (override AccountOverride, found bool, err error)
	// SetAccountOverride 写入账号级覆盖；账号不属于该租户时不落库并返回 found=false。
	SetAccountOverride(ctx context.Context, userID int64, cookieID string, override AccountOverride) (found bool, err error)
}

// Service 编排客服 Agent 的配置用例。
type Service struct {
	// repository 是客服 Agent 配置持久化端口。
	repository Repository
}

// NewService 构造客服 Agent 配置应用服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// TenantConfig 返回租户级默认配置；未配置或取值非法时按平台默认收敛，不返回错误。
func (s *Service) TenantConfig(ctx context.Context, userID int64) (TenantConfig, error) {
	if s == nil || s.repository == nil {
		return TenantConfig{}, ErrNotConfigured
	}
	// values、err 是租户全部设置原文及读取错误。
	values, err := s.repository.TenantValues(ctx, userID)
	if err != nil {
		return TenantConfig{}, err
	}
	return TenantConfig{
		Enabled: parseBoolSetting(values[KeySupportEnabled]),
		Preset:  normalizePreset(values[KeySupportPreset]),
	}, nil
}

// UpdateTenantConfig 保存租户级默认配置。
//
// 档位非法时整体拒绝，避免把坏值留在库里再靠读取路径兜底：写入路径必须 fail-closed，
// 否则「配了什么」与「生效什么」会长期不一致。
func (s *Service) UpdateTenantConfig(ctx context.Context, userID int64, config TenantConfig) error {
	if s == nil || s.repository == nil {
		return ErrNotConfigured
	}
	if !config.Preset.Valid() {
		return ErrInvalidPreset
	}
	return s.repository.SetTenantValues(ctx, userID, map[string]string{
		KeySupportEnabled: strconv.FormatBool(config.Enabled),
		KeySupportPreset:  string(config.Preset),
	})
}

// AccountConfig 返回指定账号最终生效的配置，并标注每一项是账号级覆盖还是继承租户。
//
// userID 参与归属校验：不属于该租户的账号一律返回 ErrAccountNotOwned，不做任何降级。
func (s *Service) AccountConfig(ctx context.Context, userID int64, cookieID string) (EffectiveConfig, error) {
	if s == nil || s.repository == nil {
		return EffectiveConfig{}, ErrNotConfigured
	}
	// override、found、err 是账号级覆盖、归属判定与读取错误。
	override, found, err := s.repository.AccountOverride(ctx, userID, cookieID)
	if err != nil {
		return EffectiveConfig{}, err
	}
	if !found {
		return EffectiveConfig{}, ErrAccountNotOwned
	}
	// tenant、err 是账号所属租户的默认配置。
	tenant, err := s.TenantConfig(ctx, userID)
	if err != nil {
		return EffectiveConfig{}, err
	}
	return resolve(cookieID, tenant, override), nil
}

// UpdateAccountOverride 保存账号级覆盖；两个字段均为 nil 时该账号恢复完全继承租户默认。
func (s *Service) UpdateAccountOverride(ctx context.Context, userID int64, cookieID string, override AccountOverride) error {
	if s == nil || s.repository == nil {
		return ErrNotConfigured
	}
	if override.Preset != nil && !capability.Preset(*override.Preset).Valid() {
		return ErrInvalidPreset
	}
	// found、err 是归属判定与写入错误；不属于该租户的账号不得写入任何值。
	found, err := s.repository.SetAccountOverride(ctx, userID, cookieID, override)
	if err != nil {
		return err
	}
	if !found {
		return ErrAccountNotOwned
	}
	return nil
}

// resolve 以租户默认为底、账号覆盖为顶，合成最终生效配置并保留覆盖来源。
func resolve(cookieID string, tenant TenantConfig, override AccountOverride) EffectiveConfig {
	// out 先取租户默认，再按账号覆盖逐项替换。
	out := EffectiveConfig{CookieID: cookieID, Enabled: tenant.Enabled, Preset: tenant.Preset}
	if override.Enabled != nil {
		out.Enabled = *override.Enabled
		out.AccountEnabled = override.Enabled
	}
	if override.Preset != nil {
		// preset 是收敛后的档位；非法值一律回落只读档，账号不能借坏值绕过租户上限。
		preset := normalizePreset(*override.Preset)
		out.Preset = preset
		out.AccountPreset = &preset
	}
	return out
}

// normalizePreset 把原始档位文本收敛为合法档位；未知取值一律回落到只读档。
//
// 读取路径选择回落而非报错：非法值只可能来自人工改库或跨版本遗留，此时让 Agent 以
// 最保守档位继续服务，比让整条回复链路失败更符合 fail-closed 与可用性。
func normalizePreset(raw string) capability.Preset {
	// preset 是去空白后的候选档位。
	preset := capability.Preset(strings.TrimSpace(raw))
	if preset.Valid() {
		return preset
	}
	return capability.DefaultPreset
}

// parseBoolSetting 把 user_settings 原文解析为布尔值；空串与非法文本一律按 false。
func parseBoolSetting(raw string) bool {
	// parsed、err 是解析结果与错误；该键只可能由本包写入 strconv 认可的布尔文本。
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed
}
