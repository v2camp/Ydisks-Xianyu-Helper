// ai_scope.go AI 客服的三层可配置体系：边界（哪些意图交给 AI）、语料（FAQ 与
// 在售清单）、策略（报价兜底话术）。配置存放在系统设置的 JSON 键里，改配置即时
// 生效无需重启；未配置或配置非法时回落内置默认（仅砍价意图 + 内置投诉词表），
// 保证历史行为不变。

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xianyu-go/internal/engine/aimatch"
)

// 系统设置里的配置键名，均为非敏感普通值。
const (
	// settingAIScopeKey 是边界配置键：意图白名单 + 负向组合词。
	settingAIScopeKey = "ai_scope_config"
	// settingAIKnowledgeKey 是语料配置键：FAQ 问答 + 在售清单。
	settingAIKnowledgeKey = "ai_knowledge_config"
	// settingAIPolicyKey 是策略配置键：报价与拒绝兜底话术。
	settingAIPolicyKey = "ai_policy_config"
)

// aiIntentConfig 是边界配置中的一条意图。
type aiIntentConfig struct {
	// ID 是意图名，会写入对话历史 intent 字段。
	ID string `json:"id"`
	// Enabled 表示该意图是否参与判定；false 时整条忽略。
	Enabled bool `json:"enabled"`
	// AI 表示该意图命中时是否允许 AI 接管；false 时仅记录命中不接管。
	AI bool `json:"ai"`
	// Match 是该意图命中的组合词表达式（aimatch 语法）。
	Match string `json:"match"`
}

// aiScopeConfig 是边界配置整体。
type aiScopeConfig struct {
	// Intents 是按顺序判定的意图白名单。
	Intents []aiIntentConfig `json:"intents"`
	// Negative 是负向拦截组合词，命中即禁止 AI 接管。
	Negative string `json:"negative"`
}

// aiFAQItem 是语料配置中的一条问答知识。
type aiFAQItem struct {
	// Category 是知识所属分类，仅用于展示与日志。
	Category string `json:"category"`
	// Match 是该知识命中的组合词表达式。
	Match string `json:"match"`
	// Answer 是命中后注入给 AI 的知识原文。
	Answer string `json:"answer"`
}

// aiCatalogItem 是语料配置中的一条在售资源。
type aiCatalogItem struct {
	// Title 是资源名称。
	Title string `json:"title"`
	// Detail 是资源的季数/版本/网盘等补充说明。
	Detail string `json:"detail"`
}

// aiKnowledgeConfig 是语料配置整体。
type aiKnowledgeConfig struct {
	// FAQ 是问答知识列表。
	FAQ []aiFAQItem `json:"faq"`
	// Catalog 是在售资源清单。
	Catalog []aiCatalogItem `json:"catalog"`
}

// aiPolicyConfig 是策略配置整体。
type aiPolicyConfig struct {
	// MinPriceReply 是最低价兜底话术模板，{amount} 会被替换为金额。
	MinPriceReply string `json:"min_price_reply"`
	// NoDiscountReply 是最低价不可再优惠时的兜底话术。
	NoDiscountReply string `json:"no_discount_reply"`
}

// aiCompiledIntent 是编译后的意图判定条目。
type aiCompiledIntent struct {
	// id 是意图名。
	id string
	// allowAI 表示命中后是否允许 AI 接管。
	allowAI bool
	// match 是编译后的组合词判定函数。
	match func(string) bool
}

// aiScope 是编译后的边界视图，未配置时由内置默认构造。
type aiScope struct {
	// negative 是负向拦截判定函数。
	negative func(string) bool
	// intents 是按顺序判定的意图列表。
	intents []aiCompiledIntent
}

// builtinBargainExpr 是内置砍价意图对应的组合词表达式，与历史正则覆盖范围等价。
// 金额数字段用双引号包裹按正则匹配，避免与组合词语法符号冲突。
const builtinBargainExpr = "(便宜|优惠|少点|最低|砍价|降价|打折)|(能不能&(元|块))|(\"\\d+(\\.\\d+)?\\s*(元|块)\"&(卖|行|可以))"

// builtinComplaintExpr 是内置负向词表对应的组合词表达式，与历史正则等价。
const builtinComplaintExpr = "退款|退货|投诉|差评|举报|骗子|骗人|假货|被骗|维权"

// 内置非接管意图词表与历史意图注册表等价：仅参与命中标注（写入历史），不触发接管；
// 保证未配置时「多意图归并 ambiguous」等历史语义不变。
const (
	// builtinOrderExpr 是订单/发货咨询意图。
	builtinOrderExpr = "发货|还没发|提取码|网盘|下载链接|怎么下载"
	// builtinInquiryExpr 是询价与物流政策意图。
	builtinInquiryExpr = "多少钱|什么价格|怎么卖|包邮"
	// builtinConsultExpr 是商品内容咨询意图。
	builtinConsultExpr = "内容|适合|有效|真实|怎么用"
)

// defaultScope 是 ai_scope_config 未配置时的内置边界：仅砍价允许 AI 接管，
// 其余正向意图只参与意图标注，负向词表为内置投诉词。
var defaultScope = &aiScope{
	negative: orMatchers(compileSafe(builtinComplaintExpr)),
	intents: []aiCompiledIntent{
		{id: IntentBargain, allowAI: true, match: compileSafe(builtinBargainExpr)},
		{id: IntentOrder, allowAI: false, match: compileSafe(builtinOrderExpr)},
		{id: IntentInquiry, allowAI: false, match: compileSafe(builtinInquiryExpr)},
		{id: IntentConsult, allowAI: false, match: compileSafe(builtinConsultExpr)},
	},
}

// compileSafe 编译组合词表达式并返回判定函数；编译失败时返回永远不判定的安全函数。
func compileSafe(expr string) func(string) bool {
	// fn 是编译产物；err 表示语法错误，此时按不命中处理并显式说明。
	fn, err := aimatch.Compile(expr)
	if err != nil {
		return func(string) bool { return false }
	}
	return fn
}

// orMatchers 把多个判定函数合并为「任一命中即命中」。
func orMatchers(matchers ...func(string) bool) func(string) bool {
	return func(text string) bool {
		// m 表示当前遍历到的判定函数。
		for _, m := range matchers {
			if m(text) {
				return true
			}
		}
		return false
	}
}

// loadScope 读取边界配置；未配置或解析失败时回落内置边界。
func (a *AIReplierImpl) loadScope(ctx context.Context) (*aiScope, error) {
	// raw 是边界配置的 JSON 原文；空值按内置处理。
	raw, err := a.store.Settings.Get(ctx, settingAIScopeKey)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 边界配置失败: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return defaultScope, nil
	}
	// cfgJson 是解析出的边界配置结构。
	var cfgJson aiScopeConfig
	if // err 是 JSON 解析错误。
	err := json.Unmarshal([]byte(raw), &cfgJson); err != nil {
		a.logger.Warn("AI 边界配置 JSON 非法，回落内置", "err", err)
		return defaultScope, nil
	}
	// scope 是正在装配的编译后边界。
	scope := &aiScope{negative: compileSafe(builtinComplaintExpr)}
	// negative 与 extra 合并：配置负向词在右，追加到内置词表之后。
	if strings.TrimSpace(cfgJson.Negative) != "" {
		scope.negative = orMatchers(scope.negative, compileSafe(cfgJson.Negative))
	}
	// intent 表示当前遍历到的意图配置。
	for _, intent := range cfgJson.Intents {
		if !intent.Enabled || strings.TrimSpace(intent.Match) == "" {
			continue
		}
		// match 是编译后的判定函数；编译失败时记告警并跳过该意图。
		match, compileErr := aimatch.Compile(intent.Match)
		if compileErr != nil {
			a.logger.Warn("AI 意图组合词非法，忽略该意图", "intent", intent.ID, "err", compileErr)
			continue
		}
		scope.intents = append(scope.intents, aiCompiledIntent{id: intent.ID, allowAI: intent.AI, match: match})
	}
	if len(scope.intents) == 0 {
		a.logger.Warn("AI 边界配置无可用意图，回落内置")
		return defaultScope, nil
	}
	return scope, nil
}

// decide 按边界判定文本是否允许 AI 接管，并返回命中的意图名。
// 负向命中或没有任何启用意图命中时不接管（历史语义：仅砍价命中才接管）。
func (s *aiScope) decide(text string) (takeover bool, hitIDs []string) {
	if s.negative(text) {
		return false, nil
	}
	// intent 表示当前遍历到的编译后意图。
	for _, intent := range s.intents {
		if intent.match(text) {
			hitIDs = append(hitIDs, intent.id)
			if intent.allowAI {
				takeover = true
			}
		}
	}
	return takeover, hitIDs
}

// loadKnowledge 读取语料配置；未配置时返回空语料。
func (a *AIReplierImpl) loadKnowledge(ctx context.Context) (*aiKnowledgeConfig, error) {
	// raw 是语料配置的 JSON 原文。
	raw, err := a.store.Settings.Get(ctx, settingAIKnowledgeKey)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 语料配置失败: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return &aiKnowledgeConfig{}, nil
	}
	// cfgJson 是解析出的语料配置。
	var cfgJson aiKnowledgeConfig
	if // err 是 JSON 解析错误。
	err := json.Unmarshal([]byte(raw), &cfgJson); err != nil {
		a.logger.Warn("AI 语料配置 JSON 非法，回落空语料", "err", err)
		return &aiKnowledgeConfig{}, nil
	}
	return &cfgJson, nil
}

// matchedFAQ 返回文本命中的 FAQ 条目（按配置顺序，最多 limit 条）。
func (k *aiKnowledgeConfig) matchedFAQ(text string, limit int) []aiFAQItem {
	// hits 是命中的 FAQ 列表。
	var hits []aiFAQItem
	// item 表示当前遍历到的 FAQ 条目。
	for _, item := range k.FAQ {
		if strings.TrimSpace(item.Answer) == "" || strings.TrimSpace(item.Match) == "" {
			continue
		}
		if compileSafe(item.Match)(text) {
			hits = append(hits, item)
			if len(hits) >= limit {
				break
			}
		}
	}
	return hits
}

// buildKnowledgeContext 把命中的 FAQ 与在售清单格式化成注入 system 的知识上下文。
func (k *aiKnowledgeConfig) buildKnowledgeContext(text string) string {
	// parts 是知识上下文的段落列表。
	var parts []string
	// faqHits 是当前文本命中的 FAQ 条目。
	faqHits := k.matchedFAQ(text, 3)
	if len(faqHits) > 0 {
		// lines 是 FAQ 知识的逐行格式。
		lines := make([]string, 0, len(faqHits))
		// item 表示当前遍历到的 FAQ 条目。
		for _, item := range faqHits {
			lines = append(lines, "- "+strings.TrimSpace(item.Answer))
		}
		parts = append(parts, "店铺知识（回答买家问题时必须以此为据）：\n"+strings.Join(lines, "\n"))
	}
	if len(k.Catalog) > 0 {
		// catalogLines 是在售清单的逐行格式。
		catalogLines := make([]string, 0, len(k.Catalog))
		// item 表示当前遍历到的在售资源。
		for _, item := range k.Catalog {
			// line 是单条资源的格式化描述，Detail 为空时只写标题。
			line := item.Title
			if strings.TrimSpace(item.Detail) != "" {
				line += "（" + strings.TrimSpace(item.Detail) + "）"
			}
			catalogLines = append(catalogLines, "- "+line)
		}
		parts = append(parts, "本店在售资源清单（买家问有没有/能否单买/第几季时依此回答，未在清单内的一律回答“暂时没有”）：\n"+strings.Join(catalogLines, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

// loadPolicy 读取策略配置；未配置时回落内置话术。
func (a *AIReplierImpl) loadPolicy(ctx context.Context) (*aiPolicyConfig, error) {
	// raw 是策略配置的 JSON 原文。
	raw, err := a.store.Settings.Get(ctx, settingAIPolicyKey)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 策略配置失败: %w", err)
	}
	// policy 是带内置兜底的策略结果。
	policy := &aiPolicyConfig{
		MinPriceReply:   "可以优惠的最低价格是 {amount} 元，低于这个价格暂时无法成交。",
		NoDiscountReply: "抱歉，当前价格已经是最低价，暂时不能再优惠了。",
	}
	if strings.TrimSpace(raw) == "" {
		return policy, nil
	}
	// cfgJson 是解析出的策略配置；非法时只记告警并保持内置兜底。
	var cfgJson aiPolicyConfig
	if // err 是 JSON 解析错误。
	err := json.Unmarshal([]byte(raw), &cfgJson); err != nil {
		a.logger.Warn("AI 策略配置 JSON 非法，回落内置话术", "err", err)
		return policy, nil
	}
	if strings.TrimSpace(cfgJson.MinPriceReply) != "" {
		policy.MinPriceReply = cfgJson.MinPriceReply
	}
	if strings.TrimSpace(cfgJson.NoDiscountReply) != "" {
		policy.NoDiscountReply = cfgJson.NoDiscountReply
	}
	return policy, nil
}
