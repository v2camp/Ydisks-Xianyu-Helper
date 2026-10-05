// mcp_auth_test.go 覆盖 MCP 开关、持久化令牌生命周期与调用审计脱敏的确定性分支。

package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mcpTestNow 是令牌生命周期测试统一使用的固定当前时间，保证宽限截止可精确断言。
var mcpTestNow = time.Unix(1_700_000_000, 0)

// TestMCPTokenGenerateEntropyAndShape 验证令牌生成长度、URL 安全字符集与两次生成不重复。
func TestMCPTokenGenerateEntropyAndShape(t *testing.T) {
	// first、err 是第一枚生成令牌及其生成错误。
	first, err := GenerateMCPToken()
	if err != nil {
		t.Fatalf("生成第一枚令牌失败: %v", err)
	}
	// second、err 是第二枚生成令牌及其生成错误；二者必须不同且无填充 base64url 不含斜杠加号。
	second, err := GenerateMCPToken()
	if err != nil {
		t.Fatalf("生成第二枚令牌失败: %v", err)
	}
	if len(first) < 40 {
		t.Fatalf("令牌长度异常: %d", len(first))
	}
	if first == second {
		t.Fatal("两次生成的令牌相同，随机源不可用")
	}
	if strings.ContainsAny(first, "/+=") {
		t.Fatalf("令牌必须是无填充 URL 安全 base64: %q", first)
	}
}

// TestMCPTokenRotateVerifyExpireRevoke 覆盖轮换、宽限期、到期清理与吊销全链路。
func TestMCPTokenRotateVerifyExpireRevoke(t *testing.T) {
	// store、cleanup 是临时 SQLite 仓储及其关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本用例共享的数据库上下文。
	ctx := context.Background()

	// first、err 是首次轮换生成的当前令牌及其错误。
	first, err := store.MCP.RotateToken(ctx, mcpTestNow)
	if err != nil {
		t.Fatalf("首次轮换失败: %v", err)
	}
	// emptyOK 是空令牌的校验结果，必须为假。
	if emptyOK, _ := store.MCP.VerifyPersistedToken(ctx, "", mcpTestNow); emptyOK {
		t.Fatal("空令牌不应通过校验")
	}
	// wrongOK 是错误令牌的校验结果，必须为假。
	if wrongOK, _ := store.MCP.VerifyPersistedToken(ctx, first+"x", mcpTestNow); wrongOK {
		t.Fatal("错误令牌不应通过校验")
	}
	// currentOK、currentErr 是正确令牌的校验结果与错误，必须通过并刷新使用时间。
	if currentOK, currentErr := store.MCP.VerifyPersistedToken(ctx, first, mcpTestNow); currentErr != nil || !currentOK {
		t.Fatalf("当前令牌应通过校验: ok=%v err=%v", currentOK, currentErr)
	}
	// status、statusErr 是轮换后的令牌配置态，应只看到当前令牌且使用时间已刷新。
	status, statusErr := store.MCP.TokenStatus(ctx)
	if statusErr != nil || !status.HasCurrent || status.HasPrevious {
		t.Fatalf("首次轮换后状态异常: %+v err=%v", status, statusErr)
	}
	if status.CurrentLastUsedAt != mcpTestNow.Unix() {
		t.Fatalf("最后使用时间应为 %d，实际 %d", mcpTestNow.Unix(), status.CurrentLastUsedAt)
	}

	// second、err 是第二次轮换生成的新令牌；旧令牌进入 24 小时宽限。
	second, err := store.MCP.RotateToken(ctx, mcpTestNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("第二次轮换失败: %v", err)
	}
	if second == first {
		t.Fatal("轮换后新令牌不得与旧令牌相同")
	}
	// rotateTime 是第二次轮换时刻，旧令牌截止时间为该时刻加 24 小时。
	rotateTime := mcpTestNow.Add(time.Minute)
	// withinGrace 是宽限期内时刻，新旧令牌都必须可用。
	withinGrace := rotateTime.Add(23 * time.Hour)
	// oldInGrace 是旧令牌在宽限期内的校验结果。
	if oldInGrace, _ := store.MCP.VerifyPersistedToken(ctx, first, withinGrace); !oldInGrace {
		t.Fatal("宽限期内旧令牌应继续可用")
	}
	// newInGrace 是新令牌在宽限期窗口内的校验结果。
	if newInGrace, _ := store.MCP.VerifyPersistedToken(ctx, second, withinGrace); !newInGrace {
		t.Fatal("新令牌应可用")
	}
	// status 应同时含当前与宽限令牌，且截止时间精确为 24 小时。
	status, _ = store.MCP.TokenStatus(ctx)
	if !status.HasCurrent || !status.HasPrevious || status.PreviousExpiresAt != rotateTime.Add(24*time.Hour).Unix() {
		t.Fatalf("轮换后状态异常: %+v", status)
	}

	// expired 是超过宽限窗口后的时刻，旧令牌必须失效并被惰性清理。
	expired := rotateTime.Add(24*time.Hour + time.Second)
	// oldExpired 是旧令牌超期后的校验结果。
	if oldExpired, _ := store.MCP.VerifyPersistedToken(ctx, first, expired); oldExpired {
		t.Fatal("超过宽限期的旧令牌必须失效")
	}
	// status 中宽限令牌应已被清理。
	status, _ = store.MCP.TokenStatus(ctx)
	if status.HasPrevious {
		t.Fatal("到期旧令牌应已被惰性清理")
	}
	// currentStillValid 是当前令牌在清理旧令牌后的校验结果，不受影响。
	if currentStillValid, _ := store.MCP.VerifyPersistedToken(ctx, second, expired); !currentStillValid {
		t.Fatal("到期清理不应影响当前令牌")
	}

	// third、err 是第三次轮换的新令牌；此时历史宽限行已被清理过。
	third, err := store.MCP.RotateToken(ctx, expired)
	if err != nil {
		t.Fatalf("第三次轮换失败: %v", err)
	}
	// secondGrace 是第二次轮换旧令牌在新一轮宽限内的校验结果，应可用。
	if secondGrace, _ := store.MCP.VerifyPersistedToken(ctx, second, expired.Add(time.Hour)); !secondGrace {
		t.Fatal("第二次轮换的旧令牌应进入新宽限期")
	}
	// firstNeverRevived 是更早失效令牌的校验结果，不得因再次轮换恢复。
	if firstNeverRevived, _ := store.MCP.VerifyPersistedToken(ctx, first, expired.Add(time.Hour)); firstNeverRevived {
		t.Fatal("更早的失效令牌不得因再次轮换恢复")
	}
	// thirdValid 是最新令牌的校验结果。
	if thirdValid, _ := store.MCP.VerifyPersistedToken(ctx, third, expired.Add(time.Hour)); !thirdValid {
		t.Fatal("最新令牌应可用")
	}

	// revokeErr 是吊销全部持久化令牌的错误。
	if revokeErr := store.MCP.RevokeTokens(ctx); revokeErr != nil {
		t.Fatalf("吊销失败: %v", revokeErr)
	}
	// token 是吊销后逐个复验的持久化令牌。
	for _, token := range []string{second, third} {
		// afterRevoke 是吊销后的校验结果，必须全部为假。
		if afterRevoke, _ := store.MCP.VerifyPersistedToken(ctx, token, expired.Add(2*time.Hour)); afterRevoke {
			t.Fatal("吊销后持久化令牌必须全部失效")
		}
	}
	// status 吊销后不应再存在任何令牌行。
	status, _ = store.MCP.TokenStatus(ctx)
	if status.HasCurrent || status.HasPrevious {
		t.Fatalf("吊销后令牌状态应为空: %+v", status)
	}
}

// TestMCPServerSettingsRoundTrip 覆盖启用与非本机放行两个开关的读写与默认关闭语义。
func TestMCPServerSettingsRoundTrip(t *testing.T) {
	// store、cleanup 是临时仓储及其关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本用例上下文。
	ctx := context.Background()

	// enabled 是新库默认启用状态，必须为假。
	if enabled, _ := store.MCP.Enabled(ctx); enabled {
		t.Fatal("MCP 默认必须关闭")
	}
	// allowed 是新库默认非本机放行状态，必须为假。
	if allowed, _ := store.MCP.AllowNonLoopback(ctx); allowed {
		t.Fatal("非本机访问默认必须关闭")
	}
	// enableErr 是打开启用开关时的保存错误。
	if enableErr := store.MCP.SetEnabled(ctx, true); enableErr != nil {
		t.Fatalf("保存启用状态失败: %v", enableErr)
	}
	if // allowErr 是打开非本机放行开关时的保存错误。
	allowErr := store.MCP.SetAllowNonLoopback(ctx, true); allowErr != nil {
		t.Fatalf("保存网络策略失败: %v", allowErr)
	}
	// enabled 是开启后的启用状态读回值。
	if enabled, _ := store.MCP.Enabled(ctx); !enabled {
		t.Fatal("启用状态读回应为真")
	}
	// allowed 是开启后的网络策略读回值。
	if allowed, _ := store.MCP.AllowNonLoopback(ctx); !allowed {
		t.Fatal("非本机放行读回应为真")
	}
	// disableErr 是再次关闭启用开关的错误。
	if disableErr := store.MCP.SetEnabled(ctx, false); disableErr != nil {
		t.Fatalf("关闭启用失败: %v", disableErr)
	}
	// enabled 是关闭后的启用状态读回值。
	if enabled, _ := store.MCP.Enabled(ctx); enabled {
		t.Fatal("关闭后启用状态应为假")
	}
}

// TestMCPAuditWriteListAndRedaction 验证审计写入、分页过滤，并确保秘密与明文卡密不入库。
func TestMCPAuditWriteListAndRedaction(t *testing.T) {
	// store、cleanup 是临时仓储及其关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本用例上下文。
	ctx := context.Background()

	// first 是成功的工具调用审计，参数中混入秘密、卡密与嵌套结构，写入前必须全部脱敏。
	first := MCPAuditRecord{
		CreatedAt:   mcpTestNow.Unix(),
		UserID:      1,
		TokenSource: MCPTokenSourcePersisted,
		Category:    MCPAuditCategoryTool,
		Name:        "account_set_remark",
		CookieID:    "cid-1",
		Arguments: `{"account_id":"cid-1","remark":"新备注","ai_api_key":"sk-secret-1234",
			"cards":["1111","2222"],"nested":{"token":"abc"},"confirm":true,"page":2}`,
		Success:    true,
		DurationMS: 12,
	}
	// firstErr 是第一条审计的写入错误。
	if firstErr := store.MCP.AddMCPAudit(ctx, first); firstErr != nil {
		t.Fatalf("写入第一条审计失败: %v", firstErr)
	}
	// second 是失败的资源读取审计，参数为空，令牌来源为环境引导令牌。
	second := MCPAuditRecord{
		CreatedAt: mcpTestNow.Unix() + 1, UserID: 1, TokenSource: MCPTokenSourceEnvironment,
		Category: MCPAuditCategoryResource, Name: "ydisks://accounts", CookieID: "",
		Success: false, ErrorClass: "not_found", DurationMS: 3,
	}
	// secondErr 是第二条审计的写入错误。
	if secondErr := store.MCP.AddMCPAudit(ctx, second); secondErr != nil {
		t.Fatalf("写入第二条审计失败: %v", secondErr)
	}

	// page、err 是无过滤分页结果，按时间倒序应先返回第二条。
	page, err := store.MCP.ListMCPAudit(ctx, MCPAuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("查询审计失败: %v", err)
	}
	if page.Total != 2 || len(page.Records) != 2 {
		t.Fatalf("审计总数与条数异常: total=%d len=%d", page.Total, len(page.Records))
	}
	if page.Records[0].Name != "ydisks://accounts" {
		t.Fatal("审计必须按时间倒序排列")
	}
	// firstRow 是第一条成功记录，检查脱敏结果只保留白名单标量。
	firstRow := page.Records[1]
	if !firstRow.Success || firstRow.TokenSource != MCPTokenSourcePersisted {
		t.Fatalf("第一条记录字段异常: %+v", firstRow)
	}
	// secret 是不允许出现在审计摘要中的秘密或卡密片段。
	for _, secret := range []string{"sk-secret-1234", "1111", "2222", "abc", "新备注"} {
		if strings.Contains(firstRow.Arguments, secret) {
			t.Fatalf("审计参数泄漏秘密或卡密内容 %q: %s", secret, firstRow.Arguments)
		}
	}
	// safe 是必须保留的白名单键值。
	for _, safe := range []string{"cid-1", `"confirm":true`, `"page":2`} {
		if !strings.Contains(firstRow.Arguments, safe) {
			t.Fatalf("白名单安全键应保留，缺少 %q，实际 %s", safe, firstRow.Arguments)
		}
	}

	// failedPage、err 是只看失败调用的过滤结果，应仅含第二条。
	failedPage, err := store.MCP.ListMCPAudit(ctx, MCPAuditFilter{SuccessState: -1})
	if err != nil || failedPage.Total != 1 || failedPage.Records[0].ErrorClass != "not_found" {
		t.Fatalf("失败过滤异常: %+v err=%v", failedPage, err)
	}
	// successPage、err 是只看成功调用的过滤结果，应仅含第一条。
	successPage, err := store.MCP.ListMCPAudit(ctx, MCPAuditFilter{SuccessState: 1})
	if err != nil || successPage.Total != 1 || successPage.Records[0].Name != "account_set_remark" {
		t.Fatalf("成功过滤异常: %+v err=%v", successPage, err)
	}
	// namedPage、err 是按名称精确过滤的结果，应仅含第一条。
	namedPage, err := store.MCP.ListMCPAudit(ctx, MCPAuditFilter{Name: "account_set_remark"})
	if err != nil || namedPage.Total != 1 || namedPage.Records[0].CookieID != "cid-1" {
		t.Fatalf("名称过滤异常: %+v err=%v", namedPage, err)
	}
	// single、err 是页大小为 1 的分页结果，总数不变且只返回首条。
	single, err := store.MCP.ListMCPAudit(ctx, MCPAuditFilter{Limit: 1})
	if err != nil || single.Total != 2 || len(single.Records) != 1 {
		t.Fatalf("分页行为异常: %+v err=%v", single, err)
	}
}

// TestSanitizeMCPAuditArgumentsInvalidInputs 验证非对象、非法 JSON 与超长字符串的边界处理。
func TestSanitizeMCPAuditArgumentsInvalidInputs(t *testing.T) {
	// cases 是各类不应原样落库的输入。
	cases := []string{"", "   ", "not-json", "[1,2,3]", `"scalar"`, `{"unexpected":{}}`}
	// index、input 是当前用例序号与其原始输入。
	for index, input := range cases {
		// out 是脱敏结果；除空串外的非法输入必须回落空串。
		out := SanitizeMCPAuditArguments(input)
		if out != "" {
			t.Fatalf("用例 %d 非法输入必须脱敏为空串，实际 %q", index, out)
		}
	}
	// long 是超过截断上限的白名单字符串，脱敏后必须被截断且不再包含尾部标记。
	long := strings.Repeat("a", mcpAuditValueLimit+50)
	// out 是长字符串的脱敏结果。
	out := SanitizeMCPAuditArguments(`{"status":"` + long + `"}`)
	if strings.Contains(out, strings.Repeat("a", mcpAuditValueLimit+1)) {
		t.Fatalf("超长白名单值必须截断: len=%d", len(out))
	}
	if !strings.HasSuffix(out, "…\"}") {
		t.Fatalf("截断结果应以省略号与 JSON 收尾: %s", out)
	}
}
