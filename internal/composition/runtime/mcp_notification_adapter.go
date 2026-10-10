package runtime

// mcp_notification_adapter.go 把通知渠道与不确定通知应用服务投影为 internal/mcp 的消费者端口。
// 适配器只做透传：渠道配置 JSON（SMTP 密码、机器人 secret）只经写入参数进入应用层，
// 组合层不缓存、不记录、不回传任何密钥类内容。

import (
	"context"
	"time"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
)

// mcpNotificationChannelPorts 把通知渠道应用服务投影为 MCP 通知端口。
type mcpNotificationChannelPorts struct {
	// service 是通知渠道与绑定应用服务。
	service *notificationsapp.ChannelService
}

// 编译期断言适配器满足 MCP 通知渠道端口。
var _ capability.NotificationChannelPorts = (*mcpNotificationChannelPorts)(nil)

// ListChannels 透传渠道摘要列表用例。
func (a *mcpNotificationChannelPorts) ListChannels(ctx context.Context, userID int64) ([]notificationsapp.ChannelSummary, error) {
	return a.service.ListChannels(ctx, userID)
}

// GetChannelEditor 透传渠道编辑态用例；返回值不含任何密钥。
func (a *mcpNotificationChannelPorts) GetChannelEditor(ctx context.Context, userID, channelID int64) (notificationsapp.ChannelEditor, error) {
	return a.service.GetChannelEditor(ctx, userID, channelID)
}

// CreateChannel 透传渠道创建用例；配置 JSON 只写入。
func (a *mcpNotificationChannelPorts) CreateChannel(ctx context.Context, userID int64, input notificationsapp.ChannelInput) (int64, error) {
	return a.service.CreateChannel(ctx, userID, input)
}

// UpdateChannel 透传渠道部分更新用例；配置 JSON 只写入。
func (a *mcpNotificationChannelPorts) UpdateChannel(ctx context.Context, userID, channelID int64, patch notificationsapp.ChannelPatch) error {
	return a.service.UpdateChannel(ctx, userID, channelID, patch)
}

// DeleteChannel 透传渠道删除用例。
func (a *mcpNotificationChannelPorts) DeleteChannel(ctx context.Context, userID, channelID int64) error {
	return a.service.DeleteChannel(ctx, userID, channelID)
}

// TestChannel 透传测试发送用例；发送时刻取当前墙钟。
func (a *mcpNotificationChannelPorts) TestChannel(ctx context.Context, userID, channelID int64) error {
	return a.service.TestChannel(ctx, userID, channelID, time.Now())
}

// ListBindings 透传绑定摘要列表用例。
func (a *mcpNotificationChannelPorts) ListBindings(ctx context.Context, userID int64) ([]notificationsapp.BindingSummary, error) {
	return a.service.ListBindings(ctx, userID)
}

// GetBindingIDs 透传账号启用渠道读取用例。
func (a *mcpNotificationChannelPorts) GetBindingIDs(ctx context.Context, userID int64, cookieID string) ([]int64, error) {
	return a.service.GetBindingIDs(ctx, userID, cookieID)
}

// SetBindings 透传账号绑定整组覆盖用例。
func (a *mcpNotificationChannelPorts) SetBindings(ctx context.Context, userID int64, cookieID string, channelIDs []int64) error {
	return a.service.SetBindings(ctx, userID, cookieID, channelIDs)
}

// SetSingleBinding 透传单个绑定切换用例。
func (a *mcpNotificationChannelPorts) SetSingleBinding(ctx context.Context, userID int64, cookieID string, channelID int64, enabled bool) error {
	return a.service.SetSingleBinding(ctx, userID, cookieID, channelID, enabled)
}

// DeleteBinding 透传绑定删除用例。
func (a *mcpNotificationChannelPorts) DeleteBinding(ctx context.Context, userID, bindingID int64) error {
	return a.service.DeleteBinding(ctx, userID, bindingID)
}

// DeleteAccountBindings 透传账号绑定清空用例。
func (a *mcpNotificationChannelPorts) DeleteAccountBindings(ctx context.Context, userID int64, cookieID string) error {
	return a.service.DeleteAccountBindings(ctx, userID, cookieID)
}

// mcpUncertainNotificationPorts 把不确定通知查询服务投影为 MCP 端口。
type mcpUncertainNotificationPorts struct {
	// service 是不确定通知查询应用服务。
	service *notificationsapp.Service
}

// 编译期断言适配器满足 MCP 不确定通知端口。
var _ capability.UncertainNotificationPorts = (*mcpUncertainNotificationPorts)(nil)

// ListUncertainForUser 透传当前用户的不确定通知查询。
func (a *mcpUncertainNotificationPorts) ListUncertainForUser(ctx context.Context, userID int64, limit int) ([]notificationsapp.UncertainSummary, int, error) {
	return a.service.ListForUser(ctx, userID, limit)
}

// ListUncertainForAdmin 透传管理员全局不确定通知查询。
func (a *mcpUncertainNotificationPorts) ListUncertainForAdmin(ctx context.Context, limit int) ([]notificationsapp.UncertainSummary, int, error) {
	return a.service.ListForAdmin(ctx, limit)
}

// newMCPNotificationChannelPorts 构造 MCP 通知渠道端口；渠道服务缺失时返回 nil。
func newMCPNotificationChannelPorts(ports composition.TransportPorts) *mcpNotificationChannelPorts {
	if ports.NotificationChannels == nil {
		return nil
	}
	return &mcpNotificationChannelPorts{service: ports.NotificationChannels}
}

// newMCPUncertainNotificationPorts 构造 MCP 不确定通知端口；查询服务缺失时返回 nil。
func newMCPUncertainNotificationPorts(ports composition.TransportPorts) *mcpUncertainNotificationPorts {
	if ports.UncertainNotifications == nil {
		return nil
	}
	return &mcpUncertainNotificationPorts{service: ports.UncertainNotifications}
}
