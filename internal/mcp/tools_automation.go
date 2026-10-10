// tools_automation.go 注册自动化规则域工具：
// 规则分页/全量查询、触发类型计数、规范化 dry-run 预览、新建、编辑与删除。
// 发货模板工具在 tools_delivery_templates.go，回复配置工具在 tools_replies.go。
//
// 安全与语义红线：规则校验完全复用 RuleService 的 Normalize/NormalizeForUpdate，
// MCP 层不复制触发类型、动作组合、模板绑定或价格等任何业务校验；
// dry-run 预览只回传规范化结果，绝不写入仓储；规则删除走 confirm 守卫。

package mcp

import (
	"context"
	"encoding/json"

	automationapp "xianyu-go/internal/application/automation"

	"xianyu-go/internal/capability"
)

// ruleDTO 是自动化规则的非敏感视图，字段与应用层规则模型一一对应。
type ruleDTO struct {
	// RuleID 是规则持久化标识。
	RuleID int64 `json:"rule_id"`
	// AccountID 是规则所属账号标识。
	AccountID string `json:"account_id"`
	// ItemID 是规则限定的商品标识；为空表示账号级规则。
	ItemID string `json:"item_id,omitempty"`
	// ItemTitle 是商品标题摘要。
	ItemTitle string `json:"item_title,omitempty"`
	// Name 是规则名称。
	Name string `json:"name"`
	// TriggerType 是触发类型：order_created/order_paid/buyer_reviewed/review_missing_timeout。
	TriggerType string `json:"trigger_type"`
	// Enabled 表示规则是否启用。
	Enabled bool `json:"enabled"`
	// Priority 是规则匹配优先级，数值越小越优先。
	Priority int `json:"priority"`
	// ConfigJSON 是规则扩展配置 JSON 文本。
	ConfigJSON string `json:"config_json,omitempty"`
	// SKUMigrationStatus 是规则的多 SKU 契约状态。
	SKUMigrationStatus string `json:"sku_migration_status,omitempty"`
	// Actions 是规则动作列表。
	Actions []ruleActionDTO `json:"actions"`
	// CreatedAt 是规则创建时间文本。
	CreatedAt string `json:"created_at,omitempty"`
	// UpdatedAt 是规则最近更新时间文本。
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ruleActionDTO 是规则动作的非敏感视图；不含卡密明文与任何凭证。
type ruleActionDTO struct {
	// ActionID 是动作持久化标识。
	ActionID int64 `json:"action_id"`
	// ActionType 是动作类型：confirm_shipment/send_card/send_template/send_text/adjust_price。
	ActionType string `json:"action_type"`
	// CardID 是发卡动作引用的卡密组标识。
	CardID int64 `json:"card_id,omitempty"`
	// CardName 是卡密组名称摘要。
	CardName string `json:"card_name,omitempty"`
	// DeliveryCount 是动作发送数量。
	DeliveryCount int `json:"delivery_count"`
	// MessageTemplate 是发送文本动作的文案。
	MessageTemplate string `json:"message_template,omitempty"`
	// DelaySeconds 是动作执行前延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// ConfigJSON 是动作扩展配置 JSON 文本。
	ConfigJSON string `json:"config_json,omitempty"`
	// Enabled 表示动作是否启用。
	Enabled bool `json:"enabled"`
	// SortOrder 是动作在规则中的顺序。
	SortOrder int `json:"sort_order"`
	// DeliveryTemplateID 是模板发货动作引用的发货模板标识。
	DeliveryTemplateID int64 `json:"delivery_template_id,omitempty"`
	// DeliveryTemplateName 是发货模板名称摘要。
	DeliveryTemplateName string `json:"delivery_template_name,omitempty"`
	// TemplateKeys 是模板动作需要绑定的变量键。
	TemplateKeys []string `json:"template_keys,omitempty"`
	// TemplateBindings 是模板变量到卡密组的绑定列表。
	TemplateBindings []ruleTemplateBindingDTO `json:"template_bindings,omitempty"`
	// CustomVariables 是传给模板的自定义字符串键值表。
	CustomVariables map[string]string `json:"custom_variables,omitempty"`
}

// ruleTemplateBindingDTO 是模板变量到卡密组的绑定视图。
type ruleTemplateBindingDTO struct {
	// VariableKey 是模板变量键。
	VariableKey string `json:"key"`
	// CardID 是绑定的卡密组标识。
	CardID int64 `json:"card_id"`
	// CardName 是卡密组名称摘要。
	CardName string `json:"card_name,omitempty"`
	// DeliveryCount 是每件商品准备的卡密份数。
	DeliveryCount int `json:"delivery_count"`
}

// ruleListResult 是规则全量列表返回。
type ruleListResult struct {
	// Total 是规则数量。
	Total int `json:"total"`
	// Rules 是规则视图列表。
	Rules []ruleDTO `json:"rules"`
}

// rulePageResult 是规则分页返回。
type rulePageResult struct {
	// Total 是符合条件的规则总数。
	Total int `json:"total"`
	// Page 是当前页码，从 1 开始。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// TotalPages 是总页数。
	TotalPages int `json:"total_pages"`
	// TriggerCounts 是按触发类型统计的规则数量。
	TriggerCounts map[string]int `json:"trigger_counts"`
	// Rules 是当前页规则视图列表。
	Rules []ruleDTO `json:"rules"`
}

// ruleTriggerCountsResult 是触发类型计数返回。
type ruleTriggerCountsResult struct {
	// Total 是命中过滤条件的规则总数。
	Total int `json:"total"`
	// Counts 是按触发类型统计的规则数量。
	Counts map[string]int `json:"counts"`
}

// ruleMutationResult 是规则写入操作的确认返回。
type ruleMutationResult struct {
	// RuleID 是被写入的规则标识。
	RuleID int64 `json:"rule_id"`
}

// rulePreviewDTO 是规则草稿的规范化 dry-run 结果；不包含数据库标识与时间戳。
type rulePreviewDTO struct {
	// DryRun 恒为 true，提示调用方本次未产生任何写入。
	DryRun bool `json:"dry_run"`
	// AccountID 是规则所属账号标识。
	AccountID string `json:"account_id"`
	// ItemID 是规则限定的商品标识；为空表示账号级规则。
	ItemID string `json:"item_id,omitempty"`
	// Name 是规范化后的规则名称；未提交时由服务端按触发类型生成。
	Name string `json:"name"`
	// TriggerType 是触发类型。
	TriggerType string `json:"trigger_type"`
	// Enabled 是规范化后的启用状态。
	Enabled bool `json:"enabled"`
	// Priority 是规范化后的优先级。
	Priority int `json:"priority"`
	// ConfigJSON 是规范化后的规则配置 JSON 文本。
	ConfigJSON string `json:"config_json,omitempty"`
	// SKUMigrationStatus 是服务端写入的多 SKU 契约状态。
	SKUMigrationStatus string `json:"sku_migration_status,omitempty"`
	// Actions 是规范化后的动作列表。
	Actions []ruleActionPreviewDTO `json:"actions"`
	// Note 是给 Harness 的下一步提示。
	Note string `json:"note"`
}

// ruleActionPreviewDTO 是规范化后的动作预览视图。
type ruleActionPreviewDTO struct {
	// ActionID 是更新场景下既有动作的标识；创建场景为零。
	ActionID int64 `json:"action_id,omitempty"`
	// ActionType 是动作类型。
	ActionType string `json:"action_type"`
	// CardID 是发卡动作引用的卡密组标识。
	CardID int64 `json:"card_id,omitempty"`
	// DeliveryCount 是发送数量。
	DeliveryCount int `json:"delivery_count"`
	// MessageTemplate 是发送文本动作的文案。
	MessageTemplate string `json:"message_template,omitempty"`
	// DelaySeconds 是动作延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// ConfigJSON 是规范化后的动作配置 JSON 文本。
	ConfigJSON string `json:"config_json,omitempty"`
	// Enabled 是规范化后的动作启用状态。
	Enabled bool `json:"enabled"`
	// SortOrder 是动作顺序。
	SortOrder int `json:"sort_order"`
	// DeliveryTemplateID 是模板发货动作引用的发货模板标识。
	DeliveryTemplateID int64 `json:"delivery_template_id,omitempty"`
	// TemplateBindings 是模板变量绑定列表。
	TemplateBindings []ruleTemplateBindingDTO `json:"template_bindings,omitempty"`
	// CustomVariables 是传给模板的自定义变量键值表。
	CustomVariables map[string]string `json:"custom_variables,omitempty"`
}

// templateDTO 是发货模板的非敏感视图。
type templateDTO struct {
	// TemplateID 是模板持久化标识。
	TemplateID int64 `json:"template_id"`
	// Name 是模板名称。
	Name string `json:"name"`
	// Enabled 表示模板是否允许新规则引用。
	Enabled bool `json:"enabled"`
	// Messages 是按发送顺序排列的消息列表。
	Messages []templateMessageDTO `json:"messages"`
	// Keys 是消息中提取的卡密变量键。
	Keys []string `json:"keys,omitempty"`
	// CustomKeys 是消息中引用的自定义变量键。
	CustomKeys []string `json:"custom_keys,omitempty"`
	// CreatedAt 是模板创建时间文本。
	CreatedAt string `json:"created_at,omitempty"`
	// UpdatedAt 是模板最近更新时间文本。
	UpdatedAt string `json:"updated_at,omitempty"`
}

// templateMessageDTO 是模板中的一条消息。
type templateMessageDTO struct {
	// MessageID 是消息标识。
	MessageID int64 `json:"message_id"`
	// SortOrder 是消息发送顺序。
	SortOrder int `json:"sort_order"`
	// Content 是消息正文。
	Content string `json:"content"`
}

// templateListResult 是发货模板列表返回。
type templateListResult struct {
	// Total 是模板数量。
	Total int `json:"total"`
	// Templates 是模板视图列表。
	Templates []templateDTO `json:"templates"`
}

// templateMutationResult 是模板写入操作的确认返回。
type templateMutationResult struct {
	// TemplateID 是被写入的模板标识。
	TemplateID int64 `json:"template_id"`
}

// ruleDraftRequest 是与 MCP 入参一一对应的规则草稿解码结构。
type ruleDraftRequest struct {
	// AccountID 是规则所属账号标识。
	AccountID string `json:"account_id"`
	// ItemID 是可选商品标识；为空表示账号级规则。
	ItemID string `json:"item_id"`
	// Name 是可选规则名称；省略时由服务端生成默认名称。
	Name string `json:"name"`
	// TriggerType 是触发类型。
	TriggerType string `json:"trigger_type"`
	// Enabled 表示规则是否启用。
	Enabled bool `json:"enabled"`
	// Priority 是可选优先级；非正数按服务端默认值处理。
	Priority int `json:"priority"`
	// ConfigJSON 是可选规则配置 JSON 文本。
	ConfigJSON string `json:"config_json"`
	// Actions 是规则动作列表。
	Actions []ruleActionRequest `json:"actions"`
}

// ruleActionRequest 是与 MCP 入参对应的规则动作解码结构。
type ruleActionRequest struct {
	// ActionID 是更新场景下既有动作标识。
	ActionID int64 `json:"action_id"`
	// ActionType 是动作类型。
	ActionType string `json:"action_type"`
	// CardID 是发卡动作引用的卡密组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是发送数量。
	DeliveryCount int `json:"delivery_count"`
	// MessageTemplate 是发送文本动作的文案。
	MessageTemplate string `json:"message_template"`
	// DelaySeconds 是动作延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// ConfigJSON 是动作扩展配置 JSON 文本。
	ConfigJSON string `json:"config_json"`
	// Enabled 是可选动作开关；省略时按启用处理。
	Enabled *bool `json:"enabled"`
	// SortOrder 是动作顺序。
	SortOrder int `json:"sort_order"`
	// DeliveryTemplateID 是模板发货动作引用的发货模板标识。
	DeliveryTemplateID int64 `json:"delivery_template_id"`
	// TemplateBindings 是模板变量到卡密组的绑定列表。
	TemplateBindings []ruleBindingRequest `json:"template_bindings"`
	// CustomVariables 是传给模板的自定义变量键值表。
	CustomVariables map[string]string `json:"custom_variables"`
}

// ruleBindingRequest 是与 MCP 入参对应的模板变量绑定解码结构。
type ruleBindingRequest struct {
	// VariableKey 是模板变量键。
	VariableKey string `json:"key"`
	// CardID 是绑定的卡密组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是每件商品准备的卡密份数。
	DeliveryCount int `json:"delivery_count"`
}

// templateDraftRequest 是与 MCP 入参对应的发货模板草稿解码结构。
type templateDraftRequest struct {
	// Name 是模板名称。
	Name string `json:"name"`
	// Enabled 表示模板是否允许新规则引用。
	Enabled bool `json:"enabled"`
	// Messages 是按顺序提交的消息正文。
	Messages []string `json:"messages"`
}

// decodeToolArguments 把 MCP 原始入参重新编码后解码为具名结构，复用 encoding/json 的字段映射与类型校验。
func decodeToolArguments(args Arguments, target any) error {
	// encoded 是原始入参的 JSON 表示；失败说明入参含不可编码值。
	encoded, err := json.Marshal(args)
	if err != nil {
		return InvalidArgument("工具入参不是可编码的 JSON 对象")
	}
	// err 表示入参字段与目标结构类型不匹配。
	if err := json.Unmarshal(encoded, target); err != nil {
		return InvalidArgument("工具入参字段类型与 schema 不一致，请检查嵌套数组与对象结构")
	}
	return nil
}

// RegisterRuleTools 注册自动化规则域全部工具。
func (e *Endpoint) RegisterRuleTools(p capability.RulePorts) {
	if p == nil {
		return
	}
	e.registerRuleQueryTools(p)
	e.registerRuleWriteTools(p)
}

// registerRuleQueryTools 注册规则查询、统计与规范化预览工具。
func (e *Endpoint) registerRuleQueryTools(p capability.RulePorts) {
	e.RegisterTools(
		ToolDef{
			Name: "rule_list",
			Description: "分页查询自动化规则，并返回按触发类型统计的数量。可按账号、触发类型、启用状态与关键词过滤。" +
				"只读，不返回卡密明文与任何凭证。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Description: "可选账号标识过滤条件。"},
				{Name: "trigger_type", Type: ArgString, Enum: triggerTypeValues(),
					Description: "可选触发类型过滤条件。"},
				{Name: "enabled", Type: ArgBoolean, Description: "可选启用状态过滤条件。"},
				{Name: "search", Type: ArgString, Description: "可选搜索词，匹配规则名称或商品。"},
				{Name: "page", Type: ArgInteger, Description: "页码，从 1 开始，默认 1。"},
				{Name: "page_size", Type: ArgInteger, Description: "每页条数，默认 20，最大 200。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				return rulePage(ctx, p, identity.UserID, args)
			},
		},
		ToolDef{
			Name:        "rule_list_all",
			Description: "返回当前管理员全部自动化规则（不分页）。规则数量较多时请改用 rule_list。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是规则全量列表。
				rows, listErr := p.ListRules(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				return ruleListResult{Total: len(rows), Rules: ruleDTOsFromApp(rows)}, nil
			},
		},
		ToolDef{
			Name:        "rule_trigger_counts",
			Description: "按触发类型统计自动化规则数量，支持的过滤条件与 rule_list 一致（不含分页）。只读。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Description: "可选账号标识过滤条件。"},
				{Name: "trigger_type", Type: ArgString, Enum: triggerTypeValues(),
					Description: "可选触发类型过滤条件。"},
				{Name: "enabled", Type: ArgBoolean, Description: "可选启用状态过滤条件。"},
				{Name: "search", Type: ArgString, Description: "可选搜索词，匹配规则名称或商品。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				return ruleCounts(ctx, p, identity.UserID, args)
			},
		},
	)
}

// registerRuleWriteTools 注册规则 dry-run 预览、新建、编辑与删除工具。
func (e *Endpoint) registerRuleWriteTools(p capability.RulePorts) {
	// draftArgs 是规则草稿的公共入参定义，预览、新建与编辑共用。
	draftArgs := ruleDraftArgs()
	e.RegisterTools(
		ToolDef{
			Name: "rule_preview",
			Description: "规范化 dry-run 预览：按 rule_list 相同的业务校验规则校验规则草稿并返回规范化结果，" +
				"不产生任何写入。传 rule_id 时按更新语义校验（可保留规则已引用的停用模板）。",
			Args: append([]ArgSpec{
				{Name: "rule_id", Type: ArgInteger, Description: "可选；传该参数表示按更新语义校验既有规则。"},
			}, draftArgs...),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				return rulePreview(ctx, p, identity.UserID, args)
			},
		},
		ToolDef{
			Name: "rule_create",
			Description: "创建自动化规则：先经服务端规范化校验，通过后写入并返回 rule_id。" +
				"触发类型与动作组合约束由服务端校验，失败时返回可直接修正的中文原因。",
			Args: draftArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				return ruleCreate(ctx, p, identity.UserID, args)
			},
		},
		ToolDef{
			Name: "rule_update",
			Description: "更新既有自动化规则：先按更新语义规范化校验（保留已引用的停用模板），再整体覆盖写入。" +
				"未提交的动作会被移除，请提交完整动作列表。",
			Args: append([]ArgSpec{
				{Name: "rule_id", Type: ArgInteger, Required: true, Description: "要更新的规则标识。"},
			}, draftArgs...),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				return ruleUpdate(ctx, p, identity.UserID, args)
			},
		},
		ToolDef{
			Name:        "rule_delete",
			Description: "删除自动化规则（不可逆）。规则仍有待处理运行时会被拒绝，需要先在 Web 端处理运行记录。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "rule_id", Type: ArgInteger, Required: true, Description: "要删除的规则标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是规则标识。
				id, err := args.Int("rule_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是删除用例返回的错误。
				if deleteErr := p.DeleteRule(ctx, identity.UserID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return ruleMutationResult{RuleID: int64(id)}, nil
			},
		},
	)
}

// ruleDraftArgs 返回规则草稿的公共入参定义。
func ruleDraftArgs() []ArgSpec {
	return []ArgSpec{
		{Name: "account_id", Type: ArgString, Required: true, Description: "规则所属账号标识。"},
		{Name: "trigger_type", Type: ArgString, Required: true, Enum: triggerTypeValues(),
			Description: "触发类型：order_created=拍下未付款，order_paid=付款后，buyer_reviewed=评价后，review_missing_timeout=超时未评价。"},
		{Name: "actions", Type: ArgArray, Required: true,
			Description: "动作对象数组，每项含 action_type、action_id（更新既有动作时传）、card_id、delivery_count、" +
				"message_template、delay_seconds、config_json、enabled、sort_order、delivery_template_id、template_bindings、custom_variables。"},
		{Name: "item_id", Type: ArgString, Description: "可选商品标识；省略表示账号级规则。"},
		{Name: "name", Type: ArgString, Description: "可选规则名称；省略时按触发类型生成。"},
		{Name: "enabled", Type: ArgBoolean, Description: "是否启用规则，默认 false。"},
		{Name: "priority", Type: ArgInteger, Description: "匹配优先级，数值越小越优先；非正数按服务端默认值处理。"},
		{Name: "config_json", Type: ArgString, Description: "可选规则扩展配置 JSON 文本。"},
	}
}

// triggerTypeValues 返回全部支持的触发类型枚举值。
func triggerTypeValues() []string {
	return []string{
		automationapp.TriggerOrderCreated, automationapp.TriggerOrderPaid,
		automationapp.TriggerBuyerReviewed, automationapp.TriggerReviewMissingTimeout,
	}
}

// rulePage 执行规则分页查询并组装统计结果。
func rulePage(ctx context.Context, p capability.RulePorts, userID int64, args Arguments) (any, error) {
	// limit、offset 是归一后的分页参数。
	limit, offset := args.Page()
	// page 是回显页码，从 1 开始。
	page := offset/limit + 1
	// filter 是分页与统计共用的过滤条件。
	filter := automationapp.RuleFilter{
		UserID: userID, CookieID: args.OptionalString("account_id", ""),
		TriggerType: args.OptionalString("trigger_type", ""), Search: args.OptionalString("search", ""),
	}
	// enabled 表示是否显式提交了启用状态过滤。
	if _, enabled := args["enabled"]; enabled {
		// value 是启用状态过滤值。
		value := args.OptionalBool("enabled", false)
		filter.Enabled = &value
	}
	filter.Limit, filter.Offset = limit, offset
	// rows、total、listErr 是当前页规则与总数。
	rows, total, listErr := p.ListRulesPage(ctx, filter)
	if listErr != nil {
		return nil, listErr
	}
	// counts、countErr 是同一过滤条件下的触发类型统计。
	counts, countErr := p.CountRulesByTrigger(ctx, filter)
	if countErr != nil {
		return nil, countErr
	}
	// totalPages 是总页数，无数据时为 0。
	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}
	return rulePageResult{
		Total: total, Page: page, PageSize: limit, TotalPages: totalPages,
		TriggerCounts: counts, Rules: ruleDTOsFromApp(rows),
	}, nil
}

// ruleCounts 执行触发类型计数查询。
func ruleCounts(ctx context.Context, p capability.RulePorts, userID int64, args Arguments) (any, error) {
	// filter 是计数过滤条件；分页字段不参与统计语义。
	filter := automationapp.RuleFilter{
		UserID: userID, CookieID: args.OptionalString("account_id", ""),
		TriggerType: args.OptionalString("trigger_type", ""), Search: args.OptionalString("search", ""),
	}
	// enabled 表示是否显式提交了启用状态过滤。
	if _, enabled := args["enabled"]; enabled {
		// value 是启用状态过滤值。
		value := args.OptionalBool("enabled", false)
		filter.Enabled = &value
	}
	// counts、countErr 是触发类型统计结果。
	counts, countErr := p.CountRulesByTrigger(ctx, filter)
	if countErr != nil {
		return nil, countErr
	}
	// total 是各触发类型计数之和，便于 Harness 快速判断规模。
	total := 0
	// count 是当前触发类型的规则数量。
	for _, count := range counts {
		total += count
	}
	return ruleTriggerCountsResult{Total: total, Counts: counts}, nil
}

// rulePreview 执行规则草稿的规范化 dry-run 预览；不写入任何数据。
func rulePreview(ctx context.Context, p capability.RulePorts, userID int64, args Arguments) (any, error) {
	// draft、draftErr 是解码后的规则草稿。
	draft, draftErr := ruleDraftFromArgs(args)
	if draftErr != nil {
		return nil, draftErr
	}
	// ruleID 是可选既有规则标识；大于零表示按更新语义校验。
	ruleID := args.OptionalInt("rule_id", 0)
	// input 是规范化结果。
	var input automationapp.RuleInput
	// normalizeErr 是规范化错误；更新语义下校验既有规则归属并允许保留已引用的停用模板。
	var normalizeErr error
	if ruleID > 0 {
		input, normalizeErr = p.NormalizeRuleForUpdate(ctx, userID, int64(ruleID), draft)
	} else {
		input, normalizeErr = p.NormalizeRule(ctx, userID, draft)
	}
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	return rulePreviewDTOFromInput(input), nil
}

// ruleCreate 规范化后创建自动化规则。
func ruleCreate(ctx context.Context, p capability.RulePorts, userID int64, args Arguments) (any, error) {
	// draft、draftErr 是解码后的规则草稿。
	draft, draftErr := ruleDraftFromArgs(args)
	if draftErr != nil {
		return nil, draftErr
	}
	// input、normalizeErr 是服务端规范化结果；业务校验完全由应用服务负责。
	input, normalizeErr := p.NormalizeRule(ctx, userID, draft)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	// id、createErr 是新规则标识与写入错误。
	id, createErr := p.CreateRule(ctx, input)
	if createErr != nil {
		return nil, createErr
	}
	return ruleMutationResult{RuleID: id}, nil
}

// ruleUpdate 按更新语义规范化后覆盖既有自动化规则。
func ruleUpdate(ctx context.Context, p capability.RulePorts, userID int64, args Arguments) (any, error) {
	// id 是规则标识。
	id, err := args.Int("rule_id")
	if err != nil {
		return nil, err
	}
	// draft、draftErr 是解码后的规则草稿。
	draft, draftErr := ruleDraftFromArgs(args)
	if draftErr != nil {
		return nil, draftErr
	}
	// input、normalizeErr 是服务端更新语义规范化结果。
	input, normalizeErr := p.NormalizeRuleForUpdate(ctx, userID, int64(id), draft)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	// updateErr 是规则更新用例返回的错误。
	if updateErr := p.UpdateRule(ctx, userID, int64(id), input); updateErr != nil {
		return nil, updateErr
	}
	return ruleMutationResult{RuleID: int64(id)}, nil
}

// ruleDraftFromArgs 把 MCP 入参解码为应用层规则草稿。
func ruleDraftFromArgs(args Arguments) (automationapp.RuleDraft, error) {
	// request 是规则草稿解码结果。
	var request ruleDraftRequest
	// err 是入参解码错误。
	if err := decodeToolArguments(args, &request); err != nil {
		return automationapp.RuleDraft{}, err
	}
	// actions 是转换后的动作草稿列表。
	actions := make([]automationapp.ActionDraft, 0, len(request.Actions))
	// action 是当前待转换的动作入参。
	for _, action := range request.Actions {
		// bindings 是当前动作的模板变量绑定。
		bindings := make([]automationapp.TemplateBinding, 0, len(action.TemplateBindings))
		// binding 是当前待转换的绑定入参。
		for _, binding := range action.TemplateBindings {
			bindings = append(bindings, automationapp.TemplateBinding{
				VariableKey: binding.VariableKey, CardID: binding.CardID, DeliveryCount: binding.DeliveryCount,
			})
		}
		actions = append(actions, automationapp.ActionDraft{
			ID: action.ActionID, ActionType: action.ActionType, CardID: action.CardID,
			DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate,
			DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled,
			SortOrder: action.SortOrder, DeliveryTemplateID: action.DeliveryTemplateID,
			TemplateBindings: bindings, CustomVariables: action.CustomVariables,
		})
	}
	return automationapp.RuleDraft{
		CookieID: request.AccountID, ItemID: request.ItemID, Name: request.Name,
		TriggerType: request.TriggerType, Enabled: request.Enabled, Priority: request.Priority,
		ConfigJSON: request.ConfigJSON, Actions: actions,
	}, nil
}

// ruleDTOsFromApp 批量映射规则应用模型为非敏感视图。
func ruleDTOsFromApp(rules []automationapp.Rule) []ruleDTO {
	// dtos 是规则视图列表。
	dtos := make([]ruleDTO, 0, len(rules))
	// rule 是当前待映射的规则应用模型。
	for _, rule := range rules {
		dtos = append(dtos, ruleDTOFromApp(rule))
	}
	return dtos
}

// ruleDTOFromApp 把规则应用模型映射为非敏感视图。
func ruleDTOFromApp(rule automationapp.Rule) ruleDTO {
	// actions 是动作视图列表。
	actions := make([]ruleActionDTO, 0, len(rule.Actions))
	// action 是当前待映射的动作应用模型。
	for _, action := range rule.Actions {
		actions = append(actions, ruleActionDTO{
			ActionID: action.ID, ActionType: action.ActionType, CardID: action.CardID, CardName: action.CardName,
			DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate,
			DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled,
			SortOrder: action.SortOrder, DeliveryTemplateID: action.DeliveryTemplateID,
			DeliveryTemplateName: action.DeliveryTemplateName, TemplateKeys: action.TemplateKeys,
			TemplateBindings: ruleBindingDTOsFromApp(action.TemplateBindings), CustomVariables: action.CustomVariables,
		})
	}
	return ruleDTO{
		RuleID: rule.ID, AccountID: rule.CookieID, ItemID: rule.ItemID, ItemTitle: rule.ItemTitle,
		Name: rule.Name, TriggerType: rule.TriggerType, Enabled: rule.Enabled, Priority: rule.Priority,
		ConfigJSON: rule.ConfigJSON, SKUMigrationStatus: rule.SKUMigrationStatus, Actions: actions,
		CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt,
	}
}

// ruleBindingDTOsFromApp 批量映射模板绑定为非敏感视图。
func ruleBindingDTOsFromApp(bindings []automationapp.TemplateBinding) []ruleTemplateBindingDTO {
	// 无绑定时返回 nil 以便省略该字段。
	if len(bindings) == 0 {
		return nil
	}
	// dtos 是绑定视图列表。
	dtos := make([]ruleTemplateBindingDTO, 0, len(bindings))
	// binding 是当前待映射的绑定应用模型。
	for _, binding := range bindings {
		dtos = append(dtos, ruleTemplateBindingDTO{
			VariableKey: binding.VariableKey, CardID: binding.CardID,
			CardName: binding.CardName, DeliveryCount: binding.DeliveryCount,
		})
	}
	return dtos
}

// rulePreviewDTOFromInput 把规范化结果映射为 dry-run 预览视图。
func rulePreviewDTOFromInput(input automationapp.RuleInput) rulePreviewDTO {
	// actions 是动作预览列表。
	actions := make([]ruleActionPreviewDTO, 0, len(input.Actions))
	// action 是当前待映射的规范化动作。
	for _, action := range input.Actions {
		actions = append(actions, ruleActionPreviewDTO{
			ActionID: action.ID, ActionType: action.ActionType, CardID: action.CardID,
			DeliveryCount: action.DeliveryCount, MessageTemplate: action.MessageTemplate,
			DelaySeconds: action.DelaySeconds, ConfigJSON: action.ConfigJSON, Enabled: action.Enabled,
			SortOrder: action.SortOrder, DeliveryTemplateID: action.DeliveryTemplateID,
			TemplateBindings: ruleBindingDTOsFromApp(action.TemplateBindings), CustomVariables: action.CustomVariables,
		})
	}
	return rulePreviewDTO{
		DryRun: true, AccountID: input.CookieID, ItemID: input.ItemID, Name: input.Name,
		TriggerType: input.TriggerType, Enabled: input.Enabled, Priority: input.Priority,
		ConfigJSON: input.ConfigJSON, SKUMigrationStatus: input.SKUMigrationStatus, Actions: actions,
		Note: "本次仅为规范化预览，未写入任何数据；确认无误后请调用 rule_create 或 rule_update 保存。",
	}
}
