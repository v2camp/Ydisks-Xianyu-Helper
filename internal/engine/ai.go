// ai.go AI 回复实现（优先级3）。调用 OpenAI 兼容 chat completions 接口。
// 使用商品信息、对话历史和确定性的价格边界生成回复。

package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/netguard"
)

// defaultAIBaseURL 用于本次流程后续判断的defaultAIBaseURL
const (
	defaultAIBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultAIModel   = "qwen-plus"
)

// newAIHTTPClient 用于本次流程后续判断的newAIHTTPClient
var newAIHTTPClient = func(baseURL string) (*http.Client, error) {
	return netguard.ConfiguredEndpointHTTPClient(baseURL, 30*time.Second)
}

// AIReplierImpl AI 回复实现。
type AIReplierImpl struct {
	cookieID string
	store    *db.Store
	logger   *slog.Logger
	// gen 是模型生成接缝；默认实现为单次 chat completions，客服 Agent Loop 可替换它。
	gen AIGenerator
}

// NewAIReplier 构造。
// gens 是可选生成器，最多只取第一个：不传或传 nil 时使用默认单次补全实现，
// 从而让既有调用点无需改动。
func NewAIReplier(cookieID string, store *db.Store, logger *slog.Logger, gens ...AIGenerator) *AIReplierImpl {
	if logger == nil {
		logger = slog.Default()
	}
	// gen 是本次使用的模型生成接缝。
	gen := AIGenerator(NewDefaultAIGenerator())
	if len(gens) > 0 && gens[0] != nil {
		gen = gens[0]
	}
	return &AIReplierImpl{
		cookieID: cookieID,
		store:    store,
		logger:   logger.With("account", cookieID, "subsys", "ai"),
		gen:      gen,
	}
}

// Reply 实现 AIReplier 接口。
func (a *AIReplierImpl) Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error) {
	// cfg、err 用于本次流程后续判断的cfg、err
	cfg, err := a.store.AIReply.Get(ctx, a.cookieID)
	if err != nil || cfg == nil || !cfg.AIEnabled {
		return nil, nil // 未启用 AI
	}
	// scope 是编译后的 AI 边界（意图白名单 + 负向组合词），未配置时回落内置砍价范围。
	scope, err := a.loadScope(ctx)
	if err != nil {
		return nil, err
	}
	// extraKeywords 是运维可扩展的投诉负向词表（来自设置 ai_complaint_keywords），
	// 与内置/配置负向词独立追加判定；读取失败或非法项已安全回落，不会导致漏拦。
	extraKeywords, kwErr := a.loadComplaintKeywords(ctx)
	if kwErr != nil {
		a.logger.Warn("读取投诉扩展词表失败，回落内置词表", "err", kwErr)
	}
	// takeover 表示当前消息是否允许 AI 接管；hitIDs 是按配置顺序命中的意图名。
	// 边界策略：负向命中（内置/配置/扩展词表任一）一票否决；命中启用意图才接管。
	// negHit 表示内置与配置负向词表是否命中。
	negHit := scope.negative(m.Text)
	// extraHit 表示扩展投诉负向词表是否命中。
	extraHit := intentBlockedByExtra(extraKeywords, m.Text)
	// takeover、hitIDs 是边界判定的结果：是否放行与命中的意图名，顺序与配置一致。
	takeover, hitIDs := scope.decide(m.Text)
	// userIntent 是写入对话历史的真实意图标签（多命中/零命中分别归并到 ambiguous/chitchat）。
	userIntent := primaryIntentLabel(hitIDs)
	if extraHit {
		userIntent = IntentComplaint
	}
	// 接管与不接管都必须留痕：否则评测发现回复不合格时，
	// 无法回答「这条消息当时为什么被放行或拦下」。
	a.recordDecision(ctx, m, scope.fingerprint, hitIDs, negHit, extraHit, takeover && !extraHit, userIntent)
	if !takeover || extraHit {
		return nil, nil
	}
	// aiCfg、err 用于本次流程后续判断的人工智能Cfg、err
	aiCfg, err := a.globalAIConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取全局 AI 配置失败: %w", err)
	}
	if aiCfg.APIKey == "" {
		a.logger.Warn("AI 已启用但未配置 APIKey")
		return nil, nil
	}

	// 取商品信息和当前会话状态构造 system prompt。
	itemTitle, itemPrice, itemDesc := a.itemInfo(ctx, m.ItemID)
	// history、bargainCount、isBargain、err 用于本次流程后续判断的history、bargainCount、isBargain、err
	history, bargainCount, isBargain, err := a.conversationContext(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 对话历史失败: %w", err)
	}
	if isBargain {
		bargainCount++
	}
	// withinBargainLimit 用于本次流程后续判断的withinBargain上限
	withinBargainLimit := !isBargain || bargainCount <= cfg.MaxBargainRounds
	// systemPrompt 用于本次流程后续判断的系统Prompt
	systemPrompt := buildSystemPrompt(
		cfg.CustomPrompts, itemTitle, itemPrice, itemDesc,
		cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, cfg.MaxBargainRounds, bargainCount, cfg.AutoAdjustPriceEnabled,
	)
	if !withinBargainLimit {
		systemPrompt += "\n当前买家已经超过最大砍价轮次。不得继续降价，只能礼貌说明价格不再优惠。"
	}
	// knowledge 是语料配置（FAQ 问答与手工在售清单兼容字段）。
	knowledge, err := a.loadKnowledge(ctx)
	if err != nil {
		return nil, err
	}
	// live 是动态注入数据（在售列表/库存摘要/发货推断），查询失败已降级为空。
	live := a.loadLiveKnowledge(ctx, m.ItemID)
	// knowledgeCtx 是命中 FAQ 与在售清单拼装的知识上下文；非空时注入 system 让 AI 有据可答。
	knowledgeCtx := knowledge.buildKnowledgeContext(m.Text, live)
	if knowledgeCtx != "" {
		systemPrompt += "\n\n" + knowledgeCtx
	}
	// 发货方式回答约束始终注入，防止模型声称全店统一渠道。
	systemPrompt += "\n\n" + deliveryAnswerConstraint
	// policy 是策略配置（报价/拒绝兜底话术），未配置时回落内置文案。
	policy, err := a.loadPolicy(ctx)
	if err != nil {
		return nil, err
	}

	// 调模型并取首个候选文本；生成器可替换（默认单次问答，客服 Agent 为带工具回合的 Loop）。
	// historyTurns 是交给生成器的既有对话历史，保持时间正序。
	historyTurns := make([]ConversationTurn, 0, len(history))
	// message 是当前遍历到的历史消息。
	for _, message := range history {
		historyTurns = append(historyTurns, ConversationTurn{Role: message.Role, Content: message.Content})
	}
	// content、err 分别是模型返回的首个候选文本及其调用失败原因。
	content, err := a.gen.Generate(ctx, GenerateRequest{
		APIKey:      aiCfg.APIKey,
		BaseURL:     aiCfg.BaseURL,
		Model:       aiCfg.Model,
		Temperature: defaultAIGeneratorTemperature,
		System:      systemPrompt,
		History:     historyTurns,
		Current:     m.Text,
	})
	if err != nil {
		return nil, err
	}
	// reply 用于本次流程后续判断的回复；生成器返回空文本视为模型无可用产出。
	reply, markerPrice, markerOK := extractExecutableOffer(content)
	if reply == "" {
		return nil, nil
	}
	// minimumPrice 用于本次流程后续判断的minimumPrice
	minimumPrice := minimumAllowedPrice(itemPrice, cfg.MaxDiscountPercent, cfg.MaxDiscountAmount, withinBargainLimit)
	// quote 是仅在商家开启真实改价且模型结构化报价通过校验时返回的执行提案。
	var quote *AIPriceQuoteProposal
	// markerUnsafe 表示结构化报价突破最低价或高于商品标价，必须和正文越界同样拦截。
	markerUnsafe := markerOK && (markerPrice+0.0001 < minimumPrice || markerPrice > itemPrice+0.0001)
	if // offered、unsafe 用于本次流程后续判断的offered、unsafe
	offered, unsafe := unsafeOfferedPrice(reply, minimumPrice); unsafe || markerUnsafe {
		if markerUnsafe {
			offered = markerPrice
		}
		a.logger.Warn("AI 报价超过折扣边界，使用安全回复", "offered", offered, "minimum", minimumPrice)
		if minimumPrice >= itemPrice || !withinBargainLimit {
			// noDiscount 是策略配置的拒绝兜底话术；未配置时沿用内置文案。
			noDiscount := policy.NoDiscountReply
			if noDiscount == "" {
				noDiscount = "抱歉，当前价格已经是最低价，暂时不能再优惠了。"
			}
			reply = noDiscount
		} else {
			// minPrice 是策略配置的最低价话术模板，{amount} 替换为该轮最低报价。
			minPrice := policy.MinPriceReply
			if !strings.Contains(minPrice, "{amount}") {
				minPrice = "可以优惠的最低价格是 {amount} 元，低于这个价格暂时无法成交。"
			}
			reply = strings.ReplaceAll(minPrice, "{amount}", fmt.Sprintf("%.2f", minimumPrice))
			if cfg.AutoAdjustPriceEnabled {
				quote = &AIPriceQuoteProposal{PriceCents: priceToCents(minimumPrice)}
			}
		}
	} else if cfg.AutoAdjustPriceEnabled && markerOK && markerPrice > 0 && markerPrice+0.0001 < itemPrice && replyContainsOfferedPrice(reply, markerPrice) {
		quote = &AIPriceQuoteProposal{PriceCents: priceToCents(markerPrice)}
	}
	if m.ChatID != "" && m.ItemID != "" {
		if // err 用于本次流程后续判断的err
		err := a.store.AIReply.AddConversationExchange(ctx, a.cookieID, m.ChatID, m.SenderUserID, m.ItemID,
			db.AIConversationMessage{Role: "user", Content: m.Text, Intent: userIntent, BargainCount: bargainCount},
			db.AIConversationMessage{Role: "assistant", Content: reply, Intent: "reply", BargainCount: bargainCount},
		); err != nil {
			return nil, fmt.Errorf("保存 AI 对话失败: %w", err)
		}
	}
	return &ReplyResult{Text: reply, AutoPriceQuote: quote}, nil
}

// recordDecision 记录一次接管决策的归因信息。
// 埋点失败只记 Debug，绝不影响回复链路；不接管的消息同样留痕，
// 否则「为什么这条没被接管」永远无法回答。
// fingerprint 是判定所用边界配置的指纹，用于区分模型波动与配置变更。
func (a *AIReplierImpl) recordDecision(ctx context.Context, m ChatMessage, fingerprint string, hitIDs []string, negHit, extraHit, takeover bool, intentLabel string) {
	// preview 是买家消息的截断预览，仅供人读定位，不承担审计作用。
	preview := m.Text
	// runes 是按字符计数的切片，避免中文被按字节截断成乱码。
	if runes := []rune(preview); len(runes) > 60 {
		preview = string(runes[:60]) + "…"
	}
	// reason 是判定原因简码，供聚合统计使用。
	reason := "taken"
	switch {
	case extraHit:
		reason = "extra-negative"
	case negHit:
		reason = "negative"
	case !takeover:
		reason = "no-intent"
	}
	// err 是归因写入失败原因；失败只记 Debug。
	err := a.store.AIReply.AddDecisionLog(ctx, db.AIDecisionRecord{
		CookieID:          a.cookieID,
		ChatID:            m.ChatID,
		ItemID:            m.ItemID,
		MessageHash:       logsafe.ID(m.Text),
		MessagePreview:    preview,
		Takeover:          takeover,
		HitIntents:        hitIDs,
		NegativeHit:       negHit,
		ExtraHit:          extraHit,
		IntentLabel:       intentLabel,
		Reason:            reason,
		ConfigFingerprint: fingerprint,
	})
	if err != nil {
		a.logger.Debug("写入 AI 决策归因失败", "err", err)
	}
}

// conversationContext 封装conversation上下文业务协调。
func (a *AIReplierImpl) conversationContext(ctx context.Context, m ChatMessage) ([]db.AIConversationMessage, int, bool, error) {
	// isBargain 用于本次流程后续判断的isBargain
	isBargain := bargainMessageRe.MatchString(strings.ToLower(m.Text))
	if m.ChatID == "" || m.ItemID == "" {
		return nil, 0, isBargain, nil
	}
	// history、err 用于本次流程后续判断的history、err
	history, err := a.store.AIReply.ConversationHistory(ctx, a.cookieID, m.ChatID, m.ItemID, 10)
	if err != nil {
		return nil, 0, isBargain, err
	}
	// count、err 用于本次流程后续判断的count、err
	count, err := a.store.AIReply.CurrentBargainCount(ctx, a.cookieID, m.ChatID, m.ItemID)
	return history, count, isBargain, err
}

// globalAIConfig 用于本次流程后续判断的globalAI配置
type globalAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

// intentBlockedByExtra 判断文本是否命中扩展投诉负向词表中的任一正则；命中即阻止 AI 接管。
func intentBlockedByExtra(extra []*regexp.Regexp, text string) bool {
	// re 表示当前遍历到的扩展负向正则。
	for _, re := range extra {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// loadComplaintKeywords 从设置读取可扩展的投诉负向词表并解析为已校验正则。
// 读取失败返回错误（调用方记告警并回落内置词表）；空值解析为 nil（同样回落内置，宁可多拦不可少拦）。
func (a *AIReplierImpl) loadComplaintKeywords(ctx context.Context) ([]*regexp.Regexp, error) {
	// raw 是设置 ai_complaint_keywords 的原始字符串值。
	raw, err := a.store.Settings.Get(ctx, "ai_complaint_keywords")
	if err != nil {
		return nil, err
	}
	return ParseComplaintKeywords(raw, a.logger), nil
}

// globalAIConfig 封装globalAI配置业务协调。
func (a *AIReplierImpl) globalAIConfig(ctx context.Context) (*globalAIConfig, error) {
	// apiKey、err 用于本次流程后续判断的apiKey、err
	apiKey, err := a.store.ReadSensitiveSettingForAccount(ctx, a.cookieID, "ai_api_key", "settings.use", "ai_reply")
	if err != nil {
		return nil, err
	}
	// baseURL、err 用于本次流程后续判断的baseURL、err
	baseURL, err := a.store.Settings.Get(ctx, "ai_api_url")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL, err = a.store.Settings.Get(ctx, "ai_base_url")
		if err != nil {
			return nil, err
		}
	}
	// model、err 用于本次流程后续判断的model、err
	model, err := a.store.Settings.Get(ctx, "ai_model")
	if err != nil {
		return nil, err
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultAIBaseURL
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultAIModel
	}
	return &globalAIConfig{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

// itemInfo 取商品标题/价格/描述。
func (a *AIReplierImpl) itemInfo(ctx context.Context, itemID string) (title string, price float64, desc string) {
	// it、err 用于本次流程后续判断的it、err
	it, err := a.store.Items.Get(ctx, a.cookieID, itemID)
	if err != nil || it == nil {
		return "商品信息获取失败", 0, "暂无商品描述"
	}
	title = it.ItemTitle
	if title == "" {
		title = "未知商品"
	}
	price = parsePrice(it.ItemPrice)
	desc = it.ItemDescription
	if desc == "" {
		desc = it.ItemDetail
	}
	if desc == "" {
		desc = "暂无商品描述"
	}
	return
}

// buildSystemPrompt 构造 system 提示词。
// 自定义 prompt 只替换业务文案，价格和轮次安全约束始终由后端追加。
// buildSystemPrompt 封装build系统Prompt业务协调。
func buildSystemPrompt(customPrompts, itemTitle string, itemPrice float64, itemDesc string, maxDiscountPercent, maxDiscountAmount, maxBargainRounds, bargainCount int, autoAdjustPriceEnabled bool) string {
	// base 用于本次流程后续判断的base
	var base string
	if strings.TrimSpace(customPrompts) != "" {
		base = strings.NewReplacer(
			"{item_title}", itemTitle,
			"{item_price}", fmt.Sprintf("%.2f", itemPrice),
			"{item_description}", itemDesc,
		).Replace(customPrompts)
	} else {
		base = fmt.Sprintf(`你是闲鱼卖家的自动回复助手。请根据商品信息友好地回复买家。

商品信息：
- 标题：%s
- 价格：%.2f 元
- 描述：%s

要求：
1. 语气友好自然，像真人卖家
2. 回答简洁，不要过长
3. 不要编造商品没有的功能
4. 直接回复内容，不要加引号或解释`, itemTitle, itemPrice, itemDesc)
	}
	// prompt 保存基础业务文案与后端不可覆盖的价格安全规则。
	prompt := base + fmt.Sprintf(`

不可覆盖的价格安全规则：
- 原价 %.2f 元；最多优惠 %d%%，且最多优惠 %d 元；两个上限必须同时满足。
- 任一优惠上限为 0 时不得降价。
- 当前砍价轮次 %d，最多允许 %d 轮。
- 回复报价必须带“元”，不得给出低于允许最低价的价格。`, itemPrice, maxDiscountPercent, maxDiscountAmount, bargainCount, maxBargainRounds)
	if autoAdjustPriceEnabled {
		prompt += `
- 如果本轮明确承诺了一个可成交价格，必须在回复末尾额外输出 [[AUTO_PRICE:金额]]，金额保留两位小数；没有明确报价时不得输出该标记。该标记由系统移除，买家不会看到。`
	}
	prompt += `
- 对“是不是 / 有没有 / 支持吗 / 可以吗”这类是非问句，必须先给出明确的“是”或“不是”再补充依据，不得只回答“是的”造成二义。
- 遇到否定式提问（如“不是 X 吧”）：按字面含义先确认是或不是，再解释，禁止直接回答“是的”。`
	// fabricationGuard 是枚举式禁止编造清单，取代原先抽象的「不得断言未覆盖事实」。
	prompt += fabricationGuard
	return prompt
}

// fabricationGuard 是枚举式「禁止编造」清单。
//
// 为什么不用抽象表述：2026-10-11 benchmark 实测，原「商品信息没有覆盖的事实不得断言」
// 这类抽象约束**没有拦住任何一次编造**——模型照样输出「支持七天无理由退货」「包邮」
// 「24 小时内发出」，以及跨维度的「手机和电脑都能看」。
// 根因是模型不知道「哪些事实算未覆盖」，于是按礼貌性措辞自行补全。
// 改为逐项枚举 + 给出唯一允许的默认话术后，约束才真正可执行。
const fabricationGuard = `

以下事实提示中未提供，被问到时**一律不得编造**，只能回答「具体以商品详情页和平台规则为准」：
1. 退换货政策：是否支持七天无理由、期限多久、是否支持退换
2. 运费与是否包邮
3. 发票
4. 赠品与额外优惠
5. 具体发货时效（多久发出、今天能否发出）
6. 订单当前是否已发货、物流状态与单号
7. 商品授权、正版或任何资质
8. 设备与文件格式兼容性
以上各项与商品信息冲突时，商品信息优先；商品信息未覆盖时，禁止用「应该是」「一般都」「通常」等措辞变相编造。`

// priceRe 用于本次流程后续判断的priceRe
var priceRe = regexp.MustCompile(`[^\d.]`)

// bargainMessageRe 用于本次流程后续判断的bargain消息Re
var bargainMessageRe = regexp.MustCompile(`(?i)(便宜|优惠|少点|最低|砍价|降价|打折|能不能.*(?:元|块)|\d+(?:\.\d+)?\s*(?:元|块).*(?:卖|行|可以))`)

// offeredPriceRe 用于本次流程后续判断的offeredPriceRe
var offeredPriceRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:元|块)`)

// executableOfferRe 只接受模型按约定输出的单个两位小数自动改价标记。
var executableOfferRe = regexp.MustCompile(`\[\[AUTO_PRICE:(\d+(?:\.\d{1,2})?)\]\]`)

// internalOfferMarkerRe 匹配任意格式的内部报价标记，确保模型格式错误时也不会泄露给买家。
var internalOfferMarkerRe = regexp.MustCompile(`\[\[AUTO_PRICE:[^\]]*\]\]`)

// extractExecutableOffer 从模型输出移除内部报价标记，并返回可校验的十进制金额。
func extractExecutableOffer(content string) (string, float64, bool) {
	// matches 是模型输出中所有结构化报价标记；只有恰好一个标记才可执行。
	matches := executableOfferRe.FindAllStringSubmatch(content, -1)
	// visible 是删除所有内部标记后真正发送给买家的文本。
	visible := strings.TrimSpace(internalOfferMarkerRe.ReplaceAllString(content, ""))
	if len(matches) != 1 {
		return visible, 0, false
	}
	// price 是标记中的元金额；err 表示模型输出无法解析为有限十进制数。
	price, err := strconv.ParseFloat(matches[0][1], 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) {
		return visible, 0, false
	}
	return visible, price, true
}

// priceToCents 把已经通过边界校验的元金额四舍五入为整数分。
func priceToCents(price float64) int64 {
	return int64(math.Round(price * 100))
}

// replyContainsOfferedPrice 判断内部执行价格是否与买家可见正文中的元/块报价一致。
func replyContainsOfferedPrice(reply string, target float64) bool {
	// match 是正文中当前待比较的显式价格匹配结果。
	for _, match := range offeredPriceRe.FindAllStringSubmatch(reply, -1) {
		// price 是买家可见的报价金额；err 表示该匹配无法解析为数值。
		price, err := strconv.ParseFloat(match[1], 64)
		if err == nil && math.Abs(price-target) < 0.0001 {
			return true
		}
	}
	return false
}

// minimumAllowedPrice 封装minimumAllowedPrice业务协调。
func minimumAllowedPrice(price float64, maxDiscountPercent, maxDiscountAmount int, allowDiscount bool) float64 {
	if price <= 0 {
		return 0
	}
	if !allowDiscount || maxDiscountPercent <= 0 || maxDiscountAmount <= 0 {
		return price
	}
	// byPercent 用于本次流程后续判断的byPercent
	byPercent := price * (1 - float64(maxDiscountPercent)/100)
	// byAmount 用于本次流程后续判断的byAmount
	byAmount := price - float64(maxDiscountAmount)
	// minimum 是两个折扣边界中更严格的原始最低价。
	minimum := math.Max(0, math.Max(byPercent, byAmount))
	// 金额向上取整到分，避免显示或执行价格因普通四舍五入突破折扣上限。
	return math.Ceil(minimum*100-0.0000001) / 100
}

// unsafeOfferedPrice 封装unsafeOfferedPrice业务协调。
func unsafeOfferedPrice(reply string, minimum float64) (float64, bool) {
	if minimum <= 0 {
		return 0, false
	}
	// match 表示当前遍历过程中的match
	for _, match := range offeredPriceRe.FindAllStringSubmatch(reply, -1) {
		// value、err 用于本次流程后续判断的value、err
		value, err := strconv.ParseFloat(match[1], 64)
		if err == nil && value+0.0001 < minimum {
			return value, true
		}
	}
	return 0, false
}

// truncateAIContent 封装truncateAI内容业务协调。
func truncateAIContent(content string) string {
	// maxRunes 用于本次流程后续判断的maxRunes
	const maxRunes = 2000
	// runes 用于本次流程后续判断的runes
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
}

// parsePrice 移除非数字字符后转换为 float。
func parsePrice(s string) float64 {
	// cleaned 用于本次流程后续判断的cleaned
	cleaned := priceRe.ReplaceAllString(s, "")
	if cleaned == "" {
		return 0
	}
	// f 用于本次流程后续判断的f
	f, _ := strconv.ParseFloat(cleaned, 64)
	return f
}
