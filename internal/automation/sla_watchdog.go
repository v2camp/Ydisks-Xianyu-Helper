package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
)

// deliverySLASettingKey 是发货 SLA 配置在系统设置中的键名。
// 值为 JSON：{"default_minutes":N,"per_account":{"<cookie_id>":M}}；空串表示清空并整体关闭。
const deliverySLASettingKey = "delivery_sla_config"

// deliverySLAWarningEventType 是发货 SLA 临期提醒的通知事件类型编码。
// 与 notify 订阅分类对齐，automation 包不反向依赖 notify。
const deliverySLAWarningEventType = "delivery_sla_warning"

// deliverySLAOverdueEventType 是发货 SLA 超时提醒的通知事件类型编码。
// 与临期提醒分开，便于渠道按阶段分别订阅。
const deliverySLAOverdueEventType = "delivery_sla_overdue"

// deliverySLANotifyLevel 是发货 SLA 提醒的通知级别。
// 临期与超时共用 info 级别，以事件类型区分阶段，符合任务契约的「同理」结构。
const deliverySLANotifyLevel = "info"

// defaultDeliverySLAPeriod 是发货 SLA 看门狗的默认扫描周期（60 秒）。
const defaultDeliverySLAPeriod = 60 * time.Second

// deliverySLAScanPageSize 是待发货候选扫描的单页大小，与调度器既有扫描保持同一量级。
const deliverySLAScanPageSize = 50

// deliverySLALevelNormal 表示订单尚未进入 SLA 提醒区间。
const deliverySLALevelNormal = 0

// deliverySLALevelWarning 表示已消耗 80% 及以上但未满 100% SLA，需要临期提醒。
const deliverySLALevelWarning = 1

// deliverySLALevelOverdue 表示已达到或超过 100% SLA，需要超时提醒。
const deliverySLALevelOverdue = 2

// deliverySLAConfig 是解析后的发货 SLA 配置。
type deliverySLAConfig struct {
	// DefaultMinutes 是默认 SLA 分钟数；0 表示未配置默认值。
	DefaultMinutes int `json:"default_minutes"`
	// PerAccount 是按 cookie_id 的覆盖映射；正值覆盖默认，0 表示该账号关闭监控。
	PerAccount map[string]int `json:"per_account"`
}

// parseDeliverySLAConfig 解析系统设置里的发货 SLA 配置原文。
// 返回 enabled=false 表示整体关闭：空串（缺省）、非法 JSON、default_minutes 为负数、
// per_account 出现负数覆盖（非法），或 default_minutes=0 且没有任何正数覆盖。
// default_minutes 字段缺失按 0 处理，再走「无有效覆盖即关闭」的判定。
func parseDeliverySLAConfig(raw string) (deliverySLAConfig, bool) {
	// text 是去除首尾空白后的配置文本；空串视为未配置。
	text := strings.TrimSpace(raw)
	if text == "" {
		return deliverySLAConfig{}, false
	}
	// cfg 是解析出的配置对象。
	var cfg deliverySLAConfig
	// err 保存 JSON 解析错误；解析失败视为非法配置并整体关闭。
	if err := json.Unmarshal([]byte(text), &cfg); err != nil {
		return deliverySLAConfig{}, false
	}
	// 默认分钟数为负数属于非法配置，整体关闭以免用错误阈值误报。
	if cfg.DefaultMinutes < 0 {
		return deliverySLAConfig{}, false
	}
	// hasEffective 表示是否存在正数账号覆盖（有效覆盖）。
	hasEffective := false
	// minutes 是当前遍历到的账号覆盖分钟数。
	for _, minutes := range cfg.PerAccount {
		// 负数覆盖属于非法配置，整体关闭；0 是合法的「该账号关闭」。
		if minutes < 0 {
			return deliverySLAConfig{}, false
		}
		if minutes > 0 {
			hasEffective = true
		}
	}
	// 默认 0 且无任何有效覆盖时整体关闭。
	if cfg.DefaultMinutes == 0 && !hasEffective {
		return deliverySLAConfig{}, false
	}
	return cfg, true
}

// minutesFor 返回该账号生效的 SLA 分钟数；0 表示不监控该账号。
// per_account 显式覆盖优先：正值为专属分钟数，0 表示该账号关闭；键不存在时回落默认分钟数。
func (c deliverySLAConfig) minutesFor(cookieID string) int {
	// minutes、ok 保存该账号的显式覆盖值及是否存在覆盖。
	if minutes, ok := c.PerAccount[cookieID]; ok {
		if minutes > 0 {
			return minutes
		}
		return 0
	}
	if c.DefaultMinutes > 0 {
		return c.DefaultMinutes
	}
	return 0
}

// deliverySLALevel 计算订单发货 SLA 阶段：0=正常，1=临期（≥80% 且 <100%），2=已超时（≥100%）。
// elapsed 是付款至今时长，sla 是该账号生效的 SLA 总时长；sla<=0 或 elapsed<0（时钟偏差）恒为正常。
// 恰好 80% 进入临期、恰好 100% 进入超时，保证阈值语义可被测试精确锚定。
func deliverySLALevel(elapsed, sla time.Duration) int {
	if sla <= 0 || elapsed < 0 {
		return deliverySLALevelNormal
	}
	if elapsed >= sla {
		return deliverySLALevelOverdue
	}
	// warnAt 是 80% 阈值；先除后乘避免超大 SLA 乘法溢出。
	if elapsed >= sla/5*4 {
		return deliverySLALevelWarning
	}
	return deliverySLALevelNormal
}

// deliverySLAMinutesCeil 把时长向上取整为分钟数；非正时长返回 0。
// 向上取整保证临期/超时文案在不足一分钟时不会退化成误导性的 0 分钟。
func deliverySLAMinutesCeil(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Minute - 1) / time.Minute)
}

// isPendingShipOrderStatus 判断订单状态是否属于待发货类。
// 先按仓储归一映射（paid/2 → pending_ship），再兼容历史别名 pending_delivery、
// partial_success、partial_pending_finalize，与订单查询侧的待发货集合保持一致。
func isPendingShipOrderStatus(status string) bool {
	if db.NormalizeOrderStatus(status) == "pending_ship" {
		return true
	}
	// raw 是去除空白后的原始状态文本，用于匹配历史别名。
	raw := strings.TrimSpace(status)
	return raw == "pending_delivery" || raw == "partial_success" || raw == "partial_pending_finalize"
}

// buildDeliverySLANotification 按阶段组装通知事件类型、标题与正文。
// elapsed 为付款至今时长，sla 为生效 SLA 总长；标题含订单号与剩余/超时分钟。
// 正文只描述 SLA 与处理要求，绝不包含收货地址、手机号或卡密。
func buildDeliverySLANotification(orderID string, level int, elapsed, sla time.Duration) (eventType, title, body string) {
	if level == deliverySLALevelOverdue {
		// overdueMinutes 是已超时分钟数，不足一分钟向上取整。
		overdueMinutes := deliverySLAMinutesCeil(elapsed - sla)
		return deliverySLAOverdueEventType,
			fmt.Sprintf("发货 SLA 已超时：订单 %s 超时 %d 分钟", orderID, overdueMinutes),
			fmt.Sprintf("订单 %s 已超过发货 SLA（配置 %d 分钟）共 %d 分钟，请尽快处理。", orderID, int(sla/time.Minute), overdueMinutes)
	}
	// remainingMinutes 是剩余分钟数，不足一分钟向上取整。
	remainingMinutes := deliverySLAMinutesCeil(sla - elapsed)
	return deliverySLAWarningEventType,
		fmt.Sprintf("发货 SLA 临期提醒：订单 %s 剩余 %d 分钟", orderID, remainingMinutes),
		fmt.Sprintf("订单 %s 已接近发货 SLA（配置 %d 分钟），剩余 %d 分钟，请尽快发货。", orderID, int(sla/time.Minute), remainingMinutes)
}

// DeliverySLAOrderReader 读取待发货候选订单快照；返回值由看门狗再按 paid_at/shipped_at 过滤。
// 由装配层注入仓储查询，便于测试替身与未来替换更通用的扫描。
type DeliverySLAOrderReader func(ctx context.Context) ([]db.Order, error)

// DeliverySLANotifier 按账号发送发货 SLA 提醒事件；签名与 notify.Notifier.NotifyAccountEvent 对齐。
// 实现负责路由到该账号已启用的通知渠道；ctx 只约束本次发送前的查询。
type DeliverySLANotifier interface {
	// NotifyAccountEvent 发送一条账号维度事件通知；eventType 为 delivery_sla_warning 或 delivery_sla_overdue。
	NotifyAccountEvent(cookieID, eventType, level, title, body string)
}

// deliverySLANotifierFrom 从既有 Notifier 提取账号事件通知能力。
// 只实现 NotifyAutomationRun 的实现返回 nil，看门狗降级为只记日志不发通知。
func deliverySLANotifierFrom(notifier Notifier) DeliverySLANotifier {
	// candidate、ok 保存账号事件能力及类型判断结果。
	if candidate, ok := notifier.(DeliverySLANotifier); ok {
		return candidate
	}
	return nil
}

// fetchPendingShipOrdersForSLA 分页收集 SLA 候选订单（已付款、未发货）。
// 选用 Automation.PendingShipPaidOrdersAfter：它面向 SLA 提醒，不附加「无 order_paid 运行」
// 约束，能覆盖已有运行但仍未发货的订单——那正是超时风险最高的集合。
// pageSize<=0 时回落默认单页大小；rules 为 nil 时返回空集合表示未装配。
func fetchPendingShipOrdersForSLA(ctx context.Context, rules *db.AutomationRules, pageSize int) ([]db.Order, error) {
	if rules == nil {
		return nil, nil
	}
	if pageSize <= 0 {
		pageSize = deliverySLAScanPageSize
	}
	// out 收集全部候选；待发货集合以未发货订单为界，单轮逐页扫完不会无界增长。
	out := []db.Order{}
	// afterOrderID 是订单号稳定游标，保证分页不重不漏。
	afterOrderID := ""
	for {
		// page、err 保存本页候选及查询错误。
		page, err := rules.PendingShipPaidOrdersAfter(ctx, afterOrderID, pageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		// 本页未满说明已到末尾。
		if len(page) < pageSize {
			return out, nil
		}
		afterOrderID = page[len(page)-1].OrderID
	}
}

// deliverySLAWatchdog 是发货 SLA 超时提醒看门狗。
//
// 并发与生命周期（AGENTS 1.6）：
//   - 归属：由 Center 在构造时创建并持有；由 Scheduler.Run 启动，随调度关停路径 Stop。
//   - goroutine：Start 派生唯一扫描协程，Context 来源是 Scheduler.Run 传入的 ctx；
//     Stop 或 ctx 取消时扫描协程退出，done 关闭后 Stop 才返回。
//   - 锁：runMu 保护 running/cancel/done 生命周期字段；mu 保护 notified 去重表。两把锁永不嵌套。
//   - 无锁 I/O：配置读取、订单扫描与通知发送都在不持任何锁的区间执行。
//   - 关停：Stop 幂等，可重复调用；未启动时调用为空操作。
type deliverySLAWatchdog struct {
	// runMu 保护 running、cancel 与 done；Start/Stop 串行化生命周期状态迁移。
	runMu sync.Mutex
	// running 表示扫描协程是否在运行；Start 与 Stop 的幂等性以它为准。
	running bool
	// cancel 取消扫描协程的 Context；running 为 true 时非 nil。
	cancel context.CancelFunc
	// done 在扫描协程退出后关闭，Stop 通过它等待退出完成。
	done chan struct{}
	// mu 保护 notified 去重表；登记与发送之间不持锁。
	mu sync.Mutex
	// notified 记录已发送的「订单号+级别」去重键，进程内每键只发一次。
	notified map[string]struct{}
	// getSetting 读取系统设置；为 nil 表示未装配设置仓储，配置恒为缺省并整体关闭。
	getSetting func(context.Context, string) (string, error)
	// readOrders 读取待发货候选订单；为 nil 表示未装配订单查询，扫描直接跳过。
	readOrders DeliverySLAOrderReader
	// notifier 发送账号事件通知；为 nil 时只记日志不发通知，便于测试与降级运行。
	notifier DeliverySLANotifier
	// now 返回当前时间；生产为 time.Now，测试注入假时钟驱动阈值判定。
	now func() time.Time
	// period 是扫描周期；零值或负值回落默认 60s。
	period time.Duration
	// logger 记录扫描、提醒与失败；不包含地址、手机、卡密等敏感内容。
	logger *slog.Logger
}

// newDeliverySLAWatchdog 按仓储与通知器装配发货 SLA 看门狗。
// 配置每轮扫描都重新读取，运行期可改阈值；缺省或非法配置下扫描自动关闭。
// store 为 nil 或缺少设置仓储时视为未配置；缺少订单仓储时扫描跳过。
func newDeliverySLAWatchdog(store *db.Store, notifier DeliverySLANotifier, logger *slog.Logger) *deliverySLAWatchdog {
	if logger == nil {
		logger = slog.Default()
	}
	// watchdog 是装配完成的看门狗实例；生命周期字段由 Start 初始化。
	watchdog := &deliverySLAWatchdog{
		notified: make(map[string]struct{}),
		notifier: notifier,
		now:      time.Now,
		logger:   logger,
	}
	if store != nil && store.Settings != nil {
		watchdog.getSetting = store.Settings.Get
	}
	if store != nil && store.Automation != nil {
		// readOrders 固定绑定当前仓储的待发货扫描，分页大小用生产默认值。
		watchdog.readOrders = func(ctx context.Context) ([]db.Order, error) {
			return fetchPendingShipOrdersForSLA(ctx, store.Automation, deliverySLAScanPageSize)
		}
	}
	return watchdog
}

// Start 启动发货 SLA 扫描协程；重复调用幂等，已在运行时忽略。
// ctx 提供扫描生命周期，通常传 Scheduler.Run 的 ctx；ctx 取消时扫描协程自行退出。
func (w *deliverySLAWatchdog) Start(ctx context.Context) {
	// 未装配或缺少生命周期 Context 时拒绝启动，避免创建无法回收的协程。
	if w == nil || ctx == nil {
		return
	}
	w.runMu.Lock()
	defer w.runMu.Unlock()
	// 幂等：已在运行时忽略重复启动。
	if w.running {
		return
	}
	// runCtx、cancel 把外部生命周期收敛到看门狗可独立停止的取消信号。
	runCtx, cancel := context.WithCancel(ctx)
	// done 在扫描协程退出时关闭，供 Stop 等待。
	done := make(chan struct{})
	w.cancel = cancel
	w.done = done
	w.running = true
	go w.runLoop(runCtx, done)
}

// Stop 停止扫描协程并等待退出；可重复调用，未启动时为空操作。
// 等待在锁外完成，避免持 runMu 阻塞其他生命周期调用。
func (w *deliverySLAWatchdog) Stop() {
	if w == nil {
		return
	}
	w.runMu.Lock()
	// 幂等：未运行时直接返回。
	if !w.running {
		w.runMu.Unlock()
		return
	}
	// cancel、done 是摘下的运行句柄；置空后重复 Stop 直接返回。
	cancel, done := w.cancel, w.done
	w.running = false
	w.cancel = nil
	w.done = nil
	w.runMu.Unlock()
	cancel()
	<-done
}

// runLoop 是扫描协程主体：启动即扫一次，再按周期驱动；ctx 取消或 Stop 时退出并关闭 done。
func (w *deliverySLAWatchdog) runLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	// ticker 按生效周期驱动扫描。
	ticker := time.NewTicker(w.periodValue())
	defer ticker.Stop()
	// 启动即扫描一次，避免首条提醒被推迟一个完整周期。
	w.scanOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scanOnce(ctx)
		}
	}
}

// periodValue 返回生效扫描周期；零值或负值回落默认 60s。
func (w *deliverySLAWatchdog) periodValue() time.Duration {
	if w.period > 0 {
		return w.period
	}
	return defaultDeliverySLAPeriod
}

// readConfigText 读取发货 SLA 配置原文；未装配设置仓储时返回空串表示未配置。
func (w *deliverySLAWatchdog) readConfigText(ctx context.Context) (string, error) {
	if w.getSetting == nil {
		return "", nil
	}
	return w.getSetting(ctx, deliverySLASettingKey)
}

// scanOnce 执行一轮 SLA 扫描：读配置、拉候选订单、逐单判定并提醒。
// 配置读取或订单查询失败只记日志并跳过本轮，绝不中断调度循环，也不在锁内做 I/O。
func (w *deliverySLAWatchdog) scanOnce(ctx context.Context) {
	if w == nil {
		return
	}
	// raw、settingErr 保存发货 SLA 配置原文及读取错误；读取失败时本轮降级关闭，绝不猜测阈值。
	raw, settingErr := w.readConfigText(ctx)
	if settingErr != nil {
		w.logger.Warn("读取发货 SLA 配置失败，本轮跳过扫描", "err", settingErr)
		return
	}
	// cfg、enabled 保存解析结果；缺省或非法配置整体关闭。
	cfg, enabled := parseDeliverySLAConfig(raw)
	if !enabled {
		return
	}
	// 未装配订单查询时无法扫描，直接跳过。
	if w.readOrders == nil {
		return
	}
	// orders、err 保存待发货候选订单及查询错误。
	orders, err := w.readOrders(ctx)
	if err != nil {
		w.logger.Warn("扫描待发货订单失败", "err", err)
		return
	}
	// now 是本次扫描的时间基准，由可注入时钟提供，保证测试确定性。
	now := w.now()
	// order 表示当前遍历的候选订单。
	for _, order := range orders {
		w.evaluateOrder(ctx, cfg, order, now)
	}
}

// evaluateOrder 判定单笔订单的 SLA 阶段并在需要时发送提醒。
// 只读取订单的非敏感字段（订单号、账号、状态、付款/发货时间）；通知不含地址、手机与卡密。
// 去重命中或发送都在不持锁区间完成，满足无锁 I/O 约束。
func (w *deliverySLAWatchdog) evaluateOrder(ctx context.Context, cfg deliverySLAConfig, order db.Order, now time.Time) {
	// 缺订单号时无法稳定去重与指认订单，跳过。
	if strings.TrimSpace(order.OrderID) == "" {
		return
	}
	// 只处理待发货类状态。
	if !isPendingShipOrderStatus(order.OrderStatus) {
		return
	}
	// 只监控已付款且尚未发货的订单。
	if strings.TrimSpace(order.PaidAt) == "" || strings.TrimSpace(order.ShippedAt) != "" {
		return
	}
	// slaMinutes 是该账号生效的 SLA 分钟数；0 表示不监控本账号。
	slaMinutes := cfg.minutesFor(order.CookieID)
	if slaMinutes <= 0 {
		return
	}
	// paidAt 是解析后的付款时刻；无法解析时跳过，避免用错误基准误报。
	paidAt := parseDBTime(order.PaidAt)
	if paidAt.IsZero() {
		return
	}
	// sla 是生效 SLA 总时长。
	sla := time.Duration(slaMinutes) * time.Minute
	// elapsed 是付款至今时长。
	elapsed := now.Sub(paidAt)
	// level 是 SLA 阶段：0 正常、1 临期、2 超时。
	level := deliverySLALevel(elapsed, sla)
	if level == deliverySLALevelNormal {
		return
	}
	// levelKey 是去重键里的级别段；warning 与 overdue 各自只发一次。
	levelKey := deliverySLAWarningEventType
	if level == deliverySLALevelOverdue {
		levelKey = deliverySLAOverdueEventType
	}
	// key 是进程内去重键：订单号+级别。
	key := order.OrderID + "\x00" + levelKey
	// 通知仓储现有查询只有 outbox 入队/认领/渠道与不确定列表，没有「近 48 小时同订单同级别」
	// 的历史检索入口，因此只用进程内去重；进程重启后同订单同级别可能极少重复一次，符合计划文档的容许语义。
	if !w.claimNotify(key) {
		return
	}
	// eventType、title、body 组装本次提醒内容；级别固定 info，以事件类型区分临期与超时。
	eventType, title, body := buildDeliverySLANotification(order.OrderID, level, elapsed, sla)
	// elapsedMinutes 是付款至今分钟数，仅用于日志排查。
	elapsedMinutes := int(elapsed / time.Minute)
	w.logger.Info("发送发货 SLA 提醒", "order_id", order.OrderID, "account", order.CookieID,
		"event_type", eventType, "elapsed_minutes", elapsedMinutes, "sla_minutes", slaMinutes)
	if w.notifier != nil {
		w.notifier.NotifyAccountEvent(order.CookieID, eventType, deliverySLANotifyLevel, title, body)
	}
}

// claimNotify 在进程内去重表登记「订单号+级别」键；返回 true 表示本次可以发送。
// mu 只保护 notified 映射，登记与发送之间不持锁，避免通知 I/O 拖住并发调用。
func (w *deliverySLAWatchdog) claimNotify(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	// 已登记说明本进程已发过同订单同级别提醒，直接去重。
	if _, exists := w.notified[key]; exists {
		return false
	}
	w.notified[key] = struct{}{}
	return true
}
