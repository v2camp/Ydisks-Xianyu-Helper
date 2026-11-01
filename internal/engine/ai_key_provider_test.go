// ai_key_provider_test.go 锁定密钥缓存的四条核心契约：
// 命中缓存不写审计、未命中只写一行审计、并发未命中只读一次库、敏感键写入后立即失效。
package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// countAuditRows 返回 security_audit_logs 的当前行数，用于断言缓存是否产生新审计。
func countAuditRows(t *testing.T, store *db.Store) int {
	t.Helper()
	// row 是 COUNT(*) 的唯一返回行。
	row := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM security_audit_logs`)
	// total 是本次统计出的行数。
	var total int
	// err 是统计查询的失败原因。
	if err := row.Scan(&total); err != nil {
		t.Fatalf("统计审计行数: %v", err)
	}
	return total
}

// countingKeySource 是计数型密钥来源：只返回固定密钥并累计真实读取次数，不访问数据库。
type countingKeySource struct {
	// reads 记录真实读取次数，用原子量避免测试与读取协程的数据竞争。
	reads atomic.Int32
	// value 是每次读取返回的密钥原文。
	value string
	// err 非空时直接返回该错误，用于验证失败不进入缓存。
	err error
	// entered 非空时每次真实读取先向它发信号，供并发用例观察「已进入真实读取」。
	entered chan struct{}
	// release 非空时真实读取在收到信号前阻塞，用于制造确定的并发重叠。
	release chan struct{}
}

// ReadSensitiveSettingForAccount 实现密钥来源端口，只做计数与可控阻塞，不写任何审计。
func (s *countingKeySource) ReadSensitiveSettingForAccount(ctx context.Context, _, _, _, _ string) (string, error) {
	s.reads.Add(1)
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if s.err != nil {
		return "", s.err
	}
	return s.value, nil
}

// count 返回累计的真实读取次数，供断言直接比较。
func (s *countingKeySource) count() int {
	return int(s.reads.Load())
}

// aiKeyTestClock 是可推进的测试时钟，用于在不 sleep 的前提下驱动 TTL 过期。
type aiKeyTestClock struct {
	// mu 保护 now 的并发读写。
	mu sync.Mutex
	// now 是当前虚拟时刻。
	now time.Time
}

// Now 返回当前虚拟时刻。
func (c *aiKeyTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 把虚拟时钟向前推进 d。
func (c *aiKeyTestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newAIKeyTestClock 构造从固定起点开始的虚拟时钟。
func newAIKeyTestClock() *aiKeyTestClock {
	return &aiKeyTestClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// mustGet 读取一次密钥，读取失败直接终止当前用例；返回值是密钥原文。
func mustGet(t *testing.T, provider *AIKeyProvider, source aiKeySource, cookieID string) string {
	t.Helper()
	// value、err 是本次读取结果与失败原因。
	value, err := provider.Get(context.Background(), source, cookieID, "ai_api_key", "settings.use", "ai_reply")
	if err != nil {
		t.Fatalf("读取密钥: %v", err)
	}
	return value
}

// assertReads 断言当前累计的真实读取次数等于 want，便于把「只读一次库」写成一行。
func assertReads(t *testing.T, source *countingKeySource, want int, scene string) {
	t.Helper()
	if source.count() != want {
		t.Fatalf("%s: reads=%d want=%d", scene, source.count(), want)
	}
}

// TestAIKeyCacheTTLFromEnv 环境变量解析：空值与非法值回落默认，0 禁用缓存，负数回落，过短抬到下限。
func TestAIKeyCacheTTLFromEnv(t *testing.T) {
	// cases 是环境变量原文与期望 TTL。
	cases := []struct {
		// raw 是环境变量原文。
		raw string
		// want 是期望 TTL；0 表示禁用缓存。
		want time.Duration
	}{
		{raw: "", want: defaultAIKeyCacheTTL},
		{raw: "abc", want: defaultAIKeyCacheTTL},
		{raw: "0", want: 0},
		{raw: "-5", want: defaultAIKeyCacheTTL},
		{raw: "5", want: minAIKeyCacheTTL},
		{raw: "600", want: 10 * time.Minute},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		// got 是解析出的 TTL。
		got := aiKeyCacheTTLFromEnv(func(string) string { return c.raw })
		if got != c.want {
			t.Fatalf("raw=%q got=%v want=%v", c.raw, got, c.want)
		}
	}
}

// TestAIKeyProviderMissWritesSingleAuditRow 未命中时产生且只产生一行审计，命中时不再新增。
// 这是本次优化的核心断言：审计口径从「每次访问」变为「每次真实读取」。
func TestAIKeyProviderMissWritesSingleAuditRow(t *testing.T) {
	// store、cleanup 是带一个账号与密钥的真实测试库。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// err 是写入测试密钥的失败原因。
	if err := store.Settings.Set(ctx, "ai_api_key", "sk-cache"); err != nil {
		t.Fatalf("写入密钥: %v", err)
	}
	// provider 是本用例使用的缓存实例，TTL 取默认值。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)
	// before 是读取前的审计行数。
	before := countAuditRows(t, store)

	// 首次调用必然未命中，因此必须新增一行审计。
	first := mustGet(t, provider, store, "cid")
	if first != "sk-cache" {
		t.Fatalf("首次读取密钥不符: %q", first)
	}
	// afterFirst 是首次读取后的审计行数，必须恰好 +1。
	afterFirst := countAuditRows(t, store)
	if afterFirst != before+1 {
		t.Fatalf("未命中应只写一行审计: before=%d after=%d", before, afterFirst)
	}

	// 第二次调用命中缓存，结果必须一致且不得新增审计行。
	second := mustGet(t, provider, store, "cid")
	if second != first {
		t.Fatalf("命中缓存应返回同一密钥: %q != %q", second, first)
	}
	// afterSecond 是二次读取后的审计行数，必须与首次相同。
	afterSecond := countAuditRows(t, store)
	if afterSecond != afterFirst {
		t.Fatalf("命中缓存不得新增审计: first=%d second=%d", afterFirst, afterSecond)
	}
	t.Logf("审计行数 before=%d afterFirstMiss=%d afterSecondHit=%d", before, afterFirst, afterSecond)
}

// TestAIKeyProviderConcurrentMissReadsOnce 并发未命中只读一次库：single-flight 合并重复读取。
func TestAIKeyProviderConcurrentMissReadsOnce(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// source 是阻塞式计数来源：真实读取会先发信号再等放行。
	source := &countingKeySource{value: "sk-flight", entered: make(chan struct{}, 64), release: make(chan struct{})}
	// provider 使用默认 TTL，确保走缓存分支。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)
	// workers 是并发调用数；越大越能暴露缺失的 single-flight。
	const workers = 32
	// wg 等待所有读取协程结束。
	var wg sync.WaitGroup
	wg.Add(workers)
	// values 保存每个协程读到的密钥，用于确认所有调用都拿到同一结果。
	values := make([]string, workers)
	// errs 保存每个协程的错误。
	errs := make([]error, workers)
	// i 是协程下标。
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			// value、err 是本次读取结果与错误。
			value, err := provider.Get(ctx, source, "cid", "ai_api_key", "settings.use", "ai_reply")
			values[index] = value
			errs[index] = err
		}(i)
	}
	// 等待至少一个协程进入真实读取，再放行，保证重叠窗口确定存在。
	<-source.entered
	close(source.release)
	wg.Wait()
	assertReads(t, source, 1, "并发未命中只应读一次库")
	// index 是当前校验的协程下标。
	for index := 0; index < workers; index++ {
		if errs[index] != nil || values[index] != "sk-flight" {
			t.Fatalf("第 %d 个并发读取结果不符: value=%q err=%v", index, values[index], errs[index])
		}
	}
}

// TestAIKeyProviderTTLExpiryRereads TTL 过期后必须重新读取；用注入时钟驱动，不 sleep。
func TestAIKeyProviderTTLExpiryRereads(t *testing.T) {
	// clock 是可控虚拟时钟。
	clock := newAIKeyTestClock()
	// source 是计数来源。
	source := &countingKeySource{value: "sk-ttl"}
	// provider 的 TTL 固定为一分钟，便于精确推进。
	provider := NewAIKeyProvider(time.Minute, clock.Now)

	// 首次读取必须走库。
	mustGet(t, provider, source, "cid")
	// 未过期前的第二次读取命中缓存。
	mustGet(t, provider, source, "cid")
	assertReads(t, source, 1, "未过期不应重读")

	// 推进到刚过期：下一次读取必须重新走库。
	clock.Advance(time.Minute)
	mustGet(t, provider, source, "cid")
	assertReads(t, source, 2, "过期后必须重读")
}

// TestAIKeyProviderInvalidateForcesReread 失效后下一次读取必须重新走库。
func TestAIKeyProviderInvalidateForcesReread(t *testing.T) {
	// source 是计数来源。
	source := &countingKeySource{value: "sk-invalidate"}
	// provider 使用默认 TTL。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)

	// 首次读取填充缓存。
	mustGet(t, provider, source, "cid")
	// 空账号失效不得清空全表，因此仍应命中缓存。
	provider.Invalidate("  ")
	mustGet(t, provider, source, "cid")
	assertReads(t, source, 1, "空账号失效不应清空缓存")

	// 命中目标账号的失效必须让下一次读取重新走库。
	provider.Invalidate("cid")
	mustGet(t, provider, source, "cid")
	assertReads(t, source, 2, "失效后必须重读")

	// InvalidateAll 同样必须让缓存清空。
	provider.InvalidateAll()
	mustGet(t, provider, source, "cid")
	assertReads(t, source, 3, "全量失效后必须重读")
}

// TestAIKeyProviderDisabledByNonPositiveTTL TTL<=0 时禁用缓存，每次读取都走库，保证排障可用。
func TestAIKeyProviderDisabledByNonPositiveTTL(t *testing.T) {
	// cases 是两个禁用取值：0 与负数都必须禁用。
	cases := []time.Duration{0, -time.Second}
	// ttl 是当前遍历到的 TTL。
	for _, ttl := range cases {
		// source 是计数来源。
		source := &countingKeySource{value: "sk-disabled"}
		// provider 处于禁用缓存状态。
		provider := NewAIKeyProvider(ttl, nil)
		// i 是读取轮次；三次读取都必须真实走库。
		for i := 0; i < 3; i++ {
			mustGet(t, provider, source, "cid")
		}
		assertReads(t, source, 3, "禁用缓存时必须每次都读库")
	}
}

// TestAIKeyProviderReadErrorNotCached 读取失败不得进入缓存：下一次调用必须重试并重新写审计。
func TestAIKeyProviderReadErrorNotCached(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// source 是固定失败来源。
	source := &countingKeySource{err: errors.New("审计写入失败")}
	// provider 使用默认 TTL。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)
	// i 是读取轮次：两次调用都必须返回错误，失败结果不得被缓存成「可用」。
	for i := 0; i < 2; i++ {
		// err 是本次读取的失败原因。
		if _, err := provider.Get(ctx, source, "cid", "ai_api_key", "settings.use", "ai_reply"); err == nil {
			t.Fatal("失败读取必须返回错误")
		}
	}
	assertReads(t, source, 2, "失败不得缓存")
}

// TestAIKeyProviderDoesNotCacheEmptyValue 未配置密钥时不得缓存空值：密钥补配后必须立刻生效。
func TestAIKeyProviderDoesNotCacheEmptyValue(t *testing.T) {
	// source 是初始返回空密钥的计数来源。
	source := &countingKeySource{value: ""}
	// provider 使用默认 TTL，若缓存空值则补配后读不到新密钥。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)
	// value 是首次读取结果；未配置时应为空串。
	if value := mustGet(t, provider, source, "cid"); value != "" {
		t.Fatalf("空密钥读取结果不符: %q", value)
	}
	// 补配密钥后再次读取，必须立刻拿到新值。
	source.value = "sk-late"
	// value 是补配后的读取结果；必须已是新密钥。
	if value := mustGet(t, provider, source, "cid"); value != "sk-late" {
		t.Fatalf("补配后必须立即生效: %q", value)
	}
	assertReads(t, source, 2, "空值不得进入缓存")
}

// TestAIKeyProviderRejectsEmptyLocator 缺少账号或设置键时必须拒绝读取，避免产生无归属的缓存条目。
func TestAIKeyProviderRejectsEmptyLocator(t *testing.T) {
	// ctx 是测试上下文。
	ctx := context.Background()
	// source 是计数来源，用于确认非法参数不会触达数据库。
	source := &countingKeySource{value: "sk"}
	// provider 使用默认 TTL。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)
	// cases 是非法定位参数组合。
	cases := []struct {
		// cookie 是账号标识。
		cookie string
		// key 是设置键。
		key string
	}{
		{cookie: "", key: "ai_api_key"},
		{cookie: "cid", key: "  "},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		// err 是本轮读取的失败原因。
		if _, err := provider.Get(ctx, source, c.cookie, c.key, "settings.use", "ai_reply"); err == nil {
			t.Fatalf("非法定位参数应被拒绝: cookie=%q key=%q", c.cookie, c.key)
		}
	}
	assertReads(t, source, 0, "非法参数不得读取来源")
	// err 是来源为 nil 时的失败原因；必须显式报错而不是 panic。
	if _, err := provider.Get(ctx, nil, "cid", "ai_api_key", "settings.use", "ai_reply"); err == nil {
		t.Fatal("来源为 nil 必须返回错误")
	}
}

// TestAIKeyProviderSeparatesAccountsAndKeys 缓存按账号与设置键隔离：不同账号互不命中。
func TestAIKeyProviderSeparatesAccountsAndKeys(t *testing.T) {
	// source 是计数来源。
	source := &countingKeySource{value: "sk-scope"}
	// provider 使用默认 TTL。
	provider := NewAIKeyProvider(defaultAIKeyCacheTTL, nil)
	mustGet(t, provider, source, "cid-a")
	mustGet(t, provider, source, "cid-b")
	assertReads(t, source, 2, "不同账号不得共用缓存")
}
