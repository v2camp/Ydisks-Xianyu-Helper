package qqbot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"golang.org/x/oauth2"
)

// c2cEventPayload 是官方单聊消息事件体样本，author.user_openid 为关键字段。
const c2cEventPayload = `{"op":0,"s":3,"t":"C2C_MESSAGE_CREATE","id":"event-1","d":{"id":"msg-1","content":"销量","author":{"id":"user-openid-1","user_openid":"user-openid-1"}}}`

// groupEventPayload 是官方群 @ 消息事件体样本，含 group_openid 与 author.member_openid。
const groupEventPayload = `{"op":0,"s":4,"t":"GROUP_AT_MESSAGE_CREATE","id":"event-2","d":{"id":"msg-2","content":"<@!bot> 健康","group_openid":"group-openid-1","author":{"id":"member-id","member_openid":"member-openid-1"}}}`

// replierFake 是被动回复端口的测试替身，记录最近一次调用的参数。
type replierFake struct {
	// c2cOpenID 记录单聊回复的目标标识。
	c2cOpenID string
	// c2cContent 记录单聊回复的文本。
	c2cContent string
	// c2cMsgID 记录单聊回复回传的消息标识。
	c2cMsgID string
	// groupOpenID 记录群聊回复的目标标识。
	groupOpenID string
	// groupContent 记录群聊回复的文本。
	groupContent string
	// groupMsgID 记录群聊回复回传的消息标识。
	groupMsgID string
	// err 是预设返回的回复错误。
	err error
}

// ReplyC2C 记录单聊回复参数。
func (f *replierFake) ReplyC2C(_ context.Context, openID, content, msgID string) error {
	f.c2cOpenID, f.c2cContent, f.c2cMsgID = openID, content, msgID
	return f.err
}

// ReplyGroup 记录群聊回复参数。
func (f *replierFake) ReplyGroup(_ context.Context, openID, content, msgID string) error {
	f.groupOpenID, f.groupContent, f.groupMsgID = openID, content, msgID
	return f.err
}

// gatewayClientFake 是网关客户端的测试替身。
type gatewayClientFake struct {
	// replierFake 复用回复替身。
	*replierFake
	// fetchErr 是预设的网关地址获取错误。
	fetchErr error
}

// FetchGateway 返回预设的网关地址获取结果。
func (f *gatewayClientFake) FetchGateway(_ context.Context) (*dto.WebsocketAP, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return &dto.WebsocketAP{}, nil
}

// TokenSource 返回空令牌源，测试路径不会真正发起鉴权。
func (f *gatewayClientFake) TokenSource() oauth2.TokenSource { return nil }

// TestParseInboundMessageC2C 验证单聊事件体能还原消息标识、内容与发送者 openid。
func TestParseInboundMessageC2C(t *testing.T) {
	// got 是解析出的入站消息。
	got, err := ParseInboundMessage([]byte(c2cEventPayload))
	if err != nil {
		t.Fatalf("解析单聊事件失败: %v", err)
	}
	if got.MsgID != "msg-1" || got.Content != "销量" || got.SenderOpenID != "user-openid-1" || got.GroupOpenID != "" {
		t.Fatalf("单聊解析结果不符，实际=%+v", got)
	}
}

// TestParseInboundMessageGroupKeepsGroupOpenID 验证群事件体的 group_openid 不被丢失。
func TestParseInboundMessageGroupKeepsGroupOpenID(t *testing.T) {
	// got 是解析出的入站消息。
	got, err := ParseInboundMessage([]byte(groupEventPayload))
	if err != nil {
		t.Fatalf("解析群事件失败: %v", err)
	}
	if got.GroupOpenID != "group-openid-1" {
		t.Fatalf("群 openid 丢失，实际=%q", got.GroupOpenID)
	}
	if got.SenderOpenID != "member-openid-1" {
		t.Fatalf("群场景应取 member_openid，实际=%q", got.SenderOpenID)
	}
	if got.MsgID != "msg-2" {
		t.Fatalf("群消息标识不符，实际=%q", got.MsgID)
	}
}

// TestParseInboundMessageFallsBackToAuthorID 验证 openid 缺失时回退到通用标识。
func TestParseInboundMessageFallsBackToAuthorID(t *testing.T) {
	// payload 是 author 只含通用标识的事件体。
	payload := `{"t":"C2C_MESSAGE_CREATE","d":{"id":"msg-3","content":"帮助","author":{"id":"fallback-id"}}}`
	// got 是解析出的入站消息。
	got, err := ParseInboundMessage([]byte(payload))
	if err != nil {
		t.Fatalf("解析事件失败: %v", err)
	}
	if got.SenderOpenID != "fallback-id" {
		t.Fatalf("应回退到通用标识，实际=%q", got.SenderOpenID)
	}
}

// TestParseInboundMessageErrors 验证空报文、非法 JSON 与缺负载都返回错误。
func TestParseInboundMessageErrors(t *testing.T) {
	// cases 是应判定为解析失败的输入。
	cases := map[string]string{
		"空报文":    "",
		"非法JSON": `{"d":`,
		"缺负载":    `{"t":"C2C_MESSAGE_CREATE"}`,
	}
	// name 是用例名称。
	// payload 是当前待验证的报文。
	for name, payload := range cases {
		// err 是解析返回的错误。
		if _, err := ParseInboundMessage([]byte(payload)); err == nil {
			t.Fatalf("%s 应返回解析错误", name)
		}
	}
}

// TestRunRejectsInvalidSetup 验证缺少上下文、服务或凭据时 Run 直接失败。
func TestRunRejectsInvalidSetup(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, _, _ := newTestService()
	// cases 是应判定为启动失败的输入。
	cases := map[string]func() error{
		"空上下文":/* 未注入生命周期上下文不得启动。 */ func() error { return NewGateway("id", "secret", service).Run(nil) },
		"缺服务":/* 未装配命令服务不得启动。 */ func() error { return NewGateway("id", "secret", nil).Run(context.Background()) },
		"缺凭据":/* 未配置 AppID/AppSecret 不得启动。 */ func() error { return NewGateway("", "", service).Run(context.Background()) },
	}
	// name 是用例名称。
	// run 是当前待验证的启动函数。
	for name, run := range cases {
		// runErr 是启动返回的错误。
		if runErr := run(); runErr == nil {
			t.Fatalf("%s 应返回启动错误", name)
		}
	}
}

// TestRunRepliesC2CWithCommandResult 验证单聊命令全链路：解析 → 执行 → 被动回复且回传 msg_id。
func TestRunRepliesC2CWithCommandResult(t *testing.T) {
	// service 是注入替身的命令服务。
	service, sales, _, _ := newTestService()
	sales.snapshot = SalesSnapshot{Date: "2026-10-08", Accounts: []AccountSales{{AccountID: "a", AccountName: "主力号", Orders: 2, AmountFen: 9900}}, TotalOrders: 2, AmountFen: 9900}
	// replier 是回复替身。
	replier := &replierFake{}
	// gateway 是待测入站网关。
	gateway := runGatewayWithFake(t, service, replier, c2cEventPayload)
	if gateway == nil {
		t.Fatal("网关未启动")
	}
	if replier.c2cOpenID != "user-openid-1" {
		t.Fatalf("单聊应回复发送者 openid，实际=%q", replier.c2cOpenID)
	}
	if !strings.Contains(replier.c2cContent, "· 主力号：2 单 ¥99.00") {
		t.Fatalf("单聊回复内容不符，实际=%q", replier.c2cContent)
	}
	if replier.c2cMsgID != "msg-1" {
		t.Fatalf("被动回复必须回传 msg_id，实际=%q", replier.c2cMsgID)
	}
	if replier.groupOpenID != "" {
		t.Fatalf("单聊场景不应走群聊通道，实际=%q", replier.groupOpenID)
	}
}

// TestRunRepliesGroupWhenGroupOpenIDPresent 验证群场景回复到群且剥离 @ 提及前缀。
func TestRunRepliesGroupWhenGroupOpenIDPresent(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, health, _ := newTestService()
	health.snapshot = HealthSnapshot{DatabaseOK: true, SilenceMinutes: 1, SilenceThreshold: 180}
	// replier 是回复替身。
	replier := &replierFake{}
	// gateway 是待测入站网关。
	gateway := runGatewayWithFake(t, service, replier, groupEventPayload)
	if gateway == nil {
		t.Fatal("网关未启动")
	}
	if replier.groupOpenID != "group-openid-1" {
		t.Fatalf("群场景应回复群 openid，实际=%q", replier.groupOpenID)
	}
	if replier.groupMsgID != "msg-2" {
		t.Fatalf("群被动回复必须回传 msg_id，实际=%q", replier.groupMsgID)
	}
	if !strings.Contains(replier.groupContent, "数据库：正常") {
		t.Fatalf("群回复内容不符（@ 前缀应已剥离），实际=%q", replier.groupContent)
	}
}

// TestRunSilentWhenCommandsDisabled 验证开关关闭时不回复任何内容。
func TestRunSilentWhenCommandsDisabled(t *testing.T) {
	// service 是开关关闭的命令服务。
	service, _, _, _ := newTestService()
	service.authorizer = &authorizerFake{enabled: false, allowed: true}
	// replier 是回复替身。
	replier := &replierFake{}
	runGatewayWithFake(t, service, replier, c2cEventPayload)
	if replier.c2cOpenID != "" || replier.groupOpenID != "" {
		t.Fatalf("开关关闭时不应回复，实际 c2c=%q group=%q", replier.c2cOpenID, replier.groupOpenID)
	}
}

// TestRunRepliesUnauthorizedNotice 验证未授权发送者收到含其 openid 的提示。
func TestRunRepliesUnauthorizedNotice(t *testing.T) {
	// service 是白名单不命中的命令服务。
	service, _, _, _ := newTestService()
	service.authorizer = &authorizerFake{enabled: true, allowed: false}
	// replier 是回复替身。
	replier := &replierFake{}
	runGatewayWithFake(t, service, replier, c2cEventPayload)
	if !strings.Contains(replier.c2cContent, "未授权") || !strings.Contains(replier.c2cContent, "user-openid-1") {
		t.Fatalf("未授权应回复含 openid 的提示，实际=%q", replier.c2cContent)
	}
}

// TestRunRepliesFailureNotice 验证命令执行失败时回复兜底文案且不泄露细节。
func TestRunRepliesFailureNotice(t *testing.T) {
	// service 是销量读取失败的命令服务。
	service, sales, _, _ := newTestService()
	sales.err = errors.New("数据库连接失败")
	// replier 是回复替身。
	replier := &replierFake{}
	runGatewayWithFake(t, service, replier, c2cEventPayload)
	if replier.c2cContent != FormatFailure() {
		t.Fatalf("执行失败应回复兜底文案，实际=%q", replier.c2cContent)
	}
}

// TestRunPropagatesSessionError 验证会话启动错误被原样透出。
func TestRunPropagatesSessionError(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, _, _ := newTestService()
	// originalClient 是原工厂，用于用例结束后还原。
	originalClient := newGatewayClient
	// originalStart 是原会话启动函数，用于用例结束后还原。
	originalStart := startInboundSession
	defer func() { newGatewayClient, startInboundSession = originalClient, originalStart }()
	newGatewayClient = func(_ context.Context, _, _ string) (gatewayClient, error) {
		return &gatewayClientFake{replierFake: &replierFake{}}, nil
	}
	startInboundSession = func(_ context.Context, _ gatewayClient, _ ...any) error { return errors.New("网关连接失败") }
	// err 是启动返回的错误。
	if err := NewGateway("id", "secret", service).Run(context.Background()); err == nil || !strings.Contains(err.Error(), "网关连接失败") {
		t.Fatalf("会话启动错误应被透出，实际=%v", err)
	}
}

// runGatewayWithFake 用替身客户端跑一次网关，并把指定事件体分发给已注册的处理器。
// 返回已启动的网关；事件分发失败会直接标记测试失败。
func runGatewayWithFake(t *testing.T, service *Service, replier *replierFake, payload string) *Gateway {
	t.Helper()
	// originalClient 是原工厂，用于用例结束后还原。
	originalClient := newGatewayClient
	// originalStart 是原会话启动函数，用于用例结束后还原。
	originalStart := startInboundSession
	defer func() { newGatewayClient, startInboundSession = originalClient, originalStart }()
	newGatewayClient = func(_ context.Context, _, _ string) (gatewayClient, error) {
		return &gatewayClientFake{replierFake: replier}, nil
	}
	// dispatchErr 保存事件分发过程中的首个错误。
	var dispatchErr error
	startInboundSession = func(_ context.Context, _ gatewayClient, handlers ...any) error {
		// handler 是当前注册的事件处理器。
		for _, handler := range handlers {
			// payloadStruct 是待分发的事件对象。
			payloadStruct := &dto.WSPayload{RawMessage: []byte(payload)}
			// typed 是按 botgo 具名 handler 类型断言后的处理器。
			switch typed := handler.(type) {
			case event.C2CMessageEventHandler:
				// callErr 是单聊处理器返回的错误。
				if callErr := typed(payloadStruct, nil); callErr != nil && dispatchErr == nil {
					dispatchErr = callErr
				}
			case event.GroupATMessageEventHandler:
				// callErr 是群处理器返回的错误。
				if callErr := typed(payloadStruct, nil); callErr != nil && dispatchErr == nil {
					dispatchErr = callErr
				}
			}
		}
		return nil
	}
	// gateway 是待测入站网关。
	gateway := NewGateway("id", "secret", service)
	// runErr 是网关启动错误。
	if runErr := gateway.Run(context.Background()); runErr != nil {
		t.Fatalf("网关启动失败: %v", runErr)
	}
	if dispatchErr != nil {
		t.Fatalf("事件处理失败: %v", dispatchErr)
	}
	return gateway
}

// TestInboundMessageJSONShapeMatchesOfficialSamples 锁定事件体字段契约，防止结构漂移。
func TestInboundMessageJSONShapeMatchesOfficialSamples(t *testing.T) {
	// samples 是官方两种事件体样本。
	samples := []string{c2cEventPayload, groupEventPayload}
	// sample 是当前待验证的事件体。
	for _, sample := range samples {
		// envelope 用于校验外层字段存在。
		var envelope map[string]any
		if // err 是解析错误
		err := json.Unmarshal([]byte(sample), &envelope); err != nil {
			t.Fatalf("样本不是合法 JSON: %v", err)
		}
		// data 是事件负载。
		data, ok := envelope["d"].(map[string]any)
		if !ok {
			t.Fatal("样本缺少事件负载 d")
		}
		// exists 表示事件负载是否携带消息标识字段。
		if _, exists := data["id"]; !exists {
			t.Fatal("事件负载缺少消息标识 id")
		}
	}
}
