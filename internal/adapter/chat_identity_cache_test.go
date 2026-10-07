package adapter

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/xianyu/mtop"
)

// countingIdentityClient 统计聊天身份查询次数，用于验证缓存是否避免重复请求平台。
type countingIdentityClient struct {
	// mtop.Client 保留未涉及身份查询的其余 MTOP 能力占位。
	mtop.Client
	// calls 记录 FetchChatUserInfo 被调用的次数。
	calls *atomic.Int32
	// info 是返回给调用方的非敏感展示身份。
	info *mtop.ChatUserInfo
}

// FetchChatUserInfo 累加计数并返回预设身份，不访问真实平台。
func (c countingIdentityClient) FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error) {
	c.calls.Add(1)
	return c.info, nil
}

// TestChatIdentityCacheReusesWithinTTLAndExpires 验证缓存只在复用窗口内命中。
func TestChatIdentityCacheReusesWithinTTLAndExpires(t *testing.T) {
	// cache 是被测缓存实例。
	cache := newChatIdentityCache()
	// key 是本次查询使用的账号与会话键。
	key := chatIdentityCacheKey{accountID: "cid", chatID: "chat-1"}
	// identity 是写入缓存的展示身份。
	identity := chatapp.Identity{PeerName: "买家", PeerAvatar: "avatar"}
	// baseline 是缓存写入时刻。
	baseline := time.Date(2026, time.October, 7, 8, 0, 0, 0, time.UTC)
	cache.put(key, identity, baseline)
	// cached、hit 保存窗口内的命中结果；必须命中且内容一致。
	cached, hit := cache.get(key, baseline.Add(chatIdentityTTL-time.Second))
	if !hit || cached != identity {
		t.Fatalf("窗口内缓存未命中 cached=%+v hit=%v", cached, hit)
	}
	// expiredHit 保存窗口边界的命中结果；到期必须失效。
	if _, expiredHit := cache.get(key, baseline.Add(chatIdentityTTL)); expiredHit {
		t.Fatal("缓存到期后仍然命中")
	}
	// missingHit 保存未写入键的命中结果，必须未命中。
	if _, missingHit := cache.get(chatIdentityCacheKey{accountID: "cid", chatID: "other"}, baseline); missingHit {
		t.Fatal("未写入的键不应命中缓存")
	}
}

// TestChatIdentityCacheEvictsOldestWhenFull 验证缓存超出容量时淘汰最旧条目而不是无界增长。
func TestChatIdentityCacheEvictsOldestWhenFull(t *testing.T) {
	// cache 是被测缓存实例。
	cache := newChatIdentityCache()
	// baseline 是写满缓存使用的时间基准；每条依次递增，使最旧条目可被确定性判定。
	baseline := time.Date(2026, time.October, 7, 8, 0, 0, 0, time.UTC)
	// index 是填充缓存时的条目序号。
	for index := 0; index < chatIdentityCacheCapacity; index++ {
		// writtenAt 是该条目的写入时间；全部落在复用窗口内，避免过期清理干扰淘汰断言。
		writtenAt := baseline.Add(time.Duration(index) * time.Millisecond)
		cache.put(chatIdentityCacheKey{accountID: "cid", chatID: chatIdentityKeyForTest(index)}, chatapp.Identity{PeerName: "买家"}, writtenAt)
	}
	// oldestKey 是写入最早、应当被淘汰的条目键。
	oldestKey := chatIdentityCacheKey{accountID: "cid", chatID: chatIdentityKeyForTest(0)}
	// now 是触发淘汰的当前时间；仍处于全部条目的复用窗口内。
	now := baseline.Add(time.Duration(chatIdentityCacheCapacity) * time.Millisecond)
	// 写入第 capacity+1 条，触发一次容量淘汰。
	cache.put(chatIdentityCacheKey{accountID: "cid", chatID: "extra"}, chatapp.Identity{PeerName: "新买家"}, now)
	cache.mu.Lock()
	// size 是淘汰后的条目数；必须不超过容量上限。
	size := len(cache.entries)
	cache.mu.Unlock()
	if size > chatIdentityCacheCapacity {
		t.Fatalf("缓存条目数=%d 超过容量 %d", size, chatIdentityCacheCapacity)
	}
	// _, oldestHit 保存最旧条目在淘汰后的命中结果；必须未命中。
	_, oldestHit := cache.get(oldestKey, now)
	if oldestHit {
		t.Fatal("超容量时未淘汰最旧条目")
	}
}

// chatIdentityKeyForTest 生成填充缓存用的稳定会话键。
func chatIdentityKeyForTest(index int) string {
	return "chat-" + strconv.Itoa(index)
}

// TestChatIdentityCacheNilReceiverIsSafe 验证未初始化的缓存按未命中与无操作降级，不会 panic。
func TestChatIdentityCacheNilReceiverIsSafe(t *testing.T) {
	// nilCache 是未初始化的缓存指针；测试替身或半初始化适配器可能持有它。
	var nilCache *chatIdentityCache
	// now 是本次判定使用的时间基准。
	now := time.Date(2026, time.October, 7, 8, 0, 0, 0, time.UTC)
	// nilCache.put 不得 panic，nilCache.get 必须报告未命中。
	nilCache.put(chatIdentityCacheKey{accountID: "cid", chatID: "chat-1"}, chatapp.Identity{PeerName: "买家"}, now)
	// nilHit 保存未初始化缓存的命中结果；必须为未命中。
	_, nilHit := nilCache.get(chatIdentityCacheKey{accountID: "cid", chatID: "chat-1"}, now)
	if nilHit {
		t.Fatal("未初始化的缓存不应命中")
	}
}

// TestChatIdentityCacheEvictionCleansExpiredEntries 验证淘汰时先清理过期条目再判断是否淘汰最旧条目。
func TestChatIdentityCacheEvictionCleansExpiredEntries(t *testing.T) {
	// cache 是被测缓存实例。
	cache := newChatIdentityCache()
	// baseline 是全部条目的写入时间基准。
	baseline := time.Date(2026, time.October, 7, 8, 0, 0, 0, time.UTC)
	// expiredKey 是写入时间已超出复用窗口、应被优先清理的条目键。
	expiredKey := chatIdentityCacheKey{accountID: "cid", chatID: "expired"}
	cache.put(expiredKey, chatapp.Identity{PeerName: "过期买家"}, baseline)
	// index 是填充剩余新鲜条目时的序号。
	for index := 0; index < chatIdentityCacheCapacity-1; index++ {
		cache.put(chatIdentityCacheKey{accountID: "cid", chatID: chatIdentityKeyForTest(index)}, chatapp.Identity{PeerName: "买家"}, baseline)
	}
	// now 是超出复用窗口的当前时间，使过期条目在淘汰阶段被判定失效。
	now := baseline.Add(chatIdentityTTL)
	cache.put(chatIdentityCacheKey{accountID: "cid", chatID: "fresh"}, chatapp.Identity{PeerName: "新买家"}, now)
	// _, expiredHit 保存过期条目的命中结果；必须在淘汰阶段被清理。
	_, expiredHit := cache.get(expiredKey, now)
	if expiredHit {
		t.Fatal("淘汰阶段未清理过期条目")
	}
	cache.mu.Lock()
	// size 是淘汰后的条目数；不得无界增长。
	size := len(cache.entries)
	cache.mu.Unlock()
	if size > chatIdentityCacheCapacity {
		t.Fatalf("缓存条目数=%d 超过容量 %d", size, chatIdentityCacheCapacity)
	}
}

// TestChatIdentityResolverReusesCachedIdentity 验证重复刷新命中缓存后不再请求平台。
func TestChatIdentityResolverReusesCachedIdentity(t *testing.T) {
	// store 是包含测试账号 Cookie 的临时数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// calls 统计真实平台身份查询次数。
	calls := &atomic.Int32{}
	// client 是记录调用次数的身份查询替身。
	client := countingIdentityClient{calls: calls, info: &mtop.ChatUserInfo{Nickname: "买家", AvatarURL: "avatar"}}
	// baseline 是本次测试的可控时间基准。
	baseline := time.Date(2026, time.October, 7, 8, 0, 0, 0, time.UTC)
	// resolver 是注入可控时间与共享缓存的被测适配器。
	resolver := chatIdentityResolver{
		store: store, clientProvider: func() mtop.Client { return client },
		credentials: chatCredentialRepository{store: store}, cache: newChatIdentityCache(),
		now: func() time.Time { return baseline },
	}
	// first 是首次查询结果；必须真实请求平台一次。
	if first, firstErr := resolver.Resolve(context.Background(), "cid", "chat-1"); firstErr != nil || first.PeerName != "买家" {
		t.Fatalf("首次身份查询异常 identity=%+v err=%v", first, firstErr)
	}
	// firstCalls 是首次查询后的平台调用次数；必须恰好一次。
	firstCalls := calls.Load()
	if firstCalls != 1 {
		t.Fatalf("首次查询平台调用次数=%d want 1", firstCalls)
	}
	// second 是窗口内的重复查询结果；必须命中缓存且不再调用平台。
	if second, secondErr := resolver.Resolve(context.Background(), "cid", "chat-1"); secondErr != nil || second.PeerName != "买家" {
		t.Fatalf("重复身份查询异常 identity=%+v err=%v", second, secondErr)
	}
	// reusedCalls 是重复查询后的平台调用次数；命中缓存时必须保持不变。
	reusedCalls := calls.Load()
	if reusedCalls != 1 {
		t.Fatalf("窗口内重复查询放大了平台调用，次数=%d want 1", reusedCalls)
	}
	// 时间推进到复用窗口之外后必须重新请求平台。
	baseline = baseline.Add(chatIdentityTTL)
	// expiredErr 是窗口过期后的身份查询错误；必须为空。
	_, expiredErr := resolver.Resolve(context.Background(), "cid", "chat-1")
	if expiredErr != nil {
		t.Fatalf("窗口过期后的身份查询失败: %v", expiredErr)
	}
	// expiredCalls 是窗口过期后的平台调用次数；必须重新请求一次。
	expiredCalls := calls.Load()
	if expiredCalls != 2 {
		t.Fatalf("窗口过期后平台调用次数=%d want 2", expiredCalls)
	}
}