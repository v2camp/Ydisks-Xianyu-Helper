package runtime

// mcp_settings_adapter.go 把设置应用服务投影为 internal/mcp 的 SettingsPorts。
// 适配器只做透传：敏感设置的脱敏、审计与三态命令校验全部保留在 settings 应用服务，
// 组合层不缓存也不记录任何密钥类内容。

import (
	"context"

	settingsapp "xianyu-go/internal/application/settings"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/mcp"
)

// mcpSettingsPorts 把设置应用服务投影为 MCP 设置端口。
type mcpSettingsPorts struct {
	// service 是设置应用服务。
	service *settingsapp.Service
}

// 编译期断言适配器满足 MCP 设置端口。
var _ mcp.SettingsPorts = (*mcpSettingsPorts)(nil)

// IsSensitiveSettingKey 透传敏感键判定。
func (a *mcpSettingsPorts) IsSensitiveSettingKey(key string) bool {
	return a.service.IsSensitiveSettingKey(key)
}

// PublicSystem 透传公开系统设置读取。
func (a *mcpSettingsPorts) PublicSystem(ctx context.Context) (map[string]string, error) {
	return a.service.PublicSystem(ctx)
}

// GetSystem 透传脱敏系统设置读取（含敏感键读取审计）。
func (a *mcpSettingsPorts) GetSystem(ctx context.Context, userID int64) (map[string]string, error) {
	return a.service.GetSystem(ctx, userID)
}

// ApplySystemChanges 透传系统设置原子写入。
func (a *mcpSettingsPorts) ApplySystemChanges(ctx context.Context, userID int64, values map[string]string, secrets map[string]settingsapp.SecretChange) error {
	return a.service.ApplySystemChanges(ctx, userID, values, secrets)
}

// SetSystem 透传单项系统设置写入。
func (a *mcpSettingsPorts) SetSystem(ctx context.Context, userID int64, key, value, action string) error {
	return a.service.SetSystem(ctx, userID, key, value, action)
}

// ListUser 透传用户偏好设置列表读取。
func (a *mcpSettingsPorts) ListUser(ctx context.Context, userID int64) (map[string]string, error) {
	return a.service.ListUser(ctx, userID)
}

// GetUser 透传单项用户偏好设置读取。
func (a *mcpSettingsPorts) GetUser(ctx context.Context, userID int64, key string) (string, error) {
	return a.service.GetUser(ctx, userID, key)
}

// SetUser 透传单项用户偏好设置写入。
func (a *mcpSettingsPorts) SetUser(ctx context.Context, userID int64, key, value string) error {
	return a.service.SetUser(ctx, userID, key, value)
}

// ListAIReply 透传账号 AI 设置摘要列表读取。
func (a *mcpSettingsPorts) ListAIReply(ctx context.Context, userID int64) ([]settingsapp.AIReplySettings, error) {
	return a.service.ListAIReply(ctx, userID)
}

// GetAIReply 透传账号 AI 设置摘要读取。
func (a *mcpSettingsPorts) GetAIReply(ctx context.Context, userID int64, cookieID string) (settingsapp.AIReplySettings, error) {
	return a.service.GetAIReply(ctx, userID, cookieID)
}

// UpsertAIReply 透传账号 AI 设置保存。
func (a *mcpSettingsPorts) UpsertAIReply(ctx context.Context, userID int64, cookieID string, settings settingsapp.AIReplySettings) error {
	return a.service.UpsertAIReply(ctx, userID, cookieID, settings)
}

// ListAIModels 透传远端模型目录读取；密钥只在本次调用内使用。
func (a *mcpSettingsPorts) ListAIModels(ctx context.Context, userID int64, baseURL, apiKey string) ([]string, error) {
	return a.service.ListAIModels(ctx, userID, baseURL, apiKey)
}

// TestAIConnection 透传 AI 连通性测试；诊断结果不含密钥。
func (a *mcpSettingsPorts) TestAIConnection(ctx context.Context, userID int64, baseURL, apiKey, model string) (settingsapp.AIConnectionTestResult, error) {
	return a.service.TestAIConnection(ctx, userID, baseURL, apiKey, model)
}

// newMCPSettingsPorts 构造 MCP 设置端口；设置服务缺失时返回 nil。
func newMCPSettingsPorts(ports composition.TransportPorts) *mcpSettingsPorts {
	if ports.Settings == nil {
		return nil
	}
	return &mcpSettingsPorts{service: ports.Settings}
}
