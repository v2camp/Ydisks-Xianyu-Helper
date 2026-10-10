// tools_account.go 定义账号域的工具视图 DTO 与映射辅助：账号非敏感摘要、
// 暂停/长登录状态、设置确认、任务配置与运行记录。
//
// 工具注册装配见 tools_account_register.go；本文件全部输出使用包内 DTO，
// 明确排除 Cookie、密码、令牌与加密 metadata 字段。

package mcp

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"

	"xianyu-go/internal/capability"
)

// accountDTO 是账号的非敏感详情视图；不包含 Cookie、密码、令牌与任何凭证字段。
type accountDTO struct {
	// ID 是账号稳定标识。
	ID string `json:"account_id"`
	// OwnerUserID 是账号归属的本地用户标识。
	OwnerUserID int64 `json:"owner_user_id"`
	// Nickname 是平台昵称缓存。
	Nickname string `json:"nickname"`
	// AvatarURL 是平台头像缓存地址。
	AvatarURL string `json:"avatar_url"`
	// Remark 是本地备注。
	Remark string `json:"remark"`
	// AutoConfirm 是自动确认收货开关。
	AutoConfirm bool `json:"auto_confirm"`
	// AutoConsign 是自动转已发货开关。
	AutoConsign bool `json:"auto_consign"`
	// AutoBargain 是砍价自动免拼开关。
	AutoBargain bool `json:"auto_bargain"`
	// BargainSootheTemplate 是免拼前安抚模板，空串表示不发送。
	BargainSootheTemplate string `json:"bargain_soothe_template"`
	// PauseDuration 是暂停时长（分钟）。
	PauseDuration int `json:"pause_duration"`
	// PausedUntil 是暂停截止 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// LastRefreshAt 是最近资料刷新 Unix 秒。
	LastRefreshAt int64 `json:"last_refresh_at"`
	// LoginMethod 是最近登录方式标签。
	LoginMethod string `json:"login_method"`
	// LastLoginAt 是最近登录 Unix 秒。
	LastLoginAt int64 `json:"last_login_at"`
}

// adminAccountDTO 是管理员全局账号列表项。
type adminAccountDTO struct {
	// ID 是账号稳定标识。
	ID string `json:"account_id"`
	// OwnerUserID 是归属本地用户标识。
	OwnerUserID int64 `json:"owner_user_id"`
	// Owner 是归属用户展示名。
	Owner string `json:"owner"`
	// Remark 是本地备注。
	Remark string `json:"remark"`
	// Enabled 是账号启停状态。
	Enabled bool `json:"enabled"`
	// CreatedAt 是记录创建时间文本。
	CreatedAt string `json:"created_at"`
}

// accountListResult 是账号列表返回 DTO。
type accountListResult struct {
	// Total 是本次返回条数。
	Total int `json:"total"`
	// Accounts 是账号视图列表。
	Accounts []accountDTO `json:"accounts"`
}

// adminAccountListResult 是管理员全局账号列表返回 DTO。
type adminAccountListResult struct {
	// Total 是本次返回条数。
	Total int `json:"total"`
	// Accounts 是全局账号视图列表。
	Accounts []adminAccountDTO `json:"accounts"`
}

// pauseDTO 是暂停状态视图。
type pauseDTO struct {
	// Duration 是暂停时长（分钟）。
	Duration int `json:"pause_duration"`
	// PausedUntil 是暂停截止 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 表示当前是否仍处于暂停窗口。
	Paused bool `json:"paused"`
}

// longLoginDTO 是长登录状态视图。
type longLoginDTO struct {
	// CanOpen 表示平台是否允许该账号使用长登录。
	CanOpen bool `json:"can_open"`
	// Enabled 表示当前是否已开启。
	Enabled bool `json:"enabled"`
}

// settingsAckDTO 是账号设置写入后的确认视图；运行时诊断为空表示完全成功。
type settingsAckDTO struct {
	// AccountID 是被更新的账号标识。
	AccountID string `json:"account_id"`
	// PausedUntil 是更新后的暂停截止 Unix 秒。
	PausedUntil int64 `json:"paused_until,omitempty"`
	// RuntimeWarning 是非阻断运行时诊断；为空表示无警告。
	RuntimeWarning string `json:"runtime_warning,omitempty"`
}

// accountTaskSettingsDTO 是自动评价/擦亮/每日下架配置视图。
type accountTaskSettingsDTO struct {
	// AccountID 是账号标识。
	AccountID string `json:"account_id"`
	// AutoRateEnabled 是自动评价开关。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是评价内容。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 是自动擦亮开关。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是每日擦亮时间 HH:mm。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是最近评价扫描 Unix 秒。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是最近擦亮日期 YYYY-MM-DD。
	LastPolishDate string `json:"last_polish_date"`
	// AutoDelistEnabled 是每日定时下架开关。
	AutoDelistEnabled bool `json:"auto_delist_enabled"`
	// DelistTime 是每日下架时间 HH:mm。
	DelistTime string `json:"delist_time"`
	// DelistItemIDs 是下架白名单商品标识。
	DelistItemIDs []string `json:"delist_item_ids"`
	// LastDelistDate 是最近下架日期 YYYY-MM-DD。
	LastDelistDate string `json:"last_delist_date"`
}

// taskRunDTO 是账号任务运行记录视图。
type taskRunDTO struct {
	// ID 是运行记录标识。
	ID int64 `json:"run_id"`
	// TaskType 是任务类型（rate/polish）。
	TaskType string `json:"task_type"`
	// TargetID 是本次运行对应的平台对象标识。
	TargetID string `json:"target_id"`
	// RunDate 是任务业务日期。
	RunDate string `json:"run_date"`
	// Status 是运行状态。
	Status string `json:"status"`
	// SuccessCount 是成功动作数。
	SuccessCount int `json:"success_count"`
	// FailureCount 是失败动作数。
	FailureCount int `json:"failure_count"`
	// ErrorMessage 是非敏感失败说明。
	ErrorMessage string `json:"error_message,omitempty"`
	// StartedAt 是开始 Unix 秒。
	StartedAt int64 `json:"started_at"`
	// FinishedAt 是结束 Unix 秒。
	FinishedAt int64 `json:"finished_at"`
}

// profileDTO 是账号资料刷新结果视图。
type profileDTO struct {
	// AccountID 是完成刷新的账号标识。
	AccountID string `json:"account_id"`
	// Nickname 是刷新后的平台昵称。
	Nickname string `json:"nickname"`
	// AvatarURL 是刷新后的头像地址。
	AvatarURL string `json:"avatar_url"`
	// Warning 是平台返回的非阻断提示；为空表示完全成功。
	Warning string `json:"warning,omitempty"`
}

// taskSummaryDTO 是立即执行任务的统计结果视图。
type taskSummaryDTO struct {
	// TaskType 是任务类型。
	TaskType string `json:"task_type"`
	// Found 是发现对象数。
	Found int `json:"found"`
	// Success 是成功动作数。
	Success int `json:"success"`
	// Failed 是失败动作数。
	Failed int `json:"failed"`
	// Skipped 是跳过数。
	Skipped int `json:"skipped"`
	// Message 是非敏感执行说明。
	Message string `json:"message,omitempty"`
}

// accountBoolSettingTool 构造一个账号布尔开关写工具，减少三个同类开关的重复代码。
func accountBoolSettingTool(name, description string, p capability.AccountPorts,
	apply func(ctx context.Context, p capability.AccountPorts, userID int64, id string, enabled bool) (accountapp.SettingsResult, error)) ToolDef {
	return ToolDef{
		Name: name, Description: description,
		Args: []ArgSpec{
			{Name: "account_id", Type: ArgString, Required: true, Description: "目标闲鱼账号标识。"},
			{Name: "enabled", Type: ArgBoolean, Required: true, Description: "开关目标状态。"},
		},
		Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
			// id 是目标账号标识。
			id, err := args.String("account_id")
			if err != nil {
				return nil, err
			}
			// enabled、err 是开关目标状态及其校验错误。
			enabled, err := args.Bool("enabled")
			if err != nil {
				return nil, err
			}
			// result、applyErr 是开关写入结果。
			result, applyErr := apply(ctx, p, identity.UserID, id, enabled)
			if applyErr != nil {
				return nil, applyErr
			}
			return settingsAckDTO{AccountID: id, RuntimeWarning: warningFromError(result.RuntimeError)}, nil
		},
	}
}

// applyBoolPointer 在入参提供布尔键时把指针写入目标字段。
func applyBoolPointer(args Arguments, key string, target **bool) {
	// value、ok 是入参值及其存在性。
	value, ok := args[key]
	if !ok {
		return
	}
	// flag 是布尔值。
	flag, ok := value.(bool)
	if !ok {
		return
	}
	*target = &flag
}

// applyNonPointerBool 在入参提供布尔键时更新非指针布尔字段。
func applyNonPointerBool(args Arguments, key string, target *bool) {
	// value、ok 是入参值及其存在性。
	value, ok := args[key]
	if !ok {
		return
	}
	// flag 是布尔值。
	if flag, ok := value.(bool); ok {
		*target = flag
	}
}

// warningFromError 把非阻断运行时错误转为可展示中文警告；nil 时返回空串。
func warningFromError(err error) string {
	if err == nil {
		return ""
	}
	return "持久化已保存，但运行时同步出现问题：" + err.Error()
}

// accountFromSummary 把应用层账号摘要映射为 MCP 非敏感 DTO，明确丢弃凭证相关字段。
func accountFromSummary(row accountapp.AccountSummary) accountDTO {
	return accountDTO{
		ID: row.ID, OwnerUserID: row.UserID, Nickname: row.Nickname, AvatarURL: row.AvatarURL,
		Remark: row.Remark, AutoConfirm: row.AutoConfirm, AutoConsign: row.AutoConsign,
		AutoBargain: row.AutoBargain, BargainSootheTemplate: row.BargainSootheTemplate,
		PauseDuration: row.PauseDuration, PausedUntil: row.PausedUntil,
		LastRefreshAt: row.LastRefreshAt, LoginMethod: row.LoginMethod, LastLoginAt: row.LastLoginAt,
	}
}

// runtimeStatusMap 把应用层运行时状态映射为以账号标识为键的 DTO 映射。
func runtimeStatusMap(statuses map[string]accountapp.RuntimeStatus) map[string]any {
	// items 是 MCP 输出映射，时间字段转 Unix 秒避免直接序列化应用模型。
	items := make(map[string]any, len(statuses))
	// id、status 是当前账号标识与运行时状态。
	for id, status := range statuses {
		items[id] = map[string]any{
			"state":                   status.State,
			"message":                 status.Message,
			"connected":               status.Connected,
			"failures":                status.Failures,
			"updated_at":              status.UpdatedAt.Unix(),
			"token_remaining_seconds": status.TokenRemainingSeconds,
			"token_refresh_status":    status.TokenRefreshStatus,
		}
	}
	return map[string]any{"total": len(items), "accounts": items}
}

// taskSettingsDTO 构造任务配置 DTO。
func taskSettingsDTO(id string, s automationapp.AccountTaskSettings) accountTaskSettingsDTO {
	return accountTaskSettingsDTO{
		AccountID:         s.CookieID,
		AutoRateEnabled:   s.AutoRateEnabled,
		RateContent:       s.RateContent,
		AutoPolishEnabled: s.AutoPolishEnabled,
		PolishTime:        s.PolishTime,
		LastRateScanAt:    s.LastRateScanAt,
		LastPolishDate:    s.LastPolishDate,
		AutoDelistEnabled: s.AutoDelistEnabled,
		DelistTime:        s.DelistTime,
		DelistItemIDs:     s.DelistItemIDs,
		LastDelistDate:    s.LastDelistDate,
	}
}

// taskRunFromApp 把应用层任务运行记录映射为 MCP DTO。
func taskRunFromApp(run automationapp.AccountTaskRun) taskRunDTO {
	return taskRunDTO{
		ID: run.ID, TaskType: run.TaskType, TargetID: run.TargetID, RunDate: run.RunDate,
		Status: run.Status, SuccessCount: run.SuccessCount, FailureCount: run.FailedCount,
		ErrorMessage: run.ErrorMessage, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}
