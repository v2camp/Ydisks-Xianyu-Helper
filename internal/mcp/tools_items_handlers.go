// tools_items_handlers.go 承载商品域中入参映射较复杂的处理器构造：
// 单商品发布（图片 URL）、批量预检（JSON 行）与批次创建（预检后持久化）。

package mcp

import (
	"context"
	"strings"

	itemapp "xianyu-go/internal/application/items"

	"xianyu-go/internal/capability"
)

// makePublishHandler 构造单商品发布处理器。
func (e *Endpoint) makePublishHandler(items capability.ItemPorts,
	requireAccount func(context.Context, *CallIdentity, string) error) HandlerFunc {
	return func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
		// cookieID 是发布账号标识。
		cookieID, err := args.String("account_id")
		if err != nil {
			return nil, err
		}
		// title 是必填标题。
		title, err := args.String("title")
		if err != nil {
			return nil, err
		}
		// priceCents 是必填售价（分）。
		priceCents, err := args.Int("price_cents")
		if err != nil {
			return nil, err
		}
		if priceCents < 0 {
			return nil, InvalidArgument("price_cents 不能为负数")
		}
		if // ownershipErr 是账号归属复核错误。
		ownershipErr := requireAccount(ctx, identity, cookieID); ownershipErr != nil {
			return nil, ownershipErr
		}
		// imageURLs 是公网图片 URL 列表；图片为可选项，缺省按无图发布。
		imageURLs := args.OptionalStringArray("image_urls")
		// postageMode 是邮费模式，默认包邮。
		postageMode := args.OptionalString("postage_mode", "free")
		if postageMode != "free" && postageMode != "fixed" {
			return nil, InvalidArgument("postage_mode 只能是 free 或 fixed")
		}
		// input 是组合层单发布端口入参。
		input := capability.SinglePublishInput{
			UserID:             identity.UserID,
			CookieID:           cookieID,
			Title:              title,
			Description:        args.OptionalString("description", ""),
			PriceCents:         int64(priceCents),
			OriginalPriceCents: int64(args.OptionalInt("original_price_cents", 0)),
			Quantity:           args.OptionalInt("quantity", 1),
			PostageMode:        postageMode,
			PostageCents:       int64(args.OptionalInt("postage_cents", 0)),
			ImageURLs:          imageURLs,
			CatID:              args.OptionalString("category_cat_id", ""),
		}
		if input.Quantity <= 0 {
			return nil, InvalidArgument("quantity 必须大于 0")
		}
		// outcome、publishErr 是平台发布结果。
		outcome, publishErr := items.PublishSingle(ctx, input)
		if publishErr != nil {
			return nil, publishErr
		}
		if outcome.Result == nil {
			// 平台未返回结果时按不确定处理，提示人工核对，禁止自动重试。
			return nil, Fail(ClassUncertain, "平台未返回明确的发布结果，请在 Web 端核对商品是否已上架", nil)
		}
		return publishResultDTO{
			ItemID: outcome.Result.ItemID, ItemURL: outcome.Result.ItemURL,
			Title: outcome.Result.Title, PriceText: outcome.Result.PriceText,
			CategoryID: outcome.Result.CategoryID, CategoryName: outcome.Result.CategoryName,
			ImageURL: outcome.Result.ImageURL, Quantity: outcome.Result.Quantity,
			CookieWarning: warningFromError(outcome.ResponseCookieErr),
		}, nil
	}
}

// makePreviewHandler 构造批量发布预检处理器：JSON 行 → 应用层归一化字段行 → 预检结果。
func (e *Endpoint) makePreviewHandler(items capability.ItemPorts) HandlerFunc {
	return func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
		// rows、rowErr 是批量行数组及其校验错误。
		rows, rowErr := batchRowsArg(args)
		if rowErr != nil {
			return nil, rowErr
		}
		// fieldsRows 是应用层预检接受的归一化字段映射列表。
		fieldsRows, mapErr := batchFieldsRows(rows)
		if mapErr != nil {
			return nil, mapErr
		}
		// previewRows、previewErr 是逐行预检结果。
		previewRows, previewErr := items.PreviewBatch(ctx, itemapp.BatchPreviewInput{
			UserID:          identity.UserID,
			DefaultCookieID: args.OptionalString("default_account_id", ""),
			Rows:            fieldsRows,
		})
		if previewErr != nil {
			return nil, previewErr
		}
		return previewResultFromApp(previewRows), nil
	}
}

// makeBatchCreateHandler 构造预检后持久化批次的处理器。
func (e *Endpoint) makeBatchCreateHandler(items capability.ItemPorts) HandlerFunc {
	return func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
		// rows、rowErr 是批量行数组。
		rows, rowErr := batchRowsArg(args)
		if rowErr != nil {
			return nil, rowErr
		}
		// fieldsRows、mapErr 是归一化字段行。
		fieldsRows, mapErr := batchFieldsRows(rows)
		if mapErr != nil {
			return nil, mapErr
		}
		// previewRows、previewErr 是预检结果；有错误行时仍可持久化（保留逐行错误），由应用层决定可发布行。
		previewRows, previewErr := items.PreviewBatch(ctx, itemapp.BatchPreviewInput{
			UserID:          identity.UserID,
			DefaultCookieID: args.OptionalString("default_account_id", ""),
			Rows:            fieldsRows,
		})
		if previewErr != nil {
			return nil, previewErr
		}
		// batchID、idErr 是新批次标识。
		batchID, idErr := newBatchID()
		if idErr != nil {
			return nil, idErr
		}
		// filename 是批次来源标记。
		filename := args.OptionalString("filename", "mcp.json")
		// persisted、persistErr 是持久化结果。
		persisted, persistErr := items.PersistBatch(ctx, itemapp.BatchPreviewPersistenceBatch{
			ID: batchID, UserID: identity.UserID,
			DefaultCookieID:        args.OptionalString("default_account_id", ""),
			Filename:               filename,
			PublishIntervalSeconds: args.OptionalInt("publish_interval_seconds", 0),
		}, previewRows)
		if persistErr != nil {
			return nil, persistErr
		}
		return map[string]any{
			"batch_id": persisted.PreviewID, "total": persisted.Total,
			"valid": persisted.Valid, "invalid": persisted.Invalid,
			"hint": "批次已创建，调用 item_batch_start 并传 confirm=true 才会开始发布",
		}, nil
	}
}

// batchRowsArg 读取并校验 rows 入参必须是非空对象数组。
func batchRowsArg(args Arguments) ([]map[string]any, error) {
	// raw、ok 是原始数组值。
	raw, ok := args["rows"]
	if !ok {
		return nil, InvalidArgument("缺少必填参数 rows")
	}
	// array 是通用数组。
	array, ok := raw.([]any)
	if !ok {
		return nil, InvalidArgument("rows 必须是对象数组")
	}
	if len(array) == 0 {
		return nil, InvalidArgument("rows 至少包含一行")
	}
	// rows 是类型化后的行列表。
	rows := make([]map[string]any, 0, len(array))
	// element 是当前行。
	for _, element := range array {
		// row、ok 是该行对象及其类型断言结果。
		row, ok := element.(map[string]any)
		if !ok {
			return nil, InvalidArgument("rows 的每个元素必须是对象")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// batchFieldsRows 把 MCP JSON 行映射为预检应用层识别的归一化字段名。
func batchFieldsRows(rows []map[string]any) ([]map[string]any, error) {
	// mapped 是输出的归一化字段行。
	mapped := make([]map[string]any, 0, len(rows))
	// row 是当前 MCP 输入行对象。
	for _, row := range rows {
		// fields 是该行的归一化字段。
		fields := make(map[string]any, len(row)+1)
		copyStringField(row, fields, "account_id", "cookie_id")
		copyStringField(row, fields, "title", "title")
		copyStringField(row, fields, "description", "description")
		copyStringField(row, fields, "price", "price")
		copyStringField(row, fields, "original_price", "original_price")
		copyStringField(row, fields, "postage_mode", "postage_mode")
		copyStringField(row, fields, "postage", "postage")
		copyStringField(row, fields, "category_cat_id", "category_id")
		copyStringField(row, fields, "category_cat_name", "category_name")
		copyStringField(row, fields, "channel_category_id", "channel_category_id")
		copyStringField(row, fields, "tb_category_id", "tb_category_id")
		// quantity 直接透传，预检的数字归一化接受数值类型。
		if value, ok := row["quantity"]; ok {
			fields["quantity"] = value
		}
		// imageURLs 是图片 URL 数组；预检按换行分隔文本切分。
		// value、ok 是图片 URL 入参及其存在性。
		if value, ok := row["image_urls"]; ok {
			// urls、listOK 是图片数组及其类型断言结果。
			if urls, listOK := value.([]any); listOK {
				// texts 收集字符串 URL。
				texts := make([]string, 0, len(urls))
				// element 是当前图片元素。
				for _, element := range urls {
					// text、textOK 是元素字符串值及其断言结果。
					if text, textOK := element.(string); textOK && strings.TrimSpace(text) != "" {
						texts = append(texts, strings.TrimSpace(text))
					}
				}
				fields["images"] = strings.Join(texts, "\n")
			}
		}
		mapped = append(mapped, fields)
	}
	return mapped, nil
}

// copyStringField 把 MCP 行中的字符串字段复制到预检字段映射。
func copyStringField(source, target map[string]any, sourceKey, targetKey string) {
	// value、ok 是源字段及其存在性。
	value, ok := source[sourceKey]
	if !ok {
		return
	}
	// text 是字符串值；非字符串字段忽略并交由预检报告问题。
	if text, ok := value.(string); ok {
		target[targetKey] = text
	}
}

// previewResultFromApp 映射预检结果 DTO。
func previewResultFromApp(rows []itemapp.BatchPreviewRow) batchPreviewResult {
	// validCount 统计通过预检的行数。
	validCount := 0
	// dtos 是逐行视图。
	dtos := make([]batchRowDTO, 0, len(rows))
	// row 是当前预检行结果。
	for _, row := range rows {
		// valid 表示该行无预检问题。
		valid := len(row.Errors) == 0
		if valid {
			validCount++
		}
		dtos = append(dtos, batchRowDTO{
			RowNo: row.RowNo, AccountID: row.CookieID, Title: row.Title,
			Price: row.Price, Quantity: row.Quantity, Valid: valid, Issues: row.Errors,
		})
	}
	return batchPreviewResult{Total: len(rows), Valid: validCount, Invalid: len(rows) - validCount, Rows: dtos}
}
