// livefacts.go 从样本的系统提示重建确定性守卫所需的最小事实。
//
// 评测链路不连数据库，只能用样本里的系统提示重建事实。这与 deriveExpectation 同源：
// 提示格式是评测口径的既有依赖，格式变了判定与守卫会一起失效，必须同批修。
package evalharness

import (
	"regexp"
	"strings"

	"xianyu-go/internal/engine"
)

// liveShippingLinePattern 匹配运费事实行（平台标注与平台未标注两种形态）。
var liveShippingLinePattern = regexp.MustCompile(`平台(?:标注运费信息|未标注运费与包邮信息)[^\n。]*。?`)

// liveItemFactSectionPattern 匹配「本商品确定事实」段及其后的「- 」条目。
var liveItemFactSectionPattern = regexp.MustCompile(`本商品确定事实[^\n]*：\n((?:-\s*[^\n]+\n?)+)`)

// reconstructLiveFacts 从系统提示重建守卫所需事实；无法识别的字段留空表示「无可用事实」。
// 留空而不是猜测，是守卫「没有事实不得断言」的前提。
func reconstructLiveFacts(prompt string) engine.PromptLiveFacts {
	// facts 是逐步填充的事实集合。
	facts := engine.PromptLiveFacts{}
	// match 是发货渠道声明的匹配结果。
	if match := deliveryChannelPattern.FindStringSubmatch(prompt); len(match) >= 4 {
		// basis 是旧格式的依据说明；为空表示命中的是新格式。
		basis := strings.TrimSpace(match[1])
		// raw 是匹配到的渠道正文。
		raw := strings.TrimSpace(match[2])
		if raw == "" {
			raw = strings.TrimSpace(match[3])
		}
		// 旧格式只有依据含「确定」时才是权威结论，与 deriveDeliveryExpectation 口径一致。
		if basis == "" || strings.Contains(basis, "确定") {
			// channels 是解析出的渠道名列表。
			if channels := splitChannels(raw); len(channels) > 0 {
				facts.DeliveryChannels = channels
				facts.DeliverySource = engine.DeliverySourceCard
			}
		}
	}
	// line 是提示里的运费事实行。
	if line := liveShippingLinePattern.FindString(prompt); line != "" {
		facts.ShippingFact = strings.TrimSpace(line)
	}
	// section 是商品确定事实段的匹配结果。
	if section := liveItemFactSectionPattern.FindStringSubmatch(prompt); len(section) >= 2 {
		// line 是当前遍历到的条目行。
		for _, line := range strings.Split(section[1], "\n") {
			// item 是去掉「- 」前缀后的条目。
			if item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-")); item != "" {
				facts.ItemFacts = append(facts.ItemFacts, item)
			}
		}
	}
	return facts
}
