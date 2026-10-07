// prompts.go 注册 MCP 运维提示（FR-19）：每日巡检、处理 needs_review 订单、新账号自动化开通向导、
// 卡密低库存排查补货、AI 配置体检。
//
// 安全与语义红线：提示只输出中文操作步骤、建议工具调用顺序与人工确认提醒，
// 不包含任何凭证、明文卡密或密钥；参数只做字符串插值与必填校验，不触达业务端口。

package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpproto "github.com/mark3labs/mcp-go/mcp"
)

const (
	// promptNameDailyInspection 是每日巡检提示名。
	promptNameDailyInspection = "daily_inspection"
	// promptNameResolveNeedsReview 是处理 needs_review 订单提示名。
	promptNameResolveNeedsReview = "resolve_needs_review_orders"
	// promptNameAccountAutomationOnboarding 是新账号自动化开通向导提示名。
	promptNameAccountAutomationOnboarding = "onboard_account_automation"
	// promptNameRestockLowStockCards 是卡密低库存排查补货提示名。
	promptNameRestockLowStockCards = "restock_low_stock_cards"
	// promptNameAIConfigCheck 是 AI 配置体检提示名。
	promptNameAIConfigCheck = "ai_config_health_check"
)

// promptManualConfirmReminder 是所有运维提示末尾统一附带的人工确认提醒。
const promptManualConfirmReminder = "\n\n安全提醒：任何标记为破坏性的工具（发送、发货、删除、改价、清空等）都必须先说明影响，" +
	"在获得人工明确同意后才可传 confirm=true；不确定结果（needs_review）禁止自动重试，必须先人工核对。"

// RegisterPrompts 注册全部运维提示；该注册不依赖任何业务端口。
func (e *Endpoint) RegisterPrompts() {
	if e == nil {
		return
	}
	e.registerDailyInspectionPrompt()
	e.registerResolveNeedsReviewPrompt()
	e.registerAccountOnboardingPrompt()
	e.registerRestockLowStockPrompt()
	e.registerAIConfigCheckPrompt()
}

// registerDailyInspectionPrompt 注册每日巡检提示。
func (e *Endpoint) registerDailyInspectionPrompt() {
	// prompt 是每日巡检提示定义。
	prompt := mcpproto.NewPrompt(promptNameDailyInspection,
		mcpproto.WithPromptDescription("按顺序巡检账号状态、自动化异常、待发货订单与卡密库存，输出可执行的当日待办。"))
	e.mcpServer.AddPrompt(prompt, func(ctx context.Context, request mcpproto.GetPromptRequest) (*mcpproto.GetPromptResult, error) {
		e.writeCallAudit(ctx, AuditCategoryPrompt, promptNameDailyInspection, "", true, "")
		// text 是巡检步骤正文。
		text := strings.Join([]string{
			"请按以下顺序完成今日巡检，并在每一步给出结论与异常清单：",
			"1. 调用 system_ping 与 settings_get_system 确认服务可用、MCP 服务已启用且关键开关符合预期。",
			"2. 调用 account_list 查看归属账号；对 enabled=false 或 paused_until 未过期的账号说明原因。",
			"3. 调用 analytics_dashboard 获取账号、卡密库存、关键词与订单计数，标记数量为 0 或明显偏离常态的项。",
			"4. 调用 automation_issue_list 列出 needs_review 运行与死信任务；每条给出建议处置方式（重试或人工核对）。",
			"5. 调用 order_list（status=pending_ship）查看待发货订单，确认是否有长时间未发货的订单。",
			"6. 调用 card_list 检查卡密库存行数，对低于 5 行的卡券组标记为需要补货。",
			"7. 汇总为「今日必办 / 需人工核对 / 观察项」三段，并对每条给出对应工具与参数建议。",
		}, "\n") + promptManualConfirmReminder
		return promptResult(text), nil
	})
}

// registerResolveNeedsReviewPrompt 注册处理 needs_review 订单提示。
func (e *Endpoint) registerResolveNeedsReviewPrompt() {
	// prompt 是处理 needs_review 订单提示定义。
	prompt := mcpproto.NewPrompt(promptNameResolveNeedsReview,
		mcpproto.WithPromptDescription("梳理需要人工处理的自动化异常与订单，逐条给出核对步骤与安全处置建议。"),
		mcpproto.WithArgument("account_id", mcpproto.ArgumentDescription("可选：只处理指定账号的异常。")),
		mcpproto.WithArgument("order_id", mcpproto.ArgumentDescription("可选：只处理指定订单。")),
	)
	e.mcpServer.AddPrompt(prompt, func(ctx context.Context, request mcpproto.GetPromptRequest) (*mcpproto.GetPromptResult, error) {
		// scope 是本次处理的过滤范围描述。
		scope := promptScope(request, "account_id", "order_id")
		e.writeCallAudit(ctx, AuditCategoryPrompt, promptNameResolveNeedsReview, promptArgument(request, "account_id"), true, "")
		// text 是处置步骤正文。
		text := fmt.Sprintf("请协助处理需要人工核对的自动化异常%s，步骤如下：\n"+
			"1. 调用 automation_issue_list 获取全部 needs_review 运行与死信任务，并记录 order_id、trigger_type 与错误摘要。\n"+
			"2. 对每条异常调用 order_get 读取订单当前状态，确认是否已发货、已取消或已完成。\n"+
			"3. 若订单已发货或已完成：说明该运行无需重放，建议用 automation_issue_resolve 关闭，避免重复动作。\n"+
			"4. 若订单仍处于待发货且确认平台未发出：说明可以安全重试，重试前必须先向人工确认。\n"+
			"5. 若无法从本地数据判断平台是否已发出：明确标注「平台状态不确定」，禁止自动重发，请人工到闲鱼核对。\n"+
			"6. 输出表格：订单号 / 异常类型 / 当前状态 / 结论（安全重试、需人工核对、忽略）/ 建议工具与参数。", scope) +
			promptManualConfirmReminder
		return promptResult(text), nil
	})
}

// registerAccountOnboardingPrompt 注册新账号自动化开通向导提示。
func (e *Endpoint) registerAccountOnboardingPrompt() {
	// prompt 是新账号自动化开通向导提示定义。
	prompt := mcpproto.NewPrompt(promptNameAccountAutomationOnboarding,
		mcpproto.WithPromptDescription("为指定账号逐步开通卡密、发货模板、自动化规则与 AI 回复，给出完整的下单检查清单。"),
		mcpproto.WithArgument("account_id", mcpproto.ArgumentDescription("必填：要开通自动化的闲鱼账号标识。")),
	)
	e.mcpServer.AddPrompt(prompt, func(ctx context.Context, request mcpproto.GetPromptRequest) (*mcpproto.GetPromptResult, error) {
		// accountID 是目标账号标识。
		accountID := strings.TrimSpace(promptArgument(request, "account_id"))
		e.writeCallAudit(ctx, AuditCategoryPrompt, promptNameAccountAutomationOnboarding, accountID, true, "")
		// scope 是缺少账号参数时的提示语。
		scope := fmt.Sprintf("账号 %s", accountID)
		if accountID == "" {
			return promptResult("请先在调用本提示时提供 account_id 参数（必填），再按后续步骤为该账号开通自动化。" +
				promptManualConfirmReminder), nil
		}
		// text 是开通步骤正文。
		text := fmt.Sprintf("请为 %s 按以下顺序开通自动化，每步完成后给出确认结论：\n"+
			"1. 调用 account_get 与 account_task_config_get 确认账号存在、已启用且长期登录、自动评价/擦亮配置符合预期。\n"+
			"2. 调用 card_list 检查是否已有可用的卡券组；没有则用 card_create 创建（text 发货话术 / data 逐行卡密 / api 接口取卡）。\n"+
			"3. 调用 template_list 检查发货模板；没有则用 template_create 创建，变量键将在规则中绑定卡密组。\n"+
			"4. 调用 rule_preview 校验规则草稿（付款后发货建议 send_card 或 send_template），确认无误后再用 rule_create 保存。\n"+
			"5. 如需自动确认发货，调用 account_set_auto_confirm 打开开关；发卡是自动确认发货前的最后一步。\n"+
			"6. 如需议价，调用 ai_reply_get 查看现状后用 ai_reply_set 配置；注意 AI 议价与固定改价规则不能同时启用。\n"+
			"7. 调用 settings_ai_test_connection 验证 AI 服务连通性，并汇总「已开通 / 待人工确认 / 尚未配置」三类结果。", scope) +
			promptManualConfirmReminder
		return promptResult(text), nil
	})
}

// registerRestockLowStockPrompt 注册卡密低库存排查补货提示。
func (e *Endpoint) registerRestockLowStockPrompt() {
	// prompt 是卡密低库存排查补货提示定义。
	prompt := mcpproto.NewPrompt(promptNameRestockLowStockCards,
		mcpproto.WithPromptDescription("排查卡密低库存卡券组，给出补货优先级与操作步骤。"),
		mcpproto.WithArgument("threshold", mcpproto.ArgumentDescription("可选：低库存阈值（非空卡密行数），默认 5。")),
	)
	e.mcpServer.AddPrompt(prompt, func(ctx context.Context, request mcpproto.GetPromptRequest) (*mcpproto.GetPromptResult, error) {
		// threshold 是本次使用的阈值文本。
		threshold := strings.TrimSpace(promptArgument(request, "threshold"))
		if threshold == "" {
			threshold = "5"
		}
		e.writeCallAudit(ctx, AuditCategoryPrompt, promptNameRestockLowStockCards, "", true, "")
		// text 是排查步骤正文。
		text := fmt.Sprintf("请排查低于 %s 行的卡券组并按优先级给出补货建议：\n"+
			"1. 调用 card_list 获取全部卡券组，只关注 type=data 的库存行数（api 类型由远端供货，不适用本阈值）。\n"+
			"2. 对低于阈值的卡券组按行数升序排列，行数越少优先级越高。\n"+
			"3. 调用 rule_list 检查这些卡券组是否被启用的自动化规则引用；被引用且已耗尽的卡券组会直接导致发货失败。\n"+
			"4. 对每条给出补货动作：调用 card_append_data 追加逐行卡密（只写不读），或说明应改用 card_update（会整体覆盖库存，需人工确认）。\n"+
			"5. 汇总表格：卡券组 / 当前行数 / 是否被规则引用 / 建议补货量 / 建议工具。", threshold) +
			promptManualConfirmReminder
		return promptResult(text), nil
	})
}

// registerAIConfigCheckPrompt 注册 AI 配置体检提示。
func (e *Endpoint) registerAIConfigCheckPrompt() {
	// prompt 是 AI 配置体检提示定义。
	prompt := mcpproto.NewPrompt(promptNameAIConfigCheck,
		mcpproto.WithPromptDescription("体检 AI 议价与回复配置：密钥可用性、模型连通性、账号级参数与改价方式冲突。"))
	e.mcpServer.AddPrompt(prompt, func(ctx context.Context, request mcpproto.GetPromptRequest) (*mcpproto.GetPromptResult, error) {
		e.writeCallAudit(ctx, AuditCategoryPrompt, promptNameAIConfigCheck, "", true, "")
		// text 是体检步骤正文。
		text := strings.Join([]string{
			"请对 AI 议价与回复配置做一次完整体检，按以下顺序执行并给出结论：",
			"1. 调用 settings_get_system 查看 ai_api_url 等服务端配置；敏感键只返回 configured 标记，不要尝试读取密钥原文。",
			"2. 调用 ai_models_list 拉取可用模型列表；若失败，说明是密钥还是服务地址问题，并给出修复建议。",
			"3. 调用 ai_test_connection 用目标模型做一次连通性测试，记录延迟与回复摘要。",
			"4. 调用 ai_reply_list 逐账号检查开关、折扣上限与砍价轮次是否符合业务预期。",
			"5. 调用 rule_list（trigger_type=order_created）检查是否存在启用的固定改价规则；若与某账号 AI 议价同时开启，标记为冲突并给出关闭建议。",
			"6. 汇总为「可用 / 需修正 / 存在冲突」三段，每段给出对应工具与具体参数。",
		}, "\n") + promptManualConfirmReminder
		return promptResult(text), nil
	})
}

// promptResult 构造单条用户消息的提示结果。
func promptResult(text string) *mcpproto.GetPromptResult {
	return &mcpproto.GetPromptResult{
		Messages: []mcpproto.PromptMessage{{
			Role:    mcpproto.RoleUser,
			Content: mcpproto.TextContent{Type: "text", Text: text},
		}},
	}
}

// promptArgument 读取提示参数并去除首尾空白；缺失时返回空串。
func promptArgument(request mcpproto.GetPromptRequest, name string) string {
	if request.Params.Arguments == nil {
		return ""
	}
	return strings.TrimSpace(request.Params.Arguments[name])
}

// promptScope 拼接本次处理的过滤范围描述；无参数时回退到"全部范围"。
func promptScope(request mcpproto.GetPromptRequest, names ...string) string {
	// parts 是已提供的过滤条件描述。
	parts := make([]string, 0, len(names))
	// name 是当前检查的参数名。
	for _, name := range names {
		// value 是本次调用为该参数提供的取值。
		if value := promptArgument(request, name); value != "" {
			parts = append(parts, name+"="+value)
		}
	}
	if len(parts) == 0 {
		return "（全部范围）"
	}
	return "（范围：" + strings.Join(parts, ", ") + "）"
}
