package db

import (
	"context"
	"database/sql"
)

// PendingShipPaidOrdersAfter 按订单号稳定游标分页扫描「平台已确认付款、仍待我方发货」的订单，
// 只回填 SLA 提醒所需的事实列（订单号、账号、付款时间、发货时间）。
//
// 与 PendingShipOrdersWithoutPaidRunAfter 的分工：兜底发货扫描要求订单完全没有 order_paid
// 运行且存在可用规则，用于补触发发货；本方法面向发货 SLA 超时提醒，必须覆盖「已有运行但
// 仍未发货」的订单，因此不附加运行、规则与会话约束，只要求 order_status='pending_ship'
// 且 paid_at 非空。
func (a *AutomationRules) PendingShipPaidOrdersAfter(ctx context.Context, afterOrderID string, limit int) ([]Order, error) {
	// effectiveLimit 是单页扫描上限；非正数回落保守默认值，避免无界拉取。
	effectiveLimit := limit
	if effectiveLimit <= 0 {
		effectiveLimit = 200
	}
	// cursorSQL、args 组装可选的订单号游标条件，保证分页不重不漏。
	cursorSQL := ""
	// args 是按占位符顺序拼接的查询参数列表。
	args := []any{}
	if afterOrderID != "" {
		cursorSQL = " AND o.order_id>?"
		args = append(args, afterOrderID)
	}
	args = append(args, effectiveLimit)
	// rows、err 保存待发货扫描结果集及其查询错误。
	rows, err := a.DB.QueryContext(ctx, `
SELECT o.order_id, o.cookie_id, COALESCE(o.paid_at,''), COALESCE(o.shipped_at,'')
  FROM orders o
 WHERE o.order_status='pending_ship'
   AND o.deleted_at IS NULL
   AND COALESCE(o.paid_at,'')<>''`+cursorSQL+`
 ORDER BY o.order_id ASC
 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存本页订单事实；未设置的列保持零值，调用方只读 SLA 相关字段。
	out := []Order{}
	for rows.Next() {
		// ord 是当前行的订单事实；SLA 只依赖订单号、账号、付款与发货时间。
		var ord Order
		// cookieID、paidAt、shippedAt 保存可空列的扫描缓冲，空值归一为空字符串。
		var cookieID, paidAt, shippedAt sql.NullString
		// scanErr 是当前行四列扫描失败时的错误。
		if scanErr := rows.Scan(&ord.OrderID, &cookieID, &paidAt, &shippedAt); scanErr != nil {
			return nil, scanErr
		}
		ord.CookieID = cookieID.String
		ord.PaidAt = paidAt.String
		ord.ShippedAt = shippedAt.String
		ord.OrderStatus = "pending_ship"
		out = append(out, ord)
	}
	return out, rows.Err()
}
