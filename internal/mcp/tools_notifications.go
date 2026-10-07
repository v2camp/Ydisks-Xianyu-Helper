// tools_notifications.go 注册通知域工具：通知渠道 CRUD 与连通性测试、账号渠道绑定管理、
// 以及不确定通知的用户视图与管理员全局视图。
//
// 安全与语义红线：渠道配置 JSON（SMTP 密码、机器人 secret、验证码 secret）只经写入参数
// 进入应用层，MCP 输出、错误与 DTO 永不包含配置原文；编辑态只返回脱敏字段；
// 删除、清空绑定与测试发送会触达外部或不可逆，必须显式 confirm。

package mcp

import (
	"context"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// notificationChannelDTO 是通知渠道的非敏感摘要视图；不含配置 JSON。
type notificationChannelDTO struct {
	// ChannelID 是渠道标识。
	ChannelID int64 `json:"channel_id"`
	// Name 是渠道名称。
	Name string `json:"name"`
	// Type 是渠道协议类型，例如 email、qq、captcha。
	Type string `json:"type"`
	// EventTypes 是订阅的事件类型编码。
	EventTypes string `json:"event_types,omitempty"`
	// Enabled 表示渠道是否参与通知投递。
	Enabled bool `json:"enabled"`
}

// notificationChannelEditorDTO 是渠道编辑态视图：只含非敏感字段，密钥类字段永不回显。
type notificationChannelEditorDTO struct {
	// ChannelID 是渠道标识。
	ChannelID int64 `json:"channel_id"`
	// Name 是渠道名称。
	Name string `json:"name"`
	// Type 是渠道协议类型。
	Type string `json:"type"`
	// EventTypes 是订阅的事件类型编码。
	EventTypes string `json:"event_types,omitempty"`
	// Enabled 表示渠道是否启用。
	Enabled bool `json:"enabled"`
	// ToEmail 是邮件渠道的收件地址；其它渠道为空。
	ToEmail string `json:"to_email,omitempty"`
	// UseCustomSMTP 表示邮件渠道是否使用独立 SMTP（不返回任何 SMTP 凭据）。
	UseCustomSMTP bool `json:"use_custom_smtp"`
	// Note 提示敏感字段的写入语义。
	Note string `json:"note"`
}

// notificationChannelListResult 是渠道列表返回。
type notificationChannelListResult struct {
	// Total 是渠道数量。
	Total int `json:"total"`
	// Channels 是渠道摘要列表。
	Channels []notificationChannelDTO `json:"channels"`
}

// notificationChannelMutationResult 是渠道写入操作的确认返回。
type notificationChannelMutationResult struct {
	// ChannelID 是被写入的渠道标识。
	ChannelID int64 `json:"channel_id"`
}

// notificationBindingDTO 是账号与渠道绑定的非敏感视图。
type notificationBindingDTO struct {
	// BindingID 是绑定记录标识。
	BindingID int64 `json:"binding_id"`
	// AccountID 是绑定账号标识。
	AccountID string `json:"account_id"`
	// ChannelID 是绑定渠道标识。
	ChannelID int64 `json:"channel_id"`
	// ChannelName 是渠道名称。
	ChannelName string `json:"channel_name,omitempty"`
	// Enabled 表示绑定是否启用。
	Enabled bool `json:"enabled"`
}

// notificationBindingListResult 是绑定列表返回。
type notificationBindingListResult struct {
	// Total 是绑定数量。
	Total int `json:"total"`
	// Bindings 是绑定视图列表。
	Bindings []notificationBindingDTO `json:"bindings"`
}

// notificationBindingIDsResult 是账号当前启用渠道标识返回。
type notificationBindingIDsResult struct {
	// AccountID 是账号标识。
	AccountID string `json:"account_id"`
	// ChannelIDs 是该账号当前启用的渠道标识列表。
	ChannelIDs []int64 `json:"channel_ids"`
}

// notificationUncertainDTO 是不确定通知的非敏感摘要视图；不含通知正文与错误原文。
type notificationUncertainDTO struct {
	// NotificationID 是通知记录标识。
	NotificationID int64 `json:"notification_id"`
	// ChannelID 是关联渠道标识。
	ChannelID int64 `json:"channel_id"`
	// OwnerUserID 是渠道所属用户；仅在管理员全局视图返回。
	OwnerUserID int64 `json:"owner_user_id,omitempty"`
	// EventType 是通知事件分类。
	EventType string `json:"event_type,omitempty"`
	// AttemptCount 是进入不确定状态前的尝试次数。
	AttemptCount int `json:"attempt_count,omitempty"`
	// UncertainAt 是进入不确定状态的 Unix 秒时间戳。
	UncertainAt int64 `json:"uncertain_at,omitempty"`
	// HasError 表示是否记录过本地确认错误（不返回错误原文）。
	HasError bool `json:"has_error"`
}

// notificationUncertainListResult 是不确定通知列表返回。
type notificationUncertainListResult struct {
	// Total 是不确定通知总数。
	Total int `json:"total"`
	// Items 是当前页摘要列表。
	Items []notificationUncertainDTO `json:"items"`
}

// RegisterNotificationTools 注册通知渠道、绑定与不确定通知域全部工具。
func (e *Endpoint) RegisterNotificationTools(channels NotificationChannelPorts, uncertain UncertainNotificationPorts) {
	if channels != nil {
		e.registerNotificationChannelTools(channels)
		e.registerNotificationBindingTools(channels)
	}
	if uncertain != nil {
		e.registerNotificationUncertainTools(uncertain)
	}
}

// registerNotificationChannelTools 注册通知渠道 CRUD 与测试发送工具。
func (e *Endpoint) registerNotificationChannelTools(p NotificationChannelPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "notification_channel_list",
			Description: "列出全部通知渠道的非敏感摘要（名称、类型、订阅事件与开关）；不含任何密钥。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是渠道摘要列表。
				rows, listErr := p.ListChannels(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				// channels 是渠道视图列表。
				channels := make([]notificationChannelDTO, 0, len(rows))
				// row 是当前待映射的渠道摘要。
				for _, row := range rows {
					channels = append(channels, notificationChannelDTOFromApp(row))
				}
				return notificationChannelListResult{Total: len(channels), Channels: channels}, nil
			},
		},
		ToolDef{
			Name: "notification_channel_get",
			Description: "读取单个通知渠道的编辑态：名称、类型、订阅事件、开关与邮件收件地址。" +
				"SMTP 密码、机器人 secret 等敏感字段永不返回。只读。",
			Args: []ArgSpec{{Name: "channel_id", Type: ArgInteger, Required: true, Description: "渠道标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是渠道标识。
				id, err := args.Int("channel_id")
				if err != nil {
					return nil, err
				}
				// editor、getErr 是渠道编辑态。
				editor, getErr := p.GetChannelEditor(ctx, identity.UserID, int64(id))
				if getErr != nil {
					return nil, getErr
				}
				return notificationChannelEditorDTO{
					ChannelID: editor.ID, Name: editor.Name, Type: editor.Type,
					EventTypes: editor.EventTypes, Enabled: editor.Enabled,
					ToEmail: editor.ToEmail, UseCustomSMTP: editor.UseCustomSMTP,
					Note: "敏感字段（SMTP 密码、机器人 secret 等）只可写入、不可读取；更新时请通过 config_json 重新提交完整配置。",
				}, nil
			},
		},
		ToolDef{
			Name: "notification_channel_create",
			Description: "创建通知渠道：config_json 是渠道完整配置（含密钥，只写不读）。" +
				"邮件渠道示例：{\"to_email\":\"a@b.com\",\"smtp_host\":\"smtp.example.com\",\"smtp_password\":\"...\"}。",
			Args: []ArgSpec{
				{Name: "name", Type: ArgString, Required: true, Description: "渠道名称。"},
				{Name: "type", Type: ArgString, Required: true, Description: "渠道类型，例如 email、qq、captcha。"},
				{Name: "config_json", Type: ArgString, Description: "渠道完整配置 JSON（含密钥，只写不读）。"},
				{Name: "event_types", Type: ArgString, Description: "订阅的事件类型编码，例如全部或逗号分隔列表。"},
				{Name: "enabled", Type: ArgBoolean, Description: "创建后是否启用，默认 false。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// name、type 是渠道名称与类型。
				name, nameErr := args.String("name")
				if nameErr != nil {
					return nil, nameErr
				}
				// channelType 是渠道协议类型。
				channelType, typeErr := args.String("type")
				if typeErr != nil {
					return nil, typeErr
				}
				// id、createErr 是新渠道标识与错误。
				id, createErr := p.CreateChannel(ctx, identity.UserID, notificationsapp.ChannelInput{
					Name: name, Type: channelType, Config: args.OptionalString("config_json", ""),
					EventTypes: args.OptionalString("event_types", ""), Enabled: args.OptionalBool("enabled", false),
				})
				if createErr != nil {
					return nil, createErr
				}
				return notificationChannelMutationResult{ChannelID: id}, nil
			},
		},
		ToolDef{
			Name: "notification_channel_update",
			Description: "部分更新通知渠道：只提交需要变更的字段，未提交字段保留现值。" +
				"密钥类字段通过 config_json 整体重写（只写不读）；只改邮件收件地址时请用 email_recipient，避免丢失 SMTP 凭据。",
			Args: []ArgSpec{
				{Name: "channel_id", Type: ArgInteger, Required: true, Description: "渠道标识。"},
				{Name: "name", Type: ArgString, Description: "新的渠道名称。"},
				{Name: "type", Type: ArgString, Description: "新的渠道类型。"},
				{Name: "config_json", Type: ArgString, Description: "渠道完整配置 JSON（含密钥，只写不读）。"},
				{Name: "email_recipient", Type: ArgString, Description: "仅邮件渠道：只改收件地址并保留现有 SMTP 凭据。"},
				{Name: "event_types", Type: ArgString, Description: "新的订阅事件类型编码。"},
				{Name: "enabled", Type: ArgBoolean, Description: "新的启用状态。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是渠道标识。
				id, err := args.Int("channel_id")
				if err != nil {
					return nil, err
				}
				// updateErr 是渠道更新用例返回的错误。
				if updateErr := p.UpdateChannel(ctx, identity.UserID, int64(id), channelPatchFromArgs(args)); updateErr != nil {
					return nil, updateErr
				}
				return notificationChannelMutationResult{ChannelID: int64(id)}, nil
			},
		},
		ToolDef{
			Name:        "notification_channel_delete",
			Description: "删除通知渠道及其账号绑定（不可逆，会停止相关通知投递）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "channel_id", Type: ArgInteger, Required: true, Description: "要删除的渠道标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是渠道标识。
				id, err := args.Int("channel_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是渠道删除用例返回的错误。
				if deleteErr := p.DeleteChannel(ctx, identity.UserID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return notificationChannelMutationResult{ChannelID: int64(id)}, nil
			},
		},
		ToolDef{
			Name: "notification_channel_test",
			Description: "向指定渠道发送一条测试通知（真实外部触达：邮件或机器人消息）。必须显式 confirm=true。" +
				"失败提示不回显渠道密钥。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "channel_id", Type: ArgInteger, Required: true, Description: "渠道标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是渠道标识。
				id, err := args.Int("channel_id")
				if err != nil {
					return nil, err
				}
				// testErr 是测试发送用例返回的错误。
				if testErr := p.TestChannel(ctx, identity.UserID, int64(id)); testErr != nil {
					return nil, testErr
				}
				return notificationChannelMutationResult{ChannelID: int64(id)}, nil
			},
		},
	)
}

// registerNotificationBindingTools 注册账号渠道绑定工具。
func (e *Endpoint) registerNotificationBindingTools(p NotificationChannelPorts) {
	// accountArg 是绑定工具通用的账号入参定义。
	accountArg := ArgSpec{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"}
	e.RegisterTools(
		ToolDef{
			Name:        "notification_binding_list",
			Description: "列出全部账号与通知渠道的绑定（含渠道名称与启用状态）。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是绑定摘要列表。
				rows, listErr := p.ListBindings(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				// bindings 是绑定视图列表。
				bindings := make([]notificationBindingDTO, 0, len(rows))
				// row 是当前待映射的绑定摘要。
				for _, row := range rows {
					bindings = append(bindings, notificationBindingDTO{
						BindingID: row.ID, AccountID: row.CookieID, ChannelID: row.ChannelID,
						ChannelName: row.ChannelName, Enabled: row.Enabled,
					})
				}
				return notificationBindingListResult{Total: len(bindings), Bindings: bindings}, nil
			},
		},
		ToolDef{
			Name:        "notification_binding_get",
			Description: "读取指定账号当前启用的渠道标识列表。只读。",
			Args:        []ArgSpec{accountArg},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// ids、listErr 是账号启用的渠道标识。
				ids, listErr := p.GetBindingIDs(ctx, identity.UserID, accountID)
				if listErr != nil {
					return nil, listErr
				}
				return notificationBindingIDsResult{AccountID: accountID, ChannelIDs: ids}, nil
			},
		},
		ToolDef{
			Name: "notification_binding_set",
			Description: "整组覆盖指定账号的渠道绑定：未提交的渠道会被解绑。请先 notification_binding_get 读取现状。" +
				"channel_ids 传空数组表示解绑全部渠道。",
			Args: []ArgSpec{accountArg, {Name: "channel_ids", Type: ArgArray, ItemType: ArgInteger,
				Description: "该账号要启用的渠道标识数组；传空数组表示全部解绑。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// ids、idsErr 是待启用的渠道标识数组。
				ids, idsErr := intArrayArg(args, "channel_ids")
				if idsErr != nil {
					return nil, idsErr
				}
				// setErr 是整组设置用例返回的错误。
				if setErr := p.SetBindings(ctx, identity.UserID, accountID, ids); setErr != nil {
					return nil, setErr
				}
				return notificationBindingIDsResult{AccountID: accountID, ChannelIDs: ids}, nil
			},
		},
		ToolDef{
			Name:        "notification_binding_toggle",
			Description: "切换指定账号中单个渠道绑定的启用状态，不影响该账号的其它渠道。",
			Args: []ArgSpec{
				accountArg,
				{Name: "channel_id", Type: ArgInteger, Required: true, Description: "渠道标识。"},
				{Name: "enabled", Type: ArgBoolean, Required: true, Description: "true 启用绑定，false 停用绑定。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// id 是渠道标识。
				id, err := args.Int("channel_id")
				if err != nil {
					return nil, err
				}
				// enabled 是目标启用状态。
				enabled, err := args.Bool("enabled")
				if err != nil {
					return nil, err
				}
				// toggleErr 是单条切换用例返回的错误。
				if toggleErr := p.SetSingleBinding(ctx, identity.UserID, accountID, int64(id), enabled); toggleErr != nil {
					return nil, toggleErr
				}
				return notificationBindingDTO{AccountID: accountID, ChannelID: int64(id), Enabled: enabled}, nil
			},
		},
		ToolDef{
			Name:        "notification_binding_delete",
			Description: "删除一条账号渠道绑定（不可逆，会停止该账号在该渠道的通知）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "binding_id", Type: ArgInteger, Required: true, Description: "绑定记录标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是绑定标识。
				id, err := args.Int("binding_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是绑定删除用例返回的错误。
				if deleteErr := p.DeleteBinding(ctx, identity.UserID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return notificationBindingDTO{BindingID: int64(id)}, nil
			},
		},
		ToolDef{
			Name:        "notification_binding_clear_account",
			Description: "清空指定账号的全部渠道绑定（不可逆，会停止该账号全部通知）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{accountArg},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// clearErr 是账号绑定清空用例返回的错误。
				if clearErr := p.DeleteAccountBindings(ctx, identity.UserID, accountID); clearErr != nil {
					return nil, clearErr
				}
				return notificationBindingIDsResult{AccountID: accountID}, nil
			},
		},
	)
}

// registerNotificationUncertainTools 注册不确定通知的用户视图与管理员全局视图工具。
func (e *Endpoint) registerNotificationUncertainTools(p UncertainNotificationPorts) {
	e.RegisterTools(
		ToolDef{
			Name: "notification_uncertain_list",
			Description: "列出当前管理员渠道下结果不确定的通知（外部已尝试发送但本地确认失败），" +
				"只返回事件分类与计数等脱敏元数据，便于人工核对。只读。",
			Args: []ArgSpec{{Name: "limit", Type: ArgInteger, Description: "最多返回条数，默认 20，最大 200。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// items、total、listErr 是不确定通知摘要与总数。
				items, total, listErr := p.ListUncertainForUser(ctx, identity.UserID,
					clampLimit(args.OptionalInt("limit", 20), 20, maxPageSize))
				if listErr != nil {
					return nil, listErr
				}
				return notificationUncertainListResult{Total: total, Items: notificationUncertainDTOsFromApp(items, false)}, nil
			},
		},
		ToolDef{
			Name:        "notification_uncertain_list_all",
			Description: "管理员全局视图：列出全部用户的渠道不确定通知摘要（含所属用户标识），用于跨用户排查。只读。",
			Args:        []ArgSpec{{Name: "limit", Type: ArgInteger, Description: "最多返回条数，默认 20，最大 200。"}},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// items、total、listErr 是全局不确定通知摘要与总数。
				items, total, listErr := p.ListUncertainForAdmin(ctx,
					clampLimit(args.OptionalInt("limit", 20), 20, maxPageSize))
				if listErr != nil {
					return nil, listErr
				}
				return notificationUncertainListResult{Total: total, Items: notificationUncertainDTOsFromApp(items, true)}, nil
			},
		},
	)
}

// notificationChannelDTOFromApp 把渠道摘要映射为非敏感视图。
func notificationChannelDTOFromApp(summary notificationsapp.ChannelSummary) notificationChannelDTO {
	return notificationChannelDTO{
		ChannelID: summary.ID, Name: summary.Name, Type: summary.Type,
		EventTypes: summary.EventTypes, Enabled: summary.Enabled,
	}
}

// notificationUncertainDTOsFromApp 批量映射不确定通知摘要；includeOwner 控制是否返回所属用户。
func notificationUncertainDTOsFromApp(items []notificationsapp.UncertainSummary, includeOwner bool) []notificationUncertainDTO {
	// dtos 是不确定通知视图列表。
	dtos := make([]notificationUncertainDTO, 0, len(items))
	// item 是当前待映射的摘要。
	for _, item := range items {
		// dto 是脱敏后的视图；仅管理员视图携带所属用户。
		dto := notificationUncertainDTO{
			NotificationID: item.ID, ChannelID: item.ChannelID, EventType: item.EventType,
			AttemptCount: item.AttemptCount, UncertainAt: item.UncertainAt, HasError: item.HasError,
		}
		if includeOwner {
			dto.OwnerUserID = item.OwnerUserID
		}
		dtos = append(dtos, dto)
	}
	return dtos
}

// channelPatchFromArgs 把 MCP 入参转换为渠道部分更新补丁；未提交字段保持 nil 以保留现值。
func channelPatchFromArgs(args Arguments) notificationsapp.ChannelPatch {
	// patch 是待提交的部分更新；字符串指针字段只在显式提交时赋值。
	var patch notificationsapp.ChannelPatch
	// value、ok 是渠道名称入参及其存在性。
	if value, ok := args["name"]; ok {
		// text 是新的渠道名称。
		if text, isText := value.(string); isText {
			patch.Name = &text
		}
	}
	// value、ok 是渠道类型入参及其存在性。
	if value, ok := args["type"]; ok {
		// text 是新的渠道类型。
		if text, isText := value.(string); isText {
			patch.Type = &text
		}
	}
	// value、ok 是渠道配置入参及其存在性。
	if value, ok := args["config_json"]; ok {
		// text 是新的渠道完整配置 JSON（只写不读）。
		if text, isText := value.(string); isText {
			patch.Config = &text
		}
	}
	// value、ok 是邮件收件地址入参及其存在性。
	if value, ok := args["email_recipient"]; ok {
		// text 是新的邮件收件地址。
		if text, isText := value.(string); isText {
			patch.EmailRecipient = &text
		}
	}
	// value、ok 是订阅事件编码入参及其存在性。
	if value, ok := args["event_types"]; ok {
		// text 是新的订阅事件编码。
		if text, isText := value.(string); isText {
			patch.EventTypes = &text
		}
	}
	// value、ok 是启用状态入参及其存在性。
	if value, ok := args["enabled"]; ok {
		// flag 是新的启用状态。
		if flag, isFlag := value.(bool); isFlag {
			patch.Enabled = &flag
		}
	}
	return patch
}

// intArrayArg 读取整数数组入参；允许空数组，元素非整数时返回中文参数错误。
func intArrayArg(args Arguments, name string) ([]int64, error) {
	// raw、ok 是原始数组值及其存在性。
	raw, ok := args[name]
	if !ok {
		return nil, InvalidArgument("缺少必填参数 " + name)
	}
	// array 是通用数组。
	array, ok := raw.([]any)
	if !ok {
		return nil, InvalidArgument("参数 " + name + " 必须是整数数组")
	}
	// values 是类型化后的整数列表。
	values := make([]int64, 0, len(array))
	// element 是当前数组元素。
	for _, element := range array {
		// number、err 是元素的整数化结果。
		number, err := asInt(name, element)
		if err != nil {
			return nil, err
		}
		values = append(values, int64(number))
	}
	return values, nil
}
