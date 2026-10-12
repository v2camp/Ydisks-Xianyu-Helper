// reply_image_delivery.go 把自动回复的图片投递接到聊天应用服务。
//
// 组合层在这里只做一件事：把已装配的聊天应用服务桥接为 engine 的账号级图片投递端口。
// 下载、平台上传与协议发送的完整链路都在 chatapp 内，引擎不自行测量尺寸，也不直连 WebSocket。

package runtime

import (
	"xianyu-go/internal/adapter"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/engine"
)

// buildReplyImageDeliveryProvider 装配账号级图片自动回复投递端口的提供者。
//
// services 延迟读取已完成装配的应用服务集合：提供者只在账号启动时才被调用，读到它时组合早已结束，
// 因此可以在应用服务装配完成前创建，与 supportAgentFactory 同样破解构造顺序环。
// 返回的提供者每次调用构造无状态桥接，账号重启时自然拿到最新服务引用。
// 聊天应用服务未装配时返回 nil，engine 据此跳过含图片的回复并记录原因。
func buildReplyImageDeliveryProvider(services func() *composition.Services) func() engine.ReplyImageDelivery {
	return func() engine.ReplyImageDelivery {
		if services == nil {
			return nil
		}
		// chat 是只读投影中的聊天应用服务；未装配时保持 nil，账号运行时跳过含图片的回复。
		chat := services().TransportPorts().Chat
		if chat == nil {
			return nil
		}
		return adapter.NewChatReplyDelivery(chat)
	}
}
