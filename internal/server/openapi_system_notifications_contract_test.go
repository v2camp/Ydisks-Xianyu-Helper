package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// systemAlertChannelTestSuccessPort 包装真实系统告警渠道端口，只把测试发送替换为成功。
type systemAlertChannelTestSuccessPort struct {
	// SystemAlertChannelsPort 是真实的系统告警渠道应用端口。
	SystemAlertChannelsPort
}

// Test 返回成功，避免契约测试依赖外部网络；其余 CRUD 仍走真实端口。
func (p systemAlertChannelTestSuccessPort) Test(context.Context, int64, time.Time) error {
	return nil
}

// TestOpenAPINotificationEventResponses 覆盖站内通知事件与系统告警渠道的全部成功响应契约。
func TestOpenAPINotificationEventResponses(t *testing.T) {
	// srv、store、cleanup 保存测试 HTTP 服务、数据库与资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// 系统告警渠道的测试发送使用成功替身，其余操作仍走真实数据库。
	srv.applications.systemAlertChannels = systemAlertChannelTestSuccessPort{SystemAlertChannelsPort: srv.applications.systemAlertChannels}
	// handler 是当前场景的真实 Router。
	handler := srv.Router()
	// adminCookie 是管理员认证会话。
	adminCookie := loginHelper(t, handler)
	// admin、adminErr 保存测试管理员身份。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败: %v", adminErr)
	}
	// tenantRecordErr 表示写入租户事件失败。
	if tenantRecordErr := store.Notifications.RecordEvent(context.Background(), db.NotificationEventInput{
		Scope: db.NotificationScopeTenant, OwnerUserID: admin.ID, AccountID: "cid",
		EventType: "account_offline", Level: "warn", Title: "账号掉线", Body: "正文",
	}); tenantRecordErr != nil {
		t.Fatal(tenantRecordErr)
	}
	// systemRecordErr 表示写入系统事件失败。
	if systemRecordErr := store.Notifications.RecordEvent(context.Background(), db.NotificationEventInput{
		Scope: db.NotificationScopeSystem, EventType: "business_silence", Level: "warn", Title: "业务静默", Body: "正文",
	}); systemRecordErr != nil {
		t.Fatal(systemRecordErr)
	}
	// tenantEvents、tenantErr 保存租户事件列表与读取错误；用于取得真实事件 ID。
	tenantEvents, tenantErr := store.Notifications.ListEventsForUser(context.Background(), admin.ID, 10)
	if tenantErr != nil || len(tenantEvents) == 0 {
		t.Fatalf("租户事件=%+v err=%v", tenantEvents, tenantErr)
	}
	// systemEvents、systemErr 保存系统事件列表与读取错误；用于取得真实事件 ID。
	systemEvents, systemErr := store.Notifications.ListSystemEvents(context.Background(), 10)
	if systemErr != nil || len(systemEvents) == 0 {
		t.Fatalf("系统事件=%+v err=%v", systemEvents, systemErr)
	}
	// listRequest 是租户事件列表契约请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/events", nil)
	listRequest.AddCookie(adminCookie)
	// listRecorder 捕获租户事件列表响应。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	assertOpenAPIRecordedSuccessResponse(t, listRequest, listRecorder)
	// readRequest 是租户单条已读契约请求。
	readRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/events/"+itoa(tenantEvents[0].ID)+"/read", nil)
	readRequest.AddCookie(adminCookie)
	// readRecorder 捕获租户单条已读响应。
	readRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readRecorder, readRequest)
	assertOpenAPIRecordedSuccessResponse(t, readRequest, readRecorder)
	// readAllRequest 是租户全部已读契约请求。
	readAllRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/events/read-all", nil)
	readAllRequest.AddCookie(adminCookie)
	// readAllRecorder 捕获租户全部已读响应。
	readAllRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readAllRecorder, readAllRequest)
	assertOpenAPIRecordedSuccessResponse(t, readAllRequest, readAllRecorder)
	// systemListRequest 是系统事件列表契约请求。
	systemListRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/notifications/system-events", nil)
	systemListRequest.AddCookie(adminCookie)
	// systemListRecorder 捕获系统事件列表响应。
	systemListRecorder := httptest.NewRecorder()
	handler.ServeHTTP(systemListRecorder, systemListRequest)
	assertOpenAPIRecordedSuccessResponse(t, systemListRequest, systemListRecorder)
	// systemReadRequest 是系统单条已读契约请求。
	systemReadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/notifications/system-events/"+itoa(systemEvents[0].ID)+"/read", nil)
	systemReadRequest.AddCookie(adminCookie)
	// systemReadRecorder 捕获系统单条已读响应。
	systemReadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(systemReadRecorder, systemReadRequest)
	assertOpenAPIRecordedSuccessResponse(t, systemReadRequest, systemReadRecorder)
	// systemReadAllRequest 是系统全部已读契约请求。
	systemReadAllRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/notifications/system-events/read-all", nil)
	systemReadAllRequest.AddCookie(adminCookie)
	// systemReadAllRecorder 捕获系统全部已读响应。
	systemReadAllRecorder := httptest.NewRecorder()
	handler.ServeHTTP(systemReadAllRecorder, systemReadAllRequest)
	assertOpenAPIRecordedSuccessResponse(t, systemReadAllRequest, systemReadAllRecorder)
	// createRequest 是创建系统告警渠道契约请求。
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system-alert-channels", strings.NewReader(`{"name":"系统告警","type":"webhook","config":"{}","event_types":"[\"business_silence\"]","enabled":true}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.AddCookie(adminCookie)
	// createRecorder 捕获系统渠道创建响应。
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)
	assertOpenAPIRecordedSuccessResponse(t, createRequest, createRecorder)
	// created 保存创建响应中的渠道主键。
	var created mutationIDResponse
	// decodeErr 是渠道创建响应解析错误。
	if decodeErr := json.Unmarshal(createRecorder.Body.Bytes(), &created); decodeErr != nil || created.ID <= 0 {
		t.Fatalf("系统渠道创建响应无效: %s", createRecorder.Body.String())
	}
	// channelPath 是当前系统渠道的版本化路径。
	channelPath := "/api/v1/admin/system-alert-channels/" + itoa(created.ID)
	// listChannelsRequest 是系统渠道列表契约请求。
	listChannelsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system-alert-channels", nil)
	listChannelsRequest.AddCookie(adminCookie)
	// listChannelsRecorder 捕获系统渠道列表响应。
	listChannelsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listChannelsRecorder, listChannelsRequest)
	assertOpenAPIRecordedSuccessResponse(t, listChannelsRequest, listChannelsRecorder)
	// editorRequest 是系统渠道编辑器契约请求。
	editorRequest := httptest.NewRequest(http.MethodGet, channelPath, nil)
	editorRequest.AddCookie(adminCookie)
	// editorRecorder 捕获系统渠道编辑器响应。
	editorRecorder := httptest.NewRecorder()
	handler.ServeHTTP(editorRecorder, editorRequest)
	assertOpenAPIRecordedSuccessResponse(t, editorRequest, editorRecorder)
	// updateRequest 是系统渠道部分更新契约请求。
	updateRequest := httptest.NewRequest(http.MethodPut, channelPath, strings.NewReader(`{"enabled":false}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.AddCookie(adminCookie)
	// updateRecorder 捕获系统渠道更新响应。
	updateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateRecorder, updateRequest)
	assertOpenAPIRecordedSuccessResponse(t, updateRequest, updateRecorder)
	// testRequest 是系统渠道测试发送契约请求。
	testRequest := httptest.NewRequest(http.MethodPost, channelPath+"/test", nil)
	testRequest.AddCookie(adminCookie)
	// testRecorder 捕获系统渠道测试发送响应。
	testRecorder := httptest.NewRecorder()
	handler.ServeHTTP(testRecorder, testRequest)
	assertOpenAPIRecordedSuccessResponse(t, testRequest, testRecorder)
	// deleteRequest 是系统渠道删除契约请求。
	deleteRequest := httptest.NewRequest(http.MethodDelete, channelPath, nil)
	deleteRequest.AddCookie(adminCookie)
	// deleteRecorder 捕获系统渠道删除响应。
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	assertOpenAPIRecordedSuccessResponse(t, deleteRequest, deleteRecorder)
}
