// ops_chat_test.go 锁定运营会话仓储的去重、分页、游标与保留清理语义。
//
// SQLite 始终运行；配置 TEST_POSTGRES_URL 时同一组用例会在一次性 Postgres 库上再跑一遍。

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestOpsChatRecordInboundDeduplicates 验证同一平台消息只写入一次，且只有新消息推进会话摘要。
func TestOpsChatRecordInboundDeduplicates(t *testing.T) {
	// target 是当前方言的测试目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的数据库目标副本。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store 是当前方言的仓储聚合入口。
			store := target.store
			// ctx 是本用例共用的数据库上下文。
			ctx := context.Background()
			// at 是第一条消息的固定毫秒时间。
			at := time.Now().Add(-2 * time.Hour).UnixMilli()
			// record 是同一条平台消息的记录输入。
			record := OpsInboundRecord{Channel: "qq", TargetOpenID: "group-1", PlatformMsgID: "m1", Content: "第一句", IsGroup: true, At: at}
			// first、inserted、firstErr 是首次写入结果。
			first, inserted, firstErr := store.OpsChat.RecordInbound(ctx, record)
			if firstErr != nil || !inserted || first.ID <= 0 {
				t.Fatalf("首次入站应新增，实际 inserted=%v msg=%+v err=%v", inserted, first, firstErr)
			}
			// second、duplicated、secondErr 是重复写入结果。
			second, duplicated, secondErr := store.OpsChat.RecordInbound(ctx, record)
			if secondErr != nil {
				t.Fatalf("重复入站不应报错: %v", secondErr)
			}
			if duplicated || second.ID != 0 {
				t.Fatalf("重复平台消息应判重，实际 inserted=%v msg=%+v", duplicated, second)
			}
			// page、listErr 是当前会话的消息页。
			page, listErr := store.OpsChat.ListOpsMessages(ctx, first.ConversationID, 0, 10)
			if listErr != nil {
				t.Fatalf("读取运营消息失败: %v", listErr)
			}
			if len(page.Messages) != 1 || page.Messages[0].PlatformMsgID != "m1" || page.Messages[0].Source != "inbound" {
				t.Fatalf("重复消息不得重复落库，实际 %+v", page.Messages)
			}
			// conversation、getErr 是会话摘要快照。
			conversation, getErr := store.OpsChat.GetConversation(ctx, first.ConversationID)
			if getErr != nil {
				t.Fatalf("读取运营会话失败: %v", getErr)
			}
			if conversation.LastMessageAt != at || conversation.LastMessagePreview != "第一句" || !conversation.IsGroup {
				t.Fatalf("会话摘要不符，实际 %+v", conversation)
			}
		})
	}
}

// TestOpsChatAppendOutboundAndPages 验证出站写入、按目标定位与会话分页。
func TestOpsChatAppendOutboundAndPages(t *testing.T) {
	// target 是当前方言的测试目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的数据库目标副本。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store 是当前方言的仓储聚合入口。
			store := target.store
			// ctx 是本用例共用的数据库上下文。
			ctx := context.Background()
			// at 是第一条入站消息时间。
			at := time.Now().Add(-time.Hour).UnixMilli()
			// inbound、_, inboundErr 是建立会话的入站消息。
			inbound, _, inboundErr := store.OpsChat.RecordInbound(ctx, OpsInboundRecord{
				TargetOpenID: "user-1", PlatformMsgID: "in-1", Content: "在吗", At: at,
			})
			if inboundErr != nil {
				t.Fatalf("建立运营会话失败: %v", inboundErr)
			}
			// message、conversation、appendErr 是出站写入结果。
			message, conversation, appendErr := store.OpsChat.AppendOutbound(ctx, inbound.ConversationID, "在的", "在的", at+1000)
			if appendErr != nil {
				t.Fatalf("写入出站消息失败: %v", appendErr)
			}
			if message.ID == 0 || message.Direction != "outbound" || message.Source != "operator" {
				t.Fatalf("出站消息字段不符，实际 %+v", message)
			}
			if conversation.LastMessageAt != at+1000 || conversation.LastMessagePreview != "在的" {
				t.Fatalf("出站消息未推进会话摘要，实际 %+v", conversation)
			}
			// byTarget、_, targetErr 是按目标写入的第二条出站消息。
			byTarget, _, targetErr := store.OpsChat.AppendOutboundByTarget(ctx, "qq", "user-1", "稍等", "稍等", at+2000)
			if targetErr != nil || byTarget.ID == 0 {
				t.Fatalf("按目标写入出站消息失败: msg=%+v err=%v", byTarget, targetErr)
			}
			if byTarget.Source != "command" {
				t.Fatalf("命令回复来源应为 command，实际 %q", byTarget.Source)
			}
			if // _, _, missingErr 是目标不存在时的写入结果。
			_, _, missingErr := store.OpsChat.AppendOutboundByTarget(ctx, "qq", "missing", "x", "x", at); !errors.Is(missingErr, ErrNotFound) {
				t.Fatalf("目标不存在应返回 ErrNotFound，实际 %v", missingErr)
			}
			// listPage、listErr 是会话分页结果。
			listPage, listErr := store.OpsChat.ListConversations(ctx, 1, 10)
			if listErr != nil {
				t.Fatalf("读取运营会话列表失败: %v", listErr)
			}
			if listPage.Total != 1 || len(listPage.Conversations) != 1 || listPage.Conversations[0].ID != inbound.ConversationID {
				t.Fatalf("会话分页结果不符，实际 %+v", listPage)
			}
			if listPage.Page != 1 || listPage.PageSize != 10 {
				t.Fatalf("会话分页参数未回填，实际 %+v", listPage)
			}
		})
	}
}

// TestOpsChatListMessagesCursor 验证 before_id 游标按 id 向前翻页并正确报告 HasMore。
func TestOpsChatListMessagesCursor(t *testing.T) {
	// target 是当前方言的测试目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的数据库目标副本。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store 是当前方言的仓储聚合入口。
			store := target.store
			// ctx 是本用例共用的数据库上下文。
			ctx := context.Background()
			// base 是递增消息时间的起点。
			base := time.Now().Add(-time.Hour).UnixMilli()
			// conversationID 是首条消息创建的会话主键。
			var conversationID int64
			// index 是当前写入的消息序号，用于生成稳定的平台消息标识。
			for index := 0; index < 5; index++ {
				// message、_, err 是当前入站消息写入结果。
				message, _, err := store.OpsChat.RecordInbound(ctx, OpsInboundRecord{
					TargetOpenID: "group-cursor", PlatformMsgID: "m-" + string(rune('a'+index)), Content: "msg", IsGroup: true, At: base + int64(index),
				})
				if err != nil {
					t.Fatalf("写入游标夹具失败: %v", err)
				}
				conversationID = message.ConversationID
			}
			// latest、latestErr 是最新一页消息。
			latest, latestErr := store.OpsChat.ListOpsMessages(ctx, conversationID, 0, 2)
			if latestErr != nil {
				t.Fatalf("读取最新消息页失败: %v", latestErr)
			}
			if len(latest.Messages) != 2 || !latest.HasMore {
				t.Fatalf("最新页应为 2 条且有更早消息，实际 %+v", latest)
			}
			// 倒序取 limit+1 后反转：最新页为正序的最后两条。
			if latest.Messages[0].CreatedAt > latest.Messages[1].CreatedAt {
				t.Fatalf("消息应保持时间正序，实际 %+v", latest.Messages)
			}
			// older、olderErr 是以最新页首条为游标的更早消息页。
			older, olderErr := store.OpsChat.ListOpsMessages(ctx, conversationID, latest.Messages[0].ID, 2)
			if olderErr != nil {
				t.Fatalf("读取更早消息页失败: %v", olderErr)
			}
			if len(older.Messages) != 2 || !older.HasMore {
				t.Fatalf("中间页应为 2 条且有更早消息，实际 %+v", older)
			}
			// oldest、oldestErr 是最后一页消息。
			oldest, oldestErr := store.OpsChat.ListOpsMessages(ctx, conversationID, older.Messages[0].ID, 2)
			if oldestErr != nil {
				t.Fatalf("读取最后一页失败: %v", oldestErr)
			}
			if len(oldest.Messages) != 1 || oldest.HasMore {
				t.Fatalf("最后一页应为 1 条且无更多，实际 %+v", oldest)
			}
		})
	}
}

// TestOpsChatGetConversationNotFound 验证不存在的会话返回统一 ErrNotFound。
func TestOpsChatGetConversationNotFound(t *testing.T) {
	// target 是当前方言的测试目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的数据库目标副本。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			if // _, err 是不存在会话的读取结果。
			_, err := target.store.OpsChat.GetConversation(context.Background(), 999999); !errors.Is(err, ErrNotFound) {
				t.Fatalf("不存在的会话应返回 ErrNotFound，实际 %v", err)
			}
		})
	}
}

// TestCleanupRetentionOpsChat 验证保留清理先删超期消息，再删无消息的超期会话。
func TestCleanupRetentionOpsChat(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// expired 是超出保留期的毫秒时间点。
	expired := time.Now().AddDate(0, 0, -40).UnixMilli()
	// fresh 是保留期内的毫秒时间点。
	fresh := time.Now().AddDate(0, 0, -1).UnixMilli()
	// oldWithMessage、_, err 是带超期消息的会话。
	oldWithMessage, _, err := s.OpsChat.RecordInbound(ctx, OpsInboundRecord{TargetOpenID: "old-msg", PlatformMsgID: "m-old", Content: "旧消息", At: expired})
	if err != nil {
		t.Fatalf("写入超期运营消息失败: %v", err)
	}
	// freshConversation、_, freshErr 是保留期内的会话。
	freshConversation, _, freshErr := s.OpsChat.RecordInbound(ctx, OpsInboundRecord{TargetOpenID: "fresh", PlatformMsgID: "m-fresh", Content: "新消息", At: fresh})
	if freshErr != nil {
		t.Fatalf("写入近期运营消息失败: %v", freshErr)
	}
	// err 是插入超期空会话夹具的失败原因。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ops_conversations (channel,target_openid,is_group,last_message_at,last_message_preview,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?)`, "qq", "old-empty", 0, expired, "", expired, expired); err != nil {
		t.Fatalf("插入超期空会话失败: %v", err)
	}
	// report、retentionErr 是本次清理报告及失败原因。
	report, retentionErr := s.CleanupRetention(ctx, 30)
	if retentionErr != nil {
		t.Fatalf("CleanupRetention: %v", retentionErr)
	}
	if report.OpsMessages != 1 || report.OpsConversations != 2 {
		t.Fatalf("运营清理行数异常: %+v", report)
	}
	countRows(t, s, `SELECT COUNT(*) FROM ops_messages`, 1, "仅保留近期运营消息")
	countRows(t, s, `SELECT COUNT(*) FROM ops_conversations`, 1, "仅保留近期运营会话")
	countRows(t, s, `SELECT COUNT(*) FROM ops_conversations WHERE target_openid='old-msg'`, 0, "超期会话应删除")
	// 旧会话主键从报告中已无法反查，直接断言其不存在。
	if oldWithMessage.ConversationID == freshConversation.ConversationID {
		t.Fatal("测试夹具不得共用会话主键")
	}
}
