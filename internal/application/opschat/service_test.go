package opschat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// repositoryFake 是运营对话仓储的测试替身，记录调用参数并返回预设结果。
type repositoryFake struct {
	// inboundRecord 保存最近一次入站记录参数。
	inboundRecord InboundRecord
	// inboundMessage 是入站写入返回的消息。
	inboundMessage Message
	// inboundInserted 是入站写入返回的新增标记。
	inboundInserted bool
	// inboundErr 是入站写入返回的错误。
	inboundErr error
	// appendConversationID、appendContent、appendPreview、appendAt 保存出站写入参数。
	appendConversationID int64
	appendContent        string
	appendPreview        string
	appendAt             int64
	// appendMessage、appendConversation 是出站写入返回结果。
	appendMessage      Message
	appendConversation Conversation
	// appendErr 是出站写入返回的错误。
	appendErr error
	// appendCalls 统计出站写入调用次数，用于断言发送失败不落库。
	appendCalls int
	// conversation 是 GetConversation 的预设结果。
	conversation Conversation
	// getErr 是 GetConversation 的预设错误。
	getErr error
	// listPage、listErr 是会话分页预设结果与错误。
	listPage ConversationPage
	listErr  error
	// listMessagesPage、listMessagesErr 是消息分页预设结果与错误。
	listMessagesPage MessagePage
	listMessagesErr  error
	// lastListPage、lastListPageSize 保存最近一次会话分页参数。
	lastListPage, lastListPageSize int
	// lastBeforeID、lastLimit 保存最近一次消息分页参数。
	lastBeforeID, lastLimit int64
}

// RecordInbound 记录入站写入参数并返回预设结果。
func (r *repositoryFake) RecordInbound(_ context.Context, record InboundRecord) (Message, bool, error) {
	r.inboundRecord = record
	return r.inboundMessage, r.inboundInserted, r.inboundErr
}

// AppendOutbound 记录出站写入参数并返回预设结果。
func (r *repositoryFake) AppendOutbound(_ context.Context, _ int64, conversationID int64, content, preview string, at int64) (Message, Conversation, error) {
	r.appendCalls++
	r.appendConversationID, r.appendContent, r.appendPreview, r.appendAt = conversationID, content, preview, at
	return r.appendMessage, r.appendConversation, r.appendErr
}

// AppendOutboundByTarget 记录按目标写入参数并复用预设结果。
func (r *repositoryFake) AppendOutboundByTarget(_ context.Context, _ int64, channel, targetOpenID, content, preview string, at int64) (Message, Conversation, error) {
	r.appendCalls++
	r.appendContent, r.appendPreview, r.appendAt = content, preview, at
	r.inboundRecord.Channel, r.inboundRecord.TargetOpenID = channel, targetOpenID
	return r.appendMessage, r.appendConversation, r.appendErr
}

// ListConversations 记录分页参数并返回预设结果。
func (r *repositoryFake) ListConversations(_ context.Context, _ int64, page, pageSize int) (ConversationPage, error) {
	r.lastListPage, r.lastListPageSize = page, pageSize
	return r.listPage, r.listErr
}

// GetConversation 返回预设会话或错误。
func (r *repositoryFake) GetConversation(_ context.Context, _ int64, _ int64) (Conversation, error) {
	return r.conversation, r.getErr
}

// ListMessages 记录消息分页参数并返回预设结果。
func (r *repositoryFake) ListMessages(_ context.Context, _ int64, _ int64, beforeID int64, limit int) (MessagePage, error) {
	r.lastBeforeID, r.lastLimit = beforeID, int64(limit)
	return r.listMessagesPage, r.listMessagesErr
}

// senderFake 是运营渠道发送替身，记录参数并返回预设错误。
type senderFake struct {
	// userID、target、isGroup、content 保存最近一次发送参数。
	userID  int64
	target  string
	isGroup bool
	content string
	// calls 统计发送调用次数。
	calls int
	// err 是预设发送错误。
	err error
}

// SendText 记录发送参数并返回预设错误。
func (s *senderFake) SendText(_ context.Context, userID int64, targetOpenID string, isGroup bool, content string) error {
	s.calls++
	s.userID, s.target, s.isGroup, s.content = userID, targetOpenID, isGroup, content
	return s.err
}

// TestSendValidatesContent 验证空白与超长内容被拒绝，且不触发发送或落库。
func TestSendValidatesContent(t *testing.T) {
	// cases 是应判定为内容非法的输入。
	cases := map[string]string{
		"空内容":  "",
		"纯空白":  "   \n\t",
		"超过上限": strings.Repeat("字", 1501),
	}
	// name 是用例名称；content 是当前待校验内容。
	for name, content := range cases {
		// repository、sender 是注入替身。
		repository, sender := &repositoryFake{}, &senderFake{}
		// service 是待测应用服务。
		service := NewService(repository, sender)
		if // _, _, err 是发送结果与错误。
		_, _, err := service.Send(context.Background(), SendInput{UserID: 1, ConversationID: 9, Content: content}); !errors.Is(err, ErrInvalidContent) {
			t.Fatalf("%s 应返回 ErrInvalidContent，实际 %v", name, err)
		}
		if sender.calls != 0 || repository.appendCalls != 0 {
			t.Fatalf("%s 不得触发发送或落库，实际 sender=%d append=%d", name, sender.calls, repository.appendCalls)
		}
	}
}

// TestSendSuccessTrimsAndPersists 验证发送成功后按会话目标发送并落库。
func TestSendSuccessTrimsAndPersists(t *testing.T) {
	// repository 是返回群聊会话的仓储替身。
	repository := &repositoryFake{
		conversation:       Conversation{ID: 9, Channel: "qq", TargetOpenID: "group-1", IsGroup: true},
		appendMessage:      Message{ID: 11, ConversationID: 9, Direction: "outbound", Source: "operator", Content: "你好"},
		appendConversation: Conversation{ID: 9, Channel: "qq", TargetOpenID: "group-1", IsGroup: true},
	}
	// sender 是记录参数的发送替身。
	sender := &senderFake{}
	// service 是待测应用服务，固定时钟验证落库时间。
	service := NewService(repository, sender)
	// fixed 是固定时钟。
	fixed := time.UnixMilli(1_700_000_000_000)
	service.SetClock(func() time.Time { return fixed })
	// message、conversation、err 是发送结果。
	message, conversation, err := service.Send(context.Background(), SendInput{UserID: 7, ConversationID: 9, Content: "  你好  "})
	if err != nil {
		t.Fatalf("发送应成功，实际 %v", err)
	}
	if sender.target != "group-1" || !sender.isGroup || sender.content != "你好" || sender.userID != 7 {
		t.Fatalf("发送目标或内容不符，实际 %+v", sender)
	}
	if message.ID != 11 || conversation.ID != 9 {
		t.Fatalf("返回结果不符，实际 message=%+v conversation=%+v", message, conversation)
	}
	if repository.appendContent != "你好" || repository.appendAt != fixed.UnixMilli() {
		t.Fatalf("落库内容或时间不符，实际 content=%q at=%d", repository.appendContent, repository.appendAt)
	}
}

// TestSendFailureDoesNotPersist 验证平台发送失败返回 ErrSendFailed 且不写入出站记录。
func TestSendFailureDoesNotPersist(t *testing.T) {
	// repository 是返回单聊会话的仓储替身。
	repository := &repositoryFake{conversation: Conversation{ID: 3, TargetOpenID: "user-1"}}
	// sender 是预设失败的发送替身。
	sender := &senderFake{err: errors.New("platform unavailable")}
	// service 是待测应用服务。
	service := NewService(repository, sender)
	if // _, _, err 是发送结果与错误。
	_, _, err := service.Send(context.Background(), SendInput{UserID: 1, ConversationID: 3, Content: "在吗"}); !errors.Is(err, ErrSendFailed) {
		t.Fatalf("发送失败应返回 ErrSendFailed，实际 %v", err)
	}
	if repository.appendCalls != 0 {
		t.Fatalf("发送失败不得落库，实际 append=%d", repository.appendCalls)
	}
	if sender.isGroup {
		t.Fatal("单聊会话不得走群聊通道")
	}
}

// TestSendClassifiesErrors 验证渠道未配置与会话不存在分别返回稳定错误。
func TestSendClassifiesErrors(t *testing.T) {
	// missing 是会话不存在的仓储替身。
	missing := &repositoryFake{getErr: ErrConversationNotFound}
	if // _, _, err 是会话不存在时的发送结果。
	_, _, err := NewService(missing, &senderFake{}).Send(context.Background(), SendInput{UserID: 1, ConversationID: 2, Content: "x"}); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("会话不存在应返回 ErrConversationNotFound，实际 %v", err)
	}
	// noSender 是已存在会话但未装配发送端口的仓储替身。
	noSender := &repositoryFake{conversation: Conversation{ID: 2, TargetOpenID: "u"}}
	if // _, _, err 是渠道未配置时的发送结果。
	_, _, err := NewService(noSender, nil).Send(context.Background(), SendInput{UserID: 1, ConversationID: 2, Content: "x"}); !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("未装配发送端口应返回 ErrChannelNotConfigured，实际 %v", err)
	}
}

// TestRecordInboundDefaultsChannelAndTime 验证入站记录补齐渠道与时间。
func TestRecordInboundDefaultsChannelAndTime(t *testing.T) {
	// repository 是返回新增标记的仓储替身。
	repository := &repositoryFake{inboundInserted: true}
	// service 是待测应用服务，固定时钟用于断言补齐时间。
	service := NewService(repository, nil)
	// fixed 是固定时钟。
	fixed := time.UnixMilli(1_650_000_000_000)
	service.SetClock(func() time.Time { return fixed })
	if // inserted、err 是入站记录结果。
	_, inserted, err := service.RecordInbound(context.Background(), InboundRecord{UserID: 1, TargetOpenID: " group-9 ", PlatformMsgID: " m1 ", Content: "hi"}); err != nil || !inserted {
		t.Fatalf("入站记录应成功，实际 inserted=%v err=%v", inserted, err)
	}
	if repository.inboundRecord.Channel != "qq" || repository.inboundRecord.TargetOpenID != "group-9" ||
		repository.inboundRecord.PlatformMsgID != "m1" || repository.inboundRecord.At != fixed.UnixMilli() {
		t.Fatalf("入站记录归一化不符，实际 %+v", repository.inboundRecord)
	}
}

// TestRecordOutboundTruncatesPreview 验证出站记录按 60 字符截断预览。
func TestRecordOutboundTruncatesPreview(t *testing.T) {
	// repository 是记录出站参数的仓储替身。
	repository := &repositoryFake{}
	// service 是待测应用服务。
	service := NewService(repository, nil)
	// content 是超过预览上限的正文。
	content := strings.Repeat("长", 80)
	if // _, _, err 是出站记录结果。
	_, _, err := service.RecordOutbound(context.Background(), OutboundRecord{UserID: 1, TargetOpenID: "u", Content: content}); err != nil {
		t.Fatalf("出站记录应成功，实际 %v", err)
	}
	// count 是预览实际保留的字符数。
	count := len([]rune(repository.appendPreview))
	if count != previewRunes {
		t.Fatalf("预览应截断为 %d 字符，实际 %d", previewRunes, count)
	}
}

// TestListNormalizesPagination 验证会话与消息分页参数收敛到合理上限。
func TestListNormalizesPagination(t *testing.T) {
	// repository 是分页替身。
	repository := &repositoryFake{}
	// service 是待测应用服务。
	service := NewService(repository, nil)
	if // _, err 是会话分页结果。
	_, err := service.List(context.Background(), 1, 0, 1000); err != nil {
		t.Fatalf("会话分页应成功，实际 %v", err)
	}
	if repository.lastListPage != 1 || repository.lastListPageSize != maxConversationPageSize {
		t.Fatalf("会话分页参数未收敛，实际 page=%d size=%d", repository.lastListPage, repository.lastListPageSize)
	}
	// repository.conversation 是消息分页前必须存在的会话。
	repository.conversation = Conversation{ID: 5}
	if // _, err 是消息分页结果。
	_, err := service.Messages(context.Background(), 1, 5, 0, 1000); err != nil {
		t.Fatalf("消息分页应成功，实际 %v", err)
	}
	if repository.lastLimit != maxMessageLimit {
		t.Fatalf("消息分页上限未收敛，实际 %d", repository.lastLimit)
	}
}
