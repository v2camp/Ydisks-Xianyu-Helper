// tools_system.go 注册服务自检类工具：ping、版本、调用身份与调用审计自查。

package mcp

import "context"

// pingResult 是 system_ping 工具的返回 DTO。
type pingResult struct {
	// OK 恒为 true，能拿到该结果说明协议链路与鉴权均正常。
	OK bool `json:"ok"`
	// Service 是服务标识名。
	Service string `json:"service"`
}

// versionResult 是 system_version 工具的返回 DTO。
type versionResult struct {
	// Service 是服务标识名。
	Service string `json:"service"`
	// Version 是应用版本字符串。
	Version string `json:"version"`
}

// whoamiResult 是 auth_whoami 工具的返回 DTO，不含令牌明文与任何凭证。
type whoamiResult struct {
	// UserID 是固定管理员本地用户标识。
	UserID int64 `json:"user_id"`
	// TokenSource 是本次接入使用的令牌来源。
	TokenSource TokenSource `json:"token_source"`
	// Role 恒为 admin，表明全部工具以管理员身份执行。
	Role string `json:"role"`
}

// RegisterSystemTools 注册自检与审计自查工具；不依赖任何业务域端口。
func (e *Endpoint) RegisterSystemTools() {
	e.RegisterTools(
		ToolDef{
			Name:        "system_ping",
			Description: "检查 MCP 服务与鉴权是否正常；无副作用，返回固定 pong 结构。",
			Args:        nil,
			Handler: func(_ context.Context, _ *CallIdentity, _ Arguments) (any, error) {
				return pingResult{OK: true, Service: defaultServerName}, nil
			},
		},
		ToolDef{
			Name:        "system_version",
			Description: "返回当前应用与 MCP 服务版本，用于判断 Harness 接入的服务能力基线。",
			Args:        nil,
			Handler: func(_ context.Context, _ *CallIdentity, _ Arguments) (any, error) {
				return versionResult{Service: defaultServerName, Version: e.systemVersion}, nil
			},
		},
		ToolDef{
			Name:        "auth_whoami",
			Description: "返回本次 MCP 调用使用的管理员身份与令牌来源，用于核对执行权限；不返回令牌本身。",
			Args:        nil,
			Handler: func(_ context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				return whoamiResult{UserID: identity.UserID, TokenSource: identity.Source, Role: "admin"}, nil
			},
		},
		ToolDef{
			Name: "mcp_audit_list",
			Description: "分页查询本 MCP 服务最近的工具调用审计（仅非敏感字段，参数已经过白名单脱敏）。" +
				"可按 category（tool/resource/prompt）、name、account_id、success 状态过滤。",
			Args: []ArgSpec{
				{Name: "page", Type: ArgInteger, Description: "页码，从 1 开始，默认 1。"},
				{Name: "page_size", Type: ArgInteger, Description: "每页条数，默认 20，最大 200。"},
				{Name: "category", Type: ArgString, Description: "按调用类别过滤：tool、resource 或 prompt。",
					Enum: []string{"tool", "resource", "prompt"}},
				{Name: "name", Type: ArgString, Description: "按工具/资源/提示名称精确过滤。"},
				{Name: "account_id", Type: ArgString, Description: "按目标账号标识过滤。"},
				{Name: "success", Type: ArgBoolean, Description: "按调用成败过滤；不传表示全部。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				if e.auditLister == nil {
					return nil, Fail(ClassInternal, "审计查询未启用", nil)
				}
				// filter 是组装后的审计过滤条件。
				filter := AuditFilter{
					Category: args.OptionalString("category", ""),
					Name:     args.OptionalString("name", ""),
					CookieID: args.OptionalString("account_id", ""),
				}
				// limit、offset 是归一化后的分页参数。
				filter.Limit, filter.Offset = args.Page()
				// successPresent 表示是否显式提供了成败过滤。
				if value, ok := args["success"]; ok {
					// flag 是成败过滤布尔值。
					if flag, ok := value.(bool); ok {
						filter.SuccessState = -1
						if flag {
							filter.SuccessState = 1
						}
					}
				}
				_ = identity
				return e.auditLister.ListAudits(ctx, filter)
			},
		},
	)
}
