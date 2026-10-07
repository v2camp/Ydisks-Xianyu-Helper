package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

// reconnectTransportReadyCoverageHandler 覆盖每次传输就绪后的重连补同步回调。
type reconnectTransportReadyCoverageHandler struct {
	// Handler 嵌入基础处理器接口，避免本测试重复实现无关回调。
	Handler
	// mu 保护 calls，多个并发回调可能同时写入。
	mu sync.Mutex
	// calls 记录收到的重连补同步通知次数。
	calls int
	// contexts 保存每次回调收到的任务上下文，供测试确认生命周期拥有关系。
	contexts []context.Context
}

// OnReconnectTransportReady 记录重连补同步通知及其任务上下文。
func (h *reconnectTransportReadyCoverageHandler) OnReconnectTransportReady(ctx context.Context, _ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	h.contexts = append(h.contexts, ctx)
}

// callCount 返回已收到的重连补同步通知次数。
func (h *reconnectTransportReadyCoverageHandler) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

// waitForCalls 等待回调次数达到 want，超时即失败。
func (h *reconnectTransportReadyCoverageHandler) waitForCalls(t *testing.T, want int) {
	t.Helper()
	// deadline 是本轮等待的上限时间。
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.callCount() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("重连补同步通知次数=%d want %d", h.callCount(), want)
}

// TestReconnectTransportReadyNotifiesEveryRegistration 验证重连补同步每次注册都会通知，且受账号生命周期管理。
func TestReconnectTransportReadyNotifiesEveryRegistration(t *testing.T) {
	// handler 保存接收重连补同步通知的可选回调。
	handler := &reconnectTransportReadyCoverageHandler{}
	// account 保存测试单账号运行实例。
	account := New(Config{CookieID: "reconnect-ready", Handler: handler})
	// runCtx、cancel 是账号运行时生命周期上下文和其关闭函数。
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account.lifecycle.start(runCtx, cancel)
	// 连续三次注册都必须通知：断线期间的订单事件只能在这些时机补同步。
	account.notifyReconnectTransportReady()
	account.notifyReconnectTransportReady()
	account.notifyReconnectTransportReady()
	handler.waitForCalls(t, 3)
	// ctx 是本次校验使用的上下文快照；每次回调都必须继承有效的账号生命周期上下文。
	for index, ctx := range handler.contexts {
		if ctx == nil || ctx.Err() != nil {
			t.Fatalf("第 %d 次重连补同步任务没有收到有效账号生命周期上下文", index)
		}
	}
	account.Stop()
}

// TestReconnectTransportReadySkipsUnsupportedHandler 验证处理器未实现重连补同步端口时安全跳过。
func TestReconnectTransportReadySkipsUnsupportedHandler(t *testing.T) {
	// handler 只实现基础传输就绪回调，不实现重连补同步端口。
	handler := &transportReadyCoverageHandler{}
	// account 保存测试单账号运行实例。
	account := New(Config{CookieID: "reconnect-unsupported", Handler: handler})
	// runCtx、cancel 是账号运行时生命周期上下文和其关闭函数。
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account.lifecycle.start(runCtx, cancel)
	// 未实现的端口必须安全跳过，不得 panic 或阻塞。
	account.notifyReconnectTransportReady()
	if handler.calls != 0 {
		t.Fatalf("基础传输就绪回调被重连通知误触发 %d 次", handler.calls)
	}
	account.Stop()
}

// TestReconnectTransportReadySkipsAfterLifecycleClosed 验证账号生命周期已关闭时不再派发补同步任务。
func TestReconnectTransportReadySkipsAfterLifecycleClosed(t *testing.T) {
	// handler 保存接收重连补同步通知的可选回调。
	handler := &reconnectTransportReadyCoverageHandler{}
	// account 保存测试单账号运行实例。
	account := New(Config{CookieID: "reconnect-closed", Handler: handler})
	// runCtx、cancel 是账号运行时生命周期上下文和其关闭函数。
	runCtx, cancel := context.WithCancel(context.Background())
	account.lifecycle.start(runCtx, cancel)
	account.Stop()
	// 生命周期关闭后不得再登记新任务，避免账号停止后仍发起平台同步。
	account.notifyReconnectTransportReady()
	if handler.callCount() != 0 {
		t.Fatalf("生命周期关闭后仍派发 %d 次重连补同步", handler.callCount())
	}
}

// TestReconnectTransportReadyHandlesNilHandler 验证处理器为空时安全跳过。
func TestReconnectTransportReadyHandlesNilHandler(t *testing.T) {
	// account 保存未注入事件处理器的单账号运行实例。
	account := New(Config{CookieID: "reconnect-nil-handler"})
	// runCtx、cancel 是账号运行时生命周期上下文和其关闭函数。
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account.lifecycle.start(runCtx, cancel)
	// 空闲处理器必须安全跳过，不得 panic。
	account.notifyReconnectTransportReady()
	account.Stop()
}