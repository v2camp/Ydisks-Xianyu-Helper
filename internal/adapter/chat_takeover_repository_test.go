// chat_takeover_repository_test.go 验证接管会话查询适配器把数据库模型转换为应用层模型。
package adapter

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestChatRepositoryListTakeoverSessionsStorageFailure 验证数据库关闭后适配器把错误原样返回。
func TestChatRepositoryListTakeoverSessionsStorageFailure(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 存储与关闭函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// owner 保存测试账号所属用户，用于归属过滤。
	owner, ownerErr := store.Users.GetByUsername(context.Background(), "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// err 是关闭测试数据库的失败原因。
	if err := store.DB.Close(); err != nil {
		t.Fatalf("关闭测试数据库: %v", err)
	}
	// _, queryErr 是数据库关闭后的查询结果。
	if _, queryErr := NewChatRepository(store).ListTakeoverSessions(context.Background(), owner.ID, nil, 10); queryErr == nil {
		t.Fatal("数据库关闭后适配器应返回错误")
	}
}

// TestChatRepositoryListTakeoverSessions 验证 AI 回复过的会话被聚合、计数并映射为应用层分页结果。
func TestChatRepositoryListTakeoverSessions(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 存储与关闭函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// owner 保存测试账号所属用户，用于归属过滤。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// session 是接管历史查询依赖的聊天会话摘要。
	session := db.ChatSession{CookieID: "cid", ChatID: "chat-takeover", BuyerID: "buyer-takeover", BuyerName: "买家", ItemID: "item-1", LastMessage: "你好", LastMessageAt: 1000}
	// saveErr 是首次会话写入失败原因。
	if saveErr := store.Chats.UpsertSession(ctx, session); saveErr != nil {
		t.Fatalf("写入会话失败: %v", saveErr)
	}
	// exchangeErr 是 AI 对话记忆写入结果；查询以 assistant 轮次作为接管标记。
	if exchangeErr := store.AIReply.AddConversationExchange(ctx, "cid", "chat-takeover", "buyer-takeover", "item-1",
		db.AIConversationMessage{Role: "user", Content: "你好"},
		db.AIConversationMessage{Role: "assistant", Content: "在的"},
	); exchangeErr != nil {
		t.Fatalf("写入 AI 对话失败: %v", exchangeErr)
	}
	// newerSession 是用于验证分页游标的第二个接管会话。
	newerSession := db.ChatSession{CookieID: "cid", ChatID: "chat-takeover-new", BuyerID: "buyer-new", BuyerName: "买家二", ItemID: "item-1", LastMessage: "再问", LastMessageAt: 2000}
	// secondSaveErr 是第二个会话写入失败原因。
	if secondSaveErr := store.Chats.UpsertSession(ctx, newerSession); secondSaveErr != nil {
		t.Fatalf("写入第二会话失败: %v", secondSaveErr)
	}
	// secondExchangeErr 是第二段 AI 对话记忆写入结果。
	if secondExchangeErr := store.AIReply.AddConversationExchange(ctx, "cid", "chat-takeover-new", "buyer-new", "item-1",
		db.AIConversationMessage{Role: "user", Content: "再问"},
		db.AIConversationMessage{Role: "assistant", Content: "请讲"},
	); secondExchangeErr != nil {
		t.Fatalf("写入第二段 AI 对话失败: %v", secondExchangeErr)
	}
	// repository 是聊天应用端口使用的数据库适配器。
	repository := NewChatRepository(store)
	// page、pageErr 是首页接管会话结果。
	page, pageErr := repository.ListTakeoverSessions(ctx, owner.ID, nil, 10)
	if pageErr != nil {
		t.Fatalf("读取接管会话失败: %v", pageErr)
	}
	if len(page.Sessions) != 2 || page.Sessions[0].ChatID != "chat-takeover-new" || page.Sessions[1].ChatID != "chat-takeover" {
		t.Fatalf("接管会话映射异常: %+v", page.Sessions)
	}
	if page.Sessions[0].TakeoverCount != 1 || page.Sessions[1].TakeoverCount != 1 {
		t.Fatalf("接管轮次计数异常: %+v", page.Sessions)
	}
	if page.Sessions[1].PeerName != "买家" || page.Sessions[1].ItemID != "item-1" {
		t.Fatalf("接管会话字段映射异常: %+v", page.Sessions[1])
	}
	if page.NextCursor != nil {
		t.Fatalf("整页读取不应产生下一页游标: %+v", page.NextCursor)
	}
	// paged、pagedErr 是页大小为 1 的接管会话页，用于覆盖 NextCursor 转换。
	paged, pagedErr := repository.ListTakeoverSessions(ctx, owner.ID, nil, 1)
	if pagedErr != nil {
		t.Fatalf("分页读取接管会话失败: %v", pagedErr)
	}
	if len(paged.Sessions) != 1 || !paged.HasMore || paged.NextCursor == nil {
		t.Fatalf("分页结果不符: %+v", paged)
	}
	// next、nextErr 是游标之后的接管会话页。
	next, nextErr := repository.ListTakeoverSessions(ctx, owner.ID, paged.NextCursor, 10)
	if nextErr != nil {
		t.Fatalf("游标读取接管会话失败: %v", nextErr)
	}
	if len(next.Sessions) != 1 || next.Sessions[0].ChatID != "chat-takeover" {
		t.Fatalf("游标之后应只剩较早会话: %+v", next.Sessions)
	}
}
