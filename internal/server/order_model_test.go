package server

import (
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// TestOrderDTOPaidAndShippedMapping 验证订单 DTO 的付款与发货时间字段映射及空值降级。
func TestOrderDTOPaidAndShippedMapping(t *testing.T) {
	// row 是含付款与发货时间的订单列表行。
	row := orderapp.OrderRow{
		OrderID: "sla-order", CookieID: "acc1", OrderStatus: "pending_ship",
		PaidAt: "2026-09-30T10:00:00Z", ShippedAt: "2026-09-30T11:00:00Z",
	}
	// view 是列表映射结果。
	view := orderDTOFromRow(row)
	if view.PaidAt == nil || *view.PaidAt != "2026-09-30T10:00:00Z" {
		t.Fatalf("列表 paid_at 映射异常: %+v", view.PaidAt)
	}
	if view.ShippedAt == nil || *view.ShippedAt != "2026-09-30T11:00:00Z" {
		t.Fatalf("列表 shipped_at 映射异常: %+v", view.ShippedAt)
	}

	// emptyRow 是无付款与发货时间的订单行。
	emptyRow := orderapp.OrderRow{OrderID: "empty-order", CookieID: "acc1"}
	// emptyView 是空时间映射结果。
	emptyView := orderDTOFromRow(emptyRow)
	if emptyView.PaidAt != nil || emptyView.ShippedAt != nil {
		t.Fatalf("空时间应映射为 null: paid=%v shipped=%v", emptyView.PaidAt, emptyView.ShippedAt)
	}

	// order 是详情映射的订单实体，含数据库 UTC 时间文本格式。
	order := &orderapp.Order{
		OrderID: "detail-order", CookieID: "acc2",
		PaidAt:    "2026-09-30 10:00:00.000000000",
		ShippedAt: "2026-09-30 11:00:00",
	}
	// detailView 是详情映射结果。
	detailView := orderDTOFromOrder(order, nil)
	if detailView.PaidAt == nil || *detailView.PaidAt != "2026-09-30T10:00:00Z" {
		t.Fatalf("详情 paid_at 归一异常: %+v", detailView.PaidAt)
	}
	if detailView.ShippedAt == nil || *detailView.ShippedAt != "2026-09-30T11:00:00Z" {
		t.Fatalf("详情 shipped_at 归一异常: %+v", detailView.ShippedAt)
	}

	// badOrder 是付款时间为非法文本的实体，映射应保留原值而不崩溃。
	badOrder := &orderapp.Order{OrderID: "bad-order", PaidAt: "not-a-time"}
	// badView 是非法时间映射结果。
	badView := orderDTOFromOrder(badOrder, nil)
	if badView.PaidAt == nil || *badView.PaidAt != "not-a-time" {
		t.Fatalf("非法时间应原样透出: %+v", badView.PaidAt)
	}
}

// TestApplyDeliverySLA 验证 SLA 默认值、账号覆盖与关闭分支。
func TestApplyDeliverySLA(t *testing.T) {
	// paidRef 是付款时间响应字段。
	paidRef := "2026-09-30T10:00:00Z"
	// cases 覆盖默认、覆盖、关闭与解析失败降级。
	cases := []struct {
		// name 是当前用例名称。
		name string
		// dto 是待补充 SLA 的订单响应。
		dto orderDTO
		// cfg 是 SLA 配置。
		cfg deliverySLAConfig
		// wantMinutes 是期望的 sla_minutes。
		wantMinutes int
		// wantDeadline 是期望的 sla_deadline；空串表示应为 nil。
		wantDeadline string
	}{
		{
			name: "默认值生效", dto: orderDTO{CookieID: "acc1", PaidAt: &paidRef},
			cfg: deliverySLAConfig{DefaultMinutes: 120}, wantMinutes: 120, wantDeadline: "2026-09-30T12:00:00Z",
		},
		{
			name: "账号覆盖优先", dto: orderDTO{CookieID: "acc2", PaidAt: &paidRef},
			cfg: deliverySLAConfig{DefaultMinutes: 120, PerAccount: map[string]int{"acc2": 30}},
			wantMinutes: 30, wantDeadline: "2026-09-30T10:30:00Z",
		},
		{
			name: "账号覆盖零值关闭", dto: orderDTO{CookieID: "acc3", PaidAt: &paidRef},
			cfg: deliverySLAConfig{DefaultMinutes: 60, PerAccount: map[string]int{"acc3": 0}},
			wantMinutes: 0, wantDeadline: "",
		},
		{
			name: "默认关闭", dto: orderDTO{CookieID: "acc1", PaidAt: &paidRef},
			cfg: deliverySLAConfig{}, wantMinutes: 0, wantDeadline: "",
		},
		{
			name: "无付款时间降级", dto: orderDTO{CookieID: "acc1"},
			cfg: deliverySLAConfig{DefaultMinutes: 60}, wantMinutes: 0, wantDeadline: "",
		},
		{
			name: "付款时间解析失败降级", dto: orderDTO{CookieID: "acc1", PaidAt: strPtr("bad-time")},
			cfg: deliverySLAConfig{DefaultMinutes: 60}, wantMinutes: 0, wantDeadline: "",
		},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// got 是实际补充后的响应。
		got := tc.dto
		applyDeliverySLA(&got, tc.cfg)
		if got.SLAMinutes != tc.wantMinutes {
			t.Fatalf("%s: sla_minutes=%d want=%d", tc.name, got.SLAMinutes, tc.wantMinutes)
		}
		if tc.wantDeadline == "" {
			if got.SLADeadline != nil {
				t.Fatalf("%s: sla_deadline 应为 nil，实际=%v", tc.name, *got.SLADeadline)
			}
		} else if got.SLADeadline == nil || *got.SLADeadline != tc.wantDeadline {
			t.Fatalf("%s: sla_deadline=%v want=%s", tc.name, got.SLADeadline, tc.wantDeadline)
		}
	}
}

// TestParseDeliverySLAConfig 验证 SLA 设置值解析的降级与覆盖提取。
func TestParseDeliverySLAConfig(t *testing.T) {
	// cases 覆盖空值、非法 JSON、合法形状与非正值过滤。
	cases := []struct {
		// name 是当前用例名称。
		name string
		// raw 是设置原文。
		raw string
		// wantDefault 是期望的默认分钟数。
		wantDefault int
		// wantOverride 是期望的账号覆盖值。
		wantOverride int
		// wantHasOverride 表示账号覆盖是否存在。
		wantHasOverride bool
	}{
		{name: "空串关闭", raw: "", wantDefault: 0},
		{name: "非法JSON降级", raw: "{bad", wantDefault: 0},
		{name: "零值关闭", raw: `{"default_minutes":0}`, wantDefault: 0},
		{name: "默认值", raw: `{"default_minutes":90}`, wantDefault: 90},
		{name: "账号覆盖", raw: `{"default_minutes":90,"per_account":{"acc1":15}}`, wantDefault: 90, wantOverride: 15, wantHasOverride: true},
		{name: "非正覆盖被过滤", raw: `{"default_minutes":90,"per_account":{"acc1":-1}}`, wantDefault: 90, wantHasOverride: false},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// cfg 是解析结果。
		cfg := parseDeliverySLAConfig(tc.raw)
		if cfg.DefaultMinutes != tc.wantDefault {
			t.Fatalf("%s: default=%d want=%d", tc.name, cfg.DefaultMinutes, tc.wantDefault)
		}
		// override、ok 读取账号覆盖。
		override, ok := cfg.PerAccount["acc1"]
		if ok != tc.wantHasOverride {
			t.Fatalf("%s: hasOverride=%v want=%v", tc.name, ok, tc.wantHasOverride)
		}
		if ok && override != tc.wantOverride {
			t.Fatalf("%s: override=%d want=%d", tc.name, override, tc.wantOverride)
		}
	}
}

// strPtr 返回字符串指针，供测试构造可空时间字段。
func strPtr(s string) *string {
	return &s
}
