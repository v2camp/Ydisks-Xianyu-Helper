// ai_order_facts_test.go 锁定订单履约事实的判定与表述，防止给出错误承诺。
package engine

import (
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestOrderFulfillmentState 覆盖发货标记优先、各状态别名与时间戳兜底。
func TestOrderFulfillmentState(t *testing.T) {
	// cases 覆盖各平台状态、发货标记优先与未知状态。
	cases := []struct {
		// name 是用例名称。
		name string
		// order 是订单记录。
		order *db.Order
		// want 是期望的履约状态。
		want string
	}{
		{name: "空订单", order: nil, want: orderStateUnknown},
		{name: "已发货", order: &db.Order{OrderStatus: "shipped"}, want: orderStateShipped},
		{name: "已完结", order: &db.Order{OrderStatus: "completed"}, want: orderStateShipped},
		{name: "本地发货标记优先于待发货状态", order: &db.Order{OrderStatus: "pending_ship", SystemShipped: true}, want: orderStateShipped},
		{name: "待发货", order: &db.Order{OrderStatus: "pending_ship"}, want: orderStatePaidUnshipped},
		{name: "已付款", order: &db.Order{OrderStatus: "paid"}, want: orderStatePaidUnshipped},
		{name: "待付款", order: &db.Order{OrderStatus: "processing"}, want: orderStateUnpaid},
		{name: "状态缺失但有发货时间", order: &db.Order{OrderStatus: "weird", ShippedAt: "2026-10-18T02:45:49Z"}, want: orderStateShipped},
		{name: "状态无法判定", order: &db.Order{OrderStatus: "weird"}, want: orderStateUnknown},
	}
	// tc 是当前遍历到的用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是判定出的履约状态。
			if got := orderFulfillmentState(tc.order); got != tc.want {
				t.Fatalf("履约状态不符 got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestFormatOrderFulfillment 验证事实文案包含状态、时间与禁止项约束。
func TestFormatOrderFulfillment(t *testing.T) {
	// shipped 是已发货订单的事实文案。
	shipped := formatOrderFulfillment(&db.Order{
		OrderStatus: "shipped", SystemShipped: true,
		ShippedAt: "2026-10-18T02:45:49.188235508Z",
	})
	if !strings.Contains(shipped, orderStateShipped) || !strings.Contains(shipped, "10-18 02:45") {
		t.Fatalf("已发货文案缺少状态或时间: %q", shipped)
	}
	if !strings.Contains(shipped, "不得推测物流单号") {
		t.Fatalf("已发货文案缺少禁止推测单号的约束: %q", shipped)
	}

	// unshipped 是已付款未发货订单的事实文案。
	unshipped := formatOrderFulfillment(&db.Order{
		OrderStatus: "pending_ship", PaidAt: "2026-10-18T01:00:00Z",
	})
	if !strings.Contains(unshipped, orderStatePaidUnshipped) || !strings.Contains(unshipped, "10-18 01:00") {
		t.Fatalf("待发货文案缺少状态或时间: %q", unshipped)
	}
	if !strings.Contains(unshipped, "不得承诺具体发出时间") {
		t.Fatalf("待发货文案缺少禁止承诺时间的约束: %q", unshipped)
	}

	// unpaid 是待付款订单的事实文案。
	unpaid := formatOrderFulfillment(&db.Order{OrderStatus: "processing"})
	if !strings.Contains(unpaid, orderStateUnpaid) || !strings.Contains(unpaid, "转人工") {
		t.Fatalf("待付款文案缺少状态或转人工指引: %q", unpaid)
	}

	// unknown 是状态不可判定的订单，应完全不注入。
	if got := formatOrderFulfillment(&db.Order{OrderStatus: "weird"}); got != "" {
		t.Fatalf("状态不可判定时不得注入事实 got=%q", got)
	}
}

// TestFormatOrderTime 验证时间格式化与解析失败时的退化行为。
func TestFormatOrderTime(t *testing.T) {
	// cases 覆盖空值、RFC3339 与不可解析文本。
	cases := []struct {
		// raw 是平台时间原文。
		raw string
		// want 是期望输出。
		want string
	}{
		{raw: "", want: ""},
		{raw: "2026-10-18T02:45:49.188235508Z", want: "10-18 02:45"},
		{raw: "2026-10-18 02:45:49", want: "2026-10-18 02:45"},
		{raw: "n/a", want: "n/a"},
	}
	// tc 是当前遍历到的用例。
	for _, tc := range cases {
		// got 是格式化后的时间文本。
		if got := formatOrderTime(tc.raw); got != tc.want {
			t.Fatalf("时间格式化不符 raw=%q got=%q want=%q", tc.raw, got, tc.want)
		}
	}
}
