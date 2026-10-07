package qqbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"
)

// inboundReplyTimeout 是单次入站命令回复的最长等待时间。
const inboundReplyTimeout = 15 * time.Second

// ErrGatewayNotConfigured 表示入站网关缺少 AppID 或 AppSecret，无法连接。
var ErrGatewayNotConfigured = errors.New("qq 入站网关缺少 app_id/app_secret")

// InboundMessage 是命令服务所需的入站消息要素，字段取自官方 v2 事件体。
type InboundMessage struct {
	// MsgID 是消息标识，被动回复时必须回传，否则会占用主动消息配额。
	MsgID string
	// Content 是用户输入的原始文本。
	Content string
	// SenderOpenID 是发送者标识，用于单聊场景回复与白名单校验。
	SenderOpenID string
	// GroupOpenID 是群聊场景下的群标识；单聊场景为空。
	GroupOpenID string
}

// inboundEnvelope 是 WS 事件体的外层结构，用于从原始报文还原被 SDK 丢弃的 openid。
type inboundEnvelope struct {
	// Data 是事件负载，按事件类型不同承载不同结构。
	Data json.RawMessage `json:"d"`
}

// inboundMessageBody 是消息类事件负载中与本项目相关的字段。
type inboundMessageBody struct {
	// ID 是消息标识。
	ID string `json:"id"`
	// Content 是用户输入的原始文本。
	Content string `json:"content"`
	// GroupOpenID 是群聊场景下的群标识，botgo 的 dto.Message 未包含该字段。
	GroupOpenID string `json:"group_openid"`
	// Author 是发送者信息。
	Author inboundAuthor `json:"author"`
}

// inboundAuthor 是消息发送者信息，openid 字段同样未被 botgo 的 dto.User 覆盖。
type inboundAuthor struct {
	// ID 是通用标识；单聊场景下与 user_openid 取值相同。
	ID string `json:"id"`
	// UserOpenID 是单聊场景的发送者 openid。
	UserOpenID string `json:"user_openid"`
	// MemberOpenID 是群聊场景的发送者 openid。
	MemberOpenID string `json:"member_openid"`
}

// ParseInboundMessage 从原始事件报文还原入站消息要素。
// botgo v0.2.1 的 dto.Message 与 dto.User 缺少 group_openid / user_openid / member_openid，
// 直接依赖 SDK 结构会导致 openid 被静默丢弃，因此必须从原始报文自行解析。
func ParseInboundMessage(raw []byte) (InboundMessage, error) {
	if len(raw) == 0 {
		return InboundMessage{}, errors.New("qq 入站事件报文为空")
	}
	// envelope 是事件外层结构，只关心事件负载。
	var envelope inboundEnvelope
	if // unmarshalErr 用于本次流程后续判断的解析错误
	unmarshalErr := json.Unmarshal(raw, &envelope); unmarshalErr != nil {
		return InboundMessage{}, fmt.Errorf("解析 qq 入站事件外层失败: %w", unmarshalErr)
	}
	if len(envelope.Data) == 0 {
		return InboundMessage{}, errors.New("qq 入站事件缺少事件负载")
	}
	// body 是消息事件负载。
	var body inboundMessageBody
	if // unmarshalErr 用于本次流程后续判断的解析错误
	unmarshalErr := json.Unmarshal(envelope.Data, &body); unmarshalErr != nil {
		return InboundMessage{}, fmt.Errorf("解析 qq 入站消息负载失败: %w", unmarshalErr)
	}
	// sender 是发送者标识，群聊优先取 member_openid，单聊取 user_openid，最后回退通用标识。
	sender := firstNonEmpty(body.Author.MemberOpenID, body.Author.UserOpenID, body.Author.ID)
	return InboundMessage{
		MsgID:        strings.TrimSpace(body.ID),
		Content:      body.Content,
		SenderOpenID: strings.TrimSpace(sender),
		GroupOpenID:  strings.TrimSpace(body.GroupOpenID),
	}, nil
}

// firstNonEmpty 返回首个非空且去空白后仍有内容的候选值；全部为空时返回空串。
func firstNonEmpty(candidates ...string) string {
	// candidate 是当前待判断的候选值。
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

// Replier 定义入站命令的被动回复能力。
type Replier interface {
	// ReplyC2C 被动回复单聊消息；msgID 非空即走被动通道，不占用每月 4 条的主动配额。
	ReplyC2C(ctx context.Context, openID, content, msgID string) error
	// ReplyGroup 被动回复群聊消息；msgID 非空即走被动通道。
	ReplyGroup(ctx context.Context, openID, content, msgID string) error
}

// gatewayClient 是入站网关所需的 botgo 能力抽象，便于隔离测试注入替身。
type gatewayClient interface {
	// Replier 复用被动回复能力。
	Replier
	// FetchGateway 获取 WS 网关地址信息。
	FetchGateway(ctx context.Context) (*dto.WebsocketAP, error)
	// TokenSource 返回网关鉴权使用的令牌源。
	TokenSource() oauth2.TokenSource
}

// newGatewayClient 是构造入站网关客户端的工厂；生产实现走 botgo，测试可替换为替身。
// ctx 是 Access Token 刷新协程继承的进程生命周期根 Context，禁止传入裸 Background。
var newGatewayClient = func(ctx context.Context, appID, appSecret string) (gatewayClient, error) {
	return newBotgoGatewayClient(ctx, appID, appSecret)
}

// startInboundSession 注册事件处理器并启动 WS 会话，阻塞直到连接断开或进程关闭。
// ctx 是获取网关地址与维持会话使用的进程生命周期上下文。
// 抽象为变量是为了让网关逻辑可在不触网的前提下被测试覆盖。
var startInboundSession = func(ctx context.Context, client gatewayClient, handlers ...any) error {
	// ap 是平台下发的网关地址信息；fetchErr 表示获取失败。
	ap, fetchErr := client.FetchGateway(ctx)
	if fetchErr != nil {
		return fmt.Errorf("获取 QQ 网关地址失败: %w", fetchErr)
	}
	// intent 是按已注册处理器计算出的订阅位，覆盖 C2C 与群 @ 消息。
	intent := event.RegisterHandlers(handlers...)
	return botgo.NewSessionManager().Start(ap, client.TokenSource(), &intent)
}

// botgoGatewayClient 用官方 botgo SDK 实现网关地址获取与消息被动回复。
type botgoGatewayClient struct {
	// api 是 botgo 的 openapi 实例，内部按 oauth2 TokenSource 自动刷新 Access Token。
	api openapi.OpenAPI
	// tokenSource 是网关鉴权与接口调用共用的令牌源。
	tokenSource oauth2.TokenSource
}

// newBotgoGatewayClient 用 AppID 与 AppSecret 构造网关客户端并启动 Access Token 自动刷新。
// ctx 是刷新协程继承的进程生命周期根 Context，随进程关闭取消。
func newBotgoGatewayClient(ctx context.Context, appID, appSecret string) (gatewayClient, error) {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return nil, ErrGatewayNotConfigured
	}
	// credential 保存用于换取 Access Token 的机器人凭据，不属于敏感明文日志输出范围。
	credential := &token.QQBotCredentials{AppID: strings.TrimSpace(appID), AppSecret: strings.TrimSpace(appSecret)}
	// tokenSource 基于凭据构造 oauth2 TokenSource，botgo 会在请求前自动刷新 Access Token。
	tokenSource := token.NewQQBotTokenSource(credential)
	if // err 用于本次流程后续判断的err
	err := token.StartRefreshAccessToken(ctx, tokenSource); err != nil {
		return nil, fmt.Errorf("启动 QQ 入站 Access Token 刷新失败: %w", err)
	}
	return &botgoGatewayClient{api: botgo.NewOpenAPI(credential.AppID, tokenSource), tokenSource: tokenSource}, nil
}

// FetchGateway 获取平台下发的 WS 网关地址信息。
func (c *botgoGatewayClient) FetchGateway(ctx context.Context) (*dto.WebsocketAP, error) {
	if c == nil || c.api == nil {
		return nil, errors.New("qq 入站网关客户端未初始化")
	}
	return c.api.WS(ctx, nil, "")
}

// TokenSource 返回网关鉴权使用的令牌源。
func (c *botgoGatewayClient) TokenSource() oauth2.TokenSource {
	return c.tokenSource
}

// ReplyC2C 通过 botgo 被动回复单聊消息。
func (c *botgoGatewayClient) ReplyC2C(ctx context.Context, openID, content, msgID string) error {
	return c.reply(ctx, openID, content, msgID, false)
}

// ReplyGroup 通过 botgo 被动回复群聊消息。
func (c *botgoGatewayClient) ReplyGroup(ctx context.Context, openID, content, msgID string) error {
	return c.reply(ctx, openID, content, msgID, true)
}

// reply 统一封装 botgo 的被动回复调用；group 为真时走群聊，否则走单聊。
func (c *botgoGatewayClient) reply(ctx context.Context, openID, content, msgID string, group bool) error {
	if c == nil || c.api == nil {
		return errors.New("qq 入站网关客户端未初始化")
	}
	if strings.TrimSpace(openID) == "" {
		return errors.New("qq 入站回复目标 openid 为空")
	}
	// msg 是 api-v2 纯文本消息体；MsgID 非空即被动消息，不占用主动配额。
	msg := dto.MessageToCreate{Content: content, MsgType: dto.TextMsg, MsgID: strings.TrimSpace(msgID)}
	if group {
		// groupErr 用于本次流程后续判断的groupErr
		_, groupErr := c.api.PostGroupMessage(ctx, openID, msg)
		return groupErr
	}
	// c2cErr 用于本次流程后续判断的c2cErr
	_, c2cErr := c.api.PostC2CMessage(ctx, openID, msg)
	return c2cErr
}

// Gateway 连接 QQ 机器人 WS 网关，把入站消息交给命令服务并被动回复结果。
type Gateway struct {
	// appID 是机器人接入标识。
	appID string
	// appSecret 是机器人接入密钥，禁止写入日志。
	appSecret string
	// service 是被调用的入站命令服务。
	service *Service
	// replier 是被动回复客户端；由 Run 用真实客户端填充，测试可预先注入替身。
	replier Replier
	// lifecycleCtx 是进程生命周期上下文，由 Run 注入供事件处理继承。
	lifecycleCtx context.Context
}

// NewGateway 构造入站网关；service 为 nil 时 Run 会直接返回错误。
func NewGateway(appID, appSecret string, service *Service) *Gateway {
	return &Gateway{appID: strings.TrimSpace(appID), appSecret: strings.TrimSpace(appSecret), service: service}
}

// Run 连接 WS 网关并阻塞消费事件，ctx 取消即退出。
// ctx 必须是进程生命周期上下文，禁止传入裸 Background，以满足架构门禁的后台组件根 Context 约束。
func (g *Gateway) Run(ctx context.Context) error {
	if g == nil {
		return errors.New("qq 入站网关未初始化")
	}
	if ctx == nil {
		return errors.New("qq 入站网关缺少进程生命周期上下文")
	}
	if g.service == nil {
		return errors.New("qq 入站网关缺少命令服务")
	}
	if g.appID == "" || g.appSecret == "" {
		return ErrGatewayNotConfigured
	}
	g.lifecycleCtx = ctx
	// client 是网关客户端；err 表示凭据无效或刷新链路启动失败。
	client, err := newGatewayClient(ctx, g.appID, g.appSecret)
	if err != nil {
		return err
	}
	g.replier = client
	// 处理器必须转成 botgo 的具名 handler 类型，RegisterHandlers 才能识别并算出订阅位。
	return startInboundSession(ctx, client,
		event.C2CMessageEventHandler(g.onC2CMessage),
		event.GroupATMessageEventHandler(g.onGroupATMessage),
	)
}

// onC2CMessage 处理单聊消息事件。
func (g *Gateway) onC2CMessage(event *dto.WSPayload, _ *dto.WSC2CMessageData) error {
	return g.handleEvent(event)
}

// onGroupATMessage 处理群聊 @ 机器人消息事件。
func (g *Gateway) onGroupATMessage(event *dto.WSPayload, _ *dto.WSGroupATMessageData) error {
	return g.handleEvent(event)
}

// handleEvent 把单个事件交给命令服务并把结果被动回复给发送者。
func (g *Gateway) handleEvent(event *dto.WSPayload) error {
	if event == nil {
		return errors.New("qq 入站事件为空")
	}
	if g.lifecycleCtx == nil {
		return errors.New("qq 入站网关未注入进程生命周期上下文")
	}
	// message 是从原始报文还原的入站消息；parseErr 表示报文不可解析。
	message, parseErr := ParseInboundMessage(event.RawMessage)
	if parseErr != nil {
		return parseErr
	}
	// reply 是命令服务产出的回复文本；err 表示开关关闭、未授权或执行失败。
	reply, err := g.service.Handle(g.lifecycleCtx, message.SenderOpenID, message.Content)
	switch {
	case err == nil:
		return g.replyMessage(message, reply)
	case errors.Is(err, ErrCommandsDisabled):
		// 开关关闭时不回复任何内容，避免向无关发送者暴露机器人存在。
		return nil
	case errors.Is(err, ErrUnauthorized):
		return g.replyMessage(message, FormatUnauthorized(message.SenderOpenID))
	default:
		return g.replyMessage(message, FormatFailure())
	}
}

// replyMessage 依据入站场景选择单聊或群聊通道被动回复。
func (g *Gateway) replyMessage(message InboundMessage, content string) error {
	if g.replier == nil {
		return errors.New("qq 入站网关缺少回复客户端")
	}
	// replyCtx、cancel 为单次回复提供有界取消路径，继承自进程生命周期上下文。
	replyCtx, cancel := context.WithTimeout(g.lifecycleCtx, inboundReplyTimeout)
	defer cancel()
	if message.GroupOpenID != "" {
		// groupErr 用于本次流程后续判断的群聊回复错误
		if groupErr := g.replier.ReplyGroup(replyCtx, message.GroupOpenID, content, message.MsgID); groupErr != nil {
			return fmt.Errorf("qq 群聊被动回复失败: %w", groupErr)
		}
		return nil
	}
	if message.SenderOpenID == "" {
		return errors.New("qq 入站回复目标为空")
	}
	// c2cErr 用于本次流程后续判断的单聊回复错误
	if c2cErr := g.replier.ReplyC2C(replyCtx, message.SenderOpenID, content, message.MsgID); c2cErr != nil {
		return fmt.Errorf("qq 单聊被动回复失败: %w", c2cErr)
	}
	return nil
}
