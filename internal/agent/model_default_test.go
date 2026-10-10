// model_default_test.go 覆盖默认模型客户端的协议转换：
// 工具声明下发、工具调用解析、消息角色与调用标识的回灌。

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockChatServer 启动一个返回固定响应的 OpenAI 兼容模型服务。
// toolCall 非空时返回工具调用，否则返回正文；同时把收到的请求体解码回传给断言。
func mockChatServer(t *testing.T, content string, toolCalls []map[string]any) (*httptest.Server, *map[string]any) {
	t.Helper()
	// captured 是最近一次请求的解码结果。
	captured := &map[string]any{}
	// srv 是模型接口替身服务。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// decoded 是本次请求体的解码结果。
		decoded := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		*captured = decoded
		// message 是返回的候选消息体。
		message := map[string]any{"role": "assistant", "content": content}
		if len(toolCalls) > 0 {
			message["tool_calls"] = toolCalls
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": message}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// TestDefaultModelClientSendsToolsAndParsesToolCalls 验证工具声明被下发、
// 模型返回的工具调用被解析为循环结构，且调用标识与入参完整保留。
func TestDefaultModelClientSendsToolsAndParsesToolCalls(t *testing.T) {
	// srv、captured 分别是模型接口替身与请求捕获容器。
	srv, captured := mockChatServer(t, "", []map[string]any{
		{"id": "call-1", "type": "function", "function": map[string]any{
			"name": "order_list", "arguments": `{"status":"paid","page":2}`,
		}},
	})
	// client 是待测试的默认模型客户端。
	client := NewDefaultModelClient()

	// result、err 分别是补全结果与失败原因。
	result, err := client.Step(context.Background(), StepRequest{
		APIKey: "sk-test", BaseURL: srv.URL, Model: "test-model", Temperature: 0.7,
		Messages: []Message{{Role: roleUser, Content: "查到付了款的订单"}},
		Tools: []ToolSchema{{
			Name:        "order_list",
			Description: "查询订单",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatalf("模型调用不应失败: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("应解析出一个工具调用: %+v", result.ToolCalls)
	}
	// call 是解析出的工具调用。
	call := result.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "order_list" {
		t.Fatalf("工具调用标识或名称不符: %+v", call)
	}
	if call.Args["status"] != "paid" || call.Args["page"] != float64(2) {
		t.Fatalf("工具入参解析不符: %+v", call.Args)
	}
	// tools 是实际下发给模型的工具声明数组。
	tools, ok := (*captured)["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("工具声明必须下发给模型: %+v", (*captured)["tools"])
	}
}

// TestDefaultModelClientReturnsContentWithoutToolCalls 验证模型只给正文时正文被去空白返回。
func TestDefaultModelClientReturnsContentWithoutToolCalls(t *testing.T) {
	// srv、_ 分别是模型接口替身与请求捕获容器。
	srv, _ := mockChatServer(t, "  已经发货了  ", nil)
	// client 是待测试的默认模型客户端。
	client := NewDefaultModelClient()

	// result、err 分别是补全结果与失败原因。
	result, err := client.Step(context.Background(), StepRequest{
		APIKey: "sk-test", BaseURL: srv.URL, Model: "test-model",
		Messages: []Message{{Role: roleUser, Content: "发货了吗"}},
	})
	if err != nil || result.Content != "已经发货了" || len(result.ToolCalls) != 0 {
		t.Fatalf("正文应被去空白返回且无工具调用: %+v err=%v", result, err)
	}
}

// TestDefaultModelClientRejectsInvalidBaseURL 验证非法接口地址在发起请求前被拒绝。
func TestDefaultModelClientRejectsInvalidBaseURL(t *testing.T) {
	// client 是待测试的默认模型客户端。
	client := NewDefaultModelClient()
	if // stepErr 是非法接口地址被拒绝时返回的失败原因。
	_, stepErr := client.Step(context.Background(), StepRequest{
		APIKey: "sk-test", BaseURL: "://bad-url", Model: "test-model",
	}); stepErr == nil {
		t.Fatal("非法接口地址必须被拒绝")
	}
}

// TestToOpenAIToolCallsWithoutArgsUsesEmptyObject 验证无入参的工具调用回灌时结构完整。
func TestToOpenAIToolCallsWithoutArgsUsesEmptyObject(t *testing.T) {
	// converted 是无入参工具调用转换后的协议结构。
	converted := toOpenAIToolCalls([]ToolCall{{ID: "c1", Name: "item_list"}})
	if len(converted) != 1 || converted[0].Function.Arguments != "{}" {
		t.Fatalf("无入参工具调用应回灌空对象: %+v", converted)
	}
	if // empty 是空调用列表转换后的协议结构。
	empty := toOpenAIToolCalls(nil); empty != nil {
		t.Fatalf("空调用列表应返回 nil: %+v", empty)
	}
}
