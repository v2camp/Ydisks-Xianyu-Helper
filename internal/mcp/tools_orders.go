// tools_orders.go 注册订单履约、分析与自动化异常域工具。
//
// 手动发货、订单刷新等真实平台触达动作统一要求 confirm=true；输出 DTO 明确剔除
// 刷新过程中可能携带的 UpdatedCookies 等凭证字段。

package mcp

import (
	"context"
	"time"

	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	orderapp "xianyu-go/internal/application/orders"
)

// orderRowDTO 是订单列表的非敏感行视图（不含收货地址详情）。
type orderRowDTO struct {
	// OrderID 是订单标识。
	OrderID string `json:"order_id"`
	// ItemID 是关联商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是关联商品标题。
	ItemTitle string `json:"item_title"`
	// BuyerID 是买家标识。
	BuyerID string `json:"buyer_id"`
	// SpecName 是规格名。
	SpecName string `json:"spec_name"`
	// SpecValue 是规格值。
	SpecValue string `json:"spec_value"`
	// Quantity 是购买数量文本。
	Quantity string `json:"quantity"`
	// Amount 是订单金额文本。
	Amount string `json:"amount"`
	// Status 是订单状态。
	Status string `json:"status"`
	// AccountID 是归属账号标识。
	AccountID string `json:"account_id"`
	// IsBargain 表示是否砍价订单（1/0）。
	IsBargain int `json:"is_bargain"`
	// SystemShipped 表示是否系统已发货。
	SystemShipped bool `json:"system_shipped"`
	// CreatedAt 是订单创建时间文本。
	CreatedAt string `json:"created_at"`
	// PaidAt 是付款时间文本。
	PaidAt string `json:"paid_at"`
}

// orderDetailDTO 是订单详情视图；收货信息供人工履约核对，不含任何凭证字段。
type orderDetailDTO struct {
	orderRowDTO
	// ReceiverName 是收货人姓名。
	ReceiverName string `json:"receiver_name"`
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string `json:"receiver_phone"`
	// ReceiverCity 是收货城市。
	ReceiverCity string `json:"receiver_city"`
	// ReceiverAddress 是收货地址。
	ReceiverAddress string `json:"receiver_address"`
	// Version 是订单乐观锁版本。
	Version int `json:"version"`
}

// orderListResult 是订单分页返回。
type orderListResult struct {
	// Total 是总条数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// TotalPages 是总页数。
	TotalPages int `json:"total_pages"`
	// Orders 是当前页订单行。
	Orders []orderRowDTO `json:"orders"`
}

// refreshSummaryDTO 是批量刷新汇总视图。
type refreshSummaryDTO struct {
	// Discovered 是新发现订单数。
	Discovered int `json:"discovered"`
	// Restored 是恢复的软删订单数。
	Restored int `json:"restored"`
	// Reassigned 是纠正归属的订单数。
	Reassigned int `json:"reassigned"`
	// Updated 是列表更新数。
	Updated int `json:"updated"`
	// SoftDeleted 是软删数。
	SoftDeleted int `json:"soft_deleted"`
	// DetailTotal 是补全详情数。
	DetailTotal int `json:"detail_total"`
}

// manualShipItemDTO 是单个订单的手动发货结果。
type manualShipItemDTO struct {
	// OrderID 是订单标识。
	OrderID string `json:"order_id"`
	// Status 是逐单结果状态。
	Status string `json:"status"`
	// Success 表示该单是否成功。
	Success bool `json:"success"`
	// Message 是面向操作人员的结果说明。
	Message string `json:"message"`
	// ReconciliationWarning 是非阻断补偿警告。
	ReconciliationWarning string `json:"reconciliation_warning,omitempty"`
}

// manualShipResultDTO 是批量手动发货汇总。
type manualShipResultDTO struct {
	// SuccessCount 是成功单数。
	SuccessCount int `json:"success_count"`
	// FailedCount 是失败单数。
	FailedCount int `json:"failed_count"`
	// Results 是逐单结果。
	Results []manualShipItemDTO `json:"results"`
}

// refreshJobDTO 是刷新任务的非敏感快照（不含结果 JSON 与租约令牌）。
type refreshJobDTO struct {
	// ID 是任务标识。
	ID string `json:"job_id"`
	// AccountID 是目标账号；空表示全量。
	AccountID string `json:"account_id,omitempty"`
	// Status 是任务状态。
	Status string `json:"status"`
	// FilterStatus 是订单状态过滤条件。
	FilterStatus string `json:"filter_status,omitempty"`
	// ErrorMessage 是失败诊断。
	ErrorMessage string `json:"error_message,omitempty"`
}

// issueRunDTO 是待人工处理的运行异常视图。
type issueRunDTO struct {
	// RunID 是运行标识。
	RunID int64 `json:"run_id"`
	// AccountID 是归属账号。
	AccountID string `json:"account_id"`
	// OrderID 是关联订单。
	OrderID string `json:"order_id"`
	// TriggerType 是触发类型。
	TriggerType string `json:"trigger_type"`
	// IssueKind 是异常类别。
	IssueKind string `json:"issue_kind"`
	// ErrorMessage 是非敏感错误摘要。
	ErrorMessage string `json:"error_message"`
}

// issueDeferredDTO 是死信延期任务视图。
type issueDeferredDTO struct {
	// TaskID 是任务标识。
	TaskID int64 `json:"task_id"`
	// AccountID 是归属账号。
	AccountID string `json:"account_id"`
	// TriggerType 是触发类型。
	TriggerType string `json:"trigger_type"`
	// AttemptCount 是已尝试次数。
	AttemptCount int `json:"attempt_count"`
	// ErrorMessage 是非敏感错误摘要。
	ErrorMessage string `json:"error_message"`
	// UpdatedAt 是最近更新时间文本。
	UpdatedAt string `json:"updated_at"`
}

// issueListResult 是异常列表返回。
type issueListResult struct {
	// Runs 是待处理运行异常。
	Runs []issueRunDTO `json:"runs"`
	// Deferred 是死信延期任务。
	Deferred []issueDeferredDTO `json:"deferred"`
}

// RegisterOrderTools 注册订单、分析与异常域全部工具。
func (e *Endpoint) RegisterOrderTools(orders OrderPorts, analytics AnalyticsPorts, issues IssuePorts) {
	if orders != nil {
		e.registerOrderTools(orders)
	}
	if analytics != nil {
		e.registerAnalyticsTools(analytics)
	}
	if issues != nil {
		e.registerIssueTools(issues)
	}
}

// registerOrderTools 注册订单查询与履约动作工具，按查询、刷新任务与手动发货三组装配。
// p 是订单应用用例端口。
func (e *Endpoint) registerOrderTools(p OrderPorts) {
	e.registerOrderQueryTools(p)
	e.registerOrderRefreshTools(p)
	e.registerOrderFulfillmentTools(p)
}

// registerOrderQueryTools 注册订单列表与详情查询工具。
// p 是订单应用用例端口。
func (e *Endpoint) registerOrderQueryTools(p OrderPorts) {
	// accountIDArg 是订单工具通用的可选账号过滤参数。
	accountIDArg := ArgSpec{Name: "account_id", Type: ArgString, Description: "按账号标识过滤。"}
	e.RegisterTools(
		ToolDef{
			Name:        "order_list",
			Description: "分页查询订单，支持状态、账号与关键词（订单号/商品/买家）过滤。只读，列表不含收货地址详情。",
			Args: []ArgSpec{
				{Name: "page", Type: ArgInteger, Description: "页码，从 1 开始。"},
				{Name: "page_size", Type: ArgInteger, Description: "每页条数，默认 20，最大 200。"},
				{Name: "status", Type: ArgString, Description: "按订单状态过滤。"},
				accountIDArg,
				{Name: "search", Type: ArgString, Description: "订单号、商品或买家关键词。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// limit、offset 是归一化页大小与跳过条数，应用层页码从 1 开始。
				limit, offset := args.Page()
				// query 是订单列表用例入参。
				query := orderapp.ListQuery{
					UserID:   identity.UserID,
					CookieID: args.OptionalString("account_id", ""),
					Status:   args.OptionalString("status", ""),
					Search:   args.OptionalString("search", ""),
					Page:     offset/limit + 1,
					PageSize: limit,
				}
				// result、err 是分页结果。
				result, err := p.List(ctx, query)
				if err != nil {
					return nil, err
				}
				return orderListResult{
					Total: result.Total, Page: result.Page, PageSize: result.PageSize,
					TotalPages: result.TotalPages, Orders: orderRowsFromApp(result.Rows),
				}, nil
			},
		},
		ToolDef{
			Name:        "order_get",
			Description: "读取单个订单详情，含规格、金额、状态与收货信息；不返回 Cookie 或会话字段。只读。",
			Args:        []ArgSpec{{Name: "order_id", Type: ArgString, Required: true, Description: "目标订单标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是订单标识。
				id, err := args.String("order_id")
				if err != nil {
					return nil, err
				}
				// order、getErr 是订单实体。
				order, getErr := p.Get(ctx, identity.UserID, id)
				if getErr != nil {
					return nil, getErr
				}
				return orderDetailFromApp(order), nil
			},
		},
	)
}

// registerOrderRefreshTools 注册订单平台刷新与后台刷新任务管理工具。
// p 是订单应用用例端口。
func (e *Endpoint) registerOrderRefreshTools(p OrderPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "order_refresh_single",
			Description: "向平台刷新单个订单的最新状态与详情（真实平台触达，受风控约束）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "order_id", Type: ArgString, Required: true, Description: "目标订单标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是订单标识。
				id, err := args.String("order_id")
				if err != nil {
					return nil, err
				}
				// result、refreshErr 是单订单刷新结果。
				result, refreshErr := p.RefreshSingle(ctx, identity.UserID, id)
				if refreshErr != nil {
					return nil, refreshErr
				}
				return map[string]any{
					"order_id": id, "success": result.Success, "message": result.Message,
					"detail": map[string]any{
						"quantity": result.Detail.Quantity, "spec_name": result.Detail.SpecName,
						"spec_value": result.Detail.SpecValue, "status": result.Detail.OrderStatus,
						"amount": result.Detail.Amount,
					},
				}, nil
			},
		},
		ToolDef{
			Name: "order_refresh",
			Description: "向平台批量刷新订单：传 account_id 刷新该账号，传 status 刷新指定状态订单，都不传则全量。" +
				"真实平台触达，可能耗时较长。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Description: "按账号刷新；与 status 二选一或组合。"},
				{Name: "status", Type: ArgString, Description: "按订单状态刷新。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// result、err 是批量刷新结果。
				result, err := p.Refresh(ctx, identity.UserID,
					args.OptionalString("account_id", ""), args.OptionalString("status", ""))
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"partial_failure": result.PartialFailure, "message": result.Message,
					"summary": refreshSummaryFromApp(result.Summary),
				}, nil
			},
		},
		ToolDef{
			Name:        "order_refresh_job_create",
			Description: "创建后台批量订单刷新任务并立即排队执行；返回任务 ID 供查询。",
			Args: []ArgSpec{
				{Name: "account_id", Type: ArgString, Description: "限定账号；空表示全部账号。"},
				{Name: "status", Type: ArgString, Description: "限定订单状态。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// result、err 是任务启动结果。
				result, err := p.CreateRefreshJob(ctx, identity.UserID,
					args.OptionalString("account_id", ""), args.OptionalString("status", ""))
				if err != nil {
					return nil, err
				}
				if result.Job == nil {
					return nil, Fail(ClassInternal, "刷新任务创建后返回空任务", nil)
				}
				return refreshJobDTOFromApp(result.Job), nil
			},
		},
		ToolDef{
			Name:        "order_refresh_job_get",
			Description: "查询后台订单刷新任务的状态与失败原因。只读。",
			Args:        []ArgSpec{{Name: "job_id", Type: ArgString, Required: true, Description: "任务标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是任务标识。
				id, err := args.String("job_id")
				if err != nil {
					return nil, err
				}
				// job、getErr 是任务快照。
				job, getErr := p.GetRefreshJob(ctx, identity.UserID, id)
				if getErr != nil {
					return nil, getErr
				}
				if job == nil {
					return nil, Fail(ClassNotFound, "刷新任务不存在", nil)
				}
				return refreshJobDTOFromApp(job), nil
			},
		},
		ToolDef{
			Name:        "order_refresh_job_cancel",
			Description: "取消排队或运行中的后台订单刷新任务；不回滚已经写入的订单数据。",
			Args:        []ArgSpec{{Name: "job_id", Type: ArgString, Required: true, Description: "任务标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是任务标识。
				id, err := args.String("job_id")
				if err != nil {
					return nil, err
				}
				// result、cancelErr 是取消结果。
				result, cancelErr := p.CancelRefreshJob(ctx, identity.UserID, id)
				if cancelErr != nil {
					return nil, cancelErr
				}
				// status 是取消后的任务状态文本。
				status := ""
				if result.Job != nil {
					status = result.Job.Status
				}
				return map[string]any{"job_id": id, "cancelled": result.Cancelled, "status": status}, nil
			},
		},
	)
}

// registerOrderFulfillmentTools 注册订单手动发货工具。
// p 是订单应用用例端口。
func (e *Endpoint) registerOrderFulfillmentTools(p OrderPorts) {
	e.RegisterTools(
		ToolDef{
			Name: "order_manual_ship",
			Description: "手动发货：status_only 只标记本地已发货（不触发平台动作），full_delivery 执行完整发货链路，" +
				"严格沿用既有的砍价阶段与人工核对话术。真实平台触达，必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "order_ids", Type: ArgArray, Required: true, Description: "要发货的订单标识数组。"},
				{Name: "ship_mode", Type: ArgString, Required: true, Enum: []string{"status_only", "full_delivery"},
					Description: "发货模式：status_only=仅本地标记，full_delivery=完整平台发货。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// ids 是订单标识数组。
				ids, err := args.StringArray("order_ids")
				if err != nil {
					return nil, err
				}
				// mode 是发货模式。
				mode, err := args.String("ship_mode")
				if err != nil {
					return nil, err
				}
				// result、shipErr 是手动发货结果。
				result, shipErr := p.ManualShip(ctx, orderapp.ManualShipRequest{
					UserID: identity.UserID, OrderIDs: ids, ShipMode: mode,
				})
				if shipErr != nil {
					return nil, shipErr
				}
				return manualShipFromApp(result), nil
			},
		},
	)
}

// registerAnalyticsTools 注册仪表盘与分析工具。
func (e *Endpoint) registerAnalyticsTools(p AnalyticsPorts) {
	// dateRangeArgs 是分析工具通用的起止日期参数。
	dateRangeArgs := []ArgSpec{
		{Name: "start_date", Type: ArgString, Description: "起始日期，格式 YYYY-MM-DD（本地时区）。"},
		{Name: "end_date", Type: ArgString, Description: "结束日期，格式 YYYY-MM-DD（本地时区）。"},
	}
	e.RegisterTools(
		ToolDef{
			Name:        "analytics_dashboard",
			Description: "返回账号、卡密库存、关键词与订单的全局非敏感计数概览。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// stats、err 是仪表盘计数。
				stats, err := p.DashboardStats(ctx, identity.UserID)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"total_accounts":       stats.TotalCookies,
					"active_accounts":      stats.ActiveCookies,
					"total_cards":          stats.TotalCards,
					"available_card_stock": stats.AvailableCardStock,
					"total_keywords":       stats.TotalKeywords,
					"total_orders":         stats.TotalOrders,
				}, nil
			},
		},
		ToolDef{
			Name:        "analytics_orders",
			Description: "返回日期范围内的订单收益汇总，以及按日、按状态的聚合统计。只读。",
			Args:        dateRangeArgs,
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// analytics、err 是订单分析聚合。
				analytics, err := p.OrderAnalytics(ctx, analyticsQuery(identity.UserID, args))
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"total_orders": analytics.RevenueStats.TotalOrders,
					"total_amount": analytics.RevenueStats.TotalAmount,
					"daily":        analytics.DailyStats,
					"by_status":    analytics.StatusStats,
				}, nil
			},
		},
		ToolDef{
			Name:        "analytics_valid_orders",
			Description: "分页查询参与经营分析的有效订单明细（已归一化状态与金额）。只读。",
			Args: append(dateRangeArgs,
				ArgSpec{Name: "page", Type: ArgInteger, Description: "页码，从 1 开始。"},
				ArgSpec{Name: "page_size", Type: ArgInteger, Description: "每页条数，默认 20，最大 200。"}),
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// limit、offset 是归一化页大小与跳过条数，应用层页码从 1 开始。
				limit, offset := args.Page()
				// result、err 是有效订单分页结果。
				result, err := p.ValidOrders(ctx, analyticsQuery(identity.UserID, args), offset/limit+1, limit)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"total": result.Total, "page": result.Page, "page_size": result.PageSize,
					"truncated": result.Truncated, "orders": result.Orders,
				}, nil
			},
		},
	)
}

// registerIssueTools 注册自动化异常查询与人工处理工具。
func (e *Endpoint) registerIssueTools(p IssuePorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "issue_list",
			Description: "列出需要人工处理的自动化异常运行与死信延期任务（needs_review 队列）。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// runs、deferred、err 是两类异常列表。
				runs, deferred, err := p.ListIssues(ctx, identity.UserID)
				if err != nil {
					return nil, err
				}
				return issueListResult{Runs: issueRunsFromApp(runs), Deferred: issueDeferredFromApp(deferred)}, nil
			},
		},
		ToolDef{
			Name: "issue_resolve_run",
			Description: "对一条 needs_review 运行异常执行人工处理决议（由应用层校验决议是否合法）。" +
				"必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "run_id", Type: ArgInteger, Required: true, Description: "异常运行标识。"},
				{Name: "resolution", Type: ArgString, Required: true, Description: "人工处理决议（如 resolve/retry，由后端规则校验）。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// runID 是目标异常运行标识。
				runID, err := args.Int("run_id")
				if err != nil {
					return nil, err
				}
				// resolution、err 是人工决议及其校验错误。
				resolution, err := args.String("resolution")
				if err != nil {
					return nil, err
				}
				if // resolveErr 是运行异常处理用例返回的错误。
				resolveErr := p.ResolveRunIssue(ctx, identity.UserID, int64(runID), resolution); resolveErr != nil {
					return nil, resolveErr
				}
				return map[string]any{"resolved": true, "run_id": runID}, nil
			},
		},
		ToolDef{
			Name:        "issue_resolve_deferred",
			Description: "处理死信延期任务：retry 重新入队，dismiss 放弃。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				{Name: "task_id", Type: ArgInteger, Required: true, Description: "死信任务标识。"},
				{Name: "resolution", Type: ArgString, Required: true, Enum: []string{"retry", "dismiss"},
					Description: "retry=重新排队执行，dismiss=放弃该任务。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// taskID 是目标死信任务标识。
				taskID, err := args.Int("task_id")
				if err != nil {
					return nil, err
				}
				// resolution、err 是处理动作及其校验错误。
				resolution, err := args.String("resolution")
				if err != nil {
					return nil, err
				}
				if // resolveErr 是死信任务处理用例返回的错误。
				resolveErr := p.ResolveDeferredIssue(ctx, identity.UserID, int64(taskID), resolution); resolveErr != nil {
					return nil, resolveErr
				}
				return map[string]any{"resolved": true, "task_id": taskID, "resolution": resolution}, nil
			},
		},
	)
}

// analyticsQuery 从入参组装应用层分析查询，统一使用本机时区做日期边界。
func analyticsQuery(userID int64, args Arguments) analyticsapp.Query {
	return analyticsapp.Query{
		UserID:    userID,
		StartDate: args.OptionalString("start_date", ""),
		EndDate:   args.OptionalString("end_date", ""),
		Location:  time.Local,
	}
}

// orderRowsFromApp 批量映射订单列表行 DTO。
func orderRowsFromApp(rows []orderapp.OrderRow) []orderRowDTO {
	// items 是 MCP 行视图切片。
	items := make([]orderRowDTO, 0, len(rows))
	// row 是当前订单列表应用行。
	for _, row := range rows {
		items = append(items, orderRowFromApp(row))
	}
	return items
}

// orderRowFromApp 映射单行订单列表数据。
func orderRowFromApp(row orderapp.OrderRow) orderRowDTO {
	return orderRowDTO{
		OrderID: row.OrderID, ItemID: row.ItemID, ItemTitle: row.ItemTitle, BuyerID: row.BuyerID,
		SpecName: row.SpecName, SpecValue: row.SpecValue, Quantity: row.Quantity, Amount: row.Amount,
		Status: row.OrderStatus, AccountID: row.CookieID, IsBargain: row.IsBargain,
		SystemShipped: row.SystemShipped, CreatedAt: row.CreatedAt, PaidAt: row.PaidAt,
	}
}

// orderDetailFromApp 把订单实体映射为详情 DTO，明确不携带任何凭证字段。
func orderDetailFromApp(order *orderapp.Order) orderDetailDTO {
	// base 是列表字段部分。
	base := orderRowDTO{
		OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID,
		SpecName: order.SpecName, SpecValue: order.SpecValue, Quantity: order.Quantity,
		Amount: order.Amount, Status: order.OrderStatus, AccountID: order.CookieID,
		IsBargain: order.IsBargain,
	}
	return orderDetailDTO{
		orderRowDTO:     base,
		ReceiverName:    order.ReceiverName,
		ReceiverPhone:   order.ReceiverPhone,
		ReceiverCity:    order.ReceiverCity,
		ReceiverAddress: order.ReceiverAddress,
		Version:         order.Version,
	}
}

// refreshSummaryFromApp 映射批量刷新汇总。
func refreshSummaryFromApp(summary orderapp.RefreshSummary) refreshSummaryDTO {
	return refreshSummaryDTO{
		Discovered: summary.Discovered, Restored: summary.Restored, Reassigned: summary.Reassigned,
		Updated: summary.ListUpdated, SoftDeleted: summary.SoftDeleted, DetailTotal: summary.DetailTotal,
	}
}

// refreshJobDTOFromApp 映射刷新任务快照，剔除结果 JSON 与租约令牌。
func refreshJobDTOFromApp(job *orderapp.RefreshJob) refreshJobDTO {
	return refreshJobDTO{
		ID: job.ID, AccountID: job.CookieID, Status: job.Status,
		FilterStatus: job.FilterStatus, ErrorMessage: job.ErrorMessage,
	}
}

// manualShipFromApp 映射手动发货结果。
func manualShipFromApp(result orderapp.ManualShipResult) manualShipResultDTO {
	// items 是逐单结果切片。
	items := make([]manualShipItemDTO, 0, len(result.Results))
	// item 是当前逐单发货结果。
	for _, item := range result.Results {
		items = append(items, manualShipItemDTO{
			OrderID: item.OrderID, Status: item.Status, Success: item.Success,
			Message:               item.Message,
			ReconciliationWarning: item.ReconciliationWarning,
		})
	}
	return manualShipResultDTO{SuccessCount: result.SuccessCount, FailedCount: result.FailedCount, Results: items}
}

// issueRunsFromApp 映射运行异常列表。
func issueRunsFromApp(runs []automationapp.RunIssue) []issueRunDTO {
	// items 是异常视图切片。
	items := make([]issueRunDTO, 0, len(runs))
	// run 是当前运行异常记录。
	for _, run := range runs {
		items = append(items, issueRunDTO{
			RunID: run.ID, AccountID: run.CookieID, OrderID: run.OrderID,
			TriggerType: run.TriggerType, IssueKind: run.IssueKind, ErrorMessage: run.ErrorMessage,
		})
	}
	return items
}

// issueDeferredFromApp 映射死信任务列表。
func issueDeferredFromApp(deferred []automationapp.DeferredIssue) []issueDeferredDTO {
	// items 是死信视图切片。
	items := make([]issueDeferredDTO, 0, len(deferred))
	// item 是当前死信任务记录。
	for _, item := range deferred {
		items = append(items, issueDeferredDTO{
			TaskID: item.ID, AccountID: item.CookieID, TriggerType: item.TriggerType,
			AttemptCount: item.AttemptCount, ErrorMessage: item.ErrorMessage, UpdatedAt: item.UpdatedAt,
		})
	}
	return items
}
