// ai_key_provider.go 为 AI 回复链提供带 TTL 的模型密钥读取缓存。
//
// 背景：db.Store.ReadSensitiveSetting 是 fail-closed 的安全审计入口，每调用一次就写一行
// security_audit_logs。AI 回复链每轮都要读一次 ai_api_key，按每天 300 条回复计算，
// 一个月会写出约 9000 行「同一账号、同一动作、同一资源」的重复审计。审计本身不能削弱，
// 因此在读取侧加一层有界缓存：缓存未命中（真实读取）仍然只走 ReadSensitiveSetting 并照写审计，
// 命中缓存则完全不进数据库。
//
// 并发模型（AGENTS 1.6）：
//   - 内存归属：缓存表、TTL 与时钟都属于 AIKeyProvider 实例。生产环境由进程级单例
//     （DefaultAIKeyProvider）共享，各账号的 AIReplier 只持有它的引用，不复制也不重置状态。
//   - 锁保护：mu 只保护 entries 的读写；ttl、now、flight 在构造期固化，之后只读不写。
//   - 加锁顺序：全文件只有 mu 一把锁，持锁期间禁止数据库、网络与日志 I/O；
//     singleflight 的合并发生在锁外，因此不存在「持锁等 I/O」，也不存在与其他锁的嵌套。
//   - 关停：provider 不持有 goroutine、ticker 或后台任务，进程退出无需额外关闭动作。
//
// 语义变化：审计口径从「每次访问一行」变为「每次真实读取（缓存未命中）一行」。
// 运维含义：密钥轮换后最长要等一个 TTL 才会在全部实例上生效，所以设置写入口
// （应用层 ApplySystemChanges / SetSystem）必须在敏感键落库后显式失效缓存。
// 空值（未配置密钥）不进入缓存：只有「真正读到密钥」才值得缓存，否则密钥一旦补配
// 也要等 TTL 才生效，那属于比重复审计更严重的可用性问题。
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// aiKeyCacheTTLEnv 是密钥缓存有效期的环境变量名，单位为秒；沿用 AI 相关开关的秒级环境变量风格。
	aiKeyCacheTTLEnv = "XIANYU_AI_KEY_CACHE_TTL_SECONDS"
	// defaultAIKeyCacheTTL 是默认缓存时长：10 分钟足以把重复审计压到可忽略，又不至于让密钥轮换长时间不生效。
	defaultAIKeyCacheTTL = 10 * time.Minute
	// minAIKeyCacheTTL 是缓存时长下限：更短的 TTL 只会把审计行数加回来，失去缓存意义。
	minAIKeyCacheTTL = time.Minute
)

// aiKeySource 是敏感密钥读取来源端口；生产实现是 *db.Store，测试可注入计数型假实现。
// 只声明一条读取方法，保证 provider 不感知数据库类型与 SQL 方言。
type aiKeySource interface {
	// ReadSensitiveSettingForAccount 按账号所有者读取敏感设置并写访问审计；失败即拒绝返回密钥。
	ReadSensitiveSettingForAccount(ctx context.Context, cookieID, key, action, resource string) (string, error)
}

// aiKeyCacheKey 是缓存条目的定位键：来源实例 + 账号 + 设置键。
// 显式带上来源实例，避免测试或多库进程中同一账号标识命中彼此的数据。
// 注意：来源必须可比较（当前实现均为指针），否则不能作为映射键。
type aiKeyCacheKey struct {
	// source 是读取来源实例，用于隔离不同存储或不同测试库。
	source aiKeySource
	// cookieID 是账号标识，决定缓存归属与失效范围。
	cookieID string
	// settingKey 是敏感设置键名，便于同一账号未来缓存多个键。
	settingKey string
}

// aiKeyEntry 是一条密钥缓存快照；只保存密钥原文与到期时刻，不保存审计上下文。
// 空密钥不会被写入缓存表，因此 value 在实际存储的条目里恒为非空。
type aiKeyEntry struct {
	// value 是解密后的密钥原文，不会是空串。
	value string
	// expiresAt 是该条目的到期时刻；now() 不早于它即视为过期。
	expiresAt time.Time
}

// AIKeyProvider 是带 TTL 的密钥读取缓存，可并发使用。
// 缓存只存密钥字符串，不写日志、不进入错误信息（AGENTS 1.7）。
type AIKeyProvider struct {
	// ttl 是缓存有效期；<=0 表示禁用缓存，每次读取都直连来源（排障与测试用）。
	ttl time.Duration
	// now 是统一时钟；测试注入以驱动 TTL 过期，生产为 time.Now。
	now func() time.Time
	// mu 保护 entries 的并发读写；持锁期间只做内存操作，禁止任何 I/O。
	mu sync.Mutex
	// entries 保存定位键到密钥快照的映射；实例生命周期内一直存在，不自动清理过期键（写入时覆盖）。
	entries map[aiKeyCacheKey]aiKeyEntry
	// flight 合并同一个键的并发未命中读取，保证并发未命中只打一次库、只写一行审计。
	flight singleflight.Group
}

// defaultAIKeyProviderOnce 保证进程级共享实例只构造一次。
var defaultAIKeyProviderOnce sync.Once

// defaultAIKeyProviderInstance 是进程级共享的密钥缓存实例；TTL 来自环境变量。
var defaultAIKeyProviderInstance *AIKeyProvider

// NewAIKeyProvider 构造密钥缓存；ttl<=0 表示禁用缓存，now 为 nil 时使用墙钟。
// 返回的实例可并发使用，不持有 goroutine，构造后无需关停。
func NewAIKeyProvider(ttl time.Duration, now func() time.Time) *AIKeyProvider {
	if now == nil {
		now = time.Now
	}
	return &AIKeyProvider{ttl: ttl, now: now, entries: make(map[aiKeyCacheKey]aiKeyEntry)}
}

// DefaultAIKeyProvider 返回进程级共享的密钥缓存，TTL 由 XIANYU_AI_KEY_CACHE_TTL_SECONDS 决定。
// 组合根用它注入设置写入口的失效端口，引擎各调用点用它共享同一份缓存。
func DefaultAIKeyProvider() *AIKeyProvider {
	defaultAIKeyProviderOnce.Do(func() {
		defaultAIKeyProviderInstance = NewAIKeyProvider(aiKeyCacheTTLFromEnv(os.Getenv), nil)
	})
	return defaultAIKeyProviderInstance
}

// aiKeyCacheTTLFromEnv 解析缓存 TTL 环境变量：空值与非法值回落默认，0 表示禁用缓存，负数回落默认。
func aiKeyCacheTTLFromEnv(getenv func(string) string) time.Duration {
	// raw 是环境变量原文；空串表示未配置。
	raw := strings.TrimSpace(getenv(aiKeyCacheTTLEnv))
	if raw == "" {
		return defaultAIKeyCacheTTL
	}
	// seconds 是解析出的秒数；非数字按未配置处理，避免排障时因笔误意外关闭缓存。
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return defaultAIKeyCacheTTL
	}
	if seconds == 0 {
		return 0
	}
	if seconds < 0 {
		return defaultAIKeyCacheTTL
	}
	// ttl 是换算出的有效期；低于下限时抬到下限。
	ttl := time.Duration(seconds) * time.Second
	if ttl < minAIKeyCacheTTL {
		return minAIKeyCacheTTL
	}
	return ttl
}

// Get 读取指定账号的敏感密钥；命中缓存时不访问数据库，也不会新增审计行。
// 参数 ctx 是本次读取的作用域（未命中时传给来源，取消会同时影响合并中的等待方）；
// source 是读取来源；cookieID 是审计归属账号；settingKey 是敏感设置键（白名单由来源侧校验）；
// action 与 resource 是审计语义标签，只在真实读取时使用。
// 返回值是解密后的密钥原文，调用方不得写入日志、错误信息或任何响应体。
func (p *AIKeyProvider) Get(ctx context.Context, source aiKeySource, cookieID, settingKey, action, resource string) (string, error) {
	if source == nil {
		return "", errors.New("密钥读取来源未初始化")
	}
	// normalizedCookie、normalizedKey 是去掉首尾空白的定位参数，避免同一账号因空白生成两份缓存。
	normalizedCookie, normalizedKey := strings.TrimSpace(cookieID), strings.TrimSpace(settingKey)
	if normalizedCookie == "" || normalizedKey == "" {
		return "", errors.New("密钥读取缺少账号或设置键")
	}
	if p == nil || p.ttl <= 0 {
		// 禁用缓存时直连来源：排障可用 TTL=0 恢复「每次访问都写审计」的旧口径。
		return source.ReadSensitiveSettingForAccount(ctx, normalizedCookie, normalizedKey, action, resource)
	}
	// key 是本条目的缓存定位键；同一账号的不同设置键互不影响。
	key := aiKeyCacheKey{source: source, cookieID: normalizedCookie, settingKey: normalizedKey}
	// cached、hit 是本次查表结果；命中时直接返回，不进 singleflight 也不碰数据库。
	if cached, hit := p.lookup(key); hit {
		return cached, nil
	}
	// value、err 是合并后的读取结果；并发未命中的调用共享同一次真实读取。
	value, err, _ := p.flight.Do(p.flightKey(key), func() (any, error) {
		// cached、hit 是二次检查结果：等待期间可能已被上一个完成的调用写入缓存。
		if cached, hit := p.lookup(key); hit {
			return cached, nil
		}
		// fetched、readErr 是本次真实读取的结果；失败不进入缓存，下次调用仍会重试并重写审计。
		fetched, readErr := source.ReadSensitiveSettingForAccount(ctx, normalizedCookie, normalizedKey, action, resource)
		if readErr != nil {
			return "", readErr
		}
		// 只缓存非空密钥：未配置状态不缓存，密钥补配后立刻生效，不必等 TTL 到期。
		if strings.TrimSpace(fetched) != "" {
			p.store(key, fetched)
		}
		return fetched, nil
	})
	if err != nil {
		return "", err
	}
	// cachedValue、ok 断言合并结果是字符串；类型不符只可能是内部错误，按失败处理而不是返回空密钥。
	cachedValue, ok := value.(string)
	if !ok {
		return "", errors.New("密钥缓存返回了非字符串结果")
	}
	return cachedValue, nil
}

// Invalidate 失效指定账号的全部密钥缓存；设置写入后由上层调用，让轮换立即生效。
// 账号标识按去空白比较；空标识不做任何事，避免误清全表。
func (p *AIKeyProvider) Invalidate(cookieID string) {
	if p == nil {
		return
	}
	// normalizedCookie 是与 Get 一致的空白规范化结果。
	normalizedCookie := strings.TrimSpace(cookieID)
	if normalizedCookie == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// key 是当前遍历到的缓存定位键；命中目标账号即删除。
	for key := range p.entries {
		if key.cookieID == normalizedCookie {
			delete(p.entries, key)
		}
	}
}

// InvalidateAll 清空全部密钥缓存；敏感设置写入后由应用层经窄接口调用。
// 处于禁用缓存状态时同样是安全的空操作。
func (p *AIKeyProvider) InvalidateAll() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make(map[aiKeyCacheKey]aiKeyEntry)
}

// lookup 在锁内读取未过期的缓存值；返回的 ok 表示是否命中。
// 过期条目在此处顺手删除，避免无效快照长期占用内存。
func (p *AIKeyProvider) lookup(key aiKeyCacheKey) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// entry、exists 是缓存表中的快照与存在性。
	entry, exists := p.entries[key]
	if !exists {
		return "", false
	}
	if !p.now().Before(entry.expiresAt) {
		delete(p.entries, key)
		return "", false
	}
	return entry.value, true
}

// store 在锁内写入缓存快照；调用方必须已在锁外完成 I/O。
func (p *AIKeyProvider) store(key aiKeyCacheKey, value string) {
	// expiresAt 是本条目的到期时刻，写入时按当前时钟计算一次。
	expiresAt := p.now().Add(p.ttl)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = make(map[aiKeyCacheKey]aiKeyEntry)
	}
	p.entries[key] = aiKeyEntry{value: value, expiresAt: expiresAt}
}

// flightKey 把缓存定位键转成 singleflight 的字符串键。
// 只含来源指针、账号与设置键，不含密钥内容，避免密钥出现在任何可被打印的结构里。
func (p *AIKeyProvider) flightKey(key aiKeyCacheKey) string {
	return fmt.Sprintf("%p|%s|%s", key.source, key.cookieID, key.settingKey)
}
