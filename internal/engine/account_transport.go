package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// tokenRotationGraceWindow 是上次 token 临近到期时允许快速重试的时间窗口。
// 只有首次获取失败且上次 token 仍落在窗口内，才按健康连接轮换失败处理。
const tokenRotationGraceWindow = 2 * time.Minute

// tokenRetryMaxDelay 是连续获取 token 失败时的退避上限。
// 永久无效的凭证不应继续以秒级节奏冲击平台 token 接口。
const tokenRetryMaxDelay = 15 * time.Minute

// acquireToken 为当前账号取得连接级 Token 与其绑定 Cookie 快照，供 WebSocket 注册使用。
func (a *Account) acquireToken(ctx context.Context) (string, string, error) {
	return a.acquireTokenWithMinGap(ctx, false)
}

// acquireRuntimeToken is retained as a compatibility wrapper for focused
// tests and older internal callers. It follows the same fresh-token rule.
// acquireRuntimeToken 封装acquireRuntime令牌业务协调。
func (a *Account) acquireRuntimeToken(ctx context.Context) (string, string, error) {
	return a.acquireFreshConnectionToken(ctx)
}

// acquireTokenWithMinGap 封装acquire令牌WithMinGap业务协调。
func (a *Account) acquireTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	// Invalidate any access token left by an older process/attempt before asking
	// MTOP for the token that will be bound to this connection.
	a.clearTokenCache(ctx)
	return a.refreshToken(ctx)
}

// setLastTokenStatus 封装setLast令牌状态业务协调。
func (c *credentialCoordinator) setLastTokenStatus(status string) {
	// a 是本凭证协调器绑定的账号 facade，持有刷新诊断状态。
	a := c.account
	a.mu.Lock()
	a.lastTokenStatus = status
	a.mu.Unlock()
}

// classifyTokenFailure 封装classify令牌Failure业务协调。
func classifyTokenFailure(err error) string {
	if err == nil {
		return tokenRefreshFailedAPI
	}
	if mtop.IsSessionExpiredErr(err) {
		return tokenRefreshFailedSession
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(err.Error(), "超时") {
		return tokenRefreshFailedTimeout
	}
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "network") || strings.Contains(msg, "connection") || strings.Contains(msg, "请求失败") {
		return tokenRefreshFailedNetwork
	}
	return tokenRefreshFailedAPI
}

// tokenFailureIsNonCounted 封装令牌FailureIsNonCounted业务协调。
func tokenFailureIsNonCounted(status string) bool {
	switch status {
	case tokenRefreshFailedCaptcha, tokenRefreshFailedCaptchaError, tokenRefreshSkippedCooldown:
		return true
	default:
		return false
	}
}

// RuntimeStatus 返回账号当前连接状态的线程安全快照。
func (a *Account) RuntimeStatus() RuntimeStatus {
	a.runtimeMu.Lock()
	// state 是当前运行状态枚举快照。
	state := a.runtimeState
	// message 是当前运行状态说明快照。
	message := a.runtimeMessage
	// connected 是当前连接存在且状态为 online 的快照。
	connected := a.conn != nil && a.runtimeState == RuntimeOnline
	// failures 是连续连接失败次数快照。
	failures := a.connFailures
	// updatedAt 是状态最近更新时间快照。
	updatedAt := a.runtimeUpdatedAt
	a.runtimeMu.Unlock()

	a.mu.Lock()
	// remaining 用于本次流程后续判断的remaining
	remaining := int64(0)
	if !a.tokenExpiresAt.IsZero() {
		remaining = int64(time.Until(a.tokenExpiresAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	// status 用于本次流程后续判断的状态
	status := RuntimeStatus{
		State:                 state,
		Message:               message,
		Connected:             connected,
		Failures:              failures,
		UpdatedAt:             updatedAt,
		TokenAcquiredAt:       a.tokenAcquiredAt,
		TokenExpiresAt:        a.tokenExpiresAt,
		TokenRefreshAt:        a.tokenRefreshAt,
		TokenRemainingSeconds: remaining,
		TokenRefreshStatus:    a.lastTokenStatus,
	}
	a.mu.Unlock()
	return status
}

// recordMessageReceived 更新最近收到消息时间，供 messageDispatcher 使用。
func (a *Account) recordMessageReceived(receivedAt time.Time) {
	a.runtimeMu.Lock()
	a.lastMsgReceived = receivedAt
	a.runtimeMu.Unlock()
}

// tokenRetryBase 返回不含抖动的 token 获取失败退避基准。
// 首次失败且上次 token 临近到期时按轮换场景 30 秒快速重试；连续失败按 1/2/4/8/15
// 分钟阶梯增长，避免陈旧的 tokenExpiresAt 把间隔永久钉在 30 秒。
// 本方法只读取 a.mu 保护的诊断状态，不执行外部 I/O，可在测试中确定性断言。
func (a *Account) tokenRetryBase() time.Duration {
	a.mu.Lock()
	// expiresAt 是上次成功 token 的过期时间；failures 是当前连续获取失败次数。
	expiresAt, failures := a.tokenExpiresAt, a.tokenFetchFailures
	a.mu.Unlock()
	if failures < 1 {
		failures = 1
	}
	// 轮换窗口内的首次失败：上次 token 仍在快速恢复窗口，保留 30 秒重试。
	if failures == 1 && !expiresAt.IsZero() && time.Until(expiresAt) <= tokenRotationGraceWindow {
		return 30 * time.Second
	}
	// step 把连续失败次数映射为指数阶梯，超过 4 次后固定在 16 分钟档再受上限裁剪。
	step := failures - 1
	if step > 4 {
		step = 4
	}
	// base 是阶梯计算出的分钟级退避基准，再受统一上限约束。
	base := time.Duration(1<<step) * time.Minute
	if base > tokenRetryMaxDelay {
		base = tokenRetryMaxDelay
	}
	return base
}

// tokenRetryDelay 返回实际使用的 token 重试退避，在基准上叠加抖动避免多账号同步重试。
func (a *Account) tokenRetryDelay() time.Duration {
	return withRetryJitter(a.tokenRetryBase())
}

// notifyTransportReady 封装notifyTransportReady业务协调。
func (a *Account) notifyTransportReady(ctx context.Context) {
	if // handler、ok 用于本次流程后续判断的handler、ok
	handler, ok := a.handler.(transportReadyHandler); ok {
		handler.OnTransportReady(ctx, a.CookieID)
	}
}

// notifyInitialTransportReady 仅在本 Account 实例第一次成功注册 WebSocket 后异步通知订单同步端口。
// 任务由 accountLifecycle 登记、取消和等待；断线重连不会重复发起平台请求，账号重启会构造新实例并获得一次新机会。
func (a *Account) notifyInitialTransportReady() {
	// handler、ok 保存支持首次传输就绪回调的可选业务端口。
	handler, ok := a.handler.(initialTransportReadyHandler)
	if !ok {
		return
	}
	a.initialTransportReadyOnce.Do(func() {
		// taskCtx、finish、accepted 分别表示账号生命周期拥有的同步上下文、结束登记和当前是否允许新任务。
		taskCtx, finish, accepted := a.lifecycle.beginTask()
		if !accepted {
			return
		}
		go func() {
			defer finish()
			handler.OnInitialTransportReady(taskCtx, a.CookieID)
		}()
	})
}

// notifyReconnectTransportReady 在每次 WebSocket 注册成功后异步通知重连补同步端口。
// 与首次通知不同，它不做 once 限制：断线期间丢失的订单事件只能在重连后补同步。
// 任务由 accountLifecycle 登记、取消和等待，账号停止时不会留下游离协程。
func (a *Account) notifyReconnectTransportReady() {
	// handler、ok 保存支持重连补同步回调的可选业务端口。
	handler, ok := a.handler.(reconnectTransportReadyHandler)
	if !ok {
		return
	}
	// taskCtx、finish、accepted 分别表示账号生命周期拥有的同步上下文、结束登记和当前是否允许新任务。
	taskCtx, finish, accepted := a.lifecycle.beginTask()
	if !accepted {
		return
	}
	go func() {
		defer finish()
		handler.OnReconnectTransportReady(taskCtx, a.CookieID)
	}()
}

// setRuntimeState 封装setRuntime状态业务协调。
func (a *Account) setRuntimeState(state, message string) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.runtimeState = state
	a.runtimeMessage = message
	a.runtimeUpdatedAt = time.Now()
}

// setRuntimeError 根据 err 更新 a 的展示状态；ctx 只用于已有风控通知，Token 失败保持可重试状态。
func (a *Account) setRuntimeError(ctx context.Context, err error) {
	// msg 归一错误文本大小写，兼容尚未返回结构化错误的调用方。
	msg := strings.ToLower(errString(err))
	a.runtimeMu.Lock()
	// prev 是加锁读取的旧状态，用于阻止风控提示重复发送。
	prev := a.runtimeState
	a.runtimeMu.Unlock()
	switch {
	case strings.Contains(msg, "验证"), strings.Contains(msg, "captcha"), strings.Contains(msg, "risk"), strings.Contains(msg, "rgv587"), strings.Contains(msg, "fail_sys_user_validate"):
		a.setRuntimeState(RuntimeVerificationRequired, "闲鱼要求安全验证，请重新扫码并完成验证")
		// 仅在从非验证状态转入时告警一次，避免重复刷屏。
		if prev != RuntimeVerificationRequired {
			a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求安全验证",
				"账号触发闲鱼风控验证（滑块/短信/人脸等）。系统可能无法自动恢复，请前往后台扫码完成验证。")
		}
	case mtop.IsMTopTokenExpiredErr(err), strings.Contains(msg, "token_exoired"), strings.Contains(msg, "token_expired"), strings.Contains(msg, "token_empty"):
		a.setRuntimeState(RuntimeReconnecting, "MTOP Token 刷新失败，等待重试")
	case mtop.IsSessionExpiredErr(err), strings.Contains(msg, "cookie 缺少 unb"):
		a.setRuntimeState(RuntimeAuthExpired, "登录凭证已失效，请重新扫码登录")
	default:
		a.setRuntimeState(RuntimeReconnecting, "连接异常，系统将在限速后自动重试")
	}
}

// SendText 通过当前 WebSocket 给买家发送文本消息。
func (a *Account) SendText(ctx context.Context, chatID, toUserID, text string) error {
	return a.outgoing.sendText(ctx, chatID, toUserID, text)
}

// MarkChatRead 向已连接的 WebSocket 上报会话消息已读状态，调用方负责用户所有权校验。
func (a *Account) MarkChatRead(ctx context.Context, chatID string, messageIDs []map[string]any) error {
	// conn、err 保存当前连接快照及不可发送时的生命周期错误。
	conn, _, err := a.outgoing.currentSenderState()
	if err != nil {
		return err
	}
	// readCtx、cancel 为平台上报设置固定超时，取消责任在本方法返回时释放。
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// reader、ok 保存连接是否支持可选的已读上报能力。
	reader, ok := conn.(interface {
		MarkChatRead(context.Context, string, []map[string]any) error
	})
	if !ok {
		return fmt.Errorf("当前 WebSocket 不支持已读上报")
	}
	return reader.MarkChatRead(readCtx, chatID, messageIDs)
}

// SendImage 通过当前 WebSocket 给买家发送图片消息。当前仅支持可直接访问的 CDN/公网 URL。
// width/height 为图片真实尺寸，单位为像素；传入非正值时由 WebSocket 使用默认尺寸。
func (a *Account) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, width, height int) error {
	return a.outgoing.sendImage(ctx, chatID, toUserID, imageURL, cardID, width, height)
}

// SendItemCard 通过当前 WebSocket 给指定个人会话发送商品卡片。
func (a *Account) SendItemCard(ctx context.Context, chatID, toUserID, itemID, title, imageURL, price string) error {
	return a.outgoing.sendItemCard(ctx, chatID, toUserID, itemID, title, imageURL, price)
}

// FetchChatHistory reuses the account's registered IM connection. Keeping this
// optional capability outside WSConn avoids forcing non-chat test transports to
// implement history retrieval.
// FetchChatHistory 封装Fetch聊天History业务协调。
func (a *Account) FetchChatHistory(ctx context.Context, chatID string, cursor int64, limit int) (map[string]any, string, error) {
	return a.outgoing.fetchChatHistory(ctx, chatID, cursor, limit)
}

// FetchChatConversations 封装Fetch聊天Conversations业务协调。
func (a *Account) FetchChatConversations(ctx context.Context, cursor int64, limit int) (map[string]any, string, error) {
	return a.outgoing.fetchChatConversations(ctx, cursor, limit)
}

// AutomationReady 报告自动化消息是否可以立即使用当前 WS 连接发送。
func (a *Account) AutomationReady() bool {
	return a.outgoing.automationReady()
}

// rotatePageDeviceID 对应官网 auto-login 成功后的 location.reload()：
// 新 FishEngine 使用新的 UUID-userID，普通 Set-Cookie 与自然重连不会调用它。
// rotatePageDeviceID 封装rotate页码DeviceID业务协调。
func (a *Account) rotatePageDeviceID() {
	a.mu.Lock()
	// userID 用于本次流程后续判断的用户ID
	userID := a.UserID
	if userID == "" {
		userID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	a.deviceID = protocol.GenerateDeviceID(userID)
	a.mu.Unlock()
}
