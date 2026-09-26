package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// pluginTestLogger 返回丢弃全部输出的日志器，避免插件层单测污染测试输出。
func pluginTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeMCPSession 是可控的 MCP 会话假实现：记录工具调用入参并返回预设结果。
type fakeMCPSession struct {
	// startErr、initErr、callErr 分别是建连、初始化与工具调用的失败注入；nil 表示成功。
	startErr, initErr, callErr error
	// result 是 CallTool 成功时返回的工具结果。
	result *mcp.CallToolResult
	// callArgs 记录每次工具调用的入参快照，用于断言 type 与 query。
	callArgs []map[string]any
	// closed 记录会话是否已被释放，用于验证连接竞态落败方会关闭自己的会话。
	closed bool
	// mu 保护 callArgs 与 closed，允许并发用例安全读写信道内的字段。
	mu sync.Mutex
}

// Start 实现 mcpSession 的建连步骤，返回预设错误。
func (f *fakeMCPSession) Start(context.Context) error {
	return f.startErr
}

// Initialize 实现 mcpSession 的初始化步骤，返回预设错误。
func (f *fakeMCPSession) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, f.initErr
}

// CallTool 实现 mcpSession 的工具调用，记录入参并返回预设结果或错误。
func (f *fakeMCPSession) CallTool(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// args 是本次工具调用的具名入参快照。
	args, _ := request.Params.Arguments.(map[string]any)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callArgs = append(f.callArgs, args)
	return f.result, f.callErr
}

// Close 实现 mcpSession 的释放动作，标记会话已被关闭。
func (f *fakeMCPSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// callCount 返回工具调用次数，读锁避免并发用例的数据竞争。
func (f *fakeMCPSession) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.callArgs)
}

// searchToolResult 按 find_stuff 的 NewToolResultJSON 形状组装 tools/call 成功结果：
// 文本内容承载同一份 JSON，便于验证文本解析路径。
func searchToolResult(t *testing.T, out stuffSearchOutput) *mcp.CallToolResult {
	t.Helper()
	// payload 是序列化后的出参 JSON 文本。
	payload, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("序列化 stuff.search 出参失败: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: string(payload)}},
	}
}

// newPluginTestReplier 构造注入了假会话的插件层回复器，隔离真实 MCP 网络。
func newPluginTestReplier(t *testing.T, session mcpSession) *FindStuffReplier {
	t.Helper()
	// replier 是完成假会话注入的插件层回复器。
	replier := NewFindStuffReplier("http://find-stuff:59190/mcp", "cid", pluginTestLogger())
	if replier == nil {
		t.Fatal("非空 MCP 地址应构造插件层回复器")
	}
	replier.newSession = func(string) (mcpSession, error) { return session, nil }
	return replier
}

// TestPluginReply_BookHitReturnsLinkAndCode 验证找书意图命中时组装含书名、作者、链接与提取码的文本。
func TestPluginReply_BookHitReturnsLinkAndCode(t *testing.T) {
	// session 是返回单条书籍命中的假 MCP 会话。
	session := &fakeMCPSession{result: searchToolResult(t, stuffSearchOutput{
		Type: "book", Query: "有《三体》吗", Status: "hit",
		Hits: []stuffHit{{
			EntityID: 1, Title: "三体", Author: "刘慈欣",
			PanURL: "https://pan.quark.cn/s/santi", PanCode: "abcd", PanType: "quark",
		}},
	})}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// res、err 分别是插件层回复结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("有《三体》吗", "item1", "chat1"))
	if err != nil {
		t.Fatalf("插件层不应返回错误: %v", err)
	}
	if res == nil || res.Source != "PLUGIN" {
		t.Fatalf("应命中插件层并标记 PLUGIN，got %+v", res)
	}
	// want 是回复文本必须包含的要素：书名、作者、链接与提取码。
	for _, want := range []string{"三体", "刘慈欣", "https://pan.quark.cn/s/santi", "abcd"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("回复应包含 %q，got %q", want, res.Text)
		}
	}
	// 书名号信号应判定为 book 领域。
	if got := session.callArgs[0]["type"]; got != "book" {
		t.Errorf("type: got %v want book", got)
	}
	// 查询词应是买家原文，槽位抽取交给 find_stuff。
	if got := session.callArgs[0]["query"]; got != "有《三体》吗" {
		t.Errorf("query: got %v want 原文", got)
	}
}

// TestPluginReply_DramaIntentUsesDramaType 验证主演/短剧类意图按 drama 领域检索并展示主演。
func TestPluginReply_DramaIntentUsesDramaType(t *testing.T) {
	// session 是返回单条短剧命中的假 MCP 会话。
	session := &fakeMCPSession{result: searchToolResult(t, stuffSearchOutput{
		Type: "drama", Query: "这个短剧谁主演的", Status: "hit",
		Hits: []stuffHit{{
			EntityID: 9, Title: "闪婚老公竟是首富", Actors: []string{"王小明", "李小红"},
			PanURL: "https://pan.quark.cn/s/drama9", PanType: "quark",
		}},
	})}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// res、err 分别是插件层回复结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("这个短剧谁主演的", "item2", "chat2"))
	if err != nil || res == nil {
		t.Fatalf("找短剧意图应命中插件层: res=%+v err=%v", res, err)
	}
	// 剧类关键词应判定为 drama 领域。
	if got := session.callArgs[0]["type"]; got != "drama" {
		t.Errorf("type: got %v want drama", got)
	}
	// 短剧命中应展示主演而不是作者。
	if !strings.Contains(res.Text, "王小明") || !strings.Contains(res.Text, "李小红") {
		t.Errorf("回复应包含主演，got %q", res.Text)
	}
}

// TestPluginReply_MultiReturnsCandidateList 验证多候选时给出候选列表并要求买家回复序号。
func TestPluginReply_MultiReturnsCandidateList(t *testing.T) {
	// session 是返回两条候选的假 MCP 会话。
	session := &fakeMCPSession{result: searchToolResult(t, stuffSearchOutput{
		Type: "book", Query: "有《三体》吗", Status: "multi",
		Hits: []stuffHit{
			{EntityID: 1, Title: "三体", Author: "刘慈欣"},
			{EntityID: 2, Title: "三体（全集）", Author: "刘慈欣"},
		},
	})}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// res、err 分别是插件层回复结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("有《三体》吗", "item1", "chat1"))
	if err != nil || res == nil {
		t.Fatalf("多候选应命中插件层: res=%+v err=%v", res, err)
	}
	// want 是候选文本必须包含的要素：序号引导与全部候选标题。
	for _, want := range []string{"序号", "三体", "三体（全集）"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("候选文本应包含 %q，got %q", want, res.Text)
		}
	}
	// 候选阶段不应提前泄露网盘链接，等买家选定后再给。
	if strings.Contains(res.Text, "https://") {
		t.Errorf("候选文本不应包含链接，got %q", res.Text)
	}
}

// TestPluginReply_MissReturnsNil 验证未命中时返回 nil，把回复交回后续优先级。
func TestPluginReply_MissReturnsNil(t *testing.T) {
	// session 是返回未命中的假 MCP 会话。
	session := &fakeMCPSession{result: searchToolResult(t, stuffSearchOutput{
		Type: "book", Query: "有《不存在的书》吗", Status: "miss", Hits: []stuffHit{},
	})}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// res、err 分别是插件层回复结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("有《不存在的书》吗", "item1", "chat1"))
	if err != nil {
		t.Fatalf("未命中不应视为失败: %v", err)
	}
	if res != nil {
		t.Fatalf("未命中应返回 nil 交给后续优先级，got %+v", res)
	}
	// 但检索确实发生过一次。
	if session.callCount() != 1 {
		t.Errorf("应调用一次 stuff.search，got %d", session.callCount())
	}
}

// TestPluginReply_NoIntentSkipsMCP 验证普通砍价/闲聊不触发插件层，避免无谓的 MCP 调用。
func TestPluginReply_NoIntentSkipsMCP(t *testing.T) {
	// session 是命中时才会被调用的假 MCP 会话。
	session := &fakeMCPSession{result: searchToolResult(t, stuffSearchOutput{Type: "book", Status: "miss"})}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// messages 是不应触发找资源意图的常见买家消息。
	messages := []string{"能便宜点吗", "在吗", "什么时候发货"}
	// text 是当前遍历到的买家消息。
	for _, text := range messages {
		// res、err 分别是插件层回复结果与失败原因。
		res, err := replier.Reply(context.Background(), chatMsg(text, "item1", "chat1"))
		if res != nil || err != nil {
			t.Fatalf("消息 %q 不应命中插件层: res=%+v err=%v", text, res, err)
		}
	}
	if session.callCount() != 0 {
		t.Errorf("无意图消息不应调用 MCP，got %d 次", session.callCount())
	}
}

// TestPluginReply_MCPUnavailableDegradesAndRetries 验证 MCP 不可达时降级返回 nil，且失败不被缓存、下次重试。
func TestPluginReply_MCPUnavailableDegradesAndRetries(t *testing.T) {
	// broken 是初始化失败的假会话，代表 find-stuff 容器未启动。
	broken := &fakeMCPSession{initErr: errors.New("connection refused")}
	// fixed 是重试时切换到的成功会话。
	fixed := &fakeMCPSession{result: searchToolResult(t, stuffSearchOutput{
		Type: "book", Status: "hit",
		Hits: []stuffHit{{EntityID: 3, Title: "活着", Author: "余华", PanURL: "https://pan.quark.cn/s/huozhe"}},
	})}
	// attempts 记录会话工厂被调用的次数，用于验证失败不会被缓存。
	attempts := 0
	// replier 是注入了“先坏后好”工厂的插件层回复器。
	replier := NewFindStuffReplier("http://find-stuff:59190/mcp", "cid", pluginTestLogger())
	replier.newSession = func(string) (mcpSession, error) {
		attempts++
		if attempts == 1 {
			return broken, nil
		}
		return fixed, nil
	}
	// res、err 分别是首次降级时的结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("有《活着》吗", "item1", "chat1"))
	if res != nil {
		t.Fatalf("MCP 不可达应降级返回 nil，got %+v", res)
	}
	if err == nil {
		t.Fatal("应向上返回降级原因供调用方记录日志")
	}
	if !broken.closed {
		t.Error("初始化失败的会话应被关闭，避免连接泄漏")
	}
	// res2、err2 分别是重试成功后的结果与失败原因。
	res2, err2 := replier.Reply(context.Background(), chatMsg("有《活着》吗", "item1", "chat2"))
	if err2 != nil || res2 == nil {
		t.Fatalf("重建连接后应命中插件层: res=%+v err=%v", res2, err2)
	}
	if attempts != 2 {
		t.Errorf("失败不应被缓存，会话工厂应被调用两次，got %d", attempts)
	}
}

// TestPluginReply_ToolErrorDegrades 验证工具返回 isError 时按降级处理而不是使用空结果。
func TestPluginReply_ToolErrorDegrades(t *testing.T) {
	// session 是工具返回错误的假 MCP 会话。
	session := &fakeMCPSession{result: &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{mcp.TextContent{Type: "text", Text: "book 检索必须提供 query"}},
	}}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// res、err 分别是插件层回复结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("有《三体》吗", "item1", "chat1"))
	if res != nil || err == nil {
		t.Fatalf("工具错误应降级: res=%+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "book 检索必须提供 query") {
		t.Errorf("错误信息应包含工具返回原因，got %v", err)
	}
}

// TestPluginReply_ParsesStructuredContentWithoutText 验证只有 structuredContent 时仍能解析命中。
func TestPluginReply_ParsesStructuredContentWithoutText(t *testing.T) {
	// payload 是结构化内容里的出参 JSON。
	payload, marshalErr := json.Marshal(stuffSearchOutput{
		Type: "drama", Status: "hit",
		Hits: []stuffHit{{EntityID: 5, Title: "神级赘婿", Actors: []string{"赵大力"}, PanURL: "https://pan.quark.cn/s/zhuixu"}},
	})
	if marshalErr != nil {
		t.Fatalf("序列化出参失败: %v", marshalErr)
	}
	// session 是只返回结构化内容的假 MCP 会话。
	session := &fakeMCPSession{result: &mcp.CallToolResult{RawStructuredContent: payload}}
	// replier 是注入了假会话的插件层回复器。
	replier := newPluginTestReplier(t, session)
	// res、err 分别是插件层回复结果与失败原因。
	res, err := replier.Reply(context.Background(), chatMsg("主演是谁", "item1", "chat1"))
	if err != nil || res == nil {
		t.Fatalf("结构化内容应可解析: res=%+v err=%v", res, err)
	}
	if !strings.Contains(res.Text, "赵大力") {
		t.Errorf("回复应包含主演，got %q", res.Text)
	}
}

// TestFindStuffReplier_EmptyURLDisables 验证空地址不构造插件层（显式关闭开关）。
func TestFindStuffReplier_EmptyURLDisables(t *testing.T) {
	if // replier 是空地址的构造结果，应为 nil 表示关闭插件层。
	replier := NewFindStuffReplier("  ", "cid", nil); replier != nil {
		t.Fatalf("空地址应返回 nil 关闭插件层，got %+v", replier)
	}
}

// TestResolveFindStuffMCPURL 验证 MCP 地址解析：未设置用默认内网地址，显式置空关闭，显式配置优先。
func TestResolveFindStuffMCPURL(t *testing.T) {
	// defaulted 是环境变量未设置时的解析结果。
	defaulted := resolveFindStuffMCPURL(func(string) (string, bool) { return "", false })
	if defaulted != "http://find-stuff:59190/mcp" {
		t.Errorf("未设置应回落默认地址，got %q", defaulted)
	}
	// explicit 是显式配置时的解析结果。
	explicit := resolveFindStuffMCPURL(func(string) (string, bool) { return " http://plugin.internal:59190/mcp ", true })
	if explicit != "http://plugin.internal:59190/mcp" {
		t.Errorf("显式配置应优先并去掉空白，got %q", explicit)
	}
	// disabled 是显式置空（关闭插件层）的解析结果。
	disabled := resolveFindStuffMCPURL(func(string) (string, bool) { return "", true })
	if disabled != "" {
		t.Errorf("显式置空应关闭插件层，got %q", disabled)
	}
}

// fakePluginReplier 是可控的插件层回复 mock，用于验证 resolve 的优先级顺序。
type fakePluginReplier struct {
	// result 与 err 是 Reply 的预设返回。
	result *ReplyResult
	err    error
	// called 记录被调用次数，用于断言优先级是否真的走到插件层。
	called int
}

// Reply 返回预设结果并累计调用次数。
func (f *fakePluginReplier) Reply(context.Context, ChatMessage) (*ReplyResult, error) {
	f.called++
	return f.result, f.err
}

// TestResolve_PluginLayerOrder 验证插件层位于关键词之后、AI 之前：
// 关键词命中时不触达插件层；插件命中时不触达 AI；插件降级时 AI 接管。
func TestResolve_PluginLayerOrder(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是测试用上下文。
	ctx := context.Background()

	// 子场景一：关键词优先于插件层。关键词表由迁移创建，直接写入一条与书名相关的关键词。
	if // execErr 是关键词写入失败原因。
	_, execErr := store.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','三体','关键词优先','text')`); execErr != nil {
		t.Fatalf("写入关键词失败: %v", execErr)
	}
	// plugin 是预设命中的插件层 mock。
	plugin := &fakePluginReplier{result: &ReplyResult{Text: "插件命中", Source: "PLUGIN"}}
	// ai 是预设命中的 AI 回复 mock。
	ai := &fakeAIReplier{result: &ReplyResult{Text: "AI 回复"}}
	// service 是接入插件层的回复服务。
	service := NewReplyService("cid", store, nil, nil, ai, plugin, pluginTestLogger())
	// res 是关键词与插件同时可用时的解析结果。
	res := service.resolve(ctx, chatMsg("有《三体》吗", "item1", "chat1"))
	if res == nil || res.Source != "关键词" {
		t.Fatalf("关键词应优先于插件层，got %+v", res)
	}
	if plugin.called != 0 {
		t.Errorf("关键词命中不应触达插件层，got %d 次", plugin.called)
	}

	// 子场景二：关键词不命中时插件层优先于 AI。
	plugin.called = 0
	// res2 是无关键词时的解析结果。
	res2 := service.resolve(ctx, chatMsg("有《活着》吗", "item1", "chat2"))
	if res2 == nil || res2.Source != "PLUGIN" || res2.Text != "插件命中" {
		t.Fatalf("插件层应优先于 AI，got %+v", res2)
	}
	if ai.called != 0 {
		t.Errorf("插件命中不应触达 AI，got %d 次", ai.called)
	}

	// 子场景三：插件层降级（返回 nil）时由 AI 接管。
	plugin.result = nil
	// res3 是插件降级后的解析结果。
	res3 := service.resolve(ctx, chatMsg("有《百年孤独》吗", "item1", "chat3"))
	if res3 == nil || res3.Source != "AI" {
		t.Fatalf("插件降级后应由 AI 接管，got %+v", res3)
	}

	// 子场景四：插件层报错只记录日志，仍由 AI 接管。
	plugin.err = errors.New("find_stuff 不可达")
	// res4 是插件报错后的解析结果。
	res4 := service.resolve(ctx, chatMsg("有《围城》吗", "item1", "chat4"))
	if res4 == nil || res4.Source != "AI" {
		t.Fatalf("插件报错不应中断链路，应由 AI 接管，got %+v", res4)
	}
}

// TestPluginReply_ConcurrentFirstUseSharesSession 验证并发首用时只有一个会话被保留复用，落败会话被关闭。
func TestPluginReply_ConcurrentFirstUseSharesSession(t *testing.T) {
	// mu 保护 created 列表的并发写入。
	var mu sync.Mutex
	// created 收集工厂创建的全部假会话，用于断言复用与关闭数量。
	created := make([]*fakeMCPSession, 0)
	// hitResult 是所有并发会话共用的单条命中结果；提前构造避免在 goroutine 里调用 t.Fatalf。
	hitResult := searchToolResult(t, stuffSearchOutput{
		Type: "book", Status: "hit",
		Hits: []stuffHit{{EntityID: 7, Title: "解忧杂货店", Author: "东野圭吾", PanURL: "https://pan.quark.cn/s/jieyou"}},
	})
	// replier 是并发驱动的插件层回复器，工厂每次构造一个新的假会话。
	replier := NewFindStuffReplier("http://find-stuff:59190/mcp", "cid", pluginTestLogger())
	replier.newSession = func(string) (mcpSession, error) {
		// session 是本次构造的假会话；登记后返回以便后续断言。
		session := &fakeMCPSession{result: hitResult}
		mu.Lock()
		defer mu.Unlock()
		created = append(created, session)
		return session, nil
	}
	// wg 等待全部并发调用收尾。
	var wg sync.WaitGroup
	// i 是并发调用的序号。
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if // res、err 分别是并发调用下的插件层结果与失败原因。
			res, err := replier.Reply(context.Background(), chatMsg("有《解忧杂货店》吗", "item1", "chat-concurrent")); err != nil || res == nil {
				t.Errorf("并发调用不应失败: res=%+v err=%v", res, err)
			}
		}()
	}
	wg.Wait()
	if len(created) == 0 {
		t.Fatal("并发调用应至少建立一个会话")
	}
	// closedCount 统计被关闭的落败会话数量。
	closedCount := 0
	// session 是当前遍历到的假会话。
	for _, session := range created {
		if session.closed {
			closedCount++
		}
	}
	if closedCount != len(created)-1 {
		t.Errorf("应只保留一个会话，其余全部关闭：created=%d closed=%d", len(created), closedCount)
	}
}