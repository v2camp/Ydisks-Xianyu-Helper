package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestNotificationEventRoutes 验证租户与系统站内通知事件的列表、未读与已读接口。
func TestNotificationEventRoutes(t *testing.T) {
	// srv、store、cleanup 保存测试 HTTP 服务、数据库与资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整 HTTP 路由。
	handler := srv.Router()
	// adminCookie 是管理员登录凭证。
	adminCookie := loginHelper(t, handler)
	// admin、adminErr 保存测试管理员身份。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败: %v", adminErr)
	}
	// tenantErr 表示写入租户事件失败。
	if tenantErr := store.Notifications.RecordEvent(context.Background(), db.NotificationEventInput{
		Scope: db.NotificationScopeTenant, OwnerUserID: admin.ID, AccountID: "cid",
		EventType: "account_offline", Level: "warn", Title: "账号掉线", Body: "正文",
	}); tenantErr != nil {
		t.Fatal(tenantErr)
	}
	// systemErr 表示写入系统事件失败。
	if systemErr := store.Notifications.RecordEvent(context.Background(), db.NotificationEventInput{
		Scope: db.NotificationScopeSystem, EventType: "business_silence", Level: "warn", Title: "业务静默", Body: "正文",
	}); systemErr != nil {
		t.Fatal(systemErr)
	}
	// listRecorder 捕获租户事件列表响应。
	listRecorder := httptest.NewRecorder()
	// listRequest 是租户事件列表请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/events?limit=20", nil)
	listRequest.AddCookie(adminCookie)
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("租户事件列表 status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	// tenantList 保存解析后的租户事件列表。
	var tenantList notificationEventListResponse
	// err 是租户事件列表解析或断言错误。
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &tenantList); err != nil || len(tenantList.Items) != 1 || tenantList.UnreadCount != 1 {
		t.Fatalf("租户事件列表=%+v err=%v", tenantList, err)
	}
	// readRecorder 捕获单条已读响应。
	readRecorder := httptest.NewRecorder()
	// readRequest 是单条已读请求。
	readRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/events/"+itoa(tenantList.Items[0].ID)+"/read", nil)
	readRequest.AddCookie(adminCookie)
	handler.ServeHTTP(readRecorder, readRequest)
	if readRecorder.Code != http.StatusOK {
		t.Fatalf("租户已读 status=%d body=%s", readRecorder.Code, readRecorder.Body.String())
	}
	// readAllRecorder 捕获全部已读响应。
	readAllRecorder := httptest.NewRecorder()
	// readAllRequest 是全部已读请求。
	readAllRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/events/read-all", nil)
	readAllRequest.AddCookie(adminCookie)
	handler.ServeHTTP(readAllRecorder, readAllRequest)
	if readAllRecorder.Code != http.StatusOK || !strings.Contains(readAllRecorder.Body.String(), `"updated":0`) {
		t.Fatalf("租户全部已读 status=%d body=%s", readAllRecorder.Code, readAllRecorder.Body.String())
	}
	// invalidRecorder 捕获非法事件 ID 响应。
	invalidRecorder := httptest.NewRecorder()
	// invalidRequest 是非法事件 ID 请求。
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/events/abc/read", nil)
	invalidRequest.AddCookie(adminCookie)
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法事件 ID status=%d", invalidRecorder.Code)
	}
	// missingRecorder 捕获不存在事件响应。
	missingRecorder := httptest.NewRecorder()
	// missingRequest 是不存在事件请求。
	missingRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/events/999999/read", nil)
	missingRequest.AddCookie(adminCookie)
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("不存在事件 status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
	// systemRecorder 捕获系统事件列表响应。
	systemRecorder := httptest.NewRecorder()
	// systemRequest 是系统事件列表请求。
	systemRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/notifications/system-events?limit=20", nil)
	systemRequest.AddCookie(adminCookie)
	handler.ServeHTTP(systemRecorder, systemRequest)
	if systemRecorder.Code != http.StatusOK {
		t.Fatalf("系统事件列表 status=%d body=%s", systemRecorder.Code, systemRecorder.Body.String())
	}
	// systemList 保存解析后的系统事件列表。
	var systemList notificationEventListResponse
	// err 是系统事件列表解析或断言错误。
	if err := json.Unmarshal(systemRecorder.Body.Bytes(), &systemList); err != nil || len(systemList.Items) != 1 || systemList.UnreadCount != 1 || systemList.Items[0].Scope != db.NotificationScopeSystem {
		t.Fatalf("系统事件列表=%+v err=%v", systemList, err)
	}
	// systemReadRecorder 捕获系统单条已读响应。
	systemReadRecorder := httptest.NewRecorder()
	// systemReadRequest 是系统单条已读请求。
	systemReadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/notifications/system-events/"+itoa(systemList.Items[0].ID)+"/read", nil)
	systemReadRequest.AddCookie(adminCookie)
	handler.ServeHTTP(systemReadRecorder, systemReadRequest)
	if systemReadRecorder.Code != http.StatusOK {
		t.Fatalf("系统已读 status=%d body=%s", systemReadRecorder.Code, systemReadRecorder.Body.String())
	}
	// systemAllRecorder 捕获系统全部已读响应。
	systemAllRecorder := httptest.NewRecorder()
	// systemAllRequest 是系统全部已读请求。
	systemAllRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/notifications/system-events/read-all", nil)
	systemAllRequest.AddCookie(adminCookie)
	handler.ServeHTTP(systemAllRecorder, systemAllRequest)
	if systemAllRecorder.Code != http.StatusOK || !strings.Contains(systemAllRecorder.Body.String(), `"updated":0`) {
		t.Fatalf("系统全部已读 status=%d body=%s", systemAllRecorder.Code, systemAllRecorder.Body.String())
	}
	// 普通用户不能访问系统事件。
	createMemberErr := createNotificationTestMember(store)
	if createMemberErr != nil {
		t.Fatal(createMemberErr)
	}
	// memberCookie 是普通用户登录凭证。
	memberCookie := loginAsHelper(t, handler, "notification-member", "pw")
	// memberAdminRecorder 捕获普通用户访问系统事件响应。
	memberAdminRecorder := httptest.NewRecorder()
	// memberAdminRequest 是普通用户访问系统事件请求。
	memberAdminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/notifications/system-events", nil)
	memberAdminRequest.AddCookie(memberCookie)
	handler.ServeHTTP(memberAdminRecorder, memberAdminRequest)
	if memberAdminRecorder.Code != http.StatusForbidden {
		t.Fatalf("普通用户系统事件 status=%d", memberAdminRecorder.Code)
	}
	// memberRecorder 捕获普通用户租户事件列表响应；不得看到管理员事件。
	memberRecorder := httptest.NewRecorder()
	// memberRequest 是普通用户租户事件列表请求。
	memberRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/events", nil)
	memberRequest.AddCookie(memberCookie)
	handler.ServeHTTP(memberRecorder, memberRequest)
	// memberList 保存普通用户可见事件列表。
	var memberList notificationEventListResponse
	// err 是普通用户事件列表解析或断言错误。
	if err := json.Unmarshal(memberRecorder.Body.Bytes(), &memberList); err != nil || memberRecorder.Code != http.StatusOK || len(memberList.Items) != 0 {
		t.Fatalf("普通用户事件列表 status=%d list=%+v err=%v", memberRecorder.Code, memberList, err)
	}
}

// TestSystemAlertChannelRoutes 验证管理员系统告警渠道 CRUD、脱敏与权限边界。
func TestSystemAlertChannelRoutes(t *testing.T) {
	// srv、store、cleanup 保存测试 HTTP 服务、数据库与资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整 HTTP 路由。
	handler := srv.Router()
	// adminCookie 是管理员登录凭证。
	adminCookie := loginHelper(t, handler)
	// createRecorder 捕获创建响应。
	createRecorder := httptest.NewRecorder()
	// createRequest 是创建系统告警渠道请求。
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system-alert-channels", strings.NewReader(`{"name":"系统告警","type":"webhook","config":"{\"webhook_url\":\"https://secret.example.com\"}","event_types":"[\"business_silence\"]","enabled":true}`))
	createRequest.AddCookie(adminCookie)
	handler.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("创建系统渠道 status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	// created 保存创建结果中的渠道 ID。
	var created mutationIDResponse
	// err 是创建结果解析或断言错误。
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil || created.ID <= 0 {
		t.Fatalf("创建结果=%+v err=%v", created, err)
	}
	// listRecorder 捕获系统渠道列表响应。
	listRecorder := httptest.NewRecorder()
	// listRequest 是系统渠道列表请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-alert-channels", nil)
	listRequest.AddCookie(adminCookie)
	handler.ServeHTTP(listRecorder, listRequest)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), `"系统告警"`) {
		t.Fatalf("系统渠道列表 status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	// editorRecorder 捕获编辑器响应。
	editorRecorder := httptest.NewRecorder()
	// editorRequest 是编辑器请求。
	editorRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-alert-channels/"+itoa(created.ID), nil)
	editorRequest.AddCookie(adminCookie)
	handler.ServeHTTP(editorRecorder, editorRequest)
	if editorRecorder.Code != http.StatusOK {
		t.Fatalf("系统渠道编辑器 status=%d body=%s", editorRecorder.Code, editorRecorder.Body.String())
	}
	if strings.Contains(editorRecorder.Body.String(), "secret.example.com") {
		t.Fatalf("系统渠道编辑器泄露敏感配置: %s", editorRecorder.Body.String())
	}
	// updateRecorder 捕获更新响应。
	updateRecorder := httptest.NewRecorder()
	// updateRequest 是部分更新请求。
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/system-alert-channels/"+itoa(created.ID), strings.NewReader(`{"enabled":false}`))
	updateRequest.AddCookie(adminCookie)
	handler.ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("更新系统渠道 status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	// testRecorder 捕获测试发送响应；测试服务器未注入发送器，应返回 503。
	testRecorder := httptest.NewRecorder()
	// testRequest 是测试发送请求。
	testRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system-alert-channels/"+itoa(created.ID)+"/test", nil)
	testRequest.AddCookie(adminCookie)
	handler.ServeHTTP(testRecorder, testRequest)
	if testRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("测试发送 status=%d body=%s", testRecorder.Code, testRecorder.Body.String())
	}
	// deleteRecorder 捕获删除响应。
	deleteRecorder := httptest.NewRecorder()
	// deleteRequest 是删除请求。
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/system-alert-channels/"+itoa(created.ID), nil)
	deleteRequest.AddCookie(adminCookie)
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("删除系统渠道 status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	// missingRecorder 捕获删除后编辑器响应。
	missingRecorder := httptest.NewRecorder()
	// missingRequest 是删除后编辑器请求。
	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-alert-channels/"+itoa(created.ID), nil)
	missingRequest.AddCookie(adminCookie)
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("删除后编辑器 status=%d", missingRecorder.Code)
	}
	// invalidRecorder 捕获空创建请求响应。
	invalidRecorder := httptest.NewRecorder()
	// invalidRequest 是缺少必填字段的创建请求。
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system-alert-channels", strings.NewReader(`{"name":"","type":""}`))
	invalidRequest.AddCookie(adminCookie)
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("空创建请求 status=%d", invalidRecorder.Code)
	}
	// 普通用户不能维护系统告警渠道。
	createMemberErr := createNotificationTestMember(store)
	if createMemberErr != nil {
		t.Fatal(createMemberErr)
	}
	// memberCookie 是普通用户登录凭证。
	memberCookie := loginAsHelper(t, handler, "notification-member", "pw")
	// memberRecorder 捕获普通用户创建系统渠道响应。
	memberRecorder := httptest.NewRecorder()
	// memberRequest 是普通用户创建系统渠道请求。
	memberRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system-alert-channels", strings.NewReader(`{"name":"越权","type":"webhook"}`))
	memberRequest.AddCookie(memberCookie)
	handler.ServeHTTP(memberRecorder, memberRequest)
	if memberRecorder.Code != http.StatusForbidden {
		t.Fatalf("普通用户创建系统渠道 status=%d", memberRecorder.Code)
	}
}

// createNotificationTestMember 创建一个非管理员测试用户，供权限边界用例复用。
func createNotificationTestMember(store *db.Store) error {
	// _, err 保存用户创建结果与错误。
	_, err := store.Users.Create(context.Background(), "notification-member", "notification-member@example.com", "pw")
	return err
}
