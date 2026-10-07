package adapter

import (
	"context"
	"time"
)

// transportOrderSyncMinInterval 是同一账号两次传输就绪订单同步之间的最小间隔。
// 网络抖动会连续重连；该间隔保证每次故障恢复补同步一次订单，同时不会把重连放大成平台调用风暴。
const transportOrderSyncMinInterval = 3 * time.Minute

// OnReconnectTransportReady 在每次 WebSocket 注册成功后按最小间隔补同步一次订单快照。
// 断线期间平台推送的付款系统消息可能永久丢失，本地订单事实只能靠重连后的列表同步补齐；
// 没有这次补同步，待发货兜底扫描与 SLA 看门狗都会因为缺少本地订单而无从下手。
// 首次连接由 OnInitialTransportReady 负责，因此本方法在从未同步过的账号上直接跳过。
func (a *Adapter) OnReconnectTransportReady(ctx context.Context, cookieID string) {
	// syncOrders 是构造期固定的订单同步回调；未装配时不产生副作用。
	syncOrders := a.initialOrderSync
	if syncOrders == nil {
		return
	}
	if !a.claimOrderSync(cookieID) {
		return
	}
	// syncErr 保存重连补同步的失败原因；失败只记录脱敏诊断，不影响连接本身。
	if syncErr := syncOrders(ctx, cookieID); syncErr != nil {
		a.logger.Warn("重连后的订单补同步失败", "cookie_id", cookieID, "err", syncErr)
	}
}

// markOrderSynced 记录账号最近一次订单同步的发起时间，不做节流判断。
func (a *Adapter) markOrderSynced(cookieID string) {
	a.orderSyncMu.Lock()
	if a.orderSyncAt == nil {
		a.orderSyncAt = make(map[string]time.Time)
	}
	a.orderSyncAt[cookieID] = time.Now()
	a.orderSyncMu.Unlock()
}

// claimOrderSync 判断该账号是否允许进行重连补同步，并记录本次发起时间。
// 仅当此前已经同步过且间隔未满时返回 false，避免首次连接与首次通知重复同步。
func (a *Adapter) claimOrderSync(cookieID string) bool {
	a.orderSyncMu.Lock()
	defer a.orderSyncMu.Unlock()
	if a.orderSyncAt == nil {
		a.orderSyncAt = make(map[string]time.Time)
	}
	// last、seen 保存该账号最近一次同步发起时间及是否存在。
	last, seen := a.orderSyncAt[cookieID]
	if !seen {
		return false
	}
	// now 是本次判定使用的当前时间。
	now := time.Now()
	if now.Sub(last) < transportOrderSyncMinInterval {
		return false
	}
	a.orderSyncAt[cookieID] = now
	return true
}
