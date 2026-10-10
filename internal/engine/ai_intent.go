// ai_intent.go 买家消息的意图标签与投诉扩展词表解析。
// 设计来源：同类项目把「意图注册表 + 规则前置（不确定就别猜）」作为客服路由的沉淀，
// 此处按本仓库约定用 Go 重写，并把「哪些消息交给 AI」的判定下沉到可配置的边界层。
//
// 意图判定与接管策略的唯一实现在 ai_scope.go：边界配置 ai_scope_config 未配置时回落到
// defaultScope（内置意图表达式），本文件不再重复维护一套规则前置分类。
//
// 当前消费方：ai.go 用 primaryIntentLabel 把边界层命中的意图集合收敛成写入对话历史的单一标签，
// 用 ParseComplaintKeywords 解析运维扩展的投诉负向词表。

package engine

import (
	"log/slog"
	"regexp"
	"strings"
)

// 意图常量集中定义，取值会写入 AI 对话历史的 intent 字段。
// 取值语义向后兼容说明：旧逻辑只写 "chat"/"bargain"/"reply" 三值；本文件将 "chat" 更名为
// "chitchat"（零命中兜底，语义一致），其余沿用；order/inquiry/consult/ambiguous
// 仅供意图分布观测与后续分流使用，前端/日志展示时不得把未知取值当成错误。
//
// 正负面判定不在本文件：负向拦截由 ai_scope.go 的 builtinComplaintExpr 与边界配置的
// negative 表达式负责，本文件的常量只承担「命中了什么」的标注职责。
const (
	// IntentComplaint 是负向标注标签：消息被内置/配置/扩展负向词表拦截时写入历史，
	// 表示该消息已判定为纠纷风险并拒绝 AI 接管。它不是正向意图，不参与接管决策。
	IntentComplaint = "complaint"
	// IntentBargain 是砍价意图。
	IntentBargain = "bargain"
	// IntentOrder 是订单与发货咨询意图。
	IntentOrder = "order"
	// IntentInquiry 是询价与物流政策意图。
	IntentInquiry = "inquiry"
	// IntentConsult 是商品内容咨询意图。
	IntentConsult = "consult"
	// IntentStock 是内容完整性咨询意图（全集/第几季/是否完结类）。
	IntentStock = "stock"
	// IntentRefund 是售前退款政策咨询意图（能不能退、支持退吗类）；
	// 售后纠纷表达（我要退款、退货）由负向词拦截转人工，不走本意图。
	IntentRefund = "refund"
	// IntentSpec 是商品规格咨询意图（册数/版本/规格/套装范围类）。
	IntentSpec = "spec"
	// IntentUsage 是使用与时效咨询意图（能不能用/有效期/是否会员类）。
	IntentUsage = "usage"
	// IntentChitchat 是闲聊兜底桶：边界层未命中任何意图时写入此类（即旧值 "chat" 的更名）。
	IntentChitchat = "chitchat"
	// IntentAmbiguous 是多意图同时命中时的归并标签：边界层无法确定单一意图，便于后续人工或分流策略复核。
	IntentAmbiguous = "ambiguous"
)

// ParseComplaintKeywords 把设置值（逗号或分号分隔，兼容全角）解析为已校验的正则列表，供投诉词表扩展使用。
// 非法正则被忽略并记告警；空值或仅空白返回 nil（调用方回落内置词表，宁可多拦不可少拦）。
func ParseComplaintKeywords(raw string, logger *slog.Logger) []*regexp.Regexp {
	// value 是去首尾空白后的原始设置值。
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	// parts 是按逗号或分号（含全角）切分的原始词表项。
	parts := strings.FieldsFunc(value, func(r rune) bool {
		// r 是当前待判断的分隔符字符。
		return r == ',' || r == ';' || r == '，' || r == '；'
	})
	// out 是解析通过且成功编译的扩展正则。
	out := make([]*regexp.Regexp, 0, len(parts))
	// part 表示当前遍历过程中的原始词表项。
	for _, part := range parts {
		// keyword 是当前遍历到的词表项（去空白后）。
		keyword := strings.TrimSpace(part)
		if keyword == "" {
			continue
		}
		// re 是用户提供的扩展正则；编译失败说明非法，必须忽略并告警，不能中断拦截。
		re, err := regexp.Compile(keyword)
		if err != nil {
			if logger != nil {
				logger.Warn("忽略非法投诉扩展正则，回落内置词表", "keyword", keyword, "err", err)
			}
			continue
		}
		out = append(out, re)
	}
	// 全部为空或非法时返回 nil，统一回落内置词表（宁可多拦不可少拦）。
	if len(out) == 0 {
		return nil
	}
	return out
}

// 意图落库标签的取值语义（写入 AI 对话历史 intent 字段，读取侧/前端需同步说明新增取值）：
//   - 单一意图命中：直接写该意图常量（bargain/order/inquiry/consult，以及边界配置自定义的 id）。
//   - 多意图同时命中（如「便宜点怎么发货」既砍价又问物流）：写 ambiguous，
//     表示边界层无法确定单一意图，便于后续人工或分流策略复核。
//   - 零命中：写 chitchat，表示当前边界未配置对应意图（即旧值 "chat" 的更名，语义一致）。
//
// primaryIntentLabel 把命中集合收敛成写入历史用的单一意图标签。
func primaryIntentLabel(hits []string) string {
	if len(hits) == 0 {
		return IntentChitchat
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return IntentAmbiguous
}
