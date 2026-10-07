// tools_ai.go 注册 AI 助手域工具：账号级 AI 回复设置读写、可用模型列表与连通性测试。
//
// 安全与语义红线：
//   - AI 回复设置摘要不含模型地址与 API 密钥；
//   - api_key 是只写参数：仅在本次请求内使用，不回传、不写入日志与响应；
//   - AI 议价与固定自动改价规则的冲突校验完全由 settings 应用服务负责，MCP 层不复制。

package mcp

import (
	"context"

	settingsapp "xianyu-go/internal/application/settings"
)

// aiReplySettingsDTO 是账号级 AI 回复设置摘要视图；不含模型地址、API 密钥等系统秘密。
type aiReplySettingsDTO struct {
	// AccountID 是设置所属账号标识。
	AccountID string `json:"account_id"`
	// AIEnabled 表示账号 AI 回复是否启用。
	AIEnabled bool `json:"ai_enabled"`
	// AutoAdjustPriceEnabled 表示是否把有效 AI 报价自动应用到买家新拍订单。
	AutoAdjustPriceEnabled bool `json:"auto_adjust_price_enabled"`
	// MaxDiscountPercent 是允许的最大折扣比例。
	MaxDiscountPercent int `json:"max_discount_percent"`
	// MaxDiscountAmount 是允许的最大折扣金额。
	MaxDiscountAmount int `json:"max_discount_amount"`
	// MaxBargainRounds 是允许的最大砍价轮次。
	MaxBargainRounds int `json:"max_bargain_rounds"`
	// CustomPrompts 是账号自定义提示词。
	CustomPrompts string `json:"custom_prompts,omitempty"`
}

// aiReplyListResult 是 AI 回复设置列表返回。
type aiReplyListResult struct {
	// Total 是账号数量。
	Total int `json:"total"`
	// Accounts 是账号 AI 设置列表。
	Accounts []aiReplySettingsDTO `json:"accounts"`
}

// aiModelListResult 是可用模型列表返回。
type aiModelListResult struct {
	// Total 是模型数量。
	Total int `json:"total"`
	// Models 是模型名称列表。
	Models []string `json:"models"`
	// Note 提示模型列表来源。
	Note string `json:"note,omitempty"`
}

// aiConnectionResult 是 AI 连通性测试返回；不含密钥与完整地址凭据。
type aiConnectionResult struct {
	// Model 是实际被测试的模型名称。
	Model string `json:"model"`
	// LatencyMS 是从发送请求到收到响应的毫秒数。
	LatencyMS int64 `json:"latency_ms"`
	// Reply 是模型回复正文摘要。
	Reply string `json:"reply,omitempty"`
	// Note 提示该结果仅为配置体检结论。
	Note string `json:"note"`
}

// RegisterAITools 注册 AI 助手域全部工具。
func (e *Endpoint) RegisterAITools(p SettingsPorts) {
	if p == nil {
		return
	}
	e.RegisterTools(
		ToolDef{
			Name: "ai_reply_list",
			Description: "列出全部账号的 AI 回复设置摘要（开关、折扣上限、砍价轮次与自定义提示词）。" +
				"不含模型地址与 API 密钥。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是账号 AI 设置摘要列表。
				rows, listErr := p.ListAIReply(ctx, identity.UserID)
				if listErr != nil {
					return nil, listErr
				}
				// accounts 是账号 AI 设置视图列表。
				accounts := make([]aiReplySettingsDTO, 0, len(rows))
				// row 是当前待映射的设置摘要。
				for _, row := range rows {
					accounts = append(accounts, aiReplySettingsDTOFromApp(row))
				}
				return aiReplyListResult{Total: len(accounts), Accounts: accounts}, nil
			},
		},
		ToolDef{
			Name:        "ai_reply_get",
			Description: "读取指定账号的 AI 回复设置摘要；未配置时返回默认值。只读。",
			Args:        []ArgSpec{{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// settings、getErr 是账号 AI 设置摘要。
				settings, getErr := p.GetAIReply(ctx, identity.UserID, accountID)
				if getErr != nil {
					return nil, getErr
				}
				return aiReplySettingsDTOFromApp(settings), nil
			},
		},
		ToolDef{
			Name: "ai_reply_set",
			Description: "保存指定账号的 AI 回复设置：AI 议价、自动改价、折扣与砍价轮次上限、自定义提示词。" +
				"开启自动改价前必须先启用 AI 议价；账号已有启用的固定改价规则时会被拒绝（避免两种改价方式冲突）。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Required: true, Description: "账号标识。"},
				{Name: "ai_enabled", Type: ArgBoolean, Required: true, Description: "是否启用 AI 回复/议价。"},
				{Name: "auto_adjust_price", Type: ArgBoolean, Description: "是否自动应用 AI 报价改价，默认 false。"},
				{Name: "max_discount_percent", Type: ArgInteger, Description: "最大折扣比例，0-100。"},
				{Name: "max_discount_amount", Type: ArgInteger, Description: "最大折扣金额，非负整数。"},
				{Name: "max_bargain_rounds", Type: ArgInteger, Description: "最大砍价轮次，1-10。"},
				{Name: "custom_prompts", Type: ArgString, Description: "账号自定义提示词。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// accountID 是账号标识。
				accountID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// aiEnabled 是 AI 回复开关。
				aiEnabled, err := args.Bool("ai_enabled")
				if err != nil {
					return nil, err
				}
				// input 是待保存的设置摘要；数值边界由应用服务校验。
				input := settingsapp.AIReplySettings{
					CookieID: accountID, AIEnabled: aiEnabled,
					AutoAdjustPriceEnabled: args.OptionalBool("auto_adjust_price", false),
					MaxDiscountPercent:     args.OptionalInt("max_discount_percent", 0),
					MaxDiscountAmount:      args.OptionalInt("max_discount_amount", 0),
					MaxBargainRounds:       args.OptionalInt("max_bargain_rounds", 1),
					CustomPrompts:          args.OptionalString("custom_prompts", ""),
				}
				// setErr 是 AI 设置保存用例返回的错误。
				if setErr := p.UpsertAIReply(ctx, identity.UserID, accountID, input); setErr != nil {
					return nil, setErr
				}
				return aiReplySettingsDTOFromApp(input), nil
			},
		},
		ToolDef{
			Name: "ai_models_list",
			Description: "读取 AI 服务可用模型列表。base_url 与 api_key 均可省略，省略时使用系统设置中已保存的配置。" +
				"api_key 只在本次请求内使用，不回传、不落库。只读（会记录密钥使用审计）。",
			Args: []ArgSpec{
				{Name: "base_url", Type: ArgString, Description: "AI 服务地址；省略时使用系统设置。"},
				{Name: "api_key", Type: ArgString, Description: "临时 API 密钥；省略时使用系统设置中已保存的密钥。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// models、listErr 是远端模型名称列表。
				models, listErr := p.ListAIModels(ctx, identity.UserID,
					args.OptionalString("base_url", ""), args.OptionalString("api_key", ""))
				if listErr != nil {
					return nil, listErr
				}
				return aiModelListResult{
					Total: len(models), Models: append([]string(nil), models...),
					Note: "模型列表来自 AI 服务端点；实际可用性以连通性测试为准。",
				}, nil
			},
		},
		ToolDef{
			Name: "ai_test_connection",
			Description: "发送一次最小对话请求验证 API 地址、密钥与模型组合是否可用，返回延迟与回复摘要。" +
				"base_url 与 api_key 省略时使用系统设置。api_key 只在本次请求内使用，不回传、不落库。只读。",
			Args: []ArgSpec{
				{Name: "base_url", Type: ArgString, Description: "AI 服务地址；省略时使用系统设置。"},
				{Name: "api_key", Type: ArgString, Description: "临时 API 密钥；省略时使用系统设置中已保存的密钥。"},
				{Name: "model", Type: ArgString, Required: true, Description: "要测试的模型名称。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// model 是待测试的模型名称。
				model, modelErr := args.String("model")
				if modelErr != nil {
					return nil, modelErr
				}
				// result、testErr 是连通性诊断结果。
				result, testErr := p.TestAIConnection(ctx, identity.UserID,
					args.OptionalString("base_url", ""), args.OptionalString("api_key", ""), model)
				if testErr != nil {
					return nil, testErr
				}
				return aiConnectionResult{
					Model: result.Model, LatencyMS: result.LatencyMS, Reply: result.Reply,
					Note: "该结果仅反映服务端到 AI 服务的连通性，不代表已保存的密钥与其它账号配置一致。",
				}, nil
			},
		},
	)
}

// aiReplySettingsDTOFromApp 把账号 AI 设置摘要映射为非敏感视图。
func aiReplySettingsDTOFromApp(settings settingsapp.AIReplySettings) aiReplySettingsDTO {
	return aiReplySettingsDTO{
		AccountID: settings.CookieID, AIEnabled: settings.AIEnabled,
		AutoAdjustPriceEnabled: settings.AutoAdjustPriceEnabled,
		MaxDiscountPercent:     settings.MaxDiscountPercent,
		MaxDiscountAmount:      settings.MaxDiscountAmount,
		MaxBargainRounds:       settings.MaxBargainRounds,
		CustomPrompts:          settings.CustomPrompts,
	}
}
