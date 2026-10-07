package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
)

// qqSendTimeout 是单次 QQ 机器人消息发送的最长等待时间，覆盖 Access Token 换取与消息投递。
const qqSendTimeout = 15 * time.Second

// qqRefreshBudget 是未注入进程生命周期上下文时，QQ 令牌刷新协程回退使用的有限收口预算。
const qqRefreshBudget = 24 * time.Hour

// refreshContext 返回 Access Token 刷新协程的根 Context：优先复用进程生命周期上下文，
// 未启动通知器时按实例惰性创建带有限预算的兜底上下文，避免裸 Background 无法随关闭回收。
func (n *Notifier) refreshContext() context.Context {
	if n.lifecycleCtx != nil {
		return n.lifecycleCtx
	}
	n.qqMu.Lock()
	defer n.qqMu.Unlock()
	if n.qqRefreshCtx == nil {
		// createdCtx、createdCancel 保存本次惰性创建的兜底上下文及其取消句柄。
		createdCtx, createdCancel := context.WithTimeout(context.Background(), qqRefreshBudget)
		n.qqRefreshCtx, n.qqRefreshCancel = createdCtx, createdCancel
	}
	return n.qqRefreshCtx
}

// qqBotClient 是 QQ 机器人 api-v2 的发送能力抽象，便于隔离测试注入替身。
type qqBotClient interface {
	// SendC2CMessage 向单聊用户发送一条纯文本消息；userOpenID 是与当前 AppID 绑定的用户 openid。
	SendC2CMessage(ctx context.Context, userOpenID, content string) error
	// SendGroupMessage 向群聊发送一条纯文本消息；groupOpenID 是与当前 AppID 绑定的群 openid。
	SendGroupMessage(ctx context.Context, groupOpenID, content string) error
}

// newQQBotClient 是构造 QQ 机器人客户端的工厂；生产实现走 botgo，测试可替换为替身。
// ctx 是 Access Token 刷新协程继承的进程生命周期根 Context，禁止传入裸 Background。
var newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
	return newBotgoQQClient(ctx, appID, appSecret)
}

// botgoQQClient 用官方 botgo SDK 实现 api-v2 单聊与群聊发送。
type botgoQQClient struct {
	// api 是 botgo 的 openapi 实例，内部按 oauth2 TokenSource 自动刷新 Access Token。
	api openapi.OpenAPI
}

// newBotgoQQClient 用 AppID 与 AppSecret 构造 botgo 客户端并启动 Access Token 自动刷新。
// 返回的客户端持有后台刷新协程；归属为进程级，进程退出时由运行时统一回收。
// ctx 是刷新协程继承的进程生命周期根 Context，随进程关闭取消。
func newBotgoQQClient(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return nil, fmt.Errorf("qq app_id/app_secret 不完整")
	}
	// credential 保存用于换取 Access Token 的机器人凭据，不属于敏感明文日志输出范围。
	credential := &token.QQBotCredentials{AppID: strings.TrimSpace(appID), AppSecret: strings.TrimSpace(appSecret)}
	// tokenSource 基于凭据构造 oauth2 TokenSource，botgo 会在请求前自动刷新 Access Token。
	tokenSource := token.NewQQBotTokenSource(credential)
	if // err 用于本次流程后续判断的err
	err := token.StartRefreshAccessToken(ctx, tokenSource); err != nil {
		return nil, fmt.Errorf("启动 QQ 机器人 Access Token 刷新失败: %w", err)
	}
	return &botgoQQClient{api: botgo.NewOpenAPI(credential.AppID, tokenSource)}, nil
}

// sendQQ 依据渠道配置把通知发送到 QQ 机器人单聊或群聊。
// 目标优先使用 user_openid（单聊），其次 group_openid（群聊）；两者都为空时返回配置错误。
func (n *Notifier) sendQQ(cfg map[string]any, message string) error {
	// appID 是机器人接入标识，来自渠道加密配置，禁止写入日志。
	appID := strOr(cfg, "app_id", "")
	// appSecret 是机器人接入密钥，来自渠道加密配置，禁止写入日志。
	appSecret := strOr(cfg, "app_secret", "")
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return fmt.Errorf("qq app_id/app_secret 不完整")
	}
	// userOpenID 是平台分配的单聊目标标识；不同 AppID 拿到的 openid 不同。
	userOpenID := strOr(cfg, "user_openid", "")
	// groupOpenID 是平台分配的群聊目标标识；不同 AppID 拿到的 openid 不同。
	groupOpenID := strOr(cfg, "group_openid", "")
	if strings.TrimSpace(userOpenID) == "" && strings.TrimSpace(groupOpenID) == "" {
		return fmt.Errorf("qq 需要配置 user_openid 或 group_openid")
	}
	// client 是缓存的机器人客户端；err 表示凭据无效或刷新链路启动失败。
	client, err := n.qqClient(appID, appSecret)
	if err != nil {
		return err
	}
	// ctx 为单次发送提供有界取消路径，避免平台无响应时长时间占用 outbox worker。
	ctx, cancel := context.WithTimeout(context.Background(), qqSendTimeout)
	defer cancel()
	if strings.TrimSpace(userOpenID) != "" {
		if // sendErr 用于本次流程后续判断的发送错误
		sendErr := client.SendC2CMessage(ctx, strings.TrimSpace(userOpenID), message); sendErr != nil {
			return fmt.Errorf("qq 单聊发送失败: %w", sendErr)
		}
		return nil
	}
	if // sendErr 用于本次流程后续判断的发送错误
	sendErr := client.SendGroupMessage(ctx, strings.TrimSpace(groupOpenID), message); sendErr != nil {
		return fmt.Errorf("qq 群聊发送失败: %w", sendErr)
	}
	return nil
}

// qqClient 返回按 AppID 缓存的机器人客户端；缓存未命中时构造并登记。
// 并发约束：qqMu 只保护 qqClients 读写，构造客户端在锁外完成，避免持锁执行网络 I/O。
func (n *Notifier) qqClient(appID, appSecret string) (qqBotClient, error) {
	// key 是缓存键，仅使用非敏感的 AppID。
	key := strings.TrimSpace(appID)
	n.qqMu.Lock()
	if n.qqClients == nil {
		n.qqClients = make(map[string]qqBotClient)
	}
	// cached、ok 表示缓存命中的客户端及其存在性。
	cached, ok := n.qqClients[key]
	n.qqMu.Unlock()
	if ok {
		return cached, nil
	}
	// client、err 保存新建的机器人客户端及其构造错误。
	client, err := newQQBotClient(n.refreshContext(), appID, appSecret)
	if err != nil {
		return nil, err
	}
	n.qqMu.Lock()
	// 并发首次构造时只保留最先登记的客户端，避免覆盖已有可用客户端。
	if existing, exists := n.qqClients[key]; exists {
		n.qqMu.Unlock()
		return existing, nil
	}
	n.qqClients[key] = client
	n.qqMu.Unlock()
	return client, nil
}

// SendC2CMessage 通过 botgo 向单聊用户发送纯文本消息。
func (c *botgoQQClient) SendC2CMessage(ctx context.Context, userOpenID, content string) error {
	return c.send(ctx, userOpenID, content, true)
}

// SendGroupMessage 通过 botgo 向群聊发送纯文本消息。
func (c *botgoQQClient) SendGroupMessage(ctx context.Context, groupOpenID, content string) error {
	return c.send(ctx, groupOpenID, content, false)
}

// send 统一封装 botgo 的 api-v2 发送调用；group 为真时走群聊，否则走单聊。
func (c *botgoQQClient) send(ctx context.Context, openID, content string, group bool) error {
	if c == nil || c.api == nil {
		return fmt.Errorf("qq 机器人客户端未初始化")
	}
	if strings.TrimSpace(openID) == "" {
		return fmt.Errorf("qq 目标 openid 为空")
	}
	// msg 是 api-v2 纯文本消息体，TextMsg 对应官方 msg_type=0。
	msg := dto.MessageToCreate{Content: content, MsgType: dto.TextMsg}
	if group {
		// groupErr 用于本次流程后续判断的groupErr
		_, groupErr := c.api.PostGroupMessage(ctx, openID, msg)
		return groupErr
	}
	// c2cErr 用于本次流程后续判断的c2cErr
	_, c2cErr := c.api.PostC2CMessage(ctx, openID, msg)
	return c2cErr
}
