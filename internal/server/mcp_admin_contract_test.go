package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAPIMCPAdminResponses 验证 MCP 管理接口的成功、未认证与非管理员响应均满足 OpenAPI 契约，
// 并断言令牌明文只在生成响应出现一次、状态与审计接口永不回显明文。
func TestOpenAPIMCPAdminResponses(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、测试数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含 MCP 管理路由的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie。
	sessionCookie := loginHelper(t, handler)

	// 未认证状态查询必须返回统一 401 envelope。
	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/status", nil)
	// unauthenticatedRecorder 捕获未认证请求的响应。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("未认证 MCP 状态应 401，got %d body=%s", unauthenticatedRecorder.Code, unauthenticatedRecorder.Body.String())
	}
	assertOpenAPIResponse(t, unauthenticated, unauthenticatedRecorder)

	// 非管理员访问 MCP 管理接口必须 403。
	if _, err := store.Users.Create(context.Background(), "mcpuser", "mcp@example.com", "pw"); err != nil {
		t.Fatalf("创建非管理员用户失败: %v", err)
	}
	// nonAdminCookie 是普通用户会话 Cookie。
	nonAdminCookie := loginAsHelper(t, handler, "mcpuser", "pw")
	// nonAdmin 是非管理员的状态查询请求。
	nonAdmin := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/status", nil)
	nonAdmin.AddCookie(nonAdminCookie)
	// nonAdminRecorder 捕获非管理员请求的响应。
	nonAdminRecorder := httptest.NewRecorder()
	handler.ServeHTTP(nonAdminRecorder, nonAdmin)
	if nonAdminRecorder.Code != http.StatusForbidden {
		t.Fatalf("非管理员访问 MCP 管理接口应 403，got %d body=%s", nonAdminRecorder.Code, nonAdminRecorder.Body.String())
	}
	assertOpenAPIResponse(t, nonAdmin, nonAdminRecorder)

	// 启用 MCP 服务并保持非本机访问关闭。
	settingsRequest := httptest.NewRequest(http.MethodPut, "/api/v1/mcp/settings", strings.NewReader(`{"enabled":true,"allow_non_loopback":false}`))
	settingsRequest.AddCookie(sessionCookie)
	// settingsRecorder 捕获设置更新响应。
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsRequest)
	assertOpenAPISuccessResponse(t, settingsRequest, settingsRecorder)

	// 生成令牌前状态应显示已启用且无令牌，接入地址非空。
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/status", nil)
	statusRequest.AddCookie(sessionCookie)
	// statusRecorder 捕获状态查询响应。
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	assertOpenAPISuccessResponse(t, statusRequest, statusRecorder)
	// before 是生成令牌前的状态响应。
	var before mcpStatusResponse
	// err 是状态响应 JSON 解析错误。
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &before); err != nil {
		t.Fatalf("解析 MCP 状态响应失败: %v", err)
	}
	if !before.Enabled || before.HasToken || before.Endpoint == "" {
		t.Fatalf("MCP 初始状态异常: %+v", before)
	}

	// 生成令牌：明文只在此响应出现。
	tokenRequest := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/token", nil)
	tokenRequest.AddCookie(sessionCookie)
	// tokenRecorder 捕获令牌生成响应。
	tokenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tokenRecorder, tokenRequest)
	assertOpenAPISuccessResponse(t, tokenRequest, tokenRecorder)
	// issued 是生成响应的令牌明文。
	var issued mcpTokenResponse
	// err 是令牌响应 JSON 解析错误。
	if err := json.Unmarshal(tokenRecorder.Body.Bytes(), &issued); err != nil || issued.Token == "" {
		t.Fatalf("解析 MCP 令牌响应失败: token=%q err=%v", issued.Token, err)
	}

	// 生成后状态必须显示存在令牌，且不包含明文。
	afterRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/status", nil)
	afterRequest.AddCookie(sessionCookie)
	// afterRecorder 捕获生成令牌后的状态响应。
	afterRecorder := httptest.NewRecorder()
	handler.ServeHTTP(afterRecorder, afterRequest)
	assertOpenAPISuccessResponse(t, afterRequest, afterRecorder)
	if strings.Contains(afterRecorder.Body.String(), issued.Token) {
		t.Fatal("状态接口回显了令牌明文")
	}
	// after 是生成令牌后的状态响应。
	var after mcpStatusResponse
	// err 是状态响应 JSON 解析错误。
	if err := json.Unmarshal(afterRecorder.Body.Bytes(), &after); err != nil {
		t.Fatalf("解析 MCP 状态响应失败: %v", err)
	}
	if !after.HasToken || after.TokenCreatedAt <= 0 {
		t.Fatalf("生成令牌后状态异常: %+v", after)
	}

	// 审计查询成功且不回显令牌明文。
	auditRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/audit?page=1&page_size=20", nil)
	auditRequest.AddCookie(sessionCookie)
	// auditRecorder 捕获审计查询响应。
	auditRecorder := httptest.NewRecorder()
	handler.ServeHTTP(auditRecorder, auditRequest)
	assertOpenAPISuccessResponse(t, auditRequest, auditRecorder)
	if strings.Contains(auditRecorder.Body.String(), issued.Token) {
		t.Fatal("审计接口泄漏令牌明文")
	}

	// 吊销令牌后状态必须立即回到无令牌。
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/mcp/token", nil)
	revokeRequest.AddCookie(sessionCookie)
	// revokeRecorder 捕获吊销令牌响应。
	revokeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(revokeRecorder, revokeRequest)
	assertOpenAPISuccessResponse(t, revokeRequest, revokeRecorder)

	// revokedRequest 是吊销后再次读取状态的请求。
	revokedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/status", nil)
	revokedRequest.AddCookie(sessionCookie)
	// revokedRecorder 捕获吊销后的状态响应。
	revokedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(revokedRecorder, revokedRequest)
	assertOpenAPISuccessResponse(t, revokedRequest, revokedRecorder)
	// revoked 是吊销令牌后的状态响应。
	var revoked mcpStatusResponse
	// err 是状态响应 JSON 解析错误。
	if err := json.Unmarshal(revokedRecorder.Body.Bytes(), &revoked); err != nil {
		t.Fatalf("解析 MCP 状态响应失败: %v", err)
	}
	if revoked.HasToken {
		t.Fatalf("吊销后仍报告存在令牌: %+v", revoked)
	}
}
