// plugin_reply.go 插件层回复（优先级2.5，位于关键词之后、AI 之前）：
// 识别找书/找短剧意图后调用 find_stuff 的 MCP streamable HTTP 服务检索资源，只生成回复文本。
// 插件只查不发：发送、安全闸门与幂等仍由 ReplyService.Handle 统一负责；
// find_stuff 不可达或超时按降级处理（返回 nil 并向上报错），不阻塞后续 AI/默认回复链路。

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// findStuffMCPURLEnv 是 find_stuff MCP 服务地址的环境变量名。
	findStuffMCPURLEnv = "FIND_STUFF_MCP_URL"
	// defaultFindStuffMCPURL 是 compose 内网 find-stuff 服务的 MCP streamable HTTP 端点默认地址。
	defaultFindStuffMCPURL = "http://find-stuff:59190/mcp"
	// pluginSearchTimeout 是插件层单次检索（含首次懒连接握手）的时间预算，超时即降级。
	pluginSearchTimeout = 8 * time.Second
	// pluginMaxCandidates 是 multi 状态最多展示的候选条数，防止回复文本过长被平台截断。
	pluginMaxCandidates = 5
	// pluginErrorSummaryRunes 是工具错误摘要的截断长度，避免异常长错误写满日志。
	pluginErrorSummaryRunes = 200
)

// pluginBookKeywords 是找书意图触发词；命中任意一个即按 book 领域检索。
var pluginBookKeywords = []string{"书", "作者", "读物"}

// pluginDramaKeywords 是找短剧意图触发词；命中任意一个即按 drama 领域检索，判定优先于找书词。
var pluginDramaKeywords = []string{"短剧", "电视剧", "主演", "剧名", "追剧", "剧"}

// resolveFindStuffMCPURL 解析插件层 MCP 地址：环境变量未设置时回落 compose 内网默认地址；
// 显式设置但为空字符串表示关闭插件层（NewFindStuffReplier 对空地址返回 nil）。
func resolveFindStuffMCPURL(lookup func(string) (string, bool)) string {
	if lookup == nil {
		return defaultFindStuffMCPURL
	}
	// raw、explicit 分别是环境变量原始值与是否显式设置。
	raw, explicit := lookup(findStuffMCPURLEnv)
	if !explicit {
		return defaultFindStuffMCPURL
	}
	return strings.TrimSpace(raw)
}

// mcpSession 是插件层所需的最小 MCP 会话能力；
// github.com/mark3labs/mcp-go/client 的 *client.Client 满足该接口，单测注入假实现即可隔离网络。
type mcpSession interface {
	// Start 建立传输层连接；streamable HTTP 传输默认不保持常驻连接。
	Start(ctx context.Context) error
	// Initialize 完成 MCP 初始化握手，未初始化前不允许调用工具。
	Initialize(ctx context.Context, request mcp.InitializeRequest) (*mcp.InitializeResult, error)
	// CallTool 调用一个已注册的 MCP 工具。
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	// Close 释放会话连接；仅连接竞态落败的一方会调用。
	Close() error
}

// newStreamableMCPSession 按端点构造真实的 MCP streamable HTTP 会话。
func newStreamableMCPSession(mcpURL string) (mcpSession, error) {
	return client.NewStreamableHttpClient(mcpURL)
}

// FindStuffReplier 是插件层回复实现：连接 find_stuff MCP 服务，按找书/找短剧意图检索资源。
// 并发约定：mu 只保护 session 指针的读写，连接握手与工具调用等网络等待一律在锁外执行；
// 会话按需懒连接，建连失败不缓存，下一条消息自动重试。
type FindStuffReplier struct {
	// mcpURL 是 find_stuff 的 MCP streamable HTTP 端点。
	mcpURL string
	// cookieID 是所属账号标识，只用于日志归属，不参与检索。
	cookieID string
	// logger 是插件层结构化日志器。
	logger *slog.Logger
	// newSession 按端点构造新的 MCP 会话；生产走 streamable HTTP 客户端，测试注入假会话。
	newSession func(mcpURL string) (mcpSession, error)
	// mu 保护 session 字段的读写；持锁期间禁止任何网络等待。
	mu sync.Mutex
	// session 是已完成初始化的 MCP 会话；nil 表示尚未建立或上次建连失败。
	session mcpSession
}

// NewFindStuffReplier 构造插件层回复器；mcpURL 为空表示不启用插件层，返回 nil。
func NewFindStuffReplier(mcpURL, cookieID string, logger *slog.Logger) *FindStuffReplier {
	if strings.TrimSpace(mcpURL) == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FindStuffReplier{
		mcpURL:     mcpURL,
		cookieID:   cookieID,
		logger:     logger.With("account", cookieID, "subsys", "plugin"),
		newSession: newStreamableMCPSession,
	}
}

// NewFindStuffReplierFromEnv 按进程环境变量构造插件层回复器：
// FIND_STUFF_MCP_URL 未设置时用 compose 内网默认地址，显式置空则返回 nil 关闭插件层。
func NewFindStuffReplierFromEnv(cookieID string, logger *slog.Logger) *FindStuffReplier {
	return NewFindStuffReplier(resolveFindStuffMCPURL(os.LookupEnv), cookieID, logger)
}

// Reply 实现 PluginReplier 接口：识别找书/找短剧意图并检索 find_stuff，命中时生成回复文本。
// 返回 nil 表示本层不接管（意图不匹配、未命中或工具降级），由后续优先级继续处理。
// 本方法只生成文本，不发送任何消息。
func (p *FindStuffReplier) Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error) {
	// stuffType、query、matched 分别是识别出的检索领域、查询词与是否命中意图。
	stuffType, query, matched := pluginIntent(m.Text)
	if !matched {
		return nil, nil
	}
	// callCtx、cancel 给本次检索（含首次建连握手）设置固定上限，超时即降级。
	callCtx, cancel := context.WithTimeout(ctx, pluginSearchTimeout)
	defer cancel()
	// out、err 分别是 stuff.search 的出参与失败原因。
	out, err := p.search(callCtx, stuffType, query)
	if err != nil {
		return nil, fmt.Errorf("find_stuff 插件层检索失败: %w", err)
	}
	switch out.Status {
	case "hit":
		if len(out.Hits) == 0 {
			return nil, nil
		}
		// text 是按命中条目组装好的回复文本；标题缺失等不可用结果不接管。
		text := pluginHitText(out.Hits[0], out.Type)
		if text == "" {
			return nil, nil
		}
		return &ReplyResult{Text: text, Source: "PLUGIN"}, nil
	case "multi":
		// multi 只给候选列表让买家回复序号；插件不保存会话状态，买家回复的名称会再次走本层检索。
		// text 是候选列表文本；候选为空时视为未命中，交回后续优先级。
		text := pluginCandidatesText(out.Hits, out.Type)
		if text == "" {
			return nil, nil
		}
		return &ReplyResult{Text: text, Source: "PLUGIN"}, nil
	default:
		// miss（或未知状态）不抢答：返回 nil，交回 AI/默认回复优先级，
		// 避免插件层固定话术覆盖卖家自己配置的默认回复。
		return nil, nil
	}
}

// search 通过 MCP 会话调用 stuff.search 并解析出参；会话懒连接，失败不缓存以便下一条消息重试。
func (p *FindStuffReplier) search(ctx context.Context, stuffType, query string) (*stuffSearchOutput, error) {
	// session、err 分别是可用会话与建连失败原因。
	session, err := p.ensureSession(ctx)
	if err != nil {
		return nil, err
	}
	// request 是 stuff.search 的具名入参：领域类型 + 买家原文，槽位抽取由 find_stuff 负责。
	request := mcp.CallToolRequest{}
	request.Params.Name = "stuff.search"
	request.Params.Arguments = map[string]any{"type": stuffType, "query": query}
	// result、err 分别是工具调用结果与调用失败原因。
	result, err := session.CallTool(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("调用 stuff.search 失败: %w", err)
	}
	return parseStuffSearchResult(result)
}

// ensureSession 返回已初始化的 MCP 会话；未连接时在锁外建连，建连失败不缓存。
func (p *FindStuffReplier) ensureSession(ctx context.Context) (mcpSession, error) {
	p.mu.Lock()
	// existing 是已建立会话的快照，读完后立即释放锁。
	existing := p.session
	p.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	// session、err 分别是本次新建的会话与建连失败原因。
	session, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	// winner 表示本次建连是否成为最终会话；并发落败的一方立即关闭自己的会话，避免连接泄漏。
	if p.session == nil {
		p.session = session
		p.mu.Unlock()
		return session, nil
	}
	// final 是并发情况下已经胜出的会话。
	final := p.session
	p.mu.Unlock()
	_ = session.Close()
	return final, nil
}

// connect 建立并初始化一个新的 MCP 会话；任何一步失败都会关闭半成品会话。
func (p *FindStuffReplier) connect(ctx context.Context) (mcpSession, error) {
	// session、err 分别是新建会话与构造失败原因。
	session, err := p.newSession(p.mcpURL)
	if err != nil {
		return nil, fmt.Errorf("构造 MCP 会话失败: %w", err)
	}
	if // startErr 是传输层建连失败原因。
	startErr := session.Start(ctx); startErr != nil {
		_ = session.Close()
		return nil, fmt.Errorf("启动 MCP 会话失败: %w", startErr)
	}
	// initRequest 是 MCP 初始化请求：声明客户端身份与账号归属，协议版本显式指定为 SDK 最新版本。
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{Name: "ydisks-xianyu-helper", Version: "1.0.0", Title: "account:" + p.cookieID}
	// initErr 是初始化握手失败原因。
	if _, initErr := session.Initialize(ctx, initRequest); initErr != nil {
		_ = session.Close()
		return nil, fmt.Errorf("初始化 MCP 会话失败: %w", initErr)
	}
	// 会话建立成功只记一次调试日志；重复消息复用同一会话，不会重复输出。
	p.logger.Debug("find_stuff MCP 会话已建立", "url", p.mcpURL)
	return session, nil
}

// stuffSearchOutput 是 stuff.search 的结构化出参，字段与 find_stuff 的 MCP 契约保持一致。
type stuffSearchOutput struct {
	// Type 是领域类型：book / drama。
	Type string `json:"type"`
	// Query 是 find_stuff 骨架实际使用的归一化查询词。
	Query string `json:"query"`
	// Status 取值 hit（单命中）/ multi（多候选）/ miss（未命中）。
	Status string `json:"status"`
	// Hits 是命中或候选列表；miss 时为空数组。
	Hits []stuffHit `json:"hits"`
}

// stuffHit 是命中条目：书填 author，短剧填 actors，其余字段为网盘分发信息。
type stuffHit struct {
	// EntityID 是实体（书/短剧）主键。
	EntityID int64 `json:"entity_id"`
	// Title 是书名或剧名。
	Title string `json:"title"`
	// Author 是书的作者；短剧为空。
	Author string `json:"author"`
	// Actors 是短剧的演员列表；书为空。
	Actors []string `json:"actors"`
	// PanURL 是网盘分享链接。
	PanURL string `json:"pan_url"`
	// PanCode 是网盘提取码，可为空。
	PanCode string `json:"pan_code"`
	// PanType 是网盘类型：quark / baidu / ali / 115 等。
	PanType string `json:"pan_type"`
}

// parseStuffSearchResult 解析工具结果：优先读结构化内容，缺失时解析文本内容里的同一份 JSON。
// 工具返回 isError 时按失败上报，由调用方按降级处理。
func parseStuffSearchResult(result *mcp.CallToolResult) (*stuffSearchOutput, error) {
	if result == nil {
		return nil, errors.New("find_stuff 返回空结果")
	}
	if result.IsError {
		return nil, fmt.Errorf("find_stuff 工具返回错误: %s", toolResultSummary(result))
	}
	// raw 是待解析的结构化 JSON：优先 structuredContent，缺失时取首个文本内容。
	raw := result.RawStructuredContent
	if len(raw) == 0 {
		raw = firstTextContent(result)
	}
	if len(raw) == 0 {
		return nil, errors.New("find_stuff 结果缺少结构化内容")
	}
	// out 是解析后的检索出参。
	var out stuffSearchOutput
	if // unmarshalErr 是出参 JSON 解析失败原因。
	unmarshalErr := json.Unmarshal(raw, &out); unmarshalErr != nil {
		return nil, fmt.Errorf("解析 stuff.search 结果失败: %w", unmarshalErr)
	}
	return &out, nil
}

// firstTextContent 取首个非空文本内容；find_stuff 用文本内容承载与结构化内容相同的 JSON。
func firstTextContent(result *mcp.CallToolResult) []byte {
	// content 是当前遍历到的工具结果内容项。
	for _, content := range result.Content {
		if // text、isText 分别是内容项文本与文本类型断言结果。
		text, isText := content.(mcp.TextContent); isText && strings.TrimSpace(text.Text) != "" {
			return []byte(text.Text)
		}
	}
	return nil
}

// toolResultSummary 汇总工具错误结果里的文本内容并按上限截断，仅用于日志与错误信息，不含凭证。
func toolResultSummary(result *mcp.CallToolResult) string {
	// texts 收集全部文本内容片段。
	texts := make([]string, 0, len(result.Content))
	// content 是当前遍历到的工具结果内容项。
	for _, content := range result.Content {
		if // text、isText 分别是内容项文本与文本类型断言结果。
		text, isText := content.(mcp.TextContent); isText && strings.TrimSpace(text.Text) != "" {
			texts = append(texts, text.Text)
		}
	}
	if len(texts) == 0 {
		return "无文本内容"
	}
	// summary 是拼接后的摘要；truncated 表示是否发生了截断。
	summary, _ := truncateReplyText(strings.Join(texts, "；"), pluginErrorSummaryRunes)
	return summary
}

// pluginIntent 识别消息是否像找书/找短剧，并给出检索领域与查询词。
// 规则从简：短剧词优先 → 找书词 → 书名号（《》是明确的资源指名信号）；
// 不命中则返回 false，插件层不介入普通砍价与闲聊。
func pluginIntent(text string) (stuffType, query string, matched bool) {
	// trimmed 是去掉首尾空白的原文，既用于意图判断也作为传给 find_stuff 的查询词。
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", false
	}
	if containsAnyKeyword(trimmed, pluginDramaKeywords) {
		return "drama", trimmed, true
	}
	if containsAnyKeyword(trimmed, pluginBookKeywords) {
		return "book", trimmed, true
	}
	if strings.Contains(trimmed, "《") && strings.Contains(trimmed, "》") {
		return "book", trimmed, true
	}
	return "", "", false
}

// containsAnyKeyword 报告文本是否包含任一关键词。
func containsAnyKeyword(text string, keywords []string) bool {
	// keyword 是当前遍历到的关键词。
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// pluginHitText 把单条命中组装成买家可读的回复文本：
// 第一行是《标题》与作者/主演，第二行是网盘链接与提取码（有则附上）。
// 标题缺失时返回空串，表示结果不可用、本层不接管。
func pluginHitText(hit stuffHit, stuffType string) string {
	// headline 是命中标题行；标题为空说明数据不完整。
	headline := pluginHitHeadline(hit, stuffType)
	if headline == "" {
		return ""
	}
	// lines 是逐行拼装的回复内容。
	lines := []string{headline}
	if strings.TrimSpace(hit.PanURL) != "" {
		// linkLine 是链接行；提取码非空时追加在同一行，减少消息长度。
		linkLine := "链接：" + strings.TrimSpace(hit.PanURL)
		if // code 是网盘提取码，可为空。
		code := strings.TrimSpace(hit.PanCode); code != "" {
			linkLine += " 提取码：" + code
		}
		lines = append(lines, linkLine)
	}
	return strings.Join(lines, "\n")
}

// pluginCandidatesText 把多候选组装成让买家回复序号的列表；超量时只展示前若干条。
// 候选阶段不附网盘链接，等买家选定后再次检索命中才会给链接。
func pluginCandidatesText(hits []stuffHit, stuffType string) string {
	// titles 收集可用候选的标题行，跳过标题缺失的不可用条目。
	titles := make([]string, 0, len(hits))
	// hit 是当前遍历到的候选条目。
	for _, hit := range hits {
		// headline 是候选标题行；标题缺失时跳过。
		headline := pluginHitHeadline(hit, stuffType)
		if headline == "" {
			continue
		}
		titles = append(titles, headline)
		if len(titles) >= pluginMaxCandidates {
			break
		}
	}
	if len(titles) == 0 {
		return ""
	}
	// lines 是「说明 + 编号候选」的完整候选文本。
	lines := make([]string, 0, len(titles)+1)
	lines = append(lines, fmt.Sprintf("找到 %d 个相关资源，请回复序号告诉我你要哪个：", len(titles)))
	// index、title 是当前遍历到的候选序号（从 1 开始）与标题行。
	for index, title := range titles {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, title))
	}
	return strings.Join(lines, "\n")
}

// pluginHitHeadline 生成命中标题行：《标题》+作者或主演；标题为空时返回空串。
func pluginHitHeadline(hit stuffHit, stuffType string) string {
	// title 是去掉空白后的标题。
	title := strings.TrimSpace(hit.Title)
	if title == "" {
		return ""
	}
	// headline 是带书名号的标题行。
	headline := "《" + title + "》"
	if stuffType == "drama" {
		if len(hit.Actors) > 0 {
			headline += " 主演：" + strings.Join(hit.Actors, "、")
		}
		return headline
	}
	if // author 是书的作者，可为空。
	author := strings.TrimSpace(hit.Author); author != "" {
		headline += " 作者：" + author
	}
	return headline
}