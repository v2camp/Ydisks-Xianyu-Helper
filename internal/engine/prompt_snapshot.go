// prompt_snapshot.go 重建一次 AI 回复的完整提示组成，供离线评测与事故复盘使用。
//
// 本文件只读：读取配置、商品与会话历史后按生产链路拼装 system 提示，
// 不调用模型、不写数据库、不改变 Reply 的任何行为。
// 拼装复用 ai.go 的 buildSystemPrompt 与同包的 load* 方法，避免与生产逻辑漂移。
// 与 Reply 的唯一差别在错误路径：次要配置读取失败时降级为 Warnings 而非中断。
package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PromptSnapshot 是一次 AI 回复的完整提示快照。
// 它记录「这条买家消息在生产链路下会看到什么提示」，用于离线评测与事故复盘。
type PromptSnapshot struct {
	// AccountID 是账号标识（cookie_id）；导出落盘前应按需哈希脱敏。
	AccountID string
	// ChatID 是会话标识。
	ChatID string
	// ItemID 是商品标识。
	ItemID string
	// MessageText 是触发本次回复的买家消息原文。
	MessageText string

	// Takeover 表示该消息在生产判定链下是否允许 AI 接管。
	Takeover bool
	// HitIntents 是按配置顺序命中的意图名；负向否决或零命中时为空。
	HitIntents []string
	// Intent 是写入对话历史的意图标签；被负向拦截时统一为 complaint。
	Intent string

	// Model 是当时生效的模型标识；密钥不读取也不记录。
	Model string
	// SystemPrompt 是按生产链路拼装出的最终 system 提示全文。
	SystemPrompt string
	// KnowledgeContext 是本次消息命中的 FAQ 与在售清单上下文；未命中时为空。
	KnowledgeContext string
	// History 是拼装时可见的会话历史，时间正序，不含当前消息。
	History []ConversationTurn

	// ItemTitle 是拼装时使用的商品标题。
	ItemTitle string
	// ItemPrice 是拼装时使用的商品标价，单位元。
	ItemPrice float64
	// ItemDesc 是拼装时使用的商品描述。
	ItemDesc string
	// BargainCount 是拼装时使用的砍价轮次，已计入当前消息。
	BargainCount int
	// IsBargain 表示当前消息是否被判定为砍价话术。
	IsBargain bool
	// WithinBargainLimit 表示当前轮次是否仍在允许降价的范围内。
	WithinBargainLimit bool
	// MinimumPrice 是按折扣配置算出的最低可成交价，单位元。
	MinimumPrice float64

	// MinPriceReply 是策略配置的最低价兜底话术模板；未配置时为空。
	MinPriceReply string
	// NoDiscountReply 是策略配置的不可再优惠兜底话术；未配置时为空。
	NoDiscountReply string

	// ScopeConfig 是当时生效的边界配置 JSON 原文。
	ScopeConfig string
	// KnowledgeConfig 是当时生效的语料配置 JSON 原文。
	KnowledgeConfig string
	// PolicyConfig 是当时生效的策略配置 JSON 原文。
	PolicyConfig string
	// ComplaintKeywords 是当时生效的投诉扩展负向词表原文。
	ComplaintKeywords string

	// Warnings 记录读取过程中的降级事件，保证快照可用性与缺失透明。
	Warnings []string
}

// ExportPromptSnapshot 重建指定买家消息在生产链路下会使用的完整提示组成。
// 方法只读：不生成回复、不写表；次要配置读取失败降级到 Warnings 而不中断导出。
// ctx 用于取消读取；m 需要至少带 AccountID、ChatID、ItemID、Text 才能得到完整快照。
// 返回错误的场景仅限账号 AI 配置、边界配置与对话历史这三项关键读取失败。
func (a *AIReplierImpl) ExportPromptSnapshot(ctx context.Context, m ChatMessage) (*PromptSnapshot, error) {
	// snap 是本次逐步装配的快照结果。
	snap := &PromptSnapshot{
		AccountID:   a.cookieID,
		ChatID:      m.ChatID,
		ItemID:      m.ItemID,
		MessageText: m.Text,
	}

	// readRaw 读取设置原文；失败时记入 Warnings 并返回空串，避免单项缺失中断导出。
	// label 是设置的中文名（用于告警）；key 是设置键。
	readRaw := func(label, key string) string {
		// value、readErr 是设置原文及其读取错误。
		value, readErr := a.store.Settings.Get(ctx, key)
		if readErr != nil {
			snap.Warnings = append(snap.Warnings, "读取"+label+"失败: "+readErr.Error())
			return ""
		}
		return value
	}

	// cfg 是账号级 AI 配置（折扣上限、砍价轮次、自定义业务文案）。
	cfg, err := a.store.AIReply.Get(ctx, a.cookieID)
	if err != nil {
		return nil, fmt.Errorf("读取账号 AI 配置失败: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("账号未配置 AI 回复")
	}

	// scope 是编译后的边界配置，决定该消息是否允许 AI 接管。
	scope, err := a.loadScope(ctx)
	if err != nil {
		return nil, err
	}
	// extraKeywords 是运维扩展的投诉负向词表；读取失败只降级告警，不影响快照输出。
	extraKeywords, kwErr := a.loadComplaintKeywords(ctx)
	if kwErr != nil {
		snap.Warnings = append(snap.Warnings, "读取投诉扩展词表失败: "+kwErr.Error())
	}
	// takeover 表示边界判定是否放行；hitIDs 是命中的意图名，顺序与配置一致。
	takeover, hitIDs := scope.decide(m.Text)
	// extraHit 表示扩展负向词表命中，与生产「任一命中即不接管」的条件保持一致。
	extraHit := intentBlockedByExtra(extraKeywords, m.Text)
	snap.Takeover = takeover && !extraHit
	snap.HitIntents = hitIDs
	snap.Intent = primaryIntentLabel(hitIDs)
	if extraHit {
		snap.Intent = IntentComplaint
	}

	// model 是当时生效的模型标识；只读 ai_model，不触碰加密的 API 密钥设置。
	model := strings.TrimSpace(readRaw("模型配置", "ai_model"))
	if model == "" {
		model = defaultAIModel
	}
	snap.Model = model

	// 取商品信息和当前会话状态构造 system 提示，与 Reply 的取值顺序一致。
	snap.ItemTitle, snap.ItemPrice, snap.ItemDesc = a.itemInfo(ctx, m.ItemID)
	// history 是拼装时可见的历史消息；bargainCount 是已记录的砍价轮次。
	history, bargainCount, isBargain, err := a.conversationContext(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 对话历史失败: %w", err)
	}
	if isBargain {
		bargainCount++
	}
	snap.IsBargain = isBargain
	snap.BargainCount = bargainCount
	snap.WithinBargainLimit = !isBargain || bargainCount <= cfg.MaxBargainRounds

	// systemPrompt 与生产完全同源：基础文案 + 价格安全规则 + 条件追加段。
	systemPrompt := buildSystemPrompt(
		cfg.CustomPrompts, snap.ItemTitle, snap.ItemPrice, snap.ItemDesc,
		cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, cfg.MaxBargainRounds, bargainCount, cfg.AutoAdjustPriceEnabled,
	)
	if !snap.WithinBargainLimit {
		systemPrompt += "\n当前买家已经超过最大砍价轮次。不得继续降价，只能礼貌说明价格不再优惠。"
	}

	// knowledge 是语料配置；读取失败降级告警但不中断。
	knowledge, err := a.loadKnowledge(ctx)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "读取语料配置失败: "+err.Error())
	}
	// live 是动态注入数据（在售列表、库存摘要、发货线索），实现内部已对查询失败降级为空。
	live := a.loadLiveKnowledge(ctx, m.ItemID)
	if knowledge != nil {
		snap.KnowledgeContext = knowledge.buildKnowledgeContext(m.Text, live)
	}
	if snap.KnowledgeContext != "" {
		systemPrompt += "\n\n" + snap.KnowledgeContext
	}
	// 发货方式回答约束始终注入，与生产一致，防止模型声称全店统一渠道。
	systemPrompt += "\n\n" + deliveryAnswerConstraint
	snap.SystemPrompt = systemPrompt

	// policy 是策略兜底话术；读取失败降级告警。
	policy, err := a.loadPolicy(ctx)
	if err != nil {
		snap.Warnings = append(snap.Warnings, "读取策略配置失败: "+err.Error())
	}
	if policy != nil {
		snap.MinPriceReply = policy.MinPriceReply
		snap.NoDiscountReply = policy.NoDiscountReply
	}

	// 最低可成交价：仅在仍在可降价轮次内、且两项折扣上限都大于零时才低于标价。
	snap.MinimumPrice = minimumAllowedPrice(snap.ItemPrice, cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, snap.WithinBargainLimit)

	// turns 是交给生成器的历史轮次，保持时间正序，与 Reply 的转换方式一致。
	turns := make([]ConversationTurn, 0, len(history))
	// message 是当前遍历到的历史消息。
	for _, message := range history {
		turns = append(turns, ConversationTurn{Role: message.Role, Content: message.Content})
	}
	snap.History = turns

	// 原始配置字符串：用于评测复现与跨版本漂移对比，缺项由 readRaw 记入 Warnings。
	snap.ScopeConfig = readRaw("边界配置", settingAIScopeKey)
	snap.KnowledgeConfig = readRaw("语料配置", settingAIKnowledgeKey)
	snap.PolicyConfig = readRaw("策略配置", settingAIPolicyKey)
	snap.ComplaintKeywords = readRaw("投诉扩展词表", "ai_complaint_keywords")

	return snap, nil
}
