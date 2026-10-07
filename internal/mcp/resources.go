// resources.go 注册 MCP 只读资源（FR-18）：health、dashboard、accounts、accounts/{account_id}、
// orders、automation/issues、cards/low-stock。
//
// 固定地址资源注册为 resources，带参数资源注册为 resource templates（MCP 标准做法）；
// 因此 resources/list 返回 4 个固定资源，resources/templates/list 返回 3 个参数化资源。
//
// 安全与语义红线：资源内容复用同域工具的非敏感 DTO 与脱敏规则，绝不返回凭证、
// 明文卡密或 API 密钥；每次 resources/read 都会写入审计（类别 resource）。

package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	mcpproto "github.com/mark3labs/mcp-go/mcp"

	accountapp "xianyu-go/internal/application/account"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	orderapp "xianyu-go/internal/application/orders"
)

const (
	// resourceURIScheme 是全部只读资源的固定 URI 前缀。
	resourceURIScheme = "ydisks://"
	// resourceURIHealth 是服务健康与版本资源 URI。
	resourceURIHealth = resourceURIScheme + "health"
	// resourceURIDashboard 是全局计数概览资源 URI。
	resourceURIDashboard = resourceURIScheme + "dashboard"
	// resourceURIAccounts 是账号列表资源 URI。
	resourceURIAccounts = resourceURIScheme + "accounts"
	// resourceURIAutomationIssues 是自动化异常资源 URI。
	resourceURIAutomationIssues = resourceURIScheme + "automation/issues"
	// resourceURIPrefixAccounts 是单账号资源 URI 前缀。
	resourceURIPrefixAccounts = resourceURIScheme + "accounts/"
	// resourceURIOrders 是订单资源的基础 URI。
	resourceURIOrders = resourceURIScheme + "orders"
	// resourceURICardsLowStock 是卡密低库存资源的基础 URI。
	resourceURICardsLowStock = resourceURIScheme + "cards/low-stock"
	// resourceTemplateAccount 是单账号资源模板。
	resourceTemplateAccount = resourceURIPrefixAccounts + "{account_id}"
	// resourceTemplateOrders 是带查询参数的订单资源模板。
	resourceTemplateOrders = resourceURIOrders + "{?status,account_id,search,page,page_size}"
	// resourceTemplateCardsLowStock 是带阈值参数的卡密低库存资源模板。
	resourceTemplateCardsLowStock = resourceURICardsLowStock + "{?threshold}"
	// resourceMIMETypeJSON 是全部资源内容使用的媒体类型。
	resourceMIMETypeJSON = "application/json"
	// defaultLowStockThreshold 是低库存资源的默认阈值（非空卡密行数）。
	defaultLowStockThreshold = 5
	// maxLowStockThreshold 是低库存阈值上限，避免把全部卡券误判为低库存。
	maxLowStockThreshold = 1000
)

// ResourcePorts 聚合只读资源所需的同域查询用例。
// 这些读取与对应工具使用同一应用服务，保证资源内容与工具 DTO 一致且同样脱敏。
type ResourcePorts interface {
	// DashboardStats 返回全局计数概览。
	DashboardStats(ctx context.Context, userID int64) (analyticsapp.DashboardStats, error)
	// ListAccountSummaries 返回当前管理员的账号摘要列表。
	ListAccountSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error)
	// GetAccountSummary 返回单个归属账号摘要。
	GetAccountSummary(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error)
	// ListOrders 按条件分页查询订单。
	ListOrders(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error)
	// ListAutomationIssues 返回待人工处理的运行异常与死信任务。
	ListAutomationIssues(ctx context.Context, userID int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error)
	// ListCards 返回当前管理员的卡券组列表（含库存正文，按需裁剪）。
	ListCards(ctx context.Context, userID int64) ([]cardsapp.Card, error)
}

// healthResourceDTO 是服务健康资源内容。
type healthResourceDTO struct {
	// Status 固定为 ok，表示 MCP 服务可响应只读请求。
	Status string `json:"status"`
	// Server 是服务实现名。
	Server string `json:"server"`
	// Version 是应用版本。
	Version string `json:"version"`
	// Enabled 表示 MCP 服务当前是否启用。
	Enabled bool `json:"enabled"`
	// URI 是本资源地址。
	URI string `json:"uri"`
}

// dashboardResourceDTO 是全局计数概览资源内容，键名与 analytics_dashboard 工具保持一致。
type dashboardResourceDTO struct {
	// TotalAccounts 是账号总数。
	TotalAccounts int64 `json:"total_accounts"`
	// ActiveAccounts 是启用账号数。
	ActiveAccounts int64 `json:"active_accounts"`
	// TotalCards 是卡券组总数。
	TotalCards int64 `json:"total_cards"`
	// AvailableCardStock 是可用卡密库存数。
	AvailableCardStock int64 `json:"available_card_stock"`
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64 `json:"total_keywords"`
	// TotalOrders 是订单总数。
	TotalOrders int64 `json:"total_orders"`
}

// accountResourceListDTO 是账号列表资源内容。
type accountResourceListDTO struct {
	// Total 是账号数量。
	Total int `json:"total"`
	// Accounts 是账号摘要列表。
	Accounts []accountDTO `json:"accounts"`
}

// lowStockCardsDTO 是卡密低库存资源内容。
type lowStockCardsDTO struct {
	// Threshold 是本次使用的低库存阈值。
	Threshold int `json:"threshold"`
	// Total 是命中阈值的卡券组数量。
	Total int `json:"total"`
	// Cards 是命中阈值的卡券组非敏感视图。
	Cards []cardDTO `json:"cards"`
	// Note 提示库存计数口径。
	Note string `json:"note"`
}

// resourceLoader 是资源内容加载器：返回可 JSON 序列化的非敏感 DTO。
type resourceLoader func(ctx context.Context, args map[string]any) (any, error)

// RegisterResources 注册全部只读资源；端口为 nil 时直接返回。
func (e *Endpoint) RegisterResources(p ResourcePorts) {
	if e == nil || p == nil {
		return
	}
	e.registerStaticResources(p)
	e.registerResourceTemplates(p)
}

// registerStaticResources 注册固定地址只读资源。
func (e *Endpoint) registerStaticResources(p ResourcePorts) {
	e.addResource(mcpproto.NewResource(resourceURIHealth, "health",
		mcpproto.WithResourceDescription("MCP 服务健康与版本信息（含启用状态）。"),
		mcpproto.WithMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, _ map[string]any) (any, error) {
		// enabled 是 MCP 服务启用状态；配置端口缺失时按已启用处理。
		enabled := true
		if e.config != nil {
			// value、err 是启用状态读取结果与错误。
			value, err := e.config.Enabled(ctx)
			if err != nil {
				return nil, err
			}
			enabled = value
		}
		return healthResourceDTO{
			Status: "ok", Server: defaultServerName, Version: e.systemVersion,
			Enabled: enabled, URI: resourceURIHealth,
		}, nil
	})
	e.addResource(mcpproto.NewResource(resourceURIDashboard, "dashboard",
		mcpproto.WithResourceDescription("账号、卡密库存、关键词与订单的全局非敏感计数概览。"),
		mcpproto.WithMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, _ map[string]any) (any, error) {
		// stats、statErr 是仪表盘计数。
		stats, statErr := p.DashboardStats(ctx, resourceUserID(ctx))
		if statErr != nil {
			return nil, statErr
		}
		return dashboardResourceDTO{
			TotalAccounts: stats.TotalCookies, ActiveAccounts: stats.ActiveCookies,
			TotalCards: stats.TotalCards, AvailableCardStock: stats.AvailableCardStock,
			TotalKeywords: stats.TotalKeywords, TotalOrders: stats.TotalOrders,
		}, nil
	})
	e.addResource(mcpproto.NewResource(resourceURIAccounts, "accounts",
		mcpproto.WithResourceDescription("当前管理员的账号非敏感摘要列表（不含 Cookie 与密码）。"),
		mcpproto.WithMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, _ map[string]any) (any, error) {
		// rows、listErr 是账号摘要列表。
		rows, listErr := p.ListAccountSummaries(ctx, resourceUserID(ctx))
		if listErr != nil {
			return nil, listErr
		}
		// accounts 是账号视图列表。
		accounts := make([]accountDTO, 0, len(rows))
		// row 是当前待映射的账号摘要。
		for _, row := range rows {
			accounts = append(accounts, accountFromSummary(row))
		}
		return accountResourceListDTO{Total: len(accounts), Accounts: accounts}, nil
	})
	e.addResource(mcpproto.NewResource(resourceURIAutomationIssues, "automation-issues",
		mcpproto.WithResourceDescription("待人工处理的自动化运行异常与死信任务非敏感摘要。"),
		mcpproto.WithMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, _ map[string]any) (any, error) {
		// runs、deferred、listErr 是运行异常与死信任务。
		runs, deferred, listErr := p.ListAutomationIssues(ctx, resourceUserID(ctx))
		if listErr != nil {
			return nil, listErr
		}
		return issueListResult{
			Runs: issueRunsFromApp(runs), Deferred: issueDeferredFromApp(deferred),
		}, nil
	})
}

// registerResourceTemplates 注册参数化只读资源模板。
func (e *Endpoint) registerResourceTemplates(p ResourcePorts) {
	e.addResourceTemplate(mcpproto.NewResourceTemplate(resourceTemplateAccount, "account",
		mcpproto.WithTemplateDescription("按账号标识读取单个账号的非敏感摘要。"),
		mcpproto.WithTemplateMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, args map[string]any) (any, error) {
		// accountID 是模板匹配得到的账号标识。
		accountID := strings.TrimSpace(resourceArg(args, "account_id"))
		if accountID == "" {
			return nil, InvalidArgument("资源 URI 缺少账号标识")
		}
		// summary、getErr 是账号摘要。
		summary, getErr := p.GetAccountSummary(ctx, resourceUserID(ctx), accountID)
		if getErr != nil {
			return nil, getErr
		}
		return accountFromSummary(summary), nil
	})
	e.addResourceTemplate(mcpproto.NewResourceTemplate(resourceTemplateOrders, "orders",
		mcpproto.WithTemplateDescription("订单分页列表；支持 status、account_id、search、page、page_size 查询参数。"),
		mcpproto.WithTemplateMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, args map[string]any) (any, error) {
		// query 是模板参数归一后的订单过滤条件。
		query := orderQueryFromResourceArgs(args, resourceUserID(ctx))
		// result、listErr 是订单分页结果。
		result, listErr := p.ListOrders(ctx, query)
		if listErr != nil {
			return nil, listErr
		}
		return orderListResult{
			Total: result.Total, Page: result.Page, PageSize: result.PageSize,
			TotalPages: result.TotalPages, Orders: orderRowsFromApp(result.Rows),
		}, nil
	})
	e.addResourceTemplate(mcpproto.NewResourceTemplate(resourceTemplateCardsLowStock, "cards-low-stock",
		mcpproto.WithTemplateDescription("库存行数低于阈值的 data 卡券组；支持 threshold 参数（默认 5）。"),
		mcpproto.WithTemplateMIMEType(resourceMIMETypeJSON)), func(ctx context.Context, args map[string]any) (any, error) {
		// threshold 是模板参数归一后的低库存阈值。
		threshold := clampLimit(positiveIntParam(resourceArg(args, "threshold"), defaultLowStockThreshold),
			defaultLowStockThreshold, maxLowStockThreshold)
		// rows、listErr 是全部卡券组。
		rows, listErr := p.ListCards(ctx, resourceUserID(ctx))
		if listErr != nil {
			return nil, listErr
		}
		// cards 是命中阈值的卡券组视图。
		cards := make([]cardDTO, 0, len(rows))
		// row 是当前待判定库存的卡券组。
		for _, row := range rows {
			if row.Type != "data" {
				continue
			}
			if countDataLines(row.DataContent) <= threshold {
				cards = append(cards, cardDTOFromApp(row))
			}
		}
		return lowStockCardsDTO{
			Threshold: threshold, Total: len(cards), Cards: cards,
			Note: "库存行数按非空卡密行统计；仅返回 data 类型卡券组，不含明文卡密。",
		}, nil
	})
}

// resourceUserID 从阅读上下文提取固定管理员身份；缺失时返回零值，由应用服务拒绝。
func resourceUserID(ctx context.Context) int64 {
	// identity 是鉴权中间件注入的调用身份。
	if identity := IdentityFromContext(ctx); identity != nil {
		return identity.UserID
	}
	return 0
}

// resourceArg 读取模板匹配得到的字符串参数。
// mcp-go 的 URI 模板匹配以字符串切片返回变量值，这里取首个元素并兼容裸字符串形式。
func resourceArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	// raw 是模板匹配得到的原始参数值。
	raw, ok := args[name]
	if !ok {
		return ""
	}
	// value 是按类型分支断言后的参数值。
	switch value := raw.(type) {
	case string:
		return value
	case []string:
		if len(value) == 0 {
			return ""
		}
		return value[0]
	default:
		return ""
	}
}

// orderQueryFromResourceArgs 把资源模板参数归一为订单过滤条件。
func orderQueryFromResourceArgs(args map[string]any, userID int64) orderapp.ListQuery {
	// page、pageSize 是归一后的分页参数，与订单工具保持一致的上限。
	page := positiveIntParam(resourceArg(args, "page"), 1)
	// pageSize 是归一后的单页条数，超过上限时按上限收敛。
	pageSize := clampLimit(positiveIntParam(resourceArg(args, "page_size"), defaultPageSize), defaultPageSize, maxPageSize)
	return orderapp.ListQuery{
		UserID: userID, CookieID: strings.TrimSpace(resourceArg(args, "account_id")),
		Status: strings.TrimSpace(resourceArg(args, "status")), Search: strings.TrimSpace(resourceArg(args, "search")),
		Page: page, PageSize: pageSize,
	}
}

// positiveIntParam 解析正整数字符串参数；非正或非法时回落默认值。
func positiveIntParam(raw string, fallback int) int {
	// parsed、err 是解析结果与错误。
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// addResource 注册一个固定地址只读资源及其处理器。
func (e *Endpoint) addResource(resource mcpproto.Resource, loader resourceLoader) {
	e.mcpServer.AddResource(resource, func(ctx context.Context, request mcpproto.ReadResourceRequest) ([]mcpproto.ResourceContents, error) {
		return e.serveResource(ctx, request.Params.URI, request.Params.Arguments, loader)
	})
}

// addResourceTemplate 注册一个参数化只读资源模板及其处理器。
func (e *Endpoint) addResourceTemplate(template mcpproto.ResourceTemplate, loader resourceLoader) {
	e.mcpServer.AddResourceTemplate(template, func(ctx context.Context, request mcpproto.ReadResourceRequest) ([]mcpproto.ResourceContents, error) {
		return e.serveResource(ctx, request.Params.URI, request.Params.Arguments, loader)
	})
}

// serveResource 执行资源读取：调用加载器、写审计并把结果序列化为 JSON 文本内容。
func (e *Endpoint) serveResource(ctx context.Context, uri string, args map[string]any, loader resourceLoader) ([]mcpproto.ResourceContents, error) {
	// payload、loadErr 是资源内容与读取错误。
	payload, loadErr := loader(ctx, args)
	if loadErr != nil {
		e.writeCallAudit(ctx, AuditCategoryResource, uri, "", false, errorClassOf(loadErr))
		return nil, loadErr
	}
	// encoded 是资源内容的稳定 JSON 文本。
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		e.writeCallAudit(ctx, AuditCategoryResource, uri, "", false, string(ClassInternal))
		return nil, Fail(ClassInternal, "构造资源内容失败", marshalErr)
	}
	e.writeCallAudit(ctx, AuditCategoryResource, uri, "", true, "")
	return []mcpproto.ResourceContents{mcpproto.TextResourceContents{
		URI: uri, MIMEType: resourceMIMETypeJSON, Text: string(encoded),
	}}, nil
}

// writeCallAudit 尽力写入资源或提示调用审计；失败只记日志，不影响只读结果。
func (e *Endpoint) writeCallAudit(ctx context.Context, category, name, cookieID string, success bool, errorClass string) {
	// identity 是调用身份；缺失时不写审计。
	identity := IdentityFromContext(ctx)
	if e.audit == nil || identity == nil {
		return
	}
	// auditErr 是审计写入错误，只记录非敏感日志。
	if auditErr := e.audit.AddAudit(ctx, AuditEntry{
		UserID: identity.UserID, Source: identity.Source, Category: category, Name: name,
		CookieID: cookieID, Success: success, ErrorClass: errorClass,
	}); auditErr != nil {
		slog.Default().Warn("写入 MCP 资源或提示审计失败", "category", category, "name", name, "err", auditErr)
	}
}

// errorClassOf 归一读取错误为稳定类别，仅用于审计记录。
func errorClassOf(err error) string {
	// class 是归一后的稳定错误类别。
	class, _ := Classify(err)
	return string(class)
}
