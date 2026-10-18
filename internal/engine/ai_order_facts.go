// ai_order_facts.go 把订单履约状态整理成提示词可直接引用的事实。
//
// 为什么需要：买家最高频的售后追问是「付款了没收到 / 链接失效 / 提取码不对」。
// 这类问题有确定答案——订单发没发、什么时候发的——但此前 AI 读不到订单，
// 只能回「以发货消息为准」或直接转人工，把确定性问答题变成排队长尾。
//
// 关联键用 orders.chat_id：它是订单与聊天会话的天然关联，不需要靠买家昵称推断，
// 也就不存在把别人的订单信息答给当前买家的串单风险。
package engine

import (
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// orderFactInstruction 是注入订单履约事实时的段落标题。
const orderFactInstruction = "本会话订单事实（买家问付款、发货、链接、提取码时依此回答）"

// 订单履约状态取值，供提示词表述与测试断言共用。
const (
	// orderStateUnpaid 表示订单尚未付款。
	orderStateUnpaid = "尚未付款"
	// orderStatePaidUnshipped 表示已付款但尚未发货。
	orderStatePaidUnshipped = "已付款，尚未发货"
	// orderStateShipped 表示已付款并发货。
	orderStateShipped = "已付款并发货"
	// orderStateUnknown 表示订单状态无法判定，按不可答处理。
	orderStateUnknown = "状态无法判定"
)

// orderShippedStatuses 是平台侧可确认「已发货」的订单状态。
var orderShippedStatuses = map[string]struct{}{
	"shipped": {}, "delivering": {}, "completed": {}, "success": {}, "3": {},
}

// orderPaidStatuses 是平台侧可确认「已付款待发货」的订单状态。
var orderPaidStatuses = map[string]struct{}{
	"pending_ship": {}, "paid": {}, "2": {}, "pending_delivery": {},
	"partial_success": {}, "partial_pending_finalize": {},
}

// orderUnpaidStatuses 是平台侧表示「尚未付款」的订单状态。
var orderUnpaidStatuses = map[string]struct{}{
	"processing": {}, "1": {}, "pending_pay": {}, "wait_pay": {},
}

// orderFulfillmentState 判定订单的履约状态：发货标记优先于状态文本。
// SystemShipped 由本地发货动作写入，比平台状态文本更贴近实际交付。
func orderFulfillmentState(order *db.Order) string {
	if order == nil {
		return orderStateUnknown
	}
	if order.SystemShipped {
		return orderStateShipped
	}
	// status 是归一化后的平台订单状态。
	status := strings.ToLower(strings.TrimSpace(order.OrderStatus))
	// ok 表示该状态属于已发货集合。
	if _, ok := orderShippedStatuses[status]; ok {
		return orderStateShipped
	}
	// ok 表示该状态属于已付款待发货集合。
	if _, ok := orderPaidStatuses[status]; ok {
		return orderStatePaidUnshipped
	}
	// ok 表示该状态属于未付款集合。
	if _, ok := orderUnpaidStatuses[status]; ok {
		return orderStateUnpaid
	}
	// 有发货时间即视为已发货：状态文本缺失时时间戳仍是确定证据。
	if strings.TrimSpace(order.ShippedAt) != "" {
		return orderStateShipped
	}
	return orderStateUnknown
}

// formatOrderTime 把平台时间戳格式化为「月-日 时:分」。
// 平台写入的是 RFC3339 文本；解析失败时退化为原文本前 16 个字符，绝不臆造时间。
func formatOrderTime(raw string) string {
	// trimmed 是去空白后的时间文本。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// parsed、err 分别是解析后的时间与失败原因。
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		if len(trimmed) >= 16 {
			return trimmed[:16]
		}
		return trimmed
	}
	return parsed.Format("01-02 15:04")
}

// formatOrderFulfillment 把订单履约状态整理成一行可注入的事实说明。
// 状态无法判定时返回空串：宁可不说，也不能给买家一个错结论。
func formatOrderFulfillment(order *db.Order) string {
	// state 是判定出的履约状态。
	state := orderFulfillmentState(order)
	if state == orderStateUnknown {
		return ""
	}
	// detail 是状态后缀，用于补齐时间与发货指引。
	detail := ""
	switch state {
	case orderStateShipped:
		// shippedAt 是发货时间文本，缺失时不写时间。
		if shippedAt := formatOrderTime(order.ShippedAt); shippedAt != "" {
			detail = fmt.Sprintf("（发货时间 %s）", shippedAt)
		}
		return fmt.Sprintf("该买家在本商品的订单：%s%s。买家问链接或提取码时，据此说明已经发过，"+
			"并提示在订单的发货消息里查看；不得推测物流单号，也不得承诺补偿。", state, detail)
	case orderStatePaidUnshipped:
		// paidAt 是付款时间文本，缺失时不写时间。
		if paidAt := formatOrderTime(order.PaidAt); paidAt != "" {
			detail = fmt.Sprintf("（付款时间 %s）", paidAt)
		}
		return fmt.Sprintf("该买家在本商品的订单：%s%s。买家催发货时说明会按顺序安排，"+
			"不得承诺具体发出时间。", state, detail)
	default:
		return fmt.Sprintf("该买家在本商品的订单：%s。买家说已付款时，说明这边尚未查到付款记录，"+
			"请其确认是否已拍下付款，必要时转人工核实。", state)
	}
}
