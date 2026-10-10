// tools.go 实现客服 Agent 的能力工具层。
//
// 本层只暴露只读能力，并且每次调用都严格走两步：
//  1. capability.Evaluate 求值，未放行即拒绝（不产生任何副作用）；
//  2. capability.ResolveAccountScope 断言账号作用域，模型给出的账号参数不被信任。
//
// 工具声明里刻意不暴露 account_id：Agent 的作用域由 Principal 固定为当前账号，
// 让模型有机会填账号只会带来幻觉与被诱导越权的风险。

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	orderapp "xianyu-go/internal/application/orders"

	"xianyu-go/internal/capability"
)

const (
	// defaultOrderPageSize 是 Agent 侧请求订单分页的默认页大小。
	defaultOrderPageSize = 20
	// maxOrderPageSize 是 Agent 侧请求订单分页的页大小上限，避免把整表塞进模型上下文。
	maxOrderPageSize = 50
	// maxToolResultRunes 是单条工具结果回灌模型的最大字符数，超出即截断并标注。
	maxToolResultRunes = 6000
)

// Tools 是基于能力内核的客服 Agent 工具集。
// 它只登记只读能力；写、报价与高危能力不在本期范围内，目录里也没有对客服档位声明。
type Tools struct {
	catalog *capability.Catalog
	orders  capability.OrderPorts
	items   capability.ItemPorts
}

// NewTools 构造工具集。任意依赖为 nil 时对应能力的调用会返回错误而不是静默成功。
func NewTools(catalog *capability.Catalog, orders capability.OrderPorts, items capability.ItemPorts) *Tools {
	return &Tools{catalog: catalog, orders: orders, items: items}
}

// schemas 是 Agent 侧登记的工具声明表，键为能力名。
// 声明只描述模型可填的业务入参；账号作用域由 Principal 决定，因此不在此暴露 account_id。
var schemas = map[string]ToolSchema{
	capability.CapOrderList: {
		Name:        capability.CapOrderList,
		Description: "分页查询当前账号的订单，可按状态与关键词（订单号/商品/买家）过滤。只读，列表不含收货地址详情。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"page":      map[string]any{"type": "integer", "description": "页码，从 1 开始，默认 1。"},
				"page_size": map[string]any{"type": "integer", "description": fmt.Sprintf("每页条数，默认 %d，最大 %d。", defaultOrderPageSize, maxOrderPageSize)},
				"status":    map[string]any{"type": "string", "description": "按订单状态过滤。"},
				"search":    map[string]any{"type": "string", "description": "订单号、商品或买家关键词。"},
			},
		},
	},
	capability.CapOrderGet: {
		Name:        capability.CapOrderGet,
		Description: "读取当前账号下单个订单的详情，含规格、金额、状态与收货信息。只读。",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"order_id": map[string]any{"type": "string", "description": "目标订单标识。"}},
			"required":   []string{"order_id"},
		},
	},
	capability.CapItemList: {
		Name:        capability.CapItemList,
		Description: "列出当前账号的本地货架商品（标题、价格、多规格/多数量标记）。只读。",
	},
	capability.CapItemGet: {
		Name:        capability.CapItemGet,
		Description: "读取当前账号下单个本地商品详情，含描述与扩展详情。只读。",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"item_id": map[string]any{"type": "string", "description": "商品标识。"}},
			"required":   []string{"item_id"},
		},
	},
}

// Schemas 返回当前会话档位下被放行的工具声明。
// 判定与 capability.Evaluate 完全一致：未登记声明、未登记能力、档位不足或能力对客服不可用的一律不下发。
func (t *Tools) Schemas(_ context.Context, session Session) []ToolSchema {
	if t == nil || t.catalog == nil {
		return nil
	}
	// principal 是本次会话对应的调用方身份。
	principal := session.principal()
	// names 是目录中登记的全部能力名，按字典序稳定输出。
	names := t.catalog.Names()
	// result 是被放行的工具声明。
	result := make([]ToolSchema, 0, len(names))
	// name 是当前待求值的能力名。
	for _, name := range names {
		// schema、declared 分别是当前能力的模型侧声明及其是否已登记。
		schema, declared := schemas[name]
		if !declared {
			continue
		}
		if capability.Evaluate(t.catalog, capability.Request{Principal: principal, Capability: name}) != capability.DecisionAllow {
			continue
		}
		result = append(result, schema)
	}
	return result
}

// Call 执行一次工具调用。
// 未登记、未放行、作用域越界、依赖缺失或应用层失败一律返回错误，且不产生写入副作用。
func (t *Tools) Call(ctx context.Context, session Session, call ToolCall) (string, error) {
	if t == nil || t.catalog == nil {
		return "", fmt.Errorf("能力目录未装配")
	}
	// principal 是本次会话对应的调用方身份。
	principal := session.principal()
	// spec、registered 分别是目标能力的声明与其是否已登记。
	spec, registered := t.catalog.Lookup(call.Name)
	if !registered {
		return "", fmt.Errorf("能力未登记：%s", call.Name)
	}
	if capability.Evaluate(t.catalog, capability.Request{Principal: principal, Capability: call.Name}) != capability.DecisionAllow {
		return "", fmt.Errorf("能力 %s 未获得放行", call.Name)
	}
	// scope、scopeErr 分别是断言后的账号作用域与其失败原因。
	// 请求值来自模型填写的入参：非空且不等于会话账号时一律拒绝，避免被诱导越权。
	scope, scopeErr := capability.ResolveAccountScope(principal, spec, stringArg(call.Args, "account_id"))
	if scopeErr != nil {
		return "", scopeErr
	}
	if scope == "" {
		return "", fmt.Errorf("能力 %s 需要账号作用域，但当前身份未提供账号", call.Name)
	}
	return t.dispatch(ctx, session, scope, call.Name, call.Args)
}

// dispatch 按能力名派发到对应的用例端口并序列化结果。
// 这是「能力名 → 用例调用」的唯一映射点，新增能力必须同时在此登记，否则调用被拒。
func (t *Tools) dispatch(ctx context.Context, session Session, scope, name string, args map[string]any) (string, error) {
	switch name {
	case capability.CapOrderList:
		return t.listOrders(ctx, session, scope, args)
	case capability.CapOrderGet:
		return t.getOrder(ctx, session, args)
	case capability.CapItemList:
		return t.listItems(ctx, session, scope)
	case capability.CapItemGet:
		return t.getItem(ctx, scope, args)
	default:
		return "", fmt.Errorf("能力 %s 尚未接入客服 Agent", name)
	}
}

// listOrders 读取会话账号下的订单分页。
// 归属与账号必须来自会话，模型只能提供过滤条件与分页参数。
func (t *Tools) listOrders(ctx context.Context, session Session, scope string, args map[string]any) (string, error) {
	if t.orders == nil {
		return "", fmt.Errorf("订单用例端口未装配")
	}
	// query 是按模型入参归一后的订单查询条件。
	query := orderapp.ListQuery{
		UserID:   session.UserID,
		CookieID: scope,
		Status:   stringArg(args, "status"),
		Search:   stringArg(args, "search"),
		Page:     intArg(args, "page", 1),
		PageSize: clampOrderPageSize(intArg(args, "page_size", defaultOrderPageSize)),
	}
	// result、err 分别是订单分页结果与读取失败原因。
	result, err := t.orders.List(ctx, query)
	if err != nil {
		return "", err
	}
	return marshalToolResult(result)
}

// getOrder 读取会话账号下的单个订单详情。
// 用例签名带 UserID，因此归属校验在应用层；账号仍由作用域断言固定。
func (t *Tools) getOrder(ctx context.Context, session Session, args map[string]any) (string, error) {
	if t.orders == nil {
		return "", fmt.Errorf("订单用例端口未装配")
	}
	// orderID、idErr 分别是模型填写的订单标识及其校验失败原因。
	orderID, idErr := requiredArg(args, "order_id")
	if idErr != nil {
		return "", idErr
	}
	// order、err 分别是订单详情与读取失败原因。
	order, err := t.orders.Get(ctx, session.UserID, orderID)
	if err != nil {
		return "", err
	}
	if order == nil {
		return "", fmt.Errorf("订单 %s 不存在", orderID)
	}
	return marshalToolResult(order)
}

// listItems 读取会话账号下的本地货架商品。
func (t *Tools) listItems(ctx context.Context, session Session, scope string) (string, error) {
	if t.items == nil {
		return "", fmt.Errorf("商品用例端口未装配")
	}
	// items、err 分别是账号下的货架商品与读取失败原因。
	items, err := t.items.ListItems(ctx, session.UserID, scope)
	if err != nil {
		return "", err
	}
	return marshalToolResult(items)
}

// getItem 读取会话账号下的单个本地商品详情。
// 该用例签名只有 cookieID 没有 UserID，因此账号作用域必须由上层断言固定；
// 这里传入的是 ResolveAccountScope 的返回值而不是模型给的 account_id。
func (t *Tools) getItem(ctx context.Context, scope string, args map[string]any) (string, error) {
	if t.items == nil {
		return "", fmt.Errorf("商品用例端口未装配")
	}
	// itemID、idErr 分别是模型填写的商品标识及其校验失败原因。
	itemID, idErr := requiredArg(args, "item_id")
	if idErr != nil {
		return "", idErr
	}
	// item、err 分别是商品详情与读取失败原因。
	item, err := t.items.GetItem(ctx, scope, itemID)
	if err != nil {
		return "", err
	}
	return marshalToolResult(item)
}

// marshalToolResult 把用例结果序列化为回灌模型的文本并做长度收口。
func marshalToolResult(payload any) (string, error) {
	// encoded、err 分别是序列化结果与其失败原因。
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("工具结果序列化失败: %w", err)
	}
	return truncateToolResult(string(encoded)), nil
}

// truncateToolResult 按字符数截断工具结果，避免上下文被单条结果挤爆。
func truncateToolResult(content string) string {
	// runes 是结果的字符序列，用于按字符而非字节截断，避免切坏多字节字符。
	runes := []rune(content)
	if len(runes) <= maxToolResultRunes {
		return content
	}
	return string(runes[:maxToolResultRunes]) + "…（结果已截断）"
}

// clampOrderPageSize 把页大小收敛到 [1, maxOrderPageSize]。
func clampOrderPageSize(size int) int {
	if size < 1 {
		return defaultOrderPageSize
	}
	if size > maxOrderPageSize {
		return maxOrderPageSize
	}
	return size
}

// principal 把会话归一为能力内核的调用方身份：客服 Agent 且作用域锁定当前账号。
func (s Session) principal() capability.Principal {
	return capability.Principal{
		Kind:     capability.PrincipalSupportAgent,
		UserID:   s.UserID,
		CookieID: s.CookieID,
		Preset:   s.Preset,
	}
}

// stringArg 读取字符串入参并去掉首尾空白；缺失或类型不符时返回空串。
func stringArg(args map[string]any, key string) string {
	// raw、ok 分别是入参值与它是否为字符串。
	raw, ok := args[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

// requiredArg 读取必填字符串入参；缺失或空白时返回错误。
func requiredArg(args map[string]any, key string) (string, error) {
	// value 是去空白后的入参值。
	value := stringArg(args, key)
	if value == "" {
		return "", fmt.Errorf("缺少必填入参 %s", key)
	}
	return value, nil
}

// intArg 读取整数入参；缺失、类型不符或非正数时返回 fallback。
func intArg(args map[string]any, key string, fallback int) int {
	// raw 是入参的原始值，JSON 数字解析为 float64，字符串形式由模型给出时也接受。
	switch value := args[key].(type) {
	case float64:
		// rounded 是 JSON 数字归一后的整数部分。
		rounded := int(value)
		if rounded > 0 {
			return rounded
		}
	case string:
		// parsed、err 分别是字符串入参解析出的整数及其失败原因。
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}
