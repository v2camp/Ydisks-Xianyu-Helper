// chat_timestamp_test.go 锁定聊天域时间字段的单位一致性：created_at / updated_at 必须与
// sent_at / last_message_at 同为 Unix 毫秒，防止「同一张表两种单位」回归。

package db

import (
	"context"
	"testing"
)

// unixMilliLowerBound 是毫秒时间戳的下界：秒级时间戳量级为 1e9，毫秒为 1e12，
// 取 1e11 可明确区分两者，落在下界以下说明仍按秒落库。
const unixMilliLowerBound = int64(100000000000)

// TestSaveMessagePersistsMillisecondTimestamps 验证保存消息后消息与会话的时间字段均为毫秒。
// 该断言锁定 00060 迁移与写入侧 UnixMilli 改造，避免跨字段比较与前端解析再次错乱。
func TestSaveMessagePersistsMillisecondTimestamps(t *testing.T) {
	// store、cleanup 分别保存隔离的 SQLite 存储和关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// userID 保存测试账号的数据库主键，用于建立会话所属账号。
	var userID int64
	// err 表示创建测试用户并读取其数据库主键时的错误。
	if err := store.DB.QueryRowContext(ctx, `INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`, "chat-ts-owner", "chat-ts-owner@example.com", "test-hash").Scan(&userID); err != nil {
		t.Fatalf("创建测试用户: %v", err)
	}
	// cookieID 是测试聊天所属的非敏感账号标识。
	const cookieID = "chat-ts-account"
	// err 表示创建测试账号时的数据库错误。
	if err := store.Cookies.CreateOwned(ctx, cookieID, "test-cookie", userID); err != nil {
		t.Fatalf("创建测试账号: %v", err)
	}
	// session 保存本次测试消息所属的聊天会话。
	session := ChatSession{CookieID: cookieID, ChatID: "chat-ts-session", BuyerID: "buyer-1", BuyerName: "买家"}
	// message 是一条不指定发送时间的入站消息，落库时应由存储层补齐毫秒时间戳。
	message := ChatMessage{MessageKey: "ts-incoming-1", Direction: "incoming", MessageType: "text", Content: "你好"}
	// stored 是落库后的消息，用于读取 created_at 与 sent_at。
	stored, inserted, saveErr := store.Chats.SaveMessage(ctx, session, message, true)
	if saveErr != nil || !inserted {
		t.Fatalf("保存测试消息 inserted=%v err=%v", inserted, saveErr)
	}
	// createdAt 与 sentAt 是本次待校验的消息时间字段；重新读取避免依赖内存字段。
	var createdAt, sentAt int64
	// scanErr 表示读取消息时间字段时的数据库错误。
	if scanErr := store.DB.QueryRowContext(ctx,
		`SELECT created_at,sent_at FROM chat_messages WHERE cookie_id=? AND message_key=?`,
		cookieID, stored.MessageKey).Scan(&createdAt, &sentAt); scanErr != nil {
		t.Fatalf("读取消息时间字段: %v", scanErr)
	}
	if createdAt < unixMilliLowerBound {
		t.Fatalf("chat_messages.created_at 应为毫秒，实际 %d", createdAt)
	}
	if sentAt < unixMilliLowerBound {
		t.Fatalf("chat_messages.sent_at 应为毫秒，实际 %d", sentAt)
	}
	// 落库时间不应早于消息发送时间，且两者差距应在秒级以内。
	if createdAt < sentAt || createdAt-sentAt > 5000 {
		t.Fatalf("created_at(%d) 与 sent_at(%d) 应同量级且落库不早于发送", createdAt, sentAt)
	}
	// sessionUpdatedAt 是会话摘要的更新时间，同样必须是毫秒。
	var sessionCreatedAt, sessionUpdatedAt, lastMessageAt int64
	// scanErr 表示读取会话时间字段时的数据库错误。
	if scanErr := store.DB.QueryRowContext(ctx,
		`SELECT created_at,updated_at,last_message_at FROM chat_sessions WHERE cookie_id=? AND chat_id=?`,
		cookieID, session.ChatID).Scan(&sessionCreatedAt, &sessionUpdatedAt, &lastMessageAt); scanErr != nil {
		t.Fatalf("读取会话时间字段: %v", scanErr)
	}
	if sessionCreatedAt < unixMilliLowerBound {
		t.Fatalf("chat_sessions.created_at 应为毫秒，实际 %d", sessionCreatedAt)
	}
	if sessionUpdatedAt < unixMilliLowerBound {
		t.Fatalf("chat_sessions.updated_at 应为毫秒，实际 %d", sessionUpdatedAt)
	}
	if lastMessageAt < unixMilliLowerBound {
		t.Fatalf("chat_sessions.last_message_at 应为毫秒，实际 %d", lastMessageAt)
	}
}
