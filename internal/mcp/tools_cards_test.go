// tools_cards_test.go 覆盖卡密库存域工具：明文卡密与 API 密钥零泄漏扫描，
// CRUD/追加/连通性测试的成功、越权、未找到与 confirm 守卫路径。

package mcp

import (
	"context"
	"strings"
	"testing"

	cardsapp "xianyu-go/internal/application/cards"
)

// cardDataSecret 是 data 卡券组库存正文中的第一条模拟明文卡密。
const cardDataSecret = "ZM-SECRET-7421-视频会员月卡"

// cardDataSecretSecond 是第二条模拟明文卡密，用于验证多行库存计数。
const cardDataSecretSecond = "ZM-SECRET-8830-视频会员季卡"

// cardAPISecret 是 API 请求头中的模拟密钥，任何 MCP 输出都不得包含该片段。
const cardAPISecret = "AKIASECRETBAR9911"

// fakeCardPorts 是卡密库存域假端口，记录全部用例调用并支持按方法注入错误。
type fakeCardPorts struct {
	// cards 是列表用例返回的卡券组集合。
	cards []cardsapp.Card
	// cardsByID 是详情用例按标识返回的卡券组。
	cardsByID map[int64]cardsapp.Card
	// getErrs 是详情用例按标识注入的错误。
	getErrs map[int64]error
	// listErr 是列表用例注入的错误。
	listErr error
	// createErr 是创建用例注入的错误。
	createErr error
	// updateErr 是更新用例注入的错误。
	updateErr error
	// deleteErr 是删除用例注入的错误。
	deleteErr error
	// appendErr 是追加库存用例注入的错误。
	appendErr error
	// testErr 是连通性测试用例注入的错误。
	testErr error
	// createID 是创建成功时回传的新卡券组标识。
	createID int64
	// appendedLines 是追加成功时回传的新增库存行数。
	appendedLines int
	// testResult 是连通性测试成功时回传的非敏感诊断。
	testResult cardsapp.APIRequestTestResult
	// listUserID 记录列表用例收到的用户标识。
	listUserID int64
	// getRecords 记录详情用例每次收到的用户标识与卡券组标识。
	getRecords [][2]int64
	// createCalls 记录创建用例调用次数。
	createCalls int
	// createUserID 记录创建用例最近一次收到的用户标识。
	createUserID int64
	// createdDraft 记录创建用例最近一次收到的草稿。
	createdDraft cardsapp.Draft
	// updateCalls 记录更新用例调用次数。
	updateCalls int
	// updateUserID 记录更新用例最近一次收到的用户标识。
	updateUserID int64
	// updatedID 记录更新用例最近一次收到的卡券组标识。
	updatedID int64
	// updatedDraft 记录更新用例最近一次收到的草稿。
	updatedDraft cardsapp.Draft
	// deleteCalls 记录删除用例调用次数。
	deleteCalls int
	// deleteUserID 记录删除用例最近一次收到的用户标识。
	deleteUserID int64
	// deletedID 记录删除用例最近一次收到的卡券组标识。
	deletedID int64
	// appendCalls 记录追加库存用例调用次数。
	appendCalls int
	// appendUserID 记录追加用例最近一次收到的用户标识。
	appendUserID int64
	// appendedID 记录追加用例最近一次收到的卡券组标识。
	appendedID int64
	// appendedContent 记录追加用例最近一次收到的库存正文（仅测试内存，不进任何输出）。
	appendedContent string
	// testCalls 记录连通性测试用例调用次数。
	testCalls int
	// testUserID 记录连通性测试最近一次收到的用户标识。
	testUserID int64
	// testedID 记录连通性测试最近一次收到的卡券组标识。
	testedID int64
}

// ListCards 记录用户标识并回传预设卡券组集合。
func (f *fakeCardPorts) ListCards(_ context.Context, userID int64) ([]cardsapp.Card, error) {
	f.listUserID = userID
	return f.cards, f.listErr
}

// GetCard 记录用户与卡券组标识，按注入错误或预设详情回传；都缺失时返回未找到。
func (f *fakeCardPorts) GetCard(_ context.Context, userID, cardID int64) (cardsapp.Card, error) {
	f.getRecords = append(f.getRecords, [2]int64{userID, cardID})
	// err、injected 表示该标识是否被测试显式注入了错误。
	if err, injected := f.getErrs[cardID]; injected {
		return cardsapp.Card{}, err
	}
	// card、ok 是预设的卡券组详情。
	if card, ok := f.cardsByID[cardID]; ok {
		return card, nil
	}
	return cardsapp.Card{}, cardsapp.ErrNotFound
}

// CreateCard 记录创建调用、用户与草稿并回传预设标识。
func (f *fakeCardPorts) CreateCard(_ context.Context, userID int64, draft cardsapp.Draft) (int64, error) {
	f.createCalls++
	f.createUserID = userID
	f.createdDraft = draft
	return f.createID, f.createErr
}

// UpdateCard 记录更新调用、用户、标识与草稿。
func (f *fakeCardPorts) UpdateCard(_ context.Context, userID, cardID int64, draft cardsapp.Draft) error {
	f.updateCalls++
	f.updateUserID = userID
	f.updatedID = cardID
	f.updatedDraft = draft
	return f.updateErr
}

// DeleteCard 记录删除调用、用户与标识。
func (f *fakeCardPorts) DeleteCard(_ context.Context, userID, cardID int64) error {
	f.deleteCalls++
	f.deleteUserID = userID
	f.deletedID = cardID
	return f.deleteErr
}

// AppendCardData 记录追加调用、用户、标识与库存正文并回传预设行数。
func (f *fakeCardPorts) AppendCardData(_ context.Context, userID, cardID int64, content string) (int, error) {
	f.appendCalls++
	f.appendUserID = userID
	f.appendedID = cardID
	f.appendedContent = content
	return f.appendedLines, f.appendErr
}

// TestCardAPI 记录连通性测试调用、用户与标识并回传预设诊断。
func (f *fakeCardPorts) TestCardAPI(_ context.Context, userID, cardID int64) (cardsapp.APIRequestTestResult, error) {
	f.testCalls++
	f.testUserID = userID
	f.testedID = cardID
	return f.testResult, f.testErr
}

// newCardToolEndpoint 构造注册卡密库存域工具的端点、假端口与管理员身份上下文。
func newCardToolEndpoint(t *testing.T, fake *fakeCardPorts) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterCardTools(fake)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// newLeakFakePorts 构造含明文卡密与 API 密钥夹具的假端口，模拟生产仓储的脱敏与非脱敏两种形态。
func newLeakFakePorts() *fakeCardPorts {
	// stock 是含两行明文卡密的库存正文。
	stock := cardDataSecret + "\r\n  \n" + cardDataSecretSecond + "\n"
	// secretAPIConfig 是请求头携带密钥的完整 API 配置，生产路径只在应用层内部短暂出现。
	secretAPIConfig := `{"url":"https://api.example.com/deliver","method":"POST","timeout_seconds":5,` +
		`"content_type":"application/json","headers":{"X-Api-Key":"` + cardAPISecret + `"},"params":{},"body":{}}`
	// dataCard 是列表与详情共用的 data 卡券组，应用模型带明文库存，DTO 必须只给出行数。
	dataCard := cardsapp.Card{
		ID: 10, UserID: 1, Name: "视频会员", Type: "data",
		Description: "发货用", Enabled: true, DelaySeconds: 0, DataContent: stock,
	}
	// apiCardSummary 是列表路径中数据库已脱敏的 api 卡券组形态：只带摘要不带配置。
	apiCardSummary := cardsapp.Card{
		ID: 11, UserID: 1, Name: "接口取卡", Type: "api", Enabled: true,
		APIConfigSummary: &cardsapp.APIConfigSummary{
			URL: "https://api.example.com/deliver", Method: "POST", TimeoutSeconds: 5,
			ContentType: "application/json", HeadersConfigured: true, Ready: true,
		},
	}
	// apiCardFull 是详情路径模拟：未带摘要但带完整密钥配置，验证摘要化逻辑同样不泄漏。
	apiCardFull := cardsapp.Card{ID: 11, UserID: 1, Name: "接口取卡", Type: "api", Enabled: true, APIConfig: secretAPIConfig}
	return &fakeCardPorts{
		cards:         []cardsapp.Card{dataCard, apiCardSummary},
		cardsByID:     map[int64]cardsapp.Card{10: dataCard, 11: apiCardFull},
		getErrs:       map[int64]error{},
		createID:      77,
		appendedLines: 2,
		testResult: cardsapp.APIRequestTestResult{
			Status: "success", StatusCode: 200, ResponseContentType: "application/json",
			ResponseFields: []string{"code", "data"}, ExtractedValue: "CARD-OK", ResponsePreview: `{"code":0}`,
		},
	}
}

// assertNoCardSecret 断言响应文本不含任何模拟明文卡密、密钥或敏感请求头名称。
func assertNoCardSecret(t *testing.T, text string) {
	t.Helper()
	// secret 是当前扫描的秘密片段。
	for _, secret := range []string{cardDataSecret, cardDataSecretSecond, cardAPISecret, "X-Api-Key"} {
		if strings.Contains(text, secret) {
			t.Fatalf("卡密域响应泄漏秘密片段 %q: %s", secret, text)
		}
	}
}

// TestCardViewsNeverLeakSecrets 验证列表、详情、错误与追加确认输出对明文卡密和 API 密钥零泄漏（TR-7.1）。
func TestCardViewsNeverLeakSecrets(t *testing.T) {
	// fake 是含秘密夹具的假端口。
	fake := newLeakFakePorts()
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// outputs 收集全部待扫描的工具输出文本。
	outputs := []string{}
	// listResult 是卡券组列表结果。
	listResult := invoke(endpoint, ctx, "card_list", nil)
	if listResult.IsError {
		t.Fatalf("卡券组列表失败: %s", resultJSON(t, listResult))
	}
	// listText 是列表 JSON，必须含两行库存计数与请求头已配置标记。
	listText := resultJSON(t, listResult)
	if !strings.Contains(listText, `"stock_lines":2`) || !strings.Contains(listText, `"headers_configured":true`) || !strings.Contains(listText, `"total":2`) {
		t.Fatalf("卡券组列表映射异常: %s", listText)
	}
	outputs = append(outputs, listText)
	// dataDetail 是 data 卡券组详情。
	dataDetail := invoke(endpoint, ctx, "card_get", map[string]any{"card_id": 10.0})
	if dataDetail.IsError {
		t.Fatalf("data 卡券组详情失败: %s", resultJSON(t, dataDetail))
	}
	outputs = append(outputs, resultJSON(t, dataDetail))
	// apiDetail 是 api 卡券组详情，摘要化路径也必须剔除请求头密钥。
	apiDetail := invoke(endpoint, ctx, "card_get", map[string]any{"card_id": 11.0})
	if apiDetail.IsError {
		t.Fatalf("api 卡券组详情失败: %s", resultJSON(t, apiDetail))
	}
	// apiText 是 api 详情 JSON，必须只含脱敏摘要。
	apiText := resultJSON(t, apiDetail)
	if !strings.Contains(apiText, `"ready":true`) || !strings.Contains(apiText, "api.example.com") {
		t.Fatalf("api 卡券组摘要异常: %s", apiText)
	}
	outputs = append(outputs, apiText)
	// notFound 与 forbidden 是两类错误路径输出，同样不得夹带秘密。
	fake.getErrs[404] = cardsapp.ErrNotFound
	fake.getErrs[403] = cardsapp.ErrForbidden
	outputs = append(outputs,
		resultJSON(t, invoke(endpoint, ctx, "card_get", map[string]any{"card_id": 404.0})),
		resultJSON(t, invoke(endpoint, ctx, "card_get", map[string]any{"card_id": 403.0})),
	)
	// appended 是追加库存的确认输出，只允许含标识与行数。
	appended := invoke(endpoint, ctx, "card_append_data", map[string]any{
		"card_id": 10.0, "data_content": cardDataSecret + "\n" + cardDataSecretSecond,
	})
	if appended.IsError {
		t.Fatalf("追加库存失败: %s", resultJSON(t, appended))
	}
	// appendedText 是追加确认 JSON。
	appendedText := resultJSON(t, appended)
	if !strings.Contains(appendedText, `"card_id":10`) || !strings.Contains(appendedText, `"appended_lines":2`) {
		t.Fatalf("追加确认返回异常: %s", appendedText)
	}
	outputs = append(outputs, appendedText)
	// text 是当前扫描的工具输出；任一输出含秘密片段即失败。
	for _, text := range outputs {
		assertNoCardSecret(t, text)
	}
	// 列表与详情必须透传固定管理员身份。
	if fake.listUserID != 1 || len(fake.getRecords) == 0 || fake.getRecords[0][0] != 1 {
		t.Fatalf("卡券工具未透传固定管理员身份: list=%d records=%v", fake.listUserID, fake.getRecords)
	}
}

// TestCardCreateValidationAndIdentity 验证创建入参映射、身份透传与非法参数零触达。
func TestCardCreateValidationAndIdentity(t *testing.T) {
	// fake 是回传固定新标识的假端口。
	fake := &fakeCardPorts{createID: 77, getErrs: map[int64]error{}}
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// created 是合法文本卡券组的创建结果。
	created := invoke(endpoint, ctx, "card_create", map[string]any{
		"name": "文本卡A", "type": "text", "text_content": "发货话术", "delay_seconds": 30.0,
	})
	if created.IsError {
		t.Fatalf("创建卡券组失败: %s", resultJSON(t, created))
	}
	if fake.createCalls != 1 || fake.createUserID != 1 || fake.createdDraft.Type != "text" || fake.createdDraft.DelaySeconds != 30 {
		t.Fatalf("创建草稿或身份透传异常: calls=%d user=%d draft=%+v", fake.createCalls, fake.createUserID, fake.createdDraft)
	}
	// badType 是非法类型入参，必须在触达应用用例前拒绝。
	badType := invoke(endpoint, ctx, "card_create", map[string]any{"name": "x", "type": "voice"})
	if !badType.IsError || fake.createCalls != 1 {
		t.Fatalf("非法类型必须零触达: calls=%d", fake.createCalls)
	}
	// missingType 是缺少类型的必填校验。
	missingType := invoke(endpoint, ctx, "card_create", map[string]any{"name": "x"})
	if !missingType.IsError || fake.createCalls != 1 {
		t.Fatal("缺少类型必须零触达")
	}
	// badDelay 是超出范围的发货延迟，必须零触达。
	badDelay := invoke(endpoint, ctx, "card_create", map[string]any{"name": "x", "type": "text", "delay_seconds": 7200.0})
	if !badDelay.IsError || fake.createCalls != 1 {
		t.Fatal("非法延迟必须零触达")
	}
}

// TestCardUpdateConfirmGatesStockReplace 验证元数据更新无需确认而覆盖库存必须显式确认。
func TestCardUpdateConfirmGates(t *testing.T) {
	// fake 是详情返回现有 data 卡券组的假端口。
	fake := &fakeCardPorts{
		cardsByID: map[int64]cardsapp.Card{10: {ID: 10, UserID: 1, Name: "旧名称", Type: "data", Enabled: true}},
		getErrs:   map[int64]error{},
	}
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// metadata 是只改名称与开关的元数据更新，不应要求 confirm。
	metadata := invoke(endpoint, ctx, "card_update", map[string]any{"card_id": 10.0, "name": "新名称", "enabled": false})
	if metadata.IsError {
		t.Fatalf("元数据更新失败: %s", resultJSON(t, metadata))
	}
	if fake.updateCalls != 1 || fake.updatedDraft.DataContentSet || fake.updatedDraft.Name != "新名称" || fake.updatedDraft.Enabled {
		t.Fatalf("元数据更新草稿异常: %+v", fake.updatedDraft)
	}
	// replaceDenied 是缺 confirm 的库存覆盖，必须零触达。
	replaceDenied := invoke(endpoint, ctx, "card_update", map[string]any{"card_id": 10.0, "data_content": "新行A\n新行B"})
	if !replaceDenied.IsError || fake.updateCalls != 1 {
		t.Fatal("缺 confirm 覆盖库存必须拦截且零触达")
	}
	// replaceAllowed 是显式确认后的库存覆盖。
	replaceAllowed := invoke(endpoint, ctx, "card_update", map[string]any{
		"card_id": 10.0, "data_content": "新行A\n新行B", "confirm": true,
	})
	if replaceAllowed.IsError || fake.updateCalls != 2 {
		t.Fatalf("带 confirm 覆盖库存应执行: %s", resultJSON(t, replaceAllowed))
	}
	if !fake.updatedDraft.DataContentSet || fake.updatedDraft.Type != "data" || fake.updateUserID != 1 || fake.updatedID != 10 {
		t.Fatalf("覆盖库存草稿或身份异常: %+v", fake.updatedDraft)
	}
}

// TestCardDeleteRequiresConfirm 验证删除卡券组缺 confirm 零触达，显式确认后透传标识与身份。
func TestCardDeleteRequiresConfirm(t *testing.T) {
	// fake 是删除用例假端口。
	fake := &fakeCardPorts{getErrs: map[int64]error{}}
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// denied 是缺 confirm 的删除。
	denied := invoke(endpoint, ctx, "card_delete", map[string]any{"card_id": 10.0})
	if !denied.IsError || fake.deleteCalls != 0 {
		t.Fatal("缺 confirm 删除必须拦截且零触达")
	}
	// allowed 是显式确认后的删除。
	allowed := invoke(endpoint, ctx, "card_delete", map[string]any{"card_id": 10.0, "confirm": true})
	if allowed.IsError || fake.deleteCalls != 1 || fake.deletedID != 10 || fake.deleteUserID != 1 {
		t.Fatalf("带 confirm 删除应透传: %s", resultJSON(t, allowed))
	}
}

// TestCardAppendPaths 验证追加库存的成功映射、身份透传与类型错误归一。
func TestCardAppendPaths(t *testing.T) {
	// fake 是追加用例假端口。
	fake := &fakeCardPorts{appendedLines: 3, getErrs: map[int64]error{}}
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// ok 是合法追加结果。
	ok := invoke(endpoint, ctx, "card_append_data", map[string]any{"card_id": 10.0, "data_content": "A\nB\nC"})
	if ok.IsError {
		t.Fatalf("追加库存失败: %s", resultJSON(t, ok))
	}
	if fake.appendCalls != 1 || fake.appendedID != 10 || fake.appendUserID != 1 || fake.appendedContent != "A\nB\nC" {
		t.Fatalf("追加透传异常: calls=%d id=%d user=%d content=%q", fake.appendCalls, fake.appendedID, fake.appendUserID, fake.appendedContent)
	}
	// okText 是追加确认 JSON，必须回传新增行数。
	okText := resultJSON(t, ok)
	if !strings.Contains(okText, `"appended_lines":3`) {
		t.Fatalf("追加行数映射异常: %s", okText)
	}
	// 非 data 类型的追加被应用层拒绝时，错误归一为中文入参类提示。
	fake.appendErr = cardsapp.ErrNotDataType
	// wrongType 是对非 data 卡券组追加的错误结果。
	wrongType := invoke(endpoint, ctx, "card_append_data", map[string]any{"card_id": 11.0, "data_content": "A"})
	if !wrongType.IsError || !strings.Contains(resultJSON(t, wrongType), "只有 data（逐行卡密）类型") {
		t.Fatalf("非 data 类型追加应返回中文类型错误: %s", resultJSON(t, wrongType))
	}
}

// TestCardTestAPIConfirmAndResult 验证连通性测试 confirm 守卫、诊断 DTO 映射与装配错误归一。
func TestCardTestAPIConfirmAndResult(t *testing.T) {
	// fake 是回传成功诊断的测试假端口。
	fake := &fakeCardPorts{
		testResult: cardsapp.APIRequestTestResult{
			Status: "success", StatusCode: 200, ResponseContentType: "application/json",
			ResponseFields: []string{"code", "data"}, ExtractedValue: "CARD-OK",
		},
		getErrs: map[int64]error{},
	}
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// denied 是缺 confirm 的连通性测试，必须零触达。
	denied := invoke(endpoint, ctx, "card_test_api", map[string]any{"card_id": 11.0})
	if !denied.IsError || fake.testCalls != 0 {
		t.Fatal("缺 confirm 连通性测试必须拦截且零触达")
	}
	// allowed 是显式确认后的连通性测试。
	allowed := invoke(endpoint, ctx, "card_test_api", map[string]any{"card_id": 11.0, "confirm": true})
	if allowed.IsError {
		t.Fatalf("连通性测试失败: %s", resultJSON(t, allowed))
	}
	if fake.testCalls != 1 || fake.testedID != 11 || fake.testUserID != 1 {
		t.Fatalf("连通性测试透传异常: calls=%d id=%d user=%d", fake.testCalls, fake.testedID, fake.testUserID)
	}
	// text 是诊断 JSON，必须含状态码与响应字段名且不含请求头密钥。
	text := resultJSON(t, allowed)
	// fragment 是诊断 JSON 中必须存在的响应片段。
	for _, fragment := range []string{`"card_id":11`, `"status":"success"`, `"status_code":200`, `"response_fields":["code","data"]`, `"extracted_value":"CARD-OK"`} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("连通性诊断缺少 %s: %s", fragment, text)
		}
	}
	// 测试组件未装配时归一为中文内部错误提示。
	fake.testErr = cardsapp.ErrAPITesterUnavailable
	// unavailable 是组件缺失的错误结果。
	unavailable := invoke(endpoint, ctx, "card_test_api", map[string]any{"card_id": 11.0, "confirm": true})
	if !unavailable.IsError || !strings.Contains(resultJSON(t, unavailable), "测试组件") {
		t.Fatalf("测试组件缺失应返回中文提示: %s", resultJSON(t, unavailable))
	}
}

// TestCardErrorMapping 验证卡券哨兵与校验错误映射为对应稳定中文类别文本。
func TestCardErrorMapping(t *testing.T) {
	// fake 是按标识注入各类应用错误的假端口。
	fake := &fakeCardPorts{
		getErrs: map[int64]error{
			404: cardsapp.ErrNotFound,
			403: cardsapp.ErrForbidden,
			422: &cardsapp.ValidationError{Message: "名称和类型不能为空"},
		},
	}
	// endpoint、ctx 是卡密工具端点与身份上下文。
	endpoint, ctx := newCardToolEndpoint(t, fake)
	// cases 是错误文本与期望中文片段的对照。
	cases := []struct {
		// id 是触发错误的卡券组标识。
		id int64
		// want 是归一后必须包含的中文提示。
		want string
	}{
		{id: 404, want: "卡券组不存在"},
		{id: 403, want: "无权操作该卡券组"},
		{id: 422, want: "名称和类型不能为空"},
	}
	// testCase 是当前对照用例。
	for _, testCase := range cases {
		// result 是该标识的详情错误结果。
		result := invoke(endpoint, ctx, "card_get", map[string]any{"card_id": float64(testCase.id)})
		// text 是错误中文文本。
		text := resultJSON(t, result)
		if !result.IsError || !strings.Contains(text, testCase.want) {
			t.Fatalf("标识 %d 错误归一异常: %s", testCase.id, text)
		}
		assertNoCardSecret(t, text)
	}
	// missingID 是缺少必填标识的参数错误。
	missingID := invoke(endpoint, ctx, "card_get", map[string]any{})
	if !missingID.IsError || !strings.Contains(resultJSON(t, missingID), "缺少必填参数 card_id") {
		t.Fatalf("缺少标识必须返回参数错误: %s", resultJSON(t, missingID))
	}
}

// TestCardToolAnnotations 验证删除与连通性测试标记为破坏性，其余卡券工具不标记。
func TestCardToolAnnotations(t *testing.T) {
	// endpoint、_ 是注册了卡密工具的端点。
	endpoint, _ := newCardToolEndpoint(t, &fakeCardPorts{getErrs: map[int64]error{}})
	// destructive 是必须标记破坏性的工具集合。
	destructive := map[string]bool{"card_delete": true, "card_test_api": true}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		if !strings.HasPrefix(def.Name, "card_") {
			continue
		}
		if destructive[def.Name] && !def.Destructive {
			t.Fatalf("工具 %s 必须标记破坏性", def.Name)
		}
		if !destructive[def.Name] && def.Destructive {
			t.Fatalf("工具 %s 不应标记破坏性", def.Name)
		}
	}
}
