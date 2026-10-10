// ports.go 定义客服 Agent 运行时的输入模型与两个最小端口。
//
// 端口由本包按自身用例需要定义，由组合层用既有应用服务与模型 SDK 投影实现。

package agent

import (
	"context"

	"xianyu-go/internal/capability"
)

// Session 是一次客服 Agent 运行的账号作用域。
// Preset 是该账号生效的能力档位；UserID 与 CookieID 是 Principal 的构造来源。
type Session struct {
	// UserID 是账号所属用户标识，用于数据隔离。
	UserID int64
	// CookieID 是当前账号标识；Agent 的全部工具调用都被锁定在该账号内。
	CookieID string
	// Preset 是该账号生效的能力档位。
	Preset capability.Preset
}

// ToolSchema 是暴露给模型的一次工具声明。
// Parameters 是 JSON Schema 对象，描述模型可填写的入参。
type ToolSchema struct {
	// Name 是能力名，必须与 capability.Catalog 登记的名字一致。
	Name string
	// Description 是给模型的中文用途说明。
	Description string
	// Parameters 是入参 JSON Schema；nil 表示该工具不接受入参。
	Parameters map[string]any
}

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	// ID 是模型给出的调用标识，回执消息必须原样带回。
	ID string
	// Name 是目标能力名。
	Name string
	// Args 是模型填写的入参；调用方不得直接信任其中的账号参数。
	Args map[string]any
}

// Message 是循环内的一条 chat 消息。
type Message struct {
	// Role 取 system / user / assistant / tool。
	Role string
	// Content 是消息正文；assistant 消息带工具调用时可为空。
	Content string
	// ToolCalls 是 assistant 消息携带的工具调用。
	ToolCalls []ToolCall
	// ToolCallID 是 tool 消息回执对应的调用标识。
	ToolCallID string
}

// SessionResolver 按账号解析客服 Agent 当前生效的会话配置。
//
// 端口由本包定义、由组合层投影实现：它回答「这个账号现在跑不跑、跑在哪一档」，
// 运行时只消费结论，不解释配置来源。
//
// 解析发生在每次补全之前而不是账号启动时：管理界面的开关与档位是即时生效的配置项，
// 若必须重启账号才能被观察到，那个开关就会在无声中失效。
type SessionResolver interface {
	// ResolveSession 返回账号当前生效的会话配置。
	// enabled 为 false 表示不运行 Agent；err 非空表示配置不可解析，调用方必须按未启用处理。
	ResolveSession(ctx context.Context, cookieID string) (session Session, enabled bool, err error)
}

// ToolSource 是客服 Agent 可调用的能力工具集合。
type ToolSource interface {
	// Schemas 返回当前会话档位下被放行（Decision 为 allow）的工具声明；
	// 被拒绝或未登记的能力不得出现在结果里，从源头避免模型尝试越权调用。
	Schemas(ctx context.Context, session Session) []ToolSchema
	// Call 执行一次工具调用并返回给模型看的文本结果。
	// 未放行的调用必须返回错误且不得产生任何副作用。
	Call(ctx context.Context, session Session, call ToolCall) (string, error)
}

// StepRequest 是一次模型补全请求，携带可用的工具声明。
type StepRequest struct {
	// APIKey 是模型接口密钥，属敏感值，禁止写入日志。
	APIKey string
	// BaseURL 是接口基地址。
	BaseURL string
	// Model 是模型名。
	Model string
	// Temperature 是采样温度。
	Temperature float32
	// Messages 是按时间正序的完整消息列表。
	Messages []Message
	// Tools 是本次可用的工具声明；为空表示本轮要求模型直接作答。
	Tools []ToolSchema
}

// StepResult 是一次模型补全结果。
type StepResult struct {
	// Content 是模型给出的正文。
	Content string
	// ToolCalls 是模型请求执行的工具调用；非空时 Content 通常为空。
	ToolCalls []ToolCall
}

// ModelClient 是 Agent 循环使用的一次模型补全端口，需要支持函数调用。
type ModelClient interface {
	// Step 执行一次模型补全。
	Step(ctx context.Context, req StepRequest) (StepResult, error)
}
