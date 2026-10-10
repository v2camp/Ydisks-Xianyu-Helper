package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOpenAPIAgentSupportResponses 验证客服 Agent 配置接口的成功、未认证、非法输入与越权响应
// 均满足 OpenAPI 契约，并断言账号级读取会如实标注覆盖来源、异常输入不会落库。
func TestOpenAPIAgentSupportResponses(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、测试数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含客服 Agent 配置路由的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie。
	sessionCookie := loginHelper(t, handler)

	// 未认证读取默认配置必须返回统一 401 envelope。
	unauthenticated := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/settings", nil)
	// unauthenticatedRecorder 捕获未认证请求的响应。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticated)
	if unauthenticatedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("未认证读取客服 Agent 默认配置应 401，got %d body=%s", unauthenticatedRecorder.Code, unauthenticatedRecorder.Body.String())
	}
	assertOpenAPIResponse(t, unauthenticated, unauthenticatedRecorder)

	// 初始状态必须是关闭且只读，并同时下发三个平台级档位。
	settingsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/settings", nil)
	settingsRequest.AddCookie(sessionCookie)
	// settingsRecorder 捕获默认配置读取响应。
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsRequest)
	assertOpenAPISuccessResponse(t, settingsRequest, settingsRecorder)
	// initial 是初始默认配置响应。
	var initial agentSupportSettingsResponse
	// err 是默认配置响应 JSON 解析错误。
	if err := json.Unmarshal(settingsRecorder.Body.Bytes(), &initial); err != nil {
		t.Fatalf("解析默认配置响应失败: %v", err)
	}
	if initial.Enabled || initial.Preset != "readonly" {
		t.Fatalf("初始默认配置应为关闭且只读，实际 %+v", initial)
	}
	if len(initial.Presets) != 3 {
		t.Fatalf("默认配置应同时下发 3 个档位，实际 %+v", initial.Presets)
	}

	// 保存租户默认：开启并设为标准档。
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agent/support/settings", strings.NewReader(`{"enabled":true,"preset":"standard"}`))
	updateRequest.AddCookie(sessionCookie)
	// updateRecorder 捕获默认配置保存响应。
	updateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateRecorder, updateRequest)
	assertOpenAPISuccessResponse(t, updateRequest, updateRecorder)

	// 非法档位必须 400，且不能把坏值留在库里。
	invalidRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agent/support/settings", strings.NewReader(`{"enabled":true,"preset":"root"}`))
	invalidRequest.AddCookie(sessionCookie)
	// invalidRecorder 捕获非法档位请求的响应。
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法档位应 400，got %d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
	assertOpenAPIResponse(t, invalidRequest, invalidRecorder)
	// afterInvalidRequest 是非法写入之后重新读取默认配置的请求。
	afterInvalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/settings", nil)
	afterInvalidRequest.AddCookie(sessionCookie)
	// afterInvalidRecorder 捕获非法写入后的默认配置响应。
	afterInvalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(afterInvalidRecorder, afterInvalidRequest)
	assertOpenAPISuccessResponse(t, afterInvalidRequest, afterInvalidRecorder)
	// afterInvalid 是非法写入后的默认配置响应。
	var afterInvalid agentSupportSettingsResponse
	// err 是非法写入后默认配置响应 JSON 解析错误。
	if err := json.Unmarshal(afterInvalidRecorder.Body.Bytes(), &afterInvalid); err != nil {
		t.Fatalf("解析默认配置响应失败: %v", err)
	}
	if !afterInvalid.Enabled || afterInvalid.Preset != "standard" {
		t.Fatalf("非法写入不得改动已保存的默认配置，实际 %+v", afterInvalid)
	}

	// 需要在共享测试模板之外准备一个归属当前管理员的账号，用于账号级配置读写。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil {
		t.Fatalf("读取管理员失败: %v", adminErr)
	}
	// cookieID 是账号级配置用例使用的账号标识。
	cookieID := "agent-support-account"
	// saveErr 是账号写入结果。
	if saveErr := store.Cookies.Save(context.Background(), cookieID, "unb=agent-support;", admin.ID); saveErr != nil {
		t.Fatalf("写入测试账号失败: %v", saveErr)
	}

	// 首次读取账号配置：应完全继承租户默认，覆盖来源为空。
	accountRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/accounts/"+cookieID, nil)
	accountRequest.AddCookie(sessionCookie)
	// accountRecorder 捕获账号配置读取响应。
	accountRecorder := httptest.NewRecorder()
	handler.ServeHTTP(accountRecorder, accountRequest)
	assertOpenAPISuccessResponse(t, accountRequest, accountRecorder)
	// inherited 是继承状态下的账号配置响应。
	var inherited agentSupportAccountResponse
	// err 是账号配置响应 JSON 解析错误。
	if err := json.Unmarshal(accountRecorder.Body.Bytes(), &inherited); err != nil {
		t.Fatalf("解析账号配置响应失败: %v", err)
	}
	if inherited.CookieID != cookieID || !inherited.Enabled || inherited.Preset != "standard" {
		t.Fatalf("账号应继承租户默认的开启与标准档，实际 %+v", inherited)
	}
	if inherited.AccountEnabled != nil || inherited.AccountPreset != nil {
		t.Fatalf("继承状态下不得标注账号级覆盖，实际 %+v", inherited)
	}

	// 保存账号级覆盖：关闭开关并压回只读档。
	overrideRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agent/support/accounts/"+cookieID, strings.NewReader(`{"enabled":false,"preset":"readonly"}`))
	overrideRequest.AddCookie(sessionCookie)
	// overrideRecorder 捕获账号级覆盖保存响应。
	overrideRecorder := httptest.NewRecorder()
	handler.ServeHTTP(overrideRecorder, overrideRequest)
	assertOpenAPISuccessResponse(t, overrideRequest, overrideRecorder)

	// 覆盖后读取必须返回账号级生效值并标注来源。
	overriddenRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/accounts/"+cookieID, nil)
	overriddenRequest.AddCookie(sessionCookie)
	// overriddenRecorder 捕获覆盖后的账号配置响应。
	overriddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(overriddenRecorder, overriddenRequest)
	assertOpenAPISuccessResponse(t, overriddenRequest, overriddenRecorder)
	// overridden 是覆盖后的账号配置响应。
	var overridden agentSupportAccountResponse
	// err 是覆盖后账号配置响应 JSON 解析错误。
	if err := json.Unmarshal(overriddenRecorder.Body.Bytes(), &overridden); err != nil {
		t.Fatalf("解析账号配置响应失败: %v", err)
	}
	if overridden.Enabled || overridden.Preset != "readonly" {
		t.Fatalf("账号级覆盖应生效为关闭与只读，实际 %+v", overridden)
	}
	if overridden.AccountEnabled == nil || overridden.AccountPreset == nil {
		t.Fatalf("账号级覆盖必须标注来源，实际 %+v", overridden)
	}

	// 恢复继承：两个维度都传 null。
	inheritRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agent/support/accounts/"+cookieID, strings.NewReader(`{"enabled":null,"preset":null}`))
	inheritRequest.AddCookie(sessionCookie)
	// inheritRecorder 捕获恢复继承响应。
	inheritRecorder := httptest.NewRecorder()
	handler.ServeHTTP(inheritRecorder, inheritRequest)
	assertOpenAPISuccessResponse(t, inheritRequest, inheritRecorder)

	// 恢复继承后必须回到租户默认。
	restoredRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/accounts/"+cookieID, nil)
	restoredRequest.AddCookie(sessionCookie)
	// restoredRecorder 捕获恢复继承后的账号配置响应。
	restoredRecorder := httptest.NewRecorder()
	handler.ServeHTTP(restoredRecorder, restoredRequest)
	assertOpenAPISuccessResponse(t, restoredRequest, restoredRecorder)
	// restored 是恢复继承后的账号配置响应。
	var restored agentSupportAccountResponse
	// err 是恢复继承后账号配置响应 JSON 解析错误。
	if err := json.Unmarshal(restoredRecorder.Body.Bytes(), &restored); err != nil {
		t.Fatalf("解析账号配置响应失败: %v", err)
	}
	if !restored.Enabled || restored.Preset != "standard" || restored.AccountEnabled != nil || restored.AccountPreset != nil {
		t.Fatalf("恢复继承后应回到租户默认且无覆盖标注，实际 %+v", restored)
	}

	// 不存在的账号读取必须 404，不区分「不存在」与「不属于你」。
	missingRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/support/accounts/missing-account", nil)
	missingRequest.AddCookie(sessionCookie)
	// missingRecorder 捕获不存在账号的读取响应。
	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("读取不存在账号应 404，got %d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
	assertOpenAPIResponse(t, missingRequest, missingRecorder)

	// 不存在的账号写入同样必须 404，且不得落库。
	missingUpdateRequest := httptest.NewRequest(http.MethodPut, "/api/v1/agent/support/accounts/missing-account", strings.NewReader(`{"enabled":true,"preset":"advanced"}`))
	missingUpdateRequest.AddCookie(sessionCookie)
	// missingUpdateRecorder 捕获不存在账号的写入响应。
	missingUpdateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingUpdateRecorder, missingUpdateRequest)
	if missingUpdateRecorder.Code != http.StatusNotFound {
		t.Fatalf("写入不存在账号应 404，got %d body=%s", missingUpdateRecorder.Code, missingUpdateRecorder.Body.String())
	}
	assertOpenAPIResponse(t, missingUpdateRequest, missingUpdateRecorder)
}
