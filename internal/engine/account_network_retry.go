// 本文件集中存放账号运行时的网络退避与故障判定逻辑，均为同包内纯搬移，行为不变。
package engine

import (
	"crypto/rand"
	"math/big"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/ws"
)

// retryDelay 按错误类型计算退避，并加入 0-30% 抖动。
// 多账号同时断线时，纯固定退避会让所有账号在同一秒重连，容易形成重连风暴。
// retryDelay 封装重试延迟业务协调。
func (a *Account) retryDelay(errMsg string) time.Duration {
	a.runtimeMu.Lock()
	// f 用于本次流程后续判断的f
	f := a.connFailures
	a.runtimeMu.Unlock()
	if f < 1 {
		f = 1
	}
	// base 用于本次流程后续判断的base
	base := exponentialSeconds(f)
	// secs 用于本次流程后续判断的secs
	secs := 0
	switch {
	case contains(errMsg, "no close frame received or sent"):
		secs = min(base, 30)
	case contains(errMsg, "connection refused") || contains(errMsg, "timeout"):
		secs = min(2*base, 90)
	default:
		secs = min(base, 45)
	}
	return withRetryJitter(time.Duration(secs) * time.Second)
}

// networkRetryDelay 封装network重试延迟业务协调。
func (a *Account) networkRetryDelay() time.Duration {
	a.runtimeMu.Lock()
	// f 用于本次流程后续判断的f
	f := a.networkFailures
	a.runtimeMu.Unlock()
	if f < 1 {
		f = 1
	}
	// base 是叠加账号错峰前的指数退避时长。
	base := time.Duration(min(2+exponentialSeconds(f), 60)) * time.Second
	// staggered 是加入账号专属错峰后的退避时长。
	staggered := base + accountReconnectStagger(a.CookieID)
	return withRetryJitter(staggered)
}

// accountReconnectStagger 返回账号专属的重连错峰量，取值落在 [1s, reconnectStaggerSpan]。
// 使用账号标识的稳定哈希而不是随机数：同一账号的重连节奏可预期，不同账号之间自然分散，
// 避免多账号在同一时刻集中重连。
func accountReconnectStagger(cookieID string) time.Duration {
	// sum 是账号标识字节的稳定累加值，不涉及任何敏感信息。
	sum := 0
	// ch 表示账号标识中的当前字节。
	for _, ch := range []byte(cookieID) {
		sum = (sum*31 + int(ch)) % int(reconnectStaggerSpan/time.Second)
	}
	if sum < 0 {
		sum = -sum
	}
	return time.Duration(sum+1) * time.Second
}

// exponentialSeconds 封装exponential秒数业务协调。
func exponentialSeconds(failures int) int {
	if failures < 1 {
		failures = 1
	}
	if failures > 30 {
		failures = 30
	}
	return 1 << failures
}

// withRetryJitter 封装with重试Jitter业务协调。
func withRetryJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	// maxJitter 用于本次流程后续判断的maxJitter
	maxJitter := base * 3 / 10
	if maxJitter <= 0 {
		return base
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxJitter)))
	if err != nil {
		// 熵源异常时使用时间纳秒兜底；这里只影响退避抖动，不用于安全令牌。
		return base + time.Duration(time.Now().UnixNano()%int64(maxJitter))
	}
	return base + time.Duration(n.Int64())
}

// isEstablishedNetworkError 封装isEstablishedNetwork错误业务协调。
func isEstablishedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(err.Error())
	// marker 表示当前遍历过程中的marker
	for _, marker := range []string{
		"connectionclosed", "no close frame received or sent", "connection reset",
		"connectionreseterror", "timeouterror", "timeout", "websocket: close",
		"received close frame", "failed to read frame", "unexpected eof", " eof",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// recordNetworkFailure 递增网络断线计数，驱动 networkRetryDelay 的退避阶梯。
func (a *Account) recordNetworkFailure() {
	a.runtimeMu.Lock()
	a.networkFailures++
	a.runtimeMu.Unlock()
}

// isTransientDialError 判断 WebSocket 拨号（握手）阶段的错误是否为与登录凭证无关的瞬时网络故障。
//
// 背景：握手失败的默认处理是「视为凭证失效 → 终止账号运行并提示重新登录」，这符合官网
// /im 页面在 CONN_ERROR 后展示重新登录入口的行为。但拨号阶段的错误里混有大量与凭证无关的
// 瞬时故障（DNS 解析失败、连接被拒、网络不可达、拨号超时、TLS 握手超时）。把它们一并当作
// 凭证失效，会让一次秒级的 DNS 抖动演变成账号永久停摆：连接循环退出后没有任何组件会重新拉起。
//
// 判定顺序：服务端明确拒绝的认证类错误优先排除，避免把真正的凭证问题降级为可重试错误。
func isTransientDialError(err error) bool {
	if err == nil {
		return false
	}
	// 认证类错误是服务端明确拒绝，必须走重新登录，不参与重试。
	if ws.IsInvalidTokenError(err) || ws.IsAuthenticationError(err) || ws.IsConnectLimitError(err) {
		return false
	}
	// msg 是归一化后的错误文本，仅用于匹配传输层故障标识。
	msg := strings.ToLower(err.Error())
	// marker 表示当前遍历过程中的传输层瞬时故障标识。
	for _, marker := range []string{
		// DNS：Docker 内置解析器故障与解析不到主机。
		"server misbehaving", "no such host", "name resolution",
		// 网络层：不可达、被拒、被重置。
		"network is unreachable", "connection refused", "connection reset",
		// 超时：拨号、读写、TLS 握手。
		"i/o timeout", "dial tcp", "tls handshake timeout",
		// 握手报文阶段中断。
		"failed to send handshake request", "unexpected eof", " eof", "timeout",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// recordShortDisconnect 封装recordShortDisconnect业务协调。
func (a *Account) recordShortDisconnect(connectedDuration time.Duration) bool {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if connectedDuration >= ShortConnectionThreshold {
		a.shortDisconnects = nil
		return false
	}
	// now 用于本次流程后续判断的now
	now := time.Now()
	a.shortDisconnects = append(a.shortDisconnects, now)
	// cutoff 用于本次流程后续判断的cutoff
	cutoff := now.Add(-FrequentDisconnectWindow)
	// kept 用于本次流程后续判断的kept
	kept := a.shortDisconnects[:0]
	// disconnectedAt 表示当前遍历过程中的disconnectedAt
	for _, disconnectedAt := range a.shortDisconnects {
		if !disconnectedAt.Before(cutoff) {
			kept = append(kept, disconnectedAt)
		}
	}
	a.shortDisconnects = kept
	return len(a.shortDisconnects) >= FrequentDisconnectLimit
}
