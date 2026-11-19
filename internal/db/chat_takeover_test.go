// chat_takeover_test.go 锁定客服接管会话查询的聚合计数、归属过滤与键集游标。

package db

import (
	"context"
	"testing"
)

// TestListTakeoverSessionsAggregatesAndFiltersOwnership 验证只返回属于当前账号且被 AI 回复过的会话。
func TestListTakeoverSessionsAggregatesAndFiltersOwnership(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// adminID、cookieID 是当前用户的标识与账号。
	adminID, cookieID := seedAccount(t, s)
	// otherCreated、otherErr 是第二个用户的创建结果。
	otherCreated, otherErr := s.Users.Create(ctx, "other", "other@e.com", "pw")
	if otherErr != nil || !otherCreated {
		t.Fatalf("创建第二用户失败: created=%v err=%v", otherCreated, otherErr)
	}
	// other 是第二用户记录。
	other, userErr := s.Users.GetByUsername(ctx, "other")
	if userErr != nil {
		t.Fatalf("读取第二用户失败: %v", userErr)
	}
	// otherCookieErr 是第二用户账号的创建错误。
	if otherCookieErr := s.Cookies.Save(ctx, "acc2", "cv=other", other.ID); otherCookieErr != nil {
		t.Fatalf("创建第二账号失败: %v", otherCookieErr)
	}
	// exec 是插入夹具行的通用执行函数。
	exec := func(query string, args ...any) {
		t.Helper()
		if // err 是夹具插入失败原因。
		_, err := s.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("插入夹具失败: %v", err)
		}
	}
	// 目标账号下两个可见会话：会话 A 有 2 条 assistant 记录，会话 B 只有 user 记录。
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		cookieID, "chat-a", "b1", "买家A", 2000, 2000, 2000, true, 0)
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		cookieID, "chat-b", "b2", "买家B", 1000, 1000, 1000, true, 0)
	// 隐藏会话与其它账号会话都不得出现在结果中。
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		cookieID, "chat-hidden", "b3", "买家C", 3000, 3000, 3000, true, 3000)
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		"acc2", "chat-other", "b4", "买家D", 4000, 4000, 4000, true, 0)
	// AI 记忆：会话 A 两条 assistant、会话 B 一条 user、隐藏会话与其它账号各一条 assistant。
	exec(`INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`, cookieID, "chat-a", "b1", "i1", "assistant", "回复1")
	exec(`INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`, cookieID, "chat-a", "b1", "i1", "assistant", "回复2")
	exec(`INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`, cookieID, "chat-b", "b2", "i1", "user", "你好")
	exec(`INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`, cookieID, "chat-hidden", "b3", "i1", "assistant", "回复3")
	exec(`INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`, "acc2", "chat-other", "b4", "i1", "assistant", "回复4")

	// page、listErr 是按用户读取的接管会话分页。
	page, listErr := s.Chats.ListTakeoverSessions(ctx, adminID, nil, 50)
	if listErr != nil {
		t.Fatalf("读取接管会话失败: %v", listErr)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].ChatID != "chat-a" || page.Sessions[0].TakeoverCount != 2 {
		t.Fatalf("接管会话聚合或归属过滤不符，实际 %+v", page.Sessions)
	}
	if page.HasMore || page.NextCursor != nil {
		t.Fatalf("单页结果不应报告更多页，实际 %+v", page)
	}
	// defaulted、defaultedErr 是零页大小按默认上限收敛后的结果。
	defaulted, defaultedErr := s.Chats.ListTakeoverSessions(ctx, adminID, nil, 0)
	if defaultedErr != nil || len(defaulted.Sessions) != 1 {
		t.Fatalf("零页大小未按默认上限收敛: page=%+v err=%v", defaulted, defaultedErr)
	}
}

// TestListTakeoverSessionsStorageFailure 验证数据库关闭后查询返回错误而不是空页。
func TestListTakeoverSessionsStorageFailure(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// adminID 是当前用户的标识。
	adminID, _ := seedAccount(t, s)
	// closeErr 是关闭测试数据库的失败原因。
	if closeErr := s.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库: %v", closeErr)
	}
	// _, queryErr 是数据库关闭后的查询结果。
	if _, queryErr := s.Chats.ListTakeoverSessions(context.Background(), adminID, nil, 10); queryErr == nil {
		t.Fatal("数据库关闭后接管会话查询应返回错误")
	}
}

// TestListTakeoverSessionsCursorPaging 验证 last_message_at + chat_id 键集游标稳定翻页。
func TestListTakeoverSessionsCursorPaging(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// adminID、cookieID 是当前用户的标识与账号。
	adminID, cookieID := seedAccount(t, s)
	// sessions 是三个同用户、不同最后消息时间的会话定义。
	sessions := []struct {
		chatID        string
		lastMessageAt int64
	}{
		{chatID: "chat-new", lastMessageAt: 3000},
		{chatID: "chat-mid", lastMessageAt: 2000},
		{chatID: "chat-old", lastMessageAt: 1000},
	}
	// session 是当前待插入的会话定义。
	for _, session := range sessions {
		if // err 是会话夹具插入失败原因。
		_, err := s.DB.ExecContext(ctx, `INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at,is_visible,user_hidden_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			cookieID, session.chatID, "b", "买家", session.lastMessageAt, session.lastMessageAt, session.lastMessageAt, true, 0); err != nil {
			t.Fatalf("插入会话夹具失败: %v", err)
		}
		if // err 是 AI 记忆夹具插入失败原因。
		_, err := s.DB.ExecContext(ctx, `INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content) VALUES (?,?,?,?,?,?)`,
			cookieID, session.chatID, "b", "i1", "assistant", "回复"); err != nil {
			t.Fatalf("插入 AI 记忆夹具失败: %v", err)
		}
	}
	// first、firstErr 是第一页，页大小为 2。
	first, firstErr := s.Chats.ListTakeoverSessions(ctx, adminID, nil, 2)
	if firstErr != nil {
		t.Fatalf("读取第一页失败: %v", firstErr)
	}
	if len(first.Sessions) != 2 || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("第一页应为 2 条且有下一页，实际 %+v", first)
	}
	if first.Sessions[0].ChatID != "chat-new" || first.Sessions[1].ChatID != "chat-mid" {
		t.Fatalf("第一页顺序不符，实际 %+v", first.Sessions)
	}
	// second、secondErr 是以第一页游标读取的第二页。
	second, secondErr := s.Chats.ListTakeoverSessions(ctx, adminID, first.NextCursor, 2)
	if secondErr != nil {
		t.Fatalf("读取第二页失败: %v", secondErr)
	}
	if len(second.Sessions) != 1 || second.Sessions[0].ChatID != "chat-old" || second.HasMore {
		t.Fatalf("第二页结果不符，实际 %+v", second)
	}
}
