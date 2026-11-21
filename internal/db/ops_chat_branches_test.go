// ops_chat_branches_test.go 覆盖运营会话仓储的未初始化守卫、参数默认值与空输入分支。
package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestOpsChatStorageGuards 验证所有仓储方法在未初始化时返回错误而不是 panic。
func TestOpsChatStorageGuards(t *testing.T) {
	// nilStore 是显式的空仓储指针，用于覆盖未初始化守卫。
	var nilStore *OpsChatStore
	// ctx 是本用例共用的数据库上下文。
	ctx := context.Background()
	// _, _, err 是空仓储的入站写入结果。
	if _, _, err := nilStore.RecordInbound(ctx, OpsInboundRecord{UserID: 1, TargetOpenID: "target"}); err == nil {
		t.Fatal("空仓储写入入站消息应返回错误")
	}
	// _, _, err 是空仓储的出站写入结果。
	if _, _, err := nilStore.AppendOutbound(ctx, 1, 1, "内容", "内容", 0); err == nil {
		t.Fatal("空仓储写入出站消息应返回错误")
	}
	// _, _, err 是空仓储按目标写入的结果。
	if _, _, err := nilStore.AppendOutboundByTarget(ctx, 1, "qq", "target", "内容", "内容", 0); err == nil {
		t.Fatal("空仓储按目标写入应返回错误")
	}
	// _, err 是空仓储读取会话的结果。
	if _, err := nilStore.GetConversation(ctx, 1, 1); err == nil {
		t.Fatal("空仓储读取会话应返回错误")
	}
	// _, err 是空仓储读取会话列表的结果。
	if _, err := nilStore.ListConversations(ctx, 1, 1, 10); err == nil {
		t.Fatal("空仓储读取会话列表应返回错误")
	}
	// _, err 是空仓储读取消息页的结果。
	if _, err := nilStore.ListOpsMessages(ctx, 1, 1, 0, 10); err == nil {
		t.Fatal("空仓储读取消息页应返回错误")
	}
}

// TestOpsChatInputDefaults 验证空目标、零时间与非法分页参数按仓储约定收敛。
func TestOpsChatInputDefaults(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本用例共用的数据库上下文。
	ctx := context.Background()
	// _, _, err 是缺少目标 openid 的入站写入结果。
	if _, _, err := s.OpsChat.RecordInbound(ctx, OpsInboundRecord{UserID: 1, TargetOpenID: "   "}); err == nil {
		t.Fatal("缺少目标 openid 应返回错误")
	}
	// message、inserted、recordErr 是零时间入站记录的写入结果；仓储应回退本机时间。
	message, inserted, recordErr := s.OpsChat.RecordInbound(ctx, OpsInboundRecord{UserID: 1, TargetOpenID: "user-default", Content: "无时间"})
	if recordErr != nil || !inserted || message.ConversationID <= 0 {
		t.Fatalf("零时间入站写入异常: inserted=%v message=%+v err=%v", inserted, message, recordErr)
	}
	// conversation、getErr 是零时间记录的会话快照。
	conversation, getErr := s.OpsChat.GetConversation(ctx, 1, message.ConversationID)
	if getErr != nil || conversation.LastMessageAt <= 0 {
		t.Fatalf("零时间入站未回退本机时间: conversation=%+v err=%v", conversation, getErr)
	}
	// _, _, invalidIDErr 是非法会话主键的出站写入结果。
	if _, _, invalidIDErr := s.OpsChat.AppendOutbound(ctx, 1, 0, "内容", "内容", 0); !errors.Is(invalidIDErr, ErrNotFound) {
		t.Fatalf("非法会话主键应返回 ErrNotFound，实际 %v", invalidIDErr)
	}
	// outbound、_, zeroTimeErr 是零时间出站写入结果；仓储应回退本机时间。
	outbound, _, zeroTimeErr := s.OpsChat.AppendOutbound(ctx, 1, message.ConversationID, "回复", "回复", 0)
	if zeroTimeErr != nil || outbound.CreatedAt <= 0 {
		t.Fatalf("零时间出站写入异常: message=%+v err=%v", outbound, zeroTimeErr)
	}
	// _, _, emptyTargetErr 是按空白目标写入的出站结果。
	if _, _, emptyTargetErr := s.OpsChat.AppendOutboundByTarget(ctx, 1, "qq", "  ", "内容", "内容", 0); !errors.Is(emptyTargetErr, ErrNotFound) {
		t.Fatalf("空白目标应返回 ErrNotFound，实际 %v", emptyTargetErr)
	}
	// page、pageErr 是非法分页参数收敛后的会话页。
	page, pageErr := s.OpsChat.ListConversations(ctx, 1, 0, 0)
	if pageErr != nil || page.Page != 1 || page.PageSize != 20 {
		t.Fatalf("非法分页参数未收敛: page=%+v err=%v", page, pageErr)
	}
	if page.Total < 1 || len(page.Conversations) < 1 {
		t.Fatalf("会话列表应至少包含当前会话: %+v", page)
	}
	// messages、messageErr 是零 limit 的消息页；仓储应按默认 50 条读取。
	messages, messageErr := s.OpsChat.ListOpsMessages(ctx, 1, message.ConversationID, 0, 0)
	if messageErr != nil || len(messages.Messages) < 1 {
		t.Fatalf("零 limit 消息页异常: page=%+v err=%v", messages, messageErr)
	}
}

// TestOpsChatStorageFailure 验证数据库关闭后各仓储方法返回错误而不是静默成功。
func TestOpsChatStorageFailure(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本用例共用的数据库上下文。
	ctx := context.Background()
	// fixture、_, fixtureErr 是关闭数据库前创建的会话夹具。
	fixture, _, fixtureErr := s.OpsChat.RecordInbound(ctx, OpsInboundRecord{UserID: 1, TargetOpenID: "user-failure", Content: "夹具", At: 1})
	if fixtureErr != nil {
		t.Fatalf("创建失败用例夹具: %v", fixtureErr)
	}
	// closeErr 是关闭测试数据库的失败原因。
	if closeErr := s.DB.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库: %v", closeErr)
	}
	// _, _, recordClosedErr 是数据库关闭后的入站写入结果。
	if _, _, recordClosedErr := s.OpsChat.RecordInbound(ctx, OpsInboundRecord{UserID: 1, TargetOpenID: "closed", Content: "写入"}); recordClosedErr == nil {
		t.Fatal("数据库关闭后入站写入应返回错误")
	}
	// _, _, outboundClosedErr 是数据库关闭后的出站写入结果。
	if _, _, outboundClosedErr := s.OpsChat.AppendOutbound(ctx, 1, fixture.ConversationID, "内容", "内容", 1); outboundClosedErr == nil {
		t.Fatal("数据库关闭后出站写入应返回错误")
	}
	// _, _, targetClosedErr 是数据库关闭后的按目标写入结果。
	if _, _, targetClosedErr := s.OpsChat.AppendOutboundByTarget(ctx, 1, "qq", "user-failure", "内容", "内容", 1); targetClosedErr == nil {
		t.Fatal("数据库关闭后按目标写入应返回错误")
	}
	// _, getClosedErr 是数据库关闭后的会话读取结果。
	if _, getClosedErr := s.OpsChat.GetConversation(ctx, 1, fixture.ConversationID); getClosedErr == nil {
		t.Fatal("数据库关闭后会话读取应返回错误")
	}
	// _, listClosedErr 是数据库关闭后的会话列表结果。
	if _, listClosedErr := s.OpsChat.ListConversations(ctx, 1, 1, 10); listClosedErr == nil {
		t.Fatal("数据库关闭后会话列表应返回错误")
	}
	// _, messagesClosedErr 是数据库关闭后的消息页结果。
	if _, messagesClosedErr := s.OpsChat.ListOpsMessages(ctx, 1, fixture.ConversationID, 0, 10); messagesClosedErr == nil {
		t.Fatal("数据库关闭后消息页应返回错误")
	}
}

// TestOpsPreviewFromContent 验证会话预览按字符截断，且非正数上限使用默认 60 字符。
func TestOpsPreviewFromContent(t *testing.T) {
	// short 是无需截断的短正文。
	short := "短消息"
	// shortPreview 是短正文的预览结果。
	shortPreview := opsPreviewFromContent(short, 0)
	if shortPreview != short {
		t.Fatalf("短正文不应截断，实际 %q", shortPreview)
	}
	// long 是超过默认上限的中文正文。
	long := strings.Repeat("字", 70)
	// defaultPreview 是默认上限下的预览结果。
	defaultPreview := opsPreviewFromContent(long, 0)
	if len([]rune(defaultPreview)) != 60 {
		t.Fatalf("默认预览应为 60 字符，实际 %d", len([]rune(defaultPreview)))
	}
	// customPreview 是自定义上限下的预览结果。
	customPreview := opsPreviewFromContent(long, 5)
	if customPreview != "字字字字字" {
		t.Fatalf("自定义预览长度异常，实际 %q", customPreview)
	}
}
