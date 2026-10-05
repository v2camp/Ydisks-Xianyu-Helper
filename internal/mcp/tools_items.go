// tools_items.go 注册商品货架与批量发布域工具：
// 本地商品目录 CRUD 与交付开关、平台商品同步、单商品发布、类目推荐、
// 批量发布预检/创建/启动/管理。所有平台触达动作要求 confirm=true。
//
// 批量行图片只接受公网 HTTP(S) URL，不接受本机文件路径；本地写入前先复核账号归属。

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"strings"

	itemapp "xianyu-go/internal/application/items"
)

// batchIDByteLen 是批量发布批次随机标识的随机字节数（24 位十六进制字符）。
const batchIDByteLen = 12

// itemDTO 是本地商品目录的非敏感视图；ItemDetail 原样保留为 JSON 文本，不含凭证。
type itemDTO struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// AccountID 是归属账号标识。
	AccountID string `json:"account_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Description 是商品描述。
	Description string `json:"description"`
	// Category 是平台类目标识。
	Category string `json:"category"`
	// Price 是商品价格文本。
	Price string `json:"price"`
	// IsMultiSpec 是多规格交付标记。
	IsMultiSpec bool `json:"multi_spec"`
	// MultiQuantityDelivery 是多数量交付标记。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
}

// itemListResult 是本地商品列表返回。
type itemListResult struct {
	// Total 是返回条数。
	Total int `json:"total"`
	// Items 是商品视图列表。
	Items []itemDTO `json:"items"`
}

// syncAllResultDTO 是全量同步结果视图。
type syncAllResultDTO struct {
	// TotalCount 是平台商品总数。
	TotalCount int `json:"total_count"`
	// TotalPages 是读取页数。
	TotalPages int `json:"total_pages"`
	// SavedCount 是写入/更新数。
	SavedCount int `json:"saved_count"`
	// DeletedCount 是软删数。
	DeletedCount int `json:"deleted_count"`
}

// syncPageResultDTO 是分页同步结果视图。
type syncPageResultDTO struct {
	// PageNumber 是页码。
	PageNumber int `json:"page"`
	// PageSize 是页大小。
	PageSize int `json:"page_size"`
	// CurrentCount 是本页商品数。
	CurrentCount int `json:"current_count"`
	// SavedCount 是本页写入数。
	SavedCount int `json:"saved_count"`
}

// publishResultDTO 是单商品发布结果视图，剔除平台原始 RawData 与响应 Cookie 信息。
type publishResultDTO struct {
	// ItemID 是新商品平台标识。
	ItemID string `json:"item_id"`
	// ItemURL 是商品详情地址。
	ItemURL string `json:"item_url"`
	// Title 是平台确认的标题。
	Title string `json:"title"`
	// PriceText 是平台确认的价格文本。
	PriceText string `json:"price_text"`
	// CategoryID 是平台类目标识。
	CategoryID string `json:"category_id"`
	// CategoryName 是平台类目名称。
	CategoryName string `json:"category_name"`
	// ImageURL 是主图地址。
	ImageURL string `json:"image_url"`
	// Quantity 是平台确认库存。
	Quantity int `json:"quantity"`
	// CookieWarning 是非阻断的响应 Cookie 落库警告；为空表示无警告。
	CookieWarning string `json:"cookie_warning,omitempty"`
}

// batchRowDTO 是批量发布预检行视图。
type batchRowDTO struct {
	// RowNo 是表格行号。
	RowNo int `json:"row_no"`
	// AccountID 是该行发布账号。
	AccountID string `json:"account_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是价格文本。
	Price string `json:"price"`
	// Quantity 是库存。
	Quantity int `json:"quantity"`
	// Valid 表示该行是否通过预检。
	Valid bool `json:"valid"`
	// Issues 是该行的预检问题列表。
	Issues []string `json:"issues,omitempty"`
}

// batchPreviewResult 是预检返回。
type batchPreviewResult struct {
	// Total 是总行数。
	Total int `json:"total"`
	// Valid 是通过行数。
	Valid int `json:"valid"`
	// Invalid 是问题行数。
	Invalid int `json:"invalid"`
	// Rows 是逐行预检结果。
	Rows []batchRowDTO `json:"rows"`
}

// batchInfoDTO 是批次摘要视图，剔除租约令牌、上传目录等内部字段。
type batchInfoDTO struct {
	// ID 是批次标识。
	ID string `json:"batch_id"`
	// Status 是批次状态。
	Status string `json:"status"`
	// DefaultAccountID 是默认发布账号。
	DefaultAccountID string `json:"default_account_id"`
	// Filename 是来源文件名。
	Filename string `json:"filename"`
	// TotalCount 是明细总数。
	TotalCount int `json:"total_count"`
	// SuccessCount 是成功数。
	SuccessCount int `json:"success_count"`
	// FailedCount 是失败数。
	FailedCount int `json:"failed_count"`
	// CreatedAt 是创建时间文本。
	CreatedAt string `json:"created_at"`
}

// batchDetailsResult 是批次详情返回。
type batchDetailsResult struct {
	// Batch 是批次摘要。
	Batch batchInfoDTO `json:"batch"`
	// Rows 是逐行状态。
	Rows []batchDetailRowDTO `json:"rows"`
}

// batchDetailRowDTO 是批次明细行状态视图。
type batchDetailRowDTO struct {
	// RowNo 是行号。
	RowNo int `json:"row_no"`
	// AccountID 是发布账号。
	AccountID string `json:"account_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Status 是该行发布状态。
	Status string `json:"status"`
	// ItemID 是发布成功后的商品标识。
	ItemID string `json:"item_id,omitempty"`
	// ItemURL 是发布成功后的商品地址。
	ItemURL string `json:"item_url,omitempty"`
	// FailureKind 是失败分类。
	FailureKind string `json:"failure_kind,omitempty"`
	// ErrorMessage 是非敏感失败原因。
	ErrorMessage string `json:"error_message,omitempty"`
}

// RegisterItemTools 注册商品域全部工具；accounts 提供账号归属复核，items 提供商品用例。
func (e *Endpoint) RegisterItemTools(accounts AccountPorts, items ItemPorts) {
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

// itemBoolFlagTool 构造商品多规格/多数量标记的本地写入工具。
func itemBoolFlagTool(name, description, flagName string, items ItemPorts,
	requireAccount func(context.Context, *CallIdentity, string) error) ToolDef {
	return ToolDef{
		Name: name, Description: description,
		Args: []ArgSpec{
			{Name: "account_id", Type: ArgString, Required: true, Description: "归属账号标识。"},
			{Name: "item_id", Type: ArgString, Required: true, Description: "平台商品标识。"},
			{Name: flagName, Type: ArgBoolean, Required: true, Description: "标记目标状态。"},
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
			// enabled、err 是标记目标状态及其校验错误。
			enabled, err := args.Bool(flagName)
			if err != nil {
				return nil, err
			}
			if // ownershipErr 是账号归属复核错误。
			ownershipErr := requireAccount(ctx, identity, cookieID); ownershipErr != nil {
				return nil, ownershipErr
			}
			// applyErr 是标记写入错误。
			var applyErr error
			if flagName == "multi_spec" {
				applyErr = items.SetItemMultiSpec(ctx, cookieID, itemID, enabled)
			} else {
				applyErr = items.SetItemMultiQuantity(ctx, cookieID, itemID, enabled)
			}
			if applyErr != nil {
				return nil, applyErr
			}
			return map[string]any{"updated": true, "account_id": cookieID, "item_id": itemID, flagName: enabled}, nil
		},
	}
}

// applyStringPatch 在入参提供字符串键时填充可选 patch 字段。
func applyStringPatch(args Arguments, key string, target **string) {
	// value、ok 是入参值及其存在性。
	value, ok := args[key]
	if !ok {
		return
	}
	// text 是字符串值。
	if text, ok := value.(string); ok {
		*target = &text
	}
}

// applyBoolPatch 在入参提供布尔键时填充可选 patch 字段。
func applyBoolPatch(args Arguments, key string, target **bool) {
	// value、ok 是入参值及其存在性。
	value, ok := args[key]
	if !ok {
		return
	}
	// flag 是布尔值。
	if flag, ok := value.(bool); ok {
		*target = &flag
	}
}

// newBatchID 生成与 Web 端一致形态的批次标识（batch_ 前缀加 24 位随机十六进制）。
func newBatchID() (string, error) {
	// raw 是随机字节。
	raw := make([]byte, batchIDByteLen)
	if // randErr 是随机数读取错误。
	_, randErr := rand.Read(raw); randErr != nil {
		return "", Fail(ClassInternal, "生成批次标识失败", randErr)
	}
	return "batch_" + hex.EncodeToString(raw), nil
}

// itemDTOsFromApp 批量映射本地商品 DTO。
func itemDTOsFromApp(cookieID string, rows []itemapp.CatalogItem) []itemDTO {
	// items 是商品视图切片。
	items := make([]itemDTO, 0, len(rows))
	// row 是当前本地商品记录。
	for _, row := range rows {
		// dto 是单商品视图，账号取查询入参，避免依赖记录内可能不一致的归属。
		dto := itemDTOFromApp(row)
		dto.AccountID = cookieID
		items = append(items, dto)
	}
	return items
}

// itemDTOFromApp 映射单个商品记录（不含账号覆盖）。
func itemDTOFromApp(row itemapp.CatalogItem) itemDTO {
	return itemDTO{
		ItemID: row.ItemID, AccountID: row.CookieID, Title: row.ItemTitle,
		Description: row.ItemDescription, Category: row.ItemCategory, Price: row.ItemPrice,
		IsMultiSpec: row.IsMultiSpec, MultiQuantityDelivery: row.MultiQuantityDelivery,
	}
}

// batchInfoFromApp 映射批次摘要，剔除内部字段。
func batchInfoFromApp(row itemapp.BatchInfo) batchInfoDTO {
	return batchInfoDTO{
		ID: row.ID, Status: row.Status, DefaultAccountID: row.DefaultCookieID,
		Filename: row.Filename, TotalCount: row.TotalCount, SuccessCount: row.SuccessCount,
		FailedCount: row.FailedCount, CreatedAt: row.CreatedAt,
	}
}

// batchDetailsFromApp 映射批次详情与逐行状态。
func batchDetailsFromApp(details itemapp.BatchDetails) batchDetailsResult {
	// rows 是明细行视图列表。
	rows := make([]batchDetailRowDTO, 0, len(details.Rows))
	// row 是当前批次明细行。
	for _, row := range details.Rows {
		rows = append(rows, batchDetailRowDTO{
			RowNo: row.RowNo, AccountID: row.CookieID, Title: row.Title, Status: row.Status,
			ItemID: row.ItemID, ItemURL: row.ItemURL, FailureKind: row.FailureKind,
			ErrorMessage: row.ErrorMessage,
		})
	}
	return batchDetailsResult{Batch: batchInfoFromApp(details.Batch), Rows: rows}
}

// batchResultCSV 把批次逐行结果序列化为 CSV 文本，字段顺序固定且含 BOM 便于表格软件识别中文。
func batchResultCSV(details itemapp.BatchDetails) (string, error) {
	// builder 是 CSV 内容缓冲。
	var builder strings.Builder
	// writer 是 CSV 写入器。
	writer := csv.NewWriter(&builder)
	if // headerErr 是表头写入错误。
	headerErr := writer.Write([]string{"row_no", "account_id", "title", "status", "item_id", "item_url", "failure_kind", "error_message"}); headerErr != nil {
		return "", Fail(ClassInternal, "生成 CSV 表头失败", headerErr)
	}
	// row 是当前批次明细行。
	for _, row := range details.Rows {
		// record 是当前行的 CSV 记录。
		record := []string{
			fmt.Sprintf("%d", row.RowNo), row.CookieID, row.Title, row.Status,
			row.ItemID, row.ItemURL, row.FailureKind, row.ErrorMessage,
		}
		if // writeErr 是 CSV 行写入错误。
		writeErr := writer.Write(record); writeErr != nil {
			return "", Fail(ClassInternal, "生成 CSV 行失败", writeErr)
		}
	}
	writer.Flush()
	if // flushErr 是 CSV 缓冲刷新错误。
	flushErr := writer.Error(); flushErr != nil {
		return "", Fail(ClassInternal, "刷新 CSV 缓冲失败", flushErr)
	}
	return builder.String(), nil
}
