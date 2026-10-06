// tools_chat_test.go 覆盖聊天域工具：会话与消息分页、发送（成功/不确定/越权）、
// 图片输入校验（base64 上限、非法 URL、内网地址拒绝）、confirm 守卫与关键错误归一。

package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// fakeChatPorts 是聊天域假端口，记录调用并支持按用例注入错误与结果。
type fakeChatPorts struct {
	// sessionPage 是会话分页返回。
	sessionPage chatapp.SessionPage
	// session 是单会话读取返回。
	session chatapp.Session
	// storedPage 是库内消息分页返回。
	storedPage chatapp.Page
	// conversationPage 是联系人刷新返回。
	conversationPage chatapp.ConversationPage
	// historyPage 是历史刷新返回。
	historyPage chatapp.HistoryPage
	// sentMessage 是发送成功返回的消息。
	sentMessage chatapp.Message
	// itemPage 是聊天商品分页返回。
	itemPage chatapp.ChatItemPage
	// quickReplies 是快捷回复列表返回。
	quickReplies []chatapp.QuickReply
	// buyerNote 是买家备注返回。
	buyerNote chatapp.BuyerNote
	// imageData、imageContentType 是受控下载返回的图片内容。
	imageData        []byte
	imageContentType string
	// listErr、findErr、storedErr、refreshContactsErr、refreshHistoryErr 是读取类错误。
	listErr, findErr, storedErr, refreshContactsErr, refreshHistoryErr error
	// sendTextErr、sendImageErr、markReadErr、deleteErr 是写入类错误。
	sendTextErr, sendImageErr, markReadErr, deleteErr error
	// fetchErr 是图片下载错误。
	fetchErr error
	// quickListErr、quickAddErr、quickDeleteErr、noteGetErr、noteSaveErr、itemErr 是元数据类错误。
	quickListErr, quickAddErr, quickDeleteErr, noteGetErr, noteSaveErr, itemErr error
	// listCalls、findCalls、storedCalls、refreshContactsCalls、refreshHistoryCalls、sendTextCalls、sendImageCalls、markReadCalls、deleteCalls 是调用计数。
	listCalls, findCalls, storedCalls, refreshContactsCalls, refreshHistoryCalls int
	sendTextCalls, sendImageCalls, markReadCalls, deleteCalls                    int
	// fetchCalls、quickListCalls、quickAddCalls、quickDeleteCalls、noteGetCalls、noteSaveCalls、itemCalls 是调用计数。
	fetchCalls, quickListCalls, quickAddCalls, quickDeleteCalls, noteGetCalls, noteSaveCalls, itemCalls int
	// listCursor、listLimit 是会话分页收到的游标与页大小。
	listCursor *chatapp.SessionCursor
	listLimit  int
	// textInput、imageInput 是发送用例收到的入参。
	textInput  chatapp.OutgoingInput
	imageInput chatapp.ImageInput
	// itemQuery 是聊天商品查询收到的入参。
	itemQuery chatapp.ChatItemQuery
	// savedNoteContent 是买家备注保存收到的正文。
	savedNoteContent string
}

// ListSessionPage 记录游标与页大小并回传预设分页。
func (f *fakeChatPorts) ListSessionPage(_ context.Context, _ int64, _ string, cursor *chatapp.SessionCursor, limit int) (chatapp.SessionPage, error) {
	f.listCalls++
	f.listCursor, f.listLimit = cursor, limit
	return f.sessionPage, f.listErr
}

// FindSession 回传预设会话。
func (f *fakeChatPorts) FindSession(context.Context, int64, string, string) (chatapp.Session, error) {
	f.findCalls++
	return f.session, f.findErr
}

// ListStoredMessages 回传预设消息分页。
func (f *fakeChatPorts) ListStoredMessages(context.Context, int64, string, string, int64, int) (chatapp.Page, error) {
	f.storedCalls++
	return f.storedPage, f.storedErr
}

// RefreshConversations 回传预设联系人刷新结果。
func (f *fakeChatPorts) RefreshConversations(context.Context, string, int64, int) (chatapp.ConversationPage, error) {
	f.refreshContactsCalls++
	return f.conversationPage, f.refreshContactsErr
}

// RefreshHistory 回传预设历史刷新结果。
func (f *fakeChatPorts) RefreshHistory(context.Context, string, string, int64, int, chatapp.Session) (chatapp.HistoryPage, error) {
	f.refreshHistoryCalls++
	return f.historyPage, f.refreshHistoryErr
}

// SendText 记录文本入参并回传预设结果。
func (f *fakeChatPorts) SendText(_ context.Context, input chatapp.OutgoingInput) (*chatapp.Message, error) {
	f.sendTextCalls++
	f.textInput = input
	if f.sendTextErr != nil {
		return nil, f.sendTextErr
	}
	// message 是回传的本地消息副本。
	message := f.sentMessage
	return &message, nil
}

// SendImage 记录图片入参并回传预设结果。
func (f *fakeChatPorts) SendImage(_ context.Context, input chatapp.ImageInput) (*chatapp.Message, error) {
	f.sendImageCalls++
	f.imageInput = input
	if f.sendImageErr != nil {
		return nil, f.sendImageErr
	}
	// message 是回传的本地消息副本。
	message := f.sentMessage
	return &message, nil
}

// MarkRead 记录已读调用。
func (f *fakeChatPorts) MarkRead(context.Context, int64, string, string) error {
	f.markReadCalls++
	return f.markReadErr
}

// DeleteConversation 记录删除调用。
func (f *fakeChatPorts) DeleteConversation(context.Context, int64, string, string) error {
	f.deleteCalls++
	return f.deleteErr
}

// FetchChatImage 记录下载调用并回传预设图片内容。
func (f *fakeChatPorts) FetchChatImage(context.Context, string) ([]byte, string, error) {
	f.fetchCalls++
	if f.fetchErr != nil {
		return nil, "", f.fetchErr
	}
	return f.imageData, f.imageContentType, nil
}

// ListQuickReplies 回传预设快捷回复列表。
func (f *fakeChatPorts) ListQuickReplies(context.Context, int64, string) ([]chatapp.QuickReply, error) {
	f.quickListCalls++
	return f.quickReplies, f.quickListErr
}

// CreateQuickReply 回传新建的快捷回复。
func (f *fakeChatPorts) CreateQuickReply(_ context.Context, _ int64, accountID, content string) (chatapp.QuickReply, error) {
	f.quickAddCalls++
	if f.quickAddErr != nil {
		return chatapp.QuickReply{}, f.quickAddErr
	}
	return chatapp.QuickReply{ID: 12, AccountID: accountID, Content: content, CreatedAt: 1700000000}, nil
}

// DeleteQuickReply 记录删除调用。
func (f *fakeChatPorts) DeleteQuickReply(context.Context, int64, string, int64) error {
	f.quickDeleteCalls++
	return f.quickDeleteErr
}

// GetBuyerNote 回传预设买家备注。
func (f *fakeChatPorts) GetBuyerNote(context.Context, int64, string, string) (chatapp.BuyerNote, error) {
	f.noteGetCalls++
	return f.buyerNote, f.noteGetErr
}

// SaveBuyerNote 记录保存的备注正文。
func (f *fakeChatPorts) SaveBuyerNote(_ context.Context, _ int64, accountID, buyerID, content string) (chatapp.BuyerNote, error) {
	f.noteSaveCalls++
	f.savedNoteContent = content
	if f.noteSaveErr != nil {
		return chatapp.BuyerNote{}, f.noteSaveErr
	}
	return chatapp.BuyerNote{AccountID: accountID, BuyerID: buyerID, Content: content, UpdatedAt: 1700000000}, nil
}

// ListChatItems 记录查询入参并回传预设商品分页。
func (f *fakeChatPorts) ListChatItems(_ context.Context, input chatapp.ChatItemQuery) (chatapp.ChatItemPage, error) {
	f.itemCalls++
	f.itemQuery = input
	return f.itemPage, f.itemErr
}

// newChatToolEndpoint 构造注册聊天工具的端点、假端口与身份上下文。
func newChatToolEndpoint(t *testing.T, fake *fakeChatPorts) (*Endpoint, context.Context) {
	t.Helper()
	// endpoint、_ 是协议端点。
	endpoint, _ := newProtocolEndpoint(t)
	endpoint.RegisterChatTools(fake)
	// ctx 是携带固定管理员身份（用户 1、持久化令牌）的调用上下文。
	ctx := withIdentity(context.Background(), &CallIdentity{UserID: 1, Source: SourcePersisted})
	return endpoint, ctx
}

// testChatSession 返回可用于发送用例的归属会话。
func testChatSession() chatapp.Session {
	return chatapp.Session{AccountID: "acc1", ChatID: "chat1", PeerUserID: "buyer1", PeerName: "买家"}
}

// TestChatSessionAndMessageViews 验证会话分页、游标回传、消息分页与聊天商品查询映射。
func TestChatSessionAndMessageViews(t *testing.T) {
	// fake 是聊天域假端口。
	fake := &fakeChatPorts{
		sessionPage: chatapp.SessionPage{
			Sessions: []chatapp.Session{{AccountID: "acc1", ChatID: "chat1", PeerUserID: "buyer1", PeerName: "买家", UnreadCount: 2, LastMessageAt: 1700000000000}},
			HasMore:  true,
			NextCursor: &chatapp.SessionCursor{
				LastMessageAt: 1700000000000, ChatID: "chat1",
			},
		},
		storedPage: chatapp.Page{
			Messages: []chatapp.Message{{ID: 9, AccountID: "acc1", ChatID: "chat1", Direction: "incoming", MessageType: "text", Content: "在吗", SentAt: 1700000000000}},
			Session:  testChatSession(),
			HasMore:  false,
		},
		itemPage: chatapp.ChatItemPage{Items: []chatapp.ChatItem{{ItemID: "i1", Title: "商品A", Price: "10.00"}}, Page: 1},
	}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// sessionResult 是会话分页结果。
	sessionResult := invoke(endpoint, ctx, "chat_session_list", map[string]any{
		"account_id": "acc1", "cursor_last_message_at": 1700000000000.0, "cursor_chat_id": "chat0", "limit": 10.0,
	})
	if sessionResult.IsError {
		t.Fatalf("会话分页失败: %s", resultJSON(t, sessionResult))
	}
	// sessionText 是会话分页 JSON，必须含游标、未读数与 has_more。
	sessionText := resultJSON(t, sessionResult)
	// fragment 是会话分页响应中必须出现的片段。
	for _, fragment := range []string{`"has_more":true`, `"unread_count":2`, `"next_cursor":{"last_message_at":1700000000000,"chat_id":"chat1"}`} {
		if !strings.Contains(sessionText, fragment) {
			t.Fatalf("会话分页缺少 %s: %s", fragment, sessionText)
		}
	}
	if fake.listCursor == nil || fake.listCursor.ChatID != "chat0" || fake.listLimit != 10 {
		t.Fatalf("会话游标透传异常: cursor=%+v limit=%d", fake.listCursor, fake.listLimit)
	}
	// messageResult 是库内消息分页结果。
	messageResult := invoke(endpoint, ctx, "chat_message_list", map[string]any{"account_id": "acc1", "chat_id": "chat1", "limit": 5.0})
	if messageResult.IsError {
		t.Fatalf("消息分页失败: %s", resultJSON(t, messageResult))
	}
	// messageText 是消息分页 JSON，必须含消息内容与会话摘要。
	messageText := resultJSON(t, messageResult)
	if !strings.Contains(messageText, `"content":"在吗"`) || !strings.Contains(messageText, `"peer_name":"买家"`) {
		t.Fatalf("消息分页映射异常: %s", messageText)
	}
	// itemResult 是聊天商品查询结果。
	itemResult := invoke(endpoint, ctx, "chat_item_list", map[string]any{"account_id": "acc1", "chat_id": "chat1", "role": "peer", "query": "帽"})
	if itemResult.IsError || !strings.Contains(resultJSON(t, itemResult), `"item_id":"i1"`) {
		t.Fatalf("聊天商品查询异常: %s", resultJSON(t, itemResult))
	}
	if fake.itemQuery.Role != "peer" || fake.itemQuery.Query != "帽" || fake.itemQuery.UserID != 1 {
		t.Fatalf("聊天商品查询入参异常: %+v", fake.itemQuery)
	}
	// 非法 role 由应用服务拒绝，错误归一为中文参数提示且不再产生额外查询。
	fake.itemErr = chatapp.ErrChatItemInvalid
	// badRole 是非法 role 的查询结果。
	badRole := invoke(endpoint, ctx, "chat_item_list", map[string]any{"account_id": "acc1", "chat_id": "chat1", "role": "other"})
	if !badRole.IsError || !strings.Contains(resultJSON(t, badRole), "聊天参数无效") || fake.itemCalls != 2 {
		t.Fatalf("非法 role 必须被拒绝: %s", resultJSON(t, badRole))
	}
	fake.itemErr = nil
}

// TestChatSendTextRequiresConfirmAndMapsSession 验证文本发送的 confirm 守卫、归属会话解析与入参透传。
func TestChatSendTextRequiresConfirmAndMapsSession(t *testing.T) {
	// fake 是聊天域假端口。
	fake := &fakeChatPorts{
		session:     testChatSession(),
		sentMessage: chatapp.Message{ID: 21, AccountID: "acc1", ChatID: "chat1", Direction: "outgoing", MessageType: "text", Content: "您好", Status: "sent", SentAt: 1700000000000},
	}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// denied 是缺 confirm 的发送。
	denied := invoke(endpoint, ctx, "chat_send_text", map[string]any{"account_id": "acc1", "chat_id": "chat1", "text": "您好"})
	if !denied.IsError || fake.sendTextCalls != 0 {
		t.Fatal("缺 confirm 发送文本必须拦截且零触达")
	}
	// sent 是显式确认后的发送。
	sent := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "text": "您好", "confirm": true,
	})
	if sent.IsError {
		t.Fatalf("发送文本失败: %s", resultJSON(t, sent))
	}
	if fake.sendTextCalls != 1 || fake.textInput.Text != "您好" {
		t.Fatalf("发送入参异常: %+v", fake.textInput)
	}
	// session 必须带对端标识，否则应用层无法发送。
	if fake.textInput.Session.PeerUserID != "buyer1" || fake.textInput.Session.ChatID != "chat1" {
		t.Fatalf("发送会话解析异常: %+v", fake.textInput.Session)
	}
	// sentText 是发送结果 JSON，必须回传本地消息状态。
	if !strings.Contains(resultJSON(t, sent), `"status":"sent"`) {
		t.Fatalf("发送结果映射异常: %s", resultJSON(t, sent))
	}
	// 会话不存在时返回可操作的中文提示且零发送。
	fake.session = chatapp.Session{AccountID: "acc1", ChatID: "chat2"}
	// missing 是会话缺少对端身份时的发送结果。
	missing := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc1", "chat_id": "chat2", "text": "在吗", "confirm": true,
	})
	if !strings.Contains(resultJSON(t, missing), "会话不存在或尚未同步对端身份") || fake.sendTextCalls != 1 {
		t.Fatalf("缺失会话必须拒绝且零发送: %s", resultJSON(t, missing))
	}
	// 缺少消息内容必须返回参数错误。
	empty := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "confirm": true,
	})
	if !empty.IsError || fake.sendTextCalls != 1 {
		t.Fatal("缺少消息内容必须零触达")
	}
}

// TestChatSendUncertainRequiresManualReview 验证结果不确定时返回人工核对提示且不重复发送（TR-9.1）。
func TestChatSendUncertainRequiresManualReview(t *testing.T) {
	// fake 是注入不确定错误的聊天域假端口。
	fake := &fakeChatPorts{
		session: testChatSession(),
		// sendTextErr 按应用层实际形态包装不确定错误，保证 errors.Is 可识别。
		sendTextErr: fmt.Errorf("%w: %v", chatapp.ErrSendUncertain, errors.New("websocket write timeout")),
	}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// result 是不确定结果的发送调用。
	result := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "text": "您好", "confirm": true,
	})
	if !result.IsError || fake.sendTextCalls != 1 {
		t.Fatalf("不确定结果必须只发送一次: %s", resultJSON(t, result))
	}
	// text 是错误提示，必须要求人工核对且不暴露内部实现细节。
	text := resultJSON(t, result)
	if !strings.Contains(text, "核对") || strings.Contains(text, "websocket") {
		t.Fatalf("不确定结果提示异常: %s", text)
	}
	// 本地状态保存失败同样要求人工核对。
	fake.sendTextErr = chatapp.ErrStatusSave
	// statusResult 是状态保存失败的发送结果。
	statusResult := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "text": "您好", "confirm": true,
	})
	if !strings.Contains(resultJSON(t, statusResult), "人工核对") {
		t.Fatalf("状态保存失败提示异常: %s", resultJSON(t, statusResult))
	}
	// 越权发送必须归一为无权访问。
	fake.sendTextErr = chatapp.ErrSessionForbidden
	// forbidden 是越权发送结果。
	forbidden := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc9", "chat_id": "chat1", "text": "您好", "confirm": true,
	})
	if !strings.Contains(resultJSON(t, forbidden), "无权访问该聊天账号") {
		t.Fatalf("越权发送映射异常: %s", resultJSON(t, forbidden))
	}
	// 账号离线时提示先确认账号运行状态。
	fake.sendTextErr = chatapp.ErrOffline
	// offline 是离线发送结果。
	offline := invoke(endpoint, ctx, "chat_send_text", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "text": "您好", "confirm": true,
	})
	if !strings.Contains(resultJSON(t, offline), "当前离线") {
		t.Fatalf("离线发送映射异常: %s", resultJSON(t, offline))
	}
}

// TestChatSendImageInputValidation 验证图片发送的来源互斥、base64 上限与非法地址拒绝（TR-9.2）。
func TestChatSendImageInputValidation(t *testing.T) {
	// fake 是聊天域假端口。
	fake := &fakeChatPorts{
		session:          testChatSession(),
		sentMessage:      chatapp.Message{ID: 31, AccountID: "acc1", ChatID: "chat1", MessageType: "image", Status: "sent"},
		imageData:        []byte("\x89PNG\r\n\x1a\nimage-bytes"),
		imageContentType: "image/png",
	}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// urlSent 是公网 URL 图片发送结果。
	urlSent := invoke(endpoint, ctx, "chat_send_image", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "image_url": "https://example.com/a.png", "confirm": true,
	})
	if urlSent.IsError {
		t.Fatalf("URL 图片发送失败: %s", resultJSON(t, urlSent))
	}
	if fake.fetchCalls != 1 || fake.imageInput.ContentType != "image/png" || !strings.HasSuffix(fake.imageInput.Filename, ".png") {
		t.Fatalf("URL 图片入参异常: fetch=%d input=%+v", fake.fetchCalls, fake.imageInput)
	}
	// 内网地址由受控下载拒绝，错误不得回显原始地址。
	fake.fetchErr = errors.New("下载图片失败: http://10.0.0.5/a.png")
	// internalURL 是内网图片地址发送结果。
	internalURL := invoke(endpoint, ctx, "chat_send_image", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "image_url": "http://10.0.0.5/a.png", "confirm": true,
	})
	// internalText 是拒绝提示，必须为中文且不含原始地址。
	internalText := resultJSON(t, internalURL)
	if !internalURL.IsError || !strings.Contains(internalText, "公网可访问") || strings.Contains(internalText, "10.0.0.5") {
		t.Fatalf("内网图片地址拒绝异常: %s", internalText)
	}
	if fake.sendImageCalls != 1 {
		t.Fatalf("被拒绝的图片不得触达发送: %d", fake.sendImageCalls)
	}
	// 合法 base64 图片可发送。
	// encoded 是 1x1 PNG 的 base64 编码。
	encoded := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nimage-bytes"))
	// base64Sent 是 base64 图片发送结果。
	base64Sent := invoke(endpoint, ctx, "chat_send_image", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "image_base64": encoded, "confirm": true,
	})
	if base64Sent.IsError || fake.sendImageCalls != 2 {
		t.Fatalf("base64 图片发送失败: %s", resultJSON(t, base64Sent))
	}
	// 超过 10 MiB 的 base64 必须被拒绝且零触达。
	// oversized 是超过上限的 base64 内容。
	oversized := base64.StdEncoding.EncodeToString(make([]byte, chatImageMaxBytes+1))
	// overResult 是超限图片发送结果。
	overResult := invoke(endpoint, ctx, "chat_send_image", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "image_base64": oversized, "confirm": true,
	})
	if !strings.Contains(resultJSON(t, overResult), "不超过 10 MiB") || fake.sendImageCalls != 2 {
		t.Fatalf("超限图片必须拒绝且零触达: %s", resultJSON(t, overResult))
	}
	// 非图片 base64 内容必须被拒绝。
	// notImage 是伪装成图片的文本内容。
	notImage := base64.StdEncoding.EncodeToString([]byte("this is not an image at all"))
	// notImageResult 是非图片内容发送结果。
	notImageResult := invoke(endpoint, ctx, "chat_send_image", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "image_base64": notImage, "confirm": true,
	})
	if !strings.Contains(resultJSON(t, notImageResult), "不是有效图片") || fake.sendImageCalls != 2 {
		t.Fatalf("非图片内容必须拒绝: %s", resultJSON(t, notImageResult))
	}
	// 同时提交两种来源必须被拒绝。
	// bothResult 是来源冲突的发送结果。
	bothResult := invoke(endpoint, ctx, "chat_send_image", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "image_url": "https://example.com/a.png",
		"image_base64": encoded, "confirm": true,
	})
	if !strings.Contains(resultJSON(t, bothResult), "只能二选一") || fake.sendImageCalls != 2 {
		t.Fatalf("来源冲突必须拒绝: %s", resultJSON(t, bothResult))
	}
}

// TestChatRefreshRequiresConfirm 验证平台刷新类工具缺 confirm 零触达，确认后透传游标。
func TestChatRefreshRequiresConfirm(t *testing.T) {
	// fake 是聊天域假端口。
	fake := &fakeChatPorts{
		session:          testChatSession(),
		conversationPage: chatapp.ConversationPage{HasMore: true, NextCursor: 42},
		historyPage: chatapp.HistoryPage{
			Messages: []chatapp.Message{{ID: 7, AccountID: "acc1", ChatID: "chat1", MessageType: "text", Content: "你好"}},
			Session:  testChatSession(), HasMore: false, NextCursor: 8,
		},
	}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// contactsDenied、historyDenied 是缺 confirm 的刷新。
	contactsDenied := invoke(endpoint, ctx, "chat_session_refresh_contacts", map[string]any{"account_id": "acc1"})
	// historyDenied 是缺 confirm 的历史刷新。
	historyDenied := invoke(endpoint, ctx, "chat_history_refresh", map[string]any{"account_id": "acc1", "chat_id": "chat1"})
	if !contactsDenied.IsError || !historyDenied.IsError || fake.refreshContactsCalls != 0 || fake.refreshHistoryCalls != 0 {
		t.Fatal("缺 confirm 的平台刷新必须零触达")
	}
	// contacts 是确认后的联系人刷新。
	contacts := invoke(endpoint, ctx, "chat_session_refresh_contacts", map[string]any{
		"account_id": "acc1", "cursor": 7.0, "confirm": true,
	})
	if contacts.IsError || fake.refreshContactsCalls != 1 || !strings.Contains(resultJSON(t, contacts), `"next_cursor":42`) {
		t.Fatalf("联系人刷新异常: %s", resultJSON(t, contacts))
	}
	// history 是确认后的历史刷新。
	history := invoke(endpoint, ctx, "chat_history_refresh", map[string]any{
		"account_id": "acc1", "chat_id": "chat1", "confirm": true,
	})
	if history.IsError || fake.refreshHistoryCalls != 1 || !strings.Contains(resultJSON(t, history), `"refreshed":1`) {
		t.Fatalf("历史刷新异常: %s", resultJSON(t, history))
	}
	// 刷新返回的消息与会话必须脱敏且可读。
	historyText := resultJSON(t, history)
	if !strings.Contains(historyText, `"content":"你好"`) || !strings.Contains(historyText, `"peer_user_id":"buyer1"`) {
		t.Fatalf("历史刷新映射异常: %s", historyText)
	}
}

// TestChatMarkReadAndDelete 验证标记已读与删除会话的映射、confirm 守卫与错误归一。
func TestChatMarkReadAndDelete(t *testing.T) {
	// fake 是聊天域假端口。
	fake := &fakeChatPorts{}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// read 是标记已读结果。
	read := invoke(endpoint, ctx, "chat_mark_read", map[string]any{"account_id": "acc1", "chat_id": "chat1"})
	if read.IsError || fake.markReadCalls != 1 {
		t.Fatalf("标记已读异常: %s", resultJSON(t, read))
	}
	if !strings.Contains(resultJSON(t, read), `"applied":true`) {
		t.Fatalf("标记已读缺少生效标记: %s", resultJSON(t, read))
	}
	// denied 是缺 confirm 的会话删除。
	denied := invoke(endpoint, ctx, "chat_session_delete", map[string]any{"account_id": "acc1", "chat_id": "chat1"})
	if !denied.IsError || fake.deleteCalls != 0 {
		t.Fatal("缺 confirm 删除会话必须零触达")
	}
	// allowed 是显式确认后的删除。
	allowed := invoke(endpoint, ctx, "chat_session_delete", map[string]any{"account_id": "acc1", "chat_id": "chat1", "confirm": true})
	if allowed.IsError || fake.deleteCalls != 1 {
		t.Fatalf("带 confirm 删除会话应执行: %s", resultJSON(t, allowed))
	}
	// 会话不存在时归一为未找到。
	fake.deleteErr = chatapp.ErrChatSessionNotFound
	// missing 是删除不存在会话的结果。
	missing := invoke(endpoint, ctx, "chat_session_delete", map[string]any{"account_id": "acc1", "chat_id": "chat2", "confirm": true})
	if !strings.Contains(resultJSON(t, missing), "聊天会话不存在") {
		t.Fatalf("会话不存在映射异常: %s", resultJSON(t, missing))
	}
}

// TestChatMetadataTools 验证快捷回复与买家备注的成功、失败与 confirm 路径。
func TestChatMetadataTools(t *testing.T) {
	// fake 是聊天域假端口。
	fake := &fakeChatPorts{
		quickReplies: []chatapp.QuickReply{{ID: 3, AccountID: "acc1", Content: "在的", CreatedAt: 1700000000}},
		buyerNote:    chatapp.BuyerNote{AccountID: "acc1", BuyerID: "buyer1", Content: "老客户", UpdatedAt: 1700000000},
	}
	// endpoint、ctx 是聊天工具端点与身份上下文。
	endpoint, ctx := newChatToolEndpoint(t, fake)
	// listResult 是快捷回复列表结果。
	listResult := invoke(endpoint, ctx, "chat_quick_reply_list", map[string]any{"account_id": "acc1"})
	if listResult.IsError || !strings.Contains(resultJSON(t, listResult), `"content":"在的"`) {
		t.Fatalf("快捷回复列表异常: %s", resultJSON(t, listResult))
	}
	// addResult 是快捷回复新增结果。
	addResult := invoke(endpoint, ctx, "chat_quick_reply_add", map[string]any{"account_id": "acc1", "content": "马上发"})
	if addResult.IsError || fake.quickAddCalls != 1 || !strings.Contains(resultJSON(t, addResult), `"quick_reply_id":12`) {
		t.Fatalf("快捷回复新增异常: %s", resultJSON(t, addResult))
	}
	// deleteDenied 是缺 confirm 的快捷回复删除。
	deleteDenied := invoke(endpoint, ctx, "chat_quick_reply_delete", map[string]any{"account_id": "acc1", "quick_reply_id": 3.0})
	if !deleteDenied.IsError || fake.quickDeleteCalls != 0 {
		t.Fatal("缺 confirm 删除快捷回复必须零触达")
	}
	// deleteAllowed 是显式确认后的删除。
	deleteAllowed := invoke(endpoint, ctx, "chat_quick_reply_delete", map[string]any{
		"account_id": "acc1", "quick_reply_id": 3.0, "confirm": true,
	})
	if deleteAllowed.IsError || fake.quickDeleteCalls != 1 {
		t.Fatalf("带 confirm 删除快捷回复应执行: %s", resultJSON(t, deleteAllowed))
	}
	// 超出快捷回复上限时归一为中文可操作提示。
	fake.quickAddErr = chatapp.ErrQuickReplyLimitReached
	// limited 是超限新增结果。
	limited := invoke(endpoint, ctx, "chat_quick_reply_add", map[string]any{"account_id": "acc1", "content": "再来一条"})
	if !strings.Contains(resultJSON(t, limited), "数量已达上限") {
		t.Fatalf("快捷回复超限映射异常: %s", resultJSON(t, limited))
	}
	// noteResult 是买家备注读取结果。
	noteResult := invoke(endpoint, ctx, "chat_buyer_note_get", map[string]any{"account_id": "acc1", "buyer_id": "buyer1"})
	if noteResult.IsError || !strings.Contains(resultJSON(t, noteResult), "老客户") {
		t.Fatalf("买家备注读取异常: %s", resultJSON(t, noteResult))
	}
	// savedResult 是买家备注保存结果。
	savedResult := invoke(endpoint, ctx, "chat_buyer_note_set", map[string]any{
		"account_id": "acc1", "buyer_id": "buyer1", "content": "新备注",
	})
	if savedResult.IsError || fake.noteSaveCalls != 1 || fake.savedNoteContent != "新备注" {
		t.Fatalf("买家备注保存异常: %s", resultJSON(t, savedResult))
	}
	if !strings.Contains(resultJSON(t, savedResult), `"updated_at":1700000000`) {
		t.Fatalf("买家备注更新时间缺失: %s", resultJSON(t, savedResult))
	}
	// 缺少买家标识必须返回参数错误。
	missing := invoke(endpoint, ctx, "chat_buyer_note_set", map[string]any{"account_id": "acc1", "content": "x"})
	if !missing.IsError || fake.noteSaveCalls != 1 {
		t.Fatal("缺少买家标识必须零触达")
	}
}

// TestChatToolInventory 核对聊天域工具清单与破坏性注解。
func TestChatToolInventory(t *testing.T) {
	// expected 是 Task 9 要求注册的全部工具名。
	expected := map[string]bool{
		"chat_session_list": true, "chat_message_list": true, "chat_session_refresh_contacts": true,
		"chat_history_refresh": true, "chat_item_list": true, "chat_send_text": true, "chat_send_image": true,
		"chat_mark_read": true, "chat_session_delete": true, "chat_quick_reply_list": true,
		"chat_quick_reply_add": true, "chat_quick_reply_delete": true, "chat_buyer_note_get": true, "chat_buyer_note_set": true,
	}
	// destructive 是必须标记破坏性的工具集合（平台触达与不可逆删除）。
	destructive := map[string]bool{
		"chat_session_refresh_contacts": true, "chat_history_refresh": true, "chat_send_text": true,
		"chat_send_image": true, "chat_session_delete": true, "chat_quick_reply_delete": true,
	}
	// endpoint、_ 是注册聊天工具的端点。
	endpoint, _ := newChatToolEndpoint(t, &fakeChatPorts{})
	// registered 是实际注册的工具名集合。
	registered := map[string]bool{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		registered[def.Name] = true
		if destructive[def.Name] != def.Destructive {
			t.Fatalf("工具 %s 破坏性注解异常: %t", def.Name, def.Destructive)
		}
	}
	if len(registered) != len(expected) {
		t.Fatalf("聊天域工具数量异常: got=%d want=%d", len(registered), len(expected))
	}
	// name 是期望注册的工具名。
	for name := range expected {
		if !registered[name] {
			t.Fatalf("缺少工具 %s", name)
		}
	}
	// 端口缺失时不得注册任何工具。
	empty, _ := newProtocolEndpoint(t)
	empty.RegisterChatTools(nil)
	// defs 是聊天端口缺失时注册到的工具清单，必须为空。
	if defs := empty.ToolDefs(); len(defs) != 0 {
		t.Fatalf("聊天端口缺失时不应注册工具，实际 %d 个", len(defs))
	}
	// 图片探测辅助函数必须拒绝非图片内容。
	if chatImageFilename("image/jpeg") != chatFilePrefix+".jpg" || chatImageFilename("image/webp") != chatFilePrefix+".webp" {
		t.Fatal("图片文件名映射异常")
	}
	if !strings.HasPrefix(http.DetectContentType([]byte("\x89PNG\r\n\x1a\n")), "image/") {
		t.Fatal("图片探测前置条件异常")
	}
}
