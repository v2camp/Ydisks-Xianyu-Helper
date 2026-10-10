// Package netpan 识别网盘发货渠道：从卡密正文或发货文本中判定该卡券实际交付的是
// 百度网盘、夸克网盘还是两者都有（双盘），供 AI 客服回答「有夸克吗」「什么网盘」时
// 使用确定事实，而不是用「以商品详情为准」打太极。
//
// 判定优先用网盘分享域名（出现即确定，不受商品名或文案干扰）；域名缺失时回落关键词。
// 本包只依赖标准库，可被应用层和引擎层同时引用。
package netpan

import (
	"regexp"
	"strings"
)

// 渠道展示名，写入提示词与库字段，顺序即输出顺序。
const (
	// ChannelBaidu 是百度网盘。
	ChannelBaidu = "百度网盘"
	// ChannelQuark 是夸克网盘。
	ChannelQuark = "夸克网盘"
	// ChannelXunlei 是迅雷网盘。
	ChannelXunlei = "迅雷网盘"
	// ChannelAliyun 是阿里云盘。
	ChannelAliyun = "阿里云盘"
	// ChannelTianyi 是天翼云盘。
	ChannelTianyi = "天翼云盘"
	// Channel115 是 115 网盘。
	Channel115 = "115网盘"
	// ChannelUC 是 UC 网盘。
	ChannelUC = "UC网盘"
)

// channelRule 是一条渠道判定规则：域名命中即确定，关键词仅在无域名时兜底。
type channelRule struct {
	// label 是渠道展示名。
	label string
	// domains 是该渠道分享链接域名的正则集合，命中即确定，优先于关键词。
	domains []*regexp.Regexp
	// keywords 是域名缺失时的兜底关键词，按包含匹配。
	keywords []string
}

// channelRules 是全部渠道规则，按展示顺序排列。
var channelRules = []channelRule{
	{
		label:    ChannelBaidu,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)pan\.baidu\.com`)},
		keywords: []string{"百度网盘", "百度云盘", "度盘", "百度网盘APP"},
	},
	{
		label:    ChannelQuark,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)pan\.quark\.cn`)},
		keywords: []string{"夸克网盘", "夸克APP", "夸克"},
	},
	{
		label:    ChannelXunlei,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)pan\.xunlei\.com`)},
		keywords: []string{"迅雷网盘", "迅雷"},
	},
	{
		label:    ChannelAliyun,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)(alipan|aliyundrive)\.com`)},
		keywords: []string{"阿里云盘", "阿里网盘"},
	},
	{
		label:    ChannelTianyi,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)cloud\.189\.cn`)},
		keywords: []string{"天翼云盘"},
	},
	{
		label:    Channel115,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)(115\.com|115cdn\.com)`)},
		keywords: []string{"115网盘"},
	},
	{
		label:    ChannelUC,
		domains:  []*regexp.Regexp{regexp.MustCompile(`(?i)drive\.uc\.cn`)},
		keywords: []string{"UC网盘"},
	},
}

// Detect 从任意段正文中识别发货渠道，返回按规则顺序去重的渠道展示名。
// 同时含百度与夸克链接时返回两项，即双盘；无命中返回空切片。
// texts 可传入卡密正文、发货文本等多段内容，任一命中即计入。
func Detect(texts ...string) []string {
	// joined 是待判定的全部正文，拼接一次避免逐段重复扫描。
	joined := strings.Join(texts, "\n")
	if strings.TrimSpace(joined) == "" {
		return nil
	}
	// channels 是按规则顺序去重的渠道展示名。
	channels := make([]string, 0, 2)
	// rule 表示当前遍历到的渠道规则。
	for _, rule := range channelRules {
		if !rule.matches(joined) {
			continue
		}
		channels = append(channels, rule.label)
	}
	// 无命中时返回 nil 而非空切片，调用方可用 nil 判断「未能识别」。
	if len(channels) == 0 {
		return nil
	}
	return channels
}

// matches 判定单条规则是否命中正文：域名优先，域名未命中时回落关键词。
func (r channelRule) matches(text string) bool {
	// domain 表示当前遍历到的分享域名正则；命中即确定，不必再看关键词。
	for _, domain := range r.domains {
		if domain.MatchString(text) {
			return true
		}
	}
	// keyword 表示当前遍历到的兜底关键词；按包含匹配，忽略大小写差异。
	for _, keyword := range r.keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// Encode 把渠道列表编码为落库用的存储值，用逗号连接且不含空白；空列表返回空串。
func Encode(channels []string) string {
	return strings.Join(channels, ",")
}

// Decode 把落库值还原为渠道列表；空值返回空切片，空段被跳过。
func Decode(stored string) []string {
	// trimmed 是去首尾空白后的存储值。
	trimmed := strings.TrimSpace(stored)
	if trimmed == "" {
		return nil
	}
	// parts 是按逗号切分的原始渠道段。
	parts := strings.Split(trimmed, ",")
	// out 是剔除空白段后的渠道列表。
	out := make([]string, 0, len(parts))
	// part 表示当前遍历到的渠道段。
	for _, part := range parts {
		// channel 是去空白后的渠道展示名。
		channel := strings.TrimSpace(part)
		if channel == "" {
			continue
		}
		out = append(out, channel)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
