// ops_chat_repository.go 把运营会话数据库仓储适配为运营对话应用的窄端口。
//
// 应用层只看到自己的模型；数据库模型、方言与冲突子句全部留在 internal/db。
package composition

import (
	"context"
	"errors"

	opschatapp "xianyu-go/internal/application/opschat"
	"xianyu-go/internal/db"
)

// opsChatRepository 是 opschatapp.Repository 的数据库实现。
type opsChatRepository struct {
	// store 是运营会话与消息的数据库仓储。
	store *db.OpsChatStore
}

// 编译期断言：数据库适配器必须覆盖运营对话应用声明的全部仓储能力。
var _ opschatapp.Repository = opsChatRepository{}

// newOpsChatRepository 构造运营会话仓储适配器；store 为空时返回 nil。
func newOpsChatRepository(store *db.OpsChatStore) opschatapp.Repository {
	if store == nil {
		return nil
	}
	return opsChatRepository{store: store}
}

// RecordInbound 写入入站消息并转换返回模型。
func (r opsChatRepository) RecordInbound(ctx context.Context, record opschatapp.InboundRecord) (opschatapp.Message, bool, error) {
	// message、inserted、recordErr 是数据库返回的消息、是否新增及写入错误。
	message, inserted, recordErr := r.store.RecordInbound(ctx, db.OpsInboundRecord{
		Channel: record.Channel, TargetOpenID: record.TargetOpenID, PlatformMsgID: record.PlatformMsgID,
		Direction: "inbound", Source: "inbound", Content: record.Content, IsGroup: record.IsGroup, At: record.At,
	})
	if recordErr != nil {
		return opschatapp.Message{}, false, recordErr
	}
	return opsChatMessageFromDatabase(message), inserted, nil
}

// AppendOutbound 写入出站消息并返回消息与会话快照。
func (r opsChatRepository) AppendOutbound(ctx context.Context, conversationID int64, content, preview string, at int64) (opschatapp.Message, opschatapp.Conversation, error) {
	// message、conversation、appendErr 是数据库写入结果及失败原因。
	message, conversation, appendErr := r.store.AppendOutbound(ctx, conversationID, content, preview, at)
	if errors.Is(appendErr, db.ErrNotFound) {
		return opschatapp.Message{}, opschatapp.Conversation{}, opschatapp.ErrConversationNotFound
	}
	if appendErr != nil {
		return opschatapp.Message{}, opschatapp.Conversation{}, appendErr
	}
	return opsChatMessageFromDatabase(message), opsChatConversationFromDatabase(conversation), nil
}

// AppendOutboundByTarget 按渠道与目标 openid 写入出站消息；会话不存在时转换为应用错误。
func (r opsChatRepository) AppendOutboundByTarget(ctx context.Context, channel, targetOpenID, content, preview string, at int64) (opschatapp.Message, opschatapp.Conversation, error) {
	// message、conversation、appendErr 是数据库写入结果及失败原因。
	message, conversation, appendErr := r.store.AppendOutboundByTarget(ctx, channel, targetOpenID, content, preview, at)
	if errors.Is(appendErr, db.ErrNotFound) {
		return opschatapp.Message{}, opschatapp.Conversation{}, opschatapp.ErrConversationNotFound
	}
	if appendErr != nil {
		return opschatapp.Message{}, opschatapp.Conversation{}, appendErr
	}
	return opsChatMessageFromDatabase(message), opsChatConversationFromDatabase(conversation), nil
}

// ListConversations 分页读取会话并转换模型。
func (r opsChatRepository) ListConversations(ctx context.Context, page, pageSize int) (opschatapp.ConversationPage, error) {
	// databasePage、listErr 是数据库分页结果及查询错误。
	databasePage, listErr := r.store.ListConversations(ctx, page, pageSize)
	if listErr != nil {
		return opschatapp.ConversationPage{}, listErr
	}
	// conversations 保存脱离数据库模型的会话列表。
	conversations := make([]opschatapp.Conversation, 0, len(databasePage.Conversations))
	// row 是当前待转换的数据库会话。
	for _, row := range databasePage.Conversations {
		conversations = append(conversations, opsChatConversationFromDatabase(row))
	}
	return opschatapp.ConversationPage{
		Conversations: conversations, Total: databasePage.Total, Page: databasePage.Page, PageSize: databasePage.PageSize,
	}, nil
}

// GetConversation 按主键读取会话；不存在时转换为应用错误。
func (r opsChatRepository) GetConversation(ctx context.Context, id int64) (opschatapp.Conversation, error) {
	// conversation、readErr 是数据库读取结果及错误。
	conversation, readErr := r.store.GetConversation(ctx, id)
	if errors.Is(readErr, db.ErrNotFound) {
		return opschatapp.Conversation{}, opschatapp.ErrConversationNotFound
	}
	if readErr != nil {
		return opschatapp.Conversation{}, readErr
	}
	return opsChatConversationFromDatabase(conversation), nil
}

// ListMessages 按会话与游标读取消息页并转换模型。
func (r opsChatRepository) ListMessages(ctx context.Context, conversationID, beforeID int64, limit int) (opschatapp.MessagePage, error) {
	// databasePage、listErr 是数据库消息分页结果及查询错误。
	databasePage, listErr := r.store.ListOpsMessages(ctx, conversationID, beforeID, limit)
	if listErr != nil {
		return opschatapp.MessagePage{}, listErr
	}
	// messages 保存脱离数据库模型的消息列表。
	messages := make([]opschatapp.Message, 0, len(databasePage.Messages))
	// row 是当前待转换的数据库消息。
	for _, row := range databasePage.Messages {
		messages = append(messages, opsChatMessageFromDatabase(row))
	}
	return opschatapp.MessagePage{Messages: messages, HasMore: databasePage.HasMore}, nil
}

// opsChatConversationFromDatabase 把数据库会话转换为应用模型。
func opsChatConversationFromDatabase(row db.OpsConversation) opschatapp.Conversation {
	return opschatapp.Conversation{
		ID: row.ID, Channel: row.Channel, TargetOpenID: row.TargetOpenID, IsGroup: row.IsGroup,
		LastMessageAt: row.LastMessageAt, LastMessagePreview: row.LastMessagePreview,
	}
}

// opsChatMessageFromDatabase 把数据库消息转换为应用模型。
func opsChatMessageFromDatabase(row db.OpsMessage) opschatapp.Message {
	return opschatapp.Message{
		ID: row.ID, ConversationID: row.ConversationID, Direction: row.Direction, Source: row.Source,
		Content: row.Content, CreatedAt: row.CreatedAt,
	}
}
