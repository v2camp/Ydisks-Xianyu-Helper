package runtime

// mcp_automation_adapter.go 把自动化规则、发货模板、默认回复与关键词回复应用服务
// 投影为 internal/mcp 的消费者端口。适配器只做透传：
// 规则校验、模板变量契约与回复内容归一全部留在应用服务，组合层不复制业务规则。

import (
	"context"

	automationapp "xianyu-go/internal/application/automation"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	keywordsapp "xianyu-go/internal/application/keywords"
	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
)

// mcpRulePorts 把规则应用服务投影为 MCP 规则端口。
type mcpRulePorts struct {
	// service 是自动化规则应用服务。
	service *automationapp.RuleService
}

// 编译期断言适配器满足 MCP 规则端口。
var _ capability.RulePorts = (*mcpRulePorts)(nil)

// ListRules 透传规则全量列表用例。
func (a *mcpRulePorts) ListRules(ctx context.Context, userID int64) ([]automationapp.Rule, error) {
	return a.service.ListForUser(ctx, userID)
}

// ListRulesPage 透传规则分页查询用例。
func (a *mcpRulePorts) ListRulesPage(ctx context.Context, filter automationapp.RuleFilter) ([]automationapp.Rule, int, error) {
	return a.service.ListPageForUser(ctx, filter)
}

// CountRulesByTrigger 透传规则触发类型统计用例。
func (a *mcpRulePorts) CountRulesByTrigger(ctx context.Context, filter automationapp.RuleFilter) (map[string]int, error) {
	return a.service.CountByTriggerForUser(ctx, filter)
}

// NormalizeRule 透传创建语义的规则规范化校验；不产生写入。
func (a *mcpRulePorts) NormalizeRule(ctx context.Context, userID int64, draft automationapp.RuleDraft) (automationapp.RuleInput, error) {
	return a.service.Normalize(ctx, userID, draft)
}

// NormalizeRuleForUpdate 透传更新语义的规则规范化校验；不产生写入。
func (a *mcpRulePorts) NormalizeRuleForUpdate(ctx context.Context, userID, ruleID int64, draft automationapp.RuleDraft) (automationapp.RuleInput, error) {
	return a.service.NormalizeForUpdate(ctx, userID, ruleID, draft)
}

// CreateRule 透传已规范化规则的创建用例。
func (a *mcpRulePorts) CreateRule(ctx context.Context, input automationapp.RuleInput) (int64, error) {
	return a.service.Create(ctx, input)
}

// UpdateRule 透传规则更新用例。
func (a *mcpRulePorts) UpdateRule(ctx context.Context, userID, ruleID int64, input automationapp.RuleInput) error {
	return a.service.Update(ctx, userID, ruleID, input)
}

// DeleteRule 透传规则删除用例。
func (a *mcpRulePorts) DeleteRule(ctx context.Context, userID, ruleID int64) error {
	return a.service.Delete(ctx, userID, ruleID)
}

// mcpDeliveryTemplatePorts 把发货模板应用服务投影为 MCP 模板端口。
type mcpDeliveryTemplatePorts struct {
	// service 是发货模板应用服务。
	service *deliveryapp.Service
}

// 编译期断言适配器满足 MCP 发货模板端口。
var _ capability.DeliveryTemplatePorts = (*mcpDeliveryTemplatePorts)(nil)

// ListTemplates 透传模板列表用例。
func (a *mcpDeliveryTemplatePorts) ListTemplates(ctx context.Context, userID int64) ([]deliveryapp.Template, error) {
	return a.service.List(ctx, userID)
}

// GetTemplate 透传模板详情用例。
func (a *mcpDeliveryTemplatePorts) GetTemplate(ctx context.Context, userID, templateID int64) (deliveryapp.Template, error) {
	return a.service.Get(ctx, userID, templateID)
}

// CreateTemplate 透传模板创建用例。
func (a *mcpDeliveryTemplatePorts) CreateTemplate(ctx context.Context, userID int64, draft deliveryapp.Draft) (int64, error) {
	return a.service.Create(ctx, userID, draft)
}

// UpdateTemplate 透传模板更新用例。
func (a *mcpDeliveryTemplatePorts) UpdateTemplate(ctx context.Context, userID, templateID int64, draft deliveryapp.Draft) error {
	return a.service.Update(ctx, userID, templateID, draft)
}

// DeleteTemplate 透传模板删除用例。
func (a *mcpDeliveryTemplatePorts) DeleteTemplate(ctx context.Context, userID, templateID int64) error {
	return a.service.Delete(ctx, userID, templateID)
}

// mcpDefaultReplyPorts 把默认回复应用服务投影为 MCP 默认回复端口。
type mcpDefaultReplyPorts struct {
	// service 是默认回复应用服务。
	service *defaultreplyapp.Service
}

// 编译期断言适配器满足 MCP 默认回复端口。
var _ capability.DefaultReplyPorts = (*mcpDefaultReplyPorts)(nil)

// ListDefaultReplies 透传默认回复列表用例。
func (a *mcpDefaultReplyPorts) ListDefaultReplies(ctx context.Context, userID int64) ([]defaultreplyapp.Summary, error) {
	return a.service.List(ctx, userID)
}

// GetDefaultReply 透传默认回复读取用例。
func (a *mcpDefaultReplyPorts) GetDefaultReply(ctx context.Context, userID int64, cookieID string) (defaultreplyapp.Reply, error) {
	return a.service.Get(ctx, userID, cookieID)
}

// UpsertDefaultReply 透传默认回复保存用例。
func (a *mcpDefaultReplyPorts) UpsertDefaultReply(ctx context.Context, userID int64, cookieID string, reply defaultreplyapp.Reply) error {
	return a.service.Upsert(ctx, userID, cookieID, reply)
}

// DeleteDefaultReply 透传默认回复删除用例。
func (a *mcpDefaultReplyPorts) DeleteDefaultReply(ctx context.Context, userID int64, cookieID string) error {
	return a.service.Delete(ctx, userID, cookieID)
}

// ClearDefaultReplyRecords 透传默认回复投递记录清理用例。
func (a *mcpDefaultReplyPorts) ClearDefaultReplyRecords(ctx context.Context, userID int64, cookieID string) error {
	return a.service.ClearRecords(ctx, userID, cookieID)
}

// mcpKeywordPorts 把关键词与指定商品回复应用服务投影为 MCP 关键词端口。
type mcpKeywordPorts struct {
	// service 是关键词回复应用服务。
	service *keywordsapp.Service
}

// 编译期断言适配器满足 MCP 关键词端口。
var _ capability.KeywordPorts = (*mcpKeywordPorts)(nil)

// ListKeywords 透传关键词列表用例。
func (a *mcpKeywordPorts) ListKeywords(ctx context.Context, userID int64, cookieID string) ([]keywordsapp.Keyword, error) {
	return a.service.List(ctx, userID, cookieID)
}

// AddKeyword 透传关键词新增用例。
func (a *mcpKeywordPorts) AddKeyword(ctx context.Context, userID int64, cookieID string, draft keywordsapp.Draft) (int64, error) {
	return a.service.Add(ctx, userID, cookieID, draft)
}

// ReplaceKeywords 透传关键词批量替换用例。
func (a *mcpKeywordPorts) ReplaceKeywords(ctx context.Context, userID int64, cookieID string, drafts []keywordsapp.Draft) error {
	return a.service.Replace(ctx, userID, cookieID, drafts)
}

// UpdateKeyword 透传关键词更新用例。
func (a *mcpKeywordPorts) UpdateKeyword(ctx context.Context, userID int64, cookieID string, keywordID int64, draft keywordsapp.Draft) error {
	return a.service.Update(ctx, userID, cookieID, keywordID, draft)
}

// DeleteKeywordByID 透传按标识删除关键词用例。
func (a *mcpKeywordPorts) DeleteKeywordByID(ctx context.Context, userID int64, cookieID string, keywordID int64) error {
	return a.service.DeleteByID(ctx, userID, cookieID, keywordID)
}

// DeleteKeywordByIndex 透传按序号删除关键词用例。
func (a *mcpKeywordPorts) DeleteKeywordByIndex(ctx context.Context, userID int64, cookieID string, index int) error {
	return a.service.DeleteByIndex(ctx, userID, cookieID, index)
}

// ListItemReplies 透传指定商品回复列表用例。
func (a *mcpKeywordPorts) ListItemReplies(ctx context.Context, userID int64) ([]keywordsapp.ItemReply, error) {
	return a.service.ListItemReplies(ctx, userID)
}

// GetItemReply 透传指定商品回复读取用例。
func (a *mcpKeywordPorts) GetItemReply(ctx context.Context, userID int64, cookieID, itemID string) (keywordsapp.ItemReply, error) {
	return a.service.GetItemReply(ctx, userID, cookieID, itemID)
}

// SetItemReply 透传指定商品回复写入用例。
func (a *mcpKeywordPorts) SetItemReply(ctx context.Context, userID int64, cookieID, itemID, content string) error {
	return a.service.SetItemReply(ctx, userID, cookieID, itemID, content)
}

// DeleteItemReply 透传指定商品回复删除用例。
func (a *mcpKeywordPorts) DeleteItemReply(ctx context.Context, userID int64, cookieID, itemID string) error {
	return a.service.DeleteItemReply(ctx, userID, cookieID, itemID)
}

// newMCPRulePorts 构造 MCP 规则端口；规则服务缺失时返回 nil。
func newMCPRulePorts(ports composition.TransportPorts) *mcpRulePorts {
	if ports.AutomationRules == nil {
		return nil
	}
	return &mcpRulePorts{service: ports.AutomationRules}
}

// newMCPDeliveryTemplatePorts 构造 MCP 发货模板端口；模板服务缺失时返回 nil。
func newMCPDeliveryTemplatePorts(ports composition.TransportPorts) *mcpDeliveryTemplatePorts {
	if ports.DeliveryTemplates == nil {
		return nil
	}
	return &mcpDeliveryTemplatePorts{service: ports.DeliveryTemplates}
}

// newMCPDefaultReplyPorts 构造 MCP 默认回复端口；默认回复服务缺失时返回 nil。
func newMCPDefaultReplyPorts(ports composition.TransportPorts) *mcpDefaultReplyPorts {
	if ports.DefaultReplies == nil {
		return nil
	}
	return &mcpDefaultReplyPorts{service: ports.DefaultReplies}
}

// newMCPKeywordPorts 构造 MCP 关键词端口；关键词服务缺失时返回 nil。
func newMCPKeywordPorts(ports composition.TransportPorts) *mcpKeywordPorts {
	if ports.Keywords == nil {
		return nil
	}
	return &mcpKeywordPorts{service: ports.Keywords}
}
