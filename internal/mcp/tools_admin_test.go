// tools_admin_test.go 覆盖管理员全局域工具：全局统计、用户列表脱敏、删除用户 confirm 与自删拒绝、
// 进程后台任务总览，并与账号域普通归属工具做数据边界对照。

package mcp

import (
	"context"
	"strings"
	"testing"

	adminapp "xianyu-go/internal/application/admin"

	"xianyu-go/internal/capability"
)

// adminUserPasswordLike 是用户摘要中绝不应出现的字段名集合。
var adminUserPasswordLike = []string{"password", "cookie", "token", "secret", "credential"}

// fakeAdminPorts 是管理员域假端口，记录调用并支持注入错误与任务快照。
type fakeAdminPorts struct {
	// users 是用户摘要列表返回。
	users []adminapp.UserSummary
	// stats 是全局统计返回。
	stats adminapp.Stats
	// tasks 是后台任务快照返回。
	tasks []capability.BackgroundTask
	// listErr、deleteErr、statsErr 是各用例注入错误。
	listErr, deleteErr, statsErr error
	// listCalls、deleteCalls、statsCalls 是各用例调用计数。
	listCalls, deleteCalls, statsCalls int
	// deletedCurrent、deletedTarget 是删除用例收到的当前与目标用户标识。
	deletedCurrent, deletedTarget int64
}

// ListUsers 回传预设用户摘要。
func (f *fakeAdminPorts) ListUsers(context.Context) ([]adminapp.UserSummary, error) {
	f.listCalls++
	return f.users, f.listErr
}

// DeleteUser 记录删除入参并回传预设错误。
func (f *fakeAdminPorts) DeleteUser(_ context.Context, currentUserID, targetUserID int64) error {
	f.deleteCalls++
	f.deletedCurrent, f.deletedTarget = currentUserID, targetUserID
	return f.deleteErr
}

// Stats 回传预设统计。
func (f *fakeAdminPorts) Stats(context.Context) (adminapp.Stats, error) {
	f.statsCalls++
	return f.stats, f.statsErr
}

// BackgroundTasks 回传预设后台任务快照。
func (f *fakeAdminPorts) BackgroundTasks() []capability.BackgroundTask {
	return f.tasks
}

// newAdminToolEndpoint 构造注册管理员工具的端点、假端口与身份上下文。
func newAdminToolEndpoint(t *testing.T, fake *fakeAdminPorts) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterAdminTools(fake)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// TestAdminStatsAndUserList 验证全局统计与用户列表映射及脱敏（TR-12.1）。
func TestAdminStatsAndUserList(t *testing.T) {
	// fake 是含两名用户的假管理员端口。
	fake := &fakeAdminPorts{
		users: []adminapp.UserSummary{
			{ID: 1, Username: "admin", Email: "admin@example.com", IsActive: true, IsAdmin: true, CookieCount: 2, CreatedAt: "2026-01-01"},
			{ID: 2, Username: "operator", IsActive: false, IsAdmin: false, CookieCount: 5},
		},
		stats: adminapp.Stats{TotalUsers: 2, TotalCookies: 7, ActiveCookies: 6, TotalCards: 3, TotalKeywords: 9, TotalOrders: 42},
	}
	// endpoint、ctx 是管理员工具端点与身份上下文。
	endpoint, ctx := newAdminToolEndpoint(t, fake)
	// statsResult 是全局统计结果。
	statsResult := invoke(endpoint, ctx, "admin_stats", nil)
	if statsResult.IsError {
		t.Fatalf("全局统计失败: %s", resultJSON(t, statsResult))
	}
	// statsText 是统计 JSON，必须覆盖六项计数。
	statsText := resultJSON(t, statsResult)
	// fragment 是统计响应中必须出现的计数片段。
	for _, fragment := range []string{`"total_users":2`, `"total_accounts":7`, `"active_accounts":6`, `"total_card_groups":3`, `"total_keywords":9`, `"total_orders":42`} {
		if !strings.Contains(statsText, fragment) {
			t.Fatalf("全局统计缺少 %s: %s", fragment, statsText)
		}
	}
	// userResult 是用户列表结果。
	userResult := invoke(endpoint, ctx, "admin_user_list", nil)
	if userResult.IsError {
		t.Fatalf("用户列表失败: %s", resultJSON(t, userResult))
	}
	// userText 是用户列表 JSON，必须跨用户返回摘要且不含任何凭证字段名。
	userText := resultJSON(t, userResult)
	if !strings.Contains(userText, `"username":"operator"`) || !strings.Contains(userText, `"account_count":5`) ||
		!strings.Contains(userText, `"is_admin":true`) {
		t.Fatalf("用户列表映射异常: %s", userText)
	}
	// forbidden 是绝不允许出现在用户摘要中的敏感字段名。
	for _, forbidden := range adminUserPasswordLike {
		if strings.Contains(strings.ToLower(userText), forbidden) {
			t.Fatalf("用户摘要不应包含凭证字段 %s: %s", forbidden, userText)
		}
	}
	if fake.statsCalls != 1 || fake.listCalls != 1 {
		t.Fatalf("管理员读取调用次数异常: stats=%d list=%d", fake.statsCalls, fake.listCalls)
	}
}

// TestAdminDeleteUserConfirmAndSelfGuard 验证删除用户的 confirm 守卫、身份透传与自删拒绝（TR-12.1）。
func TestAdminDeleteUserConfirmAndSelfGuard(t *testing.T) {
	// fake 是管理员域假端口。
	fake := &fakeAdminPorts{}
	// endpoint、ctx 是管理员工具端点与身份上下文。
	endpoint, ctx := newAdminToolEndpoint(t, fake)
	// denied 是缺 confirm 的删除。
	denied := invoke(endpoint, ctx, "admin_user_delete", map[string]any{"user_id": 2.0})
	if !denied.IsError || fake.deleteCalls != 0 {
		t.Fatal("缺 confirm 删除用户必须拦截且零触达")
	}
	// allowed 是显式确认后的删除，必须把固定管理员身份作为当前用户透传。
	allowed := invoke(endpoint, ctx, "admin_user_delete", map[string]any{"user_id": 2.0, "confirm": true})
	if allowed.IsError || fake.deleteCalls != 1 || fake.deletedCurrent != 1 || fake.deletedTarget != 2 {
		t.Fatalf("删除用户透传异常: %s current=%d target=%d", resultJSON(t, allowed), fake.deletedCurrent, fake.deletedTarget)
	}
	if !strings.Contains(resultJSON(t, allowed), `"deleted":true`) {
		t.Fatalf("删除确认映射异常: %s", resultJSON(t, allowed))
	}
	// 自删拒绝由应用服务给出，MCP 必须原样归一为中文可操作提示。
	fake.deleteErr = adminapp.ErrSelfDelete
	// selfDelete 是自删尝试结果。
	selfDelete := invoke(endpoint, ctx, "admin_user_delete", map[string]any{"user_id": 1.0, "confirm": true})
	if !strings.Contains(resultJSON(t, selfDelete), "不能删除当前登录管理员账号") {
		t.Fatalf("自删拒绝映射异常: %s", resultJSON(t, selfDelete))
	}
	// 运行实例收束失败必须归一为内部错误提示，不暴露底层细节。
	fake.deleteErr = adminapp.ErrRuntimeStop
	// runtimeStop 是运行收束失败结果。
	runtimeStop := invoke(endpoint, ctx, "admin_user_delete", map[string]any{"user_id": 2.0, "confirm": true})
	if !strings.Contains(resultJSON(t, runtimeStop), "停止账号运行实例失败") {
		t.Fatalf("运行收束失败映射异常: %s", resultJSON(t, runtimeStop))
	}
	// 缺少目标用户标识必须零触达。
	missing := invoke(endpoint, ctx, "admin_user_delete", map[string]any{"confirm": true})
	if !missing.IsError || fake.deleteCalls != 3 {
		t.Fatal("缺少用户标识必须零触达")
	}
}

// TestAdminBackgroundTasksOverview 验证进程后台任务总览的映射、条数上限与运行计数。
func TestAdminBackgroundTasksOverview(t *testing.T) {
	// fake 是含三类任务的假管理员端口。
	fake := &fakeAdminPorts{
		tasks: []capability.BackgroundTask{
			{ID: "t1", Name: "order_refresh", State: "running", StartedAtUnixMilli: 1700000000000},
			{ID: "t2", Name: "item_sync", State: "succeeded", StartedAtUnixMilli: 1700000001000, FinishedAtUnixMilli: 1700000002000},
			{ID: "t3", Name: "recovery", State: "failed", StartedAtUnixMilli: 1700000003000, FinishedAtUnixMilli: 1700000004000},
		},
	}
	// endpoint、ctx 是管理员工具端点与身份上下文。
	endpoint, ctx := newAdminToolEndpoint(t, fake)
	// result 是后台任务总览结果。
	result := invoke(endpoint, ctx, "admin_background_tasks", nil)
	if result.IsError {
		t.Fatalf("后台任务总览失败: %s", resultJSON(t, result))
	}
	// text 是总览 JSON，必须含运行计数、总条数与任务明细。
	text := resultJSON(t, result)
	// fragment 是后台任务总览响应中必须出现的片段。
	for _, fragment := range []string{`"total":3`, `"running":1`, `"task_id":"t1"`, `"state":"failed"`, `"finished_at":1700000002000`} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("后台任务总览缺少 %s: %s", fragment, text)
		}
	}
	// 受限条数时只返回前 N 条且运行计数按返回子集统计。
	limited := invoke(endpoint, ctx, "admin_background_tasks", map[string]any{"limit": 1.0})
	if !strings.Contains(resultJSON(t, limited), `"total":1`) {
		t.Fatalf("后台任务条数上限异常: %s", resultJSON(t, limited))
	}
	// 任务总览不含任务参数与错误正文。
	for _, forbidden := range []string{"error_message", "params", "arguments"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("后台任务总览不应包含 %s: %s", forbidden, text)
		}
	}
}

// TestAdminPortsMissingRegistration 验证管理员端口缺失时工具不注册。
func TestAdminPortsMissingRegistration(t *testing.T) {
	// empty、_ 是未注册管理员工具的端点。
	empty, _ := newProtocolEndpoint(t)
	empty.RegisterAdminTools(nil)
	// defs 是管理员端口缺失时注册到的工具清单，必须为空。
	if defs := empty.ToolDefs(); len(defs) != 0 {
		t.Fatalf("管理员端口缺失时不应注册工具，实际 %d 个", len(defs))
	}
	// fake 是管理员域假端口。
	fake := &fakeAdminPorts{}
	// endpoint、_ 是已注册管理员工具的端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterAdminTools(fake)
	// expected 是 Task 12 要求注册的工具名集合。
	expected := map[string]bool{"admin_stats": true, "admin_user_list": true, "admin_user_delete": true, "admin_background_tasks": true}
	// registered 是实际注册的工具名集合。
	registered := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		registered[def.Name] = true
		if def.Name == "admin_user_delete" && !def.Destructive {
			t.Fatal("删除用户必须标记破坏性")
		}
	}
	if len(registered) != len(expected) {
		t.Fatalf("管理员域工具数量异常: got=%d want=%d", len(registered), len(expected))
	}
	// name 是期望注册的工具名。
	for name := range expected {
		if !registered[name] {
			t.Fatalf("缺少工具 %s", name)
		}
	}
}

// TestAdminAndOwnedViewBoundary 验证普通归属视图与管理员全局视图的数据边界对照（TR-12.2）。
func TestAdminAndOwnedViewBoundary(t *testing.T) {
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	// accounts 是账号域假端口（归属视图）。
	accounts := &fakeAccountPorts{}
	// admin 是管理员域假端口（全局视图）。
	admin := &fakeAdminPorts{
		users: []adminapp.UserSummary{{ID: 7, Username: "other", CookieCount: 1}},
	}
	endpoint.RegisterAccountTools(accounts)
	endpoint.RegisterItemTools(accounts, &fakeItemPorts{})
	endpoint.RegisterAdminTools(admin)
	// ctx 是携带固定管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	// owned 是当前管理员归属的账号列表。
	owned := invoke(endpoint, ctx, "account_list", nil)
	if owned.IsError {
		t.Fatalf("归属账号列表失败: %s", resultJSON(t, owned))
	}
	// global 是跨用户的全局账号列表。
	global := invoke(endpoint, ctx, "account_list_all", nil)
	if global.IsError {
		t.Fatalf("全局账号列表失败: %s", resultJSON(t, global))
	}
	// 全局视图必须暴露归属用户字段，归属视图只返回自己的账号。
	if !strings.Contains(resultJSON(t, global), "other") {
		t.Fatalf("全局视图应返回跨用户归属信息: %s", resultJSON(t, global))
	}
	// 两个视图都必须不含凭证明文片段。
	for _, text := range []string{resultJSON(t, owned), resultJSON(t, global)} {
		if strings.Contains(text, "cookie_secret") || strings.Contains(text, "password") {
			t.Fatalf("账号视图不应包含凭证字段: %s", text)
		}
	}
	// 管理员全局工具与归属工具必须注册在同一端点且互不覆盖。
	// names 是全部已注册工具名。
	names := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		names[def.Name] = true
	}
	if !names["account_list"] || !names["account_list_all"] || !names["admin_user_list"] {
		t.Fatalf("归属与全局工具应同时注册: %v", names)
	}
	// 管理员端口缺失不影响归属工具可用。
	partial, _ := newProtocolEndpoint(t)
	partial.RegisterAccountTools(accounts)
	partial.RegisterAdminTools(nil)
	// partialNames 是部分装配下注册到的工具名集合。
	partialNames := map[string]bool{}
	// def 是部分装配下的当前工具定义。
	for _, def := range partial.ToolDefs() {
		partialNames[def.Name] = true
	}
	if partialNames["admin_stats"] || !partialNames["account_list"] {
		t.Fatalf("部分装配注册行为异常: %v", partialNames)
	}
}
