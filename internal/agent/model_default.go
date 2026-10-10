// model_default.go 提供支持函数调用的默认模型客户端。
//
// 这是 engine.DefaultAIGenerator 的工具版对照实现：engine 侧只做单次补全，
// 本客户端额外下发工具声明并解析模型返回的工具调用。

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"xianyu-go/internal/netguard"
)

const (
	// modelHTTPTimeout 是模型接口 HTTP 客户端的硬超时，必须大于单次补全预算以便上层先超时。
	modelHTTPTimeout = 30 * time.Second
	// modelStepTimeout 是单次补全的收口预算。
	modelStepTimeout = 20 * time.Second
)

// DefaultModelClient 是 ModelClient 的默认实现：OpenAI 兼容 chat completions + 函数调用。
type DefaultModelClient struct{}

// 编译期断言：默认客户端必须满足循环使用的端口。
var _ ModelClient = DefaultModelClient{}

// NewDefaultModelClient 构造默认模型客户端；实现无状态，可跨账号复用。
func NewDefaultModelClient() ModelClient {
	return DefaultModelClient{}
}

// Step 调用模型接口并解析正文与工具调用。
// ctx 是调用方上下文；req 是本次补全输入。返回错误时调用结果不可使用。
func (DefaultModelClient) Step(ctx context.Context, req StepRequest) (StepResult, error) {
	// clientCfg 是按请求解析出的客户端配置；BaseURL 为空时沿用 go-openai 默认地址。
	clientCfg := openai.DefaultConfig(req.APIKey)
	if req.BaseURL != "" {
		clientCfg.BaseURL = req.BaseURL
	}
	// httpClient、httpErr 分别是按目标地址校验后的受控 HTTP 客户端及其构造失败原因。
	httpClient, httpErr := newModelHTTPClient(clientCfg.BaseURL)
	if httpErr != nil {
		return StepResult{}, fmt.Errorf("模型接口地址无效: %w", httpErr)
	}
	clientCfg.HTTPClient = httpClient
	// client 是本次补全使用的 OpenAI 兼容客户端。
	client := openai.NewClientWithConfig(clientCfg)

	// stepCtx、cancel 是本次补全的有限收口预算，禁止使用脱离调用方的根 Context。
	stepCtx, cancel := context.WithTimeout(ctx, modelStepTimeout)
	defer cancel()
	// resp、err 分别是模型响应及其调用失败原因。
	resp, err := client.CreateChatCompletion(stepCtx, openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Tools:       toOpenAITools(req.Tools),
		Temperature: req.Temperature,
	})
	if err != nil {
		return StepResult{}, fmt.Errorf("模型调用失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return StepResult{}, nil
	}
	// message 是模型返回的首个候选消息。
	message := resp.Choices[0].Message
	return StepResult{
		Content:   strings.TrimSpace(message.Content),
		ToolCalls: fromOpenAIToolCalls(message.ToolCalls),
	}, nil
}

// newModelHTTPClient 按目标地址构造受控 HTTP 客户端；抽象为变量便于测试替换。
var newModelHTTPClient = func(baseURL string) (*http.Client, error) {
	return netguard.ConfiguredEndpointHTTPClient(baseURL, modelHTTPTimeout)
}

// toOpenAIMessages 把循环消息转换为 OpenAI 兼容消息。
func toOpenAIMessages(messages []Message) []openai.ChatCompletionMessage {
	// converted 是按原顺序转换后的消息列表。
	converted := make([]openai.ChatCompletionMessage, 0, len(messages))
	// message 是当前待转换的消息。
	for _, message := range messages {
		converted = append(converted, openai.ChatCompletionMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  toOpenAIToolCalls(message.ToolCalls),
		})
	}
	return converted
}

// toOpenAIToolCalls 把循环工具调用转换回 OpenAI 协议结构，回执消息必须原样带回调用标识。
func toOpenAIToolCalls(calls []ToolCall) []openai.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	// converted 是与输入等长的协议工具调用。
	converted := make([]openai.ToolCall, 0, len(calls))
	// call 是当前待转换的工具调用。
	for _, call := range calls {
		// arguments 是回填给模型的参数 JSON；必须始终是 JSON 对象，
		// 因为 json.Marshal(nil map) 会产出 null 而不是空对象。
		arguments := "{}"
		if len(call.Args) > 0 {
			// encoded、err 分别是参数序列化结果与失败原因；序列化失败时保留空对象。
			if encoded, err := json.Marshal(call.Args); err == nil {
				arguments = string(encoded)
			}
		}
		converted = append(converted, openai.ToolCall{
			ID:   call.ID,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      call.Name,
				Arguments: arguments,
			},
		})
	}
	return converted
}

// toOpenAITools 把能力工具声明转换为 OpenAI 协议的工具声明。
func toOpenAITools(schemas []ToolSchema) []openai.Tool {
	if len(schemas) == 0 {
		return nil
	}
	// converted 是与输入等长的协议工具声明。
	converted := make([]openai.Tool, 0, len(schemas))
	// schema 是当前待转换的工具声明。
	for _, schema := range schemas {
		// parameters 是传给模型的入参 JSON Schema；未声明入参时用空对象 schema。
		parameters := schema.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		converted = append(converted, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        schema.Name,
				Description: schema.Description,
				Parameters:  parameters,
			},
		})
	}
	return converted
}

// fromOpenAIToolCalls 把模型返回的工具调用解析为循环结构。
// 参数 JSON 解析失败时保留空入参：调用会被业务侧按缺参拒绝，而不是被静默忽略。
func fromOpenAIToolCalls(calls []openai.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	// converted 是与输入等长的循环工具调用。
	converted := make([]ToolCall, 0, len(calls))
	// call 是当前待解析的协议工具调用。
	for _, call := range calls {
		// args 是解析后的入参；解析失败时保持为 nil。
		var args map[string]any
		if strings.TrimSpace(call.Function.Arguments) != "" {
			// decodeErr 表示入参 JSON 解析失败原因；失败不阻断循环，交由业务侧拒绝。
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		converted = append(converted, ToolCall{ID: call.ID, Name: call.Function.Name, Args: args})
	}
	return converted
}
