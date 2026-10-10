// ai_live_context.go AI 回复时从本地库动态注入的在售清单、卡密库存与发货方式线索。
// 设计目标：问答口径跟随真实商品与发货配置，而不是过期的手工 catalog；
// 所有查询只读本地库，失败时降级为不注入并记 Debug，绝不阻断 AI 回复。
// 敏感约束：卡密正文只在计数时短暂读取，禁止写入提示词与日志。

package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// liveKnowledge 是一次 AI 回复构造提示词时动态查询到的注入数据。
type liveKnowledge struct {
	// CatalogLines 是动态在售列表行（已格式化为「- 标题（价格）」）；空表示无商品或查询失败。
	CatalogLines []string
	// StockLines 是卡密库存摘要行（组名 + 剩余未用张数）；空表示无数据卡组或查询失败。
	StockLines []string
	// DeliveryHint 是按商品线索推断的发货方式说明；空表示无任何线索不注入。
	DeliveryHint string
}

// deliveryAnswerConstraint 是回答发货方式类问题时必须写入 prompt 的系统约束。
// 目的：禁止模型声称全店统一渠道，无线索时给出统一兜底话术。
const deliveryAnswerConstraint = "回答发货方式、什么网盘、怎么发类问题时：提示词给出了当前商品的确定发货渠道时，必须直接明确回答该渠道（如「这款发百度网盘」），不得含糊；禁止声称全店统一支持某渠道。只有当前商品没有任何渠道线索时，才回答「不同商品发货方式不同，以商品详情和拍下的发货消息为准」。"

// liveCatalogInstruction 是动态在售列表的注入指令文案。
const liveCatalogInstruction = "本店在售列表（买家问有没有/单买/第几季时依此回答，不在列表的回答暂时没有）"

// liveStockInstruction 是卡密库存摘要的注入指令文案。
const liveStockInstruction = "库存参考（仅用于回答“还有货吗”类问题）"

// deliveryChannelKeywords 是发货渠道关键词到展示名的映射（按优先级去重）。
// 展示名用于提示词，与平台私聊敏感词解耦。
var deliveryChannelKeywords = []struct {
	// keyword 是在商品描述/模板文案/卡组名中探测的子串。
	keyword string
	// label 是命中后写入提示词的渠道展示名。
	label string
}{
	{keyword: "夸克", label: "夸克网盘"},
	{keyword: "百度", label: "百度网盘"},
	{keyword: "迅雷", label: "迅雷"},
	{keyword: "网盘码", label: "网盘"},
	{keyword: "网盘", label: "网盘"},
	{keyword: "资源码", label: "资源码"},
	{keyword: "提取码", label: "提取码"},
	{keyword: "链接", label: "链接"},
}

// deliveryStepwisePatterns 是描述里表示分步发送的模式子串（先发码/后发链接类）。
var deliveryStepwisePatterns = []string{
	"先发码", "先发资源码", "先发链接", "后发码", "后发资源码", "后发链接",
	"稍后发码", "稍后发资源码", "稍后发链接", "分步发送",
}

// loadLiveKnowledge 汇总动态在售列表、库存摘要与发货推断。
// 任一查询失败只跳过对应段落并记 Debug，返回值始终可用于拼装提示词。
func (a *AIReplierImpl) loadLiveKnowledge(ctx context.Context, itemID string) liveKnowledge {
	// live 是本次动态注入数据的累积结果。
	var live liveKnowledge
	// catalogLines、err 是动态在售列表查询结果；失败降级为不注入，不阻断回复。
	if catalogLines, err := a.loadLiveCatalogLines(ctx); err != nil {
		a.logger.Debug("动态在售列表查询失败，降级为不注入", "err", err)
	} else {
		live.CatalogLines = catalogLines
	}
	// stockLines、err 是卡密库存摘要查询结果；失败降级为不注入。
	if stockLines, err := a.loadCardStockLines(ctx); err != nil {
		a.logger.Debug("卡密库存摘要查询失败，降级为不注入", "err", err)
	} else {
		live.StockLines = stockLines
	}
	// hint、err 是发货方式推断结果；失败降级为不注入。
	if hint, err := a.inferDeliveryHints(ctx, itemID); err != nil {
		a.logger.Debug("发货方式推断查询失败，降级为不注入", "err", err)
	} else {
		live.DeliveryHint = hint
	}
	return live
}

// loadLiveCatalogLines 查询当前账号在售商品并格式化为「- 标题（价格）」行。
// ctx 控制取消；返回空切片表示当前无在售商品（调用方可回落手工 catalog）。
func (a *AIReplierImpl) loadLiveCatalogLines(ctx context.Context) ([]string, error) {
	// items、err 是当前账号全部未删除商品；本地库查询，无网络。
	items, err := a.store.Items.AllForCookie(ctx, a.cookieID)
	if err != nil {
		return nil, err
	}
	// lines 是格式化后的在售列表行。
	lines := make([]string, 0, len(items))
	// item 表示当前遍历到的商品行。
	for _, item := range items {
		// title 是去空白后的商品标题；空标题跳过，避免注入无意义行。
		title := strings.TrimSpace(item.ItemTitle)
		if title == "" {
			continue
		}
		// price 是去空白后的价格文本；为空时只写标题。
		price := strings.TrimSpace(item.ItemPrice)
		if price == "" {
			lines = append(lines, "- "+title)
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s（%s）", title, price))
	}
	return lines, nil
}

// loadCardStockLines 查询启用的数据卡密组并汇总「组名：N 张」库存行。
// 只统计非空卡密行数，绝不把卡密正文写入返回值、提示词或日志。
func (a *AIReplierImpl) loadCardStockLines(ctx context.Context) ([]string, error) {
	// ownerID 是当前账号所属用户主键；库存按用户隔离。
	ownerID, err := a.store.Cookies.GetOwnerID(ctx, a.cookieID)
	if err != nil {
		return nil, err
	}
	// cards、err 是用户全部卡密组脱敏摘要；DataContent 仅供本地计数。
	cards, err := a.store.Cards.AllForUserSummary(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	// lines 是格式化后的库存摘要行。
	lines := make([]string, 0, len(cards))
	// card 表示当前遍历到的卡密组。
	for _, card := range cards {
		// 停用组或非数据组没有“剩余张数”语义，跳过。
		if !card.Enabled || card.Type != "data" {
			continue
		}
		// name 是去空白后的组名；空名回退为未命名组，保证行可读。
		name := strings.TrimSpace(card.Name)
		if name == "" {
			name = "未命名组"
		}
		// count 是该组剩余未用的非空卡密行数。
		count := countNonEmptyLines(card.DataContent)
		lines = append(lines, fmt.Sprintf("- %s：%d 张", name, count))
	}
	return lines, nil
}

// countNonEmptyLines 统计卡密正文中非空行的数量，代表剩余未用张数。
// 函数只读取行是否为空，不保留也不返回任何卡密内容。
func countNonEmptyLines(content string) int {
	// count 是累计的非空行数。
	count := 0
	// line 表示当前遍历到的原始行。
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// deliveryCodeKeywords 是卡密/资源码类内容关键词，用于识别先码后链顺序。
var deliveryCodeKeywords = []string{"资源码", "提取码", "网盘码", "卡密", "激活码"}

// deliveryLinkKeywords 是链接类内容关键词，用于识别先码后链顺序。
var deliveryLinkKeywords = []string{"链接", "网盘", "url", "http"}

// inferDeliveryHints 按商品标题/描述/详情与发货配置推断发货方式说明。
// 无线索时返回空串（调用方不注入）；来源 b 查询失败时降级为只用来源 a。
func (a *AIReplierImpl) inferDeliveryHints(ctx context.Context, itemID string) (string, error) {
	// corpus 是收集到的全部线索文本（商品文案 + 模板消息 + 卡组名）。
	var corpus strings.Builder
	// multiMessage 是发货模板最大消息条数；大于 1 时标注分条发送。
	multiMessage := 0
	// codeThenLink 表示模板消息按先资源码后链接的顺序发送。
	codeThenLink := false
	// item、err 是当前商品详情；不存在时只用发货配置线索。
	if item, err := a.store.Items.Get(ctx, a.cookieID, itemID); err == nil && item != nil {
		corpus.WriteString(item.ItemTitle)
		corpus.WriteString("\n")
		corpus.WriteString(item.ItemDescription)
		corpus.WriteString("\n")
		corpus.WriteString(item.ItemDetail)
	} else if err != nil && !errors.Is(err, db.ErrNotFound) {
		return "", err
	}
	// sourceB 表示是否成功读到自动化发货配置（来源 b）；查询失败降级为只用来源 a。
	// cardIDs 是发货规则绑定的卡券标识，用于读取卡密自身识别出的权威渠道。
	sourceB, cardIDs, configErr := a.appendDeliveryConfigClues(ctx, itemID, &corpus, &multiMessage, &codeThenLink)
	if configErr != nil {
		a.logger.Debug("发货配置线索查询失败，降级为只用商品描述", "err", configErr)
		sourceB = false
	}
	// authoritative 是卡密正文识别出的确定渠道，优先于文案关键词探测。
	var authoritative []string
	if len(cardIDs) > 0 {
		// channels、err 是卡券渠道查询结果；失败只记日志，回落到文案探测。
		channels, err := a.store.Cards.DeliveryChannelsByIDs(ctx, cardIDs)
		if err != nil {
			a.logger.Debug("卡券发货渠道查询失败，降级为文案探测", "err", err)
		} else {
			authoritative = channels
		}
	}
	// text 是汇总后的线索全文，用于关键词与模式探测。
	text := corpus.String()
	// stepwise 表示线索含分步发送模式（描述模式或模板先码后链）。
	stepwise := containsAny(text, deliveryStepwisePatterns) || codeThenLink
	// channels 是去重后的渠道展示名；有权威渠道时以它为准。
	channels := authoritative
	if len(channels) == 0 {
		channels = detectDeliveryChannels(text)
	}
	// 无任何渠道且无分条/分步线索时不注入。
	if len(channels) == 0 && multiMessage <= 1 && !stepwise {
		return "", nil
	}
	return formatDeliveryHint(channels, stepwise, multiMessage, sourceB, len(authoritative) > 0), nil
}

// appendDeliveryConfigClues 把商品相关自动化规则绑定的模板消息与卡组名追加进线索文本。
// 返回是否读到发货配置来源与该商品绑定的卡券标识集合；多条消息时抬高 multiMessage，并探测先码后链顺序。
// 卡券标识用于读取卡密自身识别出的权威发货渠道，比文案关键词探测更可靠。
func (a *AIReplierImpl) appendDeliveryConfigClues(ctx context.Context, itemID string, corpus *strings.Builder, multiMessage *int, codeThenLink *bool) (bool, []int64, error) {
	// ownerID 是当前账号所属用户主键，用于隔离自动化规则查询。
	ownerID, err := a.store.Cookies.GetOwnerID(ctx, a.cookieID)
	if err != nil {
		return false, nil, err
	}
	// rules、err 是该用户在当前账号下的规则；再按商品过滤出相关发货规则。
	rules, _, err := a.store.Automation.ListPageForUser(ctx, db.AutomationRuleListFilter{UserID: ownerID, CookieID: a.cookieID})
	if err != nil {
		return false, nil, err
	}
	// found 表示是否命中该商品（或账号级）的自动化规则。
	found := false
	// cardIDs 是本次收集到的卡券标识，含动作直接绑定与模板变量绑定两类来源。
	cardIDs := make([]int64, 0, 4)
	// rule 表示当前遍历到的自动化规则。
	for _, rule := range rules {
		// 只关心绑定到本商品或账号级（ItemID 为空）的规则。
		if rule.ItemID != "" && rule.ItemID != itemID {
			continue
		}
		found = true
		// action 表示当前遍历到的规则动作。
		for _, action := range rule.Actions {
			if !action.Enabled {
				continue
			}
			// 动作直接绑定的卡券是发货主体，其渠道即该商品的发货渠道。
			if action.CardID > 0 {
				cardIDs = append(cardIDs, action.CardID)
			}
			corpus.WriteString(action.MessageTemplate)
			corpus.WriteString("\n")
			corpus.WriteString(action.CardName)
			corpus.WriteString("\n")
			corpus.WriteString(action.DeliveryTemplateName)
			corpus.WriteString("\n")
			// msgs 是该动作的模板消息列表，用于先码后链顺序探测。
			msgs := action.TemplateMessages
			// msg 表示当前遍历到的模板消息正文。
			for _, msg := range msgs {
				corpus.WriteString(msg)
				corpus.WriteString("\n")
			}
			// binding 表示当前遍历到的卡密变量绑定。
			for _, binding := range action.TemplateBindings {
				// 模板变量引用的卡券同样参与发货，其渠道一并计入。
				if binding.CardID > 0 {
					cardIDs = append(cardIDs, binding.CardID)
				}
				corpus.WriteString(binding.CardName)
				corpus.WriteString("\n")
			}
			// 多条模板消息体现分条发送（先码后链等）。
			if len(msgs) > *multiMessage {
				*multiMessage = len(msgs)
			}
			// 模板消息先出现码类内容、后出现链接类内容时视为先码后链。
			if len(msgs) > 1 && !*codeThenLink && isCodeThenLinkOrder(msgs) {
				*codeThenLink = true
			}
		}
	}
	return found, cardIDs, nil
}

// detectDeliveryChannels 扫描线索文本并按优先级返回去重后的渠道展示名。
func detectDeliveryChannels(text string) []string {
	// channels 是按探测顺序去重的渠道展示名。
	channels := make([]string, 0, 3)
	// seen 记录已加入的展示名，避免「网盘码」「网盘」重复输出。
	seen := make(map[string]struct{}, 3)
	// entry 表示当前遍历到的渠道关键词项。
	for _, entry := range deliveryChannelKeywords {
		if !strings.Contains(text, entry.keyword) {
			continue
		}
		// seen 表示该展示名是否已加入过；ok 是查表命中标记。
		if _, ok := seen[entry.label]; ok {
			continue
		}
		seen[entry.label] = struct{}{}
		channels = append(channels, entry.label)
	}
	return channels
}

// formatDeliveryHint 把渠道、分步、分条线索拼成一行发货方式说明。
// sourceB 表示是否含发货配置来源，影响括号里的推断依据描述。
// authoritative 表示渠道来自卡密正文本身（确定事实），此时提示词会明确要求 AI 直接回答。
func formatDeliveryHint(channels []string, stepwise bool, multiMessage int, sourceB bool, authoritative bool) string {
	// parts 是提示词中跟在前缀后的分号段落。
	parts := make([]string, 0, 3)
	if len(channels) > 0 {
		parts = append(parts, strings.Join(channels, "、"))
	}
	// multi 表示是否标注分 N 条消息发送。
	multi := multiMessage > 1
	if multi {
		// note 是分条发送说明；先码后链模式在括号中点明。
		note := fmt.Sprintf("分 %d 条消息发送", multiMessage)
		if stepwise {
			note += "（先资源码后链接）"
		}
		parts = append(parts, note)
	} else if stepwise {
		parts = append(parts, "分步发送")
	}
	// basis 是括号里的推断依据：按实际来源标注描述/发货配置/卡密。
	basis := "按描述/发货配置推断，仅参考"
	if !sourceB {
		basis = "按描述推断，仅参考"
	}
	if authoritative {
		// 卡密正文自带链接，渠道是确定事实，提示词应让 AI 直接回答而非含糊带过。
		basis = "按该商品卡密内容确定，可直接回答"
	}
	return fmt.Sprintf("该商品发货方式（%s）：%s", basis, strings.Join(parts, "；"))
}

// isCodeThenLinkOrder 判断模板消息是否按先资源码后链接的顺序发送。
// 规则：靠前消息含码类关键词且更靠后消息含链接类关键词即视为先码后链。
func isCodeThenLinkOrder(messages []string) bool {
	// firstLink 是第一条含链接类关键词的消息下标；-1 表示尚未出现。
	firstLink := -1
	// lastCode 是最后一条含码类关键词的消息下标；-1 表示尚未出现。
	lastCode := -1
	// index、message 表示当前遍历到的消息位置与正文。
	for index, message := range messages {
		if containsAny(message, deliveryCodeKeywords) {
			lastCode = index
		}
		if firstLink < 0 && containsAny(message, deliveryLinkKeywords) {
			firstLink = index
		}
	}
	return lastCode >= 0 && firstLink >= 0 && lastCode < firstLink
}

// containsAny 判断 text 是否包含 patterns 中任一子串。
func containsAny(text string, patterns []string) bool {
	// pattern 表示当前遍历到的模式子串。
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}
