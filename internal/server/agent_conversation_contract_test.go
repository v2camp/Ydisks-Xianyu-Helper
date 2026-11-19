// agent_conversation_contract_test.go 验证客服接管会话与消息接口的鉴权、参数校验与 snake_case 响应。

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentTakeoverConversationsContract 验证接管会话列表返回 takeover_count 与分页字段。
func TestAgentTakeoverConversationsContract(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、测试数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含客服 Agent 路由的真实 Router。
	handler := srv.Router()
	// ctx 是本用例共用的数据库上下文。
	ctx := context.Background()
	// 会话与 AI 记忆夹具：acc1 已在测试模板中归属管理员。
	if // err 是会话夹具插入失败原因。
	_, err := store.DB.ExecContext(ctx, `INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		"acc1", "chat-takeover", "buyer-1", "买家", 1700000000000, 1700000000000, 1700000000000, true, 0); err != nil {
		t.Fatalf("插入会话夹具失败: %v", err)
	}
	if // err 是 AI 记忆夹具插入失败原因。
	_, err := store.DB.ExecContext(ctx, `INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`,
		"acc1", "chat-takeover", "buyer-1", "item-1", "assistant", "回复"); err != nil {
		t.Fatalf("插入 AI 记忆夹具失败: %v", err)
	}
	// unauthenticated 是未携带会话 Cookie 的请求。
	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/conversations", nil)
	// unauthenticatedRecorder 捕获未认证响应。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("未认证应 401，实际 %d", unauthenticatedRecorder.Code)
	}
	assertOpenAPIResponse(t, unauthenticated, unauthenticatedRecorder)
	// sessionCookie 是管理员会话 Cookie。
	sessionCookie := loginHelper(t, handler)
	// listRequest 是接管会话列表请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/conversations?limit=10", nil)
	listRequest.AddCookie(sessionCookie)
	// listRecorder 捕获列表响应。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	assertOpenAPISuccessResponse(t, listRequest, listRecorder)
	// body 是列表响应原文。
	body := listRecorder.Body.String()
	// field 是列表响应必须出现的 snake_case 字段。
	for _, field := range []string{`"sessions"`, `"has_more"`, `"takeover_count"`, `"last_message_at"`, `"peer_user_id"`} {
		if !strings.Contains(body, field) {
			t.Fatalf("列表响应缺少字段 %s，实际 %s", field, body)
		}
	}
	// cursorRequest 是缺少时间参数的不完整游标请求。
	cursorRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/conversations?before_chat_id=chat-takeover", nil)
	cursorRequest.AddCookie(sessionCookie)
	// cursorRecorder 捕获游标错误响应。
	cursorRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cursorRecorder, cursorRequest)
	if cursorRecorder.Code != http.StatusBadRequest {
		t.Fatalf("不完整游标应 400，实际 %d body=%s", cursorRecorder.Code, cursorRecorder.Body.String())
	}
}

// TestAgentTakeoverMessagesContract 验证消息接口复用聊天历史 DTO 并要求会话定位参数。
func TestAgentTakeoverMessagesContract(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、测试数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含客服 Agent 路由的真实 Router。
	handler := srv.Router()
	// ctx 是本用例共用的数据库上下文。
	ctx := context.Background()
	// 会话与消息夹具：归属测试模板中的管理员账号 acc1。
	if // err 是会话夹具插入失败原因。
	_, err := store.DB.ExecContext(ctx, `INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		"acc1", "chat-messages", "buyer-1", "买家", 1700000000000, 1700000000000, 1700000000000, true, 0); err != nil {
		t.Fatalf("插入会话夹具失败: %v", err)
	}
	if // err 是消息夹具插入失败原因。
	_, err := store.DB.ExecContext(ctx, `INSERT INTO chat_messages (cookie_id,chat_id,message_key,direction,message_type,content,sent_at,created_at) VALUES (?,?,?,?,?,?,?,?)`,
		"acc1", "chat-messages", "key-1", "incoming", "text", "你好", 1700000000000, 1700000000000); err != nil {
		t.Fatalf("插入消息夹具失败: %v", err)
	}
	// sessionCookie 是管理员会话 Cookie。
	sessionCookie := loginHelper(t, handler)
	// missingRequest 是缺少 chat_id 的请求。
	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/conversations/messages?account_id=acc1", nil)
	missingRequest.AddCookie(sessionCookie)
	// missingRecorder 捕获参数缺失响应。
	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("缺少 chat_id 应 400，实际 %d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
	// messagesRequest 是合法消息请求。
	messagesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/conversations/messages?account_id=acc1&chat_id=chat-messages&limit=10", nil)
	messagesRequest.AddCookie(sessionCookie)
	// messagesRecorder 捕获消息响应。
	messagesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(messagesRecorder, messagesRequest)
	assertOpenAPISuccessResponse(t, messagesRequest, messagesRecorder)
	// body 是消息响应原文。
	body := messagesRecorder.Body.String()
	// field 是消息响应必须出现的 snake_case 字段。
	for _, field := range []string{`"messages"`, `"has_more"`, `"message_key"`, `"sent_at"`} {
		if !strings.Contains(body, field) {
			t.Fatalf("消息响应缺少字段 %s，实际 %s", field, body)
		}
	}
}
