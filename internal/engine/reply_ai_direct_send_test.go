// reply_ai_direct_send_test 锁定「AI 回复人工确认模式已下线」这一事实：
// AI 产出的回复经 Handle 的安全闸门后直接外发，不再存在拦截待确认的分支。
// 若日后有人重新引入拦截，这些用例会失败，提醒必须同步评估安全兜底与可观测性。

package engine

import (
	"context"
	"testing"
)

// legacyReviewSettingKey 是已下线的 AI 回复人工确认设置键，
// 仅用于模拟升级前遗留的数据库配置，验证它不再影响发送行为。
const legacyReviewSettingKey = "ai_reply_review_mode"

// TestHandleSendsAIReplyDespiteLegacyReviewSetting 验证库里残留旧 review 开关时 AI 回复仍直接外发。
// 旧实现读取该键为开启状态会拦截 AI 回复；当前实现完全不读取它，因此买家必须真的收到 AI 文本。
func TestHandleSendsAIReplyDespiteLegacyReviewSetting(t *testing.T) {
	// store、cleanup 是回复链测试仓储及清理函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是设置写入与回复发送共用的测试上下文。
	ctx := context.Background()
	// seedErr 保存旧 review 开关写入错误；该键已无人读取，这里只模拟升级遗留数据。
	if seedErr := store.Settings.Set(ctx, legacyReviewSettingKey, "1"); seedErr != nil {
		t.Fatalf("写入遗留 review 开关失败: %v", seedErr)
	}
	// sender 记录实际投递给买家的文本。
	sender := &recordingSender{}
	// ai 是返回固定草稿的 AI 替身，called 用于确认 AI 层确实被调用。
	ai := &fakeAIReplier{result: &ReplyResult{Text: "您好，这件商品还在的。"}}
	// service 是按新签名（八个固定参数）构造的回复链。
	service := NewReplyService("cid", store, sender, nil, nil, ai, nil, nil)
	// err 是本次 Handle 的返回错误；旧实现会静默拦截并返回 nil，故必须额外断言发送记录。
	if err := service.Handle(ctx, chatMsg("这件还在吗", "item-1", "chat-direct-ai")); err != nil {
		t.Fatalf("AI 回复发送失败: %v", err)
	}
	if ai.called != 1 {
		t.Fatalf("AI 层应被调用一次，got %d", ai.called)
	}
	if len(sender.texts) != 1 {
		t.Fatalf("AI 回复必须直接外发一次，got %d 次: %+v", len(sender.texts), sender.texts)
	}
	// sent 是本条用例实际投递给买家的唯一一条文本记录。
	if sent := sender.texts[0]; sent.chatID != "chat-direct-ai" || sent.toUserID != "buyer1" || sent.text != "您好，这件商品还在的。" {
		t.Fatalf("AI 回复投递目标或内容不符: %+v", sent)
	}
}

// TestHandleSendsAIReplyWithoutReviewSetting 验证全新库（无旧键）下 AI 回复同样直接发送。
// 与新签名一起构成基线：AI 链路不依赖任何 review 相关配置或注入。
func TestHandleSendsAIReplyWithoutReviewSetting(t *testing.T) {
	// store、cleanup 是回复链测试仓储及清理函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// sender 记录实际投递给买家的文本。
	sender := &recordingSender{}
	// service 注入 AI 替身，其余可选依赖全部留空，验证降级路径仍能发送。
	service := NewReplyService("cid", store, sender, nil, nil, &fakeAIReplier{result: &ReplyResult{Text: "在的，可以直接拍。"}}, nil, nil)
	if // err 是本次 Handle 的返回错误；AI 回复不再有任何拦截分支，因此必须是 nil。
	err := service.Handle(context.Background(), chatMsg("在吗", "", "chat-plain")); err != nil {
		t.Fatalf("AI 回复发送失败: %v", err)
	}
	if len(sender.texts) != 1 || sender.texts[0].text != "在的，可以直接拍。" {
		t.Fatalf("AI 回复必须直接外发: %+v", sender.texts)
	}
}
