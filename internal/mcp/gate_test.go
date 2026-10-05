// gate_test.go 覆盖 Bearer 解析、来源限流、loopback 判定、动态启用门与错误分类的确定性分支。

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpproto "github.com/mark3labs/mcp-go/mcp"

	settingsapp "xianyu-go/internal/application/settings"
)

// fakeConfig 是可控的 MCP 配置端口假实现。
type fakeConfig struct {
	// enabled 是端点启用状态返回值。
	enabled bool
	// allowNonLoopback 是非本机放行返回值。
	allowNonLoopback bool
	// hasToken 是持久化令牌存在性返回值。
	hasToken bool
	// verifyResult 是持久化令牌校验结果。
	verifyResult bool
	// verifyCalls 记录持久化校验调用次数。
	verifyCalls int
}

// Enabled 返回预置启用状态。
func (f *fakeConfig) Enabled(context.Context) (bool, error) { return f.enabled, nil }

// AllowNonLoopback 返回预置网络策略。
func (f *fakeConfig) AllowNonLoopback(context.Context) (bool, error) { return f.allowNonLoopback, nil }

// HasPersistedToken 返回预置持久化令牌存在性。
func (f *fakeConfig) HasPersistedToken(context.Context) (bool, error) { return f.hasToken, nil }

// VerifyPersistedToken 记录调用并返回预置校验结果。
func (f *fakeConfig) VerifyPersistedToken(_ context.Context, _ string) (bool, error) {
	f.verifyCalls++
	return f.verifyResult, nil
}

// 编译期断言 fakeConfig 始终满足 ConfigPort。
var _ ConfigPort = (*fakeConfig)(nil)

// fakeIdentity 是固定管理员身份端口假实现。
type fakeIdentity struct {
	// userID 是解析出的管理员标识；0 模拟身份缺失。
	userID int64
}

// AdminUserID 返回预置管理员标识。
func (f fakeIdentity) AdminUserID(context.Context) (int64, error) { return f.userID, nil }

// guardHarness 聚合守卫测试所需的假依赖、被测守卫、下游到达标记与捕获身份。
type guardHarness struct {
	// cfg 是假配置端口。
	cfg *fakeConfig
	// guard 是被测安全守卫。
	guard *Guard
	// reached 标记最近一次请求是否穿过守卫进入下游。
	reached bool
	// identity 是下游最近一次捕获的调用身份；未穿过时为 nil。
	identity *CallIdentity
	// serve 是守卫包装后的统一请求入口，下游恒定写入 200 并捕获身份。
	serve http.HandlerFunc
}

// newGuardHarness 用可控时钟构造测试守卫；envToken 是环境引导令牌。
// 返回指针以保证下游闭包写入的 reached/identity 与调用方持有的是同一夹具。
func newGuardHarness(t *testing.T, envToken string, now func() time.Time) *guardHarness {
	t.Helper()
	// h 是逐步填充的堆上夹具，下游闭包与调用方共享同一实例。
	h := &guardHarness{}
	// cfg 是默认开启、默认拒绝非本机、存在持久化令牌但令牌校验默认失败的假配置；
	// 需要放行的用例显式把 verifyResult 置真，避免错误令牌被假实现意外接受。
	h.cfg = &fakeConfig{enabled: true, hasToken: true, verifyResult: false}
	// guard、err 是被测守卫及其构造错误。
	guard, err := NewGuard(GuardConfig{
		Config:           h.cfg,
		Identity:         fakeIdentity{userID: 7},
		EnvironmentToken: envToken,
		Now:              now,
		StateCacheTTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("构造守卫失败: %v", err)
	}
	h.guard = guard
	// h.serve 是守卫下游的占位协议处理器：记录到达、捕获身份并返回 200。
	h.serve = guard.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.reached = true
		h.identity = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP
	return h
}

// newRequest 构造携带 RemoteAddr 与认证头的 MCP 请求。
func newRequest(remoteAddr, authorization string) *http.Request {
	// req 是发给守卫中间件的 POST 请求；Body 与协议内容无关，给空体即可。
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = remoteAddr
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return req
}

// TestBearerTokenParsing 覆盖 Bearer 头解析的合法与非法形态。
func TestBearerTokenParsing(t *testing.T) {
	// cases 是解析用例：输入头与期望令牌。
	cases := []struct {
		name string
		hdr  string
		want string
	}{
		{"标准", "Bearer abc123", "abc123"},
		{"小写方案", "bearer abc123", "abc123"},
		{"前后空白", "  Bearer   abc123  ", "abc123"},
		{"无头", "", ""},
		{"错误方案", "Basic abc123", ""},
		{"缺令牌", "Bearer", ""},
		{"多段", "Bearer abc def", ""},
	}
	// tc 是当前解析用例。
	for _, tc := range cases {
		// got 是解析出的令牌。
		got := bearerToken(newRequest("127.0.0.1:1000", tc.hdr))
		if got != tc.want {
			t.Fatalf("用例 %s 解析结果 %q，期望 %q", tc.name, got, tc.want)
		}
	}
}

// TestLoopbackPolicy 验证本机/非本机来源判定且 X-Forwarded-For 不参与判定。
func TestLoopbackPolicy(t *testing.T) {
	// cases 是来源判定用例：RemoteAddr、是否本机、伪造 XFF。
	cases := []struct {
		remote string
		loop   bool
		xff    string
	}{
		{"127.0.0.1:52000", true, ""},
		{"[::1]:52000", true, ""},
		{"192.168.1.5:52000", false, "127.0.0.1"},
		{"10.0.0.2:52000", false, "127.0.0.1"},
	}
	// tc 是当前来源用例。
	for _, tc := range cases {
		// req 是当前来源请求，附带伪造转发头。
		req := newRequest(tc.remote, "")
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		// got 是 loopback 判定结果。
		got := isLoopbackRequest(req)
		if got != tc.loop {
			t.Fatalf("来源 %s 判定 %v，期望 %v", tc.remote, got, tc.loop)
		}
	}
}

// TestGuardHiddenWhenDisabledOrNoToken 验证关闭状态与无令牌时端点返回 404。
func TestGuardHiddenWhenDisabledOrNoToken(t *testing.T) {
	// now 是固定时钟函数。
	now := func() time.Time { return time.Unix(1000, 0) }
	// cases 是两类隐藏场景的配置调整函数。
	cases := []func(*guardHarness){
		func(h *guardHarness) { h.cfg.enabled = false },
		func(h *guardHarness) { h.cfg.hasToken = false },
	}
	// setup 是当前隐藏场景的配置调整。
	for _, setup := range cases {
		// h 是该子用例的守卫夹具。
		h := newGuardHarness(t, "", now)
		setup(h)
		h.guard.Invalidate()
		// rec 是响应记录器。
		rec := httptest.NewRecorder()
		h.serve(rec, newRequest("127.0.0.1:1000", "Bearer some-token"))
		if rec.Code != http.StatusNotFound || h.reached {
			t.Fatalf("关闭或无令牌时必须 404 且不透传，实际 %d reached=%v", rec.Code, h.reached)
		}
	}
}

// TestGuardAuthAndRateLimit 验证错误令牌 401、超阈值 429、窗口过后正确令牌恢复。
func TestGuardAuthAndRateLimit(t *testing.T) {
	// current 是可手动推进的当前时刻。
	current := time.Unix(1000, 0)
	// h 是持久化令牌校验预置通过的守卫夹具。
	h := newGuardHarness(t, "", func() time.Time { return current })
	// first 是第一次错误令牌的响应，必须 401。
	first := httptest.NewRecorder()
	h.serve(first, newRequest("127.0.0.1:1000", "Bearer wrong-token"))
	if first.Code != http.StatusUnauthorized || h.reached {
		t.Fatalf("错误令牌应 401 且不透传，实际 %d", first.Code)
	}
	// 连续失败打满阈值；i 是当前失败序号。
	for i := 0; i < authFailuresPerSource; i++ {
		// rec 是当前失败响应记录器。
		rec := httptest.NewRecorder()
		h.serve(rec, newRequest("127.0.0.1:1000", "Bearer bad"))
	}
	// blocked 是封禁窗口内携带正确令牌的请求，必须 429。
	blocked := httptest.NewRecorder()
	h.serve(blocked, newRequest("127.0.0.1:1000", "Bearer correct"))
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("超阈值来源应被 429 封禁，实际 %d", blocked.Code)
	}
	// 推进时钟越过失败窗口，使假配置对正确令牌放行，并使配置快照失效。
	current = current.Add(authFailureWindow + time.Second)
	h.cfg.verifyResult = true
	h.guard.Invalidate()
	// recovered 是窗口过期后的请求，正确令牌应放行并注入身份。
	recovered := httptest.NewRecorder()
	h.serve(recovered, newRequest("127.0.0.1:1000", "Bearer correct"))
	if recovered.Code != http.StatusOK || !h.reached || h.identity == nil {
		t.Fatalf("窗口过后正确令牌应放行，实际 %d reached=%v", recovered.Code, h.reached)
	}
	if h.identity.Source != SourcePersisted || h.identity.UserID != 7 {
		t.Fatalf("持久化令牌调用身份异常: %+v", h.identity)
	}
}

// TestGuardEnvironmentTokenAndNetworkPolicy 验证环境令牌校验与非本机放行开关。
func TestGuardEnvironmentTokenAndNetworkPolicy(t *testing.T) {
	// now 是固定时钟函数。
	now := func() time.Time { return time.Unix(2000, 0) }
	// h 是配置了环境引导令牌的守卫夹具。
	h := newGuardHarness(t, "env-secret", now)
	// denied 是局域网来源默认策略下的响应，即使令牌正确也必须 403。
	denied := httptest.NewRecorder()
	h.serve(denied, newRequest("192.168.1.9:1000", "Bearer env-secret"))
	if denied.Code != http.StatusForbidden || h.reached {
		t.Fatalf("非本机来源默认应 403，实际 %d", denied.Code)
	}
	// 打开非本机放行并使快照失效后，同来源携带环境令牌应放行。
	h.cfg.allowNonLoopback = true
	h.guard.Invalidate()
	// allowed 是放行后的响应。
	allowed := httptest.NewRecorder()
	h.serve(allowed, newRequest("192.168.1.9:1000", "Bearer env-secret"))
	if allowed.Code != http.StatusOK || !h.reached {
		t.Fatalf("放行开关打开后非本机正确令牌应可达下游，实际 %d", allowed.Code)
	}
	if h.identity == nil || h.identity.Source != SourceEnvironment || h.identity.UserID != 7 {
		t.Fatalf("环境令牌调用身份异常: %+v", h.identity)
	}
}

// TestRequireConfirm 覆盖破坏性工具确认参数的真假与类型边界。
func TestRequireConfirm(t *testing.T) {
	// cases 是确认参数用例。
	cases := []struct {
		args map[string]any
		want bool
	}{
		{nil, false},
		{map[string]any{}, false},
		{map[string]any{"confirm": false}, false},
		{map[string]any{"confirm": "true"}, false},
		{map[string]any{"confirm": true}, true},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		// got 是确认判定结果。
		got := RequireConfirm(tc.args)
		if got != tc.want {
			t.Fatalf("参数 %v 判定 %v，期望 %v", tc.args, got, tc.want)
		}
	}
}

// TestClassifyMapsKnownErrors 验证已知哨兵与不确定错误的中文类别映射不泄漏内部文本。
func TestClassifyMapsKnownErrors(t *testing.T) {
	// cases 是分类用例：输入错误与期望类别。
	cases := []struct {
		err   error
		class ErrorClass
	}{
		{settingsapp.ErrForbidden, ClassForbidden},
		{settingsapp.ErrAccountNotFound, ClassNotFound},
		{settingsapp.ErrInvalidUser, ClassUnauthorized},
		{errors.New("外部动作结果不确定，需要人工"), ClassUncertain},
		{errors.New("auth_expired 平台登录态失效"), ClassSessionExpired},
		{errors.New("SQL ERROR syntax near x"), ClassInternal},
	}
	// tc 是当前分类用例。
	for _, tc := range cases {
		// class、msg 是归一结果。
		class, msg := Classify(tc.err)
		if class != tc.class {
			t.Fatalf("错误 %v 分类 %q，期望 %q", tc.err, class, tc.class)
		}
		if strings.TrimSpace(msg) == "" {
			t.Fatalf("类别 %q 必须给出中文摘要", class)
		}
	}
	// classified 是显式入参错误，类别必须原样保留。
	classified := InvalidArgument("缺少 confirm 确认")
	// class、_ 是其归一类别。
	class, _ := Classify(classified)
	if class != ClassInvalidArgument {
		t.Fatalf("显式分类错误类别异常: %q", class)
	}
}

// TestResultBuilders 验证 JSON 成功结果与中文错误结果的协议结构。
func TestResultBuilders(t *testing.T) {
	// ok、err 是结构化成功结果及其构造错误。
	ok, err := JSONResult(map[string]any{"ping": "pong"})
	if err != nil {
		t.Fatalf("构造 JSON 结果失败: %v", err)
	}
	if ok.IsError || len(ok.Content) != 1 {
		t.Fatalf("成功结果结构异常: %+v", ok)
	}
	// failResult、class 是错误结果及其类别。
	failResult, class := ErrorResult(InvalidArgument("参数错误"))
	if !failResult.IsError || class != ClassInvalidArgument {
		t.Fatalf("错误结果结构异常: %+v class=%q", failResult, class)
	}
	// text 是纯文本结果。
	text := TextResult("ok")
	if text.IsError {
		t.Fatal("纯文本结果不应标记错误")
	}
	// 编译期保证结果类型与 mcp-go 协议类型一致。
	var _ *mcpproto.CallToolResult = text
}
