// ops_agent_contract_test.go 验证运营 Agent 对话接口的鉴权、错误映射与 snake_case 响应契约。

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	opschatapp "xianyu-go/internal/application/opschat"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// opsAgentPortStub 是运营 Agent 端口的测试替身，用于覆盖处理器错误映射分支。
type opsAgentPortStub struct {
	// listPage、listErr 是会话分页预设结果。
	listPage opschatapp.ConversationPage
	listErr  error
	// messagesPage、messagesErr 是消息分页预设结果。
	messagesPage opschatapp.MessagePage
	messagesErr  error
	// sendMessage、sendConversation、sendErr 是发送预设结果。
	sendMessage      opschatapp.Message
	sendConversation opschatapp.Conversation
	sendErr          error
}

// List 返回预设会话分页。
func (s opsAgentPortStub) List(context.Context, int, int) (opschatapp.ConversationPage, error) {
	return s.listPage, s.listErr
}

// Messages 返回预设消息分页。
func (s opsAgentPortStub) Messages(context.Context, int64, int64, int) (opschatapp.MessagePage, error) {
	return s.messagesPage, s.messagesErr
}

// Send 返回预设发送结果。
func (s opsAgentPortStub) Send(context.Context, opschatapp.SendInput) (opschatapp.Message, opschatapp.Conversation, error) {
	return s.sendMessage, s.sendConversation, s.sendErr
}

// newOpsAgentStubRouter 构造只含运营发送路由的测试路由器，并注入管理员会话。
func newOpsAgentStubRouter(port opsAgentPortStub) http.Handler {
	// server 是只持有应用端口的最小 Server。
	server := &Server{applications: NewApplicationPorts(ApplicationPortsInput{OpsAgent: port})}
	// router 是待测路由。
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// session 是测试注入的管理员会话。
			session := &db.Session{UserID: 1, IsAdmin: true}
			next.ServeHTTP(w, r.WithContext(auth.WithSession(r.Context(), session)))
		})
	})
	router.Post("/api/v1/agent/ops/conversations/{id}/messages", server.sendOpsConversationMessage)
	return router
}

// TestOpsSendErrorMapping 验证发送错误映射到 400/404/409/502 与统一错误信封。
func TestOpsSendErrorMapping(t *testing.T) {
	// cases 是待验证的错误与期望状态码。
	cases := []struct {
		name   string
		err    error
		status int
	}{
		{name: "invalid-content", err: opschatapp.ErrInvalidContent, status: http.StatusBadRequest},
		{name: "not-found", err: opschatapp.ErrConversationNotFound, status: http.StatusNotFound},
		{name: "channel-missing", err: opschatapp.ErrChannelNotConfigured, status: http.StatusConflict},
		{name: "send-failed", err: opschatapp.ErrSendFailed, status: http.StatusBadGateway},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError},
	}
	// item 是当前待验证的错误映射用例。
	for _, item := range cases {
		// router 是注入预设失败的测试路由。
		router := newOpsAgentStubRouter(opsAgentPortStub{sendErr: item.err})
		// request 是带合法正文的发送请求。
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/ops/conversations/1/messages", strings.NewReader(`{"content":"hello"}`))
		request.Header.Set("Content-Type", "application/json")
		// recorder 捕获响应。
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != item.status {
			t.Fatalf("%s 应返回 %d，实际 %d body=%s", item.name, item.status, recorder.Code, recorder.Body.String())
		}
	}
}

// TestOpsSendSuccessShape 验证发送成功返回 201 与 snake_case 的消息与会话。
func TestOpsSendSuccessShape(t *testing.T) {
	// router 是注入成功结果的测试路由。
	router := newOpsAgentStubRouter(opsAgentPortStub{
		sendMessage:      opschatapp.Message{ID: 3, ConversationID: 1, Direction: "outbound", Source: "operator", Content: "hi", CreatedAt: 1700000000000},
		sendConversation: opschatapp.Conversation{ID: 1, Channel: "qq", TargetOpenID: "group-1", IsGroup: true, LastMessageAt: 1700000000000, LastMessagePreview: "hi"},
	})
	// request 是合法发送请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/ops/conversations/1/messages", strings.NewReader(`{"content":"hi"}`))
	request.Header.Set("Content-Type", "application/json")
	// recorder 捕获响应。
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("发送成功应 201，实际 %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	// body 是响应原文，用于断言 snake_case 字段存在。
	body := recorder.Body.String()
	// field 是必须出现的 snake_case 字段。
	for _, field := range []string{`"message"`, `"conversation"`, `"conversation_id"`, `"target_openid"`, `"is_group"`, `"last_message_preview"`} {
		if !strings.Contains(body, field) {
			t.Fatalf("响应缺少字段 %s，实际 %s", field, body)
		}
	}
}

// TestOpsAgentRoutesRequireAuthAndAdmin 验证运营接口对未认证与非管理员分别返回 401 与 403。
func TestOpsAgentRoutesRequireAuthAndAdmin(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、测试数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含运营路由的真实 Router。
	handler := srv.Router()
	// unauthenticated 是未携带会话 Cookie 的请求。
	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/agent/ops/conversations", nil)
	// unauthenticatedRecorder 捕获未认证响应。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("未认证应 401，实际 %d", unauthenticatedRecorder.Code)
	}
	assertOpenAPIResponse(t, unauthenticated, unauthenticatedRecorder)
	// 创建普通用户并登录，验证管理员门。
	created, createErr := store.Users.Create(context.Background(), "ops-user", "ops@e.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("创建普通用户失败: created=%v err=%v", created, createErr)
	}
	// userCookie 是普通用户会话。
	userCookie := loginAsHelper(t, handler, "ops-user", "pw")
	// forbidden 是普通用户的列表请求。
	forbidden := httptest.NewRequest(http.MethodGet, "/api/v1/agent/ops/conversations", nil)
	forbidden.AddCookie(userCookie)
	// forbiddenRecorder 捕获越权响应。
	forbiddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRecorder, forbidden)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("非管理员应 403，实际 %d body=%s", forbiddenRecorder.Code, forbiddenRecorder.Body.String())
	}
}

// TestOpsAgentRealServerFlow 验证真实服务上的列表、消息、404 与渠道未配置 409。
func TestOpsAgentRealServerFlow(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、测试数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含运营路由的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie。
	sessionCookie := loginHelper(t, handler)
	// message、_, recordErr 是建立运营会话的入站消息。
	message, _, recordErr := store.OpsChat.RecordInbound(context.Background(), db.OpsInboundRecord{
		TargetOpenID: "group-contract", PlatformMsgID: "msg-contract", Content: "你好", IsGroup: true, At: 1700000000000,
	})
	if recordErr != nil {
		t.Fatalf("写入运营会话失败: %v", recordErr)
	}
	// listRequest 是会话列表请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/ops/conversations", nil)
	listRequest.AddCookie(sessionCookie)
	// listRecorder 捕获列表响应。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	assertOpenAPISuccessResponse(t, listRequest, listRecorder)
	// listBody 是列表响应原文。
	listBody := listRecorder.Body.String()
	// field 是列表响应必须出现的字段。
	for _, field := range []string{`"conversations"`, `"total"`, `"page"`, `"page_size"`, `"total_pages"`, `"target_openid"`, `"last_message_at"`} {
		if !strings.Contains(listBody, field) {
			t.Fatalf("列表响应缺少字段 %s，实际 %s", field, listBody)
		}
	}
	// messagesRequest 是消息列表请求。
	messagesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/ops/conversations/"+itoa(message.ConversationID)+"/messages", nil)
	messagesRequest.AddCookie(sessionCookie)
	// messagesRecorder 捕获消息响应。
	messagesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(messagesRecorder, messagesRequest)
	assertOpenAPISuccessResponse(t, messagesRequest, messagesRecorder)
	if !strings.Contains(messagesRecorder.Body.String(), `"direction"`) {
		t.Fatalf("消息响应缺少 snake_case 字段: %s", messagesRecorder.Body.String())
	}
	// missingRequest 读取不存在的会话消息。
	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/ops/conversations/999999/messages", nil)
	missingRequest.AddCookie(sessionCookie)
	// missingRecorder 捕获 404 响应。
	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("不存在会话应 404，实际 %d", missingRecorder.Code)
	}
	assertOpenAPIResponse(t, missingRequest, missingRecorder)
	// emptySend 是空内容发送请求，应在触达渠道前被 400 拒绝。
	emptySend := httptest.NewRequest(http.MethodPost, "/api/v1/agent/ops/conversations/"+itoa(message.ConversationID)+"/messages", strings.NewReader(`{"content":"  "}`))
	emptySend.AddCookie(sessionCookie)
	// emptyRecorder 捕获空内容响应。
	emptyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(emptyRecorder, emptySend)
	if emptyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("空内容应 400，实际 %d body=%s", emptyRecorder.Code, emptyRecorder.Body.String())
	}
	// validSend 是凭据未配置时的发送请求。
	validSend := httptest.NewRequest(http.MethodPost, "/api/v1/agent/ops/conversations/"+itoa(message.ConversationID)+"/messages", strings.NewReader(`{"content":"在吗"}`))
	validSend.AddCookie(sessionCookie)
	// validRecorder 捕获渠道未配置响应。
	validRecorder := httptest.NewRecorder()
	handler.ServeHTTP(validRecorder, validSend)
	if validRecorder.Code != http.StatusConflict {
		t.Fatalf("凭据未配置应 409，实际 %d body=%s", validRecorder.Code, validRecorder.Body.String())
	}
	assertOpenAPIResponse(t, validSend, validRecorder)
	// count、countErr 是发送失败后的消息总数，必须保持不变。
	count, countErr := store.OpsChat.ListOpsMessages(context.Background(), message.ConversationID, 0, 10)
	if countErr != nil {
		t.Fatalf("读取消息数失败: %v", countErr)
	}
	if len(count.Messages) != 1 {
		t.Fatalf("发送失败不得落库，实际消息数 %d", len(count.Messages))
	}
}
