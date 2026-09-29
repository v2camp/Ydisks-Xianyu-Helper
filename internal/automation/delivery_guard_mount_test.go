package automation

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// guardTestTask 返回门禁挂载测试共用的非敏感聊天身份。
func guardTestTask() Task {
	return Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer", OrderID: "guard-order", TriggerType: TriggerBuyerReviewed}
}

// TestSendTextBlockedByContentGuard 验证违禁内容在出站前被拦截：不发送且返回不可重试错误。
func TestSendTextBlockedByContentGuard(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// executor 是注入测试发送器的自动化动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// guardErr 保存门禁拦截错误。
	guardErr := executor.sendText(context.Background(), guardTestTask(), "加微信 13812345678 领取")
	if !isDeliveryGuardError(guardErr) || !errors.Is(guardErr, errContentBlocked) {
		t.Fatalf("违禁内容未被拦截: %v", guardErr)
	}
	if !errors.Is(guardErr, ErrMessageNotSent) || !strings.HasPrefix(guardErr.Error(), "[no_retry]") {
		t.Fatalf("门禁错误必须确定未发送且不可重试: %v", guardErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("命中门禁后仍发出消息: %v", sender.texts)
	}
	// urlErr 是非网盘外链的拦截结果。
	urlErr := executor.sendText(context.Background(), guardTestTask(), "下载 https://example.com/drop")
	if !isDeliveryGuardError(urlErr) || len(sender.texts) != 0 {
		t.Fatalf("非网盘外链未被拦截: %v texts=%v", urlErr, sender.texts)
	}
	// okErr 是干净网盘链接的发送结果，网盘交付链接必须放行。
	okErr := executor.sendText(context.Background(), guardTestTask(), "网盘 https://pan.baidu.com/s/1abc")
	if okErr != nil || len(sender.texts) != 1 {
		t.Fatalf("网盘链接被误拦: err=%v texts=%v", okErr, sender.texts)
	}
}

// TestSendTextBlockedByDeadLink 验证链接健康检查失败时拦截发送。
func TestSendTextBlockedByDeadLink(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// executor 是注入测试发送器的自动化动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// 断网传输层让任意链接探活都以网络错误收场。
	useStubLinkClient(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})})
	// linkErr 保存链接失效拦截错误。
	linkErr := executor.sendText(context.Background(), guardTestTask(), "资料 https://pan.baidu.com/s/dead")
	if !isDeliveryGuardError(linkErr) || !errors.Is(linkErr, errLinkDead) {
		t.Fatalf("失效链接未被拦截: %v", linkErr)
	}
	if !errors.Is(linkErr, ErrMessageNotSent) || !strings.HasPrefix(linkErr.Error(), "[no_retry]") || len(sender.texts) != 0 {
		t.Fatalf("失效链接错误语义错误: %v texts=%v", linkErr, sender.texts)
	}
}

// TestSendTextGuardSettingsToggles 验证设置开关、额外违禁词与可疑链接放行行为。
func TestSendTextGuardSettingsToggles(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是设置写入与发送共用的上下文。
	ctx := context.Background()
	// sender 记录放行后的消息。
	sender := &testSender{}
	// executor 是注入测试发送器的自动化动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// setErr 保存写入门禁配置的错误。
	if setErr := store.Settings.Set(ctx, deliveryContentGuardSettingKey, `{"enabled":false}`); setErr != nil {
		t.Fatal(setErr)
	}
	// offErr 是门禁关闭后的发送结果，违规文本必须放行。
	offErr := executor.sendText(ctx, guardTestTask(), "加微信 线下交易")
	if offErr != nil || len(sender.texts) != 1 {
		t.Fatalf("门禁关闭后仍被拦截: err=%v texts=%v", offErr, sender.texts)
	}
	// extraErr 保存写入额外违禁词配置的错误。
	if extraErr := store.Settings.Set(ctx, deliveryContentGuardSettingKey, `{"enabled":true,"extra_block_words":"内部码|禁发"}`); extraErr != nil {
		t.Fatal(extraErr)
	}
	// blockedExtra 是额外违禁词命中结果。
	blockedExtra := executor.sendText(ctx, guardTestTask(), "凭内部码兑换")
	if !isDeliveryGuardError(blockedExtra) || !strings.Contains(blockedExtra.Error(), "额外违禁词") {
		t.Fatalf("额外违禁词未命中: %v", blockedExtra)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("额外违禁词命中后仍发出消息: %v", sender.texts)
	}
	// linkOffErr 保存写入关闭链接检查配置的错误。
	if linkOffErr := store.Settings.Set(ctx, deliveryContentGuardSettingKey, `{"link_check":false}`); linkOffErr != nil {
		t.Fatal(linkOffErr)
	}
	// 断网传输层制造必然失效的链接；关闭链接检查后必须放行。
	useStubLinkClient(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})})
	// skipped 是关闭链接检查后的发送结果。
	skipped := executor.sendText(ctx, guardTestTask(), "网盘 https://pan.baidu.com/s/dead")
	if skipped != nil || len(sender.texts) != 2 {
		t.Fatalf("关闭链接检查后仍被拦截: err=%v texts=%v", skipped, sender.texts)
	}
}

// TestGuardOutboundTextSuspectLinkLogsOnly 验证可疑状态码只记日志不拦截。
func TestGuardOutboundTextSuspectLinkLogsOnly(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// executor 装配日志器，覆盖可疑链接的告警分支。
	executor := automationActionExecutor{store: store, logger: slog.Default()}
	// 断网传输层把链接探活判为不可达之外的场景由本用例单独注入可疑状态。
	useStubLinkClient(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		// response 返回 403 可疑状态码且没有响应体，模拟网盘反爬并覆盖空响应体收尾分支。
		response := &http.Response{StatusCode: http.StatusForbidden, Body: nil, Header: make(http.Header)}
		return response, nil
	})})
	// suspectErr 是可疑链接的门禁判定结果，必须放行。
	suspectErr := executor.guardOutboundText(context.Background(), "cid", "提取 https://pan.baidu.com/s/403")
	if suspectErr != nil {
		t.Fatalf("可疑链接不应拦截: %v", suspectErr)
	}
	// offCfg 验证整门禁关闭时连扫描都短路。
	if setErr := store.Settings.Set(context.Background(), deliveryContentGuardSettingKey, `{"enabled":false}`); setErr != nil {
		t.Fatal(setErr)
	}
	// disabledErr 是门禁关闭后的判定结果。
	disabledErr := executor.guardOutboundText(context.Background(), "cid", "加微信")
	if disabledErr != nil {
		t.Fatalf("门禁关闭仍被拦截: %v", disabledErr)
	}
}

// TestSendCardTextBlockedByGuard 验证卡密文本在发送前被门禁拦截。
func TestSendCardTextBlockedByGuard(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是卡密创建与执行共用的上下文。
	ctx := context.Background()
	// admin 是测试卡密的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// cardID、err 保存违规文本卡密的创建结果。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "违规卡", Type: "text", TextContent: "加微信 领卡", Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// executor 是注入测试发送器的自动化动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// sendErr 保存卡密发送结果，必须是门禁拦截错误。
	_, sendErr := executor.sendCard(ctx, guardTestTask(), db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: "{}"})
	if !isDeliveryGuardError(sendErr) || len(sender.texts) != 0 {
		t.Fatalf("卡密门禁未生效: err=%v texts=%v", sendErr, sender.texts)
	}
}

// TestSendTemplateBlockedByGuard 验证发货模板消息在发送前被门禁拦截且不进入结果未知分类。
func TestSendTemplateBlockedByGuard(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// executor 是注入测试发送器的自动化动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
	// result、sendErr 保存模板动作的执行结果。
	result, sendErr := executor.sendTemplate(context.Background(), guardTestTask(), db.AutomationAction{
		ActionType: ActionSendTemplate, ConfigJSON: "{}", TemplateMessages: []string{"第一条正常", "第二条加微信"},
	})
	if !isDeliveryGuardError(sendErr) {
		t.Fatalf("模板门禁未生效: %v", sendErr)
	}
	// uncertain 验证门禁错误没有被误包装为结果未知。
	var uncertain *uncertainActionError
	if errors.As(sendErr, &uncertain) {
		t.Fatalf("门禁错误被误判为结果未知: %v", sendErr)
	}
	if result.sent != 1 || len(sender.texts) != 1 || sender.texts[0] != "第一条正常" {
		t.Fatalf("模板发送进度错误: sent=%d texts=%v err=%v", result.sent, sender.texts, sendErr)
	}
}

// TestSendAPICardBlockedByGuard 验证 API 卡内容命中门禁时按确定未发送返回。
func TestSendAPICardBlockedByGuard(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是卡密创建与执行共用的上下文。
	ctx := context.Background()
	// admin 是测试 API 卡密的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// cardID、err 保存 API 卡密组的创建结果。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "API违规", Type: "api", APIConfig: `{"url":"https://example.com"}`, Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	// fetcher 返回注定命中门禁的 API 卡内容。
	fetcher := &guardAPIFetcher{content: "加V 兑换"}
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// executor 是注入 API 客户端与发送器的自动化动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}, apiFetcher: func() APICardFetcher { return fetcher }}
	// sendErr 保存 API 卡发送结果，必须保持门禁错误而不是结果未知。
	_, sendErr := executor.sendCard(ctx, guardTestTask(), db.AutomationAction{ID: 3, ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: "{}"})
	if !isDeliveryGuardError(sendErr) || len(sender.texts) != 0 {
		t.Fatalf("API 卡门禁未生效: err=%v texts=%v", sendErr, sender.texts)
	}
	// uncertain 验证门禁错误没有被误包装为结果未知。
	var uncertain *uncertainActionError
	if errors.As(sendErr, &uncertain) {
		t.Fatalf("门禁错误被误判为结果未知: %v", sendErr)
	}
}

// guardAPIFetcher 返回固定违规内容的 API 卡替身。
type guardAPIFetcher struct {
	// content 是每次取卡返回的违规文本。
	content string
}

// Fetch 返回固定违规内容，不发起真实网络请求。
func (f *guardAPIFetcher) Fetch(context.Context, APICardRequest) (APICardResult, error) {
	return APICardResult{Content: f.content}, nil
}

// TestActionSendTextRunQuarantinedOnGuardHit 验证门禁拦截后运行进入人工核对、发出通知且禁止自动重试。
func TestActionSendTextRunQuarantinedOnGuardHit(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是规则创建与执行共用的上下文。
	ctx := context.Background()
	// admin 是自动化规则的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleErr 保存创建违规文本规则时的数据库错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "guard-block", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "加微信 领取资料", Enabled: true}},
	}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// notifier 记录终态通知。
	notifier := &recordingNotifier{}
	// center 是注入发送器与通知器的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{Notifier: notifier})
	// runErr 保存门禁拦截后的运行结果错误。
	runErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "guard-order", ChatID: "chat", BuyerID: "buyer"})
	if !errors.Is(runErr, errAutomationNeedsReview) {
		t.Fatalf("门禁拦截应转人工核对: %v", runErr)
	}
	if !errors.Is(runErr, errContentBlocked) || !strings.Contains(runErr.Error(), "[no_retry]") {
		t.Fatalf("运行错误应携带不可重试的门禁原因: %v", runErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("命中门禁后仍发出消息: %v", sender.texts)
	}
	// status、message 保存运行终态与原因。
	var status, message string
	// queryErr 保存读取运行终态时的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,error_message FROM automation_runs WHERE order_id='guard-order'`).Scan(&status, &message); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" || !strings.Contains(message, "发货内容门禁拦截") {
		t.Fatalf("运行终态错误: status=%q message=%q", status, message)
	}
	// msgs 保存通知消息列表，必须出现人工核对通知。
	msgs := notifier.messages()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "needs_review") && !strings.Contains(msgs[0], "人工") {
		t.Fatalf("缺少人工核对通知: %v", msgs)
	}
	// callStatus 是通知调用记录里的终态编码。
	callStatus := ""
	// call 是当前通知调用记录。
	for _, call := range notifier.calls {
		callStatus = call.status
	}
	if callStatus != "needs_review" {
		t.Fatalf("通知终态应为 needs_review: %q", callStatus)
	}
	// runID 是被隔离运行的主键。
	var runID int64
	if // idErr 保存读取被隔离运行主键时的数据库错误。
	idErr := store.DB.QueryRowContext(ctx, `SELECT id FROM automation_runs WHERE order_id='guard-order'`).Scan(&runID); idErr != nil {
		t.Fatal(idErr)
	}
	// claimed 验证恢复队列不会重新领取门禁拦截的运行。
	claimed, claimErr := store.Automation.ClaimRecoveryRun(ctx, runID, 0)
	if claimErr != nil || claimed {
		t.Fatalf("门禁拦截运行被恢复队列领取: claimed=%v err=%v", claimed, claimErr)
	}
}

// TestActionSendTextRunPartialProofKeptOnGuardHit 验证前序已发内容在门禁拦截后仍保留人工补发凭证。
func TestActionSendTextRunPartialProofKeptOnGuardHit(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是规则创建与执行共用的上下文。
	ctx := context.Background()
	// admin 是自动化规则的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleErr 保存创建两步消息规则时的数据库错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "guard-partial", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendText, MessageTemplate: "第一条正常内容", Enabled: true, SortOrder: 1},
			{ActionType: ActionSendText, MessageTemplate: "第二条扫码领取", Enabled: true, SortOrder: 2},
		},
	}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// sender 记录已成功发出的消息。
	sender := &testSender{}
	// center 是注入发送器的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// runErr 保存第二条被门禁拦截后的运行错误。
	runErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "guard-partial-order", ChatID: "chat", BuyerID: "buyer"})
	if !errors.Is(runErr, errAutomationNeedsReview) || len(sender.texts) != 1 {
		t.Fatalf("部分发送后的门禁拦截处理错误: err=%v texts=%v", runErr, sender.texts)
	}
	// status、proof 保存运行终态与加密后的发货凭证。
	var status, proof string
	// queryErr 保存读取运行终态与凭证时的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,delivery_proof FROM automation_runs WHERE order_id='guard-partial-order'`).Scan(&status, &proof); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "needs_review" || proof == "" {
		t.Fatalf("已发内容凭证未保留: status=%q proof=%q", status, proof)
	}
}

// TestGuardBlockQuarantineFailureIsReturned 验证门禁拦截后隔离落库失败时错误完整上抛。
func TestGuardBlockQuarantineFailureIsReturned(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是规则创建与执行共用的上下文。
	ctx := context.Background()
	// admin 是自动化规则的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleErr 保存创建违规文本规则时的数据库错误。
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "guard-quarantine-fail", TriggerType: TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendText, MessageTemplate: "办证 高仿", Enabled: true}},
	}); ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// triggerErr 保存阻止 needs_review 状态写入的 SQLite 触发器创建错误。
	if _, triggerErr := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_needs_review
		BEFORE UPDATE OF status ON automation_runs
		WHEN NEW.status='needs_review'
		BEGIN SELECT RAISE(ABORT, 'forced quarantine failure'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// center 是注入发送器的自动化中心。
	center := New(store, testSenderProvider{sender: sender}, nil)
	// runErr 保存隔离失败后的聚合错误。
	runErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", TriggerType: TriggerBuyerReviewed, OrderID: "guard-quarantine-order", ChatID: "chat", BuyerID: "buyer"})
	if !errors.Is(runErr, errAutomationNeedsReview) || !errors.Is(runErr, errAutomationQuarantine) || !isDeliveryGuardError(runErr) {
		t.Fatalf("隔离失败错误链不完整: %v", runErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("隔离失败场景仍发出消息: %v", sender.texts)
	}
}

// TestFreeShipBargainStopsOnSootheGuardHit 验证安抚话术命中门禁时停止免拼且不发送。
func TestFreeShipBargainStopsOnSootheGuardHit(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是设置写入与执行共用的上下文。
	ctx := context.Background()
	// admin 是账号设置的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// template 是命中门禁的安抚模板。
	template := "加微信 有惊喜"
	// settingsErr 保存写入安抚模板时的数据库错误。
	if _, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, BargainSootheTemplate: &template}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// client 是若被错误调用即可暴露抢跑的 MTOP 替身。
	client := &fakeMTop{freeShippingOK: true}
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// center 是注入免拼替身与发送器的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: client})
	// freeShipErr 保存安抚话术被门禁拦截后的免拼结果。
	freeShipErr := center.actions.freeShipBargain(ctx, Task{AccountID: "cid", OrderID: "bargain-guard", ItemID: "item", BuyerID: "buyer", ChatID: "chat", IsBargain: true})
	if !isDeliveryGuardError(freeShipErr) {
		t.Fatalf("安抚话术门禁未生效: %v", freeShipErr)
	}
	if client.freeShippingCalls != 0 || len(sender.texts) != 0 {
		t.Fatalf("门禁拦截后仍执行免拼或发送: free=%d texts=%v", client.freeShippingCalls, sender.texts)
	}
}

// TestBargainPendingSootheGuardNeedsReview 验证待刀成阶段安抚话术门禁命中后阶段进入人工核对。
func TestBargainPendingSootheGuardNeedsReview(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是设置写入与任务处理共用的上下文。
	ctx := context.Background()
	// admin 是账号设置的所有者。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// enabled 是本测试显式开启的独立自动免拼开关。
	enabled := true
	// template 是命中门禁的安抚模板。
	template := "线下交易 优惠"
	// settingsErr 保存写入账号设置时的数据库错误。
	if _, settingsErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: admin.ID, AutoBargain: &enabled, BargainSootheTemplate: &template}); settingsErr != nil {
		t.Fatal(settingsErr)
	}
	// client 是若被错误调用即可暴露抢跑的 MTOP 替身。
	client := &fakeMTop{freeShippingOK: true}
	// sender 记录任何被误发出的消息。
	sender := &testSender{}
	// center 是注入免拼替身与发送器的自动化中心。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: client})
	// handleErr 保存待刀成任务的处理结果，门禁拦截应上抛。
	handleErr := center.HandleTask(ctx, Task{Source: "ws", AccountID: "cid", TriggerType: TriggerBargainPending, OrderID: "bargain-guard-stage", ItemID: "item", BuyerID: "buyer", ChatID: "chat"})
	if !isDeliveryGuardError(handleErr) {
		t.Fatalf("待刀成门禁拦截未上抛: %v", handleErr)
	}
	if client.freeShippingCalls != 0 || len(sender.texts) != 0 {
		t.Fatalf("门禁拦截后仍执行免拼或发送: free=%d texts=%v", client.freeShippingCalls, sender.texts)
	}
	// stageStatus 是免拼阶段终态。
	var stageStatus string
	// queryErr 保存读取阶段终态时的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status FROM bargain_free_shipping_stages WHERE order_id='bargain-guard-stage'`).Scan(&stageStatus); queryErr != nil {
		t.Fatal(queryErr)
	}
	if stageStatus != "needs_review" {
		t.Fatalf("免拼阶段应进入人工核对: %q", stageStatus)
	}
}

// TestLoadDeliveryGuardConfigDefaults 验证设置缺失与非法 JSON 时按默认启用门禁。
func TestLoadDeliveryGuardConfigDefaults(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是设置写入与读取共用的上下文。
	ctx := context.Background()
	// withStore 是装配了设置仓储的执行器。
	withStore := automationActionExecutor{store: store}
	// missing 是键缺失时的配置。
	missing := withStore.loadDeliveryGuardConfig(ctx)
	if !missing.Enabled || !missing.LinkCheck {
		t.Fatalf("键缺失应按默认启用: %+v", missing)
	}
	// setErr 保存写入非法 JSON 的错误（仓储层不校验，由应用层校验，这里模拟历史脏数据）。
	if setErr := store.Settings.Set(ctx, deliveryContentGuardSettingKey, "{bad json"); setErr != nil {
		t.Fatal(setErr)
	}
	// invalid 是非法 JSON 时的配置。
	invalid := withStore.loadDeliveryGuardConfig(ctx)
	if !invalid.Enabled || !invalid.LinkCheck {
		t.Fatalf("非法 JSON 应按默认启用: %+v", invalid)
	}
	// bare 是未装配仓储的执行器，必须按默认启用而不能放行。
	bare := automationActionExecutor{}
	// bareCfg 是未装配仓储时的配置。
	bareCfg := bare.loadDeliveryGuardConfig(ctx)
	if !bareCfg.Enabled || !bareCfg.LinkCheck {
		t.Fatalf("未装配仓储应按默认启用: %+v", bareCfg)
	}
	// dropErr 保存删除设置表的错误；用于模拟设置读取失败必须按默认启用。
	if _, dropErr := store.DB.ExecContext(ctx, `DROP TABLE system_settings`); dropErr != nil {
		t.Fatal(dropErr)
	}
	// failedRead 是设置读取失败时的配置，安全默认必须保持启用。
	failedRead := withStore.loadDeliveryGuardConfig(ctx)
	if !failedRead.Enabled || !failedRead.LinkCheck {
		t.Fatalf("设置读取失败应按默认启用: %+v", failedRead)
	}
}

// TestPersistedDeliveryProofIfAny 验证门禁隔离时凭证导出的三种边界。
func TestPersistedDeliveryProofIfAny(t *testing.T) {
	// nilWhenDisabled 是关闭持久化时的结果，必须为 nil。
	if persistedDeliveryProofIfAny(false, shipmentDeliveryProof{tradeText: "x"}) != nil {
		t.Fatal("关闭持久化时不应导出凭证")
	}
	// nilWhenEmpty 是空凭证的结果，必须为 nil。
	if persistedDeliveryProofIfAny(true, shipmentDeliveryProof{}) != nil {
		t.Fatal("空凭证不应导出")
	}
	// exported 是非空凭证的导出结果。
	exported := persistedDeliveryProofIfAny(true, shipmentDeliveryProof{tradeText: "卡密A", expectedUnits: 2, preparedUnits: 1})
	if exported == nil || exported.TradeText != "卡密A" || exported.ExpectedUnits != 2 {
		t.Fatalf("凭证导出错误: %+v", exported)
	}
	// refillOnly 是只有补取占用标记的凭证导出结果，也必须保留。
	refillOnly := persistedDeliveryProofIfAny(true, shipmentDeliveryProof{refillPending: true})
	if refillOnly == nil || !refillOnly.RefillPending {
		t.Fatalf("补取占用标记未保留: %+v", refillOnly)
	}
}
