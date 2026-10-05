// tools_account_test.go 覆盖账号域工具的 DTO 映射、归属用户透传与破坏性确认守卫。

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mcpproto "github.com/mark3labs/mcp-go/mcp"

	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
)

// fakeAccountPorts 是账号域端口的可计数假实现。
type fakeAccountPorts struct {
	// restartCalls 记录重启调用的账号标识。
	restartCalls []string
	// runCalls 记录立即执行任务的账号与类型。
	runCalls [][2]string
	// deleteCalls 记录删除调用的账号标识。
	deleteCalls []string
	// lastSettingsUser 记录最近综合设置写入携带的用户标识。
	lastSettingsUser int64
}

// ListAdminSummaries 返回两个跨用户账号摘要。
func (f *fakeAccountPorts) ListAdminSummaries(context.Context) ([]accountapp.AdminAccountSummary, error) {
	return []accountapp.AdminAccountSummary{
		{ID: "a1", UserID: 1, Owner: "admin", Enabled: true, Remark: "主号"},
		{ID: "a2", UserID: 2, Owner: "other", Enabled: false},
	}, nil
}

// ListSummaries 返回一个归属账号。
func (f *fakeAccountPorts) ListSummaries(_ context.Context, userID int64) ([]accountapp.AccountSummary, error) {
	return []accountapp.AccountSummary{{ID: "a1", UserID: userID, Nickname: "鱼老板", AutoConfirm: true, PauseDuration: 10}}, nil
}

// GetOwnedSummary 返回固定非敏感账号详情。
func (f *fakeAccountPorts) GetOwnedSummary(_ context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error) {
	return accountapp.AccountSummary{ID: cookieID, UserID: userID, Nickname: "鱼老板", AutoBargain: true}, nil
}

// RequireOwnership 默认放行。
func (f *fakeAccountPorts) RequireOwnership(context.Context, int64, string) error { return nil }

// UpdateAccountSettings 记录归属用户并返回成功结果。
func (f *fakeAccountPorts) UpdateAccountSettings(_ context.Context, input accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error) {
	f.lastSettingsUser = input.UserID
	return accountapp.SettingsResult{PausedUntil: 123}, nil
}

// SetAccountStatus 返回空状态结果。
func (f *fakeAccountPorts) SetAccountStatus(context.Context, int64, string, bool) (accountapp.StatusResult, error) {
	return accountapp.StatusResult{}, nil
}

// SetAccountPause 返回含截止时间的设置结果。
func (f *fakeAccountPorts) SetAccountPause(context.Context, int64, string, int) (accountapp.SettingsResult, error) {
	return accountapp.SettingsResult{PausedUntil: 456}, nil
}

// GetAccountPause 返回固定暂停状态。
func (f *fakeAccountPorts) GetAccountPause(context.Context, int64, string) (accountapp.PauseState, error) {
	return accountapp.PauseState{Duration: 30, PausedUntil: 456, Paused: true}, nil
}

// SetAutoConfirm 返回空设置结果。
func (f *fakeAccountPorts) SetAutoConfirm(context.Context, int64, string, bool) (accountapp.SettingsResult, error) {
	return accountapp.SettingsResult{}, nil
}

// SetAutoConsign 返回空设置结果。
func (f *fakeAccountPorts) SetAutoConsign(context.Context, int64, string, bool) (accountapp.SettingsResult, error) {
	return accountapp.SettingsResult{}, nil
}

// SetAutoBargain 返回空设置结果。
func (f *fakeAccountPorts) SetAutoBargain(context.Context, int64, string, bool) (accountapp.SettingsResult, error) {
	return accountapp.SettingsResult{}, nil
}

// SetRemark 返回空设置结果。
func (f *fakeAccountPorts) SetRemark(context.Context, int64, string, string) (accountapp.SettingsResult, error) {
	return accountapp.SettingsResult{}, nil
}

// QueryLongLogin 返回可开启且未开启的长登录状态。
func (f *fakeAccountPorts) QueryLongLogin(context.Context, int64, string) (accountapp.LongLoginResult, error) {
	return accountapp.LongLoginResult{CanOpenLongLogin: true, Enabled: false}, nil
}

// SetLongLogin 返回已开启状态。
func (f *fakeAccountPorts) SetLongLogin(context.Context, int64, string, bool) (accountapp.LongLoginResult, error) {
	return accountapp.LongLoginResult{CanOpenLongLogin: true, Enabled: true}, nil
}

// RuntimeStatuses 返回一个账号的运行时状态。
func (f *fakeAccountPorts) RuntimeStatuses(context.Context) (map[string]accountapp.RuntimeStatus, error) {
	return map[string]accountapp.RuntimeStatus{
		"a1": {State: "online", Connected: true, UpdatedAt: time.Unix(1000, 0)},
	}, nil
}

// RestartAccount 记录重启账号。
func (f *fakeAccountPorts) RestartAccount(_ context.Context, cookieID string) error {
	f.restartCalls = append(f.restartCalls, cookieID)
	return nil
}

// RefreshProfile 返回固定资料刷新结果。
func (f *fakeAccountPorts) RefreshProfile(_ context.Context, _ int64, cookieID string) (accountapp.ProfileResult, error) {
	return accountapp.ProfileResult{AccountID: cookieID, Nickname: "新昵称", AvatarURL: "http://example.com/a.png"}, nil
}

// DeleteAccount 记录删除账号。
func (f *fakeAccountPorts) DeleteAccount(_ context.Context, _ int64, cookieID string) error {
	f.deleteCalls = append(f.deleteCalls, cookieID)
	return nil
}

// GetAccountTaskSettings 返回固定评价/擦亮配置。
func (f *fakeAccountPorts) GetAccountTaskSettings(_ context.Context, cookieID string) (automationapp.AccountTaskSettings, error) {
	return automationapp.AccountTaskSettings{CookieID: cookieID, AutoRateEnabled: true, RateContent: "好评", PolishTime: "08:30"}, nil
}

// UpdateAccountTaskSettings 原样返回更新配置。
func (f *fakeAccountPorts) UpdateAccountTaskSettings(_ context.Context, settings automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error) {
	return settings, nil
}

// ListAccountTaskRuns 返回两条运行记录。
func (f *fakeAccountPorts) ListAccountTaskRuns(context.Context, string, int) ([]automationapp.AccountTaskRun, error) {
	return []automationapp.AccountTaskRun{
		{ID: 1, TaskType: "rate", Status: "success", SuccessCount: 2, StartedAt: 10, FinishedAt: 12},
		{ID: 2, TaskType: "polish", Status: "failed", FailedCount: 1, ErrorMessage: "风控"},
	}, nil
}

// RunAccountTask 记录立即执行的任务。
func (f *fakeAccountPorts) RunAccountTask(_ context.Context, cookieID, taskType string) (automationapp.TaskSummary, error) {
	f.runCalls = append(f.runCalls, [2]string{cookieID, taskType})
	return automationapp.TaskSummary{TaskType: taskType, Found: 3, Success: 3}, nil
}

// newAccountToolEndpoint 构造注册了账号工具且携带假端口的端点与身份上下文。
func newAccountToolEndpoint(t *testing.T) (*Endpoint, *fakeAccountPorts, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// ports 是假账号端口。
	ports := &fakeAccountPorts{}
	endpoint.RegisterAccountTools(ports)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ports, ctx
}

// invoke 直接以 JSON-RPC 请求形态调用指定工具，绕过网络层但经过确认/审计/错误归一框架。
func invoke(e *Endpoint, ctx context.Context, name string, args map[string]any) *mcpproto.CallToolResult {
	// request 是合成的工具调用请求。
	request := mcpproto.CallToolRequest{}
	request.Params.Name = name
	request.Params.Arguments = args
	// def 是已注册工具定义；未注册时直接返回错误结果。
	def, ok := e.toolDefs[name]
	if !ok {
		// unknown、_ 是未注册工具的协议错误结果。
		unknown, _ := ErrorResult(InvalidArgument("工具未注册: "+name))
		return unknown
	}
	// result、_ 是框架执行结果；业务错误已经被归一为 isError。
	result, _ := e.invokeTool(ctx, def, request)
	return result
}

// resultJSON 提取工具结果的文本 JSON。
func resultJSON(t *testing.T, result *mcpproto.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("工具结果缺少内容")
	}
	// text 是首个文本内容。
	text, ok := result.Content[0].(mcpproto.TextContent)
	if !ok {
		t.Fatalf("结果内容不是文本: %T", result.Content[0])
	}
	return text.Text
}

// TestAccountListToolsMapping 验证全局与归属列表的 DTO 映射和字段裁剪。
func TestAccountListToolsMapping(t *testing.T) {
	// endpoint、_、ctx 是账号工具端点与身份上下文。
	endpoint, _, ctx := newAccountToolEndpoint(t)
	// allResult 是全局账号列表结果。
	allResult := invoke(endpoint, ctx, "account_list_all", nil)
	if allResult.IsError {
		t.Fatalf("全局账号列表失败: %s", resultJSON(t, allResult))
	}
	// allText 是全局列表 JSON，必须含跨用户归属信息但不含凭证明文。
	allText := resultJSON(t, allResult)
	if !strings.Contains(allText, "other") || !strings.Contains(allText, `"total":2`) {
		t.Fatalf("全局列表内容异常: %s", allText)
	}
	// ownedResult 是归属账号列表结果。
	ownedResult := invoke(endpoint, ctx, "account_list", nil)
	// owned 是解析后的归属列表结构。
	var owned struct {
		Accounts []map[string]any `json:"accounts"`
	}
	// unmarshalErr 是归属列表 JSON 的解析错误。
	if unmarshalErr := json.Unmarshal([]byte(resultJSON(t, ownedResult)), &owned); unmarshalErr != nil {
		t.Fatalf("归属列表 JSON 非法: %v", unmarshalErr)
	}
	if len(owned.Accounts) != 1 || owned.Accounts[0]["account_id"] != "a1" {
		t.Fatalf("归属列表映射异常: %+v", owned.Accounts)
	}
	// 凭证与内部字段不得出现在输出中。
	for _, secret := range []string{"password", "cookie_value", "metadata", "value"} {
		if strings.Contains(strings.ToLower(allText), secret) {
			t.Fatalf("账号列表疑似泄漏字段 %q: %s", secret, allText)
		}
	}
}

// TestAccountGetRequiresArguments 验证缺参工具返回中文参数错误。
func TestAccountGetRequiresArguments(t *testing.T) {
	// endpoint、_、ctx 是账号工具端点与上下文。
	endpoint, _, ctx := newAccountToolEndpoint(t)
	// result 是缺少 account_id 的调用结果。
	result := invoke(endpoint, ctx, "account_get", map[string]any{})
	// text 是错误结果文本，必须提示缺失参数名。
	if !result.IsError || !strings.Contains(resultJSON(t, result), "account_id") {
		t.Fatalf("缺参必须返回中文参数错误，实际 %+v", result)
	}
}

// TestDestructiveToolsRequireConfirm 验证重启/删除/立即执行工具缺 confirm 时零用例触达。
func TestDestructiveToolsRequireConfirm(t *testing.T) {
	// endpoint、ports、ctx 是账号工具端点、假端口与上下文。
	endpoint, ports, ctx := newAccountToolEndpoint(t)
	// cases 是破坏性工具用例：工具名、入参、被调端口计数的校验函数。
	cases := []struct {
		name string
		args map[string]any
		hits func() int
	}{
		{"account_restart", map[string]any{"account_id": "a1"}, func() int { return len(ports.restartCalls) }},
		{"account_delete", map[string]any{"account_id": "a1"}, func() int { return len(ports.deleteCalls) }},
		{"account_task_run", map[string]any{"account_id": "a1", "task_type": "rate"}, func() int { return len(ports.runCalls) }},
	}
	// tc 是当前破坏性工具用例。
	for _, tc := range cases {
		// denied 是不带 confirm 的调用，必须失败且不触达端口。
		denied := invoke(endpoint, ctx, tc.name, tc.args)
		if !denied.IsError || tc.hits() != 0 {
			t.Fatalf("工具 %s 缺 confirm 必须被拦截", tc.name)
		}
		// 带 confirm=true 的同一调用应成功并触达一次端口。
		tc.args["confirm"] = true
		// allowed 是显式确认后的调用结果。
		allowed := invoke(endpoint, ctx, tc.name, tc.args)
		if allowed.IsError || tc.hits() != 1 {
			t.Fatalf("工具 %s 带 confirm 后应执行: %s", tc.name, resultJSON(t, allowed))
		}
	}
}

// TestAccountSettingsPropagateUser 验证写入工具把固定管理员用户标识透传给用例。
func TestAccountSettingsPropagateUser(t *testing.T) {
	// endpoint、ports、ctx 是账号工具端点、假端口与身份为用户 1 的上下文。
	endpoint, ports, ctx := newAccountToolEndpoint(t)
	// result 是备注写入结果。
	result := invoke(endpoint, ctx, "account_set_remark", map[string]any{"account_id": "a1", "remark": "新备注"})
	if result.IsError || ports.lastSettingsUser != 1 {
		t.Fatalf("备注写入必须透传用户 1，实际 user=%d err=%s", ports.lastSettingsUser, resultJSON(t, result))
	}
}

// TestAccountToolAnnotations 验证破坏性与只读注解在注册清单上的分布。
func TestAccountToolAnnotations(t *testing.T) {
	// endpoint、_、_ 是账号工具端点。
	endpoint, _, _ := newAccountToolEndpoint(t)
	// destructive 是应当标记破坏性的工具名集合。
	destructive := map[string]bool{
		"account_restart": true, "account_refresh_profile": true, "account_delete": true,
		"account_set_long_login": true, "account_task_run": true,
	}
	// def 是当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		if !strings.HasPrefix(def.Name, "account_") {
			continue
		}
		// want 是该工具期望的破坏性标记。
		want := destructive[def.Name]
		if def.Destructive != want {
			t.Fatalf("工具 %s 破坏性标记 %v，期望 %v", def.Name, def.Destructive, want)
		}
	}
}
