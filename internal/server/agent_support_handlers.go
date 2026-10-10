package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	agentadminapp "xianyu-go/internal/application/agentadmin"
	"xianyu-go/internal/auth"
)

// agentPresetResponse 是单个档位的展示元数据响应 DTO。
type agentPresetResponse struct {
	// Value 是档位标识，写入配置时必须原样回传。
	Value string `json:"value"`
	// Label 是界面显示名。
	Label string `json:"label"`
	// Description 是一句话说明该档位放开到哪一步。
	Description string `json:"description"`
}

// agentSupportSettingsResponse 是租户级客服 Agent 默认配置响应 DTO。
type agentSupportSettingsResponse struct {
	// Enabled 表示该租户的账号默认是否运行客服 Agent。
	Enabled bool `json:"enabled"`
	// Preset 是账号未覆盖时使用的档位。
	Preset string `json:"preset"`
	// Presets 是平台级档位定义，随配置下发使界面无需硬编码档位名单。
	Presets []agentPresetResponse `json:"presets"`
}

// agentSupportSettingsUpdateRequest 是租户级默认配置的更新请求 DTO。
type agentSupportSettingsUpdateRequest struct {
	// Enabled 是更新后的默认开关。
	Enabled bool `json:"enabled"`
	// Preset 是更新后的默认档位，必须是平台定义的档位标识。
	Preset string `json:"preset"`
}

// agentSupportAccountResponse 是账号级生效配置响应 DTO，并把覆盖来源一并告知界面。
type agentSupportAccountResponse struct {
	// CookieID 是配置归属账号。
	CookieID string `json:"cookie_id"`
	// Enabled 是该账号最终生效的开关。
	Enabled bool `json:"enabled"`
	// Preset 是该账号最终生效的档位。
	Preset string `json:"preset"`
	// AccountEnabled 非空表示开关来自账号级覆盖，null 表示继承租户默认。
	AccountEnabled *bool `json:"account_enabled"`
	// AccountPreset 非空表示档位来自账号级覆盖，null 表示继承租户默认。
	AccountPreset *string `json:"account_preset"`
	// Presets 是平台级档位定义。
	Presets []agentPresetResponse `json:"presets"`
}

// agentSupportAccountUpdateRequest 是账号级覆盖的更新请求 DTO。
// 字段为 null 表示该维度恢复继承租户默认，两个字段都为 null 即完全跟随租户。
type agentSupportAccountUpdateRequest struct {
	// Enabled 为 null 时该账号的开关恢复继承租户默认。
	Enabled *bool `json:"enabled"`
	// Preset 为 null 时该账号的档位恢复继承租户默认。
	Preset *string `json:"preset"`
}

// mountVersionedAgentSupportRoutes 挂载客服 Agent 配置的 `/api/v1` 入口。
//
// 归任意已登录会话：租户级默认与账号级覆盖都只作用于调用方自己的数据，越权由应用用例
// 的归属校验拒绝，不依赖路由层的管理员门。
func (s *Server) mountVersionedAgentSupportRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/agent/support/settings", s.agentSupportSettings)
		r.Put("/api/v1/agent/support/settings", s.updateAgentSupportSettings)
		r.Get("/api/v1/agent/support/accounts/{cookie_id}", s.agentSupportAccount)
		r.Put("/api/v1/agent/support/accounts/{cookie_id}", s.updateAgentSupportAccount)
	})
}

// agentSupportSettings 返回租户级客服 Agent 默认配置与平台级档位定义。
func (s *Server) agentSupportSettings(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// config、err 是租户级默认配置及其读取错误。
	config, err := s.agentSupportApplication().TenantSettings(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取客服 Agent 默认配置失败")
		return
	}
	writeJSON(w, http.StatusOK, agentSupportSettingsResponse{
		Enabled: config.Enabled, Preset: string(config.Preset), Presets: agentPresetResponses(),
	})
}

// updateAgentSupportSettings 保存租户级客服 Agent 默认配置。
func (s *Server) updateAgentSupportSettings(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// payload 是本次更新请求 DTO。
	var payload agentSupportSettingsUpdateRequest
	// err 是请求体 JSON 解析错误。
	if err := decodeJSON(r, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	// updateErr 是租户级默认配置保存错误。
	updateErr := s.agentSupportApplication().UpdateTenantSettings(r.Context(), sess.UserID, agentadminapp.TenantConfig{
		Enabled: payload.Enabled, Preset: agentadminapp.PresetValue(payload.Preset),
	})
	if updateErr != nil {
		writeAgentSupportError(w, updateErr, "保存客服 Agent 默认配置失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// agentSupportAccount 返回指定账号最终生效的客服 Agent 配置与覆盖来源。
func (s *Server) agentSupportAccount(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// cookieID 是路径中的账号标识。
	cookieID := chi.URLParam(r, "cookie_id")
	// config、err 是该账号的生效配置及其读取错误。
	config, err := s.agentSupportApplication().AccountSettings(r.Context(), sess.UserID, cookieID)
	if err != nil {
		writeAgentSupportError(w, err, "读取客服 Agent 账号配置失败")
		return
	}
	writeJSON(w, http.StatusOK, agentSupportAccountResponseFromConfig(config))
}

// updateAgentSupportAccount 保存账号级客服 Agent 覆盖。
func (s *Server) updateAgentSupportAccount(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// cookieID 是路径中的账号标识。
	cookieID := chi.URLParam(r, "cookie_id")
	// payload 是本次更新请求 DTO；字段为 null 表示该维度恢复继承。
	var payload agentSupportAccountUpdateRequest
	// err 是请求体 JSON 解析错误。
	if err := decodeJSON(r, &payload); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	// updateErr 是账号级覆盖保存错误。
	updateErr := s.agentSupportApplication().UpdateAccountOverride(r.Context(), sess.UserID, cookieID, agentadminapp.AccountOverride{
		Enabled: payload.Enabled, Preset: payload.Preset,
	})
	if updateErr != nil {
		writeAgentSupportError(w, updateErr, "保存客服 Agent 账号配置失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// agentSupportAccountResponseFromConfig 把应用层生效配置转换为 HTTP 响应 DTO。
func agentSupportAccountResponseFromConfig(config agentadminapp.EffectiveConfig) agentSupportAccountResponse {
	// accountPreset 是账号级覆盖档位的响应形态；未覆盖时保持 null。
	var accountPreset *string
	if config.AccountPreset != nil {
		// presetValue 是覆盖档位的文本形式。
		presetValue := string(*config.AccountPreset)
		accountPreset = &presetValue
	}
	return agentSupportAccountResponse{
		CookieID: config.CookieID, Enabled: config.Enabled, Preset: string(config.Preset),
		AccountEnabled: config.AccountEnabled, AccountPreset: accountPreset,
		Presets: agentPresetResponses(),
	}
}

// agentPresetResponses 把平台级档位定义转换为 HTTP 响应 DTO。
func agentPresetResponses() []agentPresetResponse {
	// options 是平台级档位展示定义。
	options := agentadminapp.PresetOptions()
	// out 是转换后的响应列表。
	out := make([]agentPresetResponse, 0, len(options))
	// option 是当前遍历到的档位定义。
	for _, option := range options {
		out = append(out, agentPresetResponse{Value: string(option.Value), Label: option.Label, Description: option.Description})
	}
	return out
}

// writeAgentSupportError 把客服 Agent 配置的应用错误映射为统一 HTTP 响应。
//
// 归属失败统一按「账号不存在」返回：区分「不存在」与「不属于你」会让接口变成账号枚举器。
// 档位非法属于调用方输入问题，因此映射为 400 而不是 500。
func writeAgentSupportError(w http.ResponseWriter, err error, fallbackMessage string) {
	switch {
	case errors.Is(err, agentadminapp.ErrAccountNotOwned):
		writeErr(w, http.StatusNotFound, "账号不存在")
	case errors.Is(err, agentadminapp.ErrInvalidPreset):
		writeErr(w, http.StatusBadRequest, "档位取值非法")
	default:
		writeErr(w, http.StatusInternalServerError, fallbackMessage)
	}
}
