// result.go 提供 MCP 工具结果的统一构造器：成功结果携带稳定 JSON 文本，
// 失败结果携带中文摘要且 isError=true，领域处理器不直接拼装底层协议结构。

package mcp

import (
	"encoding/json"

	mcpproto "github.com/mark3labs/mcp-go/mcp"
)

// JSONResult 把任意可序列化 DTO 编码为 JSON 文本工具结果。
// 编码失败属于不应发生的编程错误，回退为内部错误结果而不是返回半截内容。
func JSONResult(payload any) (*mcpproto.CallToolResult, error) {
	// encoded 是 DTO 的稳定 JSON 编码；MCP 侧 DTO 全部为不含秘密的具名结构。
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, Fail(ClassInternal, "构造工具结果失败", err)
	}
	return &mcpproto.CallToolResult{
		Content: []mcpproto.Content{
			mcpproto.TextContent{Type: "text", Text: string(encoded)},
		},
	}, nil
}

// TextResult 构造纯文本工具结果，用于自检等不需要结构化 DTO 的工具。
func TextResult(text string) *mcpproto.CallToolResult {
	return &mcpproto.CallToolResult{
		Content: []mcpproto.Content{
			mcpproto.TextContent{Type: "text", Text: text},
		},
	}
}

// ErrorResult 把错误归一为 isError=true 的中文工具结果；返回的结果与错误类别一并给出，
// 便于调用方在写审计时复用同一类别而不二次分类。
func ErrorResult(err error) (*mcpproto.CallToolResult, ErrorClass) {
	// class、message 是归一后的稳定类别与中文公开摘要。
	class, message := Classify(err)
	return &mcpproto.CallToolResult{
		IsError: true,
		Content: []mcpproto.Content{
			mcpproto.TextContent{Type: "text", Text: message},
		},
	}, class
}
