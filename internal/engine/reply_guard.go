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

// deflectionTargets 是「把买家推回别处」的指向对象（商品详情 / 发货消息）。
// 只有与 deflectionPredicates 同时出现才判为回避：单独的「详情页写了 1-4 季」是陈述，不是推诿。
var deflectionTargets = []string{"商品详情", "详情页", "详情里", "发货消息"}

// deflectionPredicates 是推诿性判断词（以…为准 / 没标注 / 说不准）。
// 刻意不含「不确定」：它是正当的表达不确定，不能与指向对象共现就当回避改写。
var deflectionPredicates = []string{"为准", "没标注", "没有标注", "说不准"}

// containsDeflection 判断文本是否含回避话术。
// 先匹配枚举话术（覆盖「我这边也说不准」这类没有指向对象的固定句式），
// 再按「指向对象 + 推诿判断词」共现判定：精确子串匹配会漏掉
// 「以商品详情页说明和拍下后的发货消息为准」这类插入式变体，共现规则能兜住。
func containsDeflection(text string) bool {
	// phrase 是当前遍历到的枚举回避话术。
	for _, phrase := range replyDeflectionPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	// hasTarget 表示文本是否出现推诿指向对象；没有指向对象时直接判为非回避。
	hasTarget := false
	// target 是当前遍历到的推诿指向对象。
	for _, target := range deflectionTargets {
		if strings.Contains(text, target) {
			hasTarget = true
			break
		}
	}
	if !hasTarget {
		return false
	}
	// predicate 是当前遍历到的推诿判断词；与指向对象共现即为回避。
	for _, predicate := range deflectionPredicates {
		if strings.Contains(text, predicate) {
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

// complianceForbiddenRefundPhrases 是外发文本中一律不得出现的绝对化拒绝话术。
// 2026-11-07 真实跑分 compliance 组 2/7，其中一条就是「不支持退款」被机检判失败。
// 这类话术无法事后追偿，命中即整句替换为唯一默认话术。
var complianceForbiddenRefundPhrases = []string{
	"不支持退款", "概不退款", "一律不退", "无法退款", "不能退款",
}

// complianceRefundDefault 是退款禁语命中后的唯一默认话术。
const complianceRefundDefault = "虚拟商品发货后按平台规则处理；如资料失效或与描述不符，可联系我们协商补发或退款。"

// complianceAuthenticityPhrases 是无依据否定正版/授权的绝对断言。
// 商品信息通常不提供授权与正版资质，断言「不是」与断言「是」同样没有依据。
var complianceAuthenticityPhrases = []string{"不是正版", "不是正品", "非正版", "非正品"}

// complianceAuthenticityDefault 是正版否定断言命中后的唯一默认话术。
const complianceAuthenticityDefault = "商品信息未标注正版或授权信息，具体以商品详情页说明为准。"

// shippingClaimPhrases 是无条件承诺包邮的表述；仅在运费事实未确认包邮时改写。
var shippingClaimPhrases = []string{"包邮", "免运费", "不用另付运费", "不用付运费", "包运费"}

// shippingClaimSafeMarkers 是已带否定或限定的标记，命中说明不是无条件承诺，不应改写。
var shippingClaimSafeMarkers = []string{"不包邮", "不包运费", "不免运费", "未标注", "没有标注", "另行商议", "另议", "以拍下", "以下单"}

// shippingDefaultQualifier 是无运费事实时对包邮承诺的唯一默认限定话术。
const shippingDefaultQualifier = "运费以下单页面显示为准。"

// guardCompliance 在模型生成后、外发前做三类确定性合规改写：
// 退换货禁语、无依据包邮承诺、无依据正版否定断言。
// reply 是模型产出或已过价格守卫的回复文本；live 是本次注入的事实集合。
// 返回改写后的文本与是否发生改写；逐句处理以保留同一回复里的渠道等有效信息。
func guardCompliance(reply string, live liveKnowledge) (string, bool) {
	// changed 表示是否至少有一句被改写。
	changed := false
	// kept 是逐句处理后保留的句子。
	kept := make([]string, 0, len(splitReplySentences(reply)))
	// sentence 是当前遍历到的句子。
	for _, sentence := range splitReplySentences(reply) {
		// replacement、ok 是该句的合规替换话术与是否需要替换。
		if replacement, ok := complianceReplacement(sentence, live); ok {
			// 替换话术自带句号，拼句前去掉，避免与 Join 的分隔符叠加成「。。」。
			kept = append(kept, strings.TrimSuffix(replacement, "。"))
			changed = true
			continue
		}
		kept = append(kept, sentence)
	}
	if !changed {
		return reply, false
	}
	// body 是替换后拼成的正文。
	body := strings.TrimSpace(strings.Join(kept, "。"))
	if body == "" {
		return reply, false
	}
	return ensureReplySentenceEnd(body), true
}

// complianceReplacement 返回单句应替换成的话术；无需改写时 ok 为 false。
// 判定顺序固定：退款禁语 > 正版否定 > 包邮承诺，命中即返回，避免多规则互相覆盖。
func complianceReplacement(sentence string, live liveKnowledge) (string, bool) {
	// phrase 是当前遍历到的退款禁语。
	for _, phrase := range complianceForbiddenRefundPhrases {
		if strings.Contains(sentence, phrase) {
			return complianceRefundDefault, true
		}
	}
	// phrase 是当前遍历到的正版否定断言。
	for _, phrase := range complianceAuthenticityPhrases {
		if strings.Contains(sentence, phrase) {
			return complianceAuthenticityDefault, true
		}
	}
	if claimsFreeShipping(sentence) && !shippingFactConfirmsFreeShipping(live.ShippingFact) {
		// fact 是已注入的运费事实；有事实用事实施加限定，无事实用通用限定。
		if fact := strings.TrimSpace(live.ShippingFact); fact != "" {
			return fact, true
		}
		return shippingDefaultQualifier, true
	}
	return "", false
}

// claimsFreeShipping 判断单句是否在无条件承诺包邮。
// 先排除已带否定或限定的表述，避免把「不包邮」「平台未标注包邮信息」误判为承诺。
func claimsFreeShipping(sentence string) bool {
	// marker 是当前遍历到的限定/否定标记。
	for _, marker := range shippingClaimSafeMarkers {
		if strings.Contains(sentence, marker) {
			return false
		}
	}
	// phrase 是当前遍历到的包邮承诺表述。
	for _, phrase := range shippingClaimPhrases {
		if strings.Contains(sentence, phrase) {
			return true
		}
	}
	return false
}

// shippingFactConfirmsFreeShipping 判断运费事实是否明确标注包邮。
// 「不包邮」与「未标注…包邮信息」都含「包邮」字样，必须排除，否则会把无依据承诺当成事实放行。
func shippingFactConfirmsFreeShipping(fact string) bool {
	return strings.Contains(fact, "包邮") && !strings.Contains(fact, "不包邮") && !strings.Contains(fact, "未标注")
}

// ApplyReplyGuardsForEval 用与生产相同的确定性守卫改写一条模型回复，供离线评测复用。
// 与生产 applyAnswerGuards 的差异：不模拟价格守卫（评测样本不触发结构化改价），
// 只跑合规兜底与事实兜底，顺序与生产一致。
// 返回改写后的文本、最终答案来源（llm/policy-compliance/policy-guard）与是否发生改写。
func ApplyReplyGuardsForEval(reply string, facts PromptLiveFacts) (string, string, bool) {
	// live 是把导出事实转成守卫内部使用的形态。
	live := liveKnowledge{
		DeliverySource:   facts.DeliverySource,
		DeliveryChannels: facts.DeliveryChannels,
		ItemFacts:        facts.ItemFacts,
		ShippingFact:     facts.ShippingFact,
	}
	// out 是逐层改写后的文本。
	out := reply
	// source 是最终答案来源，未改写时为模型原稿。
	source := answerSourceLLM
	// changed 表示是否至少发生一次改写。
	changed := false
	// guarded、hit 是合规兜底后的文本与是否命中。
	if guarded, hit := guardCompliance(out, live); hit {
		out = guarded
		source = answerSourcePolicyCompliance
		changed = true
	}
	// guarded、hit 是事实兜底后的文本与是否命中。
	if guarded, hit := guardDeflection(out, live); hit {
		out = guarded
		source = answerSourcePolicyGuard
		changed = true
	}
	return out, source, changed
}
