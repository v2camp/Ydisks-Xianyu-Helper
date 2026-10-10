// tools_items_register.go 承载商品货架与批量发布域工具的注册装配：
// 本地商品目录读写、平台同步、单商品发布与批量发布批次管理按业务责任拆分为若干注册组。
//
// 仅做 ToolDef 的声明与分组，工具名、说明、入参、破坏性标记与注册顺序保持不变，
// 复杂入参映射的处理器仍由 tools_items_handlers.go 提供。

package mcp

import (
	"context"

	itemapp "xianyu-go/internal/application/items"

	"xianyu-go/internal/capability"
)

// RegisterItemTools 注册商品域全部工具；accounts 提供账号归属复核，items 提供商品用例。
func (e *Endpoint) RegisterItemTools(accounts capability.AccountPorts, items capability.ItemPorts) {
	if items == nil {
		return
	}
	// requireAccount 是本地商品写入前统一的归属复核闭包。
	requireAccount := func(ctx context.Context, identity *CallIdentity, cookieID string) error {
		if accounts == nil {
			return Fail(ClassInternal, "账号归属校验未装配", nil)
		}
		return accounts.RequireOwnership(ctx, identity.UserID, cookieID)
	}
	// accountArg 是商品工具通用的账号参数。
	accountArg := ArgSpec{Name: "account_id", Type: ArgString, Required: true, Description: "归属账号标识。"}
	e.registerItemCatalogReadTools(items, accountArg)
	e.registerItemCatalogWriteTools(items, accountArg, requireAccount)
	e.registerItemSyncTools(items, accountArg)
	e.registerItemPublishTools(items, accountArg, requireAccount)
	e.registerItemBatchTools(items)
}

// registerItemCatalogReadTools 注册本地商品只读工具：货架列表与单商品详情。
// items 是商品应用用例端口；accountArg 是通用的必填账号参数。
func (e *Endpoint) registerItemCatalogReadTools(items capability.ItemPorts, accountArg ArgSpec) {
	e.RegisterTools(
		ToolDef{
			Name:        "item_list",
			Description: "列出指定账号下的本地货架商品（标题、价格、多规格/多数量标记）。只读，不读取平台凭证。",
			Args:        []ArgSpec{accountArg},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// rows、listErr 是本地商品列表。
				rows, listErr := items.ListItems(ctx, identity.UserID, cookieID)
				if listErr != nil {
					return nil, listErr
				}
				return itemListResult{Total: len(rows), Items: itemDTOsFromApp(cookieID, rows)}, nil
			},
		},
		ToolDef{
			Name:        "item_get",
			Description: "读取单个本地商品详情（含描述与扩展详情 JSON）。只读。",
			Args: []ArgSpec{
				accountArg,
				{Name: "item_id", Type: ArgString, Required: true, Description: "平台商品标识。"},
			},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// itemID、err 是商品标识及其校验错误。
				itemID, err := args.String("item_id")
				if err != nil {
					return nil, err
				}
				// item、getErr 是商品记录。
				item, getErr := items.GetItem(ctx, cookieID, itemID)
				if getErr != nil {
					return nil, getErr
				}
				// dto 是该商品的 MCP 视图。
				dto := itemDTOFromApp(item)
				dto.AccountID = cookieID
				return map[string]any{"item": dto, "detail": item.ItemDetail}, nil
			},
		},
	)
}

// registerItemCatalogWriteTools 注册本地商品写入工具：创建、局部更新、交付标记与逻辑删除。
// items 是商品应用用例端口；accountArg 是必填账号参数；requireAccount 负责写入前归属复核。
func (e *Endpoint) registerItemCatalogWriteTools(items capability.ItemPorts, accountArg ArgSpec,
	requireAccount func(context.Context, *CallIdentity, string) error) {
	e.RegisterTools(
		ToolDef{
			Name:        "item_create",
			Description: "在本地货架创建或恢复商品记录（不触达平台；平台上架请用 item_publish_single）。",
			Args: []ArgSpec{
				accountArg,
				{Name: "item_id", Type: ArgString, Required: true, Description: "平台商品标识（账号内唯一键）。"},
				{Name: "title", Type: ArgString, Required: true, Description: "商品标题。"},
				{Name: "description", Type: ArgString, Description: "商品描述。"},
				{Name: "price", Type: ArgString, Description: "商品价格文本。"},
				{Name: "category", Type: ArgString, Description: "平台类目标识。"},
				{Name: "multi_spec", Type: ArgBoolean, Description: "是否多规格交付。"},
				{Name: "multi_quantity_delivery", Type: ArgBoolean, Description: "是否多数量交付。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				if // ownershipErr 是账号归属复核错误。
				ownershipErr := requireAccount(ctx, identity, cookieID); ownershipErr != nil {
					return nil, ownershipErr
				}
				// itemID、err 是商品标识及其校验错误。
				itemID, err := args.String("item_id")
				if err != nil {
					return nil, err
				}
				// title、err 是标题及其校验错误。
				title, err := args.String("title")
				if err != nil {
					return nil, err
				}
				// input 是创建商品的应用层输入。
				input := itemapp.CatalogWriteInput{
					ItemID: itemID, ItemTitle: title,
					ItemDescription:       args.OptionalString("description", ""),
					ItemPrice:             args.OptionalString("price", ""),
					ItemCategory:          args.OptionalString("category", ""),
					IsMultiSpec:           args.OptionalBool("multi_spec", false),
					MultiQuantityDelivery: args.OptionalBool("multi_quantity_delivery", false),
				}
				if // createErr 是本地商品创建错误。
				createErr := items.CreateItem(ctx, cookieID, input); createErr != nil {
					return nil, createErr
				}
				return map[string]any{"created": true, "account_id": cookieID, "item_id": itemID}, nil
			},
		},
		ToolDef{
			Name:        "item_update",
			Description: "局部更新本地商品字段：标题、描述、价格、类目、多规格/多数量标记；未传字段保持不变。",
			Args: []ArgSpec{
				accountArg,
				{Name: "item_id", Type: ArgString, Required: true, Description: "平台商品标识。"},
				{Name: "title", Type: ArgString, Description: "新标题。"},
				{Name: "description", Type: ArgString, Description: "新描述。"},
				{Name: "price", Type: ArgString, Description: "新价格文本。"},
				{Name: "category", Type: ArgString, Description: "新类目标识。"},
				{Name: "multi_spec", Type: ArgBoolean, Description: "多规格交付标记。"},
				{Name: "multi_quantity_delivery", Type: ArgBoolean, Description: "多数量交付标记。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// itemID、err 是商品标识及其校验错误。
				itemID, err := args.String("item_id")
				if err != nil {
					return nil, err
				}
				if // ownershipErr 是账号归属复核错误。
				ownershipErr := requireAccount(ctx, identity, cookieID); ownershipErr != nil {
					return nil, ownershipErr
				}
				// patch 是逐步填充的可选字段集合。
				patch := itemapp.CatalogPatchInput{}
				applyStringPatch(args, "title", &patch.ItemTitle)
				applyStringPatch(args, "description", &patch.ItemDescription)
				applyStringPatch(args, "price", &patch.ItemPrice)
				applyStringPatch(args, "category", &patch.ItemCategory)
				applyBoolPatch(args, "multi_spec", &patch.IsMultiSpec)
				applyBoolPatch(args, "multi_quantity_delivery", &patch.MultiQuantityDelivery)
				if // updateErr 是本地商品更新错误。
				updateErr := items.UpdateItem(ctx, cookieID, itemID, patch); updateErr != nil {
					return nil, updateErr
				}
				return map[string]any{"updated": true, "account_id": cookieID, "item_id": itemID}, nil
			},
		},
		itemBoolFlagTool("item_set_multi_spec", "更新商品多规格交付标记（本地配置，不触达平台）。",
			"multi_spec", items, requireAccount),
		itemBoolFlagTool("item_set_multi_quantity", "更新商品多数量交付标记（本地配置，不触达平台）。",
			"multi_quantity_delivery", items, requireAccount),
		ToolDef{
			Name:        "item_delete",
			Description: "逻辑删除本地商品及其商品级自动化规则（不可逆，不影响平台侧商品）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountArg,
				{Name: "item_id", Type: ArgString, Required: true, Description: "要删除的平台商品标识。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// itemID、err 是商品标识及其校验错误。
				itemID, err := args.String("item_id")
				if err != nil {
					return nil, err
				}
				if // ownershipErr 是账号归属复核错误。
				ownershipErr := requireAccount(ctx, identity, cookieID); ownershipErr != nil {
					return nil, ownershipErr
				}
				if // deleteErr 是商品删除用例错误。
				deleteErr := items.DeleteItem(ctx, cookieID, itemID); deleteErr != nil {
					return nil, deleteErr
				}
				return map[string]any{"deleted": true, "account_id": cookieID, "item_id": itemID}, nil
			},
		},
	)
}

// registerItemSyncTools 注册平台商品同步与类目推荐工具：全量同步、分页同步与关键词类目推荐。
// items 是商品应用用例端口；accountArg 是通用的必填账号参数。
func (e *Endpoint) registerItemSyncTools(items capability.ItemPorts, accountArg ArgSpec) {
	e.RegisterTools(
		ToolDef{
			Name: "item_sync_all",
			Description: "从平台全量同步账号在售商品并与本地记录对账（真实平台触达，可能新增/软删本地商品）。" +
				"必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountArg,
				{Name: "max_pages", Type: ArgInteger, Description: "最多读取页数；不传或 0 表示不限制。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// result、syncErr 是全量同步结果。
				result, syncErr := items.SyncItemsAll(ctx, itemapp.SyncQuery{
					UserID: identity.UserID, CookieID: cookieID, MaxPages: args.OptionalInt("max_pages", 0),
				})
				if syncErr != nil {
					return nil, syncErr
				}
				return syncAllResultDTO{
					TotalCount: result.TotalCount, TotalPages: result.TotalPages,
					SavedCount: result.SavedCount, DeletedCount: result.DeletedCount,
				}, nil
			},
		},
		ToolDef{
			Name:        "item_sync_page",
			Description: "从平台按页同步账号在售商品（真实平台触达）。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountArg,
				{Name: "page", Type: ArgInteger, Required: true, Description: "平台页码，从 1 开始。"},
				{Name: "page_size", Type: ArgInteger, Description: "单页条数。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// page、err 是平台页码及其校验错误。
				page, err := args.Int("page")
				if err != nil || page < 1 {
					return nil, InvalidArgument("page 必须是不小于 1 的整数")
				}
				// result、syncErr 是分页同步结果。
				result, syncErr := items.SyncItemsPage(ctx, itemapp.SyncQuery{
					UserID: identity.UserID, CookieID: cookieID, PageNumber: page,
					PageSize: args.OptionalInt("page_size", 0),
				})
				if syncErr != nil {
					return nil, syncErr
				}
				return syncPageResultDTO{
					PageNumber: result.PageNumber, PageSize: result.PageSize,
					CurrentCount: result.CurrentCount, SavedCount: result.SavedCount,
				}, nil
			},
		},
		ToolDef{
			Name:        "item_category_recommend",
			Description: "按商品关键词获取平台建议发布类目（只读平台查询）。",
			Args: []ArgSpec{
				accountArg,
				{Name: "keyword", Type: ArgString, Required: true, Description: "商品关键词，如“虚拟教程”。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// cookieID 是账号标识。
				cookieID, err := args.String("account_id")
				if err != nil {
					return nil, err
				}
				// keyword、err 是关键词及其校验错误。
				keyword, err := args.String("keyword")
				if err != nil {
					return nil, err
				}
				// category、recommendErr 是推荐类目。
				category, recommendErr := items.RecommendCategory(ctx, identity.UserID, cookieID, keyword)
				if recommendErr != nil {
					return nil, recommendErr
				}
				return category, nil
			},
		},
	)
}

// registerItemPublishTools 注册单商品发布与批量发布预检/创建工具。
// items 是商品应用用例端口；accountArg 是必填账号参数；requireAccount 负责发布前归属复核。
func (e *Endpoint) registerItemPublishTools(items capability.ItemPorts, accountArg ArgSpec,
	requireAccount func(context.Context, *CallIdentity, string) error) {
	e.RegisterTools(
		ToolDef{
			Name: "item_publish_single",
			Description: "向平台发布单个商品（真实平台触达）。图片仅接受公网 HTTP(S) URL；" +
				"不传类目时沿用自动推荐与电子资料兜底。必须显式 confirm=true。",
			Destructive: true,
			Args: []ArgSpec{
				accountArg,
				{Name: "title", Type: ArgString, Required: true, Description: "商品标题。"},
				{Name: "description", Type: ArgString, Description: "商品描述。"},
				{Name: "price_cents", Type: ArgInteger, Required: true, Description: "售价，单位分。"},
				{Name: "original_price_cents", Type: ArgInteger, Description: "原价，单位分。"},
				{Name: "quantity", Type: ArgInteger, Description: "库存数量，默认 1。"},
				{Name: "postage_mode", Type: ArgString, Enum: []string{"free", "fixed"}, Description: "邮费模式：free 包邮或 fixed 固定邮费。"},
				{Name: "postage_cents", Type: ArgInteger, Description: "固定邮费，单位分（postage_mode=fixed 时使用）。"},
				{Name: "image_urls", Type: ArgArray, ItemType: ArgString, Description: "商品图片公网 URL 数组。"},
				{Name: "category_cat_id", Type: ArgString, Description: "指定闲鱼类目 CatID；不传走自动推荐。"},
			},
			Handler: e.makePublishHandler(items, requireAccount),
		},
		ToolDef{
			Name: "item_batch_preview",
			Description: "对批量发布 JSON 行做本地预检（不触达平台、不落库）。每行是对象，字段与上传表格一致：" +
				"account_id、title、description、price、original_price、quantity、postage_mode、postage、image_urls、category。" +
				"图片只接受公网 URL。只读。",
			Args: []ArgSpec{
				{Name: "default_account_id", Type: ArgString, Description: "未指定 account_id 的行使用的默认账号。"},
				{Name: "rows", Type: ArgArray, Required: true, Description: "批量发布行对象数组。"},
			},
			Handler: e.makePreviewHandler(items),
		},
		ToolDef{
			Name: "item_batch_create",
			Description: "把批量发布 JSON 行预检通过后持久化为待发布批次，返回 batch_id（不自动开始发布）。" +
				"图片只接受公网 URL。",
			Args: []ArgSpec{
				{Name: "default_account_id", Type: ArgString, Description: "默认发布账号。"},
				{Name: "rows", Type: ArgArray, Required: true, Description: "批量发布行对象数组。"},
				{Name: "publish_interval_seconds", Type: ArgInteger, Description: "相邻发布请求最小间隔秒数。"},
				{Name: "filename", Type: ArgString, Description: "来源名称标记，便于审计识别。"},
			},
			Handler: e.makeBatchCreateHandler(items),
		},
	)
}

// registerItemBatchTools 注册批量发布批次管理工具：启动、列表、详情、结果导出、取消、重试与删除。
// items 是商品应用用例端口。
func (e *Endpoint) registerItemBatchTools(items capability.ItemPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "item_batch_start",
			Description: "启动一个已预检持久化的批量发布任务（真实平台触达，后台执行）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "batch_id", Type: ArgString, Required: true, Description: "item_batch_create 返回的批次标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// batchID 是批次标识。
				batchID, err := args.String("batch_id")
				if err != nil {
					return nil, err
				}
				// status、startErr 是启动结果状态（queued/running 等）。
				status, startErr := items.StartBatch(ctx, identity.UserID, batchID)
				if startErr != nil {
					return nil, startErr
				}
				return map[string]any{"batch_id": batchID, "start_status": status}, nil
			},
		},
		ToolDef{
			Name:        "item_batch_list",
			Description: "查询最近的批量发布批次及成功/失败计数。只读。",
			Args: []ArgSpec{
				{Name: "limit", Type: ArgInteger, Description: "返回条数，默认 20，最大 200。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// limit 是收敛后的条数上限。
				limit := args.OptionalInt("limit", defaultPageSize)
				if limit <= 0 || limit > maxPageSize {
					limit = maxPageSize
				}
				// rows、listErr 是批次列表。
				rows, listErr := items.ListBatches(ctx, identity.UserID, limit)
				if listErr != nil {
					return nil, listErr
				}
				// dtos 是批次视图列表。
				dtos := make([]batchInfoDTO, 0, len(rows))
				// row 是当前批次应用摘要。
				for _, row := range rows {
					dtos = append(dtos, batchInfoFromApp(row))
				}
				return map[string]any{"total": len(dtos), "batches": dtos}, nil
			},
		},
		ToolDef{
			Name:        "item_batch_get",
			Description: "查询单个批量发布批次的状态与逐行发布结果。只读。",
			Args:        []ArgSpec{{Name: "batch_id", Type: ArgString, Required: true, Description: "批次标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// batchID 是批次标识。
				batchID, err := args.String("batch_id")
				if err != nil {
					return nil, err
				}
				// details、getErr 是批次详情。
				details, getErr := items.GetBatch(ctx, identity.UserID, batchID)
				if getErr != nil {
					return nil, getErr
				}
				return batchDetailsFromApp(details), nil
			},
		},
		ToolDef{
			Name:        "item_batch_result_csv",
			Description: "导出批量发布批次的逐行结果 CSV 文本（含行号、状态、商品链接、失败原因）。只读。",
			Args:        []ArgSpec{{Name: "batch_id", Type: ArgString, Required: true, Description: "批次标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// batchID 是批次标识。
				batchID, err := args.String("batch_id")
				if err != nil {
					return nil, err
				}
				// details、getErr 是批次详情。
				details, getErr := items.GetBatch(ctx, identity.UserID, batchID)
				if getErr != nil {
					return nil, getErr
				}
				// csvText、csvErr 是导出的 CSV 文本。
				csvText, csvErr := batchResultCSV(details)
				if csvErr != nil {
					return nil, csvErr
				}
				return map[string]any{"batch_id": batchID, "content_type": "text/csv", "csv": csvText}, nil
			},
		},
		ToolDef{
			Name:        "item_batch_cancel",
			Description: "请求取消批量发布批次（不会回滚已发布商品）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "batch_id", Type: ArgString, Required: true, Description: "批次标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// batchID 是批次标识。
				batchID, err := args.String("batch_id")
				if err != nil {
					return nil, err
				}
				// status、cancelErr 是取消结果（canceling/canceled）。
				status, cancelErr := items.CancelBatch(ctx, identity.UserID, batchID)
				if cancelErr != nil {
					return nil, cancelErr
				}
				return map[string]any{"batch_id": batchID, "cancel_status": status}, nil
			},
		},
		ToolDef{
			Name:        "item_batch_retry_failed",
			Description: "让批次中失败的商品行重新排队发布（真实平台触达）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "batch_id", Type: ArgString, Required: true, Description: "批次标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// batchID、err 是批次标识及其校验错误。
				batchID, err := args.String("batch_id")
				if err != nil {
					return nil, err
				}
				// status、retryErr 是重试排队结果。
				status, retryErr := items.RetryBatch(ctx, identity.UserID, batchID)
				if retryErr != nil {
					return nil, retryErr
				}
				return map[string]any{"batch_id": batchID, "retry_status": status}, nil
			},
		},
		ToolDef{
			Name:        "item_batch_delete",
			Description: "删除非运行批次及其本地记录（不可逆，不影响已上架商品）。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "batch_id", Type: ArgString, Required: true, Description: "批次标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// batchID 是批次标识。
				batchID, err := args.String("batch_id")
				if err != nil {
					return nil, err
				}
				if // deleteErr 是批次删除用例错误。
				deleteErr := items.DeleteBatch(ctx, identity.UserID, batchID); deleteErr != nil {
					return nil, deleteErr
				}
				return map[string]any{"deleted": true, "batch_id": batchID}, nil
			},
		},
	)
}
