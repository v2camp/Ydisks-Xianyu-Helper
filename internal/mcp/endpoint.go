// endpoint.go 装配 MCP Streamable HTTP 协议端点：
// Guard 安全中间件在外，mcp-go 协议处理器在内，二者组成单一 http.Handler 挂到 /mcp。
// 工具、资源与提示的注册通过 MCPServer() 返回的协议实例在后续装配阶段完成。

package mcp

import (
	"context"
	"net/http"
	"sync"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	// defaultServerName 是 MCP initialize 握手向 Harness 声明的服务实现名。
	defaultServerName = "ydisks-xianyu-helper"
	// defaultServerVersion 是 MCP 服务实现版本；与应用版本独立演进。
	defaultServerVersion = "1.0.0"
)

// EndpointConfig 是构造 MCP 协议端点的配置集合。
type EndpointConfig struct {
	// Config 是启用门与令牌校验端口，必填。
	Config ConfigPort
	// Identity 是固定管理员身份端口，必填。
	Identity IdentityPort
	// Audit 是调用审计端口；nil 时允许不审计但装配处必须显式知晓（生产装配禁止为 nil）。
	Audit AuditPort
	// AuditLister 是审计分页查询端口；nil 时 mcp_audit_list 工具返回未配置错误。
	AuditLister AuditListPort
	// SystemVersion 是 system_version 工具返回的应用版本字符串。
	SystemVersion string
	// EnvironmentToken 是 XIANYU_MCP_TOKEN 引导令牌；空白表示未配置。
	EnvironmentToken string
	// Now 是可注入时钟；nil 时使用墙钟。
	Now func() time.Time
	// ServerName 自定义握手服务名；空白时使用默认名。
	ServerName string
	// ServerVersion 自定义握手版本；空白时使用默认版本。
	ServerVersion string
}

// Endpoint 聚合安全守卫与 mcp-go Streamable HTTP 协议处理器，是 internal/composition 唯一持有的传输句柄。
type Endpoint struct {
	// guard 是 Bearer/loopback/启用门安全守卫。
	guard *Guard
	// config 是启用状态与网络策略端口，供 resources/read 的 health 视图读取启用状态。
	config ConfigPort
	// audit 是工具/资源/提示调用审计端口；写失败只记日志不影响业务结果。
	audit AuditPort
	// auditLister 是审计分页查询端口，供 mcp_audit_list 使用。
	auditLister AuditListPort
	// systemVersion 是 system_version 工具返回的应用版本。
	systemVersion string
	// mcpServer 是 mcp-go 协议服务器，工具/资源/提示注册到该实例。
	mcpServer *mcpserver.MCPServer
	// streamable 是实现 http.Handler 的 Streamable HTTP 传输。
	streamable *mcpserver.StreamableHTTPServer
	// registryMu 保护 toolDefs 的并发注册与读取；持锁期间不做外部 I/O。
	registryMu sync.RWMutex
	// toolDefs 是已注册工具的定义清单，供工具清单对照与禁能力面测试。
	toolDefs map[string]ToolDef
}

// NewEndpoint 校验必需依赖并构造协议端点；此时未注册任何业务工具。
func NewEndpoint(cfg EndpointConfig) (*Endpoint, error) {
	// guard、guardErr 是安全守卫及其构造错误。
	guard, guardErr := NewGuard(GuardConfig{
		Config:           cfg.Config,
		Identity:         cfg.Identity,
		EnvironmentToken: cfg.EnvironmentToken,
		Now:              cfg.Now,
	})
	if guardErr != nil {
		return nil, guardErr
	}
	// serverName 是握手声明的服务名，空白时回落默认名。
	serverName := cfg.ServerName
	if serverName == "" {
		serverName = defaultServerName
	}
	// serverVersion 是握手声明的服务版本，空白时回落默认版本。
	serverVersion := cfg.ServerVersion
	if serverVersion == "" {
		serverVersion = defaultServerVersion
	}
	// mcpInstance 是 mcp-go 协议服务器；工具结果错误由处理器自行转 isError，不使用 SDK 全局错误钩子。
	// 即使首批工具尚未注册也显式声明 tools/resources/prompts 能力，保证三类 list 方法始终可用。
	mcpInstance := mcpserver.NewMCPServer(serverName, serverVersion,
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(false, false),
		mcpserver.WithPromptCapabilities(false),
	)
	// streamable 是挂载为单一 HTTP 处理器的 Streamable HTTP 传输。
	// 启用有状态会话：2025 版协议的 Harness 回落 initialize 握手后需要 Mcp-Session-Id 校验，
	// 2026-07-28 无状态核心的请求按协议版本逐请求识别、不受该开关影响；本应用固定单实例部署，无粘滞问题。
	streamable := mcpserver.NewStreamableHTTPServer(mcpInstance, mcpserver.WithStateful(true))
	return &Endpoint{
		guard:         guard,
		config:        cfg.Config,
		audit:         cfg.Audit,
		auditLister:   cfg.AuditLister,
		systemVersion: cfg.SystemVersion,
		mcpServer:     mcpInstance,
		streamable:    streamable,
		toolDefs:      make(map[string]ToolDef),
	}, nil
}

// Guard 返回安全守卫，供管理接口在令牌/开关变更后调用 Invalidate。
func (e *Endpoint) Guard() *Guard {
	return e.guard
}

// ProtocolServer 返回 mcp-go 协议服务器，供各域注册工具、资源与提示。
func (e *Endpoint) ProtocolServer() *mcpserver.MCPServer {
	return e.mcpServer
}

// Handler 返回守卫包装后的完整 HTTP 处理器：未通过安全门的请求不会进入 JSON-RPC 处理。
func (e *Endpoint) Handler() http.Handler {
	return e.guard.Middleware(e.streamable)
}

// CloseSessions 在进程关闭阶段主动结束 MCP 流会话，加速 SSE 长连接随 HTTP 优雅关闭排空。
func (e *Endpoint) CloseSessions(ctx context.Context) error {
	if e == nil || e.streamable == nil {
		return nil
	}
	e.streamable.CloseSessions(ctx)
	return nil
}

// compile-time assertion确保端点返回值始终是标准库处理器。
var _ http.Handler = (*Endpoint)(nil)

// ServeHTTP 让 Endpoint 自身也满足 http.Handler，便于直接挂载与测试。
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.Handler().ServeHTTP(w, r)
}
