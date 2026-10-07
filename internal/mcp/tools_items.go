// tools_items.go 定义商品货架与批量发布域的工具视图 DTO 与映射辅助：
// 本地商品、平台同步结果、单商品发布结果、批量预检行、批次摘要与明细行。
//
// 工具注册装配见 tools_items_register.go，复杂入参处理器见 tools_items_handlers.go；
// 批量行图片只接受公网 HTTP(S) URL，不接受本机文件路径。

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
