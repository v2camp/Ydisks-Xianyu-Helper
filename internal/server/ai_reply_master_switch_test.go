// ai_reply_master_switch_test.go 端到端锁定「缺省不提交开关」与「总闸决定生效值」。
//
// 这条链路跨 HTTP DTO → 应用服务 → 适配器 → 数据库四层，
// 三态语义（未提交 = 不改开关）必须在最外层就体现，否则只改砍价护栏就会把账号写成显式关闭。
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// putAIReply 发送一次账号 AI 设置保存请求并返回响应记录器。
// 参数 h 是已装配的测试路由；cookie 是登录态；body 是请求 JSON 原文。
func putAIReply(t *testing.T, h http.Handler, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	// req 是本用例构造的保存请求。
	req := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 捕获本次保存响应。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// getAIReplyRaw 读取一次账号 AI 设置并返回解码后的 JSON 对象。
// 参数 h 是已装配的测试路由；cookie 是登录态。
func getAIReplyRaw(t *testing.T, h http.Handler, cookie *http.Cookie) map[string]any {
	t.Helper()
	// req 是读取请求。
	req := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/acc1", nil)
	req.AddCookie(cookie)
	// rec 捕获读取响应。
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("读取 AI 设置 status=%d body=%s", rec.Code, rec.Body.String())
	}
	// payload 是解码后的响应对象。
	var payload map[string]any
	// err 是响应体反序列化失败原因。
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解码 AI 设置响应: %v", err)
	}
	return payload
}

// TestAIReplyUpdateKeepsSwitchWhenOmitted 未提交 ai_enabled 时不得改写已保存的显式开关。
func TestAIReplyUpdateKeepsSwitchWhenOmitted(t *testing.T) {
	// srv、store、cleanup 是测试服务、数据库聚合与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 是测试路由；cookie 是登录态。
	h := srv.Router()
	// cookie 是本用例使用的登录会话。
	cookie := loginHelper(t, h)
	// ctx 是本用例使用的上下文。
	ctx := context.Background()

	// 首次保存：账号显式启用，砍价轮次 3。
	first := putAIReply(t, h, cookie, `{"ai_enabled":true,"auto_adjust_price_enabled":false,"max_discount_percent":10,"max_discount_amount":20,"max_bargain_rounds":3}`)
	if first.Code != http.StatusOK {
		t.Fatalf("首次保存 status=%d body=%s", first.Code, first.Body.String())
	}
	// 第二次保存：请求体完全不带 ai_enabled，只把砍价轮次改成 5。
	second := putAIReply(t, h, cookie, `{"auto_adjust_price_enabled":false,"max_discount_percent":10,"max_discount_amount":20,"max_bargain_rounds":5}`)
	if second.Code != http.StatusOK {
		t.Fatalf("第二次保存 status=%d body=%s", second.Code, second.Body.String())
	}
	// storedEnabled 是数据库中的 ai_enabled 实际整数；必须仍为 1。
	var storedEnabled int
	// err 是读取 ai_enabled 整数值的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT ai_enabled FROM ai_reply_settings WHERE cookie_id='acc1'`).Scan(&storedEnabled); err != nil {
		t.Fatalf("读取 ai_enabled: %v", err)
	}
	if storedEnabled != 1 {
		t.Fatalf("缺省提交不得关闭账号: ai_enabled=%d", storedEnabled)
	}
	// payload 是保存后的读取结果：开关保留、护栏更新、来源仍是账号显式值。
	payload := getAIReplyRaw(t, h, cookie)
	if payload["ai_enabled"] != true || payload["max_bargain_rounds"] != float64(5) {
		t.Fatalf("缺省提交后读取结果不符: %+v", payload)
	}
	if payload["source"] != aiReplySourceAccount {
		t.Fatalf("已有显式行时来源应为 account: %+v", payload)
	}
}

// TestAIReplyEffectiveFollowsMasterSwitch 总闸关闭时接口必须报告生效值为 false，但保留账号显式值。
func TestAIReplyEffectiveFollowsMasterSwitch(t *testing.T) {
	// srv、store、cleanup 是测试服务、数据库聚合与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 是测试路由；cookie 是登录态。
	h := srv.Router()
	// cookie 是本用例使用的登录会话。
	cookie := loginHelper(t, h)
	// ctx 是本用例使用的上下文。
	ctx := context.Background()

	// 从未保存过设置的账号：默认跟随总闸，来源为 global。
	before := getAIReplyRaw(t, h, cookie)
	if before["ai_enabled"] != true || before["source"] != aiReplySourceGlobal || before["effective_enabled"] != true {
		t.Fatalf("未保存过设置时应跟随总闸: %+v", before)
	}
	// 关闭总闸后，同一个账号的生效值必须变为 false，而账号级值不变。
	// err 是写入总闸配置的错误。
	if err := store.Settings.Set(ctx, "ai_master_enabled", "false"); err != nil {
		t.Fatalf("关闭总闸: %v", err)
	}
	// after 是总闸关闭后的读取结果。
	after := getAIReplyRaw(t, h, cookie)
	if after["effective_enabled"] != false || after["ai_enabled"] != true {
		t.Fatalf("总闸关闭后生效值应为 false: %+v", after)
	}
	// 总闸关闭时仍要允许开启自动改价：AI 不会运行，账号级前提按跟随总闸解析为启用。
	enable := putAIReply(t, h, cookie, `{"auto_adjust_price_enabled":true,"max_discount_percent":10,"max_discount_amount":20,"max_bargain_rounds":3}`)
	if enable.Code != http.StatusOK {
		t.Fatalf("总闸关闭时开启自动改价应允许: status=%d body=%s", enable.Code, enable.Body.String())
	}
}
