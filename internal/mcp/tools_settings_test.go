// tools_settings_test.go 覆盖设置与 AI 域工具：敏感三态写入的秘密零回传、脱敏读取、
// MCP 自身状态键拦截、用户偏好设置与 AI 回复设置读写、模型列表与连通性诊断。

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	settingsapp "xianyu-go/internal/application/settings"
)

// aiAPIKeySecret 是夹具中的模拟 AI 密钥，任何响应与错误都不得包含该片段。
const aiAPIKeySecret = "sk-mcp-secret-3399"

// smtpSettingSecret 是夹具中的模拟 SMTP 密码，任何响应与错误都不得包含该片段。
const smtpSettingSecret = "SMTP-SETTING-SECRET-5521"

// fakeSettingsPorts 是设置与 AI 域假端口，记录调用并支持注入错误。
type fakeSettingsPorts struct {
	// system 是系统设置读取返回（通常为脱敏结果）。
	system map[string]string
	// user 是用户偏好设置返回。
	user map[string]string
	// userValue 是单项用户设置返回。
	userValue string
	// aiReplies 是账号 AI 设置摘要列表返回。
	aiReplies []settingsapp.AIReplySettings
	// aiReply 是单个账号 AI 设置摘要返回。
	aiReply settingsapp.AIReplySettings
	// models 是模型列表返回。
	models []string
	// connection 是连通性诊断返回。
	connection settingsapp.AIConnectionTestResult
	// systemErr、userErr、getUserErr、aiListErr、aiGetErr、aiSetErr、modelsErr、connErr 是各用例注入错误。
	systemErr, userErr, getUserErr, aiListErr, aiGetErr, aiSetErr, modelsErr, connErr error
	// sensitiveKeys 是测试环境认定的敏感键集合。
	sensitiveKeys map[string]bool
	// getSystemCalls、setSystemCalls、applyCalls、listUserCalls、getUserCalls、setUserCalls 是设置类调用计数。
	getSystemCalls, setSystemCalls, applyCalls, listUserCalls, getUserCalls, setUserCalls int
	// aiListCalls、aiGetCalls、aiSetCalls、modelsCalls、connCalls 是 AI 类调用计数。
	aiListCalls, aiGetCalls, aiSetCalls, modelsCalls, connCalls int
	// appliedValues、appliedSecrets 是批量写入收到的参数。
	appliedValues  map[string]string
	appliedSecrets map[string]settingsapp.SecretChange
	// setSystemKey、setSystemValue、setSystemAction 是单项系统写入收到的参数。
	setSystemKey, setSystemValue, setSystemAction string
	// upsertedAI 是 AI 设置保存收到的参数。
	upsertedAI settingsapp.AIReplySettings
	// modelsBaseURL、modelsAPIKey 是模型列表收到的参数。
	modelsBaseURL, modelsAPIKey string
	// connModel、connBaseURL、connAPIKey 是连通性测试收到的参数。
	connModel, connBaseURL, connAPIKey string
}

// IsSensitiveSettingKey 报告给定键是否属于测试敏感集合。
func (f *fakeSettingsPorts) IsSensitiveSettingKey(key string) bool {
	return f.sensitiveKeys[key]
}

// PublicSystem 回传公开系统设置。
func (f *fakeSettingsPorts) PublicSystem(context.Context) (map[string]string, error) {
	return f.system, f.systemErr
}

// GetSystem 记录调用并回传脱敏系统设置。
func (f *fakeSettingsPorts) GetSystem(context.Context, int64) (map[string]string, error) {
	f.getSystemCalls++
	return f.system, f.systemErr
}

// ApplySystemChanges 记录批量写入参数。
func (f *fakeSettingsPorts) ApplySystemChanges(_ context.Context, _ int64, values map[string]string, secrets map[string]settingsapp.SecretChange) error {
	f.applyCalls++
	f.appliedValues, f.appliedSecrets = values, secrets
	return nil
}

// SetSystem 记录单项写入参数。
func (f *fakeSettingsPorts) SetSystem(_ context.Context, _ int64, key, value, action string) error {
	f.setSystemCalls++
	f.setSystemKey, f.setSystemValue, f.setSystemAction = key, value, action
	return nil
}

// ListUser 记录调用并回传用户偏好设置。
func (f *fakeSettingsPorts) ListUser(context.Context, int64) (map[string]string, error) {
	f.listUserCalls++
	return f.user, f.userErr
}

// GetUser 记录调用并回传单项用户设置。
func (f *fakeSettingsPorts) GetUser(context.Context, int64, string) (string, error) {
	f.getUserCalls++
	return f.userValue, f.getUserErr
}

// SetUser 记录单项用户设置写入。
func (f *fakeSettingsPorts) SetUser(context.Context, int64, string, string) error {
	f.setUserCalls++
	return f.userErr
}

// ListAIReply 记录调用并回传账号 AI 设置列表。
func (f *fakeSettingsPorts) ListAIReply(context.Context, int64) ([]settingsapp.AIReplySettings, error) {
	f.aiListCalls++
	return f.aiReplies, f.aiListErr
}

// GetAIReply 记录调用并回传单个账号 AI 设置。
func (f *fakeSettingsPorts) GetAIReply(context.Context, int64, string) (settingsapp.AIReplySettings, error) {
	f.aiGetCalls++
	return f.aiReply, f.aiGetErr
}

// UpsertAIReply 记录 AI 设置保存参数。
func (f *fakeSettingsPorts) UpsertAIReply(_ context.Context, _ int64, _ string, settings settingsapp.AIReplySettings) error {
	f.aiSetCalls++
	f.upsertedAI = settings
	return f.aiSetErr
}

// ListAIModels 记录模型列表参数并回传预设模型。
func (f *fakeSettingsPorts) ListAIModels(_ context.Context, _ int64, baseURL, apiKey string) ([]string, error) {
	f.modelsCalls++
	f.modelsBaseURL, f.modelsAPIKey = baseURL, apiKey
	return f.models, f.modelsErr
}

// TestAIConnection 记录连通性测试参数并回传预设诊断。
func (f *fakeSettingsPorts) TestAIConnection(_ context.Context, _ int64, baseURL, apiKey, model string) (settingsapp.AIConnectionTestResult, error) {
	f.connCalls++
	f.connBaseURL, f.connAPIKey, f.connModel = baseURL, apiKey, model
	return f.connection, f.connErr
}

// newSettingsToolEndpoint 构造注册设置与 AI 工具的端点、假端口与身份上下文。
func newSettingsToolEndpoint(t *testing.T, fake *fakeSettingsPorts) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterSettingsTools(fake)
	endpoint.RegisterAITools(fake)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// newSensitiveSettingsFake 构造含四类敏感键的假设置端口。
func newSensitiveSettingsFake() *fakeSettingsPorts {
	return &fakeSettingsPorts{
		sensitiveKeys: map[string]bool{
			"ai_api_key": true, "smtp_password": true,
			"qq_reply_secret_key": true, "captcha.remote_secret_key": true,
		},
		// system 模拟数据库层脱敏结果：敏感键只保留 configured 标记。
		system: map[string]string{
			"ai_api_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
			"log_level":  "info",
			"ai_api_key": "configured", "smtp_password": "configured",
			"qq_reply_secret_key": "configured", "captcha.remote_secret_key": "configured",
		},
		user:      map[string]string{"theme": "dark"},
		userValue: "dark",
		aiReplies: []settingsapp.AIReplySettings{{CookieID: "acc1", AIEnabled: true, MaxDiscountPercent: 10, MaxBargainRounds: 3}},
		aiReply:   settingsapp.AIReplySettings{CookieID: "acc1", AIEnabled: true, AutoAdjustPriceEnabled: true, MaxDiscountPercent: 15, MaxBargainRounds: 4, CustomPrompts: "礼貌出价"},
		models:    []string{"qwen-plus", "qwen-max"},
		connection: settingsapp.AIConnectionTestResult{
			Model: "qwen-plus", LatencyMS: 320, Reply: "连接成功",
		},
	}
}

// assertNoSettingsSecret 断言文本不含任何模拟设置密钥原文。
func assertNoSettingsSecret(t *testing.T, text string) {
	t.Helper()
	// secret 是当前扫描的密钥片段。
	for _, secret := range []string{aiAPIKeySecret, smtpSettingSecret} {
		if strings.Contains(text, secret) {
			t.Fatalf("设置域响应泄漏密钥 %q: %s", secret, text)
		}
	}
}

// TestSettingsSensitiveReadIsRedacted 验证脱敏读取只返回 configured 标记且不泄漏密钥（TR-11.1）。
func TestSettingsSensitiveReadIsRedacted(t *testing.T) {
	// fake 是含四类敏感键的假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// result 是系统设置读取结果。
	result := invoke(endpoint, ctx, "settings_get_system", nil)
	if result.IsError {
		t.Fatalf("系统设置读取失败: %s", resultJSON(t, result))
	}
	// text 是脱敏设置 JSON。
	text := resultJSON(t, result)
	if !strings.Contains(text, `"ai_api_key":"configured"`) || !strings.Contains(text, `"log_level":"info"`) {
		t.Fatalf("脱敏读取映射异常: %s", text)
	}
	// 敏感键名清单必须覆盖四类键，且不含任何值。
	if !strings.Contains(text, `"sensitive_keys":[`) || !strings.Contains(text, "captcha.remote_secret_key") {
		t.Fatalf("敏感键清单缺失: %s", text)
	}
	assertNoSettingsSecret(t, text)
	if fake.getSystemCalls != 1 {
		t.Fatalf("脱敏读取调用次数异常: %d", fake.getSystemCalls)
	}
}

// TestSettingsSensitiveWriteNeverEchoes 验证敏感写入的密钥在响应与错误三处零出现（TR-11.1）。
func TestSettingsSensitiveWriteNeverEchoes(t *testing.T) {
	// fake 是含四类敏感键的假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// batch 是普通设置 + 敏感三态命令的批量写入。
	batch := invoke(endpoint, ctx, "settings_update_system", map[string]any{
		"values": map[string]any{"log_level": "debug"},
		"secrets": map[string]any{
			"ai_api_key":          map[string]any{"action": "replace", "value": aiAPIKeySecret},
			"smtp_password":       map[string]any{"action": "retain"},
			"qq_reply_secret_key": map[string]any{"action": "clear"},
		},
	})
	if batch.IsError {
		t.Fatalf("批量写入失败: %s", resultJSON(t, batch))
	}
	// batchText 是写入确认 JSON，只允许出现键名，不允许出现 value。
	batchText := resultJSON(t, batch)
	if !strings.Contains(batchText, `"applied":true`) || !strings.Contains(batchText, `"log_level"`) ||
		!strings.Contains(batchText, `"ai_api_key"`) {
		t.Fatalf("写入确认映射异常: %s", batchText)
	}
	assertNoSettingsSecret(t, batchText)
	if batchText != "" && strings.Contains(batchText, aiAPIKeySecret) {
		t.Fatalf("写入响应回显了密钥: %s", batchText)
	}
	// 落库参数必须真实携带密钥（只写语义），且动作透传正确。
	if fake.appliedSecrets["ai_api_key"].Value != aiAPIKeySecret ||
		fake.appliedSecrets["smtp_password"].Action != "retain" ||
		fake.appliedSecrets["qq_reply_secret_key"].Action != "clear" {
		t.Fatalf("敏感命令透传异常: %+v", fake.appliedSecrets)
	}
	if fake.appliedValues["log_level"] != "debug" {
		t.Fatalf("普通设置透传异常: %+v", fake.appliedValues)
	}
	// single 是单项敏感键写入，响应同样只回传键名。
	single := invoke(endpoint, ctx, "settings_set_system", map[string]any{
		"key": "smtp_password", "value": smtpSettingSecret, "action": "replace",
	})
	if single.IsError {
		t.Fatalf("单项敏感写入失败: %s", resultJSON(t, single))
	}
	// singleText 是单项写入确认 JSON。
	singleText := resultJSON(t, single)
	if !strings.Contains(singleText, `"secret_keys":["smtp_password"]`) || strings.Contains(singleText, `"updated_keys"`) {
		t.Fatalf("单项敏感写入确认异常: %s", singleText)
	}
	assertNoSettingsSecret(t, singleText)
	if fake.setSystemKey != "smtp_password" || fake.setSystemValue != smtpSettingSecret || fake.setSystemAction != "replace" {
		t.Fatalf("单项写入透传异常: key=%s action=%s", fake.setSystemKey, fake.setSystemAction)
	}
	// 敏感写入失败时的错误也不得回显密钥原文。
	fake2 := newSensitiveSettingsFake()
	fake2.systemErr = errors.New("audit failed while handling key " + aiAPIKeySecret)
	// endpoint2、ctx2 是注入审计失败的设置端点。
	endpoint2, ctx2 := newSettingsToolEndpoint(t, fake2)
	// readFailure 是审计失败的脱敏读取结果。
	readFailure := invoke(endpoint2, ctx2, "settings_get_system", nil)
	if !readFailure.IsError {
		t.Fatal("审计失败必须返回错误")
	}
	assertNoSettingsSecret(t, resultJSON(t, readFailure))
}

// TestSettingsRejectsMCPStateKeys 验证 MCP 自身状态键不能经通用设置入口修改。
func TestSettingsRejectsMCPStateKeys(t *testing.T) {
	// fake 是含敏感键集合的假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// 单项写入必须拦截两个 MCP 状态键，且零触达应用服务。
	single := invoke(endpoint, ctx, "settings_set_system", map[string]any{
		"key": mcpStateSettingEnabled, "value": "false",
	})
	if !single.IsError || fake.setSystemCalls != 0 {
		t.Fatalf("MCP 状态键必须被拦截: %s", resultJSON(t, single))
	}
	if !strings.Contains(resultJSON(t, single), "MCP 服务开关") {
		t.Fatalf("拦截提示异常: %s", resultJSON(t, single))
	}
	// 批量写入的 values 与 secrets 同样必须被拦截。
	batchValues := invoke(endpoint, ctx, "settings_update_system", map[string]any{
		"values": map[string]any{mcpStateSettingAllowNonLoopback: "true"},
	})
	if !batchValues.IsError || fake.applyCalls != 0 {
		t.Fatalf("批量普通设置中的 MCP 状态键必须被拦截: %s", resultJSON(t, batchValues))
	}
	// batchSecrets 是敏感命令中包含 MCP 状态键的写入。
	batchSecrets := invoke(endpoint, ctx, "settings_update_system", map[string]any{
		"secrets": map[string]any{mcpStateSettingEnabled: map[string]any{"action": "replace", "value": "1"}},
	})
	if !batchSecrets.IsError || fake.applyCalls != 0 {
		t.Fatalf("批量敏感命令中的 MCP 状态键必须被拦截: %s", resultJSON(t, batchSecrets))
	}
	// 空写入必须被拒绝。
	emptyWrite := invoke(endpoint, ctx, "settings_update_system", map[string]any{})
	if !emptyWrite.IsError || fake.applyCalls != 0 {
		t.Fatal("空写入必须被拒绝")
	}
	// 普通键写入正常放行。
	normal := invoke(endpoint, ctx, "settings_set_system", map[string]any{"key": "log_level", "value": "warn"})
	if normal.IsError || fake.setSystemCalls != 1 {
		t.Fatalf("普通设置写入应放行: %s", resultJSON(t, normal))
	}
}

// TestSettingsUpdateValidation 验证批量写入的入参形状校验。
func TestSettingsUpdateValidation(t *testing.T) {
	// fake 是含敏感键集合的假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// badValues 是值类型非法的普通设置，必须零触达。
	badValues := invoke(endpoint, ctx, "settings_update_system", map[string]any{
		"values": map[string]any{"log_level": 1.0},
	})
	if !badValues.IsError || fake.applyCalls != 0 || !strings.Contains(resultJSON(t, badValues), "必须是字符串") {
		t.Fatalf("非法普通设置必须零触达: %s", resultJSON(t, badValues))
	}
	// badAction 是缺少 action 的敏感命令，必须零触达。
	badAction := invoke(endpoint, ctx, "settings_update_system", map[string]any{
		"secrets": map[string]any{"ai_api_key": map[string]any{"value": aiAPIKeySecret}},
	})
	if !badAction.IsError || fake.applyCalls != 0 {
		t.Fatalf("缺少 action 的敏感命令必须零触达: %s", resultJSON(t, badAction))
	}
	assertNoSettingsSecret(t, resultJSON(t, badAction))
	// 应用服务的三态校验错误必须归一为可展示中文提示。
	fake.applyCalls = 0
	fake.sensitiveKeys = map[string]bool{"ai_api_key": true}
	// invalidAction 是非法 action 字符串，由应用服务拒绝。
	fake.systemErr = nil
	// serviceErr 是应用服务返回的校验错误。
	serviceErr := &settingsapp.ValidationError{Message: "敏感设置命令无效"}
	// fake2 是模拟应用服务校验错误的设置端口。
	fake2 := newSensitiveSettingsFake()
	fake2.systemErr = serviceErr
	// endpoint2、ctx2 是注入校验错误的设置端点。
	endpoint2, ctx2 := newSettingsToolEndpoint(t, fake2)
	// result2 是被应用服务拒绝的脱敏读取。
	result2 := invoke(endpoint2, ctx2, "settings_get_system", nil)
	if !strings.Contains(resultJSON(t, result2), "敏感设置命令无效") {
		t.Fatalf("设置校验错误未回传中文原因: %s", resultJSON(t, result2))
	}
}

// TestUserSettingsTools 验证用户偏好设置读写的成功与失败路径。
func TestUserSettingsTools(t *testing.T) {
	// fake 是假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// listResult 是用户设置列表结果。
	listResult := invoke(endpoint, ctx, "settings_user_list", nil)
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"theme":"dark"`) {
		t.Fatalf("用户设置列表异常: %s", resultJSON(t, listResult))
	}
	// getResult 是单项用户设置读取结果。
	getResult := invoke(endpoint, ctx, "settings_user_get", map[string]any{"key": "theme"})
	if getResult.IsError || !strings.Contains(resultJSON(t, getResult), `"configured":true`) {
		t.Fatalf("用户设置读取异常: %s", resultJSON(t, getResult))
	}
	// setResult 是用户设置写入结果。
	setResult := invoke(endpoint, ctx, "settings_user_set", map[string]any{"key": "theme", "value": "light"})
	if setResult.IsError || fake.setUserCalls != 1 {
		t.Fatalf("用户设置写入异常: %s", resultJSON(t, setResult))
	}
	// 未配置的键返回 configured=false。
	fake.userValue = ""
	// emptyResult 是未配置键的读取结果。
	emptyResult := invoke(endpoint, ctx, "settings_user_get", map[string]any{"key": "missing"})
	if !strings.Contains(resultJSON(t, emptyResult), `"configured":false`) {
		t.Fatalf("未配置键映射异常: %s", resultJSON(t, emptyResult))
	}
	// 缺少必填键必须零触达。
	missing := invoke(endpoint, ctx, "settings_user_set", map[string]any{"value": "x"})
	if !missing.IsError || fake.setUserCalls != 1 {
		t.Fatal("缺少设置键必须零触达")
	}
}

// TestAIReplyToolsAndConflict 验证 AI 回复设置读写与改价冲突错误映射（TR-11.2）。
func TestAIReplyToolsAndConflict(t *testing.T) {
	// fake 是假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// listResult 是 AI 设置列表结果。
	listResult := invoke(endpoint, ctx, "ai_reply_list", nil)
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"ai_enabled":true`) {
		t.Fatalf("AI 设置列表异常: %s", resultJSON(t, listResult))
	}
	// getResult 是账号 AI 设置读取结果。
	getResult := invoke(endpoint, ctx, "ai_reply_get", map[string]any{"account_id": "acc1"})
	if getResult.IsError {
		t.Fatalf("AI 设置读取失败: %s", resultJSON(t, getResult))
	}
	// getText 是 AI 设置 JSON，必须含折扣与轮次，且不含模型地址与密钥字段。
	getText := resultJSON(t, getResult)
	if !strings.Contains(getText, `"max_discount_percent":15`) || !strings.Contains(getText, "礼貌出价") {
		t.Fatalf("AI 设置映射异常: %s", getText)
	}
	// forbidden 是不得出现在 AI 设置视图中的系统秘密字段名。
	for _, forbidden := range []string{"api_key", "base_url", "ai_api_url"} {
		if strings.Contains(getText, forbidden) {
			t.Fatalf("AI 设置不应包含系统秘密字段 %s: %s", forbidden, getText)
		}
	}
	// setResult 是 AI 设置保存结果。
	setResult := invoke(endpoint, ctx, "ai_reply_set", map[string]any{
		"account_id": "acc1", "ai_enabled": true, "auto_adjust_price": true,
		"max_discount_percent": 20.0, "max_discount_amount": 500.0, "max_bargain_rounds": 5.0,
		"custom_prompts": "温和议价",
	})
	if setResult.IsError || fake.aiSetCalls != 1 {
		t.Fatalf("AI 设置保存失败: %s", resultJSON(t, setResult))
	}
	if !fake.upsertedAI.AutoAdjustPriceEnabled || fake.upsertedAI.MaxDiscountPercent != 20 ||
		fake.upsertedAI.MaxBargainRounds != 5 || fake.upsertedAI.CustomPrompts != "温和议价" {
		t.Fatalf("AI 设置入参映射异常: %+v", fake.upsertedAI)
	}
	// 改价冲突必须归一为可展示中文原因且不重试。
	fake.aiSetErr = settingsapp.ErrPricingModeConflict
	// conflict 是冲突场景的保存结果。
	conflict := invoke(endpoint, ctx, "ai_reply_set", map[string]any{"account_id": "acc1", "ai_enabled": true})
	if !strings.Contains(resultJSON(t, conflict), "不能同时启用") {
		t.Fatalf("改价冲突映射异常: %s", resultJSON(t, conflict))
	}
	// 账号越权必须归一为无权提示。
	fake.aiSetErr = settingsapp.ErrForbidden
	// forbidden 是越权保存结果。
	forbidden := invoke(endpoint, ctx, "ai_reply_set", map[string]any{"account_id": "acc9", "ai_enabled": true})
	if !strings.Contains(resultJSON(t, forbidden), "无权操作该资源") {
		t.Fatalf("账号越权映射异常: %s", resultJSON(t, forbidden))
	}
	// 数值边界由应用服务校验并回传中文原因。
	fake.aiSetErr = &settingsapp.ValidationError{Message: "最大砍价轮次必须在 1 到 10 之间"}
	// outOfRange 是越界数值的保存结果。
	outOfRange := invoke(endpoint, ctx, "ai_reply_set", map[string]any{"account_id": "acc1", "ai_enabled": true, "max_bargain_rounds": 99.0})
	if !strings.Contains(resultJSON(t, outOfRange), "最大砍价轮次必须在 1 到 10 之间") {
		t.Fatalf("数值边界映射异常: %s", resultJSON(t, outOfRange))
	}
}

// TestAIModelsAndConnectionTools 验证模型列表与连通性测试的透传与秘密不回显（TR-11.2）。
func TestAIModelsAndConnectionTools(t *testing.T) {
	// fake 是假设置端口。
	fake := newSensitiveSettingsFake()
	// endpoint、ctx 是设置工具端点与身份上下文。
	endpoint, ctx := newSettingsToolEndpoint(t, fake)
	// modelsResult 是模型列表结果。
	modelsResult := invoke(endpoint, ctx, "ai_models_list", map[string]any{
		"base_url": "https://ai.example.com/v1", "api_key": aiAPIKeySecret,
	})
	if modelsResult.IsError {
		t.Fatalf("模型列表失败: %s", resultJSON(t, modelsResult))
	}
	// modelsText 是模型列表 JSON，必须含模型名且不含密钥。
	modelsText := resultJSON(t, modelsResult)
	if !strings.Contains(modelsText, "qwen-plus") || !strings.Contains(modelsText, `"total":2`) {
		t.Fatalf("模型列表映射异常: %s", modelsText)
	}
	assertNoSettingsSecret(t, modelsText)
	if fake.modelsAPIKey != aiAPIKeySecret || fake.modelsBaseURL != "https://ai.example.com/v1" {
		t.Fatalf("模型列表入参透传异常: base=%s", fake.modelsBaseURL)
	}
	// connResult 是连通性测试结果。
	connResult := invoke(endpoint, ctx, "ai_test_connection", map[string]any{"model": "qwen-plus"})
	if connResult.IsError {
		t.Fatalf("连通性测试失败: %s", resultJSON(t, connResult))
	}
	// connText 是诊断 JSON，必须含延迟与模型名且不含密钥。
	connText := resultJSON(t, connResult)
	if !strings.Contains(connText, `"latency_ms":320`) || !strings.Contains(connText, `"model":"qwen-plus"`) {
		t.Fatalf("连通性诊断映射异常: %s", connText)
	}
	assertNoSettingsSecret(t, connText)
	if fake.connModel != "qwen-plus" || fake.connCalls != 1 {
		t.Fatalf("连通性入参透传异常: %+v", fake.connModel)
	}
	// 省略 base_url 与 api_key 时透传空串，由应用服务回退系统设置。
	if fake.connBaseURL != "" || fake.connAPIKey != "" {
		t.Fatalf("缺省参数应透传空串: base=%q key=%q", fake.connBaseURL, fake.connAPIKey)
	}
	// 缺少模型名必须零触达。
	missing := invoke(endpoint, ctx, "ai_test_connection", map[string]any{})
	if !missing.IsError || fake.connCalls != 1 {
		t.Fatal("缺少模型名必须零触达")
	}
	// 连通性失败必须归一为中文提示且不含远端细节。
	fake.connErr = errors.New("dial tcp 10.1.2.3:443: connect: connection refused")
	// connFailure 是连通性失败结果。
	connFailure := invoke(endpoint, ctx, "ai_test_connection", map[string]any{"model": "qwen-plus"})
	if !connFailure.IsError || strings.Contains(resultJSON(t, connFailure), "10.1.2.3") {
		t.Fatalf("连通性失败不得回显基础设施细节: %s", resultJSON(t, connFailure))
	}
}

// TestSettingsToolInventory 核对设置与 AI 域工具清单及注解。
func TestSettingsToolInventory(t *testing.T) {
	// expected 是 Task 11 要求注册的全部工具名。
	expected := map[string]bool{
		"settings_get_system": true, "settings_update_system": true, "settings_set_system": true,
		"settings_user_list": true, "settings_user_get": true, "settings_user_set": true,
		"ai_reply_list": true, "ai_reply_get": true, "ai_reply_set": true,
		"ai_models_list": true, "ai_test_connection": true,
	}
	// endpoint、_ 是注册设置与 AI 工具的端点。
	endpoint, _ := newSettingsToolEndpoint(t, newSensitiveSettingsFake())
	// registered 是实际注册的工具名集合。
	registered := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		registered[def.Name] = true
		if def.Destructive {
			t.Fatalf("设置与 AI 域不应存在破坏性工具: %s", def.Name)
		}
	}
	if len(registered) != len(expected) {
		t.Fatalf("设置与 AI 域工具数量异常: got=%d want=%d", len(registered), len(expected))
	}
	// name 是期望注册的工具名。
	for name := range expected {
		if !registered[name] {
			t.Fatalf("缺少工具 %s", name)
		}
	}
	// 端口缺失时两类工具均不得注册。
	empty, _ := newProtocolEndpoint(t)
	empty.RegisterSettingsTools(nil)
	empty.RegisterAITools(nil)
	// defs 是设置端口缺失时注册到的工具清单，必须为空。
	if defs := empty.ToolDefs(); len(defs) != 0 {
		t.Fatalf("设置端口缺失时不应注册工具，实际 %d 个", len(defs))
	}
	// 排序辅助函数必须稳定且不修改入参。
	// input 是待排序的键名切片。
	input := []string{"zeta", "alpha", "mid"}
	// sorted = 排序结果。
	sorted := sortedStrings(input)
	if sorted[0] != "alpha" || sorted[2] != "zeta" || input[0] != "zeta" {
		t.Fatalf("字符串排序异常: sorted=%v input=%v", sorted, input)
	}
}
