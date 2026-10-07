// mcp_transport_policy_test.go 验证 MCP 传输层门禁的反向自检：
// 注入违规依赖或协议标记必须失败，合规源码必须通过，测试文件不参与生产门禁。

package main

import (
	"go/parser"
	"go/token"
	"testing"
)

// TestMCPTransportDependencyBoundary 逐一验证 MCP 传输包对实现层与装配根依赖 fail-closed。
func TestMCPTransportDependencyBoundary(t *testing.T) {
	// forbidden 是必须被拒绝的实现层与装配根导入，含子包。
	for _, forbidden /* forbidden 是当前待验证的禁止导入路径。 */ := range []string{
		"internal/server", "internal/server/router",
		"internal/db", "internal/db/migrations",
		"internal/xianyu", "internal/xianyu/ws",
		"internal/browser",
		"internal/automation",
		"internal/engine",
		"internal/adapter",
		"internal/composition", "internal/composition/runtime",
	} {
		if !isForbiddenMCPTransportImport("internal/mcp/endpoint.go", forbidden) {
			t.Fatalf("MCP 传输包导入 %s 应被拒绝", forbidden)
		}
	}
	// allowed 是 MCP 传输包允许的标准库、协议库与应用模型依赖。
	for _, allowed /* allowed 是当前待验证的允许导入路径。 */ := range []string{
		"context", "net/http", "github.com/mark3labs/mcp-go/mcp",
		"internal/application/orders", "internal/application/settings",
	} {
		if isForbiddenMCPTransportImport("internal/mcp/endpoint.go", allowed) {
			t.Fatalf("MCP 传输包导入 %s 不应被拒绝", allowed)
		}
	}
	if isForbiddenMCPTransportImport("internal/server/server.go", "internal/db") {
		t.Fatal("非 MCP 包不应套用 MCP 依赖规则")
	}
	if isForbiddenMCPTransportImport("internal/mcp/endpoint_test.go", "internal/db") {
		t.Fatal("测试文件不应被 MCP 依赖规则阻断")
	}
}

// TestServerMCPProtocolIsolationBoundary 验证 Server 引入 MCP 协议实现或 JSON-RPC 语义时必须失败。
func TestServerMCPProtocolIsolationBoundary(t *testing.T) {
	// offendingSource 是同时导入 mcp-go 且出现 jsonrpc 标记的合成 Server 文件。
	offendingSource := []byte(`package server
import "github.com/mark3labs/mcp-go/server"

// dispatch 处理 jsonrpc 请求。
func dispatch() {}
`)
	// fset 是合成文件的统一位置集合。
	fset := token.NewFileSet()
	// offending、parseErr 分别是合成违规文件与其解析错误。
	offending, parseErr := parser.ParseFile(fset, "server.go", offendingSource, parser.ParseComments)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// violations 是违规文件的协议越界结果：1 条导入 + 2 条文本标记。
	violations := checkServerMCPProtocolIsolation("internal/server/server.go", offending, offendingSource, fset)
	if len(violations) != 3 {
		t.Fatalf("Server 协议越界应命中 3 条: %+v", violations)
	}
	// cleanSource 是不含任何协议标记的合规 Server 文件。
	cleanSource := []byte("package server\n\n// mount 注册通用额外路由。\nfunc mount() {}\n")
	// clean、cleanErr 分别是合规合成文件与其解析错误。
	clean, cleanErr := parser.ParseFile(token.NewFileSet(), "server.go", cleanSource, parser.ParseComments)
	if cleanErr != nil {
		t.Fatal(cleanErr)
	}
	// got 是合规 Server 文件的协议门禁结果。
	if got := checkServerMCPProtocolIsolation("internal/server/server.go", clean, cleanSource, token.NewFileSet()); len(got) != 0 {
		t.Fatalf("合规 Server 文件不应被协议门禁阻断: %+v", got)
	}
	// got 是测试文件在协议门禁下的结果。
	if got := checkServerMCPProtocolIsolation("internal/server/server_test.go", offending, offendingSource, token.NewFileSet()); len(got) != 0 {
		t.Fatalf("测试文件不应被协议门禁阻断: %+v", got)
	}
}
