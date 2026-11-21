// Package opschat 提供运营 Agent 对话的应用层编排：入站/出站留痕、会话与消息分页、Web 人工发送。
//
// 边界：本包不依赖 HTTP、数据库模型或平台 SDK，只声明消费方端口；
// 发送失败时不得写入半条消息——先确认平台发送成功，再落库出站记录。
package opschat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"xianyu-go/internal/qqbot"
)

// ErrConversationNotFound 表示目标运营会话不存在。
var ErrConversationNotFound = errors.New("运营会话不存在")

// ErrChannelNotConfigured 表示 QQ 渠道凭据未就绪，无法发送。
var ErrChannelNotConfigured = errors.New("运营渠道未配置")

// ErrInvalidContent 表示发送内容为空或超过平台允许的字符数。
var ErrInvalidContent = errors.New("运营消息内容无效")

// ErrSendFailed 表示平台发送失败，本地不得留下出站记录。
var ErrSendFailed = errors.New("运营消息发送失败")

const (
	// defaultConversationPageSize 是会话列表未指定页大小时的默认值。
	defaultConversationPageSize = 20
	// maxConversationPageSize 是会话列表允许的最大页大小，避免单次扫描过大。
	maxConversationPageSize = 100
	// defaultMessageLimit 是消息列表未指定 limit 时的默认条数。
	defaultMessageLimit = 50
	// maxMessageLimit 是消息列表允许的最大条数。
	maxMessageLimit = 200
	// previewRunes 是会话摘要预览保留的最大字符数。
	previewRunes = 60
	// defaultChannel 是运营渠道标识，当前只有 QQ。
	defaultChannel = "qq"
)

// Conversation 是运营会话的应用层模型，不含任何凭证。
type Conversation struct {
	// ID 是会话主键。
	ID int64
	// UserID 是会话所属本地用户，决定数据隔离范围。
	UserID int64
	// Channel 是渠道标识，当前为 qq。
	Channel string
	// TargetOpenID 是平台会话目标 openid。
	TargetOpenID string
	// IsGroup 表示目标是否为群聊。
	IsGroup bool
	// LastMessageAt 是最近消息的 Unix 毫秒时间。
	LastMessageAt int64
	// LastMessagePreview 是最近消息的截断预览。
	LastMessagePreview string
}

// Message 是运营消息的应用层模型，不直接序列化为 HTTP 响应。
type Message struct {
	// ID 是消息主键。
	ID int64
	// ConversationID 是所属会话主键。
	ConversationID int64
	// Direction 是消息方向：inbound 或 outbound。
	Direction string
	// Source 是消息来源：inbound、command 或 operator。
	Source string
	// Content 是消息正文，属于业务内容。
	Content string
	// CreatedAt 是消息写入时的 Unix 毫秒时间。
	CreatedAt int64
}

// ConversationPage 是运营会话分页结果。
type ConversationPage struct {
	// Conversations 是当前页会话列表。
	Conversations []Conversation
	// Total 是会话总数。
	Total int
	// Page 是当前页码，从 1 开始。
	Page int
	// PageSize 是本次请求使用的页大小。
	PageSize int
}

// MessagePage 是运营消息游标结果。
type MessagePage struct {
	// Messages 是按时间正序排列的当前页消息。
	Messages []Message
	// HasMore 表示是否还有更早的消息。
	HasMore bool
}

// InboundRecord 是一次入站消息的留痕输入。
type InboundRecord struct {
	// UserID 是消息所属本地用户；零值表示调用方未提供归属。
	UserID int64
	// Channel 是渠道标识，空白时按 qq 处理。
	Channel string
	// TargetOpenID 是平台会话目标 openid。
	TargetOpenID string
	// PlatformMsgID 是平台消息标识；非空时参与去重。
	PlatformMsgID string
	// Content 是消息正文。
	Content string
	// IsGroup 表示目标是否为群聊。
	IsGroup bool
	// At 是消息时间（Unix 毫秒）；零值时使用服务时钟。
	At int64
}

// OutboundRecord 是一次出站消息的留痕输入。
type OutboundRecord struct {
	// UserID 是消息所属本地用户；零值表示调用方未提供归属。
	UserID int64
	// Channel 是渠道标识，空白时按 qq 处理。
	Channel string
	// TargetOpenID 是平台会话目标 openid。
	TargetOpenID string
	// Content 是消息正文。
	Content string
	// IsGroup 表示目标是否为群聊。
	IsGroup bool
	// At 是消息时间（Unix 毫秒）；零值时使用服务时钟。
	At int64
}

// SendInput 是 Web 人工发送的输入。
type SendInput struct {
	// UserID 是发起发送的登录用户，用于读取该用户的 QQ 凭据。
	UserID int64
	// ConversationID 是目标运营会话主键。
	ConversationID int64
	// Content 是待发送的纯文本内容。
	Content string
}

// Repository 定义运营对话用例需要的最小持久化能力。
type Repository interface {
	// RecordInbound 幂等写入入站消息，返回消息与是否为新增记录。
	RecordInbound(context.Context, InboundRecord) (Message, bool, error)
	// AppendOutbound 按用户与会话主键写入出站消息并更新会话摘要。
	AppendOutbound(context.Context, int64, int64, string, string, int64) (Message, Conversation, error)
	// AppendOutboundByTarget 按用户、渠道与目标 openid 写入出站消息，供命令回复留痕使用。
	AppendOutboundByTarget(context.Context, int64, string, string, string, string, int64) (Message, Conversation, error)
	// ListConversations 分页读取指定用户的会话列表与总数。
	ListConversations(context.Context, int64, int, int) (ConversationPage, error)
	// GetConversation 按用户与会话主键读取会话，不存在时返回 ErrConversationNotFound。
	GetConversation(context.Context, int64, int64) (Conversation, error)
	// ListMessages 按用户、会话与游标读取消息页。
	ListMessages(context.Context, int64, int64, int64, int) (MessagePage, error)
}

// Sender 定义运营渠道的文本发送能力；实现负责读取凭据，应用层不接触秘密。
type Sender interface {
	// SendText 向目标发送一条纯文本消息；isGroup 决定走群聊还是单聊通道。
	SendText(ctx context.Context, userID int64, targetOpenID string, isGroup bool, content string) error
}

// Service 编排运营对话用例，不持有 HTTP 请求或数据库连接。
type Service struct {
	// repository 保存调用方注入的最小持久化端口。
	repository Repository
	// sender 保存运营渠道发送端口；为 nil 时发送返回 ErrChannelNotConfigured。
	sender Sender
	// clock 返回当前时刻，供测试替换；nil 时使用系统时间。
	clock func() time.Time
}

// NewService 构造运营对话应用服务；repository 为空时列表与发送都会失败。
func NewService(repository Repository, sender Sender) *Service {
	return &Service{repository: repository, sender: sender}
}

// SetClock 注入测试用时间源；nil 表示恢复系统时间。
func (s *Service) SetClock(clock func() time.Time) {
	if s != nil {
		s.clock = clock
	}
}

// now 返回服务当前时间；未注入时钟时使用系统 UTC 时间。
func (s *Service) now() time.Time {
	if s != nil && s.clock != nil {
		return s.clock()
	}
	return time.Now().UTC()
}

// normalizeChannel 返回去除空白后的渠道标识；空白时回退 qq。
func normalizeChannel(channel string) string {
	// normalized 是去除首尾空白后的渠道标识。
	normalized := strings.TrimSpace(channel)
	if normalized == "" {
		return defaultChannel
	}
	return normalized
}

// preview 截断消息正文作为会话摘要预览。
func preview(content string) string {
	if utf8.RuneCountInString(content) <= previewRunes {
		return content
	}
	// runes 是按字符切分的正文，避免按字节截断中文。
	runes := []rune(content)
	return string(runes[:previewRunes])
}

// RecordInbound 写入一条入站消息；返回是否为新增记录，重复平台消息返回 false。
func (s *Service) RecordInbound(ctx context.Context, record InboundRecord) (Message, bool, error) {
	if s == nil || s.repository == nil {
		return Message{}, false, ErrChannelNotConfigured
	}
	if record.UserID <= 0 {
		return Message{}, false, ErrConversationNotFound
	}
	// normalized 是补齐渠道与时间后的入站记录。
	normalized := record
	normalized.Channel = normalizeChannel(record.Channel)
	normalized.TargetOpenID = strings.TrimSpace(record.TargetOpenID)
	if normalized.TargetOpenID == "" {
		return Message{}, false, ErrInvalidContent
	}
	normalized.PlatformMsgID = strings.TrimSpace(record.PlatformMsgID)
	if normalized.At <= 0 {
		normalized.At = s.now().UnixMilli()
	}
	return s.repository.RecordInbound(ctx, normalized)
}

// RecordOutbound 写入一条出站命令回复留痕；目标会话不存在时返回 ErrConversationNotFound。
func (s *Service) RecordOutbound(ctx context.Context, record OutboundRecord) (Message, Conversation, error) {
	if s == nil || s.repository == nil {
		return Message{}, Conversation{}, ErrChannelNotConfigured
	}
	if record.UserID <= 0 {
		return Message{}, Conversation{}, ErrConversationNotFound
	}
	// normalized 是补齐渠道与时间后的出站记录。
	normalized := record
	normalized.Channel = normalizeChannel(record.Channel)
	normalized.TargetOpenID = strings.TrimSpace(record.TargetOpenID)
	if normalized.TargetOpenID == "" {
		return Message{}, Conversation{}, ErrInvalidContent
	}
	if normalized.At <= 0 {
		normalized.At = s.now().UnixMilli()
	}
	return s.repository.AppendOutboundByTarget(ctx, normalized.UserID, normalized.Channel, normalized.TargetOpenID, record.Content, preview(record.Content), normalized.At)
}

// List 分页读取当前用户的运营会话；page 最小为 1，page_size 默认 20、上限 100。
func (s *Service) List(ctx context.Context, userID int64, page, pageSize int) (ConversationPage, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return ConversationPage{}, ErrChannelNotConfigured
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultConversationPageSize
	}
	if pageSize > maxConversationPageSize {
		pageSize = maxConversationPageSize
	}
	// result、listErr 保存会话分页结果及查询错误。
	result, listErr := s.repository.ListConversations(ctx, userID, page, pageSize)
	if listErr != nil {
		return ConversationPage{}, listErr
	}
	// 仓储返回的页码与页大小必须与本次收敛结果一致，避免响应出现零值。
	result.Page, result.PageSize = page, pageSize
	return result, nil
}

// Messages 读取指定用户会话的消息页；会话不存在或不属于该用户时返回 ErrConversationNotFound。
func (s *Service) Messages(ctx context.Context, userID, conversationID, beforeID int64, limit int) (MessagePage, error) {
	if s == nil || s.repository == nil || userID <= 0 || conversationID <= 0 {
		return MessagePage{}, ErrConversationNotFound
	}
	// lookupErr 是会话存在性校验结果。
	_, lookupErr := s.repository.GetConversation(ctx, userID, conversationID)
	if lookupErr != nil {
		return MessagePage{}, lookupErr
	}
	if limit <= 0 {
		limit = defaultMessageLimit
	}
	if limit > maxMessageLimit {
		limit = maxMessageLimit
	}
	return s.repository.ListMessages(ctx, userID, conversationID, beforeID, limit)
}

// Send 发送一条 Web 人工消息：先校验内容，再确认会话与渠道，平台发送成功后才落库。
// 平台失败返回 ErrSendFailed 且不写入任何出站记录。
func (s *Service) Send(ctx context.Context, input SendInput) (Message, Conversation, error) {
	if s == nil || s.repository == nil {
		return Message{}, Conversation{}, ErrChannelNotConfigured
	}
	// content 是去除首尾空白后的待发送正文。
	content := strings.TrimSpace(input.Content)
	if content == "" || utf8.RuneCountInString(content) > qqbot.MaxReplyRunes {
		return Message{}, Conversation{}, ErrInvalidContent
	}
	if input.ConversationID <= 0 || input.UserID <= 0 {
		return Message{}, Conversation{}, ErrConversationNotFound
	}
	// conversation、lookupErr 保存目标会话快照及读取错误。
	conversation, lookupErr := s.repository.GetConversation(ctx, input.UserID, input.ConversationID)
	if lookupErr != nil {
		return Message{}, Conversation{}, lookupErr
	}
	if s.sender == nil {
		return Message{}, Conversation{}, ErrChannelNotConfigured
	}
	// sendErr 是平台发送结果；失败时不得落库，避免本地冒充已发送。
	if sendErr := s.sender.SendText(ctx, input.UserID, conversation.TargetOpenID, conversation.IsGroup, content); sendErr != nil {
		return Message{}, Conversation{}, fmt.Errorf("%w: %v", ErrSendFailed, sendErr)
	}
	// message、updated、appendErr 是落库后的出站消息、会话快照及写入错误。
	message, updated, appendErr := s.repository.AppendOutbound(ctx, input.UserID, conversation.ID, content, preview(content), s.now().UnixMilli())
	if appendErr != nil {
		return Message{}, Conversation{}, appendErr
	}
	return message, updated, nil
}
