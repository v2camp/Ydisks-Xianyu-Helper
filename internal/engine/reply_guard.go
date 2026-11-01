package engine

import "strings"

// replyDeflectionPhrases 是「已有确定事实时仍把买家推回商品详情页」的回避话术。
// 这批话术正是 2026-10 复盘里确定性评测 9/9 失败的全部形态：提示词已经给出渠道与规格事实，
// 模型仍然复述知识库里的通用免责句。提示词能压低概率但压不到零，所以必须有一层确定性兜底。
var replyDeflectionPhrases = []string{
	"以商品详情为准", "以商品详情页为准", "以商品详情页说明为准", "以详情页为准",
	"以商品详情和拍下后的发货消息为准", "以拍下后的发货消息为准",
	"详情里没标注", "详情页没标注", "详情里没有标注", "详情页没有标注",
	"我这边也说不准", "我也说不准", "说不准",
}

// guardMinKeptRunes 是清理回避句后仍可独立成句的最短字符数。
// 低于该长度说明回复几乎整段都在回避，此时只保留事实句比拼凑残句更可读。
const guardMinKeptRunes = 8

// guardDeflection 在「提示已给出确定事实」时清除回复中的回避话术。
// reply 是模型产出或已过价格守卫的回复文本；live 是本次注入的事实集合。
// 返回改写后的文本与是否发生了改写；没有可断言的事实时一律不改，
// 否则会把「本来就不该断言」的合法回避误改成编造。
func guardDeflection(reply string, live liveKnowledge) (string, bool) {
	// fact 是当前事实集合能支撑的结论句；hasFact 为假时不得改写回复。
	fact, hasFact := guardFactStatement(live)
	if !hasFact || !containsDeflection(reply) {
		return reply, false
	}
	// kept 是剔除回避内容后保留的句子。
	kept := make([]string, 0, 4)
	// sentence 是当前遍历到的句子。
	for _, sentence := range splitReplySentences(reply) {
		// trimmed 是该句去掉回避分句后的剩余内容；整句都是回避时为空。
		if trimmed := stripDeflectionClauses(sentence); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	// body 是保留句子拼成的正文。
	body := strings.Join(kept, "。")
	if len([]rune(body)) < guardMinKeptRunes {
		return fact, true
	}
	if strings.Contains(body, strings.TrimSuffix(fact, "。")) {
		return body + "。", true
	}
	return body + "。" + fact, true
}

// guardFactStatement 把事实集合整理成一句可断言的话，并声明是否足以断言。
// 只有两种事实可以断言：卡密正文识别出的渠道（card-channel）与商品标题抽取的规格。
// delivery-config 与 item-text 是文本规则猜出来的，拿它们断言等于把「编造渠道」换个方向重演，
// 因此刻意排除。
func guardFactStatement(live liveKnowledge) (string, bool) {
	if live.DeliverySource == deliverySourceCard {
		if len(live.DeliveryChannels) > 0 {
			return "本商品发货渠道为：" + strings.Join(live.DeliveryChannels, "、") + "。", true
		}
		// trimmed 是去掉首尾空白的渠道结论；空白表示没有可用结论。
		if trimmed := strings.TrimSpace(live.DeliveryHint); trimmed != "" {
			return ensureReplySentenceEnd(trimmed), true
		}
	}
	if len(live.ItemFacts) > 0 {
		return "本商品确定信息：" + strings.Join(live.ItemFacts, "；") + "。", true
	}
	return "", false
}

// containsDeflection 判断文本是否含任一回避话术。
func containsDeflection(text string) bool {
	// phrase 是当前遍历到的回避话术。
	for _, phrase := range replyDeflectionPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// splitReplySentences 按中英文句末标点与换行切分句子，并丢弃空白片段。
func splitReplySentences(reply string) []string {
	// raw 是按标点切分出的原始片段。
	raw := strings.FieldsFunc(reply, func(r rune) bool {
		switch r {
		case '。', '！', '？', '!', '?', '\n':
			return true
		}
		return false
	})
	// sentences 是去空白后的句子。
	sentences := make([]string, 0, len(raw))
	// fragment 是当前遍历到的原始片段。
	for _, fragment := range raw {
		// trimmed 是去空白后的片段；空片段不构成句子。
		if trimmed := strings.TrimSpace(fragment); trimmed != "" {
			sentences = append(sentences, trimmed)
		}
	}
	return sentences
}

// stripDeflectionClauses 删除一个句子中含回避话术的分句，保留其余分句。
// 分句边界取中文与英文逗号：整句丢弃会连带删掉「这款是双盘发货的」这类有用信息，
// 只删回避分句既去掉推诿又不丢事实。
func stripDeflectionClauses(sentence string) string {
	if !containsDeflection(sentence) {
		return sentence
	}
	// kept 是保留的分句。
	kept := make([]string, 0, 3)
	// clause 是当前遍历到的分句。
	for _, clause := range strings.FieldsFunc(sentence, func(r rune) bool {
		return r == '，' || r == ','
	}) {
		// trimmed 是去空白后的分句。
		trimmed := strings.TrimSpace(clause)
		if trimmed == "" || containsDeflection(trimmed) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, "，")
}

// ensureReplySentenceEnd 保证一句话以句末标点结尾，避免拼接后出现「…哦本商品」这类黏连。
func ensureReplySentenceEnd(text string) string {
	if strings.HasSuffix(text, "。") || strings.HasSuffix(text, "！") || strings.HasSuffix(text, "？") {
		return text
	}
	return text + "。"
}
