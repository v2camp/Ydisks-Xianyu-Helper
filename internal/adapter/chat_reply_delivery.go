package adapter

import (
	"context"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/engine"
)

// chatReplyDelivery 把引擎的图片自动回复投递端口适配到聊天应用服务。
// 自动回复图片因此与人工发送共用同一套下载、上传、协议发送和本地状态收口规则。
type chatReplyDelivery struct {
	// application 是消息页面使用的聊天应用服务；自动回复图片经它完成完整发送链路。
	application *chatapp.Service
}

// NewChatReplyDelivery 创建图片自动回复投递端口。
// application 为 nil 时返回 nil，表示当前进程不装配该副作用，账号运行时据此跳过含图片的回复。
func NewChatReplyDelivery(application *chatapp.Service) engine.ReplyImageDelivery {
	if application == nil {
		return nil
	}
	return chatReplyDelivery{application: application}
}

// SendReplyImage 经聊天应用发送一条图片回复。
// ctx 是回复流程的取消边界；accountID、chatID 与 toUserID 共同确定投递目标；imageURL 是规则配置的原始图片地址。
// 返回值保留聊天应用的三类错误语义：确定失败、结果不确定与状态保存失败，供 reply_once 分段策略判断。
func (d chatReplyDelivery) SendReplyImage(ctx context.Context, accountID, chatID, toUserID, imageURL string) error {
	// message 保存聊天应用返回的本地消息记录；投递结论由错误表达，消息本体不回传给引擎。
	_, err := d.application.SendImageURL(ctx, chatapp.ImageURLInput{
		Session:  chatapp.Session{AccountID: accountID, ChatID: chatID, PeerUserID: toUserID},
		ImageURL: imageURL,
	})
	return err
}

// chatImageURLDownloader 把适配器内公用的 URL 图片下载规则实现为聊天应用端口。
// 与自动化发货共用同一套公网校验、重定向复核与体积限制，避免两套 SSRF 规则各自漂移。
type chatImageURLDownloader struct{}

// NewChatImageDownloader 创建聊天应用的 URL 图片下载端口；下载结果只在单次发送生命周期内使用。
func NewChatImageDownloader() chatapp.ImageURLDownloader {
	return chatImageURLDownloader{}
}

// DownloadChatImage 下载 rawURL 指向的图片；ctx 控制本次网络读取取消。
// 返回图片字节、媒体类型与上传文件名；拒绝内网地址、重定向降级、非图片响应与超限体积。
func (chatImageURLDownloader) DownloadChatImage(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	return downloadAutomationImage(ctx, rawURL)
}
