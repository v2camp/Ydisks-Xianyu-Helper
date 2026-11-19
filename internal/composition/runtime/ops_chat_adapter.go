// ops_chat_adapter.go 把 qqbot 的留痕与发送端口接到运营对话应用服务。
//
// 记录端口只做模型转换与错误分类；日志只带会话主键与错误类别，不记录消息正文或凭据。
// 发送端口按 AppID 缓存机器人发送器，构造在锁外完成，刷新协程继承进程生命周期 Context。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	opschatapp "xianyu-go/internal/application/opschat"
	"xianyu-go/internal/db"
	qqbot "xianyu-go/internal/qqbot"
)

// opsChatErrorKind 把留痕错误归类为稳定标签，避免把数据库细节写进日志。
func opsChatErrorKind(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, opschatapp.ErrConversationNotFound):
		return "conversation_not_found"
	case errors.Is(err, opschatapp.ErrInvalidContent):
		return "invalid_content"
	case errors.Is(err, opschatapp.ErrChannelNotConfigured):
		return "channel_not_configured"
	default:
		return "internal"
	}
}

// opsChatRecorder 把 QQ 传输层的消息留痕端口适配到运营对话应用服务。
type opsChatRecorder struct {
	// service 是运营对话应用服务；nil 时记录操作保持无副作用。
	service *opschatapp.Service
	// logger 记录留痕失败，只带会话主键与错误类别。
	logger *slog.Logger
}

// 编译期断言：记录器必须满足 qqbot 传输层声明的留痕端口。
var _ qqbot.Recorder = opsChatRecorder{}

// opsChatRecordTarget 返回消息的会话目标；群聊优先使用群 openid。
func opsChatRecordTarget(message qqbot.RecordedMessage) (string, bool) {
	// group 是去除空白后的群标识；非空表示群聊。
	group := strings.TrimSpace(message.GroupOpenID)
	if group != "" {
		return group, true
	}
	return strings.TrimSpace(message.SenderOpenID), false
}

// RecordInbound 记录一条入站消息，返回它是否为新的平台消息。
func (r opsChatRecorder) RecordInbound(ctx context.Context, message qqbot.RecordedMessage) (bool, error) {
	if r.service == nil {
		return false, nil
	}
	// target、isGroup 是本次记录使用的会话目标与群聊标记。
	target, isGroup := opsChatRecordTarget(message)
	// stored、inserted、recordErr 是写入后的消息、是否新增与失败原因。
	stored, inserted, recordErr := r.service.RecordInbound(ctx, opschatapp.InboundRecord{
		Channel: "qq", TargetOpenID: target, PlatformMsgID: message.MsgID, Content: message.Content,
		IsGroup: isGroup, At: message.At.UnixMilli(),
	})
	if recordErr != nil {
		r.warn("QQ 运营入站消息记录失败", stored.ConversationID, recordErr)
		return false, recordErr
	}
	return inserted, nil
}

// RecordOutbound 记录一条出站消息；source 由传输层给出，当前固定为 command。
func (r opsChatRecorder) RecordOutbound(ctx context.Context, message qqbot.RecordedMessage, source string) error {
	if r.service == nil {
		return nil
	}
	// target、isGroup 是本次记录使用的会话目标与群聊标记。
	target, isGroup := opsChatRecordTarget(message)
	// stored、conversation、recordErr 是写入后的消息、会话快照与失败原因。
	_, conversation, recordErr := r.service.RecordOutbound(ctx, opschatapp.OutboundRecord{
		Channel: "qq", TargetOpenID: target, Content: message.Content, IsGroup: isGroup, At: message.At.UnixMilli(),
	})
	if recordErr != nil {
		r.warn("QQ 运营出站消息记录失败", conversation.ID, recordErr)
		return recordErr
	}
	return nil
}

// warn 记录留痕失败；只输出会话主键与错误类别，不输出消息正文。
func (r opsChatRecorder) warn(message string, conversationID int64, err error) {
	if r.logger == nil {
		return
	}
	r.logger.Warn(message, "conversation_id", conversationID, "error_kind", opsChatErrorKind(err))
}

// opsQQSender 把 qqbot 主动消息发送器适配为运营对话应用的发送端口。
// 并发约束：mu 只保护 senders 映射；读取设置与构造发送器都在锁外完成，避免持锁做网络 I/O。
type opsQQSender struct {
	// store 提供 QQ 连接器设置读取，敏感密钥只在本适配器内短暂存在。
	store *db.Store
	// lifecycleContext 返回进程生命周期上下文，供发送器令牌刷新协程继承。
	lifecycleContext func() context.Context
	// mu 保护 senders 映射。
	mu sync.Mutex
	// senders 是按 AppID 缓存的发送器，键为非敏感接入标识。
	senders map[string]*qqbot.Sender
}

// 编译期断言：发送端口必须满足运营对话应用声明的发送契约。
var _ opschatapp.Sender = (*opsQQSender)(nil)

// newOpsQQSender 构造运营人工发送适配器；store 缺失时返回 nil。
func newOpsQQSender(store *db.Store, lifecycleContext func() context.Context) *opsQQSender {
	if store == nil {
		return nil
	}
	return &opsQQSender{store: store, lifecycleContext: lifecycleContext, senders: make(map[string]*qqbot.Sender)}
}

// buildOpsChatSender 返回运营人工发送端口；store 缺失时返回 nil 接口，避免装填有类型的 nil。
func buildOpsChatSender(store *db.Store, lifecycleContext func() context.Context) opschatapp.Sender {
	// sender 是构造好的发送适配器；store 缺失时保持 nil。
	sender := newOpsQQSender(store, lifecycleContext)
	if sender == nil {
		return nil
	}
	return sender
}

// SendText 读取当前用户的 QQ 凭据并发送纯文本消息。
// 凭据缺失返回 ErrChannelNotConfigured；平台失败由应用层转换为 ErrSendFailed。
func (s *opsQQSender) SendText(ctx context.Context, userID int64, targetOpenID string, isGroup bool, content string) error {
	if s == nil || s.store == nil || s.store.Settings == nil {
		return opschatapp.ErrChannelNotConfigured
	}
	// appID 是机器人接入标识；读取失败或缺失按渠道未配置处理。
	appID, appIDErr := s.store.Settings.Get(ctx, settingQQBotAppID)
	if appIDErr != nil || strings.TrimSpace(appID) == "" {
		return opschatapp.ErrChannelNotConfigured
	}
	// appSecret 是机器人接入密钥，属敏感键必须带审计读取，禁止写入日志。
	appSecret, secretErr := s.store.ReadSensitiveSetting(ctx, userID, settingQQBotAppSecret, "settings.use", "qqbot")
	if secretErr != nil || strings.TrimSpace(appSecret) == "" {
		return opschatapp.ErrChannelNotConfigured
	}
	// sender、senderErr 是按 AppID 缓存或新构造的发送器及失败原因。
	sender, senderErr := s.sender(appID, appSecret)
	if senderErr != nil {
		return fmt.Errorf("构造 QQ 运营发送器失败: %w", senderErr)
	}
	return sender.SendText(ctx, targetOpenID, isGroup, content)
}

// sender 返回按 AppID 缓存的发送器；缓存未命中时在锁外构造并登记。
func (s *opsQQSender) sender(appID, appSecret string) (*qqbot.Sender, error) {
	// key 是缓存键，仅使用非敏感的 AppID。
	key := strings.TrimSpace(appID)
	s.mu.Lock()
	if s.senders == nil {
		s.senders = make(map[string]*qqbot.Sender)
	}
	// cached、ok 表示缓存命中的发送器及其存在性。
	cached, ok := s.senders[key]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	// lifecycleCtx 是发送器令牌刷新协程继承的进程生命周期上下文；缺失时不得启动刷新。
	var lifecycleCtx context.Context
	if s.lifecycleContext != nil {
		lifecycleCtx = s.lifecycleContext()
	}
	if lifecycleCtx == nil {
		return nil, errors.New("运营发送器缺少进程生命周期上下文")
	}
	// sender、buildErr 是新建的发送器及其构造错误。
	sender, buildErr := qqbot.NewSender(lifecycleCtx, appID, appSecret)
	if buildErr != nil {
		return nil, buildErr
	}
	s.mu.Lock()
	// 并发首次构造时只保留最先登记的发送器，避免覆盖已有可用实例。
	if existing, exists := s.senders[key]; exists {
		s.mu.Unlock()
		return existing, nil
	}
	s.senders[key] = sender
	s.mu.Unlock()
	return sender, nil
}
