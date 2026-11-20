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

	"xianyu-go/internal/capability"
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

// RegisterNotificationTools 注册通知渠道与不确定通知域全部工具。
func (e *Endpoint) RegisterNotificationTools(channels capability.NotificationChannelPorts, uncertain capability.UncertainNotificationPorts) {
	if channels != nil {
		e.registerNotificationChannelTools(channels)
	}
	if uncertain != nil {
		e.registerNotificationUncertainTools(uncertain)
	}
}

// registerNotificationChannelTools 注册通知渠道 CRUD 与测试发送工具。
func (e *Endpoint) registerNotificationChannelTools(p capability.NotificationChannelPorts) {
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
					ToEmail: editor.ToEmail,
					Note:    "敏感字段（SMTP 密码、机器人 secret 等）只可写入、不可读取；更新时请通过 config_json 重新提交完整配置。",
				}, nil
			},
		},
		ToolDef{
			Name: "notification_channel_create",
			Description: "创建通知渠道：config_json 是渠道完整配置（含密钥，只写不读）。" +
				"邮件渠道示例：{\"to_email\":\"a@b.com\"}；SMTP 发件配置属于当前用户，不在渠道内维护。",
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
				"密钥类字段通过 config_json 整体重写（只写不读）；邮件渠道可直接用 config_json 提交 to_email。",
			Args: []ArgSpec{
				{Name: "channel_id", Type: ArgInteger, Required: true, Description: "渠道标识。"},
				{Name: "name", Type: ArgString, Description: "新的渠道名称。"},
				{Name: "type", Type: ArgString, Description: "新的渠道类型。"},
				{Name: "config_json", Type: ArgString, Description: "渠道完整配置 JSON（含密钥，只写不读）。"},
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

// registerNotificationUncertainTools 注册不确定通知的用户视图与管理员全局视图工具。
func (e *Endpoint) registerNotificationUncertainTools(p capability.UncertainNotificationPorts) {
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
	// ReplaceConfig 固定为 true：MCP 的 config_json 语义是「渠道完整配置」，保持整体替换而不触发 HTTP 编辑器的按键合并。
	patch.ReplaceConfig = true
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
