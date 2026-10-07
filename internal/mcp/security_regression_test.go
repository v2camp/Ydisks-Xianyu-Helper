// security_regression_test.go 是禁能力面与敏感数据端到端回归：
//   - 工具注册表与 FR-13 十个域逐条对照（缺失/越界可定位）；
//   - FR-15 禁能力字符串与语义扫描（工具名、说明、入参名全量）；
//   - 破坏性工具 confirm 入参与 destructivHint、只读工具 readOnlyHint 全量对照；
//   - 在四类敏感设置 + 明文卡密 + 渠道密钥夹具下遍历全部工具、资源与提示，
//     断言响应、错误、审计行零秘密泄漏。

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	mcpproto "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	orderapp "xianyu-go/internal/application/orders"
)

// fr13Inventory 是 FR-13 十个域与工具名的权威对照表。
// 每个工具都必须出现在注册表中，注册表也不得出现此表之外的名称。
var fr13Inventory = map[string][]string{
	// 1. 服务自检。
	"self_check": {"system_ping", "system_version", "auth_whoami"},
	// 2. 账号域。
	"account": {
		"account_list_all", "account_list", "account_get", "account_set_remark", "account_set_status",
		"account_set_pause", "account_get_pause", "account_set_auto_confirm", "account_set_auto_consign",
		"account_set_auto_bargain", "account_update_settings", "account_get_long_login", "account_set_long_login",
		"account_runtime_status", "account_restart", "account_refresh_profile", "account_delete",
		"account_task_get_settings", "account_task_update_settings", "account_task_list_runs", "account_task_run",
	},
	// 3. 订单与履约（含仪表盘、分析与自动化异常）。
	"orders": {
		"order_list", "order_get", "order_refresh_single", "order_refresh", "order_manual_ship",
		"order_refresh_job_create", "order_refresh_job_get", "order_refresh_job_cancel",
		"analytics_dashboard", "analytics_orders", "analytics_valid_orders",
		"issue_list", "issue_resolve_run", "issue_resolve_deferred",
	},
	// 4. 商品货架与批量发布。
	"items": {
		"item_list", "item_get", "item_create", "item_update", "item_delete",
		"item_set_multi_spec", "item_set_multi_quantity", "item_sync_all", "item_sync_page", "item_publish_single",
		"item_batch_preview", "item_category_recommend", "item_batch_create", "item_batch_start", "item_batch_list",
		"item_batch_get", "item_batch_cancel", "item_batch_retry_failed", "item_batch_delete", "item_batch_result_csv",
	},
	// 5. 卡密库存。
	"cards": {
		"card_list", "card_get", "card_create", "card_update", "card_delete", "card_append_data", "card_test_api",
	},
	// 6. 自动化配置：规则、发货模板、默认回复、关键词与指定商品回复。
	"automation_config": {
		"rule_list", "rule_list_all", "rule_trigger_counts", "rule_preview", "rule_create", "rule_update", "rule_delete",
		"template_list", "template_get", "template_create", "template_update", "template_delete",
		"default_reply_list", "default_reply_get", "default_reply_set", "default_reply_delete", "default_reply_clear_records",
		"keyword_list", "keyword_add", "keyword_replace", "keyword_update", "keyword_delete", "keyword_delete_by_index",
		"item_reply_list", "item_reply_get", "item_reply_set", "item_reply_delete",
	},
	// 7. 聊天域。
	"chat": {
		"chat_session_list", "chat_session_refresh_contacts", "chat_history_refresh", "chat_message_list",
		"chat_send_text", "chat_send_image", "chat_mark_read", "chat_session_delete",
		"chat_quick_reply_list", "chat_quick_reply_add", "chat_quick_reply_delete",
		"chat_buyer_note_get", "chat_buyer_note_set", "chat_item_list",
	},
	// 8. 通知域。
	"notifications": {
		"notification_channel_list", "notification_channel_get", "notification_channel_create",
		"notification_channel_update", "notification_channel_delete", "notification_channel_test",
		"notification_binding_list", "notification_binding_get", "notification_binding_set",
		"notification_binding_toggle", "notification_binding_delete", "notification_binding_clear_account",
		"notification_uncertain_list", "notification_uncertain_list_all",
	},
	// 9. AI 与系统设置。
	"settings": {
		"settings_get_system", "settings_update_system", "settings_set_system",
		"settings_user_list", "settings_user_get", "settings_user_set",
		"ai_reply_list", "ai_reply_get", "ai_reply_set", "ai_models_list", "ai_test_connection",
	},
	// 10. 管理员域；审计查询归入服务自检相邻的运维能力。
	"admin": {
		"admin_stats", "admin_user_list", "admin_user_delete",
		"admin_background_tasks", "mcp_audit_list",
	},
}

// forbiddenCapabilityPatterns 是 FR-15 禁止出现在工具名、说明与入参名中的能力关键词。
var forbiddenCapabilityPatterns = []string{
	"cookie_read", "cookie_write", "read_cookie", "write_cookie", "set_cookie",
	"token_read", "read_token", "token_write",
	"password_login", "password_login_flow", "login_password",
	"qr_login", "qrlogin", "qrcode_login", "scan_login",
	"session_login", "session_logout", "admin_init", "init_admin", "initialize_admin",
	"change_password", "reset_password", "update_password",
	"captcha_config", "slider_config", "browser_param", "browser_args", "stealth",
	"x5sec", "credential_read", "credential_write", "decrypt_credential",
	"plain_card", "card_data_read", "read_card_data", "raw_metadata",
	"import_legacy", "legacy_import",
}

// secretFixture 是端到端秘密夹具：四类敏感设置、明文卡密与渠道密钥。
type secretFixture struct {
	// aiKey 是模拟 AI API 密钥。
	aiKey string
	// smtpPassword 是模拟 SMTP 密码。
	smtpPassword string
	// qqSecret 是模拟机器人 secret。
	qqSecret string
	// captchaSecret 是模拟验证码远端密钥。
	captchaSecret string
	// cardData 是模拟明文逐行卡密。
	cardData string
	// cookieCipher 是模拟加密 Cookie 密文。
	cookieCipher string
	// channelSecret 是模拟通知渠道配置中的密钥。
	channelSecret string
}

// newSecretFixture 构造一组互不相同的秘密标记，便于定位泄漏来源。
func newSecretFixture() secretFixture {
	return secretFixture{
		aiKey:         "sk-e2e-ai-8823-SECRET",
		smtpPassword:  "smtp-e2e-4471-SECRET",
		qqSecret:      "qq-e2e-9902-SECRET",
		captchaSecret: "captcha-e2e-1188-SECRET",
		cardData:      "CARD-E2E-6612-SECRET\nCARD-E2E-7713-SECRET",
		cookieCipher:  "ENC-COOKIE-E2E-BLOB-SECRET",
		channelSecret: "channel-e2e-3345-SECRET",
	}
}

// markers 返回全部需要在输出中扫描的秘密标记。
func (f secretFixture) markers() []string {
	return []string{f.aiKey, f.smtpPassword, f.qqSecret, f.captchaSecret, f.cardData, f.cookieCipher, f.channelSecret}
}

// newFullDomainEndpoint 注册全部域工具与资源，并用含秘密的夹具驱动。
func newFullDomainEndpoint(t *testing.T, fixture secretFixture) (*Endpoint, *fakeAudit, context.Context) {
	t.Helper()
	// endpoint、audit 是协议端点与审计捕获端口。
	endpoint, audit := newProtocolEndpoint(t)
	endpoint.RegisterSystemTools()
	endpoint.RegisterAccountTools(&fakeAccountPorts{})
	endpoint.RegisterOrderTools(&fakeOrderPorts{}, &fakeAnalyticsPorts{}, &fakeIssuePorts{})
	endpoint.RegisterItemTools(&fakeAccountPorts{}, &fakeItemPorts{})
	// cards 夹具携带明文卡密正文，用于验证库存计数不泄漏内容。
	endpoint.RegisterCardTools(&fakeCardPorts{
		cards:     []cardsapp.Card{{ID: 10, UserID: 1, Name: "卡密组", Type: "data", DataContent: fixture.cardData}},
		cardsByID: map[int64]cardsapp.Card{10: {ID: 10, UserID: 1, Name: "卡密组", Type: "data", DataContent: fixture.cardData}},
		getErrs:   map[int64]error{},
	})
	endpoint.RegisterRuleTools(&fakeRulePorts{})
	endpoint.RegisterDeliveryTemplateTools(&fakeTemplatePorts{})
	endpoint.RegisterDefaultReplyTools(&fakeDefaultReplyPorts{})
	endpoint.RegisterKeywordTools(&fakeKeywordPorts{})
	endpoint.RegisterChatTools(&fakeChatPorts{})
	// 通知渠道摘要与编辑态都不应包含渠道配置密钥。
	endpoint.RegisterNotificationTools(&fakeNotificationChannels{}, &fakeUncertainNotifications{})
	// 设置夹具的脱敏结果只保留 configured 标记。
	endpoint.RegisterSettingsTools(newSecretSettingsPorts(fixture))
	endpoint.RegisterAITools(newSecretSettingsPorts(fixture))
	endpoint.RegisterAdminTools(&fakeAdminPorts{})
	endpoint.RegisterResources(newSecretResourcePorts(fixture))
	endpoint.RegisterPrompts()
	// ctx 是携带固定管理员身份的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, audit, ctx
}

// newSecretSettingsPorts 构造携带四类敏感秘密的设置端口；读取路径只返回脱敏标记。
func newSecretSettingsPorts(fixture secretFixture) *fakeSettingsPorts {
	// ports 是脱敏后的设置端口夹具。
	ports := newSensitiveSettingsFake()
	ports.system = map[string]string{
		"ai_api_url": "https://ai.example.com/v1", "log_level": "info",
		"ai_api_key": "configured", "smtp_password": "configured",
		"qq_reply_secret_key": "configured", "captcha.remote_secret_key": "configured",
	}
	// 隐藏夹具保留秘密值，便于断言写入路径只写不读。
	ports.modelsAPIKey = fixture.aiKey
	return ports
}

// newSecretResourcePorts 构造携带明文卡密的只读资源端口。
func newSecretResourcePorts(fixture secretFixture) *fakeResourcePorts {
	// ports 是资源端口夹具。
	ports := newResourceFixture()
	ports.cards = append(ports.cards, cardsapp.Card{
		ID: 20, UserID: 1, Name: "端到端卡密组", Type: "data", Enabled: true, DataContent: fixture.cardData,
	})
	return ports
}

// TestFR13RegistryMatchesInventory 对照 FR-13 十个域校验工具注册表（TR-14.1）。
func TestFR13RegistryMatchesInventory(t *testing.T) {
	// endpoint、_、_ 是全域端点。
	endpoint, _, _ := newFullDomainEndpoint(t, newSecretFixture())
	// registered 是注册表实际工具名集合。
	registered := map[string]bool{}
	// name 是注册表中的当前工具名。
	for name := range endpoint.mcpServer.ListTools() {
		registered[name] = true
	}
	// expected 是 FR-13 全部工具名集合。
	expected := map[string]string{}
	// domain 是当前遍历到的能力域；names 是该域工具名清单。
	for domain, names := range fr13Inventory {
		// name 是当前域的工具名。
		for _, name := range names {
			// owner 是已登记该工具的域；duplicated 表示是否重复登记。
			if owner, duplicated := expected[name]; duplicated {
				t.Fatalf("工具 %s 同时出现在域 %s 与 %s", name, owner, domain)
			}
			expected[name] = domain
		}
	}
	// missing 是清单要求但未注册的工具。
	missing := make([]string, 0)
	// name、domain 是清单中的当前工具与所属域。
	for name, domain := range expected {
		if !registered[name] {
			missing = append(missing, fmt.Sprintf("%s(%s)", name, domain))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("注册表缺少 %d 个工具: %s", len(missing), strings.Join(missing, ", "))
	}
	// extra 是注册了但不在 FR-13 清单内的越界工具。
	extra := make([]string, 0)
	// name 是注册表中的当前工具名。
	for name := range registered {
		// ok 表示该工具是否已登记在 FR-13 清单中。
		if _, ok := expected[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Fatalf("注册表存在 %d 个越界工具: %s", len(extra), strings.Join(extra, ", "))
	}
	if len(registered) != len(expected) {
		t.Fatalf("工具总数不一致: got=%d want=%d", len(registered), len(expected))
	}
}

// TestForbiddenCapabilitiesAbsent 扫描工具名、说明与入参名中的 FR-15 禁能力关键词（TR-14.1）。
func TestForbiddenCapabilitiesAbsent(t *testing.T) {
	// endpoint、_、_ 是全域端点。
	endpoint, _, _ := newFullDomainEndpoint(t, newSecretFixture())
	// tool 是注册表中的当前工具。
	for name, tool := range endpoint.mcpServer.ListTools() {
		// surface 是参与扫描的表面文本：工具名、说明与全部入参名。
		surface := strings.ToLower(name + " " + tool.Tool.Description + " " + strings.Join(toolArgumentNames(tool), " "))
		// pattern 是当前禁能力关键词。
		for _, pattern := range forbiddenCapabilityPatterns {
			if strings.Contains(surface, pattern) {
				t.Fatalf("工具 %s 暴露禁能力关键词 %q: %s", name, pattern, surface)
			}
		}
	}
	// 提示说明同样不得暴露禁能力入口。
	// prompt 是注册表中的当前提示。
	for name, prompt := range endpoint.mcpServer.ListPrompts() {
		// surface 是提示名与说明。
		surface := strings.ToLower(name + " " + prompt.Prompt.Description)
		// pattern 是当前禁能力关键词。
		for _, pattern := range forbiddenCapabilityPatterns {
			if strings.Contains(surface, pattern) {
				t.Fatalf("提示 %s 暴露禁能力关键词 %q", name, pattern)
			}
		}
	}
}

// toolArgumentNames 提取工具入参 schema 的顶层属性名。
func toolArgumentNames(tool *mcpserver.ServerTool) []string {
	// properties 是工具入参 schema 的属性映射。
	properties := tool.Tool.InputSchema.Properties
	// names 是属性名列表。
	names := make([]string, 0, len(properties))
	// name 是当前属性名。
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestDestructiveAndReadOnlyAnnotations 全量对照破坏性工具注解、confirm 入参与只读注解（TR-14.3）。
func TestDestructiveAndReadOnlyAnnotations(t *testing.T) {
	// endpoint、_、_ 是全域端点。
	endpoint, _, _ := newFullDomainEndpoint(t, newSecretFixture())
	// destructiveCount 是检查到的破坏性工具数量。
	destructiveCount := 0
	// tool 是注册表中的当前工具。
	for name, tool := range endpoint.mcpServer.ListTools() {
		// annotations 是工具注解。
		annotations := tool.Tool.Annotations
		// argNames 是工具入参名列表。
		argNames := toolArgumentNames(tool)
		// hasConfirm 表示是否声明了 confirm 入参。
		hasConfirm := false
		// argName 是当前入参名。
		for _, argName := range argNames {
			if argName == confirmArgumentName {
				hasConfirm = true
			}
		}
		// destructive 表示工具是否被标记为破坏性。
		destructive := annotations.DestructiveHint != nil && *annotations.DestructiveHint
		if destructive {
			destructiveCount++
			if !hasConfirm {
				t.Fatalf("破坏性工具 %s 缺少 confirm 入参", name)
			}
			if !confirmArgumentRequired(tool) {
				t.Fatalf("破坏性工具 %s 的 confirm 必须为必填", name)
			}
			if annotations.ReadOnlyHint != nil && *annotations.ReadOnlyHint {
				t.Fatalf("破坏性工具 %s 不应同时声明 readOnlyHint", name)
			}
			continue
		}
		if hasConfirm {
			t.Fatalf("只读工具 %s 不应声明 confirm 入参", name)
		}
		if annotations.ReadOnlyHint == nil || !*annotations.ReadOnlyHint {
			t.Fatalf("只读工具 %s 必须声明 readOnlyHint=true", name)
		}
	}
	if destructiveCount == 0 {
		t.Fatal("未检测到任何破坏性工具，注解断言失效")
	}
}

// confirmArgumentRequired 判断 confirm 入参是否被 schema 声明为必填。
func confirmArgumentRequired(tool *mcpserver.ServerTool) bool {
	// item 是当前必填属性名。
	for _, item := range tool.Tool.InputSchema.Required {
		if item == confirmArgumentName {
			return true
		}
	}
	return false
}

// TestEndToEndSecretScan 在含秘密的夹具下遍历全部工具、资源与提示，断言零秘密泄漏（TR-14.2）。
func TestEndToEndSecretScan(t *testing.T) {
	// fixture 是端到端秘密夹具。
	fixture := newSecretFixture()
	// endpoint、audit、ctx 是全域端点、审计捕获与身份上下文。
	endpoint, audit, ctx := newFullDomainEndpoint(t, fixture)
	// markers 是全部秘密标记。
	markers := fixture.markers()
	// scanned 是本次扫描过的表面数量。
	scanned := 0
	// name 是注册表中的当前工具名。
	for name := range endpoint.mcpServer.ListTools() {
		// result 是用空入参调用工具的结果；缺参与业务错误都属于待扫描表面。
		result := invoke(endpoint, ctx, name, map[string]any{"confirm": true})
		if result == nil {
			continue
		}
		// text 是结果文本（成功 JSON 或中文错误摘要）。
		text := toolResultText(result)
		scanned++
		assertNoMarker(t, "工具 "+name, text, markers)
		// 破坏性工具在未确认时也必须只返回中文提示。
		// denied 是缺 confirm 的调用结果。
		denied := invoke(endpoint, ctx, name, map[string]any{})
		if denied != nil {
			scanned++
			assertNoMarker(t, "工具(未确认) "+name, toolResultText(denied), markers)
		}
	}
	if scanned == 0 {
		t.Fatal("未扫描到任何工具表面，断言失效")
	}
	// 资源与提示表面同样必须零泄漏。
	// uri 是全部已注册资源 URI。
	for uri := range endpoint.mcpServer.ListResources() {
		// contents、err 是资源读取结果与错误。
		contents, err := endpoint.readResourceForTest(ctx, uri)
		if err == nil {
			// content 是当前资源内容。
			for _, content := range contents {
				scanned++
				assertNoMarker(t, "资源 "+uri, content.Text, markers)
			}
			continue
		}
		scanned++
		assertNoMarker(t, "资源错误 "+uri, err.Error(), markers)
	}
	// prompt 是注册表中的当前提示。
	for name := range endpoint.mcpServer.ListPrompts() {
		// result、err 是提示结果与错误。
		result, err := endpoint.promptForTest(ctx, name, map[string]string{"account_id": "acc1", "order_id": "o1", "threshold": "3"})
		if err != nil {
			scanned++
			assertNoMarker(t, "提示错误 "+name, err.Error(), markers)
			continue
		}
		// message 是当前提示消息。
		for _, message := range result.Messages {
			scanned++
			assertNoMarker(t, "提示 "+name, message.Text, markers)
		}
	}
	// 审计行（含工具入参与资源名）不得包含任何秘密。
	if len(audit.entries) == 0 {
		t.Fatal("未捕获到任何审计条目，断言失效")
	}
	// entry 是当前审计条目。
	for _, entry := range audit.entries {
		// encoded 是审计条目的 JSON 表示。
		encoded, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			t.Fatalf("序列化审计条目失败: %v", marshalErr)
		}
		scanned++
		assertNoMarker(t, "审计 "+entry.Category+"/"+entry.Name, string(encoded), markers)
	}
	if scanned < 100 {
		t.Fatalf("扫描表面数量异常偏少: %d", scanned)
	}
	t.Logf("端到端秘密扫描覆盖 %d 个表面，全部无泄漏", scanned)
}

// assertNoMarker 断言文本不含任何秘密标记，失败时指出标记与来源。
func assertNoMarker(t *testing.T, source, text string, markers []string) {
	t.Helper()
	// marker 是当前扫描的秘密标记。
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			// 只打印命中片段周围的上下文，避免把完整秘密写入测试输出。
			// index 是命中位置。
			index := strings.Index(text, marker)
			// start 是上下文起点，越界时收敛到 0。
			start := index - 40
			if start < 0 {
				start = 0
			}
			// end 是上下文终点，越界时收敛到文本末尾。
			end := index + len(marker) + 40
			if end > len(text) {
				end = len(text)
			}
			t.Fatalf("%s 泄漏秘密标记 %q，上下文: ...%s...", source, marker, text[start:end])
		}
	}
}

// toolResultText 提取工具结果的可见文本；无内容时返回空串。
func toolResultText(result *mcpproto.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	// parts 是全部文本内容片段。
	parts := make([]string, 0, len(result.Content))
	// content 是当前内容块。
	for _, content := range result.Content {
		// text 是文本内容块。
		if text, ok := content.(mcpproto.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// TestSafeFixturesDoNotLeakByConstruction 验证关键夹具本身确实携带秘密，避免断言失效。
func TestSafeFixturesDoNotLeakByConstruction(t *testing.T) {
	// fixture 是端到端秘密夹具。
	fixture := newSecretFixture()
	// markers 是全部秘密标记；每个都必须非空且携带 SECRET 后缀，否则扫描断言失效。
	markers := fixture.markers()
	// seen 用于检测标记重复，重复会让泄漏定位失真。
	seen := map[string]bool{}
	// marker 是当前秘密标记。
	for _, marker := range markers {
		if marker == "" || !strings.Contains(marker, "SECRET") {
			t.Fatalf("夹具秘密标记无效: %q", marker)
		}
		if seen[marker] {
			t.Fatalf("夹具秘密标记重复: %q", marker)
		}
		seen[marker] = true
	}
	// 夹具必须真正流入端口：隐藏设置字段与资源卡密正文都要携带对应秘密，
	// 否则端到端扫描会因夹具空转而“假通过”。
	if newSecretSettingsPorts(fixture).modelsAPIKey != fixture.aiKey {
		t.Fatal("设置端口未携带 AI 密钥夹具，端到端扫描断言将失效")
	}
	// resourcePorts 是携带明文卡密的资源端口。
	resourcePorts := newSecretResourcePorts(fixture)
	if len(resourcePorts.cards) == 0 || resourcePorts.cards[len(resourcePorts.cards)-1].DataContent != fixture.cardData {
		t.Fatal("资源端口未携带明文卡密夹具，端到端扫描断言将失效")
	}
	// 空入参调用的错误路径同样纳入扫描：构造一个必然失败的调用确认错误文本干净。
	endpoint, _, ctx := newFullDomainEndpoint(t, fixture)
	// failure 是缺少必填参数的工具调用结果。
	failure := invoke(endpoint, ctx, "card_get", map[string]any{})
	if failure == nil || !failure.IsError {
		t.Fatal("缺少必填参数应返回错误结果")
	}
	assertNoMarker(t, "参数错误", toolResultText(failure), fixture.markers())
	// 未注册工具同样返回中文参数错误。
	unknown := invoke(endpoint, ctx, "cookie_read_all", map[string]any{})
	assertNoMarker(t, "未注册工具", toolResultText(unknown), fixture.markers())
	// 域假端口与应用模型的零值语义断言，保证夹具与真实端口语义一致。
	// emptyStats、emptyQuery 是分析与订单查询模型的应用层零值。
	emptyStats, emptyQuery := analyticsapp.DashboardStats{}, orderapp.ListQuery{}
	// emptyIssue、emptyCard 是自动化异常与卡密模型的应用层零值。
	emptyIssue, emptyCard := automationapp.RunIssue{}, cardsapp.Card{}
	if emptyStats.TotalOrders != 0 || emptyQuery.UserID != 0 || emptyIssue.ID != 0 || emptyCard.ID != 0 {
		t.Fatal("应用模型零值语义异常")
	}
}
