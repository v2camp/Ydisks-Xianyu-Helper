// tools_automation_test.go 覆盖自动化规则、发货模板、默认回复与关键词回复域工具：
// 规则写入必须经过服务端规范化端口且 dry-run 零写入、confirm 零触达与错误归一。

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	keywordsapp "xianyu-go/internal/application/keywords"
)

// normalizedRuleName 是假端口模拟服务端生成的规则名称，用于断言 MCP 未重建校验。
const normalizedRuleName = "付款后自动发货（服务端生成）"

// fakeRulePorts 是规则域假端口，记录每个用例调用并支持注入错误。
type fakeRulePorts struct {
	// rules 是列表与分页用例返回的规则。
	rules []automationapp.Rule
	// total 是分页用例返回的总数。
	total int
	// counts 是触发类型统计结果。
	counts map[string]int
	// normalizeErr、createErr、updateErr、deleteErr 分别是各阶段注入错误。
	normalizeErr, createErr, updateErr, deleteErr error
	// createID 是创建成功返回的规则标识。
	createID int64
	// pageFilter、countFilter 是分页与统计收到的过滤条件。
	pageFilter, countFilter automationapp.RuleFilter
	// normalizeUserID、normalizeDraft 是创建语义规范化收到的身份与草稿。
	normalizeUserID int64
	normalizeDraft  automationapp.RuleDraft
	// updateDraft、updateRuleID 是更新语义规范化收到的草稿与规则标识。
	updateDraft  automationapp.RuleDraft
	updateRuleID int64
	// createInput、updateInput 是最终写入的规范化结果。
	createInput, updateInput automationapp.RuleInput
	// deletedRuleID 是删除用例收到的规则标识。
	deletedRuleID int64
	// listCalls、pageCalls、countCalls、normalizeCalls、normalizeUpdateCalls、createCalls、updateCalls、deleteCalls 是各用例调用次数。
	listCalls, pageCalls, countCalls, normalizeCalls, normalizeUpdateCalls, createCalls, updateCalls, deleteCalls int
}

// ListRules 记录调用并回传预设规则列表。
func (f *fakeRulePorts) ListRules(context.Context, int64) ([]automationapp.Rule, error) {
	f.listCalls++
	return f.rules, nil
}

// ListRulesPage 记录过滤条件并回传预设分页结果。
func (f *fakeRulePorts) ListRulesPage(_ context.Context, filter automationapp.RuleFilter) ([]automationapp.Rule, int, error) {
	f.pageCalls++
	f.pageFilter = filter
	return f.rules, f.total, nil
}

// CountRulesByTrigger 记录过滤条件并回传预设统计。
func (f *fakeRulePorts) CountRulesByTrigger(_ context.Context, filter automationapp.RuleFilter) (map[string]int, error) {
	f.countCalls++
	f.countFilter = filter
	return f.counts, nil
}

// NormalizeRule 模拟服务端创建语义规范化：只返回结果，不写入任何数据。
func (f *fakeRulePorts) NormalizeRule(_ context.Context, userID int64, draft automationapp.RuleDraft) (automationapp.RuleInput, error) {
	f.normalizeCalls++
	f.normalizeUserID = userID
	f.normalizeDraft = draft
	if f.normalizeErr != nil {
		return automationapp.RuleInput{}, f.normalizeErr
	}
	return normalizedRuleInput(userID, draft), nil
}

// NormalizeRuleForUpdate 模拟服务端更新语义规范化：只返回结果，不写入任何数据。
func (f *fakeRulePorts) NormalizeRuleForUpdate(_ context.Context, userID, ruleID int64, draft automationapp.RuleDraft) (automationapp.RuleInput, error) {
	f.normalizeUpdateCalls++
	f.normalizeUserID = userID
	f.updateRuleID = ruleID
	f.updateDraft = draft
	if f.normalizeErr != nil {
		return automationapp.RuleInput{}, f.normalizeErr
	}
	return normalizedRuleInput(userID, draft), nil
}

// CreateRule 记录创建输入并返回预设标识。
func (f *fakeRulePorts) CreateRule(_ context.Context, input automationapp.RuleInput) (int64, error) {
	f.createCalls++
	f.createInput = input
	return f.createID, f.createErr
}

// UpdateRule 记录更新输入。
func (f *fakeRulePorts) UpdateRule(_ context.Context, _, ruleID int64, input automationapp.RuleInput) error {
	f.updateCalls++
	f.updateRuleID = ruleID
	f.updateInput = input
	return f.updateErr
}

// DeleteRule 记录删除标识。
func (f *fakeRulePorts) DeleteRule(_ context.Context, _, ruleID int64) error {
	f.deleteCalls++
	f.deletedRuleID = ruleID
	return f.deleteErr
}

// normalizedRuleInput 构造带服务端默认值的规范化结果，用于断言 MCP 直接透传而非自行校验。
func normalizedRuleInput(userID int64, draft automationapp.RuleDraft) automationapp.RuleInput {
	// actions 是模拟服务端补齐默认值后的动作列表。
	actions := make([]automationapp.ActionInput, 0, len(draft.Actions))
	// index、action 是草稿动作序号与内容。
	for index, action := range draft.Actions {
		// enabled 是模拟服务端默认启用逻辑。
		enabled := true
		if action.Enabled != nil {
			enabled = *action.Enabled
		}
		// count 是模拟服务端最小发送数量约束。
		count := action.DeliveryCount
		if count <= 0 {
			count = 1
		}
		// sortOrder 是模拟服务端补齐的动作顺序。
		sortOrder := action.SortOrder
		if sortOrder <= 0 {
			sortOrder = index + 1
		}
		actions = append(actions, automationapp.ActionInput{
			ID: action.ID, ActionType: action.ActionType, CardID: action.CardID, DeliveryCount: count,
			MessageTemplate: action.MessageTemplate, DelaySeconds: action.DelaySeconds,
			ConfigJSON: action.ConfigJSON, Enabled: enabled, SortOrder: sortOrder,
			DeliveryTemplateID: action.DeliveryTemplateID, TemplateBindings: action.TemplateBindings,
			CustomVariables: action.CustomVariables,
		})
	}
	return automationapp.RuleInput{
		UserID: userID, CookieID: draft.CookieID, ItemID: draft.ItemID, Name: normalizedRuleName,
		TriggerType: draft.TriggerType, Enabled: draft.Enabled, Priority: 100,
		ConfigJSON: "{}", SKUMigrationStatus: "ready", Actions: actions,
	}
}

// fakeTemplatePorts 是发货模板域假端口。
type fakeTemplatePorts struct {
	// templates 是列表用例返回的模板。
	templates []deliveryapp.Template
	// template 是详情用例返回的模板。
	template deliveryapp.Template
	// createID 是创建成功返回的模板标识。
	createID int64
	// deleteErr 是删除用例注入错误。
	deleteErr error
	// createDraft、updateDraft 是写入用例收到的草稿。
	createDraft, updateDraft deliveryapp.Draft
	// listCalls、getCalls、createCalls、updateCalls、deleteCalls 是各用例调用次数。
	listCalls, getCalls, createCalls, updateCalls, deleteCalls int
}

// ListTemplates 回传预设模板列表。
func (f *fakeTemplatePorts) ListTemplates(context.Context, int64) ([]deliveryapp.Template, error) {
	f.listCalls++
	return f.templates, nil
}

// GetTemplate 回传预设模板详情。
func (f *fakeTemplatePorts) GetTemplate(context.Context, int64, int64) (deliveryapp.Template, error) {
	f.getCalls++
	return f.template, nil
}

// CreateTemplate 记录创建草稿并返回预设标识。
func (f *fakeTemplatePorts) CreateTemplate(_ context.Context, _ int64, draft deliveryapp.Draft) (int64, error) {
	f.createCalls++
	f.createDraft = draft
	return f.createID, nil
}

// UpdateTemplate 记录更新草稿。
func (f *fakeTemplatePorts) UpdateTemplate(_ context.Context, _, _ int64, draft deliveryapp.Draft) error {
	f.updateCalls++
	f.updateDraft = draft
	return nil
}

// DeleteTemplate 记录删除调用并返回注入错误。
func (f *fakeTemplatePorts) DeleteTemplate(context.Context, int64, int64) error {
	f.deleteCalls++
	return f.deleteErr
}

// fakeDefaultReplyPorts 是默认回复域假端口。
type fakeDefaultReplyPorts struct {
	// summaries 是列表用例返回的配置。
	summaries []defaultreplyapp.Summary
	// reply 是读取用例返回的配置。
	reply defaultreplyapp.Reply
	// listErr、getErr 是列表与读取用例注入错误。
	listErr, getErr error
	// upserted 是保存用例收到的配置。
	upserted defaultreplyapp.Reply
	// upsertAccount 是保存用例收到的账号标识。
	upsertAccount string
	// listCalls、getCalls、upsertCalls、deleteCalls、clearCalls 是各用例调用次数。
	listCalls, getCalls, upsertCalls, deleteCalls, clearCalls int
}

// ListDefaultReplies 回传预设配置列表。
func (f *fakeDefaultReplyPorts) ListDefaultReplies(context.Context, int64) ([]defaultreplyapp.Summary, error) {
	f.listCalls++
	return f.summaries, f.listErr
}

// GetDefaultReply 回传预设配置。
func (f *fakeDefaultReplyPorts) GetDefaultReply(context.Context, int64, string) (defaultreplyapp.Reply, error) {
	f.getCalls++
	return f.reply, f.getErr
}

// UpsertDefaultReply 记录保存内容。
func (f *fakeDefaultReplyPorts) UpsertDefaultReply(_ context.Context, _ int64, cookieID string, reply defaultreplyapp.Reply) error {
	f.upsertCalls++
	f.upsertAccount = cookieID
	f.upserted = reply
	return nil
}

// DeleteDefaultReply 记录删除调用。
func (f *fakeDefaultReplyPorts) DeleteDefaultReply(context.Context, int64, string) error {
	f.deleteCalls++
	return nil
}

// ClearDefaultReplyRecords 记录记录清理调用。
func (f *fakeDefaultReplyPorts) ClearDefaultReplyRecords(context.Context, int64, string) error {
	f.clearCalls++
	return nil
}

// fakeKeywordPorts 是关键词与指定商品回复域假端口。
type fakeKeywordPorts struct {
	// keywords 是列表用例返回的关键词规则。
	keywords []keywordsapp.Keyword
	// itemReplies 是商品回复列表用例返回的数据。
	itemReplies []keywordsapp.ItemReply
	// itemReply 是商品回复读取用例返回的数据。
	itemReply keywordsapp.ItemReply
	// addID 是新增成功返回的标识。
	addID int64
	// addErr、replaceErr、updateErr、deleteErr 是写入用例注入错误。
	addErr, replaceErr, updateErr, deleteErr error
	// addDraft、updateDraft 是写入用例收到的草稿。
	addDraft, updateDraft keywordsapp.Draft
	// replaceDrafts 是批量替换收到的草稿。
	replaceDrafts []keywordsapp.Draft
	// deletedID 是按标识删除收到的标识。
	deletedID int64
	// deletedIndex 是按序号删除收到的序号。
	deletedIndex int
	// setContent 是商品回复写入收到的正文。
	setContent string
	// listCalls、addCalls、replaceCalls、updateCalls、deleteCalls、deleteIndexCalls、itemListCalls、itemGetCalls、itemSetCalls、itemDeleteCalls 是各用例调用次数。
	listCalls, addCalls, replaceCalls, updateCalls, deleteCalls, deleteIndexCalls int
	itemListCalls, itemGetCalls, itemSetCalls, itemDeleteCalls                    int
}

// ListKeywords 回传预设关键词规则。
func (f *fakeKeywordPorts) ListKeywords(context.Context, int64, string) ([]keywordsapp.Keyword, error) {
	f.listCalls++
	return f.keywords, nil
}

// AddKeyword 记录新增草稿并返回预设标识。
func (f *fakeKeywordPorts) AddKeyword(_ context.Context, _ int64, _ string, draft keywordsapp.Draft) (int64, error) {
	f.addCalls++
	f.addDraft = draft
	return f.addID, f.addErr
}

// ReplaceKeywords 记录批量替换草稿。
func (f *fakeKeywordPorts) ReplaceKeywords(_ context.Context, _ int64, _ string, drafts []keywordsapp.Draft) error {
	f.replaceCalls++
	f.replaceDrafts = drafts
	return f.replaceErr
}

// UpdateKeyword 记录更新草稿。
func (f *fakeKeywordPorts) UpdateKeyword(_ context.Context, _ int64, _ string, _ int64, draft keywordsapp.Draft) error {
	f.updateCalls++
	f.updateDraft = draft
	return f.updateErr
}

// DeleteKeywordByID 记录按标识删除。
func (f *fakeKeywordPorts) DeleteKeywordByID(_ context.Context, _ int64, _ string, keywordID int64) error {
	f.deleteCalls++
	f.deletedID = keywordID
	return f.deleteErr
}

// DeleteKeywordByIndex 记录按序号删除。
func (f *fakeKeywordPorts) DeleteKeywordByIndex(_ context.Context, _ int64, _ string, index int) error {
	f.deleteIndexCalls++
	f.deletedIndex = index
	return f.deleteErr
}

// ListItemReplies 回传预设商品回复列表。
func (f *fakeKeywordPorts) ListItemReplies(context.Context, int64) ([]keywordsapp.ItemReply, error) {
	f.itemListCalls++
	return f.itemReplies, nil
}

// GetItemReply 回传预设商品回复。
func (f *fakeKeywordPorts) GetItemReply(context.Context, int64, string, string) (keywordsapp.ItemReply, error) {
	f.itemGetCalls++
	return f.itemReply, nil
}

// SetItemReply 记录商品回复写入正文。
func (f *fakeKeywordPorts) SetItemReply(_ context.Context, _ int64, _, _, content string) error {
	f.itemSetCalls++
	f.setContent = content
	return nil
}

// DeleteItemReply 记录商品回复删除调用。
func (f *fakeKeywordPorts) DeleteItemReply(context.Context, int64, string, string) error {
	f.itemDeleteCalls++
	return nil
}

// newRuleToolEndpoint 构造注册规则工具的端点、假端口与身份上下文。
func newRuleToolEndpoint(t *testing.T, fake *fakeRulePorts) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterRuleTools(fake)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// ruleCreateArgs 构造一份合法的规则创建入参。
func ruleCreateArgs() map[string]any {
	return map[string]any{
		"account_id": "acc1", "trigger_type": "order_paid", "enabled": true,
		"actions": []any{
			map[string]any{"action_type": "send_card", "card_id": 7.0, "delivery_count": 2.0},
			map[string]any{"action_type": "confirm_shipment", "sort_order": 2.0, "enabled": false},
		},
	}
}

// TestRuleListAndCountsMapping 验证规则分页、全量与触发类型计数的过滤与映射。
func TestRuleListAndCountsMapping(t *testing.T) {
	// fake 是规则域假端口。
	fake := &fakeRulePorts{
		rules: []automationapp.Rule{{
			ID: 3, CookieID: "acc1", ItemID: "i1", Name: "付款后自动发货", TriggerType: "order_paid", Enabled: true, Priority: 100,
			Actions: []automationapp.Action{{ID: 5, ActionType: "send_card", CardID: 7, CardName: "视频会员", DeliveryCount: 1, Enabled: true, SortOrder: 1}},
		}},
		total:  1,
		counts: map[string]int{"order_paid": 1},
	}
	// endpoint、ctx 是规则工具端点与身份上下文。
	endpoint, ctx := newRuleToolEndpoint(t, fake)
	// pageResult 是分页查询结果。
	pageResult := invoke(endpoint, ctx, "rule_list", map[string]any{"account_id": "acc1", "enabled": true, "page": 2.0, "page_size": 5.0})
	if pageResult.IsError {
		t.Fatalf("规则分页失败: %s", resultJSON(t, pageResult))
	}
	// pageText 是分页 JSON，必须含触发统计、总数与动作视图。
	pageText := resultJSON(t, pageResult)
	// fragment 是分页响应中必须出现的片段。
	for _, fragment := range []string{`"total":1`, `"page":2`, `"total_pages":1`, `"trigger_counts":{"order_paid":1}`, `"card_name":"视频会员"`} {
		if !strings.Contains(pageText, fragment) {
			t.Fatalf("规则分页缺少 %s: %s", fragment, pageText)
		}
	}
	if fake.pageFilter.CookieID != "acc1" || fake.pageFilter.Enabled == nil || !*fake.pageFilter.Enabled ||
		fake.pageFilter.Limit != 5 || fake.pageFilter.Offset != 5 {
		t.Fatalf("分页过滤条件透传异常: %+v", fake.pageFilter)
	}
	// allResult 是全量列表结果。
	allResult := invoke(endpoint, ctx, "rule_list_all", nil)
	if allResult.IsError || !strings.Contains(resultJSON(t, allResult), `"rule_id":3`) {
		t.Fatalf("规则全量列表异常: %s", resultJSON(t, allResult))
	}
	// countsResult 是触发类型计数结果。
	countsResult := invoke(endpoint, ctx, "rule_trigger_counts", map[string]any{"trigger_type": "order_paid"})
	if countsResult.IsError {
		t.Fatalf("触发类型计数失败: %s", resultJSON(t, countsResult))
	}
	// countsText 是计数 JSON，总数应由各触发类型计数求和得到。
	countsText := resultJSON(t, countsResult)
	if !strings.Contains(countsText, `"counts":{"order_paid":1}`) || !strings.Contains(countsText, `"total":1`) {
		t.Fatalf("触发类型计数映射异常: %s", countsText)
	}
	if fake.listCalls != 1 || fake.pageCalls != 1 || fake.countCalls != 2 {
		t.Fatalf("查询用例调用次数异常: list=%d page=%d count=%d", fake.listCalls, fake.pageCalls, fake.countCalls)
	}
}

// TestRulePreviewDryRunAndNormalizePort 验证 dry-run 预览经过规范化端口且不产生任何写入（TR-8.1）。
func TestRulePreviewDryRunAndNormalizePort(t *testing.T) {
	// fake 是规则域假端口。
	fake := &fakeRulePorts{createID: 12}
	// endpoint、ctx 是规则工具端点与身份上下文。
	endpoint, ctx := newRuleToolEndpoint(t, fake)
	// preview 是创建语义的 dry-run 预览。
	preview := invoke(endpoint, ctx, "rule_preview", ruleCreateArgs())
	if preview.IsError {
		t.Fatalf("规则预览失败: %s", resultJSON(t, preview))
	}
	// previewText 是预览 JSON，必须标记 dry_run 并回传服务端规范化结果。
	previewText := resultJSON(t, preview)
	// fragment 是预览响应中必须出现的片段。
	for _, fragment := range []string{`"dry_run":true`, `"name":"` + normalizedRuleName + `"`, `"priority":100`, `"sku_migration_status":"ready"`} {
		if !strings.Contains(previewText, fragment) {
			t.Fatalf("规则预览缺少 %s: %s", fragment, previewText)
		}
	}
	if fake.normalizeCalls != 1 || fake.createCalls != 0 || fake.updateCalls != 0 || fake.deleteCalls != 0 {
		t.Fatalf("dry-run 必须零写入: normalize=%d create=%d update=%d delete=%d", fake.normalizeCalls, fake.createCalls, fake.updateCalls, fake.deleteCalls)
	}
	if fake.normalizeDraft.CookieID != "acc1" || fake.normalizeDraft.TriggerType != "order_paid" || len(fake.normalizeDraft.Actions) != 2 {
		t.Fatalf("预览入参映射异常: %+v", fake.normalizeDraft)
	}
	// 第二个动作显式提交 enabled=false 与 sort_order=2，动作字段必须完整透传。
	// disabled 是第二个动作草稿。
	disabled := fake.normalizeDraft.Actions[1]
	if disabled.Enabled == nil || *disabled.Enabled || disabled.SortOrder != 2 {
		t.Fatalf("动作开关与顺序映射异常: %+v", disabled)
	}
	// 更新语义预览必须改走 NormalizeForUpdate。
	updateArgs := ruleCreateArgs()
	updateArgs["rule_id"] = 9.0
	updateArgs["actions"] = []any{map[string]any{"action_type": "send_card", "action_id": 5.0, "card_id": 7.0}}
	// updatePreview 是更新语义的 dry-run 预览。
	updatePreview := invoke(endpoint, ctx, "rule_preview", updateArgs)
	if updatePreview.IsError || fake.normalizeUpdateCalls != 1 || fake.updateRuleID != 9 {
		t.Fatalf("更新语义预览异常: %s", resultJSON(t, updatePreview))
	}
	if fake.normalizeDraft.Actions[0].ID != 0 || fake.updateDraft.Actions[0].ID != 5 {
		t.Fatalf("动作标识透传异常: create=%+v update=%+v", fake.normalizeDraft.Actions[0], fake.updateDraft.Actions[0])
	}
}

// TestRuleCreateAndUpdatePassNormalizedInput 验证写入工具先规范化再落库，且直接透传规范化结果。
func TestRuleCreateAndUpdatePassNormalizedInput(t *testing.T) {
	// fake 是规则域假端口。
	fake := &fakeRulePorts{createID: 12}
	// endpoint、ctx 是规则工具端点与身份上下文。
	endpoint, ctx := newRuleToolEndpoint(t, fake)
	// created 是规则创建结果。
	created := invoke(endpoint, ctx, "rule_create", ruleCreateArgs())
	if created.IsError {
		t.Fatalf("规则创建失败: %s", resultJSON(t, created))
	}
	if !strings.Contains(resultJSON(t, created), `"rule_id":12`) {
		t.Fatalf("创建结果缺少 rule_id: %s", resultJSON(t, created))
	}
	if fake.normalizeCalls != 1 || fake.createCalls != 1 || fake.createInput.Name != normalizedRuleName ||
		fake.createInput.SKUMigrationStatus != "ready" || fake.createInput.Priority != 100 {
		t.Fatalf("创建必须先经规范化端口并透传结果: input=%+v", fake.createInput)
	}
	if fake.createInput.UserID != 1 || fake.createInput.Actions[0].DeliveryCount != 2 {
		t.Fatalf("创建输入身份或动作映射异常: %+v", fake.createInput)
	}
	// updateArgs 是带 rule_id 的规则更新入参。
	updateArgs := ruleCreateArgs()
	updateArgs["rule_id"] = 9.0
	// updated 是规则更新结果。
	updated := invoke(endpoint, ctx, "rule_update", updateArgs)
	if updated.IsError {
		t.Fatalf("规则更新失败: %s", resultJSON(t, updated))
	}
	if fake.normalizeUpdateCalls != 1 || fake.updateCalls != 1 || fake.updateRuleID != 9 || fake.updateInput.Name != normalizedRuleName {
		t.Fatalf("更新必须先经更新语义规范化端口: id=%d input=%+v", fake.updateRuleID, fake.updateInput)
	}
	// 服务端拒绝草稿（例如缺少动作）时不得产生任何写入，错误原因原样回传。
	fake.normalizeErr = &automationapp.ValidationError{Message: "至少需要一个自动化动作"}
	// rejected 是被服务端拒绝的创建结果。
	rejected := invoke(endpoint, ctx, "rule_create", map[string]any{"account_id": "acc1", "trigger_type": "order_paid"})
	if !rejected.IsError || fake.createCalls != 1 {
		t.Fatalf("服务端拒绝时不得写入: %s", resultJSON(t, rejected))
	}
	if !strings.Contains(resultJSON(t, rejected), "至少需要一个自动化动作") {
		t.Fatalf("服务端校验原因未回传: %s", resultJSON(t, rejected))
	}
	fake.normalizeErr = nil
}

// TestRuleValidationErrorSurfacesMessage 验证规则校验错误以中文原因返回且不自动重试。
func TestRuleValidationErrorSurfacesMessage(t *testing.T) {
	// fake 是注入规则校验错误的假端口。
	fake := &fakeRulePorts{normalizeErr: &automationapp.ValidationError{Message: "拍下未付款规则至少需要一个已启用的改价动作"}}
	// endpoint、ctx 是规则工具端点与身份上下文。
	endpoint, ctx := newRuleToolEndpoint(t, fake)
	// result 是被服务端拒绝的创建结果。
	result := invoke(endpoint, ctx, "rule_create", ruleCreateArgs())
	if !result.IsError {
		t.Fatal("规则校验失败必须返回错误结果")
	}
	if !strings.Contains(resultJSON(t, result), "拍下未付款规则至少需要一个已启用的改价动作") {
		t.Fatalf("校验错误未回传中文原因: %s", resultJSON(t, result))
	}
	if fake.normalizeCalls != 1 || fake.createCalls != 0 {
		t.Fatalf("校验失败不得写入: normalize=%d create=%d", fake.normalizeCalls, fake.createCalls)
	}
	// 规则不存在与运行残留错误必须映射为对应中文提示。
	fake.normalizeErr = automationapp.ErrRuleNotFound
	// notFound 是更新不存在规则的错误结果。
	notFound := invoke(endpoint, ctx, "rule_update", map[string]any{
		"rule_id": 4.0, "account_id": "acc1", "trigger_type": "order_paid",
		"actions": []any{map[string]any{"action_type": "send_card", "card_id": 7.0}},
	})
	if !strings.Contains(resultJSON(t, notFound), "自动化规则不存在") {
		t.Fatalf("规则不存在映射异常: %s", resultJSON(t, notFound))
	}
	fake.deleteErr = automationapp.ErrRuleActive
	// active 是删除仍有运行规则时的错误结果。
	active := invoke(endpoint, ctx, "rule_delete", map[string]any{"rule_id": 4.0, "confirm": true})
	if !strings.Contains(resultJSON(t, active), "仍有待处理的运行") {
		t.Fatalf("规则运行残留映射异常: %s", resultJSON(t, active))
	}
}

// TestRuleDeleteRequiresConfirm 验证规则删除缺 confirm 零触达。
func TestRuleDeleteRequiresConfirm(t *testing.T) {
	// fake 是规则域假端口。
	fake := &fakeRulePorts{}
	// endpoint、ctx 是规则工具端点与身份上下文。
	endpoint, ctx := newRuleToolEndpoint(t, fake)
	// denied 是缺 confirm 的删除。
	denied := invoke(endpoint, ctx, "rule_delete", map[string]any{"rule_id": 4.0})
	if !denied.IsError || fake.deleteCalls != 0 {
		t.Fatal("缺 confirm 删除规则必须拦截且零触达")
	}
	// allowed 是显式确认后的删除。
	allowed := invoke(endpoint, ctx, "rule_delete", map[string]any{"rule_id": 4.0, "confirm": true})
	if allowed.IsError || fake.deleteCalls != 1 || fake.deletedRuleID != 4 {
		t.Fatalf("带 confirm 删除应执行: %s", resultJSON(t, allowed))
	}
}

// TestTemplateToolsMappingAndConfirm 验证发货模板 CRUD 的成功路径与删除 confirm 守卫（TR-8.2）。
func TestTemplateToolsMappingAndConfirm(t *testing.T) {
	// fake 是发货模板域假端口。
	fake := &fakeTemplatePorts{
		templates: []deliveryapp.Template{{ID: 2, Name: "发货模板", Enabled: true,
			Messages: []deliveryapp.Message{{ID: 1, SortOrder: 1, Content: "卡密：{code}"}}, Keys: []string{"code"}}},
		template: deliveryapp.Template{ID: 2, Name: "发货模板", Enabled: false,
			Messages: []deliveryapp.Message{{ID: 1, SortOrder: 1, Content: "卡密：{code}"}}, Keys: []string{"code"}},
		createID: 8,
	}
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterDeliveryTemplateTools(fake)
	// ctx 是携带固定管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	// listResult 是模板列表结果。
	listResult := invoke(endpoint, ctx, "template_list", nil)
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"keys":["code"]`) {
		t.Fatalf("模板列表映射异常: %s", resultJSON(t, listResult))
	}
	// getResult 是模板详情结果。
	getResult := invoke(endpoint, ctx, "template_get", map[string]any{"template_id": 2.0})
	if getResult.IsError {
		t.Fatalf("模板详情失败: %s", resultJSON(t, getResult))
	}
	// getText 是详情 JSON，必须含消息正文与变量键。
	getText := resultJSON(t, getResult)
	if !strings.Contains(getText, `"content":"卡密：{code}"`) || !strings.Contains(getText, `"enabled":false`) {
		t.Fatalf("模板详情映射异常: %s", getText)
	}
	// created 是模板创建结果。
	created := invoke(endpoint, ctx, "template_create", map[string]any{
		"name": "新模板", "enabled": true, "messages": []any{"第一条 {code}", "第二条"},
	})
	if created.IsError || fake.createCalls != 1 || fake.createDraft.Name != "新模板" || len(fake.createDraft.Messages) != 2 {
		t.Fatalf("模板创建映射异常: %s", resultJSON(t, created))
	}
	// updated 是模板更新结果。
	updated := invoke(endpoint, ctx, "template_update", map[string]any{
		"template_id": 2.0, "name": "改名", "messages": []any{"新消息"},
	})
	if updated.IsError || fake.updateCalls != 1 || fake.updateDraft.Name != "改名" || len(fake.updateDraft.Messages) != 1 {
		t.Fatalf("模板更新映射异常: %s", resultJSON(t, updated))
	}
	// denied 是缺 confirm 的删除。
	denied := invoke(endpoint, ctx, "template_delete", map[string]any{"template_id": 2.0})
	if !denied.IsError || fake.deleteCalls != 0 {
		t.Fatal("缺 confirm 删除模板必须拦截且零触达")
	}
	// 被规则引用的模板删除必须回传中文原因。
	fake.deleteErr = deliveryapp.ErrReferenced
	// referenced 是引用冲突的删除结果。
	referenced := invoke(endpoint, ctx, "template_delete", map[string]any{"template_id": 2.0, "confirm": true})
	if !strings.Contains(resultJSON(t, referenced), "仍被自动化规则引用") {
		t.Fatalf("模板引用冲突映射异常: %s", resultJSON(t, referenced))
	}
	fake.deleteErr = nil
	// allowed 是显式确认后的删除。
	allowed := invoke(endpoint, ctx, "template_delete", map[string]any{"template_id": 2.0, "confirm": true})
	if allowed.IsError || fake.deleteCalls != 2 {
		t.Fatalf("带 confirm 删除模板应执行: %s", resultJSON(t, allowed))
	}
}

// TestDefaultReplyToolsMappingAndConfirm 验证默认回复读写与清理的 confirm 守卫（TR-8.2）。
func TestDefaultReplyToolsMappingAndConfirm(t *testing.T) {
	// fake 是默认回复域假端口。
	fake := &fakeDefaultReplyPorts{
		summaries: []defaultreplyapp.Summary{{CookieID: "acc1", Reply: defaultreplyapp.Reply{Enabled: true, ReplyContent: "您好", ReplyOnce: true}}},
		reply:     defaultreplyapp.Reply{Enabled: true, ReplyContent: "您好", ReplyImageURL: "https://example.com/a.png"},
	}
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterDefaultReplyTools(fake)
	// ctx 是携带固定管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	// listResult 是默认回复列表结果。
	listResult := invoke(endpoint, ctx, "default_reply_list", nil)
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"reply_once":true`) {
		t.Fatalf("默认回复列表异常: %s", resultJSON(t, listResult))
	}
	// getResult 是默认回复读取结果。
	getResult := invoke(endpoint, ctx, "default_reply_get", map[string]any{"account_id": "acc1"})
	if getResult.IsError || !strings.Contains(resultJSON(t, getResult), "https://example.com/a.png") {
		t.Fatalf("默认回复读取异常: %s", resultJSON(t, getResult))
	}
	// setResult 是默认回复保存结果。
	setResult := invoke(endpoint, ctx, "default_reply_set", map[string]any{
		"account_id": "acc1", "enabled": true, "reply_content": "在的", "reply_image_url": "https://example.com/b.png", "reply_once": true,
	})
	if setResult.IsError || fake.upsertCalls != 1 || fake.upserted.ReplyContent != "在的" || !fake.upserted.ReplyOnce {
		t.Fatalf("默认回复保存映射异常: %s", resultJSON(t, setResult))
	}
	// deleteDenied、clearDenied 是缺 confirm 的两类破坏性操作。
	deleteDenied := invoke(endpoint, ctx, "default_reply_delete", map[string]any{"account_id": "acc1"})
	// clearDenied 是缺 confirm 的记录清理。
	clearDenied := invoke(endpoint, ctx, "default_reply_clear_records", map[string]any{"account_id": "acc1"})
	if !deleteDenied.IsError || !clearDenied.IsError || fake.deleteCalls != 0 || fake.clearCalls != 0 {
		t.Fatal("缺 confirm 的默认回复删除与清理必须零触达")
	}
	// deleteAllowed、clearAllowed 是显式确认后的操作。
	deleteAllowed := invoke(endpoint, ctx, "default_reply_delete", map[string]any{"account_id": "acc1", "confirm": true})
	// clearAllowed 是显式确认后的记录清理。
	clearAllowed := invoke(endpoint, ctx, "default_reply_clear_records", map[string]any{"account_id": "acc1", "confirm": true})
	if deleteAllowed.IsError || clearAllowed.IsError || fake.deleteCalls != 1 || fake.clearCalls != 1 {
		t.Fatal("带 confirm 的默认回复删除与清理应执行")
	}
	// 未配置默认回复时错误归一为未找到。
	fake.getErr = defaultreplyapp.ErrConfigNotFound
	// notFound 是未配置账号的读取结果。
	notFound := invoke(endpoint, ctx, "default_reply_get", map[string]any{"account_id": "acc2"})
	if !strings.Contains(resultJSON(t, notFound), "尚未配置默认回复") {
		t.Fatalf("默认回复未找到映射异常: %s", resultJSON(t, notFound))
	}
}

// TestKeywordToolsMappingAndConfirm 验证关键词回复 CRUD 与破坏性操作守卫（TR-8.2）。
func TestKeywordToolsMappingAndConfirm(t *testing.T) {
	// fake 是关键词域假端口。
	fake := &fakeKeywordPorts{
		keywords: []keywordsapp.Keyword{{ID: 6, CookieID: "acc1", Keyword: "在吗", Reply: "在的", Type: "text"}},
		addID:    6,
	}
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterKeywordTools(fake)
	// ctx 是携带固定管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	// listResult 是关键词列表结果。
	listResult := invoke(endpoint, ctx, "keyword_list", map[string]any{"account_id": "acc1"})
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"keyword":"在吗"`) {
		t.Fatalf("关键词列表异常: %s", resultJSON(t, listResult))
	}
	// added 是关键词新增结果。
	added := invoke(endpoint, ctx, "keyword_add", map[string]any{"account_id": "acc1", "keyword": "发货", "reply": "马上发"})
	if added.IsError || fake.addCalls != 1 || fake.addDraft.Keyword != "发货" || fake.addDraft.Reply != "马上发" {
		t.Fatalf("关键词新增映射异常: %s", resultJSON(t, added))
	}
	// updated 是关键词更新结果。
	updated := invoke(endpoint, ctx, "keyword_update", map[string]any{
		"account_id": "acc1", "keyword_id": 6.0, "keyword": "在吗", "type": "image", "image_url": "https://example.com/a.png",
	})
	if updated.IsError || fake.updateCalls != 1 || fake.updateDraft.Type != "image" || fake.updateDraft.ImageURL == "" {
		t.Fatalf("关键词更新映射异常: %s", resultJSON(t, updated))
	}
	// replaceDenied 是缺 confirm 的批量替换。
	replaceDenied := invoke(endpoint, ctx, "keyword_replace", map[string]any{
		"account_id": "acc1", "keywords": []any{map[string]any{"keyword": "a", "reply": "b"}},
	})
	if !replaceDenied.IsError || fake.replaceCalls != 0 {
		t.Fatal("缺 confirm 批量替换必须拦截且零触达")
	}
	// replaced 是显式确认后的批量替换。
	replaced := invoke(endpoint, ctx, "keyword_replace", map[string]any{
		"account_id": "acc1", "confirm": true,
		"keywords": []any{map[string]any{"keyword": "a", "reply": "b"}, map[string]any{"keyword": "c", "reply": "d"}},
	})
	if replaced.IsError || fake.replaceCalls != 1 || len(fake.replaceDrafts) != 2 {
		t.Fatalf("批量替换映射异常: %s", resultJSON(t, replaced))
	}
	// 替换结果必须提示原有规则已被删除。
	if !strings.Contains(resultJSON(t, replaced), "原有未提交的关键词规则已被删除") {
		t.Fatalf("批量替换缺少风险提示: %s", resultJSON(t, replaced))
	}
	// deleteDenied、indexDenied 是缺 confirm 的两种删除。
	deleteDenied := invoke(endpoint, ctx, "keyword_delete", map[string]any{"account_id": "acc1", "keyword_id": 6.0})
	// indexDenied 是缺 confirm 的按序号删除。
	indexDenied := invoke(endpoint, ctx, "keyword_delete_by_index", map[string]any{"account_id": "acc1", "index": 0.0})
	if !deleteDenied.IsError || !indexDenied.IsError || fake.deleteCalls != 0 || fake.deleteIndexCalls != 0 {
		t.Fatal("缺 confirm 的关键词删除必须零触达")
	}
	// deleteAllowed、indexAllowed 是显式确认后的删除。
	deleteAllowed := invoke(endpoint, ctx, "keyword_delete", map[string]any{"account_id": "acc1", "keyword_id": 6.0, "confirm": true})
	// indexAllowed 是显式确认后的按序号删除。
	indexAllowed := invoke(endpoint, ctx, "keyword_delete_by_index", map[string]any{"account_id": "acc1", "index": 1.0, "confirm": true})
	if deleteAllowed.IsError || indexAllowed.IsError || fake.deleteCalls != 1 || fake.deleteIndexCalls != 1 ||
		fake.deletedID != 6 || fake.deletedIndex != 1 {
		t.Fatalf("带 confirm 关键词删除应执行: %s / %s", resultJSON(t, deleteAllowed), resultJSON(t, indexAllowed))
	}
	// 校验错误必须回传服务端中文原因且不写入。
	fake.addErr = &keywordsapp.ValidationError{Message: "文字回复内容不能为空"}
	// partial 是缺回复内容的无效新增。
	partial := invoke(endpoint, ctx, "keyword_add", map[string]any{"account_id": "acc1", "keyword": "a"})
	if !strings.Contains(resultJSON(t, partial), "文字回复内容不能为空") {
		t.Fatalf("关键词校验错误映射异常: %s", resultJSON(t, partial))
	}
}

// TestItemReplyToolsMappingAndConfirm 验证指定商品回复读写与删除守卫。
func TestItemReplyToolsMappingAndConfirm(t *testing.T) {
	// fake 是关键词域假端口（含商品回复能力）。
	fake := &fakeKeywordPorts{
		itemReplies: []keywordsapp.ItemReply{{ItemID: "i1", CookieID: "acc1", ReplyContent: "专属回复"}},
		itemReply:   keywordsapp.ItemReply{ItemID: "i1", CookieID: "acc1", ReplyContent: "专属回复"},
	}
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterKeywordTools(fake)
	// ctx 是携带固定管理员身份的上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	// listResult 是商品回复列表结果。
	listResult := invoke(endpoint, ctx, "item_reply_list", nil)
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"item_id":"i1"`) {
		t.Fatalf("商品回复列表异常: %s", resultJSON(t, listResult))
	}
	// getResult 是商品回复读取结果。
	getResult := invoke(endpoint, ctx, "item_reply_get", map[string]any{"account_id": "acc1", "item_id": "i1"})
	if getResult.IsError || !strings.Contains(resultJSON(t, getResult), "专属回复") {
		t.Fatalf("商品回复读取异常: %s", resultJSON(t, getResult))
	}
	// setResult 是商品回复写入结果。
	setResult := invoke(endpoint, ctx, "item_reply_set", map[string]any{"account_id": "acc1", "item_id": "i1", "reply_content": "新回复"})
	if setResult.IsError || fake.itemSetCalls != 1 || fake.setContent != "新回复" {
		t.Fatalf("商品回复写入异常: %s", resultJSON(t, setResult))
	}
	// denied 是缺 confirm 的删除。
	denied := invoke(endpoint, ctx, "item_reply_delete", map[string]any{"account_id": "acc1", "item_id": "i1"})
	if !denied.IsError || fake.itemDeleteCalls != 0 {
		t.Fatal("缺 confirm 删除商品回复必须零触达")
	}
	// allowed 是显式确认后的删除。
	allowed := invoke(endpoint, ctx, "item_reply_delete", map[string]any{"account_id": "acc1", "item_id": "i1", "confirm": true})
	if allowed.IsError || fake.itemDeleteCalls != 1 {
		t.Fatalf("带 confirm 删除商品回复应执行: %s", resultJSON(t, allowed))
	}
	// 缺少商品标识必须返回参数错误且零触达。
	missing := invoke(endpoint, ctx, "item_reply_set", map[string]any{"account_id": "acc1", "reply_content": "x"})
	if !missing.IsError || fake.itemSetCalls != 1 || !strings.Contains(resultJSON(t, missing), "缺少必填参数 item_id") {
		t.Fatalf("缺少商品标识必须返回参数错误: %s", resultJSON(t, missing))
	}
}

// TestAutomationDomainDestructiveInventory 验证自动化配置域的破坏性工具清单与注解一致。
func TestAutomationDomainDestructiveInventory(t *testing.T) {
	// destructive 是必须标记破坏性的工具集合。
	destructive := map[string]bool{
		"rule_delete": true, "template_delete": true, "default_reply_delete": true,
		"default_reply_clear_records": true, "keyword_replace": true, "keyword_delete": true,
		"keyword_delete_by_index": true, "item_reply_delete": true,
	}
	// endpoint、_ 是注册全部自动化配置域工具的端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterRuleTools(&fakeRulePorts{})
	endpoint.RegisterDeliveryTemplateTools(&fakeTemplatePorts{})
	endpoint.RegisterDefaultReplyTools(&fakeDefaultReplyPorts{})
	endpoint.RegisterKeywordTools(&fakeKeywordPorts{})
	// seen 是实际注册到的破坏性工具集合。
	seen := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		if def.Destructive {
			seen[def.Name] = true
		}
	}
	if len(seen) != len(destructive) {
		t.Fatalf("破坏性工具数量不一致: got=%v want=%v", seen, destructive)
	}
	// name 是期望的破坏性工具名。
	for name := range destructive {
		if !seen[name] {
			t.Fatalf("工具 %s 必须标记破坏性", name)
		}
	}
	// 端口为 nil 时对应域工具不得注册。
	empty, _ := newProtocolEndpoint(t)
	empty.RegisterRuleTools(nil)
	empty.RegisterDeliveryTemplateTools(nil)
	empty.RegisterDefaultReplyTools(nil)
	empty.RegisterKeywordTools(nil)
	// defs 是端口缺失时注册到的工具清单，必须为空。
	if defs := empty.ToolDefs(); len(defs) != 0 {
		t.Fatalf("端口缺失时不应注册任何工具，实际 %d 个", len(defs))
	}
}

// TestAutomationDomainToolInventory 核对自动化配置域注册的工具清单完整且无越界工具。
func TestAutomationDomainToolInventory(t *testing.T) {
	// expected 是 Task 8 要求注册的全部工具名。
	expected := []string{
		"rule_list", "rule_list_all", "rule_trigger_counts", "rule_preview", "rule_create", "rule_update", "rule_delete",
		"template_list", "template_get", "template_create", "template_update", "template_delete",
		"default_reply_list", "default_reply_get", "default_reply_set", "default_reply_delete", "default_reply_clear_records",
		"keyword_list", "keyword_add", "keyword_replace", "keyword_update", "keyword_delete", "keyword_delete_by_index",
		"item_reply_list", "item_reply_get", "item_reply_set", "item_reply_delete",
	}
	// endpoint 是注册全部自动化配置域工具后的端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterRuleTools(&fakeRulePorts{})
	endpoint.RegisterDeliveryTemplateTools(&fakeTemplatePorts{})
	endpoint.RegisterDefaultReplyTools(&fakeDefaultReplyPorts{})
	endpoint.RegisterKeywordTools(&fakeKeywordPorts{})
	// registered 是实际注册的工具名集合。
	registered := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		registered[def.Name] = true
	}
	if len(registered) != len(expected) {
		t.Fatalf("自动化配置域工具数量异常: got=%d want=%d", len(registered), len(expected))
	}
	// name 是期望注册的工具名。
	for _, name := range expected {
		if !registered[name] {
			t.Fatalf("缺少工具 %s", name)
		}
	}
}

// TestRuleToolErrorsAreChinese 验证规则入参解码错误返回中文提示。
func TestRuleToolErrorsAreChinese(t *testing.T) {
	// fake 是规则域假端口。
	fake := &fakeRulePorts{}
	// endpoint、ctx 是规则工具端点与身份上下文。
	endpoint, ctx := newRuleToolEndpoint(t, fake)
	// badShape 是 actions 元素类型错误的入参。
	badShape := invoke(endpoint, ctx, "rule_create", map[string]any{
		"account_id": "acc1", "trigger_type": "order_paid", "actions": []any{"不是对象"},
	})
	if !badShape.IsError || fake.normalizeCalls != 0 {
		t.Fatalf("非法 actions 结构必须零触达: %s", resultJSON(t, badShape))
	}
	// message 是归一后的错误文本。
	message := resultJSON(t, badShape)
	if !strings.Contains(message, "工具入参字段类型") && !strings.Contains(message, "缺少必填参数") {
		t.Fatalf("入参错误未归一为可读中文: %s", message)
	}
	// 规则删除错误必须可由 errors.Is 识别，保证上层分类稳定。
	if class, _ := Classify(automationapp.ErrRuleNotFound); class != ClassNotFound {
		t.Fatalf("规则不存在类别异常: %s", class)
	}
	// 发货模板校验错误沿用服务端中文提示。
	if class, publicMessage := Classify(deliveryapp.ErrInvalidInput); class != ClassInvalidArgument || !strings.Contains(publicMessage, "发货模板") {
		t.Fatalf("模板校验错误类别异常: %s %s", class, publicMessage)
	}
	// 空规则校验错误提示保持稳定。
	if class, publicMessage := Classify(errors.New("unclassified")); class != ClassInternal || publicMessage == "" {
		t.Fatalf("未归类错误应归一为内部错误: %s %s", class, publicMessage)
	}
}
