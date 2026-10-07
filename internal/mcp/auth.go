// auth.go 实现 MCP 端点的 Bearer 令牌鉴权：
//   - 仅接受 Authorization: Bearer <token> 方案；
//   - 环境引导令牌（XIANYU_MCP_TOKEN）与持久化令牌均可通过，二者都做常量时间比较；
//   - 按来源 IP 记录失败次数，窗口内超阈值短暂封禁，正确令牌不受其他来源封禁影响。

package mcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// authFailureWindow 是同一来源失败计数的滑动窗口长度。
	authFailureWindow = 5 * time.Minute
	// authFailuresPerSource 是窗口内允许的最大失败次数，达到后在窗口剩余时间内拒绝。
	authFailuresPerSource = 20
	// authLimiterMaxBuckets 是失败计数桶的上限，超过后顺带清理过期桶，防止内存无界增长。
	authLimiterMaxBuckets = 2048
)

// bearerScheme 是 Authorization 头要求的方案前缀（按 RFC 大小写不敏感处理）。
const bearerScheme = "bearer "

// failureBucket 记录单个来源在当前窗口内的失败次数与窗口截止时刻。
type failureBucket struct {
	// count 是窗口内累计失败次数。
	count int
	// expires 是该失败窗口的截止时刻；到达后计数重置。
	expires time.Time
}

// sourceLimiter 按来源 IP 记录鉴权失败；所有方法可并发调用，由内部互斥锁保护。
type sourceLimiter struct {
	// mu 保护 buckets 映射的全部读写；持锁期间只做内存操作。
	mu sync.Mutex
	// buckets 是来源键到失败桶的映射。
	buckets map[string]failureBucket
	// now 返回当前时间，测试可注入可控时钟。
	now func() time.Time
}

// newSourceLimiter 构造来源失败限流器。
func newSourceLimiter(now func() time.Time) *sourceLimiter {
	if now == nil {
		now = time.Now
	}
	return &sourceLimiter{buckets: make(map[string]failureBucket), now: now}
}

// allow 报告指定来源此刻是否放行，并返回仍需等待的封禁时长（放行时为 0）。
func (l *sourceLimiter) allow(source string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// bucket、ok 是该来源当前窗口的失败桶及其存在性。
	bucket, ok := l.buckets[source]
	if !ok || !now.Before(bucket.expires) {
		return true, 0
	}
	// retryAfter 是窗口剩余封禁时长；未达阈值时放行。
	if bucket.count < authFailuresPerSource {
		return true, 0
	}
	return false, bucket.expires.Sub(now)
}

// recordFailure 为指定来源累加一次失败计数，过期窗口自动重新起算。
func (l *sourceLimiter) recordFailure(source string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// bucket 是更新前的失败桶；窗口过期则从空桶重新计数。
	bucket, ok := l.buckets[source]
	if !ok || !now.Before(bucket.expires) {
		bucket = failureBucket{expires: now.Add(authFailureWindow)}
	}
	bucket.count++
	l.buckets[source] = bucket
	if len(l.buckets) > authLimiterMaxBuckets {
		// key、expired 是遍历到的桶键与过期判定；顺手清理过期桶限制内存。
		for key, expired := range l.buckets {
			if !now.Before(expired.expires) {
				delete(l.buckets, key)
			}
		}
	}
}

// bearerToken 从请求头提取 Bearer 令牌；方案错误、令牌为空或出现多段时返回空串。
func bearerToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	// header 是原始 Authorization 头值。
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < len(bearerScheme) {
		return ""
	}
	if !strings.EqualFold(header[:len(bearerScheme)], bearerScheme) {
		return ""
	}
	// token 是方案前缀后的剩余内容；禁止内部空白，避免意外携带多段凭证。
	token := strings.TrimSpace(header[len(bearerScheme):])
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return ""
	}
	return token
}

// constantTimeEqual 在两串等长时以常量时间比较；长度不同直接为假但同样不泄露比较内容。
func constantTimeEqual(expected, presented string) bool {
	if expected == "" || len(expected) != len(presented) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) == 1
}

// authenticate 校验 presented 令牌：优先环境引导令牌，其次持久化令牌。
// 命中时返回令牌来源；任何不匹配或查询错误都返回空来源与失败判定，错误细节不写入 HTTP 响应。
func authenticate(ctx context.Context, cfg ConfigPort, environmentToken, presented string) (TokenSource, bool) {
	// 环境令牌非空时先做常量时间比较；环境令牌不入库、不经哈希。
	if constantTimeEqual(strings.TrimSpace(environmentToken), presented) {
		return SourceEnvironment, true
	}
	// matched、err 是持久化令牌的校验结果与仓储错误；错误按不匹配处理以避免泄露存储状态。
	matched, err := cfg.VerifyPersistedToken(ctx, presented)
	if err != nil || !matched {
		return "", false
	}
	return SourcePersisted, true
}
