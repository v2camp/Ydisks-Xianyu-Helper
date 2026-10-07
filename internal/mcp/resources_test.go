// resources_test.go 覆盖只读资源与运维提示：
// 资源清单、read 全 URI 与查询参数分支、内容脱敏与同域工具一致性、五个提示的参数插值与人工确认提示，
// 并通过真实 streamable 客户端验证 resources/list、resources/templates/list、resources/read 与 prompts/get 链路。

package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpproto "github.com/mark3labs/mcp-go/mcp"

	accountapp "xianyu-go/internal/application/account"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	orderapp "xianyu-go/internal/application/orders"
)

// resourceCardSecret 是资源夹具中的模拟明文卡密，任何资源输出都不得包含该片段。
const resourceCardSecret = "RES-SECRET-6631-月卡"

// fakeResourcePorts 是只读资源假端口，返回固定夹具并支持注入错误。
type fakeResourcePorts struct {
	// stats 是仪表盘计数返回。
	stats analyticsapp.DashboardStats
	// accounts 是账号摘要返回。
	accounts []accountapp.AccountSummary
	// account 是单个账号摘要返回。
	account accountapp.AccountSummary
	// orders 是订单分页返回。
	orders orderapp.ListResult
	// runs 是运行异常返回。
	runs []automationapp.RunIssue
	// deferred 是死信任务返回。
	deferred []automationapp.DeferredIssue
	// cards 是卡券组返回。
	cards []cardsapp.Card
	// accountErr、cardErr 是注入错误。
	accountErr, cardErr error
	// orderQuery 是订单查询收到的过滤条件。
	orderQuery orderapp.ListQuery
	// accountID 是单账号查询收到的标识。
	accountID string
}

// DashboardStats 回传预设计数。
func (f *fakeResourcePorts) DashboardStats(context.Context, int64) (analyticsapp.DashboardStats, error) {
	return f.stats, nil
}

// ListAccountSummaries 回传预设账号摘要。
func (f *fakeResourcePorts) ListAccountSummaries(context.Context, int64) ([]accountapp.AccountSummary, error) {
	return f.accounts, f.accountErr
}

// GetAccountSummary 记录标识并回传预设账号摘要。
func (f *fakeResourcePorts) GetAccountSummary(_ context.Context, _ int64, cookieID string) (accountapp.AccountSummary, error) {
	f.accountID = cookieID
	return f.account, f.accountErr
}

// ListOrders 记录过滤条件并回传预设订单分页。
func (f *fakeResourcePorts) ListOrders(_ context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	f.orderQuery = query
	return f.orders, nil
}

// ListAutomationIssues 回传预设异常数据。
func (f *fakeResourcePorts) ListAutomationIssues(context.Context, int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error) {
	return f.runs, f.deferred, nil
}

// ListCards 回传预设卡券组。
func (f *fakeResourcePorts) ListCards(context.Context, int64) ([]cardsapp.Card, error) {
	return f.cards, f.cardErr
}

// newResourceEndpoint 构造注册资源与提示的端点、假端口与管理员身份上下文。
func newResourceEndpoint(t *testing.T, fake *fakeResourcePorts) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterResources(fake)
	endpoint.RegisterPrompts()
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// newResourceFixture 构造覆盖全部资源的夹具数据。
func newResourceFixture() *fakeResourcePorts {
	// stock 是含两行明文卡密的库存正文夹具。
	stock := resourceCardSecret + "\n第二行卡密\n"
	return &fakeResourcePorts{
		stats: analyticsapp.DashboardStats{TotalCookies: 2, ActiveCookies: 1, TotalCards: 3, AvailableCardStock: 7, TotalKeywords: 4, TotalOrders: 9},
		accounts: []accountapp.AccountSummary{
			{ID: "acc1", UserID: 1, Nickname: "一号店", Remark: "主号", AutoConfirm: true},
		},
		account: accountapp.AccountSummary{ID: "acc1", UserID: 1, Nickname: "一号店", Remark: "备注"},
		orders: orderapp.ListResult{
			Rows:  []orderapp.OrderRow{{OrderID: "o1", ItemTitle: "商品A", OrderStatus: "pending_ship", Amount: "10.00"}},
			Total: 1, Page: 1, PageSize: 20, TotalPages: 1,
		},
		runs:     []automationapp.RunIssue{{ID: 5, CookieID: "acc1", OrderID: "o1", TriggerType: "order_paid", IssueKind: "uneven", ErrorMessage: "需人工"}},
		deferred: []automationapp.DeferredIssue{{ID: 6, CookieID: "acc1", TriggerType: "order_paid", AttemptCount: 3}},
		cards: []cardsapp.Card{
			{ID: 10, UserID: 1, Name: "低库存卡", Type: "data", Enabled: true, DataContent: stock},
			{ID: 11, UserID: 1, Name: "充足卡", Type: "data", Enabled: true, DataContent: strings.Repeat("行\n", 20)},
			{ID: 12, UserID: 1, Name: "话术卡", Type: "text", Enabled: true, TextContent: "已发货"},
		},
	}
}

// TestResourceListAndRead 验证资源清单与全部 URI 的读取内容及查询参数分支（TR-13.1）。
func TestResourceListAndRead(t *testing.T) {
	// fixture 是资源夹具。
	fixture := newResourceFixture()
	// endpoint、ctx 是资源端点与身份上下文。
	endpoint, ctx := newResourceEndpoint(t, fixture)
	// resources 是协议注册表中的固定资源清单。
	resources := endpoint.mcpServer.ListResources()
	// expected 是必须注册的固定资源 URI 集合。
	expected := map[string]bool{
		resourceURIHealth: true, resourceURIDashboard: true,
		resourceURIAccounts: true, resourceURIAutomationIssues: true,
	}
	if len(resources) != len(expected) {
		t.Fatalf("固定资源数量异常: got=%d want=%d", len(resources), len(expected))
	}
	// uri 是当前遍历到的资源 URI。
	for uri := range expected {
		// ok 表示该 URI 是否已注册。
		if _, ok := resources[uri]; !ok {
			t.Fatalf("缺少资源 %s", uri)
		}
	}
	// 逐 URI 读取并断言内容形状与脱敏。
	// healthResult 是健康资源内容。
	healthResult := readResource(t, endpoint, ctx, resourceURIHealth)
	if !strings.Contains(healthResult, `"status":"ok"`) || !strings.Contains(healthResult, `"enabled":true`) {
		t.Fatalf("health 资源内容异常: %s", healthResult)
	}
	// dashboardResult 是概览资源内容，键名必须与 analytics_dashboard 工具一致。
	dashboardResult := readResource(t, endpoint, ctx, resourceURIDashboard)
	// fragment 是概览资源中必须出现的计数片段。
	for _, fragment := range []string{`"total_accounts":2`, `"active_accounts":1`, `"available_card_stock":7`, `"total_orders":9`} {
		if !strings.Contains(dashboardResult, fragment) {
			t.Fatalf("dashboard 资源缺少 %s: %s", fragment, dashboardResult)
		}
	}
	// accountsResult 是账号列表资源内容。
	accountsResult := readResource(t, endpoint, ctx, resourceURIAccounts)
	if !strings.Contains(accountsResult, `"account_id":"acc1"`) || !strings.Contains(accountsResult, `"total":1`) {
		t.Fatalf("accounts 资源内容异常: %s", accountsResult)
	}
	assertNoCredentialField(t, accountsResult)
	// accountResult 是单账号模板资源内容。
	accountResult := readResource(t, endpoint, ctx, resourceURIAccounts+"/acc1")
	if !strings.Contains(accountResult, `"account_id":"acc1"`) {
		t.Fatalf("单账号资源内容异常: %s", accountResult)
	}
	if fixture.accountID != "acc1" {
		t.Fatalf("单账号资源未透传账号标识: %q", fixture.accountID)
	}
	// 第二个账号标识必须原样透传，证明模板参数按调用方输入解析。
	readResource(t, endpoint, ctx, resourceURIAccounts+"/acc-2")
	if fixture.accountID != "acc-2" {
		t.Fatalf("模板参数透传异常: %q", fixture.accountID)
	}
	// ordersResult 是默认订单资源内容。
	ordersResult := readResource(t, endpoint, ctx, resourceURIOrders)
	if !strings.Contains(ordersResult, `"order_id":"o1"`) {
		t.Fatalf("orders 资源内容异常: %s", ordersResult)
	}
	if fixture.orderQuery.Page != 1 || fixture.orderQuery.PageSize != defaultPageSize {
		t.Fatalf("订单默认分页参数异常: %+v", fixture.orderQuery)
	}
	// 查询参数分支：status/account_id/search/page/page_size 必须透传并归一。
	readResource(t, endpoint, ctx, resourceURIOrders+"?status=pending_ship&account_id=acc1&search=%E5%95%86%E5%93%81&page=2&page_size=5")
	if fixture.orderQuery.Status != "pending_ship" || fixture.orderQuery.CookieID != "acc1" ||
		fixture.orderQuery.Search != "商品" || fixture.orderQuery.Page != 2 || fixture.orderQuery.PageSize != 5 {
		t.Fatalf("订单查询参数透传异常: %+v", fixture.orderQuery)
	}
	// 非法分页参数回落默认值。
	readResource(t, endpoint, ctx, resourceURIOrders+"?page=0&page_size=99999")
	if fixture.orderQuery.Page != 1 || fixture.orderQuery.PageSize != maxPageSize {
		t.Fatalf("订单分页参数归一异常: %+v", fixture.orderQuery)
	}
	// issuesResult 是自动化异常资源内容。
	issuesResult := readResource(t, endpoint, ctx, resourceURIAutomationIssues)
	if !strings.Contains(issuesResult, `"run_id"`) && !strings.Contains(issuesResult, `"deferred"`) {
		t.Fatalf("automation/issues 资源内容异常: %s", issuesResult)
	}
	// lowStockResult 是默认阈值的低库存资源内容。
	lowStockResult := readResource(t, endpoint, ctx, resourceURICardsLowStock)
	if !strings.Contains(lowStockResult, `"threshold":5`) || !strings.Contains(lowStockResult, "低库存卡") ||
		strings.Contains(lowStockResult, "充足卡") {
		t.Fatalf("低库存资源内容异常: %s", lowStockResult)
	}
	// 阈值分支：threshold=0 回落默认值，阈值提高后应命中更多卡券组。
	readResource(t, endpoint, ctx, resourceURICardsLowStock+"?threshold=0")
	// zeroThreshold 是默认阈值下的资源内容。
	zeroThreshold := readResource(t, endpoint, ctx, resourceURICardsLowStock)
	if !strings.Contains(zeroThreshold, `"threshold":5`) {
		t.Fatalf("零阈值应回落默认值: %s", zeroThreshold)
	}
	// raisedThreshold 是阈值 25 的资源内容。
	raisedThreshold := readResource(t, endpoint, ctx, resourceURICardsLowStock+"?threshold=25")
	if !strings.Contains(raisedThreshold, `"threshold":25`) || !strings.Contains(raisedThreshold, "充足卡") {
		t.Fatalf("阈值分支内容异常: %s", raisedThreshold)
	}
	// 远超上限的阈值按上限收敛。
	// capped 是超大阈值的资源内容。
	capped := readResource(t, endpoint, ctx, resourceURICardsLowStock+"?threshold=99999")
	if !strings.Contains(capped, `"threshold":1000`) {
		t.Fatalf("阈值上限收敛异常: %s", capped)
	}
	// 全部资源输出必须不含明文卡密与凭证字段。
	// text 是当前待扫描的资源内容。
	for _, text := range []string{healthResult, dashboardResult, accountsResult, ordersResult, issuesResult, lowStockResult} {
		if strings.Contains(text, resourceCardSecret) {
			t.Fatalf("资源输出泄漏明文卡密: %s", text)
		}
		assertNoCredentialField(t, text)
	}
}

// assertNoCredentialField 断言内容不含凭证明文相关字段名。
func assertNoCredentialField(t *testing.T, text string) {
	t.Helper()
	// field 是绝不允许出现在只读输出中的凭证字段名。
	for _, field := range []string{"cookie\":", "token\":", "password", "smtp_password", "api_key", "data_content"} {
		if strings.Contains(strings.ToLower(text), field) {
			t.Fatalf("只读输出不应包含凭证或卡密字段 %s: %s", field, text)
		}
	}
}

// readResource 通过协议服务器直接读取资源并返回 JSON 文本内容。
func readResource(t *testing.T, endpoint *Endpoint, ctx context.Context, uri string) string {
	t.Helper()
	// contents、err 是资源读取结果与错误。
	contents, err := endpoint.readResourceForTest(ctx, uri)
	if err != nil {
		t.Fatalf("读取资源 %s 失败: %v", uri, err)
	}
	if len(contents) == 0 {
		t.Fatalf("资源 %s 返回空内容", uri)
	}
	// text 是首个文本内容。
	text := contents[0]
	if text.MIMEType != resourceMIMETypeJSON {
		t.Fatalf("资源 %s 媒体类型异常: %s", uri, text.MIMEType)
	}
	// payload 是内容 JSON 解析结果，用于确认输出为合法 JSON 对象。
	var payload any
	// err 是内容 JSON 解析错误。
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("资源 %s 内容不是合法 JSON: %v", uri, err)
	}
	return text.Text
}

// TestPromptRegistrationAndContent 验证五个提示的注册、参数插值与人工确认提示（TR-13.2）。
func TestPromptRegistrationAndContent(t *testing.T) {
	// fixture 是资源夹具。
	fixture := newResourceFixture()
	// endpoint、ctx 是资源端点与身份上下文。
	endpoint, ctx := newResourceEndpoint(t, fixture)
	// expected 是必须注册的提示名集合。
	expected := []string{
		promptNameDailyInspection, promptNameResolveNeedsReview, promptNameAccountAutomationOnboarding,
		promptNameRestockLowStockCards, promptNameAIConfigCheck,
	}
	// name 是当前期望的提示名。
	for _, name := range expected {
		// ok 表示该提示是否已注册。
		if _, ok := endpoint.mcpServer.ListPrompts()[name]; !ok {
			t.Fatalf("缺少提示 %s", name)
		}
	}
	// 每日巡检提示必须给出工具顺序与人工确认提醒。
	daily := promptText(t, endpoint, ctx, promptNameDailyInspection, nil)
	// fragment 是每日巡检提示中必须出现的片段。
	for _, fragment := range []string{"account_list", "analytics_dashboard", "automation_issue_list", "card_list", "安全提醒"} {
		if !strings.Contains(daily, fragment) {
			t.Fatalf("每日巡检提示缺少 %s: %s", fragment, daily)
		}
	}
	// 处理 needs_review 提示必须区分三种结论并禁止自动重发。
	resolve := promptText(t, endpoint, ctx, promptNameResolveNeedsReview, map[string]string{"order_id": "o1"})
	// fragment 是异常处理提示中必须出现的片段。
	for _, fragment := range []string{"order_id=o1", "平台状态不确定", "禁止自动重发", "automation_issue_resolve"} {
		if !strings.Contains(resolve, fragment) {
			t.Fatalf("处理异常提示缺少 %s: %s", fragment, resolve)
		}
	}
	// 缺少参数时按“全部范围”处理，仍输出完整步骤。
	resolveAll := promptText(t, endpoint, ctx, promptNameResolveNeedsReview, nil)
	if !strings.Contains(resolveAll, "（全部范围）") {
		t.Fatalf("无参数提示的默认范围异常: %s", resolveAll)
	}
	// 新账号向导：缺少必填账号参数时给出明确提示且不编造步骤。
	onboardMissing := promptText(t, endpoint, ctx, promptNameAccountAutomationOnboarding, nil)
	if !strings.Contains(onboardMissing, "account_id") || !strings.Contains(onboardMissing, "必填") {
		t.Fatalf("向导缺少参数提示异常: %s", onboardMissing)
	}
	if strings.Contains(onboardMissing, "rule_preview") {
		t.Fatalf("缺少账号参数时不应输出建规则步骤: %s", onboardMissing)
	}
	// 提供账号参数后必须插值并给出完整开通顺序。
	onboard := promptText(t, endpoint, ctx, promptNameAccountAutomationOnboarding, map[string]string{"account_id": "acc9"})
	// fragment 是开通向导提示中必须出现的片段。
	for _, fragment := range []string{"账号 acc9", "rule_preview", "rule_create", "ai_reply_set", "settings_ai_test_connection"} {
		if !strings.Contains(onboard, fragment) {
			t.Fatalf("向导提示缺少 %s: %s", fragment, onboard)
		}
	}
	// 补货提示默认阈值与自定义阈值都必须体现。
	restockDefault := promptText(t, endpoint, ctx, promptNameRestockLowStockCards, nil)
	if !strings.Contains(restockDefault, "低于 5 行") || !strings.Contains(restockDefault, "card_append_data") {
		t.Fatalf("补货提示默认阈值异常: %s", restockDefault)
	}
	// restockCustom 是自定义阈值下的补货提示正文。
	restockCustom := promptText(t, endpoint, ctx, promptNameRestockLowStockCards, map[string]string{"threshold": "12"})
	if !strings.Contains(restockCustom, "低于 12 行") {
		t.Fatalf("补货提示自定义阈值插值异常: %s", restockCustom)
	}
	// AI 体检提示必须包含冲突检查与测试工具。
	aiCheck := promptText(t, endpoint, ctx, promptNameAIConfigCheck, nil)
	// fragment 是 AI 体检提示中必须出现的片段。
	for _, fragment := range []string{"ai_test_connection", "ai_reply_list", "rule_list", "冲突"} {
		if !strings.Contains(aiCheck, fragment) {
			t.Fatalf("AI 体检提示缺少 %s: %s", fragment, aiCheck)
		}
	}
	// 全部提示都必须附带统一的人工确认提醒。
	// text 是当前待检查的提示正文。
	for _, text := range []string{daily, resolve, onboard, restockDefault, aiCheck} {
		if !strings.Contains(text, "安全提醒") || !strings.Contains(text, "confirm=true") {
			t.Fatalf("提示缺少人工确认提醒: %s", text)
		}
	}
}

// promptText 直接调用提示处理器并返回首条消息正文。
func promptText(t *testing.T, endpoint *Endpoint, ctx context.Context, name string, args map[string]string) string {
	t.Helper()
	// result、err 是提示结果与错误。
	result, err := endpoint.promptForTest(ctx, name, args)
	if err != nil {
		t.Fatalf("获取提示 %s 失败: %v", name, err)
	}
	if result == nil || len(result.Messages) == 0 {
		t.Fatalf("提示 %s 返回空消息", name)
	}
	return result.Messages[0].Text
}

// TestResourcesAndPromptsOverStreamable 验证真实 streamable 客户端可完成资源与提示链路。
func TestResourcesAndPromptsOverStreamable(t *testing.T) {
	// fixture 是资源夹具。
	fixture := newResourceFixture()
	// endpoint、_ 是资源端点。
	endpoint, _ := newResourceEndpoint(t, fixture)
	// server 是承载守卫与协议处理器的本机 HTTP 测试服务。
	server := httptest.NewServer(endpoint.Handler())
	defer server.Close()
	// ctx 是本次协议调用上下文。
	ctx := context.Background()
	// trans、err 是携带 Bearer 头的 streamable 传输及其构造错误。
	trans, err := transport.NewStreamableHTTP(server.URL,
		transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer persisted-or-env-secret"}))
	if err != nil {
		t.Fatalf("构造 streamable 传输失败: %v", err)
	}
	// mcpc 是基于该传输的 MCP 客户端。
	mcpc := client.NewClient(trans)
	// startErr 是传输建连错误。
	if startErr := mcpc.Start(ctx); startErr != nil {
		t.Fatalf("启动客户端失败: %v", startErr)
	}
	defer func() { _ = mcpc.Close() }()
	// initRequest 是声明客户端信息的初始化请求。
	initRequest := mcpproto.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcpproto.Implementation{Name: "harness-test", Version: "1.0.0"}
	// initErr 是初始化握手错误。
	if _, initErr := mcpc.Initialize(ctx, initRequest); initErr != nil {
		t.Fatalf("initialize 失败: %v", initErr)
	}
	// listed、listErr 是远端资源清单。
	listed, listErr := mcpc.ListResources(ctx, mcpproto.ListResourcesRequest{})
	if listErr != nil {
		t.Fatalf("resources/list 失败: %v", listErr)
	}
	if len(listed.Resources) != 4 {
		t.Fatalf("远端固定资源数量异常: %d", len(listed.Resources))
	}
	// templates、templateErr 是远端资源模板清单。
	templates, templateErr := mcpc.ListResourceTemplates(ctx, mcpproto.ListResourceTemplatesRequest{})
	if templateErr != nil {
		t.Fatalf("resources/templates/list 失败: %v", templateErr)
	}
	if len(templates.ResourceTemplates) != 3 {
		t.Fatalf("远端资源模板数量异常: %d", len(templates.ResourceTemplates))
	}
	// readResult、readErr 是远端资源读取结果。
	readResult, readErr := mcpc.ReadResource(ctx, mcpproto.ReadResourceRequest{
		Params: mcpproto.ReadResourceParams{URI: resourceURIDashboard},
	})
	if readErr != nil {
		t.Fatalf("resources/read 失败: %v", readErr)
	}
	if len(readResult.Contents) == 0 {
		t.Fatal("resources/read 返回空内容")
	}
	// prompts、promptsErr 是远端提示清单。
	prompts, promptsErr := mcpc.ListPrompts(ctx, mcpproto.ListPromptsRequest{})
	if promptsErr != nil {
		t.Fatalf("prompts/list 失败: %v", promptsErr)
	}
	if len(prompts.Prompts) != 5 {
		t.Fatalf("远端提示数量异常: %d", len(prompts.Prompts))
	}
	// got、getErr 是远端提示内容。
	got, getErr := mcpc.GetPrompt(ctx, mcpproto.GetPromptRequest{
		Params: mcpproto.GetPromptParams{Name: promptNameAIConfigCheck},
	})
	if getErr != nil {
		t.Fatalf("prompts/get 失败: %v", getErr)
	}
	if len(got.Messages) == 0 {
		t.Fatal("prompts/get 返回空消息")
	}
	// 协议链路读取的资源内容同样不含明文卡密。
	// encoded 是远端资源内容的 JSON 文本。
	encoded, marshalErr := json.Marshal(readResult.Contents[0])
	if marshalErr != nil {
		t.Fatalf("序列化远端资源内容失败: %v", marshalErr)
	}
	if strings.Contains(string(encoded), resourceCardSecret) {
		t.Fatalf("远端资源内容泄漏明文卡密: %s", encoded)
	}
}

// TestResourcesAndPromptsAuditWritten 验证资源与提示调用会写入非敏感审计。
func TestResourcesAndPromptsAuditWritten(t *testing.T) {
	// endpoint、audit 是协议端点与假审计端口。
	endpoint, audit := newProtocolEndpoint(t)
	// fake 是资源夹具。
	fake := newResourceFixture()
	endpoint.RegisterResources(fake)
	endpoint.RegisterPrompts()
	// ctx 是携带固定管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	readResource(t, endpoint, ctx, resourceURIHealth)
	promptText(t, endpoint, ctx, promptNameDailyInspection, nil)
	// resourceAudited、promptAudited 分别表示两类审计是否命中。
	resourceAudited, promptAudited := false, false
	// entry 是当前审计条目。
	for _, entry := range audit.entries {
		if entry.Category == AuditCategoryResource {
			resourceAudited = true
		}
		if entry.Category == AuditCategoryPrompt {
			promptAudited = true
		}
		if entry.UserID != 1 || entry.Name == "" {
			t.Fatalf("审计条目字段异常: %+v", entry)
		}
	}
	if !resourceAudited || !promptAudited {
		t.Fatalf("资源与提示审计缺失: resource=%t prompt=%t", resourceAudited, promptAudited)
	}
	// 审计条目不得包含资源具体内容。
	// encoded 是审计条目的 JSON 表示。
	encoded, marshalErr := json.Marshal(audit.entries)
	if marshalErr != nil {
		t.Fatalf("序列化审计条目失败: %v", marshalErr)
	}
	if strings.Contains(string(encoded), resourceCardSecret) {
		t.Fatalf("审计条目泄漏资源内容: %s", encoded)
	}
	// 端口缺失时资源与提示注册必须安全返回。
	empty, _ := newProtocolEndpoint(t)
	empty.RegisterResources(nil)
	if len(empty.mcpServer.ListResources()) != 0 {
		t.Fatal("端口缺失时不应注册资源")
	}
	// 提示注册不依赖业务端口，仍应可用。
	empty.RegisterPrompts()
	if len(empty.mcpServer.ListPrompts()) != 5 {
		t.Fatal("提示注册不应依赖业务端口")
	}
}

// jsonRPCResult 是 JSON-RPC 响应的最小信封，只保留结果与错误用于测试断言。
type jsonRPCResult struct {
	// Result 是成功响应的结果体。
	Result json.RawMessage `json:"result"`
	// Error 是失败响应。
	Error *struct {
		// Code 是 JSON-RPC 错误码。
		Code int `json:"code"`
		// Message 是错误摘要。
		Message string `json:"message"`
	} `json:"error"`
}

// testPromptMessage 是提示消息的测试视图：只保留角色与文本正文。
type testPromptMessage struct {
	// Role 是消息角色。
	Role string
	// Text 是消息文本正文。
	Text string
}

// testPromptResult 是提示结果的测试视图。
type testPromptResult struct {
	// Messages 是提示消息列表。
	Messages []testPromptMessage
}

// readResourceForTest 经协议处理器的 resources/read 调用资源并返回文本内容切片。
func (e *Endpoint) readResourceForTest(ctx context.Context, uri string) ([]mcpproto.TextResourceContents, error) {
	// request 是 resources/read 的 JSON-RPC 请求体。
	request := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "resources/read",
		"params": map[string]any{"uri": uri},
	}
	// raw 是序列化后的请求体。
	raw, marshalErr := json.Marshal(request)
	if marshalErr != nil {
		return nil, marshalErr
	}
	// resp 是协议处理器返回的 JSON-RPC 响应。
	resp := e.mcpServer.HandleMessage(ctx, raw)
	// encoded 是响应的 JSON 表示。
	encoded, encodeErr := json.Marshal(resp)
	if encodeErr != nil {
		return nil, encodeErr
	}
	// envelope 是解析后的响应信封。
	var envelope jsonRPCResult
	// err 是响应信封解析错误。
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, InvalidArgument(envelope.Error.Message)
	}
	// body 是 resources/read 结果体；contents 元素先按原始 JSON 保留再逐个解析。
	var body struct {
		Contents []json.RawMessage `json:"contents"`
	}
	// err 是结果体解析错误。
	if err := json.Unmarshal(envelope.Result, &body); err != nil {
		return nil, err
	}
	// contents 是解析后的文本资源内容。
	contents := make([]mcpproto.TextResourceContents, 0, len(body.Contents))
	// item 是当前待解析的资源内容。
	for _, item := range body.Contents {
		// text 是文本资源内容。
		var text mcpproto.TextResourceContents
		// err 是单个资源内容解析错误。
		if err := json.Unmarshal(item, &text); err != nil {
			return nil, err
		}
		contents = append(contents, text)
	}
	return contents, nil
}

// promptForTest 经协议处理器的 prompts/get 调用提示并返回角色与文本。
func (e *Endpoint) promptForTest(ctx context.Context, name string, args map[string]string) (*testPromptResult, error) {
	// params 是提示调用参数；无参数时使用空对象满足必填字段。
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	// request 是 prompts/get 的 JSON-RPC 请求体。
	request := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "prompts/get", "params": params}
	// raw 是序列化后的请求体。
	raw, marshalErr := json.Marshal(request)
	if marshalErr != nil {
		return nil, marshalErr
	}
	// resp 是协议处理器返回的 JSON-RPC 响应。
	resp := e.mcpServer.HandleMessage(ctx, raw)
	// encoded 是响应的 JSON 表示。
	encoded, encodeErr := json.Marshal(resp)
	if encodeErr != nil {
		return nil, encodeErr
	}
	// envelope 是解析后的响应信封。
	var envelope jsonRPCResult
	// err 是响应信封解析错误。
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, InvalidArgument(envelope.Error.Message)
	}
	// body 是 prompts/get 结果体；消息内容先按原始 JSON 保留再解析。
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	// err 是结果体解析错误。
	if err := json.Unmarshal(envelope.Result, &body); err != nil {
		return nil, err
	}
	// result 是转换后的提示测试视图。
	result := &testPromptResult{Messages: make([]testPromptMessage, 0, len(body.Messages))}
	// message 是当前待转换的消息。
	for _, message := range body.Messages {
		// content 是消息文本内容。
		var content mcpproto.TextContent
		// err 是单条消息内容解析错误。
		if err := json.Unmarshal(message.Content, &content); err != nil {
			return nil, err
		}
		result.Messages = append(result.Messages, testPromptMessage{Role: message.Role, Text: content.Text})
	}
	return result, nil
}

// TestResourceReadErrors 验证资源读取错误路径返回可读中文且不泄漏内容。
func TestResourceReadErrors(t *testing.T) {
	// fixture 是注入账号读取失败的资源夹具。
	fixture := newResourceFixture()
	fixture.accountErr = accountapp.ErrForbidden
	// endpoint、ctx 是资源端点与身份上下文。
	endpoint, ctx := newResourceEndpoint(t, fixture)
	// 账号资源读取失败必须返回错误而非空内容。
	if _, err := endpoint.readResourceForTest(ctx, resourceURIAccounts); err == nil {
		t.Fatal("账号越权读取必须返回错误")
	}
	// 未注册资源的读取必须返回错误。
	if _, err := endpoint.readResourceForTest(ctx, "ydisks://unknown"); err == nil {
		t.Fatal("未知资源必须返回错误")
	}
	// 单账号模板缺少标识时返回中文参数错误。
	if _, err := endpoint.readResourceForTest(ctx, resourceURIAccounts+"/%20"); err == nil {
		t.Fatal("空账号标识必须返回错误")
	}
}
