package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// fakeQQClient 是 qqBotClient 的测试替身，记录调用次数与目标且不触网。
type fakeQQClient struct {
	// mu 保护以下计数与最近目标字段，避免并发测试中的数据竞争。
	mu sync.Mutex
	// c2cCalls 统计单聊发送调用次数。
	c2cCalls int
	// groupCalls 统计群聊发送调用次数。
	groupCalls int
	// c2cErr 是单聊发送的固定错误，为零表示成功。
	c2cErr error
	// groupErr 是群聊发送的固定错误，为零表示成功。
	groupErr error
	// lastC2CUserID 记录最近一次单聊目标用户 openid。
	lastC2CUserID string
	// lastGroupID 记录最近一次群聊目标群 openid。
	lastGroupID string
}

// SendC2CMessage 实现 qqBotClient 单聊发送接口，仅记录调用并回放预设错误。
func (f *fakeQQClient) SendC2CMessage(ctx context.Context, userOpenID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.c2cCalls++
	f.lastC2CUserID = userOpenID
	if f.c2cErr != nil {
		return f.c2cErr
	}
	return nil
}

// SendGroupMessage 实现 qqBotClient 群聊发送接口，仅记录调用并回放预设错误。
func (f *fakeQQClient) SendGroupMessage(ctx context.Context, groupOpenID, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groupCalls++
	f.lastGroupID = groupOpenID
	if f.groupErr != nil {
		return f.groupErr
	}
	return nil
}

// withFakeQQClient 临时把 newQQBotClient 替换为返回指定替身的工厂；测试结束后恢复原实现。
func withFakeQQClient(t *testing.T, client qqBotClient) {
	t.Helper()
	// original 保存被替换前的工厂实现，Cleanup 时还原。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		return client, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
}

// TestSendQQ_SingleChatPriority 验证 user_openid 与 group_openid 并存时优先单聊。
func TestSendQQ_SingleChatPriority(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	withFakeQQClient(t, fake)
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 同时配置单聊与群聊目标，应只走单聊。
	cfg := map[string]any{"app_id": "aid", "app_secret": "sec", "user_openid": "u1", "group_openid": "g1"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(cfg, "hello"); err != nil {
		t.Fatalf("sendQQ: %v", err)
	}
	if fake.c2cCalls != 1 {
		t.Errorf("c2cCalls=%d want 1", fake.c2cCalls)
	}
	if fake.groupCalls != 0 {
		t.Errorf("groupCalls=%d want 0", fake.groupCalls)
	}
	if fake.lastC2CUserID != "u1" {
		t.Errorf("lastC2CUserID=%s want u1", fake.lastC2CUserID)
	}
}

// TestSendQQ_GroupFallback 验证仅配置群聊目标时走群聊发送。
func TestSendQQ_GroupFallback(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	withFakeQQClient(t, fake)
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 仅配置群聊目标。
	cfg := map[string]any{"app_id": "aid", "app_secret": "sec", "group_openid": "g1"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(cfg, "hi"); err != nil {
		t.Fatalf("sendQQ: %v", err)
	}
	if fake.groupCalls != 1 || fake.c2cCalls != 0 {
		t.Errorf("groupCalls=%d c2cCalls=%d want 1/0", fake.groupCalls, fake.c2cCalls)
	}
}

// TestSendQQ_BothOpenIDEmpty 验证单聊与群聊目标皆空时返回配置错误且不构造客户端。
func TestSendQQ_BothOpenIDEmpty(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	// constructed 标记工厂是否被调用过。
	constructed := false
	// original 保存原工厂实现。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		constructed = true
		return fake, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 缺省两个目标 openid。
	cfg := map[string]any{"app_id": "aid", "app_secret": "sec"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(cfg, "hi"); err == nil {
		t.Fatal("期望缺目标配置错误")
	}
	if constructed {
		t.Error("目标皆空不应构造 QQ 客户端")
	}
}

// TestSendQQ_MissingCredentials 验证缺 app_id/app_secret 时返回配置错误且不构造客户端。
func TestSendQQ_MissingCredentials(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	// constructed 标记工厂是否被调用过。
	constructed := false
	// original 保存原工厂实现。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		constructed = true
		return fake, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 仅有单聊目标，缺少 app_id/app_secret。
	cfg := map[string]any{"user_openid": "u1"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(cfg, "hi"); err == nil {
		t.Fatal("期望缺凭据配置错误")
	}
	if constructed {
		t.Error("缺凭据不应构造 QQ 客户端")
	}
}

// TestSendQQ_SendErrorWrapped 验证发送失败错误被中文包装后透传。
func TestSendQQ_SendErrorWrapped(t *testing.T) {
	// platformErr 是平台返回的固定错误。
	platformErr := errors.New("platform down")
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{c2cErr: platformErr}
	withFakeQQClient(t, fake)
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 配置单聊目标以触发单聊发送路径。
	cfg := map[string]any{"app_id": "aid", "app_secret": "sec", "user_openid": "u1"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(cfg, "hi"); err == nil {
		t.Fatal("期望发送错误")
	} else if !strings.Contains(err.Error(), "qq 单聊发送失败") {
		t.Errorf("错误信息缺少中文包装: %v", err)
	}
}

// TestSendQQ_ClientCacheByAppID 验证同一 AppID 复用同一客户端，构造仅发生一次。
func TestSendQQ_ClientCacheByAppID(t *testing.T) {
	// shared 用于本次流程后续判断的shared
	shared := &fakeQQClient{}
	// constructCount 统计工厂被调用次数。
	constructCount := 0
	// original 保存原工厂实现。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		constructCount++
		return shared, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 两次发送使用相同 AppID，应命中缓存。
	cfg := map[string]any{"app_id": "aid", "app_secret": "sec", "user_openid": "u1"}
	_ = notifier.sendQQ(cfg, "m1")
	_ = notifier.sendQQ(cfg, "m2")
	if constructCount != 1 {
		t.Errorf("constructCount=%d want 1", constructCount)
	}
	if shared.c2cCalls != 2 {
		t.Errorf("c2cCalls=%d want 2", shared.c2cCalls)
	}
}

// TestSendQQ_ConcurrentConstruct 验证并发首次构造不损坏缓存且不重复发送。
func TestSendQQ_ConcurrentConstruct(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	// original 保存原工厂实现。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		// 模拟构造耗时以放大并发竞争窗口。
		time.Sleep(time.Millisecond)
		return fake, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// cfg 所有并发使用同一 AppID。
	cfg := map[string]any{"app_id": "aid", "app_secret": "sec", "user_openid": "u1"}
	// wg 用于等待全部并发发送完成。
	var wg sync.WaitGroup
	for // i 用于本次流程后续判断的i
	i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = notifier.sendQQ(cfg, "m")
		}()
	}
	wg.Wait()
	if fake.c2cCalls != 20 {
		t.Errorf("c2cCalls=%d want 20", fake.c2cCalls)
	}
	notifier.qqMu.Lock()
	// cached 是并发结束后缓存中的客户端数量。
	cached := len(notifier.qqClients)
	notifier.qqMu.Unlock()
	if cached != 1 {
		t.Errorf("cached=%d want 1", cached)
	}
}

// TestRouteByChannelType_QQ 验证 send() 把 qq 类型路由到 sendQQ 并成功发送。
func TestRouteByChannelType_QQ(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	withFakeQQClient(t, fake)
	// notifier 用于本次流程后续判断的notifier
	notifier := &Notifier{}
	// ch 配置完整且使用 qq 类型，send 应路由到 QQ 发送实现。
	ch := db.NotificationChannel{Type: "qq", Config: `{"app_id":"aid","app_secret":"sec","user_openid":"u1"}`}
	if // err 用于本次流程后续判断的err
	err := notifier.send(ch, "hi"); err != nil {
		t.Fatalf("send qq: %v", err)
	}
	if fake.c2cCalls != 1 {
		t.Errorf("c2cCalls=%d want 1", fake.c2cCalls)
	}
}
