package adapter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdapterReconnectOrderSyncThrottling 验证重连补同步按最小间隔节流，且首次连接不重复同步。
func TestAdapterReconnectOrderSyncThrottling(t *testing.T) {
	// store、cleanup 保存测试适配器依赖及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存待触发传输就绪事件的适配器。
	adapter := New(store, nil, nil)
	// calls 统计订单同步回调次数。
	calls := &atomic.Int32{}
	adapter.initialOrderSync = func(context.Context, string) error {
		calls.Add(1)
		return nil
	}
	// ctx 是本测试回调调用共用的上下文。
	ctx := context.Background()
	// 从未同步过的账号直接跳过重连补同步，避免与首次连接同步重复。
	adapter.OnReconnectTransportReady(ctx, "cid")
	// skippedCalls 是首次重连通知后的同步次数；必须为零。
	skippedCalls := calls.Load()
	if skippedCalls != 0 {
		t.Fatalf("首次重连通知不应同步订单，次数=%d", skippedCalls)
	}
	// 首次连接完成同步并记录时间。
	adapter.OnInitialTransportReady(ctx, "cid")
	// initialCalls 是首次连接同步后的次数；必须恰好一次。
	initialCalls := calls.Load()
	if initialCalls != 1 {
		t.Fatalf("首次连接同步次数=%d want 1", initialCalls)
	}
	// 紧随其后的重连通知在最小间隔内必须被节流。
	adapter.OnReconnectTransportReady(ctx, "cid")
	// throttledCalls 是节流后的同步次数；必须保持不变。
	throttledCalls := calls.Load()
	if throttledCalls != 1 {
		t.Fatalf("最小间隔内的重连补同步未被节流，次数=%d", throttledCalls)
	}
	// 把最近同步时间回拨到间隔之外，模拟一次较长的断线恢复。
	adapter.orderSyncMu.Lock()
	adapter.orderSyncAt["cid"] = time.Now().Add(-transportOrderSyncMinInterval - time.Second)
	adapter.orderSyncMu.Unlock()
	adapter.OnReconnectTransportReady(ctx, "cid")
	// recoveredCalls 是间隔之外重连补同步后的次数；必须新增一次。
	recoveredCalls := calls.Load()
	if recoveredCalls != 2 {
		t.Fatalf("间隔之外的重连补同步未执行，次数=%d want 2", recoveredCalls)
	}
}

// TestAdapterReconnectOrderSyncIsSerialized 验证并发重连通知只产生一次补同步。
func TestAdapterReconnectOrderSyncIsSerialized(t *testing.T) {
	// store、cleanup 保存测试适配器依赖及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存待触发传输就绪事件的适配器。
	adapter := New(store, nil, nil)
	// calls 统计订单同步回调次数。
	calls := &atomic.Int32{}
	adapter.initialOrderSync = func(context.Context, string) error {
		calls.Add(1)
		return nil
	}
	// 预置一个已经超出间隔的同步时间，使全部并发通知都满足时间条件。
	adapter.orderSyncMu.Lock()
	adapter.orderSyncAt = map[string]time.Time{
		"cid": time.Now().Add(-transportOrderSyncMinInterval - time.Second),
	}
	adapter.orderSyncMu.Unlock()
	// callers 是并发重连通知数量，模拟网络抖动引发的集中重连。
	callers := 16
	// wg 等待全部并发通知完成。
	var wg sync.WaitGroup
	// start 是并发起跑信号。
	start := make(chan struct{})
	// index 是并发通知的下标。
	for index := 0; index < callers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			adapter.OnReconnectTransportReady(context.Background(), "cid")
		}()
	}
	close(start)
	wg.Wait()
	// serializedCalls 是并发重连补同步的最终次数；必须只有一次。
	serializedCalls := calls.Load()
	if serializedCalls != 1 {
		t.Fatalf("并发重连补同步次数=%d want 1", serializedCalls)
	}
}

// TestAdapterReconnectOrderSyncWithoutCallback 验证未装配订单同步回调时重连通知无副作用。
func TestAdapterReconnectOrderSyncWithoutCallback(t *testing.T) {
	// store、cleanup 保存测试适配器依赖及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存未注入订单同步回调的适配器。
	adapter := New(store, nil, nil)
	adapter.initialOrderSync = nil
	// 缺少回调时必须安全返回，不得 panic。
	adapter.OnReconnectTransportReady(context.Background(), "cid")
	adapter.OnInitialTransportReady(context.Background(), "cid")
}

// TestAdapterReconnectOrderSyncRecordsFailure 验证补同步失败只记录诊断，不影响连接本身。
func TestAdapterReconnectOrderSyncRecordsFailure(t *testing.T) {
	// store、cleanup 保存测试适配器依赖及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存待触发传输就绪事件的适配器。
	adapter := New(store, nil, nil)
	// calls 统计订单同步回调次数。
	calls := &atomic.Int32{}
	adapter.initialOrderSync = func(context.Context, string) error {
		calls.Add(1)
		return errors.New("同步失败")
	}
	// ctx 是本测试回调调用共用的上下文。
	ctx := context.Background()
	// 首次连接同步失败只记录诊断，调用本身不返回错误。
	adapter.OnInitialTransportReady(ctx, "cid")
	// 把最近同步时间回拨到间隔之外，触发一次会失败的补同步。
	adapter.orderSyncMu.Lock()
	adapter.orderSyncAt["cid"] = time.Now().Add(-transportOrderSyncMinInterval - time.Second)
	adapter.orderSyncMu.Unlock()
	adapter.OnReconnectTransportReady(ctx, "cid")
	// failureCalls 是两次失败同步的累计次数；必须都被执行而不是被静默跳过。
	failureCalls := calls.Load()
	if failureCalls != 2 {
		t.Fatalf("同步失败路径次数=%d want 2", failureCalls)
	}
}