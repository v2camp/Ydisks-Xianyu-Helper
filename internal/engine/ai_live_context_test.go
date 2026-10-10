// ai_live_context_test.go 动态知识注入的聚焦测试：在售清单、库存摘要、发货推断、
// 降级路径与边界词表扩充。全部走本地 SQLite 夹具，不访问网络。
package engine

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// seedLiveItem 向测试库写入一条在售商品，返回写入错误。
func seedLiveItem(t *testing.T, s *db.Store, itemID, title, price, desc string) {
	t.Helper()
	// err 是商品写入数据库时的错误。
	if _, err := s.DB.ExecContext(context.Background(),
		`INSERT INTO item_info (cookie_id, item_id, item_title, item_price, item_description, item_detail)
		 VALUES ('cid', ?, ?, ?, ?, '')`, itemID, title, price, desc); err != nil {
		t.Fatalf("写入商品 %s 失败: %v", itemID, err)
	}
}

// TestLoadLiveCatalogLines 动态列表按「- 标题（价格）」格式输出，空标题跳过。
func TestLoadLiveCatalogLines(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	seedLiveItem(t, s, "i1", "糯糯下山", "25", "描述")
	seedLiveItem(t, s, "i2", "梦遇崔郎", "", "描述")
	seedLiveItem(t, s, "i3", "  ", "10", "空标题")
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// lines、err 是动态列表查询结果。
	lines, err := a.loadLiveCatalogLines(context.Background())
	if err != nil {
		t.Fatalf("loadLiveCatalogLines: %v", err)
	}
	// joined 是便于断言的拼接结果。
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "- 糯糯下山（25）") {
		t.Fatalf("缺少带价格行: %s", joined)
	}
	if !strings.Contains(joined, "- 梦遇崔郎") {
		t.Fatalf("缺少无价格行: %s", joined)
	}
	if strings.Contains(joined, "  ") || strings.Contains(joined, "空标题") {
		t.Fatalf("空标题不应输出: %s", joined)
	}
	if len(lines) != 2 {
		t.Fatalf("应输出 2 行，实际 %d: %s", len(lines), joined)
	}
}

// TestLoadCardStockLines 库存摘要输出「组名：N 张」，跳过停用组与非数据组。
func TestLoadCardStockLines(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是卡组写入上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户。
	ownerID, err := s.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatalf("GetOwnerID: %v", err)
	}
	// 启用数据组 2 行库存。
	if _, err := s.Cards.Create(ctx, &db.CardFull{UserID: ownerID, Name: "夸克资源码", Type: "data", DataContent: "code-1\ncode-2", Enabled: true}); err != nil {
		t.Fatalf("创建数据卡组失败: %v", err)
	}
	// 停用数据组应跳过。
	if _, err := s.Cards.Create(ctx, &db.CardFull{UserID: ownerID, Name: "停用组", Type: "data", DataContent: "x", Enabled: false}); err != nil {
		t.Fatalf("创建停用卡组失败: %v", err)
	}
	// 文本组不计入张数。
	if _, err := s.Cards.Create(ctx, &db.CardFull{UserID: ownerID, Name: "文本组", Type: "text", TextContent: "hello", Enabled: true}); err != nil {
		t.Fatalf("创建文本卡组失败: %v", err)
	}
	// 空名启用数据组应回退未命名组。
	if _, err := s.Cards.Create(ctx, &db.CardFull{UserID: ownerID, Name: "", Type: "data", DataContent: "a\n\n b", Enabled: true}); err != nil {
		t.Fatalf("创建空名卡组失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// lines、err 是库存摘要查询结果。
	lines, err := a.loadCardStockLines(ctx)
	if err != nil {
		t.Fatalf("loadCardStockLines: %v", err)
	}
	// joined 是便于断言的拼接结果。
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "- 夸克资源码：2 张") {
		t.Fatalf("缺少数据组库存行: %s", joined)
	}
	if !strings.Contains(joined, "- 未命名组：2 张") {
		t.Fatalf("空名组应回退未命名组: %s", joined)
	}
	if strings.Contains(joined, "停用组") || strings.Contains(joined, "文本组") {
		t.Fatalf("停用组与文本组不应输出: %s", joined)
	}
	if strings.Contains(joined, "code-1") {
		t.Fatalf("卡密正文不得注入: %s", joined)
	}
}

// TestBuildKnowledgeContextLiveCatalog 动态列表优先注入，手工 catalog 不再出现。
func TestBuildKnowledgeContextLiveCatalog(t *testing.T) {
	// k 是带手工 catalog 的语料配置。
	k := &aiKnowledgeConfig{Catalog: []aiCatalogItem{{Title: "旧清单", Detail: "旧说明"}}}
	// live 是动态列表非空的注入数据。
	live := liveKnowledge{CatalogLines: []string{"- 新商品（10）"}}
	// ctx 是拼装结果。
	ctx := k.buildKnowledgeContext("有货吗", live)
	if !strings.Contains(ctx, liveCatalogInstruction) || !strings.Contains(ctx, "- 新商品（10）") {
		t.Fatalf("动态列表未注入: %s", ctx)
	}
	if strings.Contains(ctx, "旧清单") || strings.Contains(ctx, "本店在售资源清单") {
		t.Fatalf("动态非空时不得回落手工 catalog: %s", ctx)
	}
}

// TestBuildKnowledgeContextFallbackManualCatalog 动态列表为空时回落手工 catalog。
func TestBuildKnowledgeContextFallbackManualCatalog(t *testing.T) {
	// k 是带手工 catalog 的语料配置。
	k := &aiKnowledgeConfig{Catalog: []aiCatalogItem{{Title: "糯糯下山", Detail: "1-3季全"}}}
	// ctx 是动态为空时的拼装结果。
	ctx := k.buildKnowledgeContext("有第三季吗", liveKnowledge{})
	if !strings.Contains(ctx, "糯糯下山（1-3季全）") {
		t.Fatalf("应回落手工 catalog: %s", ctx)
	}
}

// TestBuildKnowledgeContextStockAndDelivery 库存摘要与发货推断按需注入。
func TestBuildKnowledgeContextStockAndDelivery(t *testing.T) {
	// k 是空语料配置。
	k := &aiKnowledgeConfig{}
	// live 是含库存与发货线索的注入数据。
	live := liveKnowledge{
		StockLines:   []string{"- 夸克资源码：3 张"},
		DeliveryHint: "该商品发货方式（按描述推断，仅参考）：夸克网盘",
	}
	// ctx 是拼装结果。
	ctx := k.buildKnowledgeContext("还有货吗", live)
	if !strings.Contains(ctx, liveStockInstruction) || !strings.Contains(ctx, "夸克资源码：3 张") {
		t.Fatalf("库存摘要未注入: %s", ctx)
	}
	if !strings.Contains(ctx, "该商品发货方式（按描述推断，仅参考）：夸克网盘") {
		t.Fatalf("发货推断未注入: %s", ctx)
	}
	// 纯 FAQ 场景不应出现库存段落。
	faqOnly := k.buildKnowledgeContext("x", liveKnowledge{})
	if strings.Contains(faqOnly, liveStockInstruction) {
		t.Fatalf("无库存数据不应注入库存段落: %s", faqOnly)
	}
}

// TestLoadLiveKnowledgeQueryErrorDegrade 本地库查询失败时降级为不注入并继续。
func TestLoadLiveKnowledgeQueryErrorDegrade(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// 关闭底层数据库以制造查询失败。
	if err := s.DB.Close(); err != nil {
		t.Fatalf("关闭数据库失败: %v", err)
	}
	// live 是降级后的注入数据，应全部为空且不 panic。
	live := a.loadLiveKnowledge(context.Background(), "item-x")
	if len(live.CatalogLines) != 0 || len(live.StockLines) != 0 || live.DeliveryHint != "" {
		t.Fatalf("查询失败应降级为空: %+v", live)
	}
}

// TestInferDeliveryHintsFromDescription 商品描述关键词推断渠道，分步发送模式单独标注。
func TestInferDeliveryHintsFromDescription(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	seedLiveItem(t, s, "d1", "短剧合集", "30", "夸克网盘发货，先发码后发链接")
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(context.Background(), "d1")
	if err != nil {
		t.Fatalf("inferDeliveryHints: %v", err)
	}
	if !strings.Contains(hint, "夸克网盘") {
		t.Fatalf("缺少渠道线索: %s", hint)
	}
	if !strings.Contains(hint, "分步发送") {
		t.Fatalf("分步模式未标注: %s", hint)
	}
	if !strings.Contains(hint, "按描述推断") {
		t.Fatalf("无发货配置时应标注仅按描述: %s", hint)
	}
}

// TestInferDeliveryHintsFromTemplate 多条模板消息标注分 N 条发送，配置来源写入依据。
func TestInferDeliveryHintsFromTemplate(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是夹具写入上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户。
	ownerID, err := s.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatalf("GetOwnerID: %v", err)
	}
	// template 是两步发货模板：先资源码后链接。
	templateID, err := s.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{
		UserID: ownerID, Name: "分步模板", Enabled: true,
		Messages: []string{"资源码：ABCD", "链接：https://example.invalid/s"},
	})
	if err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
	// rule 是绑定该模板的商品级自动化规则。
	if _, err := s.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: ownerID, CookieID: "cid", ItemID: "t1", Name: "发货规则",
		TriggerType: "order_paid", Enabled: true,
		Actions: []db.AutomationActionInput{{
			ActionType: "send_template", DeliveryTemplateID: templateID,
			ConfigJSON: `{}`, Enabled: true, SortOrder: 1,
		}},
	}); err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(ctx, "t1")
	if err != nil {
		t.Fatalf("inferDeliveryHints: %v", err)
	}
	if !strings.Contains(hint, "资源码") || !strings.Contains(hint, "链接") {
		t.Fatalf("模板渠道未提取: %s", hint)
	}
	if !strings.Contains(hint, "分 2 条消息发送（先资源码后链接）") {
		t.Fatalf("多步发送未标注: %s", hint)
	}
	if !strings.Contains(hint, "按描述/发货配置推断") {
		t.Fatalf("有配置来源时应标注双来源: %s", hint)
	}
}

// TestInferDeliveryHintsNoClue 无任何线索时返回空且不注入。
func TestInferDeliveryHintsNoClue(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	seedLiveItem(t, s, "n1", "普通商品", "9", "无特别说明")
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(context.Background(), "n1")
	if err != nil {
		t.Fatalf("inferDeliveryHints: %v", err)
	}
	if hint != "" {
		t.Fatalf("无线索应返回空: %q", hint)
	}
}

// TestInferDeliveryHintsItemMissing 商品不存在时只用配置线索，缺失不算错误。
func TestInferDeliveryHintsItemMissing(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(context.Background(), "no-such-item")
	if err != nil {
		t.Fatalf("商品缺失不应报错: %v", err)
	}
	if hint != "" {
		t.Fatalf("无线索应返回空: %q", hint)
	}
}

// TestDeliveryAnswerConstraintInPrompt 系统约束文案必须进入 system prompt。
func TestDeliveryAnswerConstraintInPrompt(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// srv、rec 是记录 system prompt 的 mock 模型。
	srv, rec := aiContentMockServer(t, "不同商品发货方式不同哦")
	s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`)
	s.Settings.Set(ctx, "ai_api_key", "sk-test")
	s.Settings.Set(ctx, "ai_api_url", srv.URL)
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// res 是完整回复链路结果。
	res, err := a.Reply(ctx, chatMsg("能便宜点吗", "item1", "chat-c"))
	if err != nil || res == nil {
		t.Fatalf("应接管: res=%v err=%v", res, err)
	}
	if !strings.Contains(rec.system(), deliveryAnswerConstraint) {
		t.Fatalf("系统约束未写入 prompt: %s", rec.system())
	}
}

// TestBuiltinNegativeWordsExpanded 新增负向词命中时禁止 AI 接管。
func TestBuiltinNegativeWordsExpanded(t *testing.T) {
	// s、cleanup 是空配置 store，回落内置边界。
	s, cleanup := scopeStore(t, "", "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// scope、err 是内置边界。
	scope, err := a.loadScope(context.Background())
	if err != nil {
		t.Fatalf("loadScope: %v", err)
	}
	// samples 是必须被负向拦截的扩展词样例。
	samples := []string{"加微信聊", "扫码支付", "线下交易", "我去法院起诉", "打12315投诉会封号", "站外便宜"}
	// sample 表示当前遍历到的拦截样例。
	for _, sample := range samples {
		// take 是该样例的接管判定。
		take, _ := scope.decide(sample)
		if take {
			t.Fatalf("负向词 %q 应拦截", sample)
		}
	}
	// 纯砍价仍然接管。
	if take, _ := scope.decide("能便宜点吗"); !take {
		t.Fatal("纯砍价应接管")
	}
}

// TestBuiltinOrderWordsExpanded 发货意图扩充词应命中内置 order 意图（仅标注不接管）。
func TestBuiltinOrderWordsExpanded(t *testing.T) {
	// s、cleanup 是空配置 store。
	s, cleanup := scopeStore(t, "", "", "")
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// scope、err 是内置边界。
	scope, err := a.loadScope(context.Background())
	if err != nil {
		t.Fatalf("loadScope: %v", err)
	}
	// samples 是应命中 order 的扩充问法。
	samples := []string{"怎么发", "什么时候发", "多久发", "发货方式是什么", "什么网盘"}
	// sample 表示当前遍历到的问法。
	for _, sample := range samples {
		// take、hits 是判定结果。
		take, hits := scope.decide(sample)
		if take {
			t.Fatalf("order 意图不应接管: %q", sample)
		}
		if len(hits) != 1 || hits[0] != IntentOrder {
			t.Fatalf("%q 应命中 order: hits=%v", sample, hits)
		}
	}
}

// TestCountNonEmptyLines 覆盖空行/回车换行的库存计数。
func TestCountNonEmptyLines(t *testing.T) {
	// got 是混合换行与空白内容的非空行计数。
	if got := countNonEmptyLines("a\n\n b \r\n"); got != 2 {
		t.Fatalf("countNonEmptyLines=%d want 2", got)
	}
	// got 是空内容的非空行计数。
	if got := countNonEmptyLines(""); got != 0 {
		t.Fatalf("空内容应为 0，实际 %d", got)
	}
}

// TestFormatDeliveryHintBases 无配置来源时依据只写描述。
func TestFormatDeliveryHintBases(t *testing.T) {
	// onlyDesc 是无配置来源的推断结果。
	onlyDesc := formatDeliveryHint([]string{"夸克网盘"}, false, 0, false, false)
	if !strings.Contains(onlyDesc, "按描述推断") || strings.Contains(onlyDesc, "发货配置") {
		t.Fatalf("无配置来源依据错误: %s", onlyDesc)
	}
	// stepOnly 是仅分步无线条数的结果。
	stepOnly := formatDeliveryHint(nil, true, 1, true, false)
	if !strings.Contains(stepOnly, "分步发送") {
		t.Fatalf("仅分步未标注: %s", stepOnly)
	}
	// multiNoStep 是多条消息无分步的结果。
	multiNoStep := formatDeliveryHint(nil, false, 3, true, false)
	if !strings.Contains(multiNoStep, "分 3 条消息发送") || strings.Contains(multiNoStep, "先资源码") {
		t.Fatalf("多条无分步标注错误: %s", multiNoStep)
	}
}

// TestFormatDeliveryHintAuthoritative 卡密识别出的渠道应标记为可确定，供 AI 直接回答。
// 这是客服不再对「有夸克吗」打太极的关键：依据文案必须区别于「仅参考」的推断。
func TestFormatDeliveryHintAuthoritative(t *testing.T) {
	// authoritative 是卡密渠道确定时的结果。
	authoritative := formatDeliveryHint([]string{"百度网盘", "夸克网盘"}, false, 1, true, true)
	if !strings.Contains(authoritative, "按该商品卡密内容确定") {
		t.Fatalf("卡密渠道应标注为可确定: %s", authoritative)
	}
	if strings.Contains(authoritative, "仅参考") {
		t.Fatalf("卡密渠道不应再标注仅参考: %s", authoritative)
	}
	if !strings.Contains(authoritative, "百度网盘、夸克网盘") {
		t.Fatalf("双盘渠道应并列输出: %s", authoritative)
	}
	// 同样是双盘，但来自文案推断时仍应标注仅参考。
	inferred := formatDeliveryHint([]string{"百度网盘", "夸克网盘"}, false, 1, true, false)
	if !strings.Contains(inferred, "仅参考") || strings.Contains(inferred, "卡密内容确定") {
		t.Fatalf("文案推断不应冒充确定事实: %s", inferred)
	}
}

// TestDetectDeliveryChannelsDedupe 网盘码与网盘同义，渠道展示名去重后只输出一次。
func TestDetectDeliveryChannelsDedupe(t *testing.T) {
	// channels 是对「网盘码」文本的渠道探测结果。
	channels := detectDeliveryChannels("发网盘码")
	if len(channels) != 1 || channels[0] != "网盘" {
		t.Fatalf("渠道去重失败: %v", channels)
	}
}

// TestLoadCardStockLinesQueryError 卡组查询失败时向上返回错误由调用方降级。
func TestLoadCardStockLinesQueryError(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// 删除 cards 表使卡组查询失败，但账号归属查询仍可成功。
	if _, err := s.DB.ExecContext(context.Background(), `DROP TABLE cards`); err != nil {
		t.Fatalf("删除 cards 表失败: %v", err)
	}
	// lines、err 是库存查询结果，应返回错误而不是空成功。
	lines, err := a.loadCardStockLines(context.Background())
	if err == nil || lines != nil {
		t.Fatalf("卡组查询失败应报错: lines=%v err=%v", lines, err)
	}
}

// TestInferDeliveryHintsConfigQueryError 配置来源查询失败时降级为只用商品描述。
func TestInferDeliveryHintsConfigQueryError(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	seedLiveItem(t, s, "e1", "资源合集", "20", "百度网盘发货")
	// 删除 automation 表使发货配置查询失败；商品读取不受影响。
	if _, err := s.DB.ExecContext(context.Background(), `DROP TABLE automation_rules`); err != nil {
		t.Fatalf("删除 automation_rules 表失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果，应降级为描述线索而非报错。
	hint, err := a.inferDeliveryHints(context.Background(), "e1")
	if err != nil {
		t.Fatalf("配置查询失败应降级: %v", err)
	}
	if !strings.Contains(hint, "百度网盘") || !strings.Contains(hint, "按描述推断") {
		t.Fatalf("应只用描述线索: %s", hint)
	}
}

// TestAppendDeliveryConfigCluesOwnerQueryError 归属查询失败时返回错误由调用方降级。
func TestAppendDeliveryConfigCluesOwnerQueryError(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// 删除 cookies 表使归属查询失败。
	if _, err := s.DB.ExecContext(context.Background(), `DROP TABLE cookies`); err != nil {
		t.Fatalf("删除 cookies 表失败: %v", err)
	}
	// corpus 是线索文本缓冲。
	var corpus strings.Builder
	// multi 是模板最大消息条数；codeThenLink 是先码后链标记。
	multi, codeThenLink := 0, false
	// found、err 是线索收集结果，应返回错误而不是静默成功。
	found, _, err := a.appendDeliveryConfigClues(context.Background(), "any-item", &corpus, &multi, &codeThenLink)
	if err == nil || found {
		t.Fatalf("归属查询失败应报错: found=%v err=%v", found, err)
	}
}

// TestAppendDeliveryConfigCluesSkips 发货配置线索跳过无关商品规则与停用动作。
func TestAppendDeliveryConfigCluesSkips(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是夹具写入上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户。
	ownerID, err := s.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatalf("GetOwnerID: %v", err)
	}
	// otherRule 是绑定到其他商品的规则，应被跳过。
	if _, err := s.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: ownerID, CookieID: "cid", ItemID: "other-item", Name: "无关规则",
		TriggerType: "order_paid", Enabled: true,
		Actions: []db.AutomationActionInput{{
			ActionType: "send_card", CardID: 0, MessageTemplate: "无关消息",
			ConfigJSON: `{}`, Enabled: true, SortOrder: 1,
		}},
	}); err != nil {
		t.Fatalf("创建无关规则失败: %v", err)
	}
	// disabledActionRule 含停用动作，其文案不应进入线索。
	if _, err := s.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: ownerID, CookieID: "cid", ItemID: "skip-item", Name: "停用动作规则",
		TriggerType: "order_paid", Enabled: true,
		Actions: []db.AutomationActionInput{{
			ActionType: "send_card", MessageTemplate: "停用动作文案",
			ConfigJSON: `{}`, Enabled: false, SortOrder: 1,
		}},
	}); err != nil {
		t.Fatalf("创建停用动作规则失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// corpus 是线索文本缓冲。
	var corpus strings.Builder
	// multi 是模板最大消息条数；codeThenLink 是先码后链标记。
	multi, codeThenLink := 0, false
	// found 是线索来源命中结果。
	found, _, err := a.appendDeliveryConfigClues(ctx, "skip-item", &corpus, &multi, &codeThenLink)
	if err != nil {
		t.Fatalf("appendDeliveryConfigClues: %v", err)
	}
	if !found {
		t.Fatal("商品级规则应命中")
	}
	if strings.Contains(corpus.String(), "无关消息") || strings.Contains(corpus.String(), "停用动作文案") {
		t.Fatalf("无关与停用动作不应进入线索: %s", corpus.String())
	}
}

// TestAppendDeliveryConfigCluesBindingCardName 卡密变量绑定的组名进入线索文本。
func TestAppendDeliveryConfigCluesBindingCardName(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是夹具写入上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户。
	ownerID, err := s.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatalf("GetOwnerID: %v", err)
	}
	// card 是带卡密变量绑定的数据卡组，组名含渠道关键词用于线索探测。
	cardID, err := s.Cards.Create(ctx, &db.CardFull{UserID: ownerID, Name: "百度网盘资源组", Type: "data", DataContent: "x", Enabled: true})
	if err != nil {
		t.Fatalf("创建卡组失败: %v", err)
	}
	// template 是引用卡密变量的发货模板。
	templateID, err := s.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{
		UserID: ownerID, Name: "绑定模板", Enabled: true,
		Messages: []string{"{{cards.main}}"},
	})
	if err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
	// rule 是绑定该模板的商品级规则。
	if _, err := s.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: ownerID, CookieID: "cid", ItemID: "bind-item", Name: "绑定规则",
		TriggerType: "order_paid", Enabled: true,
		Actions: []db.AutomationActionInput{{
			ActionType: "send_template", DeliveryTemplateID: templateID,
			TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 1}},
			ConfigJSON:       `{}`, Enabled: true, SortOrder: 1,
		}},
	}); err != nil {
		t.Fatalf("创建绑定规则失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(ctx, "bind-item")
	if err != nil {
		t.Fatalf("inferDeliveryHints: %v", err)
	}
	// 卡组名「百度网盘资源组」应被识别为百度网盘渠道，证明绑定名称进入线索。
	if !strings.Contains(hint, "百度网盘") {
		t.Fatalf("绑定卡组名未进入线索: %s", hint)
	}
}

// TestInferDeliveryHintsPrefersCardAuthoritativeChannels 卡密正文识别出的渠道是确定事实，
// 应压过商品描述里的渠道关键词，并让提示词标注为可直接回答——这是「有夸克吗」不再打太极的关键。
func TestInferDeliveryHintsPrefersCardAuthoritativeChannels(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是夹具写入上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户。
	ownerID, err := s.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatalf("GetOwnerID: %v", err)
	}
	// cardID 是已由应用层提取渠道（百度网盘）的卡券；正文含百度分享链接。
	cardID, err := s.Cards.Create(ctx, &db.CardFull{
		UserID: ownerID, Name: "资源组", Type: "data", DataContent: "https://pan.baidu.com/s/1x",
		Enabled: true, DeliveryChannels: "百度网盘",
	})
	if err != nil {
		t.Fatalf("创建卡组失败: %v", err)
	}
	// 商品描述故意写夸克，用于验证权威渠道优先于文案关键词探测。
	seedLiveItem(t, s, "auth-item", "网盘合集", "19", "夸克网盘发货")
	// rule 是绑定该卡券的商品级发货规则。
	if _, err := s.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: ownerID, CookieID: "cid", ItemID: "auth-item", Name: "发货规则",
		TriggerType: "order_paid", Enabled: true,
		Actions: []db.AutomationActionInput{{
			ActionType: "send_card", CardID: cardID, ConfigJSON: `{}`, Enabled: true, SortOrder: 1,
		}},
	}); err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(ctx, "auth-item")
	if err != nil {
		t.Fatalf("inferDeliveryHints: %v", err)
	}
	if !strings.Contains(hint, "百度网盘") {
		t.Fatalf("应采用卡密权威渠道: %s", hint)
	}
	if strings.Contains(hint, "夸克网盘") {
		t.Fatalf("描述里的夸克不应压过卡密渠道: %s", hint)
	}
	if !strings.Contains(hint, "按该商品卡密内容确定") || strings.Contains(hint, "仅参考") {
		t.Fatalf("卡密渠道应标注为可确定: %s", hint)
	}
}

// TestInferDeliveryHintsFallsBackToTextWhenCardHasNoChannel 卡券未识别出渠道时回落文案探测，
// 并保留「仅参考」依据，避免把推断冒充确定事实。
func TestInferDeliveryHintsFallsBackToTextWhenCardHasNoChannel(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是夹具写入上下文。
	ctx := context.Background()
	// ownerID 是测试账号所属用户。
	ownerID, err := s.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatalf("GetOwnerID: %v", err)
	}
	// cardID 是未识别出渠道的卡券（正文不含网盘线索）。
	cardID, err := s.Cards.Create(ctx, &db.CardFull{
		UserID: ownerID, Name: "通用组", Type: "data", DataContent: "code-1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建卡组失败: %v", err)
	}
	seedLiveItem(t, s, "fb-item", "资源合集", "12", "夸克网盘发货")
	// rule 是绑定该卡券的商品级发货规则。
	if _, err := s.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: ownerID, CookieID: "cid", ItemID: "fb-item", Name: "发货规则",
		TriggerType: "order_paid", Enabled: true,
		Actions: []db.AutomationActionInput{{
			ActionType: "send_card", CardID: cardID, ConfigJSON: `{}`, Enabled: true, SortOrder: 1,
		}},
	}); err != nil {
		t.Fatalf("创建规则失败: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// hint、err 是发货推断结果。
	hint, err := a.inferDeliveryHints(ctx, "fb-item")
	if err != nil {
		t.Fatalf("inferDeliveryHints: %v", err)
	}
	if !strings.Contains(hint, "夸克网盘") {
		t.Fatalf("应回落文案渠道: %s", hint)
	}
	if !strings.Contains(hint, "按描述/发货配置推断") || !strings.Contains(hint, "仅参考") {
		t.Fatalf("无卡密渠道时应标注仅参考: %s", hint)
	}
	if strings.Contains(hint, "卡密内容确定") {
		t.Fatalf("文案推断不应冒充确定事实: %s", hint)
	}
}
