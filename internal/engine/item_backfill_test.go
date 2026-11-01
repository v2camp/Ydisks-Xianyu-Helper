package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// backfillHandlerFixture 在本地商品归属夹具上扩展可选的单商品回补与决策留痕能力。
// 所有可变字段都由 mu 保护：回补在独立 goroutine 中执行，测试主协程会并发读取计数。
type backfillHandlerFixture struct {
	*localOwnershipHandlerFixture
	// mu 保护 owned、calls、records、gate、backfillErr、becomeOwned 与 panicOnce 字段。
	mu sync.Mutex
	// owned 是本地商品归属结论；成功回补后会按 becomeOwned 置为 true，模拟商品写入本地库。
	owned bool
	// becomeOwned 表示回补成功后是否把商品置为本地存在。
	becomeOwned bool
	// calls 记录 BackfillItem 被调用的次数，用于验证去重与限流。
	calls int
	// backfillErr 是回补按场景返回的错误。
	backfillErr error
	// panicOnce 表示下一次回补直接 panic，用于验证 panic 不会冒泡到账号运行时。
	panicOnce bool
	// gate 非空时回补会阻塞到该通道关闭或 ctx 结束，用于构造并发窗口与超时场景。
	gate chan struct{}
	// finished 在每次回补结束后收到一条通知，建立测试主协程与回补 goroutine 的同步关系。
	finished chan struct{}
	// records 保存收到的决策归因记录，供断言 item-sync-requested / item-sync-failed 留痕。
	records []db.AIDecisionRecord
}

// newBackfillHandlerFixture 构造带回补能力的处理器夹具，finished 使用带缓冲通道避免回补 goroutine 阻塞。
func newBackfillHandlerFixture(owned bool) *backfillHandlerFixture {
	return &backfillHandlerFixture{
		localOwnershipHandlerFixture: &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: owned},
		owned:                        owned,
		finished:                     make(chan struct{}, 16),
	}
}

// ItemBelongsToAccount 返回受锁保护的本地商品归属结论，覆盖内嵌夹具的无锁读取。
func (fixture *backfillHandlerFixture) ItemBelongsToAccount(_ context.Context, _, _ string) (bool, error) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.owned, nil
}

// setOwned 在锁内更新本地商品归属结论，模拟回补成功后的商品落库效果。
func (fixture *backfillHandlerFixture) setOwned(owned bool) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.owned = owned
}

// isOwned 在锁内读取本地商品归属结论，供测试轮询等待回补落库。
func (fixture *backfillHandlerFixture) isOwned() bool {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.owned
}

// callCount 在锁内读取回补调用次数。
func (fixture *backfillHandlerFixture) callCount() int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.calls
}

// reasons 在锁内复制已收到的决策原因简码，避免断言期间与回补 goroutine 竞争。
func (fixture *backfillHandlerFixture) reasons() []string {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	// out 是本次复制出的原因简码列表。
	out := make([]string, 0, len(fixture.records))
	for // index 表示当前遍历到的归因记录下标。
	index := range fixture.records {
		out = append(out, fixture.records[index].Reason)
	}
	return out
}

// recordsCopy 在锁内复制归因记录，供断言 item_id 与 cookie_id 是否被正确带上。
func (fixture *backfillHandlerFixture) recordsCopy() []db.AIDecisionRecord {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	// out 是归因记录的副本，供调用方在锁外断言。
	out := make([]db.AIDecisionRecord, len(fixture.records))
	copy(out, fixture.records)
	return out
}

// BackfillItem 模拟一次单商品回补：先记录调用次数，再按 gate 阻塞，最后返回错误或 panic。
func (fixture *backfillHandlerFixture) BackfillItem(ctx context.Context, _, itemID string) error {
	// done 保证即使发生 panic 也会向测试主协程发出完成信号。
	done := func() {
		if fixture.finished != nil {
			fixture.finished <- struct{}{}
		}
	}
	fixture.mu.Lock()
	fixture.calls++
	// gate 是本次回补的并发窗口阻塞信号，nil 表示不阻塞。
	gate := fixture.gate
	// err 是本次回补要返回的错误，nil 表示成功。
	err := fixture.backfillErr
	// become 表示成功时是否把商品置为本地存在。
	become := fixture.becomeOwned
	// shouldPanic 表示本次回补是否故意 panic。
	shouldPanic := fixture.panicOnce
	fixture.panicOnce = false
	fixture.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			done()
			return ctx.Err()
		}
	}
	if err == nil && become {
		fixture.setOwned(true)
	}
	done()
	if shouldPanic {
		panic("模拟回补 panic: " + itemID)
	}
	return err
}

// setGate 在锁内设置阻塞信号，供并发用例打开并发窗口。
func (fixture *backfillHandlerFixture) setGate(gate chan struct{}) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.gate = gate
}

// AddDecisionLog 收集回补链路写入的决策归因记录，不触碰真实数据库。
func (fixture *backfillHandlerFixture) AddDecisionLog(_ context.Context, rec db.AIDecisionRecord) error {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.records = append(fixture.records, rec)
	return nil
}

// waitFinished 等待一次回补结束或超时，建立异步断言的前置条件。
func (fixture *backfillHandlerFixture) waitFinished(t *testing.T) {
	t.Helper()
	select {
	case <-fixture.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("回补未在预期时间内结束")
	}
}

// newBackfillTestDispatcher 构造只注入本地归属与回补能力的消息分发器，不启动任何网络连接。
func newBackfillTestDispatcher(handler Handler) *messageDispatcher {
	// dispatcher 是待测分发器；本地身份固定为 self，避免依赖真实凭证。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CookieID:       "cid",
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return handler },
	})
	return &dispatcher
}

// itemBackfillTestMessage 构造绑定固定账号、会话和商品的入站买家消息。
func itemBackfillTestMessage(itemID string) ChatMessage {
	return ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: itemID, SenderUserID: "buyer", Text: "这个还有货吗"}
}

// TestItemBackfillRequestedOnMissingItem 验证商品不在本地库时触发一次回补，本条仍不回复且留下 item-sync-requested 归因。
func TestItemBackfillRequestedOnMissingItem(t *testing.T) {
	// fixture 提供本地商品缺失事实，并实现可选回补与留痕能力。
	fixture := newBackfillHandlerFixture(false)
	// dispatcher 只装配当前账号与处理器，不启动网络连接。
	dispatcher := newBackfillTestDispatcher(fixture)
	// allowed 是商品缺失时的自动回复结论，必须为 false。
	allowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-missing"))
	if allowed {
		t.Fatal("商品缺失时不得允许自动回复")
	}
	fixture.waitFinished(t)
	waitForCondition(t, func() bool { return fixture.callCount() == 1 })
	if fixture.localOwnershipHandlerFixture.role.RoleSource != "platform_unresolved" {
		t.Fatalf("商品缺失未固定待同步标记: role=%+v", fixture.localOwnershipHandlerFixture.role)
	}
	// records 是回补链路写入的归因记录。
	records := fixture.recordsCopy()
	if len(records) != 1 || records[0].Reason != AIReasonItemSyncRequested {
		t.Fatalf("回补请求未正确留痕: %+v", records)
	}
	if records[0].ItemID != "item-missing" || records[0].CookieID != "cid" || records[0].Takeover {
		t.Fatalf("归因记录字段异常: %+v", records[0])
	}
}

// TestItemBackfillConcurrentTriggersOnce 验证同一账号同一商品的 10 次并发请求只触发一次回补。
func TestItemBackfillConcurrentTriggersOnce(t *testing.T) {
	// fixture 阻塞回补以保持并发窗口打开。
	fixture := newBackfillHandlerFixture(false)
	// gate 是并发窗口的释放信号。
	gate := make(chan struct{})
	fixture.setGate(gate)
	// dispatcher 装配回补能力，用于并发准入判断。
	dispatcher := newBackfillTestDispatcher(fixture)
	// wg 等待 10 个并发准入判断全部返回。
	var wg sync.WaitGroup
	for // index 是并发请求序号。
	index := 0; index < 10; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-race")) {
				t.Error("商品缺失时不得允许自动回复")
			}
		}()
	}
	wg.Wait()
	// 并发窗口内只允许一个回补进入，因此调用次数应为 1（回补仍被 gate 阻塞）。
	waitForCondition(t, func() bool { return fixture.callCount() == 1 })
	close(gate)
	fixture.waitFinished(t)
	if fixture.callCount() != 1 {
		t.Fatalf("并发回补应只触发一次: calls=%d", fixture.callCount())
	}
}

// TestItemBackfillFailureUsesNegativeCache 验证回补失败后进入 24 小时负缓存，期间不再触发。
func TestItemBackfillFailureUsesNegativeCache(t *testing.T) {
	// clock 是可控时钟，用于推进到负缓存边界。
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	// coordinator 使用可控时钟与默认策略参数。
	coordinator := newItemBackfillCoordinator(itemBackfillConfig{Now: clock.Now})
	// 说明：以下断言全部通过状态机验证负缓存边界，不启动任何异步任务。
	if // outcome 是首次申请结果。
	outcome := coordinator.tryAcquire("cid", "item-1"); outcome != itemBackfillStarted {
		t.Fatalf("首次申请应获得执行权: %s", outcome)
	}
	coordinator.release("cid", "item-1", errors.New("平台请求失败"))
	clock.Advance(time.Hour)
	if // outcome 是负缓存期内的申请结果。
	outcome := coordinator.tryAcquire("cid", "item-1"); outcome != itemBackfillCooling {
		t.Fatalf("负缓存期内不应重试: %s", outcome)
	}
	clock.Advance(ItemBackfillFailureTTL)
	if // outcome 是负缓存过期后的申请结果。
	outcome := coordinator.tryAcquire("cid", "item-1"); outcome != itemBackfillStarted {
		t.Fatalf("负缓存过期后应允许重试: %s", outcome)
	}
}

// TestItemBackfillAccountRateLimit 验证单账号在限流窗口内只允许触发默认次数，窗口滑过后恢复。
func TestItemBackfillAccountRateLimit(t *testing.T) {
	// clock 是可控时钟，用于滑动限流窗口。
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	// coordinator 使用可控时钟验证限流窗口滑动。
	coordinator := newItemBackfillCoordinator(itemBackfillConfig{Now: clock.Now})
	for // index 表示窗口内第几次触发。
	index := 0; index < ItemBackfillAccountLimit; index++ {
		if // outcome 是当前次申请结果。
		outcome := coordinator.tryAcquire("cid", "item-"+string(rune('a'+index))); outcome != itemBackfillStarted {
			t.Fatalf("窗口内第 %d 次应允许触发: %s", index+1, outcome)
		}
	}
	if // outcome 是超出阈值后的申请结果。
	outcome := coordinator.tryAcquire("cid", "item-over"); outcome != itemBackfillThrottled {
		t.Fatalf("超过阈值应被限流: %s", outcome)
	}
	if // outcome 是其他账号的申请结果，用于验证限流按账号隔离。
	outcome := coordinator.tryAcquire("cid-other", "item-over"); outcome != itemBackfillStarted {
		t.Fatalf("限流不应影响其他账号: %s", outcome)
	}
	clock.Advance(ItemBackfillAccountWindow + time.Second)
	if // outcome 是窗口滑过后的申请结果。
	outcome := coordinator.tryAcquire("cid", "item-after-window"); outcome != itemBackfillStarted {
		t.Fatalf("窗口滑过后应恢复触发: %s", outcome)
	}
}

// TestItemBackfillRecoveryAfterSuccess 验证回补成功后该会话下一次消息通过本地商品门禁并写入卖家角色。
func TestItemBackfillRecoveryAfterSuccess(t *testing.T) {
	// fixture 首次判定商品缺失，回补成功后模拟商品进入本地库。
	fixture := newBackfillHandlerFixture(false)
	fixture.mu.Lock()
	fixture.becomeOwned = true
	fixture.mu.Unlock()
	// dispatcher 装配回补成功后会更新归属的处理器。
	dispatcher := newBackfillTestDispatcher(fixture)
	// firstAllowed 是商品缺失时的结论，必须为 false。
	if firstAllowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-late")); firstAllowed {
		t.Fatal("回补完成前不得允许自动回复")
	}
	fixture.waitFinished(t)
	waitForCondition(t, fixture.isOwned)
	// secondAllowed 是回补成功后的复判结果，此时商品已在本地库。
	if secondAllowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-late")); !secondAllowed {
		t.Fatal("回补成功后应恢复卖家自动回复")
	}
	if fixture.localOwnershipHandlerFixture.role.AccountRole != "seller" || fixture.localOwnershipHandlerFixture.role.RoleSource != "local_item" {
		t.Fatalf("回补成功后未写入卖家角色: %+v", fixture.localOwnershipHandlerFixture.role)
	}
	if fixture.callCount() != 1 {
		t.Fatalf("回补成功后又重复触发: calls=%d", fixture.callCount())
	}
}

// TestItemBackfillFailureKeepsFailClosed 验证回补失败时仍然不回、不 panic，并写下 item-sync-failed 归因。
func TestItemBackfillFailureKeepsFailClosed(t *testing.T) {
	// fixture 的回补固定返回平台错误。
	fixture := newBackfillHandlerFixture(false)
	fixture.mu.Lock()
	fixture.backfillErr = errors.New("平台详情接口失败")
	fixture.mu.Unlock()
	// dispatcher 装配固定失败的回补能力。
	dispatcher := newBackfillTestDispatcher(fixture)
	// firstAllowed 是回补失败前的准入结论。
	if firstAllowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-fail")); firstAllowed {
		t.Fatal("商品缺失时不得允许自动回复")
	}
	fixture.waitFinished(t)
	waitForCondition(t, func() bool { return len(fixture.reasons()) == 2 })
	// reasons 是本次链路写入的原因序列：请求 + 失败。
	reasons := fixture.reasons()
	if reasons[0] != AIReasonItemSyncRequested || reasons[1] != AIReasonItemSyncFailed {
		t.Fatalf("回补失败归因顺序异常: %v", reasons)
	}
	// secondAllowed 是失败负缓存期内的再次准入结论，必须仍为 false。
	if secondAllowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-fail")); secondAllowed {
		t.Fatal("回补失败后仍不得允许自动回复")
	}
	if fixture.callCount() != 1 {
		t.Fatalf("失败负缓存期内不应重复请求平台: calls=%d", fixture.callCount())
	}
}

// TestItemBackfillTimeoutKeepsFailClosed 验证回补超时按失败处理，仍然不回且不阻塞准入判断。
func TestItemBackfillTimeoutKeepsFailClosed(t *testing.T) {
	// fixture 阻塞回补直到超时。
	fixture := newBackfillHandlerFixture(false)
	// gate 永不在测试期间关闭，回补只能被超时取消。
	fixture.setGate(make(chan struct{}))
	// dispatcher 装配超时预算被压短的协调器。
	dispatcher := newBackfillTestDispatcher(fixture)
	// 超时预算压到毫秒级，避免测试等待。
	dispatcher.itemBackfill = newItemBackfillCoordinator(itemBackfillConfig{Timeout: 20 * time.Millisecond})
	if // allowed 是回补超时前的准入结论，必须为 false。
	allowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-slow")); allowed {
		t.Fatal("回补超时不应放行自动回复")
	}
	fixture.waitFinished(t)
	waitForCondition(t, func() bool { return len(fixture.reasons()) == 2 })
	if // reasons 是超时链路的归因序列。
	reasons := fixture.reasons(); reasons[len(reasons)-1] != AIReasonItemSyncFailed {
		t.Fatalf("超时应记录失败归因: %v", reasons)
	}
	// 超时进入负缓存：再次准入不得重复请求平台。
	if // allowed 是负缓存期内的准入结论，必须为 false。
	allowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-slow")); allowed {
		t.Fatal("超时后仍不得允许自动回复")
	}
	if fixture.callCount() != 1 {
		t.Fatalf("超时后不应立即重试: calls=%d", fixture.callCount())
	}
}

// TestItemBackfillPanicDoesNotEscape 验证回补 panic 被吞掉，准入结论不变且占位被释放。
func TestItemBackfillPanicDoesNotEscape(t *testing.T) {
	// fixture 的下一次回补直接 panic。
	fixture := newBackfillHandlerFixture(false)
	fixture.mu.Lock()
	fixture.panicOnce = true
	fixture.mu.Unlock()
	// dispatcher 装配会 panic 的回补能力。
	dispatcher := newBackfillTestDispatcher(fixture)
	// allowed 是回补 panic 时的准入结论，必须为 false。
	allowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-panic"))
	if allowed {
		t.Fatal("回补 panic 时不得允许自动回复")
	}
	fixture.waitFinished(t)
	// 占位必须被释放：panic 后再次请求应能重新触发。
	waitForCondition(t, func() bool {
		dispatcher.itemBackfill.mu.Lock()
		defer dispatcher.itemBackfill.mu.Unlock()
		return len(dispatcher.itemBackfill.inflight) == 0
	})
	if // allowed 是 panic 后的准入结论，必须为 false。
	allowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-panic")); allowed {
		t.Fatal("回补 panic 后仍不得允许自动回复")
	}
	fixture.waitFinished(t)
	if fixture.callCount() != 2 {
		t.Fatalf("panic 后占位未释放: calls=%d", fixture.callCount())
	}
}

// TestItemBackfillUnsupportedHandlerKeepsLegacyBehavior 验证处理器缺少可选回补能力时行为与改动前一致。
func TestItemBackfillUnsupportedHandlerKeepsLegacyBehavior(t *testing.T) {
	// observer 只有本地归属能力，没有回补与留痕能力。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: false}
	// dispatcher 装配不支持回补能力的处理器。
	dispatcher := newBackfillTestDispatcher(observer)
	// allowed 是缺少回补能力时的准入结论，必须为 false。
	allowed := dispatcher.canAutoReply(context.Background(), itemBackfillTestMessage("item-legacy"))
	if allowed {
		t.Fatal("缺少回补能力时仍应拒绝自动回复")
	}
	if observer.roleWrites != 1 || observer.role.RoleSource != "platform_unresolved" {
		t.Fatalf("缺少回补能力时行为发生变化: writes=%d role=%+v", observer.roleWrites, observer.role)
	}
}

// fakeClock 是测试用可控时钟，Now 与 Advance 均不加锁：仅在单协程用例中使用。
type fakeClock struct {
	// now 是当前虚拟时间。
	now time.Time
}

// Now 返回当前虚拟时间，满足 itemBackfillConfig.Now 的签名。
func (clock *fakeClock) Now() time.Time { return clock.now }

// Advance 向前推进虚拟时间，用于跨越限流窗口与负缓存边界。
func (clock *fakeClock) Advance(delta time.Duration) { clock.now = clock.now.Add(delta) }

// waitForCondition 在 3 秒内轮询条件，用于等待异步回补产生的副作用。
func waitForCondition(t *testing.T, condition func() bool) {
	t.Helper()
	// deadline 是轮询截止时间，避免异步缺陷导致测试挂死。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待异步条件超时")
}
