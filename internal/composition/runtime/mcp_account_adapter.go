package runtime

// mcp_account_adapter.go 把账号域应用服务集合投影为 internal/mcp 的 AccountPorts。
// 适配器只做方法名与参数的透传，业务规则全部保留在应用服务内。

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/mcp"
)

// mcpAccountPorts 聚合账号域 MCP 工具需要的应用服务。
type mcpAccountPorts struct {
	// summaries 是账号摘要与归属用例服务。
	summaries *accountapp.SummaryService
	// settings 是账号本地设置用例服务。
	settings *accountapp.SettingsService
	// longLogin 是长登录平台用例服务。
	longLogin *accountapp.LongLoginService
	// runtimeStatus 是账号运行时用例服务。
	runtimeStatus *accountapp.RuntimeService
	// profile 是账号资料刷新用例服务。
	profile *accountapp.ProfileService
	// delete 是账号删除用例服务。
	delete *accountapp.DeleteService
	// tasks 是自动评价/擦亮任务用例服务。
	tasks *automationapp.Service
}

// 编译期断言适配器始终满足 mcp.AccountPorts。
var _ mcp.AccountPorts = (*mcpAccountPorts)(nil)

// newMCPAccountPorts 用 TransportPorts 中的账号应用服务构造 MCP 账号端口。
func newMCPAccountPorts(ports composition.TransportPorts) *mcpAccountPorts {
	return &mcpAccountPorts{
		summaries:     ports.AccountSummaries,
		settings:      ports.AccountSettings,
		longLogin:     ports.AccountLongLogin,
		runtimeStatus: ports.AccountRuntime,
		profile:       ports.AccountProfile,
		delete:        ports.AccountDelete,
		tasks:         ports.AccountTasks,
	}
}

// ListAdminSummaries 透传管理员全局账号列表用例。
func (a *mcpAccountPorts) ListAdminSummaries(ctx context.Context) ([]accountapp.AdminAccountSummary, error) {
	return a.summaries.ListAdminSummaries(ctx)
}

// ListSummaries 透传归属账号列表用例。
func (a *mcpAccountPorts) ListSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error) {
	return a.summaries.ListSummaries(ctx, userID)
}

// GetOwnedSummary 透传归属账号详情用例。
func (a *mcpAccountPorts) GetOwnedSummary(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error) {
	return a.summaries.GetOwnedSummary(ctx, userID, cookieID)
}

// RequireOwnership 透传账号归属校验用例。
func (a *mcpAccountPorts) RequireOwnership(ctx context.Context, userID int64, cookieID string) error {
	return a.summaries.RequireOwnership(ctx, userID, cookieID)
}

// UpdateAccountSettings 透传账号综合设置写入用例。
func (a *mcpAccountPorts) UpdateAccountSettings(ctx context.Context, input accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error) {
	return a.settings.UpdateSettings(ctx, input)
}

// SetAccountStatus 透传账号启停用例。
func (a *mcpAccountPorts) SetAccountStatus(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.StatusResult, error) {
	return a.settings.SetStatus(ctx, userID, cookieID, enabled)
}

// SetAccountPause 透传暂停时长写入用例，返回含新暂停截止时间的设置结果。
func (a *mcpAccountPorts) SetAccountPause(ctx context.Context, userID int64, cookieID string, minutes int) (accountapp.SettingsResult, error) {
	return a.settings.SetPause(ctx, userID, cookieID, minutes)
}

// GetAccountPause 透传暂停状态读取用例。
func (a *mcpAccountPorts) GetAccountPause(ctx context.Context, userID int64, cookieID string) (accountapp.PauseState, error) {
	return a.settings.GetPause(ctx, userID, cookieID)
}

// SetAutoConfirm 透传自动确认收货开关用例。
func (a *mcpAccountPorts) SetAutoConfirm(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error) {
	return a.settings.SetAutoConfirm(ctx, userID, cookieID, enabled)
}

// SetAutoConsign 透传自动转已发货开关用例。
func (a *mcpAccountPorts) SetAutoConsign(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error) {
	return a.settings.SetAutoConsign(ctx, userID, cookieID, enabled)
}

// SetAutoBargain 透传砍价自动免拼开关用例。
func (a *mcpAccountPorts) SetAutoBargain(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error) {
	return a.settings.SetAutoBargain(ctx, userID, cookieID, enabled)
}

// SetRemark 透传账号备注写入用例。
func (a *mcpAccountPorts) SetRemark(ctx context.Context, userID int64, cookieID, remark string) (accountapp.SettingsResult, error) {
	return a.settings.SetRemark(ctx, userID, cookieID, remark)
}

// QueryLongLogin 透传长登录状态查询用例。
func (a *mcpAccountPorts) QueryLongLogin(ctx context.Context, userID int64, cookieID string) (accountapp.LongLoginResult, error) {
	return a.longLogin.Query(ctx, userID, cookieID)
}

// SetLongLogin 透传长登录开关平台用例。
func (a *mcpAccountPorts) SetLongLogin(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.LongLoginResult, error) {
	return a.longLogin.Set(ctx, userID, cookieID, enabled)
}

// RuntimeStatuses 透传全部账号运行时状态用例。
func (a *mcpAccountPorts) RuntimeStatuses(ctx context.Context) (map[string]accountapp.RuntimeStatus, error) {
	return a.runtimeStatus.RuntimeStatuses(ctx)
}

// RestartAccount 透传账号运行实例重启用例。
func (a *mcpAccountPorts) RestartAccount(ctx context.Context, cookieID string) error {
	return a.runtimeStatus.Restart(ctx, cookieID)
}

// RefreshProfile 透传账号资料刷新用例。
func (a *mcpAccountPorts) RefreshProfile(ctx context.Context, userID int64, cookieID string) (accountapp.ProfileResult, error) {
	return a.profile.RefreshProfile(ctx, userID, cookieID)
}

// DeleteAccount 透传账号删除用例。
func (a *mcpAccountPorts) DeleteAccount(ctx context.Context, userID int64, cookieID string) error {
	return a.delete.Delete(ctx, userID, cookieID)
}

// GetAccountTaskSettings 透传自动评价/擦亮配置读取用例。
func (a *mcpAccountPorts) GetAccountTaskSettings(ctx context.Context, cookieID string) (automationapp.AccountTaskSettings, error) {
	return a.tasks.GetSettings(ctx, cookieID)
}

// UpdateAccountTaskSettings 透传自动评价/擦亮配置写入用例。
func (a *mcpAccountPorts) UpdateAccountTaskSettings(ctx context.Context, settings automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error) {
	return a.tasks.UpdateSettings(ctx, settings)
}

// ListAccountTaskRuns 透传账号任务运行记录查询用例。
func (a *mcpAccountPorts) ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]automationapp.AccountTaskRun, error) {
	return a.tasks.ListRuns(ctx, cookieID, limit)
}

// RunAccountTask 透传立即执行账号任务用例。
func (a *mcpAccountPorts) RunAccountTask(ctx context.Context, cookieID, taskType string) (automationapp.TaskSummary, error) {
	return a.tasks.Run(ctx, cookieID, taskType)
}
