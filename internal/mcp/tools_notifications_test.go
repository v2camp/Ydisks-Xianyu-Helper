// tools_notifications_test.go 覆盖通知域工具：渠道 CRUD 与编辑态脱敏、秘密零泄漏扫描、
// 账号绑定读/写/切换/删除/清空、不确定通知的用户视图与管理员视图数据边界。

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// smtpSecret 是夹具中的模拟 SMTP 密码，任何 MCP 输出与错误都不得包含该片段。
const smtpSecret = "SMTP-SECRET-9911"

// botSecret 是夹具中的模拟机器人 secret，任何 MCP 输出与错误都不得包含该片段。
const botSecret = "BOT-SECRET-7733"

// fakeNotificationChannels 是通知渠道域假端口，记录调用并支持注入错误。
type fakeNotificationChannels struct {
	// channels 是渠道摘要列表返回。
	channels []notificationsapp.ChannelSummary
	// editor 是渠道编辑态返回。
	editor notificationsapp.ChannelEditor
	// bindings 是绑定摘要列表返回。
	bindings []notificationsapp.BindingSummary
	// bindingIDs 是账号启用渠道标识返回。
	bindingIDs []int64
	// listErr、editorErr、createErr、updateErr、deleteErr、testErr 是渠道类错误。
	listErr, editorErr, createErr, updateErr, deleteErr, testErr error
	// bindingsErr、bindingIDsErr、setErr、toggleErr、deleteBindingErr、clearErr 是绑定类错误。
	bindingsErr, bindingIDsErr, setErr, toggleErr, deleteBindingErr, clearErr error
	// createID 是创建成功返回的渠道标识。
	createID int64
	// listCalls、editorCalls、createCalls、updateCalls、deleteCalls、testCalls 是渠道类调用计数。
	listCalls, editorCalls, createCalls, updateCalls, deleteCalls, testCalls int
	// bindingsCalls、bindingIDsCalls、setCalls、toggleCalls、deleteBindingCalls、clearCalls 是绑定类调用计数。
	bindingsCalls, bindingIDsCalls, setCalls, toggleCalls, deleteBindingCalls, clearCalls int
	// createdInput、updatedPatch 是写入用例收到的入参。
	createdInput notificationsapp.ChannelInput
	updatedPatch notificationsapp.ChannelPatch
	// setChannelIDs 是整组设置收到的渠道标识。
	setChannelIDs []int64
	// toggledEnabled 是单条切换收到的目标状态。
	toggledEnabled bool
	// deletedBindingID 是删除绑定收到的标识。
	deletedBindingID int64
}

// ListChannels 回传预设渠道摘要。
func (f *fakeNotificationChannels) ListChannels(context.Context, int64) ([]notificationsapp.ChannelSummary, error) {
	f.listCalls++
	return f.channels, f.listErr
}

// GetChannelEditor 回传预设编辑态。
func (f *fakeNotificationChannels) GetChannelEditor(context.Context, int64, int64) (notificationsapp.ChannelEditor, error) {
	f.editorCalls++
	return f.editor, f.editorErr
}

// CreateChannel 记录创建入参并返回预设标识。
func (f *fakeNotificationChannels) CreateChannel(_ context.Context, _ int64, input notificationsapp.ChannelInput) (int64, error) {
	f.createCalls++
	f.createdInput = input
	return f.createID, f.createErr
}

// UpdateChannel 记录更新补丁。
func (f *fakeNotificationChannels) UpdateChannel(_ context.Context, _, _ int64, patch notificationsapp.ChannelPatch) error {
	f.updateCalls++
	f.updatedPatch = patch
	return f.updateErr
}

// DeleteChannel 记录删除调用。
func (f *fakeNotificationChannels) DeleteChannel(context.Context, int64, int64) error {
	f.deleteCalls++
	return f.deleteErr
}

// TestChannel 记录测试发送调用。
func (f *fakeNotificationChannels) TestChannel(context.Context, int64, int64) error {
	f.testCalls++
	return f.testErr
}

// ListBindings 回传预设绑定摘要。
func (f *fakeNotificationChannels) ListBindings(context.Context, int64) ([]notificationsapp.BindingSummary, error) {
	f.bindingsCalls++
	return f.bindings, f.bindingsErr
}

// GetBindingIDs 回传预设渠道标识。
func (f *fakeNotificationChannels) GetBindingIDs(context.Context, int64, string) ([]int64, error) {
	f.bindingIDsCalls++
	return f.bindingIDs, f.bindingIDsErr
}

// SetBindings 记录整组设置入参。
func (f *fakeNotificationChannels) SetBindings(_ context.Context, _ int64, _ string, channelIDs []int64) error {
	f.setCalls++
	f.setChannelIDs = channelIDs
	return f.setErr
}

// SetSingleBinding 记录单条切换入参。
func (f *fakeNotificationChannels) SetSingleBinding(_ context.Context, _ int64, _ string, _ int64, enabled bool) error {
	f.toggleCalls++
	f.toggledEnabled = enabled
	return f.toggleErr
}

// DeleteBinding 记录删除绑定入参。
func (f *fakeNotificationChannels) DeleteBinding(_ context.Context, _, bindingID int64) error {
	f.deleteBindingCalls++
	f.deletedBindingID = bindingID
	return f.deleteBindingErr
}

// DeleteAccountBindings 记录账号绑定清空调用。
func (f *fakeNotificationChannels) DeleteAccountBindings(context.Context, int64, string) error {
	f.clearCalls++
	return f.clearErr
}

// fakeUncertainNotifications 是不确定通知查询假端口。
type fakeUncertainNotifications struct {
	// items 是查询返回的摘要列表。
	items []notificationsapp.UncertainSummary
	// total 是查询返回的总数。
	total int
	// userErr、adminErr 是两类查询注入错误。
	userErr, adminErr error
	// userCalls、adminCalls 是两类查询调用计数。
	userCalls, adminCalls int
	// userLimit、adminLimit 是两类查询收到的条数上限。
	userLimit, adminLimit int
}

// ListUncertainForUser 记录用户视图查询。
func (f *fakeUncertainNotifications) ListUncertainForUser(_ context.Context, _ int64, limit int) ([]notificationsapp.UncertainSummary, int, error) {
	f.userCalls++
	f.userLimit = limit
	return f.items, f.total, f.userErr
}

// ListUncertainForAdmin 记录管理员视图查询。
func (f *fakeUncertainNotifications) ListUncertainForAdmin(_ context.Context, limit int) ([]notificationsapp.UncertainSummary, int, error) {
	f.adminCalls++
	f.adminLimit = limit
	return f.items, f.total, f.adminErr
}

// newNotificationToolEndpoint 构造注册通知工具的端点、假端口与身份上下文。
func newNotificationToolEndpoint(t *testing.T, channels *fakeNotificationChannels, uncertain *fakeUncertainNotifications) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// channels、uncertain 任一为 nil 时对应域工具不注册。
	var channelPorts NotificationChannelPorts
	if channels != nil {
		channelPorts = channels
	}
	// uncertainPorts 是不确定通知端口；为 nil 时该类工具不注册。
	var uncertainPorts UncertainNotificationPorts
	if uncertain != nil {
		uncertainPorts = uncertain
	}
	endpoint.RegisterNotificationTools(channelPorts, uncertainPorts)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// assertNoNotificationSecret 断言输出文本不含任何模拟渠道密钥。
func assertNoNotificationSecret(t *testing.T, text string) {
	t.Helper()
	// secret 是当前扫描的密钥片段。
	for _, secret := range []string{smtpSecret, botSecret, "smtp_password", "smtpPassword"} {
		if strings.Contains(text, secret) {
			t.Fatalf("通知域响应泄漏渠道密钥 %q: %s", secret, text)
		}
	}
}

// TestNotificationChannelViewsNeverLeakSecrets 验证列表与编辑态输出对渠道密钥零泄漏（TR-10.1）。
func TestNotificationChannelViewsNeverLeakSecrets(t *testing.T) {
	// fake 是含渠道摘要与编辑态的假端口。
	fake := &fakeNotificationChannels{
		channels: []notificationsapp.ChannelSummary{
			{ID: 3, Name: "邮件通知", Type: "email", EventTypes: "all", Enabled: true},
			{ID: 4, Name: "机器人通知", Type: "qq", EventTypes: "order_paid", Enabled: false},
		},
		editor: notificationsapp.ChannelEditor{
			ID: 3, Name: "邮件通知", Type: "email", EventTypes: "all", Enabled: true,
			ToEmail: "owner@example.com", UseCustomSMTP: true,
		},
		createID: 9,
	}
	// endpoint、ctx 是通知工具端点与身份上下文。
	endpoint, ctx := newNotificationToolEndpoint(t, fake, nil)
	// listResult 是渠道列表结果。
	listResult := invoke(endpoint, ctx, "notification_channel_list", nil)
	if listResult.IsError {
		t.Fatalf("渠道列表失败: %s", resultJSON(t, listResult))
	}
	// listText 是列表 JSON，必须含两类渠道且不含密钥字段。
	listText := resultJSON(t, listResult)
	if !strings.Contains(listText, `"total":2`) || !strings.Contains(listText, `"type":"qq"`) {
		t.Fatalf("渠道列表映射异常: %s", listText)
	}
	assertNoNotificationSecret(t, listText)
	assertNoChannelConfigField(t, listText)
	// editorResult 是渠道编辑态结果。
	editorResult := invoke(endpoint, ctx, "notification_channel_get", map[string]any{"channel_id": 3.0})
	if editorResult.IsError {
		t.Fatalf("渠道编辑态失败: %s", resultJSON(t, editorResult))
	}
	// editorText 是编辑态 JSON，必须含收件地址与自定义 SMTP 标记，且明确密钥只写不读。
	editorText := resultJSON(t, editorResult)
	// fragment 是编辑态响应中必须出现的片段。
	for _, fragment := range []string{`"to_email":"owner@example.com"`, `"use_custom_smtp":true`, "只可写入"} {
		if !strings.Contains(editorText, fragment) {
			t.Fatalf("渠道编辑态缺少 %s: %s", fragment, editorText)
		}
	}
	assertNoNotificationSecret(t, editorText)
	assertNoChannelConfigField(t, editorText)
	// createResult 是渠道创建结果。
	createResult := invoke(endpoint, ctx, "notification_channel_create", map[string]any{
		"name": "新邮件渠道", "type": "email",
		"config_json": `{"to_email":"a@b.com","smtp_password":"` + smtpSecret + `"}`, "enabled": true,
	})
	if createResult.IsError || fake.createCalls != 1 {
		t.Fatalf("渠道创建失败: %s", resultJSON(t, createResult))
	}
	// 创建响应不得回显提交的配置明文。
	assertNoNotificationSecret(t, resultJSON(t, createResult))
	if fake.createdInput.Config == "" || fake.createdInput.Name != "新邮件渠道" {
		t.Fatalf("渠道创建入参异常: %+v", fake.createdInput)
	}
}

// assertNoChannelConfigField 断言输出中不含渠道配置字段。
func assertNoChannelConfigField(t *testing.T, text string) {
	t.Helper()
	// field 是绝不允许出现在输出中的配置字段名。
	for _, field := range []string{`"config"`, `"config_json":"{"`} {
		if strings.Contains(text, field) {
			t.Fatalf("通知输出不应包含渠道配置字段 %s: %s", field, text)
		}
	}
}

// TestNotificationChannelUpdateSemantics 验证部分更新只提交显式字段，密钥类字段只写不读。
func TestNotificationChannelUpdateSemantics(t *testing.T) {
	// fake 是通知渠道假端口。
	fake := &fakeNotificationChannels{}
	// endpoint、ctx 是通知工具端点与身份上下文。
	endpoint, ctx := newNotificationToolEndpoint(t, fake, nil)
	// metadataOnly 是只改名称与开关的更新。
	metadataOnly := invoke(endpoint, ctx, "notification_channel_update", map[string]any{
		"channel_id": 3.0, "name": "改名后", "enabled": false,
	})
	if metadataOnly.IsError || fake.updateCalls != 1 {
		t.Fatalf("渠道元数据更新失败: %s", resultJSON(t, metadataOnly))
	}
	// patch 必须只包含显式提交的字段，其余保持 nil 以保留现值。
	if fake.updatedPatch.Name == nil || *fake.updatedPatch.Name != "改名后" ||
		fake.updatedPatch.Enabled == nil || *fake.updatedPatch.Enabled ||
		fake.updatedPatch.Config != nil || fake.updatedPatch.Type != nil || fake.updatedPatch.EventTypes != nil {
		t.Fatalf("渠道更新补丁异常: %+v", fake.updatedPatch)
	}
	// recipientOnly 是只改邮件收件地址的更新，必须走 email_recipient 以保留 SMTP 凭据。
	recipientOnly := invoke(endpoint, ctx, "notification_channel_update", map[string]any{
		"channel_id": 3.0, "email_recipient": "new@example.com",
	})
	if recipientOnly.IsError || fake.updatedPatch.EmailRecipient == nil || *fake.updatedPatch.EmailRecipient != "new@example.com" {
		t.Fatalf("邮件收件地址更新异常: %+v", fake.updatedPatch)
	}
	// 更新响应同样不得回显任何密钥。
	assertNoNotificationSecret(t, resultJSON(t, recipientOnly))
	// 越权或参数错误时归一为对应中文提示。
	fake.updateErr = notificationsapp.ErrChannelForbidden
	// forbidden 是越权更新结果。
	forbidden := invoke(endpoint, ctx, "notification_channel_update", map[string]any{"channel_id": 9.0, "name": "x"})
	if !strings.Contains(resultJSON(t, forbidden), "无权操作该通知渠道") {
		t.Fatalf("渠道越权映射异常: %s", resultJSON(t, forbidden))
	}
	fake.updateErr = errors.New("smtp dial tcp 10.0.0.9:25: connect: connection refused")
	// infra 是未归类错误的归一结果，不得回显底层网络细节。
	infra := invoke(endpoint, ctx, "notification_channel_update", map[string]any{"channel_id": 3.0, "name": "y"})
	if !infra.IsError || strings.Contains(resultJSON(t, infra), "10.0.0.9") {
		t.Fatalf("未归类错误不得回显基础设施细节: %s", resultJSON(t, infra))
	}
	fake.updateErr = nil
}

// TestNotificationChannelConfirmGates 验证渠道删除与测试发送的 confirm 守卫（TR-10.2）。
func TestNotificationChannelConfirmGates(t *testing.T) {
	// fake 是通知渠道假端口。
	fake := &fakeNotificationChannels{}
	// endpoint、ctx 是通知工具端点与身份上下文。
	endpoint, ctx := newNotificationToolEndpoint(t, fake, nil)
	// deleteDenied、testDenied 是缺 confirm 的破坏性操作。
	deleteDenied := invoke(endpoint, ctx, "notification_channel_delete", map[string]any{"channel_id": 3.0})
	// testDenied 是缺 confirm 的测试发送。
	testDenied := invoke(endpoint, ctx, "notification_channel_test", map[string]any{"channel_id": 3.0})
	if !deleteDenied.IsError || !testDenied.IsError || fake.deleteCalls != 0 || fake.testCalls != 0 {
		t.Fatal("缺 confirm 的渠道删除与测试发送必须零触达")
	}
	// deleteAllowed、testAllowed 是显式确认后的操作。
	deleteAllowed := invoke(endpoint, ctx, "notification_channel_delete", map[string]any{"channel_id": 3.0, "confirm": true})
	// testAllowed 是显式确认后的测试发送。
	testAllowed := invoke(endpoint, ctx, "notification_channel_test", map[string]any{"channel_id": 3.0, "confirm": true})
	if deleteAllowed.IsError || testAllowed.IsError || fake.deleteCalls != 1 || fake.testCalls != 1 {
		t.Fatalf("带 confirm 的渠道删除与测试发送应执行: %s / %s",
			resultJSON(t, deleteAllowed), resultJSON(t, testAllowed))
	}
	// 通知器未启用时归一为中文服务端提示。
	fake.testErr = notificationsapp.ErrNotifierUnavailable
	// unavailable 是通知器缺失的测试发送结果。
	unavailable := invoke(endpoint, ctx, "notification_channel_test", map[string]any{"channel_id": 3.0, "confirm": true})
	if !strings.Contains(resultJSON(t, unavailable), "通知器未启用") {
		t.Fatalf("通知器缺失映射异常: %s", resultJSON(t, unavailable))
	}
	// 测试发送失败不得回显渠道密钥或远端细节。
	fake.testErr = errors.New("smtp auth failed for user owner@example.com password " + smtpSecret)
	// failed = 测试发送失败结果。
	failed := invoke(endpoint, ctx, "notification_channel_test", map[string]any{"channel_id": 3.0, "confirm": true})
	assertNoNotificationSecret(t, resultJSON(t, failed))
}

// TestNotificationBindingTools 验证绑定读/整组设置/单条切换/删除/清空的映射与 confirm 守卫。
func TestNotificationBindingTools(t *testing.T) {
	// fake 是通知渠道假端口（含绑定能力）。
	fake := &fakeNotificationChannels{
		bindings: []notificationsapp.BindingSummary{
			{ID: 5, CookieID: "acc1", ChannelID: 3, ChannelName: "邮件通知", Enabled: true},
			{ID: 6, CookieID: "acc1", ChannelID: 4, ChannelName: "机器人通知", Enabled: false},
		},
		bindingIDs: []int64{3},
	}
	// endpoint、ctx 是通知工具端点与身份上下文。
	endpoint, ctx := newNotificationToolEndpoint(t, fake, nil)
	// listResult 是绑定列表结果。
	listResult := invoke(endpoint, ctx, "notification_binding_list", nil)
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"channel_name":"邮件通知"`) {
		t.Fatalf("绑定列表异常: %s", resultJSON(t, listResult))
	}
	// getResult 是账号绑定读取结果。
	getResult := invoke(endpoint, ctx, "notification_binding_get", map[string]any{"account_id": "acc1"})
	if getResult.IsError || !strings.Contains(resultJSON(t, getResult), `"channel_ids":[3]`) {
		t.Fatalf("账号绑定读取异常: %s", resultJSON(t, getResult))
	}
	// setResult 是整组设置结果。
	setResult := invoke(endpoint, ctx, "notification_binding_set", map[string]any{
		"account_id": "acc1", "channel_ids": []any{3.0, 4.0},
	})
	if setResult.IsError || fake.setCalls != 1 || len(fake.setChannelIDs) != 2 {
		t.Fatalf("整组设置异常: %s", resultJSON(t, setResult))
	}
	// emptySet 是空数组整组设置，表示解绑全部渠道。
	emptySet := invoke(endpoint, ctx, "notification_binding_set", map[string]any{"account_id": "acc1", "channel_ids": []any{}})
	if emptySet.IsError || fake.setCalls != 2 || len(fake.setChannelIDs) != 0 {
		t.Fatalf("空数组整组设置异常: %s", resultJSON(t, emptySet))
	}
	// badIDs 是元素类型错误的设置入参，必须被拒绝且零触达。
	badIDs := invoke(endpoint, ctx, "notification_binding_set", map[string]any{"account_id": "acc1", "channel_ids": []any{"3"}})
	if !badIDs.IsError || fake.setCalls != 2 {
		t.Fatalf("非法渠道数组必须零触达: %s", resultJSON(t, badIDs))
	}
	// toggleResult 是单条切换结果。
	toggleResult := invoke(endpoint, ctx, "notification_binding_toggle", map[string]any{
		"account_id": "acc1", "channel_id": 4.0, "enabled": true,
	})
	if toggleResult.IsError || fake.toggleCalls != 1 || !fake.toggledEnabled {
		t.Fatalf("单条切换异常: %s", resultJSON(t, toggleResult))
	}
	// deleteDenied、clearDenied 是缺 confirm 的破坏性操作。
	deleteDenied := invoke(endpoint, ctx, "notification_binding_delete", map[string]any{"binding_id": 5.0})
	// clearDenied 是缺 confirm 的账号绑定清空。
	clearDenied := invoke(endpoint, ctx, "notification_binding_clear_account", map[string]any{"account_id": "acc1"})
	if !deleteDenied.IsError || !clearDenied.IsError || fake.deleteBindingCalls != 0 || fake.clearCalls != 0 {
		t.Fatal("缺 confirm 的绑定删除与清空必须零触达")
	}
	// deleteAllowed、clearAllowed 是显式确认后的操作。
	deleteAllowed := invoke(endpoint, ctx, "notification_binding_delete", map[string]any{"binding_id": 5.0, "confirm": true})
	// clearAllowed 是显式确认后的账号绑定清空。
	clearAllowed := invoke(endpoint, ctx, "notification_binding_clear_account", map[string]any{"account_id": "acc1", "confirm": true})
	if deleteAllowed.IsError || clearAllowed.IsError || fake.deleteBindingCalls != 1 || fake.clearCalls != 1 ||
		fake.deletedBindingID != 5 {
		t.Fatalf("带 confirm 的绑定删除与清空应执行: %s / %s",
			resultJSON(t, deleteAllowed), resultJSON(t, clearAllowed))
	}
	// 账号越权时归一为中文提示。
	fake.setErr = notificationsapp.ErrAccountForbidden
	// forbidden 是越权整组设置结果。
	forbidden := invoke(endpoint, ctx, "notification_binding_set", map[string]any{"account_id": "acc9", "channel_ids": []any{3.0}})
	if !strings.Contains(resultJSON(t, forbidden), "无权操作该通知渠道或账号绑定") {
		t.Fatalf("账号越权映射异常: %s", resultJSON(t, forbidden))
	}
}

// TestNotificationUncertainViews 验证不确定通知的用户视图与管理员视图数据边界（TR-10.2）。
func TestNotificationUncertainViews(t *testing.T) {
	// fake 是不确定通知查询假端口。
	fake := &fakeUncertainNotifications{
		items: []notificationsapp.UncertainSummary{
			{ID: 11, ChannelID: 3, OwnerUserID: 7, EventType: "order_paid", AttemptCount: 2, UncertainAt: 1700000000, HasError: true},
		},
		total: 1,
	}
	// endpoint、ctx 是通知工具端点与身份上下文。
	endpoint, ctx := newNotificationToolEndpoint(t, nil, fake)
	// userResult 是用户视图结果。
	userResult := invoke(endpoint, ctx, "notification_uncertain_list", map[string]any{"limit": 5.0})
	if userResult.IsError {
		t.Fatalf("不确定通知用户视图失败: %s", resultJSON(t, userResult))
	}
	// userText 是用户视图 JSON：不得包含所属用户标识，避免越权推断其他用户。
	userText := resultJSON(t, userResult)
	if strings.Contains(userText, `"owner_user_id"`) || !strings.Contains(userText, `"has_error":true`) {
		t.Fatalf("用户视图数据边界异常: %s", userText)
	}
	if fake.userLimit != 5 {
		t.Fatalf("用户视图条数上限异常: %d", fake.userLimit)
	}
	// adminResult 是管理员全局视图结果。
	adminResult := invoke(endpoint, ctx, "notification_uncertain_list_all", nil)
	if adminResult.IsError {
		t.Fatalf("不确定通知管理员视图失败: %s", resultJSON(t, adminResult))
	}
	// adminText 是管理员视图 JSON：必须包含所属用户标识以便跨用户排查。
	adminText := resultJSON(t, adminResult)
	if !strings.Contains(adminText, `"owner_user_id":7`) || !strings.Contains(adminText, `"total":1`) {
		t.Fatalf("管理员视图数据边界异常: %s", adminText)
	}
	// 两类视图的条数上限互相独立。
	if fake.adminLimit != 20 || fake.userCalls != 1 || fake.adminCalls != 1 {
		t.Fatalf("不确定通知查询调用异常: user=%d admin=%d limit=%d", fake.userCalls, fake.adminCalls, fake.adminLimit)
	}
	// 摘要输出不得包含通知正文或错误原文。
	if strings.Contains(adminText, "error_message") || strings.Contains(adminText, "body") {
		t.Fatalf("不确定通知摘要不应包含正文或错误原文: %s", adminText)
	}
}

// TestNotificationToolInventory 核对通知域工具清单、破坏性注解与端口缺失行为。
func TestNotificationToolInventory(t *testing.T) {
	// expected 是 Task 10 要求注册的全部工具名。
	expected := map[string]bool{
		"notification_channel_list": true, "notification_channel_get": true, "notification_channel_create": true,
		"notification_channel_update": true, "notification_channel_delete": true, "notification_channel_test": true,
		"notification_binding_list": true, "notification_binding_get": true, "notification_binding_set": true,
		"notification_binding_toggle": true, "notification_binding_delete": true, "notification_binding_clear_account": true,
		"notification_uncertain_list": true, "notification_uncertain_list_all": true,
	}
	// destructive 是必须标记破坏性的工具集合。
	destructive := map[string]bool{
		"notification_channel_delete": true, "notification_channel_test": true,
		"notification_binding_delete": true, "notification_binding_clear_account": true,
	}
	// endpoint、_ 是注册通知工具的端点。
	endpoint, _ := newNotificationToolEndpoint(t, &fakeNotificationChannels{}, &fakeUncertainNotifications{})
	// registered 是实际注册的工具名集合。
	registered := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		registered[def.Name] = true
		if destructive[def.Name] != def.Destructive {
			t.Fatalf("工具 %s 破坏性注解异常: %t", def.Name, def.Destructive)
		}
	}
	if len(registered) != len(expected) {
		t.Fatalf("通知域工具数量异常: got=%d want=%d", len(registered), len(expected))
	}
	// name 是期望注册的工具名。
	for name := range expected {
		if !registered[name] {
			t.Fatalf("缺少工具 %s", name)
		}
	}
	// 两个端口都缺失时不得注册任何工具。
	empty, _ := newProtocolEndpoint(t)
	empty.RegisterNotificationTools(nil, nil)
	// defs 是通知端口缺失时注册到的工具清单，必须为空。
	if defs := empty.ToolDefs(); len(defs) != 0 {
		t.Fatalf("通知端口缺失时不应注册工具，实际 %d 个", len(defs))
	}
	// 仅缺不确定通知端口时，渠道与绑定工具仍应可用。
	partial, _ := newProtocolEndpoint(t)
	partial.RegisterNotificationTools(&fakeNotificationChannels{}, nil)
	// partialNames 是部分装配下注册到的工具名集合。
	partialNames := map[string]bool{}
	// def 是部分装配下的当前工具定义。
	for _, def := range partial.ToolDefs() {
		partialNames[def.Name] = true
	}
	if partialNames["notification_uncertain_list"] || !partialNames["notification_channel_list"] {
		t.Fatalf("部分装配注册行为异常: %v", partialNames)
	}
}
