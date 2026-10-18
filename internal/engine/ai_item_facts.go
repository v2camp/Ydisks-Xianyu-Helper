// ai_item_facts.go 把平台商品数据整理成提示词可直接引用的事实。
//
// 背景：生产 26 个商品的 item_description 全为空，item_detail 全是平台 UI JSON
// （图片地址、标签样式、埋点参数）。原实现把 item_detail 当「商品描述」整段注入
// system prompt，既挤占预算又给模型一堆与买家问题无关的噪音。
//
// 同时，买家最常问的规格（几季/几集/几册）只存在于商品标题里。
// 标题本来就会被注入，但混在营销文案里，模型时常「看到却不敢用」，
// 于是出现同一商品一会儿「说不准」一会儿「34 集全集」的自相矛盾。
// 本文件把标题里的规格抽成独立事实行，让「标题写明的规格」变成可直答的事实。
package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// defaultItemDescription 是平台详情没有可读文案时的中性描述。
// 刻意不含「以详情为准」以外的承诺，避免与禁止编造清单冲突。
const defaultItemDescription = "平台未提供文字描述，具体以商品标题与详情页为准。"

// platformItemDetail 只解析商品详情 JSON 中买家关心的少量字段。
// 其余内容（图片、标签、埋点参数）一律丢弃，避免把 UI 数据注入提示词。
type platformItemDetail struct {
	// DetailParams 是平台详情中的业务字段集合。
	DetailParams struct {
		// PostInfo 是平台标注的运费/自提信息，如「包邮」「仅自提」。
		PostInfo string `json:"postInfo"`
		// Title 是平台侧商品标题，与 item_title 同源，仅用于校验。
		Title string `json:"title"`
	} `json:"detail_params"`
}

// itemSpecRangeRe 匹配标题中的区间规格，如「1-4 季」「30 到 34 集」「全 8 册」的前半段。
var itemSpecRangeRe = regexp.MustCompile(`(\d+)\s*[-~—－至到]\s*(\d+)\s*(季|集|册|期|本)`)

// itemSpecAllRe 匹配标题中的全量规格，如「全 8 册」「全12集」。
var itemSpecAllRe = regexp.MustCompile(`全\s*(\d+)\s*(季|集|册|期|本)`)

// itemSpecCountRe 匹配标题中的「N 集全」写法，如「47集全」「8册全」。
var itemSpecCountRe = regexp.MustCompile(`(\d+)\s*(季|集|册|期|本)\s*全`)

// looksLikePlatformJSON 判断文本是否平台结构化数据而非人写文案。
// 只看首个非空字符：以 { 或 [ 开头即视为结构化数据，内容一律不进提示词。
func looksLikePlatformJSON(text string) bool {
	// trimmed 是去掉首尾空白后的文本。
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

// sanitizeItemDescription 把商品描述整理成可注入提示词的文案。
// 人工填写的描述原样保留；平台 JSON 详情降级为中性描述，不再整段注入。
func sanitizeItemDescription(description, detail string) string {
	// desc 是人工填写的商品描述。
	if desc := strings.TrimSpace(description); desc != "" {
		return desc
	}
	// detailText 是剔除平台结构化数据后的详情文本。
	if detailText := strings.TrimSpace(detail); detailText != "" && !looksLikePlatformJSON(detailText) {
		return detailText
	}
	return defaultItemDescription
}

// extractItemSpecFacts 从商品标题抽取可直接回答的规格事实。
// 返回形如「1-4 季（共 4 季）」「全 8 册」「47 集」的短语，最多 3 条，去重后按标题出现顺序输出。
func extractItemSpecFacts(title string) []string {
	// trimmed 是去首尾空白后的标题；空标题没有可抽取内容。
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return nil
	}
	// facts 是按出现顺序累积的规格事实。
	facts := make([]string, 0, 3)
	// seen 记录已加入的短规范文本，避免「1-4 季」与「全 4 季」重复表达同一事实。
	seen := make(map[string]struct{}, 3)
	// appendFact 在未达上限且未重复时追加一条事实。
	appendFact := func(key, fact string) {
		if len(facts) >= 3 {
			return
		}
		// ok 表示该规格是否已登记过。
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		facts = append(facts, fact)
	}
	// match 是当前遍历到的区间规格匹配。
	for _, match := range itemSpecRangeRe.FindAllStringSubmatch(trimmed, -1) {
		// start、end、unit 分别是区间起点、终点与单位。
		start, end, unit := match[1], match[2], match[3]
		// startNum、endNum 是解析出的整数；解析失败或区间非法时跳过。
		var startNum, endNum int
		// err 表示区间起点解析失败。
		if _, err := fmt.Sscanf(start, "%d", &startNum); err != nil {
			continue
		}
		// err 表示区间终点解析失败。
		if _, err := fmt.Sscanf(end, "%d", &endNum); err != nil {
			continue
		}
		if endNum < startNum || endNum-startNum > 999 {
			continue
		}
		appendFact(fmt.Sprintf("%s-%s%s", start, end, unit),
			fmt.Sprintf("%s-%s %s（共 %d %s）", start, end, unit, endNum-startNum+1, unit))
	}
	// residual 是抹掉区间匹配后的标题，供全量与计数规则使用。
	// 必须先抹掉区间：否则「1-4 季全集」会被计数规则当成「4 季全」，
	// 抽出与区间矛盾的第二个规格（该误判由 TestExtractItemSpecFacts 抓到）。
	residual := itemSpecRangeRe.ReplaceAllString(trimmed, "  ")
	// match 是当前遍历到的全量规格匹配。
	for _, match := range itemSpecAllRe.FindAllStringSubmatch(residual, -1) {
		appendFact("all-"+match[1]+match[2], fmt.Sprintf("全 %s %s", match[1], match[2]))
	}
	// match 是当前遍历到的「N 集全」写法匹配。
	for _, match := range itemSpecCountRe.FindAllStringSubmatch(residual, -1) {
		appendFact("count-"+match[1]+match[2], fmt.Sprintf("%s %s", match[1], match[2]))
	}
	return facts
}

// extractTradePostInfo 从平台详情 JSON 读取运费/自提标注。
// 只承认平台明确写出的三种取值，其余一律返回空串，避免把非结构化内容当事实。
func extractTradePostInfo(detail string) string {
	// trimmed 是去空白后的详情文本；非 JSON 直接跳过。
	trimmed := strings.TrimSpace(detail)
	if !looksLikePlatformJSON(trimmed) {
		return ""
	}
	// parsed 是只含可读字段的解析结果；解析失败按无标注处理。
	var parsed platformItemDetail
	// err 表示详情 JSON 解析失败。
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return ""
	}
	// postInfo 是去空白后的平台标注文本。
	postInfo := strings.TrimSpace(parsed.DetailParams.PostInfo)
	switch postInfo {
	case "包邮", "不包邮", "仅自提":
		return postInfo
	default:
		return ""
	}
}
