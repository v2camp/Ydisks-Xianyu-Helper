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

// TestListChannels 列出通知渠道。
func TestListChannels(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 先创建一个渠道。
	body := `{"name":"钉钉","type":"dingtalk","config":"{}","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列出。
	req2 := httptest.NewRequest(http.MethodGet, "/notification-channels", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// arr 用于本次流程后续判断的arr
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["name"] != "钉钉" {
		t.Fatalf("渠道列表异常: %+v", arr)
	}
}

// TestGetChannelEditorReturnsEmailRecipient 验证编辑接口回显收件邮箱且不泄露 SMTP 密码。
func TestGetChannelEditorReturnsEmailRecipient(t *testing.T) {
	// srv、store、cleanup 保存当前测试 HTTP 服务、数据库和资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整 HTTP 路由。
	handler := srv.Router()
	// sessionCookie 是当前测试用户的认证 Cookie。
	sessionCookie := loginHelper(t, handler)
	// insertErr 保存写入含敏感字段的测试邮件渠道结果。
	_, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,?,?)`, "邮件", "email", `{"to_email":"receiver@example.com","use_custom_smtp":false,"smtp_password":"secret"}`, 1, 1)
	if insertErr != nil {
		t.Fatalf("写入邮件渠道失败: %v", insertErr)
	}
	// request 是读取邮件渠道脱敏编辑配置的请求。
	request := httptest.NewRequest(http.MethodGet, "/notification-channels/1", nil)
	request.AddCookie(sessionCookie)
	// recorder 捕获编辑接口响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("编辑渠道 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"to_email":"receiver@example.com"`) || strings.Contains(recorder.Body.String(), "smtp_password") || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("编辑渠道响应异常或泄露秘密: %s", recorder.Body.String())
	}
}

// TestGetChannelEditorReturnsConfigAndSavedCredentialKeys 验证编辑接口回读可回显配置与敏感键名，且不回传任何凭据值。
func TestGetChannelEditorReturnsConfigAndSavedCredentialKeys(t *testing.T) {
	// srv、store、cleanup 保存当前测试 HTTP 服务、数据库和资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整 HTTP 路由。
	handler := srv.Router()
	// sessionCookie 是当前测试用户的认证 Cookie。
	sessionCookie := loginHelper(t, handler)
	// insertErr 保存写入含渠道级凭据的测试 qq 渠道结果。
	_, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,?,?)`, "QQ", "qq", `{"app_id":"fixture-app","app_secret":"fixture-secret","user_openid":"fixture-user","group_openid":"fixture-group"}`, 1, 1)
	if insertErr != nil {
		t.Fatalf("写入 qq 渠道失败: %v", insertErr)
	}
	// request 是读取 qq 渠道脱敏编辑配置的请求。
	request := httptest.NewRequest(http.MethodGet, "/notification-channels/1", nil)
	request.AddCookie(sessionCookie)
	// recorder 捕获编辑接口响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("编辑 qq 渠道 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// decoded 是解析后的响应，用于断言契约字段而不是匹配字符串字面量。
	var decoded struct {
		// Config 是可回显的非敏感配置键值。
		Config map[string]string `json:"config"`
		// SavedCredentialKeys 是已保存凭据的键名。
		SavedCredentialKeys []string `json:"saved_credential_keys"`
	}
	// decodeErr 保存响应解析错误。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &decoded); decodeErr != nil {
		t.Fatalf("解析编辑响应失败: %v", decodeErr)
	}
	if decoded.Config["user_openid"] != "fixture-user" || decoded.Config["group_openid"] != "fixture-group" || len(decoded.Config) != 2 {
		t.Fatalf("可回显配置异常: %+v", decoded.Config)
	}
	if len(decoded.SavedCredentialKeys) != 0 {
		t.Fatalf("qq 渠道不再声明渠道级凭据: %+v", decoded.SavedCredentialKeys)
	}
	// body 保存编辑接口返回的 JSON 文本，用于确认凭据值从未出现。
	body := recorder.Body.String()
	if strings.Contains(body, "fixture-app") || strings.Contains(body, "fixture-secret") {
		t.Fatalf("qq 编辑响应泄露渠道级凭据: %s", body)
	}
	if strings.Contains(body, "has_channel_credentials") {
		t.Fatalf("旧契约字段不应继续出现在响应中: %s", body)
	}
}

// TestCreateChannelMissingName 缺 name/type 400。
func TestCreateChannelMissingName(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"type":"dingtalk"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 name 应 400，got %d", rec.Code)
	}
}

// TestCreateChannelBadJSON 非法 JSON 400。
func TestCreateChannelBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestUpdateChannel 更新渠道。
func TestUpdateChannel(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 创建。
	body := `{"name":"钉钉","type":"dingtalk","config":"{\"webhook_url\":\"https://example.com\"}","event_types":"[\"account_offline\"]","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cr 用于本次流程后续判断的cr
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 用于本次流程后续判断的标识
	id := int64(cr["id"].(float64))

	// 部分更新：只切换 enabled，不应清空 name/type/config/event_types。
	upd := `{"enabled":false}`
	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(id), strings.NewReader(upd))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// row、err 用于本次流程后续判断的row、err
	row, err := store.Notifications.GetChannelRowForUser(context.Background(), id, 1)
	if err != nil {
		t.Fatalf("get channel row: %v", err)
	}
	if row == nil {
		t.Fatal("channel row missing")
	}
	if row.Name != "钉钉" || row.Type != "dingtalk" || row.Config != `{"webhook_url":"https://example.com"}` ||
		row.EventTypes != `["account_offline"]` || row.Enabled {
		t.Fatalf("partial update should preserve omitted fields and change enabled: %+v", row)
	}

	// 显式传空 event_types 表示恢复为接收全部事件，其它字段仍保留。
	req3 := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(id), strings.NewReader(`{"event_types":""}`))
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("clear event_types status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	row, err = store.Notifications.GetChannelRowForUser(context.Background(), id, 1)
	if err != nil {
		t.Fatalf("get channel row after clear: %v", err)
	}
	if row.EventTypes != "" || row.Name != "钉钉" || row.Config != `{"webhook_url":"https://example.com"}` {
		t.Fatalf("event_types clear should not affect other fields: %+v", row)
	}
}

// TestUpdateChannelBadID 无效 ID 400。
func TestUpdateChannelBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/notification-channels/abc", strings.NewReader(`{}`))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestUpdateChannelBadJSON 非法 JSON 400。
func TestUpdateChannelBadJSON(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPut, "/notification-channels/1", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestDeleteChannel 删除渠道。
func TestDeleteChannel(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// body 用于本次流程后续判断的请求体
	body := `{"name":"钉钉","type":"dingtalk","config":"{}","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cr 用于本次流程后续判断的cr
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 用于本次流程后续判断的标识
	id := int64(cr["id"].(float64))

	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodDelete, "/notification-channels/"+itoa(id), nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("delete status=%d", rec2.Code)
	}
}

// TestDeleteChannelBadID 无效 ID 400。
func TestDeleteChannelBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodDelete, "/notification-channels/abc", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestTestChannelNoNotifier 未注入通知器 503。
func TestTestChannelNoNotifier(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels/1/test", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无通知器应 503，got %d", rec.Code)
	}
}

// TestTestChannelBadID 无效 ID 400。
func TestTestChannelBadID(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/notification-channels/abc/test", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestNotificationChannelsRejectCrossUserMutation 验证跨用户渠道更新与删除被拒绝且不改动对方数据。
func TestNotificationChannelsRejectCrossUserMutation(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "notif2", "notif2@example.com", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// u2 用于本次流程后续判断的u2
	u2, _ := store.Users.GetByUsername(ctx, "notif2")
	// otherID、err 用于本次流程后续判断的otherID、err
	otherID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "other-secret", Type: "webhook", Config: `{}`, Enabled: true, UserID: u2.ID,
	})
	if err != nil {
		t.Fatalf("create other channel: %v", err)
	}

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// updateOtherReq 用于本次流程后续判断的updateOtherReq
	updateOtherReq := httptest.NewRequest(http.MethodPut, "/notification-channels/"+itoa(otherID), strings.NewReader(`{"enabled":false}`))
	updateOtherReq.AddCookie(cookie)
	// updateOtherRec 用于本次流程后续判断的updateOtherRec
	updateOtherRec := httptest.NewRecorder()
	h.ServeHTTP(updateOtherRec, updateOtherReq)
	if updateOtherRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user update should be 403, got %d body=%s", updateOtherRec.Code, updateOtherRec.Body.String())
	}
	// otherRow、err 用于本次流程后续判断的otherRow、err
	otherRow, err := store.Notifications.GetChannelRowForUser(ctx, otherID, u2.ID)
	if err != nil {
		t.Fatalf("get other channel: %v", err)
	}
	if otherRow == nil || !otherRow.Enabled {
		t.Fatalf("cross-user update should not mutate other channel: %+v", otherRow)
	}

	// deleteOtherReq 用于本次流程后续判断的deleteOtherReq
	deleteOtherReq := httptest.NewRequest(http.MethodDelete, "/notification-channels/"+itoa(otherID), nil)
	deleteOtherReq.AddCookie(cookie)
	// deleteOtherRec 用于本次流程后续判断的deleteOtherRec
	deleteOtherRec := httptest.NewRecorder()
	h.ServeHTTP(deleteOtherRec, deleteOtherReq)
	if deleteOtherRec.Code != http.StatusForbidden {
		t.Fatalf("cross-user delete should be 403, got %d body=%s", deleteOtherRec.Code, deleteOtherRec.Body.String())
	}
}
