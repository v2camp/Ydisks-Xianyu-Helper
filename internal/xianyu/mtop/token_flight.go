// token_flight.go 按账号合并与限频 Token 刷新。
//
// 背景：一次聊天页刷新会并发触发几十个业务接口，每个接口在签名令牌过期时都会各自调用
// token API。若不合并，同一账号会在数百毫秒内打出几十次 token 请求，被平台判定为刷接口并
// 触发风控惩罚。本文件提供账号级串行、结果复用和最小间隔三层保护：
//  1. 同一账号的并发刷新串行执行，只有第一个调用真正请求平台；
//  2. 落在复用窗口内、且使用同一份 Cookie 指纹的调用直接复用真实请求结果；
//  3. 相邻两次真实请求保持最小间隔，网络抖动后的重连风暴不会连续打满平台。
//
// 该保护只覆盖签名令牌的刷新入口；风控验证链接的重取路径直接调用底层 token 请求，
// 不受这里的限频影响，保证人工接管验证的能力不被削弱。
package mtop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// tokenRefreshShareWindow 是同一账号复用最近一次真实 Token 刷新结果的窗口。
// 页面刷新等并发场景会在数百毫秒内提出大量刷新请求，命中窗口的调用直接复用结果，不再重复请求平台。
// 变量而非常量：测试可临时缩短该窗口验证过期后的重新请求。
var tokenRefreshShareWindow = 3 * time.Second

// tokenRefreshMinInterval 是同一账号两次真实 Token 请求之间的最小间隔。
// 网络抖动后的重连风暴会让同一账号连续刷新，这里用固定间隔给平台留出节奏，避免被判为刷接口。
// 变量而非常量：测试可临时缩短该间隔验证限频分支。
var tokenRefreshMinInterval = 1200 * time.Millisecond

// tokenFlightState 保存按账号分组的 Token 刷新合并状态；零值可用，生命周期与所属客户端实例一致。
type tokenFlightState struct {
	// mu 保护 groups 的并发读写。
	mu sync.Mutex
	// groups 保存账号键到刷新分组状态的映射，分组在首次使用时惰性创建。
	groups map[string]*tokenFlightGroup
}

// tokenFlightGroup 保存单个账号的刷新串行锁、最近请求时间与可复用结果。
type tokenFlightGroup struct {
	// gate 是该账号的串行锁；容量固定为 1，等待方必须响应 Context 取消。
	gate chan struct{}
	// lastStarted 是最近一次真实 Token 请求的开始时间，用于计算最小间隔。
	lastStarted time.Time
	// shared 是最近一次真实请求结果快照；为空表示当前没有可复用结果。
	shared *tokenFlightSnapshot
}

// tokenFlightSnapshot 保存一次真实 Token 请求的结果快照，供窗口内的并发调用复用。
type tokenFlightSnapshot struct {
	// cookiesFingerprint 是本次真实请求使用的 Cookie 指纹；指纹不同的调用不得复用结果。
	cookiesFingerprint string
	// result 是真实请求返回的刷新结果；失败调用保留返回体以便调用方读取 Set-Cookie。
	result *RefreshResult
	// err 是真实请求返回的错误；只有可安全复用的错误才会进入本快照。
	err error
	// at 是本快照的写入时间，用于判断复用窗口是否仍然有效。
	at time.Time
}

// group 返回账号键对应的刷新分组，首次使用时惰性创建。
// key 是账号键；调用方必须保证 key 非空，空键会导致所有匿名调用串行在同一分组上。
func (s *tokenFlightState) group(key string) *tokenFlightGroup {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups == nil {
		s.groups = make(map[string]*tokenFlightGroup)
	}
	// existing 是已存在的分组；为空表示需要新建。
	existing := s.groups[key]
	if existing != nil {
		return existing
	}
	// created 是新建的账号分组；gate 容量为 1 才能保证串行。
	created := &tokenFlightGroup{gate: make(chan struct{}, 1)}
	s.groups[key] = created
	return created
}

// Do 以账号级串行与结果复用执行一次 Token 刷新。
// ctx 控制等待与真实请求的生命周期；key 是账号键；cookiesFingerprint 是本次调用的 Cookie 指纹；
// call 是真正请求平台的函数。返回值与各自独立调用保持一致：
//   - 并发调用中只有第一个真正执行 call，其余调用在复用窗口内直接获得同一结果；
//   - 等待串行锁时 ctx 取消，返回 ctx 错误且不消耗平台请求；
//   - 真实请求因调用方 Context 取消或超时而失败时不写入可复用结果，后续等待者可以重新发起请求。
func (s *tokenFlightState) Do(ctx context.Context, key, cookiesFingerprint string, call func(context.Context) (*RefreshResult, error)) (*RefreshResult, error) {
	// group 是当前账号的刷新分组；同账号调用都必须经过它的串行锁。
	group := s.group(key)
	// acquire 尝试取得串行锁；等待期间响应调用方取消信号。
	select {
	case group.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-group.gate }()
	// shared 是当前可复用的结果快照；只有指纹一致且仍在窗口内才允许复用。
	shared := group.shared
	if shared != nil && shared.cookiesFingerprint == cookiesFingerprint && time.Since(shared.at) < tokenRefreshShareWindow {
		return shared.result, shared.err
	}
	// wait 是本次调用为满足最小间隔还需要等待的时长；为零表示可以立即请求。
	wait := tokenRefreshMinInterval - time.Since(group.lastStarted)
	if wait > 0 {
		// timer 与 cancel 为等待提供可取消的计时器，避免持有串行锁时无法响应取消。
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// startedAt 记录真实请求的开始时间，供下一次调用的最小间隔计算使用。
	startedAt := time.Now()
	// result、callErr 是本次真实请求的结果与错误。
	result, callErr := call(ctx)
	// ctxErr 标记调用方 Context 触发的失败；这类失败不能毒化后续等待者。
	ctxErr := errors.Is(callErr, context.Canceled) || errors.Is(callErr, context.DeadlineExceeded)
	if ctxErr {
		// 取消类失败仍推进最小间隔窗口，避免等待者立刻无间隔重试。
		group.lastStarted = startedAt
		return result, callErr
	}
	group.lastStarted = startedAt
	group.shared = &tokenFlightSnapshot{
		cookiesFingerprint: cookiesFingerprint,
		result:             result,
		err:                callErr,
		at:                 time.Now(),
	}
	return result, callErr
}

// tokenRefreshAccountKey 从 Cookie 串推导账号级刷新键。
// 优先使用 unb（账号标识），保证 Cookie 更新前后仍命中同一分组与限频状态；
// 缺少 unb 时退化为 Cookie 指纹，此时只能合并使用完全相同 Cookie 的并发调用。
func tokenRefreshAccountKey(cookiesStr string) string {
	if // unb 是 Cookie 中的账号标识；非空时是跨刷新稳定的分组键。
	unb := strings.TrimSpace(protocol.TransCookies(cookiesStr)["unb"]); unb != "" {
		return "unb:" + unb
	}
	return "ck:" + tokenRefreshCookiesFingerprint(cookiesStr)
}

// tokenRefreshCookiesFingerprint 计算 Cookie 串的稳定指纹，用于判断结果是否可以复用。
// 指纹只用于本地比较，不进入日志或远端请求。
func tokenRefreshCookiesFingerprint(cookiesStr string) string {
	// digest 是 Cookie 串的 SHA-256 摘要，避免在内存中长期保存明文比较结果。
	digest := sha256.Sum256([]byte(cookiesStr))
	return hex.EncodeToString(digest[:])
}