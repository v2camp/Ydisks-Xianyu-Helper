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
	err := notifier.sendQQ(qqTestContext(t), cfg, "hello", 0); err != nil {
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
	err := notifier.sendQQ(qqTestContext(t), cfg, "hi", 0); err != nil {
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
	err := notifier.sendQQ(qqTestContext(t), cfg, "hi", 0); err == nil {
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
	err := notifier.sendQQ(qqTestContext(t), cfg, "hi", 0); err == nil {
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
	err := notifier.sendQQ(qqTestContext(t), cfg, "hi", 0); err == nil {
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
	_ = notifier.sendQQ(qqTestContext(t), cfg, "m1", 0)
	_ = notifier.sendQQ(qqTestContext(t), cfg, "m2", 0)
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
			_ = notifier.sendQQ(qqTestContext(t), cfg, "m", 0)
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

// qqTestContext 提供测试用的有界上下文；根 Context 必须带有限预算以满足架构门禁。
func qqTestContext(t *testing.T) context.Context {
	t.Helper()
	// ctx、cancel 保存带一秒预算的测试上下文及其释放函数。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

// settingsRepositoryFake 是可注入系统设置的通知仓储替身，用于验证 QQ 连接器凭据回退。
type settingsRepositoryFake struct {
	// settings 保存按全局读取的系统设置值。
	settings map[string]string
	// scoped 保存按用户作用域读取的系统设置值，优先于全局设置。
	scoped map[string]string
}

// AccountChannels 返回空渠道列表，QQ 凭据测试不依赖账号绑定。
func (f *settingsRepositoryFake) AccountChannels(ctx context.Context, cookieID string) ([]db.NotificationChannel, error) {
	return nil, nil
}

// EnqueueOutbox 丢弃入队请求，QQ 凭据测试不依赖 outbox 持久化。
func (f *settingsRepositoryFake) EnqueueOutbox(ctx context.Context, messages []db.NotificationOutboxInput) error {
	return nil
}

// ClaimOutbox 返回空批次，QQ 凭据测试不驱动 outbox worker。
func (f *settingsRepositoryFake) ClaimOutbox(ctx context.Context, workerToken string, now time.Time, limit int) ([]db.NotificationOutboxMessage, error) {
	return nil, nil
}

// GetChannel 返回空渠道，QQ 凭据测试直接调用 sendQQ 而不经过渠道查询。
func (f *settingsRepositoryFake) GetChannel(ctx context.Context, channelID int64) (*db.NotificationChannel, error) {
	return nil, nil
}

// CompleteOutbox 返回未确认，QQ 凭据测试不依赖 outbox 收口。
func (f *settingsRepositoryFake) CompleteOutbox(ctx context.Context, messageID int64, workerToken string) (bool, error) {
	return false, nil
}

// MarkOutboxUncertain 返回未隔离，QQ 凭据测试不触发不确定隔离路径。
func (f *settingsRepositoryFake) MarkOutboxUncertain(ctx context.Context, messageID int64, workerToken, lastError string) (bool, error) {
	return false, nil
}

// RetryOutbox 返回未更新，QQ 凭据测试不触发重试路径。
func (f *settingsRepositoryFake) RetryOutbox(ctx context.Context, messageID int64, workerToken, lastError string, nextAttemptAt int64, permanent bool) (bool, error) {
	return false, nil
}

// GetSetting 按全局作用域返回注入的系统设置值。
func (f *settingsRepositoryFake) GetSetting(ctx context.Context, key string) (string, error) {
	return f.settings[key], nil
}

// GetSettingForUser 按用户作用域返回注入的系统设置值，用于验证敏感键走带审计的读取。
func (f *settingsRepositoryFake) GetSettingForUser(ctx context.Context, userID int64, key string) (string, error) {
	return f.scoped[key], nil
}

// TestSendQQ_FallsBackToSystemConnector 验证渠道配置未填凭据时使用系统连接器设置。
func TestSendQQ_FallsBackToSystemConnector(t *testing.T) {
	// fake 用于本次流程后续判断的fake
	fake := &fakeQQClient{}
	// capturedID、capturedSecret 记录工厂实际收到的凭据，用于断言来源。
	var capturedID, capturedSecret string
	// original 保存原工厂实现。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		capturedID, capturedSecret = appID, appSecret
		return fake, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
	// repository 提供系统连接器中已配置的机器人凭据。
	repository := &settingsRepositoryFake{settings: map[string]string{"qqbot.app_id": "sys_id", "qqbot.app_secret": "sys_secret"}}
	// notifier 是带仓储的通知器，凭据唯一来源为系统连接器。
	notifier := &Notifier{repository: repository}
	// cfg 只配置目标 openid，不含凭据。
	cfg := map[string]any{"user_openid": "u1"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(qqTestContext(t), cfg, "hi", 0); err != nil {
		t.Fatalf("sendQQ: %v", err)
	}
	if capturedID != "sys_id" || capturedSecret != "sys_secret" {
		t.Errorf("凭据未回退到系统连接器: id=%s secret=%s", capturedID, capturedSecret)
	}
	if fake.c2cCalls != 1 {
		t.Errorf("c2cCalls=%d want 1", fake.c2cCalls)
	}
}

// TestSendQQ_ChannelConfigOverridesSystemConnector 验证渠道配置凭据优先于系统连接器设置。
func TestSendQQ_ChannelConfigOverridesSystemConnector(t *testing.T) {
	// capturedID 记录工厂最终收到的 AppID，用于断言优先级。
	capturedID := ""
	// original 保存原工厂实现。
	original := newQQBotClient
	newQQBotClient = func(ctx context.Context, appID, appSecret string) (qqBotClient, error) {
		capturedID = appID
		return &fakeQQClient{}, nil
	}
	t.Cleanup(func() { newQQBotClient = original })
	// repository 提供与渠道配置不同的系统连接器凭据。
	repository := &settingsRepositoryFake{settings: map[string]string{"qqbot.app_id": "sys_id", "qqbot.app_secret": "sys_secret"}}
	// notifier 是带仓储的通知器。
	notifier := &Notifier{repository: repository}
	// cfg 在渠道级显式填写凭据，应优先于系统连接器。
	cfg := map[string]any{"app_id": "ch_id", "app_secret": "ch_secret", "user_openid": "u1"}
	if // err 用于本次流程后续判断的err
	err := notifier.sendQQ(qqTestContext(t), cfg, "hi", 0); err != nil {
		t.Fatalf("sendQQ: %v", err)
	}
	if capturedID != "ch_id" {
		t.Errorf("capturedID=%s want ch_id（渠道配置应优先）", capturedID)
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
