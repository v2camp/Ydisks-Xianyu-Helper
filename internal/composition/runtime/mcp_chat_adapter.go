package runtime

// mcp_chat_adapter.go 把聊天应用服务投影为 internal/mcp 的 ChatPorts。
// 适配器只做透传；发送回显、结果不确定与代次隔离语义保留在 chat 应用服务。
// 聊天图片公网下载复用组合层统一受控出站策略，内网地址与非图片一律拒绝。

import (
	"context"

	chatapp "xianyu-go/internal/application/chat"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/mcp"
)

// mcpChatPorts 把聊天应用服务投影为 MCP 聊天端口。
type mcpChatPorts struct {
	// service 是聊天应用服务。
	service *chatapp.Service
}

// 编译期断言适配器满足 MCP 聊天端口。
var _ mcp.ChatPorts = (*mcpChatPorts)(nil)

// ListSessionPage 透传会话键集分页用例。
func (a *mcpChatPorts) ListSessionPage(ctx context.Context, userID int64, accountID string, cursor *chatapp.SessionCursor, limit int) (chatapp.SessionPage, error) {
	return a.service.ListSessionPage(ctx, userID, accountID, cursor, limit)
}

// FindSession 透传单个归属会话读取用例。
func (a *mcpChatPorts) FindSession(ctx context.Context, userID int64, accountID, chatID string) (chatapp.Session, error) {
	return a.service.FindSession(ctx, userID, accountID, chatID)
}

// ListStoredMessages 透传本地消息分页用例。
func (a *mcpChatPorts) ListStoredMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) (chatapp.Page, error) {
	return a.service.ListStoredMessages(ctx, userID, accountID, chatID, beforeID, limit)
}

// RefreshConversations 透传平台联系人刷新用例。
func (a *mcpChatPorts) RefreshConversations(ctx context.Context, accountID string, cursor int64, limit int) (chatapp.ConversationPage, error) {
	return a.service.RefreshConversations(ctx, accountID, cursor, limit)
}

// RefreshHistory 透传平台历史消息刷新用例。
func (a *mcpChatPorts) RefreshHistory(ctx context.Context, accountID, chatID string, cursor int64, limit int, session chatapp.Session) (chatapp.HistoryPage, error) {
	return a.service.RefreshHistory(ctx, accountID, chatID, cursor, limit, session)
}

// SendText 透传文本发送用例。
func (a *mcpChatPorts) SendText(ctx context.Context, input chatapp.OutgoingInput) (*chatapp.Message, error) {
	return a.service.SendText(ctx, input)
}

// SendImage 透传图片发送用例。
func (a *mcpChatPorts) SendImage(ctx context.Context, input chatapp.ImageInput) (*chatapp.Message, error) {
	return a.service.SendImage(ctx, input)
}

// MarkRead 透传会话已读用例。
func (a *mcpChatPorts) MarkRead(ctx context.Context, userID int64, accountID, chatID string) error {
	return a.service.MarkRead(ctx, userID, accountID, chatID)
}

// DeleteConversation 透传会话删除用例。
func (a *mcpChatPorts) DeleteConversation(ctx context.Context, userID int64, accountID, chatID string) error {
	return a.service.DeleteConversation(ctx, userID, accountID, chatID)
}

// FetchChatImage 复用组合层公网图片下载：只允许公网地址、限制大小并校验媒体类型。
func (a *mcpChatPorts) FetchChatImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	return composition.DownloadPublishImageURL(ctx, rawURL)
}

// ListQuickReplies 透传快捷回复列表用例。
func (a *mcpChatPorts) ListQuickReplies(ctx context.Context, userID int64, accountID string) ([]chatapp.QuickReply, error) {
	return a.service.ListQuickReplies(ctx, userID, accountID)
}

// CreateQuickReply 透传快捷回复新增用例。
func (a *mcpChatPorts) CreateQuickReply(ctx context.Context, userID int64, accountID, content string) (chatapp.QuickReply, error) {
	return a.service.CreateQuickReply(ctx, userID, accountID, content)
}

// DeleteQuickReply 透传快捷回复删除用例。
func (a *mcpChatPorts) DeleteQuickReply(ctx context.Context, userID int64, accountID string, quickReplyID int64) error {
	return a.service.DeleteQuickReply(ctx, userID, accountID, quickReplyID)
}

// GetBuyerNote 透传买家备注读取用例。
func (a *mcpChatPorts) GetBuyerNote(ctx context.Context, userID int64, accountID, buyerID string) (chatapp.BuyerNote, error) {
	return a.service.GetBuyerNote(ctx, userID, accountID, buyerID)
}

// SaveBuyerNote 透传买家备注保存用例。
func (a *mcpChatPorts) SaveBuyerNote(ctx context.Context, userID int64, accountID, buyerID, content string) (chatapp.BuyerNote, error) {
	return a.service.SaveBuyerNote(ctx, userID, accountID, buyerID, content)
}

// ListChatItems 透传聊天商品卡片查询用例。
func (a *mcpChatPorts) ListChatItems(ctx context.Context, input chatapp.ChatItemQuery) (chatapp.ChatItemPage, error) {
	return a.service.ListChatItems(ctx, input)
}

// newMCPChatPorts 构造 MCP 聊天端口；聊天服务缺失时返回 nil。
func newMCPChatPorts(ports composition.TransportPorts) *mcpChatPorts {
	if ports.Chat == nil {
		return nil
	}
	return &mcpChatPorts{service: ports.Chat}
}
