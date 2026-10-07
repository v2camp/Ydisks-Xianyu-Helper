package logsafe

import (
	"errors"
	"strings"
	"testing"
)

// TestRedactionHelpers 封装TestRedactionHelpers业务协调。
func TestRedactionHelpers(t *testing.T) {
	if // emptyError 是 nil 错误的安全空文本。
	emptyError := Error(nil); emptyError != "" {
		t.Fatalf("nil 错误应返回空文本: %q", emptyError)
	}
	if ID(" secret ") != ID("secret") || len(ID("secret")) != 12 {
		t.Fatal("ID should be trimmed, stable, and short")
	}
	if ID("") != "" {
		t.Fatal("empty ID should remain empty")
	}
	if // got 用于本次流程后续判断的got
	got := URL("https://example.com/path?q=token#secret"); got != "https://example.com/path" {
		t.Fatalf("URL leaked query or fragment: %q", got)
	}
	if // got 用于本次流程后续判断的got
	got := URL("not-a-url"); got != "<redacted>" {
		t.Fatalf("invalid URL = %q", got)
	}
}

// TestErrorRedactsDiagnosticSecrets 验证错误日志不会保留 URL 查询和常见凭证键值。
func TestErrorRedactsDiagnosticSecrets(t *testing.T) {
	// err 保存包含模拟凭证和 webhook 查询参数的底层错误。
	err := errors.New(`Post "https://hooks.example.test/send?access_token=token-value": cookie=unb=account-secret password='password-value'`)
	// got 保存经过诊断脱敏的错误文本。
	got := Error(err)
	// secret 表示当前待确认未出现在诊断文本中的模拟秘密。
	for _, secret := range []string{"token-value", "account-secret", "password-value"} {
		if strings.Contains(got, secret) {
			t.Fatalf("脱敏错误仍包含秘密 %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "https://hooks.example.test/send") {
		t.Fatalf("应保留 URL 的安全路径: %s", got)
	}
}

// TestTextRedactsQuotedCredentialPairs 验证 JSON 风格的 "键":"值" 凭证对会被整体脱敏且不误伤普通字段。
func TestTextRedactsQuotedCredentialPairs(t *testing.T) {
	// tokenRequest 复刻第三方 SDK 打印换取令牌请求体的形态，其中 clientSecret 必须被隐藏。
	tokenRequest := `retrieve access token URL:https://bots.qq.com/app/getAppAccessToken req:{"appId":"102012345","clientSecret":"plain-secret-value"}`
	// got 保存清洗后的诊断文本。
	got := Text(tokenRequest)
	// secret 表示每个不得保留在安全文本中的模拟秘密。
	for _, secret := range []string{"plain-secret-value"} {
		if strings.Contains(got, secret) {
			t.Fatalf("脱敏文本仍包含秘密 %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, `"clientSecret":"<redacted>"`) {
		t.Fatalf("应保留敏感键名并替换其值: %s", got)
	}
	if !strings.Contains(got, `"appId":"102012345"`) {
		t.Fatalf("非敏感字段不应被改写: %s", got)
	}
	// tokenResponse 复刻 SDK 打印令牌响应体的形态，access_token 必须被隐藏。
	tokenResponse := `access token:{"access_token":"token-value-leaked","expires_in":"7200"}`
	if // sanitizedResponse 是令牌响应体清洗结果。
	sanitizedResponse := Text(tokenResponse); strings.Contains(sanitizedResponse, "token-value-leaked") {
		t.Fatalf("脱敏文本仍包含访问令牌: %s", sanitizedResponse)
	}
	// structuredToken 复刻 SDK 用 %+v 打印 oauth2.Token 的形态，两种令牌都必须被隐藏。
	structuredToken := `token:&{AccessToken:access-leaked TokenType:Bearer RefreshToken:refresh-leaked Expiry:2026-10-08}`
	// sanitizedStructured 是结构化令牌文本清洗结果。
	sanitizedStructured := Text(structuredToken)
	if strings.Contains(sanitizedStructured, "access-leaked") || strings.Contains(sanitizedStructured, "refresh-leaked") {
		t.Fatalf("结构化令牌文本未脱敏: %s", sanitizedStructured)
	}
	if // plainJSON 是不含敏感键名的普通 JSON，必须原样保留。
	plainJSON := `{"nickname":"小明","count":3}`; Text(plainJSON) != plainJSON {
		t.Fatalf("普通 JSON 不应被改写: %s", Text(plainJSON))
	}
}

// TestExternalErrorRedactsCredentialPaths 验证外部网络错误不会保留 Telegram Token、Webhook 路径、用户信息或查询参数。
func TestExternalErrorRedactsCredentialPaths(t *testing.T) {
	if externalURLOrigin("not-a-url") != "<redacted>" {
		t.Fatal("无效外部地址应完全脱敏")
	}
	if externalQuotedTarget("invalid") != "<redacted>" {
		t.Fatal("缺少动作分隔符的请求地址应完全脱敏")
	}
	// diagnosticErr 模拟 net/http 在连接失败时返回的完整请求地址和附加凭证键值。
	diagnosticErr := errors.New(`Post "https://user:password@api.telegram.org/bot123456:REVIEW_SECRET/sendMessage?access_token=query-secret": dial tcp: connection refused`)
	// sanitized 保存面向日志、数据库和内部包装的外部错误文本。
	sanitized := ExternalError(diagnosticErr)
	// secret 表示每个不得保留在安全诊断文本中的模拟秘密。
	for _, secret := range []string{"user", "password", "123456:REVIEW_SECRET", "sendMessage", "query-secret"} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("外部错误仍包含秘密 %q: %s", secret, sanitized)
		}
	}
	if !strings.Contains(sanitized, "https://api.telegram.org/<redacted>") || !strings.Contains(sanitized, "connection refused") {
		t.Fatalf("外部错误未保留安全诊断上下文: %s", sanitized)
	}
	// malformedDiagnostic 模拟配置错误导致 URL 解析器直接回显不合法 Webhook 秘密的场景。
	malformedDiagnostic := ExternalError(errors.New(`parse "://MALFORMED_WEBHOOK_SECRET/path": missing protocol scheme`))
	if strings.Contains(malformedDiagnostic, "MALFORMED_WEBHOOK_SECRET") || !strings.Contains(malformedDiagnostic, `parse "<redacted>"`) {
		t.Fatalf("不合法外部地址未完全脱敏: %s", malformedDiagnostic)
	}
	if // emptyExternalError 验证 nil 外部错误保持安全空文本。
	emptyExternalError := ExternalError(nil); emptyExternalError != "" {
		t.Fatalf("nil 外部错误应返回空文本: %q", emptyExternalError)
	}
}
