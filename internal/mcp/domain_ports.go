// domain_ports.go 汇总 internal/mcp 各域工具消费的最小应用端口。
// 这些接口由 internal/mcp 按用例需要自定义，由 internal/composition/runtime 用
// 既有应用服务投影实现；接口签名只引用 internal/application/* 的应用模型，
// 不引用 server、db 或任何平台实现包。

package mcp

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
)

// AccountPorts 聚合账号域工具所需的账号用例集合。
type AccountPorts interface {
	// 摘要与归属。
	ListAdminSummaries(ctx context.Context) ([]accountapp.AdminAccountSummary, error)
	ListSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error)
	GetOwnedSummary(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error)
	RequireOwnership(ctx context.Context, userID int64, cookieID string) error
	// 本地配置写入。
	UpdateAccountSettings(ctx context.Context, input accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error)
	SetAccountStatus(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.StatusResult, error)
	SetAccountPause(ctx context.Context, userID int64, cookieID string, minutes int) (accountapp.SettingsResult, error)
	GetAccountPause(ctx context.Context, userID int64, cookieID string) (accountapp.PauseState, error)
	SetAutoConfirm(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetAutoConsign(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetAutoBargain(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetRemark(ctx context.Context, userID int64, cookieID, remark string) (accountapp.SettingsResult, error)
	// 平台触达与运行时。
	QueryLongLogin(ctx context.Context, userID int64, cookieID string) (accountapp.LongLoginResult, error)
	SetLongLogin(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.LongLoginResult, error)
	RuntimeStatuses(ctx context.Context) (map[string]accountapp.RuntimeStatus, error)
	RestartAccount(ctx context.Context, cookieID string) error
	RefreshProfile(ctx context.Context, userID int64, cookieID string) (accountapp.ProfileResult, error)
	DeleteAccount(ctx context.Context, userID int64, cookieID string) error
	// 自动评价/擦亮任务。
	GetAccountTaskSettings(ctx context.Context, cookieID string) (automationapp.AccountTaskSettings, error)
	UpdateAccountTaskSettings(ctx context.Context, settings automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error)
	ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]automationapp.AccountTaskRun, error)
	RunAccountTask(ctx context.Context, cookieID, taskType string) (automationapp.TaskSummary, error)
}

// DomainPorts 是全部域工具端口的聚合；某域为 nil 时该域工具不注册。
type DomainPorts struct {
	// Account 是账号域端口。
	Account AccountPorts
}
