package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logsafe"
)

// 商品回补的默认策略参数。
// 这些值同时决定「平台请求预算」和「状态内存占用」，改动前需评估平台风控与账号并发量。
const (
	// ItemBackfillFailureTTL 是回补失败后的负缓存时长：期间同一商品不再重试，默认 24 小时。
	ItemBackfillFailureTTL = 24 * time.Hour
	// ItemBackfillAccountWindow 是按账号限流的时间窗口长度，默认 5 分钟。
	ItemBackfillAccountWindow = 5 * time.Minute
	// ItemBackfillAccountLimit 是单账号在一个限流窗口内允许触发的回补次数，默认 3 次。
	ItemBackfillAccountLimit = 3
	// ItemBackfillTimeout 是单次回补的最长执行时间；它略长于防抖窗口，避免慢平台请求长期占用状态。
	ItemBackfillTimeout = 8 * time.Second
	// itemBackfillStateLimit 是状态表条目上限；超限时整体放弃旧状态而不是无限增长。
	itemBackfillStateLimit = 4096
	// itemBackfillSweepInterval 是惰性清理的最小间隔，避免每条消息都遍历状态表。
	itemBackfillSweepInterval = time.Minute
	// itemBackfillPreviewRunes 是决策日志消息预览的最大字符数，避免放大日志体积。
	itemBackfillPreviewRunes = 60
)

// replyItemBackfiller 是业务处理器可选提供的单商品回补能力。
// 引擎侧只决定「何时触发」，不关心平台调用与落库细节；缺失该能力时保持改动前的 fail-closed 行为。
type replyItemBackfiller interface {
	// BackfillItem 补齐 accountID 名下的 itemID 商品本地记录；实现必须幂等，且不得在内部吞掉归属核验。
	BackfillItem(ctx context.Context, accountID, itemID string) error
}

// replyAIDecisionRecorder 是业务处理器可选提供的 AI 决策留痕能力。
// 缺失时回补链路只写运行日志，不影响准入判断。
type replyAIDecisionRecorder interface {
	// AddDecisionLog 写入一条决策归因记录；实现须自行保证不返回凭证类信息。
	AddDecisionLog(ctx context.Context, rec db.AIDecisionRecord) error
}

// itemBackfillOutcome 表示一次回补执行权申请的结果。
type itemBackfillOutcome int

const (
	// itemBackfillStarted 表示本次申请拿到执行权，调用方可以启动回补任务。
	itemBackfillStarted itemBackfillOutcome = iota
	// itemBackfillInFlight 表示同一账号同一商品已有回补在跑，本次不重复触发。
	itemBackfillInFlight
	// itemBackfillCooling 表示该商品处于失败负缓存期，本次不触发。
	itemBackfillCooling
	// itemBackfillThrottled 表示账号在限流窗口内的触发次数已用尽，本次不触发。
	itemBackfillThrottled
	// itemBackfillUnavailable 表示协调器未装配（零值分发器），本次不触发。
	itemBackfillUnavailable
)

// String 返回申请结果的可读名称，仅供运行日志使用，不参与任何业务判断。
func (o itemBackfillOutcome) String() string {
	switch o {
	case itemBackfillStarted:
		return "started"
	case itemBackfillInFlight:
		return "in-flight"
	case itemBackfillCooling:
		return "failure-cached"
	case itemBackfillThrottled:
		return "rate-limited"
	default:
		return "unavailable"
	}
}

// itemBackfillConfig 描述回补协调器的可调参数；零值字段统一回落到包级默认常量。
type itemBackfillConfig struct {
	// Now 提供当前时间（本地单调时钟语义），测试可注入固定时钟以获得确定性断言。
	Now func() time.Time
	// FailureTTL 是失败负缓存时长；<=0 时使用 ItemBackfillFailureTTL。
	FailureTTL time.Duration
	// AccountWindow 是按账号限流窗口；<=0 时使用 ItemBackfillAccountWindow。
	AccountWindow time.Duration
	// AccountLimit 是窗口内允许的触发次数；<=0 时使用 ItemBackfillAccountLimit。
	AccountLimit int
	// Timeout 是单次回补超时；<=0 时使用 ItemBackfillTimeout。
	Timeout time.Duration
}

// itemBackfillCoordinator 维护单商品回补的去重、失败负缓存与按账号限流状态。
//
// 并发模型：
//   - 唯一的 mu 保护 inflight、failures、attempts、lastSweep 四个字段，不存在嵌套加锁，
//     因此不存在死锁顺序问题；任何新字段都必须挂在同一把锁下并在此注释中登记。
//   - 持锁期间只做内存读写，禁止执行数据库、平台请求或任何可能阻塞的 I/O；
//     网络调用由 runItemBackfill 在锁释放后执行。
//   - 同一 (账号, 商品) 通过 inflight 实现 single-flight：只有首个调用方拿到执行权，
//     其余调用方立即返回 itemBackfillInFlight，不排队等待，也不共享结果。
//
// 生命周期：随 messageDispatcher 构造创建，跟随账号运行时对象一起被回收；
// 不启动后台 goroutine，过期条目在申请触发时惰性清理，因此没有显式关停接口。
type itemBackfillCoordinator struct {
	// mu 保护本结构全部可变字段；持锁期间不得执行 I/O。
	mu sync.Mutex
	// inflight 保存进行中的回补：键为 itemBackfillKey(账号, 商品)，值为开始时间，用于清理卡死条目。
	inflight map[string]time.Time
	// failures 保存失败负缓存：键同上，值为解除冷却的时间点。
	failures map[string]time.Time
	// attempts 保存账号到限流窗口内触发时间的映射；切片按时间升序，长度不超过 accountLimit。
	attempts map[string][]time.Time
	// lastSweep 记录最近一次惰性清理时间，避免每条消息都做全表遍历。
	lastSweep time.Time
	// now 提供当前时间，测试可替换为固定时钟。
	now func() time.Time
	// failureTTL、accountWindow、accountLimit、timeout 是构造期归一后的生效参数。
	failureTTL    time.Duration
	accountWindow time.Duration
	accountLimit  int
	timeout       time.Duration
}

// newItemBackfillCoordinator 构造回补协调器，并把零值参数归一为包级默认常量。
func newItemBackfillCoordinator(config itemBackfillConfig) *itemBackfillCoordinator {
	// now 是归一后的时钟，缺省使用 time.Now。
	now := config.Now
	if now == nil {
		now = time.Now
	}
	// failureTTL、accountWindow、accountLimit、timeout 是归一后的策略参数。
	failureTTL := config.FailureTTL
	if failureTTL <= 0 {
		failureTTL = ItemBackfillFailureTTL
	}
	// accountWindow 是归一后的按账号限流窗口。
	accountWindow := config.AccountWindow
	if accountWindow <= 0 {
		accountWindow = ItemBackfillAccountWindow
	}
	// accountLimit 是归一后的窗口内触发次数上限。
	accountLimit := config.AccountLimit
	if accountLimit <= 0 {
		accountLimit = ItemBackfillAccountLimit
	}
	// timeout 是归一后的单次回补超时预算。
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = ItemBackfillTimeout
	}
	return &itemBackfillCoordinator{
		inflight:      make(map[string]time.Time),
		failures:      make(map[string]time.Time),
		attempts:      make(map[string][]time.Time),
		lastSweep:     now(),
		now:           now,
		failureTTL:    failureTTL,
		accountWindow: accountWindow,
		accountLimit:  accountLimit,
		timeout:       timeout,
	}
}

// itemBackfillKey 拼接账号与商品的一次性去重键；使用不可能出现在标识中的分隔符避免键冲突。
func itemBackfillKey(accountID, itemID string) string {
	return strings.TrimSpace(accountID) + "\x00" + strings.TrimSpace(itemID)
}

// timeoutDuration 返回单次回补的超时预算；nil 协调器返回默认常量，供防御性调用。
func (c *itemBackfillCoordinator) timeoutDuration() time.Duration {
	if c == nil || c.timeout <= 0 {
		return ItemBackfillTimeout
	}
	return c.timeout
}

// tryAcquire 申请一次回补执行权：先去重、再查失败负缓存、最后按账号限流。
// accountID、itemID 来自当前消息；返回 started 时调用方必须保证后续调用 release 或 abandon。
func (c *itemBackfillCoordinator) tryAcquire(accountID, itemID string) itemBackfillOutcome {
	if c == nil {
		return itemBackfillUnavailable
	}
	// key 是本次申请的去重键。
	key := itemBackfillKey(accountID, itemID)
	// now 是本次申请的时刻，同一申请内保持不变。
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(now)
	if // startedAt 表示是否已有相同商品的回补在跑。
	_, running := c.inflight[key]; running {
		return itemBackfillInFlight
	}
	if // until 表示失败负缓存的解除时间。
	until, cooling := c.failures[key]; cooling && now.Before(until) {
		return itemBackfillCooling
	}
	// window 是该账号在当前限流窗口内的触发时间序列，已剔除窗口外记录。
	window := c.pruneAttemptsLocked(accountID, now)
	if len(window) >= c.accountLimit {
		c.attempts[accountID] = window
		return itemBackfillThrottled
	}
	c.attempts[accountID] = append(window, now)
	c.inflight[key] = now
	return itemBackfillStarted
}

// release 释放执行权，并在失败时写入负缓存；同一键重复释放是安全的，不会放大状态。
func (c *itemBackfillCoordinator) release(accountID, itemID string, err error) {
	if c == nil {
		return
	}
	// key 是本次释放对应的去重键。
	key := itemBackfillKey(accountID, itemID)
	// now 是释放时刻，用于计算负缓存解禁时间。
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, key)
	if err != nil {
		c.failures[key] = now.Add(c.failureTTL)
	}
}

// abandon 释放执行权但不记录失败，用于账号已停止或回补发生 panic 的场景。
func (c *itemBackfillCoordinator) abandon(accountID, itemID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, itemBackfillKey(accountID, itemID))
}

// pruneAttemptsLocked 返回账号在当前窗口内的触发时间序列并删除过期记录；调用方必须已持有 mu。
func (c *itemBackfillCoordinator) pruneAttemptsLocked(accountID string, now time.Time) []time.Time {
	// window 是该账号已有的触发时间序列，按时间升序。
	window := c.attempts[accountID]
	// kept 保存仍落在窗口内的记录。
	kept := window[:0]
	for // at 表示当前遍历到的历史触发时间。
	_, at := range window {
		if now.Sub(at) <= c.accountWindow {
			kept = append(kept, at)
		}
	}
	return kept
}

// sweepLocked 惰性清理过期状态；调用方必须已持有 mu，且清理过程不执行任何 I/O。
// 触发条件：距上次清理超过 itemBackfillSweepInterval，或状态表条目数超过上限。
func (c *itemBackfillCoordinator) sweepLocked(now time.Time) {
	// total 是当前状态表条目总数，用于判断是否需要强制清理。
	total := len(c.inflight) + len(c.failures) + len(c.attempts)
	if total <= itemBackfillStateLimit && now.Sub(c.lastSweep) < itemBackfillSweepInterval {
		return
	}
	c.lastSweep = now
	// staleAfter 是判定「回补卡死」的时长：正常回补受 timeout 约束，超过三倍视为异常残留。
	staleAfter := 3 * c.timeoutDuration()
	for // key、startedAt 表示进行中的回补键及开始时间。
	key, startedAt := range c.inflight {
		if now.Sub(startedAt) > staleAfter {
			delete(c.inflight, key)
		}
	}
	for // key、until 表示失败负缓存的键及解禁时间。
	key, until := range c.failures {
		if !now.Before(until) {
			delete(c.failures, key)
		}
	}
	for // accountID 表示当前遍历到的账号标识。
	accountID := range c.attempts {
		// window 是剔除过期记录后的窗口序列；为空时删除该账号条目。
		window := c.pruneAttemptsLocked(accountID, now)
		if len(window) == 0 {
			delete(c.attempts, accountID)
			continue
		}
		c.attempts[accountID] = window
	}
	if len(c.attempts) > itemBackfillStateLimit {
		// 账号数量异常时整体丢弃限流记忆：只影响限流精度，不会放行错误回复。
		c.attempts = make(map[string][]time.Time)
	}
	if len(c.failures) > itemBackfillStateLimit {
		// 失败负缓存异常膨胀时整体丢弃，最坏结果是提前重试一次平台请求。
		c.failures = make(map[string]time.Time)
	}
}

// requestItemBackfill 在商品不在本地库时申请一次异步回补，并立即返回。
// 该方法只做状态登记与留痕：本条消息仍然判定为「不回复」，回补结果不参与本次准入判断。
func (d *messageDispatcher) requestItemBackfill(ctx context.Context, message ChatMessage) {
	// handler 是当前账号业务处理器；只有实现了可选回补能力才允许触发。
	handler := d.currentHandler()
	// backfiller、supported 保存可选回补端口与能力标识；缺失时保持改动前的 fail-closed 行为。
	backfiller, supported := handler.(replyItemBackfiller)
	if !supported || backfiller == nil || d.itemBackfill == nil {
		return
	}
	// outcome 是本次申请结果；非 started 表示已有进行中任务、处于失败冷却或被限流。
	outcome := d.itemBackfill.tryAcquire(message.AccountID, message.ItemID)
	if outcome != itemBackfillStarted {
		d.logger.Debug("商品回补未触发", "chat_id", message.ChatID, "item_id", message.ItemID, "reason", outcome.String())
		return
	}
	// recorder 是可选留痕端口；缺失时只保留运行日志。
	recorder, _ := handler.(replyAIDecisionRecorder)
	d.recordItemBackfillDecision(ctx, recorder, message, AIReasonItemSyncRequested)
	// 回补在独立 goroutine 中执行，避免阻塞本条消息的准入与后续消息处理。
	go d.runItemBackfill(backfiller, recorder, message)
}

// runItemBackfill 在独立 goroutine 中执行一次回补：接管生命周期、超时与 panic，并在结束后写回留痕。
// 任何失败都只影响下一次是否重试，不改变「本条不回复」的既定结论。
func (d *messageDispatcher) runItemBackfill(backfiller replyItemBackfiller, recorder replyAIDecisionRecorder, message ChatMessage) {
	// taskCtx、finish、accepted 是账号生命周期为该回补任务发放的上下文与释放函数。
	taskCtx, finish, accepted := d.beginTask()
	if !accepted {
		// 账号已停止或拒绝新任务：放弃回补并清除占位，不写失败负缓存。
		d.itemBackfill.abandon(message.AccountID, message.ItemID)
		return
	}
	defer finish()
	defer func() {
		// recovered 保存回补过程中的 panic；必须在此吞掉，绝不允许冒泡到账号运行时。
		if recovered := recover(); recovered != nil {
			d.logger.Error("商品回补发生 panic，已忽略", "chat_id", message.ChatID, "item_id", message.ItemID, "panic", recovered)
			d.itemBackfill.abandon(message.AccountID, message.ItemID)
		}
	}()
	// backfillCtx、cancel 给单次回补加超时上界，避免慢平台请求长期占用去重状态。
	backfillCtx, cancel := context.WithTimeout(taskCtx, d.itemBackfill.timeoutDuration())
	defer cancel()
	// err 是回补结果：成功表示商品已写入本地库，该会话下一次消息可直接复判。
	err := backfiller.BackfillItem(backfillCtx, message.AccountID, message.ItemID)
	d.itemBackfill.release(message.AccountID, message.ItemID, err)
	if err != nil {
		d.logger.Warn("商品回补失败，进入失败负缓存", "chat_id", message.ChatID, "item_id", message.ItemID, "err", err)
		d.recordItemBackfillDecision(taskCtx, recorder, message, AIReasonItemSyncFailed)
		return
	}
	d.logger.Info("商品回补成功，已写入本地商品库", "chat_id", message.ChatID, "item_id", message.ItemID)
}

// recordItemBackfillDecision 写一条商品回补的决策归因记录；留痕失败只记 Debug，不影响任何链路。
func (d *messageDispatcher) recordItemBackfillDecision(ctx context.Context, recorder replyAIDecisionRecorder, message ChatMessage, reason string) {
	if recorder == nil || ctx == nil {
		return
	}
	// rec 是写入 ai_decision_log 的归因记录；回补事件不属于 AI 接管，takeover 固定为 false。
	rec := db.AIDecisionRecord{
		CookieID:       message.AccountID,
		ChatID:         message.ChatID,
		ItemID:         message.ItemID,
		MessageHash:    logsafe.ID(message.Text),
		MessagePreview: itemBackfillPreview(message.Text),
		Takeover:       false,
		Reason:         reason,
	}
	if // err 是归因写入失败原因。
	err := recorder.AddDecisionLog(ctx, rec); err != nil {
		d.logger.Debug("写入商品回补决策日志失败", "chat_id", message.ChatID, "item_id", message.ItemID, "err", err)
	}
}

// itemBackfillPreview 截断买家消息作为日志预览，仅供人工定位，不承担审计作用。
func itemBackfillPreview(text string) string {
	// runes 按字符切分，避免中文被按字节截断成乱码。
	runes := []rune(text)
	if len(runes) > itemBackfillPreviewRunes {
		return string(runes[:itemBackfillPreviewRunes]) + "…"
	}
	return text
}
