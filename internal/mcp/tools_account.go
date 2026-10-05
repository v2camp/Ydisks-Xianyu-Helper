// tools_account.go 注册账号域工具：账号巡检、本地开关配置、长登录、运行时、
// 自动评价/擦亮任务与重启/刷新/删除等平台触达动作。
//
// 全部输出使用包内 DTO，明确排除 Cookie、密码、令牌与加密 metadata 字段。

package mcp

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
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

// accountTaskSettingsDTO 是自动评价/擦亮配置视图。
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

// RegisterAccountTools 注册账号域全部工具。
func (e *Endpoint) RegisterAccountTools(p AccountPorts) {
	if p == nil {
		return
	}
	// accountID 是所有账号级工具共用的必填账号参数定义。
	accountID := ArgSpec{Name: "account_id", Type: ArgString, Required: true,
		Description: "目标闲鱼账号标识（cookie_id）。"}
	e.RegisterTools(
		ToolDef{
			Name: "account_list_all",
			Description: "列出系统中全部账号的非敏感摘要（管理员全局视图，含归属用户与启停状态）。" +
				"只读，不返回 Cookie、令牌或密码字段。",
			Handler: func(ctx context.Context, _ *CallIdentity, _ Arguments) (any, error) {
				// rows 是全部账号摘要。
				rows, err := p.ListAdminSummaries(ctx)
				if err != nil {
					return nil, err
				}
				// items 是 MCP 视图切片。
				items := make([]adminAccountDTO, 0, len(rows))
				// row 是当前全局账号应用层摘要。
				for _, row := range rows {
					items = append(items, adminAccountDTO{
						ID: row.ID, OwnerUserID: row.UserID, Owner: row.Owner,
						Remark: row.Remark, Enabled: row.Enabled, CreatedAt: row.CreatedAt,
					})
				}
				return adminAccountListResult{Total: len(items), Accounts: items}, nil
			},
		},
		ToolDef{
			Name: "account_list",
			Description: "列出当前管理员归属账号的非敏感详情列表（含开关、暂停、昵称、最近登录信息）。" +
				"只读，不返回任何凭证明文。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows 是归属账号摘要列表。
				rows, err := p.ListSummaries(ctx, identity.UserID)
				if err != nil {
					return nil, err
				}
				// items 是 MCP 视图切片。
				items := make([]accountDTO, 0, len(rows))
				// row 是当前归属账号应用层摘要。
				for _, row := range rows {
					items = append(items, accountFromSummary(row))
				}
				return accountListResult{Total: len(items), Accounts: items}, nil
			},
		},
		ToolDef{
			Name: "account_get",
			Description: "读取单个归属账号的非敏感详情；目标账号不存在或不属于调用者时返回错误。只读。",
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// row、getErr 是账号摘要及其错误。
				row, getErr := p.GetOwnedSummary(ctx, identity.UserID, id)
				if getErr != nil {
					return nil, getErr
				}
				return accountFromSummary(row), nil
			},
		},
		ToolDef{
			Name: "account_set_remark",
			Description: "更新账号备注（仅本地配置，不触达平台）。",
			Args: []ArgSpec{
				accountID,
				{Name: "remark", Type: ArgString, Required: true, Description: "新的账号备注内容。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id、remark 是目标账号与新备注。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// remark、err 是新备注文本及其校验错误。
				remark, err := args.String("remark")
				if err != nil {
					return nil, err
				}
				// result、updateErr 是备注写入结果。
				result, updateErr := p.UpdateAccountSettings(ctx, accountapp.SettingsUpdateInput{
					UserID: identity.UserID, AccountID: id, Remark: &remark,
				})
				if updateErr != nil {
					return nil, updateErr
				}
				return settingsAckDTO{AccountID: id, RuntimeWarning: warningFromError(result.RuntimeError)}, nil
			},
		},
		ToolDef{
			Name: "account_set_status",
			Description: "启用或暂停账号的本地运行开关（停止/恢复该账号的自动化与长连接，不直接联系买家）。",
			Args: []ArgSpec{
				accountID,
				{Name: "enabled", Type: ArgBoolean, Required: true, Description: "true 启用账号，false 停用。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// enabled、err 是目标启停状态及其校验错误。
				enabled, err := args.Bool("enabled")
				if err != nil {
					return nil, err
				}
				// result、setErr 是启停写入结果。
				result, setErr := p.SetAccountStatus(ctx, identity.UserID, id, enabled)
				if setErr != nil {
					return nil, setErr
				}
				return settingsAckDTO{AccountID: id, RuntimeWarning: warningFromError(result.RuntimeError)}, nil
			},
		},
		ToolDef{
			Name: "account_set_pause",
			Description: "设置账号暂停时长（分钟）；传 0 立即恢复。暂停期间该账号不执行自动化动作。",
			Args: []ArgSpec{
				accountID,
				{Name: "pause_minutes", Type: ArgInteger, Required: true, Description: "暂停分钟数；0 表示立即恢复。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// minutes、err 是暂停分钟数及其校验错误。
				minutes, err := args.Int("pause_minutes")
				if err != nil {
					return nil, err
				}
				if minutes < 0 {
					return nil, InvalidArgument("pause_minutes 不能为负数")
				}
				// result、setErr 是暂停写入结果。
				result, setErr := p.SetAccountPause(ctx, identity.UserID, id, minutes)
				if setErr != nil {
					return nil, setErr
				}
				// state、getErr 是写入后的完整暂停状态。
				state, getErr := p.GetAccountPause(ctx, identity.UserID, id)
				if getErr != nil {
					return settingsAckDTO{AccountID: id, PausedUntil: result.PausedUntil}, nil
				}
				return pauseDTO{Duration: state.Duration, PausedUntil: state.PausedUntil, Paused: state.Paused}, nil
			},
		},
		ToolDef{
			Name: "account_get_pause",
			Description: "读取账号当前暂停时长与截止时间。只读。",
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// state、getErr 是暂停状态。
				state, getErr := p.GetAccountPause(ctx, identity.UserID, id)
				if getErr != nil {
					return nil, getErr
				}
				return pauseDTO{Duration: state.Duration, PausedUntil: state.PausedUntil, Paused: state.Paused}, nil
			},
		},
		accountBoolSettingTool("account_set_auto_confirm",
			"更新账号自动确认收货开关（付款自动化链路总开关之一，仅本地配置）。", p,
			func(ctx context.Context, p AccountPorts, userID int64, id string, enabled bool) (accountapp.SettingsResult, error) {
				return p.SetAutoConfirm(ctx, userID, id, enabled)
			}),
		accountBoolSettingTool("account_set_auto_consign",
			"更新账号发货后自动转已发货开关（仅本地配置，不改变砍价阶段免拼顺序）。", p,
			func(ctx context.Context, p AccountPorts, userID int64, id string, enabled bool) (accountapp.SettingsResult, error) {
				return p.SetAutoConsign(ctx, userID, id, enabled)
			}),
		accountBoolSettingTool("account_set_auto_bargain",
			"更新账号砍价“待刀成”阶段自动免拼开关（仅本地配置；实际免拼仍严格按 WS 阶段触发）。", p,
			func(ctx context.Context, p AccountPorts, userID int64, id string, enabled bool) (accountapp.SettingsResult, error) {
				return p.SetAutoBargain(ctx, userID, id, enabled)
			}),
		ToolDef{
			Name: "account_update_settings",
			Description: "一次性更新账号本地综合设置：备注、自动确认/寄售/免拼开关、暂停分钟数、免拼安抚模板。" +
				"只传需要修改的字段；不提供任何凭证字段。",
			Args: []ArgSpec{
				accountID,
				{Name: "remark", Type: ArgString, Description: "新备注；不传表示不修改。"},
				{Name: "auto_confirm", Type: ArgBoolean, Description: "自动确认收货开关。"},
				{Name: "auto_consign", Type: ArgBoolean, Description: "自动转已发货开关。"},
				{Name: "auto_bargain", Type: ArgBoolean, Description: "砍价自动免拼开关。"},
				{Name: "pause_minutes", Type: ArgInteger, Description: "暂停分钟数；0 恢复。"},
				{Name: "bargain_soothe_template", Type: ArgString, Description: "免拼前安抚模板内容，空串表示不发送。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// input 是逐步填充的综合设置输入。
				input := accountapp.SettingsUpdateInput{AccountID: id}
				// value、ok 是备注入参及其存在性。
				if value, ok := args["remark"]; ok {
					// remark 是备注字符串值。
					remark, _ := value.(string)
					input.Remark = &remark
				}
				applyBoolPointer(args, "auto_confirm", &input.AutoConfirm)
				applyBoolPointer(args, "auto_consign", &input.AutoConsign)
				applyBoolPointer(args, "auto_bargain", &input.AutoBargain)
				// value、ok 是暂停分钟入参及其存在性。
				if value, ok := args["pause_minutes"]; ok {
					// number 是 JSON 数字形态；类型非法时返回中文参数错误。
					number, numberOK := value.(float64)
					if !numberOK {
						return nil, InvalidArgument("pause_minutes 必须是整数")
					}
					// minutes 是暂停分钟数。
					minutes := int(number)
					input.PauseDuration = &minutes
				}
				// value、ok 是安抚模板入参及其存在性。
				if value, ok := args["bargain_soothe_template"]; ok {
					// template 是安抚模板内容。
					template, _ := value.(string)
					input.BargainSootheTemplate = &template
				}
				// 补归属用户后一次性原子写入全部选定字段。
				input.UserID = identity.UserID
				// applied、applyErr 是综合设置写入结果。
				applied, applyErr := p.UpdateAccountSettings(ctx, input)
				if applyErr != nil {
					return nil, applyErr
				}
				return settingsAckDTO{
					AccountID:      id,
					PausedUntil:    applied.PausedUntil,
					RuntimeWarning: warningFromError(applied.RuntimeError),
				}, nil
			},
		},
		ToolDef{
			Name: "account_get_long_login",
			Description: "查询账号平台长登录可用与开启状态（平台只读查询，会更新本地会话但不修改业务数据）。",
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// result、queryErr 是长登录状态。
				result, queryErr := p.QueryLongLogin(ctx, identity.UserID, id)
				if queryErr != nil {
					return nil, queryErr
				}
				return longLoginDTO{CanOpen: result.CanOpenLongLogin, Enabled: result.Enabled}, nil
			},
		},
		ToolDef{
			Name: "account_set_long_login",
			Description: "向平台开启或关闭账号长登录（真实平台触达，会刷新会话）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountID,
				{Name: "enabled", Type: ArgBoolean, Required: true, Description: "true 开启长登录，false 关闭。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// enabled、err 是长登录目标状态及其校验错误。
				enabled, err := args.Bool("enabled")
				if err != nil {
					return nil, err
				}
				// result、setErr 是平台返回的长登录状态。
				result, setErr := p.SetLongLogin(ctx, identity.UserID, id, enabled)
				if setErr != nil {
					return nil, setErr
				}
				return longLoginDTO{CanOpen: result.CanOpenLongLogin, Enabled: result.Enabled}, nil
			},
		},
		ToolDef{
			Name: "account_runtime_status",
			Description: "返回全部账号运行时状态总览（连接状态、失败计数、令牌阶段等非敏感诊断）。只读。",
			Handler: func(ctx context.Context, _ *CallIdentity, _ Arguments) (any, error) {
				// statuses、err 是按账号标识索引的运行时状态。
				statuses, err := p.RuntimeStatuses(ctx)
				if err != nil {
					return nil, err
				}
				return runtimeStatusMap(statuses), nil
			},
		},
		ToolDef{
			Name: "account_restart",
			Description: "重启指定账号的运行实例（断开并重新建立长连接，真实平台触达）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				if // restartErr 是账号重启指令的下发错误。
				restartErr := p.RestartAccount(ctx, id); restartErr != nil {
					return nil, restartErr
				}
				return settingsAckDTO{AccountID: id, RuntimeWarning: "账号重启指令已下发"}, nil
			},
		},
		ToolDef{
			Name: "account_refresh_profile",
			Description: "向平台刷新账号昵称与头像资料（真实平台只读触达）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// result、refreshErr 是资料刷新结果。
				result, refreshErr := p.RefreshProfile(ctx, identity.UserID, id)
				if refreshErr != nil {
					return nil, refreshErr
				}
				return profileDTO{
					AccountID: result.AccountID, Nickname: result.Nickname,
					AvatarURL: result.AvatarURL, Warning: result.ErrorMessage,
				}, nil
			},
		},
		ToolDef{
			Name: "account_delete",
			Description: "删除指定账号的本地记录（不可逆；不删除平台侧账号，会先停止该账号运行实例）。" +
				"必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				if // deleteErr 是账号删除用例返回的错误。
				deleteErr := p.DeleteAccount(ctx, identity.UserID, id); deleteErr != nil {
					return nil, deleteErr
				}
				return map[string]any{"deleted": true, "account_id": id}, nil
			},
		},
		ToolDef{
			Name: "account_task_get_settings",
			Description: "读取账号自动评价与每日擦亮配置。只读。",
			Args:        []ArgSpec{accountID},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// settings、getErr 是任务配置。
				settings, getErr := p.GetAccountTaskSettings(ctx, id)
				if getErr != nil {
					return nil, getErr
				}
				return taskSettingsDTO(id, settings), nil
			},
		},
		ToolDef{
			Name: "account_task_update_settings",
			Description: "更新账号自动评价开关/评价内容与每日擦亮开关/执行时间（仅本地配置）。",
			Args: []ArgSpec{
				accountID,
				{Name: "auto_rate_enabled", Type: ArgBoolean, Description: "是否启用自动评价。"},
				{Name: "rate_content", Type: ArgString, Description: "自动评价内容。"},
				{Name: "auto_polish_enabled", Type: ArgBoolean, Description: "是否启用每日擦亮。"},
				{Name: "polish_time", Type: ArgString, Description: "每日擦亮时间，格式 HH:mm。"},
			},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// current、getErr 是现有配置，用于局部更新。
				current, getErr := p.GetAccountTaskSettings(ctx, id)
				if getErr != nil {
					return nil, getErr
				}
				current.CookieID = id
				applyNonPointerBool(args, "auto_rate_enabled", &current.AutoRateEnabled)
				applyNonPointerBool(args, "auto_polish_enabled", &current.AutoPolishEnabled)
				// value、ok 是评价内容入参及其存在性。
				if value, ok := args["rate_content"]; ok {
					// rateContent 是评价内容字符串。
					current.RateContent, _ = value.(string)
				}
				// value、ok 是擦亮时间入参及其存在性。
				if value, ok := args["polish_time"]; ok {
					// polishTime 是擦亮时间字符串。
					current.PolishTime, _ = value.(string)
				}
				// saved、updateErr 是更新后的配置。
				saved, updateErr := p.UpdateAccountTaskSettings(ctx, current)
				if updateErr != nil {
					return nil, updateErr
				}
				return taskSettingsDTO(id, saved), nil
			},
		},
		ToolDef{
			Name: "account_task_list_runs",
			Description: "查询账号自动评价/擦亮的最近运行记录。只读。",
			Args: []ArgSpec{
				accountID,
				{Name: "limit", Type: ArgInteger, Description: "返回条数上限，默认 20，最大 200。"},
			},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// limit 是收敛后的条数上限。
				limit := args.OptionalInt("limit", defaultPageSize)
				if limit <= 0 || limit > maxPageSize {
					limit = maxPageSize
				}
				// runs、listErr 是运行记录列表。
				runs, listErr := p.ListAccountTaskRuns(ctx, id, limit)
				if listErr != nil {
					return nil, listErr
				}
				// items 是 MCP 视图列表。
				items := make([]taskRunDTO, 0, len(runs))
				// run 是当前任务运行记录。
				for _, run := range runs {
					items = append(items, taskRunFromApp(run))
				}
				return map[string]any{"total": len(items), "runs": items}, nil
			},
		},
		ToolDef{
			Name: "account_task_run",
			Description: "立即执行一次账号自动评价或擦亮任务（真实平台触达，受账号开关与平台风控约束）。" +
				"task_type 取 rate（评价）或 polish（擦亮）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountID,
				{Name: "task_type", Type: ArgString, Required: true, Enum: []string{"rate", "polish"},
					Description: "要立即执行的任务类型：rate=自动评价，polish=商品擦亮。"},
			},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// id 是目标账号标识。
				id, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// taskType、err 是任务类型及其校验错误。
				taskType, err := args.String("task_type")
				if err != nil {
					return nil, err
				}
				// summary、runErr 是任务执行汇总。
				summary, runErr := p.RunAccountTask(ctx, id, taskType)
				if runErr != nil {
					return nil, runErr
				}
				return taskSummaryDTO{
					TaskType: summary.TaskType, Found: summary.Found, Success: summary.Success,
					Failed: summary.Failed, Skipped: summary.Skipped, Message: summary.Message,
				}, nil
			},
		},
	)
}

// accountBoolSettingTool 构造一个账号布尔开关写工具，减少三个同类开关的重复代码。
func accountBoolSettingTool(name, description string, p AccountPorts,
	apply func(ctx context.Context, p AccountPorts, userID int64, id string, enabled bool) (accountapp.SettingsResult, error)) ToolDef {
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
