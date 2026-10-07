package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	mcpadminapp "xianyu-go/internal/application/mcpadmin"
	"xianyu-go/internal/auth"
)

// mcpStatusResponse 是 MCP 服务状态接口的具名响应 DTO，只含非敏感配置态。
type mcpStatusResponse struct {
	// Enabled 表示 MCP 服务是否已启用。
	Enabled bool `json:"enabled"`
	// AllowNonLoopback 表示是否显式放行非本机来源访问 /mcp。
	AllowNonLoopback bool `json:"allow_non_loopback"`
	// HasToken 表示当前是否存在持久化令牌（含宽限期内旧令牌）。
	HasToken bool `json:"has_token"`
	// TokenCreatedAt 是当前令牌创建时间（Unix 秒），无令牌时为 0。
	TokenCreatedAt int64 `json:"token_created_at"`
	// TokenLastUsedAt 是当前令牌最后一次鉴权成功时间（Unix 秒），从未使用为 0。
	TokenLastUsedAt int64 `json:"token_last_used_at"`
	// HasPreviousToken 表示是否存在宽限期内的旧令牌。
	HasPreviousToken bool `json:"has_previous_token"`
	// PreviousTokenExpiresAt 是宽限旧令牌的失效时刻（Unix 秒），无旧令牌时为 0。
	PreviousTokenExpiresAt int64 `json:"previous_token_expires_at"`
	// Endpoint 是 Harness 应使用的接入地址（形如 http://host/mcp）。
	Endpoint string `json:"endpoint"`
}

// mcpTokenResponse 是令牌生成/轮换接口的具名响应 DTO。
// Token 是仅在本次响应出现的明文，后续任何接口都不再返回。
type mcpTokenResponse struct {
	// Token 是本次生成或轮换得到的令牌明文。
	Token string `json:"token"`
}

// mcpSettingsUpdateRequest 是 MCP 启用开关与网络策略的更新请求 DTO。
type mcpSettingsUpdateRequest struct {
	// Enabled 是更新后的启用状态。
	Enabled bool `json:"enabled"`
	// AllowNonLoopback 是更新后的非本机访问策略。
	AllowNonLoopback bool `json:"allow_non_loopback"`
}

// mcpAuditEntryResponse 是单条调用审计的具名响应 DTO；参数已是白名单脱敏摘要。
type mcpAuditEntryResponse struct {
	// ID 是审计行主键。
	ID int64 `json:"id"`
	// CreatedAt 是调用发生时间（Unix 秒）。
	CreatedAt int64 `json:"created_at"`
	// UserID 是执行调用的固定管理员本地用户标识。
	UserID int64 `json:"user_id"`
	// TokenSource 是本次调用的令牌来源（persisted/environment）。
	TokenSource string `json:"token_source"`
	// Category 是调用类别：tool、resource 或 prompt。
	Category string `json:"category"`
	// Name 是被调用的工具、资源或提示名称。
	Name string `json:"name"`
	// CookieID 是调用目标账号标识；无账号归属时为空串。
	CookieID string `json:"cookie_id"`
	// Arguments 是键级脱敏后的参数 JSON 摘要。
	Arguments string `json:"arguments"`
	// Success 表示调用是否成功完成。
	Success bool `json:"success"`
	// ErrorClass 是失败类别的稳定标识；成功时为空串。
	ErrorClass string `json:"error_class"`
	// DurationMS 是调用耗时（毫秒）。
	DurationMS int64 `json:"duration_ms"`
}

// mcpAuditResponse 是调用审计分页接口的具名响应 DTO。
type mcpAuditResponse struct {
	// Success 表示查询是否完成。
	Success bool `json:"success"`
	// Data 是当前页审计记录。
	Data []mcpAuditEntryResponse `json:"data"`
	// Total 是满足筛选条件的总记录数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// TotalPages 是总页数。
	TotalPages int `json:"total_pages"`
}

// mountVersionedMCPAdminRoutes 挂载 MCP 管理接口的 `/api/v1` 管理员入口。
// 全部操作都需要管理员会话；生成接口的一次性明文只在该响应出现。
func (s *Server) mountVersionedMCPAdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Use(auth.RequireAdmin)
		r.Get("/api/v1/mcp/status", s.mcpStatus)
		r.Post("/api/v1/mcp/token", s.mcpGenerateToken)
		r.Delete("/api/v1/mcp/token", s.mcpRevokeToken)
		r.Put("/api/v1/mcp/settings", s.mcpUpdateSettings)
		r.Get("/api/v1/mcp/audit", s.mcpAuditList)
	})
}

// mcpStatus 返回 MCP 服务当前的非敏感管理状态与接入地址。
func (s *Server) mcpStatus(w http.ResponseWriter, r *http.Request) {
	// status、err 是 MCP 管理状态及其读取错误。
	status, err := s.mcpAdminApplication().Status(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询 MCP 服务状态失败")
		return
	}
	writeJSON(w, http.StatusOK, mcpStatusResponse{
		Enabled:                status.Enabled,
		AllowNonLoopback:       status.AllowNonLoopback,
		HasToken:               status.Token.HasCurrent || status.Token.HasPrevious,
		TokenCreatedAt:         status.Token.CurrentCreatedAt,
		TokenLastUsedAt:        status.Token.CurrentLastUsedAt,
		HasPreviousToken:       status.Token.HasPrevious,
		PreviousTokenExpiresAt: status.Token.PreviousExpiresAt,
		Endpoint:               mcpEndpointAddress(r),
	})
}

// mcpGenerateToken 生成或轮换持久化令牌，并返回只在本次响应出现一次的明文。
func (s *Server) mcpGenerateToken(w http.ResponseWriter, r *http.Request) {
	// token、err 是本次生成的一次性明文及其错误。
	token, err := s.mcpAdminApplication().RotateToken(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "生成 MCP 令牌失败")
		return
	}
	writeJSON(w, http.StatusOK, mcpTokenResponse{Token: token})
}

// mcpRevokeToken 立即吊销全部持久化令牌。
func (s *Server) mcpRevokeToken(w http.ResponseWriter, r *http.Request) {
	// err 是吊销持久化令牌的错误。
	if err := s.mcpAdminApplication().RevokeToken(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "吊销 MCP 令牌失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// mcpUpdateSettings 保存 MCP 启用开关与网络策略。
func (s *Server) mcpUpdateSettings(w http.ResponseWriter, r *http.Request) {
	// payload 是本次更新请求 DTO。
	var payload mcpSettingsUpdateRequest
	// err 是请求体 JSON 解析错误。
	if err := decodeJSON(r, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	// updateErr 是保存启用开关与网络策略的错误。
	if updateErr := s.mcpAdminApplication().UpdateSettings(r.Context(), mcpadminapp.SettingsUpdate{
		Enabled: payload.Enabled, AllowNonLoopback: payload.AllowNonLoopback,
	}); updateErr != nil {
		writeErr(w, http.StatusInternalServerError, "保存 MCP 设置失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// mcpAuditList 分页查询 MCP 调用审计。
func (s *Server) mcpAuditList(w http.ResponseWriter, r *http.Request) {
	// query 是本次请求的查询参数集合。
	query := r.URL.Query()
	// page、pageSize、offset 是归一化后的分页参数。
	page, pageSize, offset := mcpadminapp.PageBounds(atoiDefault(query.Get("page"), 1), atoiDefault(query.Get("page_size"), 20))
	// filter 是组装后的审计过滤条件。
	filter := mcpadminapp.AuditFilter{
		Limit: pageSize, Offset: offset,
		Category: query.Get("category"), Name: query.Get("name"), CookieID: query.Get("account_id"),
	}
	// 仅在显式提供 success 参数时按成败过滤，取值 true/false。
	if raw := query.Get("success"); raw != "" {
		if raw == "true" {
			filter.SuccessState = 1
		} else {
			filter.SuccessState = -1
		}
	}
	// audit、err 是分页审计结果及其查询错误。
	audit, err := s.mcpAdminApplication().ListAudit(r.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询 MCP 调用审计失败")
		return
	}
	// records 是当前页审计响应列表。
	records := make([]mcpAuditEntryResponse, 0, len(audit.Records))
	// row 是当前审计记录。
	for _, row := range audit.Records {
		records = append(records, mcpAuditEntryResponse{
			ID: row.ID, CreatedAt: row.CreatedAt, UserID: row.UserID,
			TokenSource: row.TokenSource, Category: row.Category, Name: row.Name,
			CookieID: row.CookieID, Arguments: row.Arguments, Success: row.Success,
			ErrorClass: row.ErrorClass, DurationMS: row.DurationMS,
		})
	}
	writeJSON(w, http.StatusOK, mcpAuditResponse{
		Success: true, Data: records, Total: audit.Total,
		Page: page, PageSize: pageSize, TotalPages: mcpadminapp.TotalPages(audit.Total, pageSize),
	})
}

// mcpEndpointAddress 依据请求推导 Harness 应使用的 MCP 接入地址。
func mcpEndpointAddress(r *http.Request) string {
	// scheme 是请求使用的协议；本服务默认明文监听，仅在显式 TLS 时使用 https。
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/mcp"
}
