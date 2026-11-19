// sender.go 提供运营 Agent 主动消息发送器：Web 人工发送走平台主动消息接口。
//
// 与入站网关不同，主动消息不携带 msg_id，消耗平台主动消息配额；失败只能如实上报，
// 不允许在本地冒充已发送。错误信息不得包含消息正文。
package qqbot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
)

// Sender 是 QQ 机器人主动消息发送器。
type Sender struct {
	// api 是 botgo 的 openapi 实例，内部按 oauth2 TokenSource 自动刷新 Access Token。
	api openapi.OpenAPI
}

// NewSender 用 AppID 与 AppSecret 构造发送器并启动 Access Token 自动刷新。
// ctx 是刷新协程继承的进程生命周期根 Context，禁止传入裸 Background。
func NewSender(ctx context.Context, appID, appSecret string) (*Sender, error) {
	if ctx == nil {
		return nil, errors.New("qq 发送器缺少进程生命周期上下文")
	}
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return nil, ErrGatewayNotConfigured
	}
	// credential 保存用于换取 Access Token 的机器人凭据，不属于敏感明文日志输出范围。
	credential := &token.QQBotCredentials{AppID: strings.TrimSpace(appID), AppSecret: strings.TrimSpace(appSecret)}
	// tokenSource 基于凭据构造 oauth2 TokenSource，botgo 会在请求前自动刷新 Access Token。
	tokenSource := token.NewQQBotTokenSource(credential)
	if // err 是刷新链路启动失败原因，不含凭据明文。
	err := token.StartRefreshAccessToken(ctx, tokenSource); err != nil {
		return nil, fmt.Errorf("启动 QQ 发送器 Access Token 刷新失败: %w", err)
	}
	return &Sender{api: botgo.NewOpenAPI(credential.AppID, tokenSource)}, nil
}

// SendText 向目标发送一条纯文本主动消息；isGroup 决定走群聊还是单聊通道。
// 错误包装只描述通道与失败类别，不回显消息正文。
func (s *Sender) SendText(ctx context.Context, targetOpenID string, isGroup bool, content string) error {
	if s == nil || s.api == nil {
		return errors.New("qq 发送器未初始化")
	}
	// target 是去除空白后的平台目标 openid。
	target := strings.TrimSpace(targetOpenID)
	if target == "" {
		return errors.New("qq 发送目标 openid 为空")
	}
	// msg 是 api-v2 纯文本消息体；不带 msg_id 即为主动消息。
	msg := dto.MessageToCreate{Content: content, MsgType: dto.TextMsg}
	if isGroup {
		// groupErr 是群聊主动发送结果。
		if groupErr := postGroupText(ctx, s.api, target, msg); groupErr != nil {
			return fmt.Errorf("qq 群聊主动发送失败: %w", groupErr)
		}
		return nil
	}
	// c2cErr 是单聊主动发送结果。
	if c2cErr := postC2CText(ctx, s.api, target, msg); c2cErr != nil {
		return fmt.Errorf("qq 单聊主动发送失败: %w", c2cErr)
	}
	return nil
}

// postGroupText 调用 botgo 群聊主动消息接口。
func postGroupText(ctx context.Context, api openapi.OpenAPI, target string, msg dto.MessageToCreate) error {
	// err 是群聊主动消息发送错误。
	_, err := api.PostGroupMessage(ctx, target, msg)
	return err
}

// postC2CText 调用 botgo 单聊主动消息接口。
func postC2CText(ctx context.Context, api openapi.OpenAPI, target string, msg dto.MessageToCreate) error {
	// err 是单聊主动消息发送错误。
	_, err := api.PostC2CMessage(ctx, target, msg)
	return err
}
