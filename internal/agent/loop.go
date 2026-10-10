// loop.go 实现客服 Agent 的工具回合循环。
//
// 循环只做编排，不含任何业务判定：可用工具由 capability 求值决定，
// 模型调用由 ModelClient 承担，结果正文最终仍要经过 engine 的价格守卫。

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"xianyu-go/internal/engine"
)

const (
	// roleSystem 是 system 消息角色。
	roleSystem = "system"
	// roleUser 是买家消息角色。
	roleUser = "user"
	// roleAssistant 是模型回复角色。
	roleAssistant = "assistant"
	// roleTool 是工具结果回执角色。
	roleTool = "tool"
	// roleAssistantForHistory 是对话历史中模型回复使用的角色名。
	roleAssistantForHistory = "assistant"
)

// Loop 是客服 Agent 的工具回合循环。
type Loop struct {
	model  ModelClient
	tools  ToolSource
	budget Budget
	logger *slog.Logger
}

// NewLoop 构造循环。model 为 nil 时 Run 直接返回错误并由上层回落；
// tools 为 nil 时循环退化为单次问答（不携带任何工具声明）。
func NewLoop(model ModelClient, tools ToolSource, budget Budget, logger *slog.Logger) *Loop {
	if logger == nil {
		logger = slog.Default()
	}
	return &Loop{
		model:  model,
		tools:  tools,
		budget: budget.normalized(),
		logger: logger.With("subsys", "support-agent"),
	}
}

// Run 执行一次 Agent 循环并返回最终回复正文。
// req 是 engine 解析好的模型配置与提示词；session 决定可用工具与账号作用域。
// 返回错误表示循环未能产出最终答复，调用方必须回落到单次问答。
func (l *Loop) Run(ctx context.Context, session Session, req engine.GenerateRequest) (string, error) {
	if l == nil || l.model == nil {
		return "", fmt.Errorf("客服 Agent 模型端口未装配")
	}
	if ctx == nil {
		return "", fmt.Errorf("客服 Agent 缺少调用上下文")
	}
	// loopCtx、cancel 是整次循环的总预算，保证 Agent 不会长期占住一条会话。
	loopCtx, cancel := context.WithTimeout(ctx, l.budget.TotalTimeout)
	defer cancel()

	// available 是本会话被放行的工具声明；档位不足或能力未登记时为空。
	available := l.availableTools(loopCtx, session)
	// messages 是完整消息列表：system 提示词 + 既有对话 + 本次买家输入。
	messages := buildMessages(req)

	// round 是当前回合序号；前 budget.MaxRounds 个回合允许调用工具。
	for round := 0; round <= l.budget.MaxRounds; round++ {
		// stepTools 是本回合交给模型的工具声明；最后一轮不再给工具，逼它给出最终答复。
		var stepTools []ToolSchema
		if round < l.budget.MaxRounds {
			stepTools = available
		}
		// result、err 分别是本回合的模型结果与调用失败原因。
		result, err := l.model.Step(loopCtx, StepRequest{
			APIKey:      req.APIKey,
			BaseURL:     req.BaseURL,
			Model:       req.Model,
			Temperature: req.Temperature,
			Messages:    messages,
			Tools:       stepTools,
		})
		if err != nil {
			return "", err
		}
		if len(result.ToolCalls) == 0 {
			return strings.TrimSpace(result.Content), nil
		}
		// 模型仍在请求工具但工具预算已用尽：不允许无上限地继续调用。
		if round >= l.budget.MaxRounds {
			return "", fmt.Errorf("%w：第 %d 回合仍请求调用工具", ErrBudgetExhausted, round)
		}
		messages = append(messages, Message{Role: roleAssistant, ToolCalls: result.ToolCalls})
		messages = append(messages, l.executeRound(loopCtx, session, result.ToolCalls)...)
	}
	return "", ErrBudgetExhausted
}

// availableTools 返回本会话被放行的工具声明；工具层缺失或无可用工具时返回 nil。
func (l *Loop) availableTools(ctx context.Context, session Session) []ToolSchema {
	if l.tools == nil {
		return nil
	}
	return l.tools.Schemas(ctx, session)
}

// executeRound 执行一个回合内的全部工具调用，并为每个调用生成回执消息。
// 超过单轮上限的调用只回执错误，不执行——避免模型用一次请求触发大批量读取。
func (l *Loop) executeRound(ctx context.Context, session Session, calls []ToolCall) []Message {
	// results 是与 calls 一一对应的回执消息，顺序必须保持一致。
	results := make([]Message, 0, len(calls))
	// index、call 分别是当前调用的位置与内容。
	for index, call := range calls {
		if index >= l.budget.MaxCallsPerRound {
			results = append(results, Message{
				Role:       roleTool,
				ToolCallID: call.ID,
				Content:    fmt.Sprintf("本轮工具调用数已超过上限 %d，该调用未执行。", l.budget.MaxCallsPerRound),
			})
			continue
		}
		results = append(results, Message{Role: roleTool, ToolCallID: call.ID, Content: l.callTool(ctx, session, call)})
	}
	return results
}

// callTool 在单次调用预算内执行一次工具调用，并把失败归一为文本结果回灌模型。
// 工具失败是模型应当看到的反馈（它可以换参数重试或直接作答），因此不中断循环；
// 但被拒绝的调用只返回错误文本，绝不执行。
func (l *Loop) callTool(ctx context.Context, session Session, call ToolCall) string {
	// toolCtx、cancel 是单次工具调用的收口预算。
	toolCtx, cancel := context.WithTimeout(ctx, l.budget.ToolTimeout)
	defer cancel()
	// content、err 分别是工具结果与执行失败原因。
	content, err := l.tools.Call(toolCtx, session, call)
	if err != nil {
		l.logger.Warn("客服 Agent 工具调用失败", "capability", call.Name, "err", err)
		return fmt.Sprintf("调用 %s 失败：%v", call.Name, err)
	}
	return content
}

// buildMessages 把 engine 的生成请求展开为循环使用的消息列表。
func buildMessages(req engine.GenerateRequest) []Message {
	// messages 是 system 提示词、历史回合与本次输入拼装出的消息列表。
	messages := make([]Message, 0, len(req.History)+2)
	if strings.TrimSpace(req.System) != "" {
		messages = append(messages, Message{Role: roleSystem, Content: req.System})
	}
	// turn 是当前遍历到的历史回合。
	for _, turn := range req.History {
		// role 是该回合在循环内使用的角色，非 assistant 一律按买家消息处理。
		role := roleUser
		if turn.Role == roleAssistantForHistory {
			role = roleAssistant
		}
		messages = append(messages, Message{Role: role, Content: turn.Content})
	}
	return append(messages, Message{Role: roleUser, Content: req.Current})
}
