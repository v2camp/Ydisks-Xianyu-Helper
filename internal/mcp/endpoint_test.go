// endpoint_test.go 用 mcp-go streamable 客户端验证 /mcp 协议链路：
// initialize → ping → tools/list 全流程必须穿过安全守卫；无令牌请求在 JSON-RPC 之前被 401 拒绝。

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpproto "github.com/mark3labs/mcp-go/mcp"
)

// fakeAudit 是记录审计写入的假端口。
type fakeAudit struct {
	// entries 保存全部写入的审计条目。
	entries []AuditEntry
}

// AddAudit 记录审计条目且不返回错误。
func (f *fakeAudit) AddAudit(_ context.Context, entry AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

// newProtocolEndpoint 构造协议测试端点：启用、环境令牌与持久化令牌固定、管理员身份为 1。
func newProtocolEndpoint(t *testing.T) (*Endpoint, *fakeAudit) {
	t.Helper()
	// audit 是假审计端口。
	audit := &fakeAudit{}
	// endpoint、err 是协议端点及其构造错误。
	endpoint, err := NewEndpoint(EndpointConfig{
		Config:           &fakeConfig{enabled: true, hasToken: true, validToken: "persisted-or-env-secret"},
		Identity:         fakeIdentity{userID: 1},
		Audit:            audit,
		EnvironmentToken: "persisted-or-env-secret",
		Now:              func() time.Time { return time.Unix(3000, 0) },
	})
	if err != nil {
		t.Fatalf("构造协议端点失败: %v", err)
	}
	return endpoint, audit
}

// TestStreamableProtocolInitializeAndList 验证真实 streamable 客户端可完成握手、ping 与空工具列表。
func TestStreamableProtocolInitializeAndList(t *testing.T) {
	// endpoint、_ 是被测协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// server 是承载守卫与协议处理器的本机 HTTP 测试服务。
	server := httptest.NewServer(endpoint.Handler())
	defer server.Close()
	// ctx 是本用例的握手上下文。
	ctx := context.Background()
	// trans、err 是携带 Bearer 头的 streamable HTTP 传输及其构造错误。
	trans, err := transport.NewStreamableHTTP(server.URL,
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer persisted-or-env-secret"}))
	if err != nil {
		t.Fatalf("构造 streamable 传输失败: %v", err)
	}
	// mcpc 是基于该传输的 MCP 客户端。
	mcpc := client.NewClient(trans)
	// startErr 是传输建连错误。
	if startErr := mcpc.Start(ctx); startErr != nil {
		t.Fatalf("启动客户端失败: %v", startErr)
	}
	defer func() { _ = mcpc.Close() }()
	// initRequest 是声明客户端信息的初始化请求。
	initRequest := mcpproto.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcpproto.Implementation{Name: "harness-test", Version: "1.0.0"}
	// initResult、initErr 是握手结果及其错误。
	initResult, initErr := mcpc.Initialize(ctx, initRequest)
	if initErr != nil {
		t.Fatalf("initialize 失败: %v", initErr)
	}
	if initResult.ServerInfo.Name != defaultServerName {
		t.Fatalf("服务名异常: %q", initResult.ServerInfo.Name)
	}
	// pingErr 是协议 ping 的错误。
	if pingErr := mcpc.Ping(ctx); pingErr != nil {
		t.Fatalf("ping 失败: %v", pingErr)
	}
	// tools、toolsErr 是工具列表结果；Task 3 阶段尚未注册业务工具，列表必须为空。
	tools, toolsErr := mcpc.ListTools(ctx, mcpproto.ListToolsRequest{})
	if toolsErr != nil {
		t.Fatalf("tools/list 失败: %v", toolsErr)
	}
	if len(tools.Tools) != 0 {
		t.Fatalf("Task 3 阶段工具列表应为空，实际 %d 个", len(tools.Tools))
	}
}

// TestStreamableRejectsUnauthenticatedHTTP 验证无 Bearer 头的请求在进入 JSON-RPC 前直接 401。
func TestStreamableRejectsUnauthenticatedHTTP(t *testing.T) {
	// endpoint、_ 是被测协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// rec 是无认证 POST 的响应记录器；请求体给一个 JSON-RPC 外壳，证明它不会被解析。
	rec := httptest.NewRecorder()
	// req 是无 Bearer 头的 initialize 请求，安全门必须在解析前拒绝。
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.RemoteAddr = "127.0.0.1:51000"
	endpoint.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 Bearer 请求必须 401，实际 %d", rec.Code)
	}
	// 关闭开关并失效快照后，携带正确令牌的请求也必须 404 隐藏端点。
	endpoint.Guard().Invalidate()
	// cfg、ok 是守卫内部假配置及其类型断言结果。
	if cfg, ok := endpoint.guard.config.(*fakeConfig); ok {
		cfg.enabled = false
	}
	// disabledRec 是关闭状态下的响应记录器。
	disabledRec := httptest.NewRecorder()
	// disabledReq 是关闭状态下携带正确令牌的请求。
	disabledReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	disabledReq.RemoteAddr = "127.0.0.1:51001"
	disabledReq.Header.Set("Authorization", "Bearer persisted-or-env-secret")
	endpoint.Handler().ServeHTTP(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusNotFound {
		t.Fatalf("关闭状态必须 404 隐藏端点，实际 %d", disabledRec.Code)
	}
}
