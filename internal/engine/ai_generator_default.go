// ai_generator_default.go 提供 AIGenerator 的默认实现：单次调用 OpenAI 兼容 chat completions。
//
// 该实现承载接缝引入前的全部模型调用行为（客户端构造、消息拼装、超时预算、错误包装），
// 保证接缝落地后对外行为零变化。

package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

const (
	// defaultAIGeneratorTemperature 是 AIReplierImpl 使用的默认采样温度，与接缝引入前保持一致。
	defaultAIGeneratorTemperature float32 = 0.7
	// aiGenerationTimeout 是单次模型调用的收口预算，避免上游挂死拖垮会话。
	aiGenerationTimeout = 30 * time.Second
)

// DefaultAIGenerator 是 AIGenerator 的默认实现：一次调用、一个候选、无工具回合。
// 它不持有任何可变状态，可被多个账号的回复链路并发复用。
type DefaultAIGenerator struct{}

// NewDefaultAIGenerator 构造默认生成器。
// 返回接口类型，便于调用方在 Agent Loop 失败时回落到单次问答。
func NewDefaultAIGenerator() AIGenerator {
	return DefaultAIGenerator{}
}

// Generate 调用 OpenAI 兼容 chat completions 接口并返回首个候选文本。
// ctx 是调用方上下文；req 是本次补全输入。
// 返回错误时调用方不得使用返回文本；返回空字符串且无错误表示模型没有产出内容。
func (DefaultAIGenerator) Generate(ctx context.Context, req GenerateRequest) (string, error) {
	// clientCfg 是按请求解析出的客户端配置；BaseURL 为空时沿用 go-openai 默认地址。
	clientCfg := openai.DefaultConfig(req.APIKey)
	if req.BaseURL != "" {
		clientCfg.BaseURL = req.BaseURL
	}
	// httpClient、err 分别是按目标地址校验后的受控 HTTP 客户端及其构造失败原因。
	httpClient, err := newAIHTTPClient(clientCfg.BaseURL)
	if err != nil {
		return "", fmt.Errorf("AI API 地址无效: %w", err)
	}
	clientCfg.HTTPClient = httpClient
	// client 是本次补全使用的 OpenAI 兼容客户端。
	client := openai.NewClientWithConfig(clientCfg)

	// messages 是 system 提示词、历史回合与本次输入拼装出的消息列表。
	messages := make([]openai.ChatCompletionMessage, 0, len(req.History)+2)
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: req.System})
	// turn 是当前遍历到的历史回合。
	for _, turn := range req.History {
		// role 是该回合在接口协议中的角色；非 assistant 一律按买家消息处理。
		role := openai.ChatMessageRoleUser
		if turn.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: truncateAIContent(turn.Content)})
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: req.Current})

	// genCtx、cancel 是本次模型调用的有限收口预算，禁止使用脱离调用方的根 Context。
	genCtx, cancel := context.WithTimeout(ctx, aiGenerationTimeout)
	defer cancel()
	// resp、err 分别是模型响应及其调用失败原因。
	resp, err := client.CreateChatCompletion(genCtx, openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
	})
	if err != nil {
		return "", fmt.Errorf("AI 调用失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", nil
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
