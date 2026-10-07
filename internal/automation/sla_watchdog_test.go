package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// slaNotifyCall 记录一次发货 SLA 通知的全部参数，供测试断言内容与去重。
type slaNotifyCall struct {
	// cookieID 是通知归属账号标识。
	cookieID string
	// eventType 是通知事件类型编码。
	eventType string
	// level 是通知级别。
	level string
	// title 是通知标题。
	title string
	// body 是通知正文。
	body string
}

// slaTestNotifier 记录发货 SLA 通知调用的测试替身。
type slaTestNotifier struct {
	// calls 保存按发生顺序的通知参数记录。
	calls []slaNotifyCall
}

// NotifyAccountEvent 记录一条账号事件通知参数。
func (n *slaTestNotifier) NotifyAccountEvent(cookieID, eventType, level, title, body string) {
	n.calls = append(n.calls, slaNotifyCall{cookieID: cookieID, eventType: eventType, level: level, title: title, body: body})
}

// slaDualNotifier 同时满足旧 Notifier 与发货 SLA 账号事件接口，用于验证能力提取成功路径。
type slaDualNotifier struct {
	// slaTestNotifier 复用通知参数记录能力。
	slaTestNotifier
}

// NotifyAutomationRun 满足旧 Notifier 接口；本测试不关心自动化运行终态。
func (n *slaDualNotifier) NotifyAutomationRun(context.Context, int64, string, string, string, string, string, string) {
}

// slaAutomationOnlyNotifier 只满足旧 Notifier 接口，用于验证能力提取失败时的降级。
type slaAutomationOnlyNotifier struct{}

// NotifyAutomationRun 满足旧 Notifier 接口；不提供账号事件能力。
func (slaAutomationOnlyNotifier) NotifyAutomationRun(context.Context, int64, string, string, string, string, string, string) {
}

// slaTestSettings 是可编程的系统设置读取替身。
type slaTestSettings struct {
	// raw 是每次读取返回的配置原文。
	raw string
	// err 非 nil 时模拟设置读取失败。
	err error
	// calls 记录读取次数，用于验证关闭或失败路径不再触发订单扫描。
	calls int
}

// get 返回预设配置原文或错误，并累计读取次数。
func (s *slaTestSettings) get(context.Context, string) (string, error) {
	s.calls++
	return s.raw, s.err
}

// slaTestOrders 是可编程的待发货候选读取替身。
type slaTestOrders struct {
	// orders 是每次读取返回的候选订单。
	orders []db.Order
	// err 非 nil 时模拟订单查询失败。
	err error
	// calls 记录读取次数。
	calls int
	// scanned 非 nil 时在每次读取后非阻塞投递信号，消除扫描协程与断言之间的竞态。
	scanned chan struct{}
}

// read 返回预设候选订单或错误，并在需要时发出扫描信号。
func (s *slaTestOrders) read(context.Context) ([]db.Order, error) {
	s.calls++
	if s.scanned != nil {
		// 信号只用于测试同步；缓冲区满时丢弃，不阻塞扫描协程。
		select {
		case s.scanned <- struct{}{}:
		default:
		}
	}
	return s.orders, s.err
}

// slaTestClock 是可手动推进的假时钟，保证 SLA 阈值判定的确定性。
type slaTestClock struct {
	// current 是当前假时间。
	current time.Time
}

// now 返回当前假时间，匹配 deliverySLAWatchdog.now 的签名。
func (c *slaTestClock) now() time.Time { return c.current }

// newTestSLAWatchdog 用可编程替身构造被测看门狗，供行为测试直接驱动 scanOnce。
func newTestSLAWatchdog(settings *slaTestSettings, orders *slaTestOrders, notifier DeliverySLANotifier, clock *slaTestClock) *deliverySLAWatchdog {
	return &deliverySLAWatchdog{
		notified:   make(map[string]struct{}),
		getSetting: settings.get,
		readOrders: orders.read,
		notifier:   notifier,
		now:        clock.now,
		logger:     silentTestLogger(),
	}
}

// slaBaseTime 是测试统一使用的基准时刻，全部相对偏移以它为锚点。
var slaBaseTime = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// slaOrderAtElapsed 构造一笔已付款待发货订单，付款时刻为基准减去 elapsed。
func slaOrderAtElapsed(orderID, cookieID string, elapsed time.Duration) db.Order {
	return db.Order{
		OrderID:     orderID,
		CookieID:    cookieID,
		OrderStatus: "pending_ship",
		PaidAt:      slaBaseTime.Add(-elapsed).Format(time.RFC3339Nano),
	}
}

// TestParseDeliverySLAConfigAndMinutesFor 覆盖配置解析与按账号取值：缺省、非法、默认、覆盖与关闭。
func TestParseDeliverySLAConfigAndMinutesFor(t *testing.T) {
	// cases 是表驱动用例，同时断言整体开关与典型账号的生效分钟数。
	cases := []struct {
		// name 是用例名称。
		name string
		// raw 是设置原文。
		raw string
		// wantEnabled 是期望的整体开关。
		wantEnabled bool
		// wantDefault 是期望的默认账号生效分钟数。
		wantDefault int
		// wantOverride 是期望的覆盖账号生效分钟数。
		wantOverride int
	}{
		{"空串缺省关闭", "", false, 0, 0},
		{"纯空白缺省关闭", "   ", false, 0, 0},
		{"非法 JSON 关闭", "{bad json", false, 0, 0},
		{"JSON 数组非法关闭", "[1,2]", false, 0, 0},
		{"负默认非法关闭", `{"default_minutes":-1}`, false, 0, 0},
		{"负覆盖非法关闭", `{"default_minutes":100,"per_account":{"acct":-5}}`, false, 0, 0},
		{"默认零且无有效覆盖关闭", `{"default_minutes":0}`, false, 0, 0},
		{"默认零且覆盖为零关闭", `{"default_minutes":0,"per_account":{"acct":0}}`, false, 0, 0},
		{"默认生效", `{"default_minutes":120}`, true, 120, 120},
		{"覆盖优先于默认", `{"default_minutes":120,"per_account":{"acct":30}}`, true, 120, 30},
		{"覆盖零关闭单账号", `{"default_minutes":120,"per_account":{"acct":0}}`, true, 120, 0},
		{"默认零仅覆盖账号生效", `{"default_minutes":0,"per_account":{"acct":45}}`, true, 0, 45},
		{"缺字段按零默认再判覆盖", `{"per_account":{"acct":50}}`, true, 0, 50},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// cfg、enabled 保存解析结果。
		cfg, enabled := parseDeliverySLAConfig(tc.raw)
		if enabled != tc.wantEnabled {
			t.Fatalf("%s: enabled = %v, want %v", tc.name, enabled, tc.wantEnabled)
		}
		if !enabled {
			continue
		}
		// gotDefault、gotOverride 保存默认账号与覆盖账号的生效分钟数。
		gotDefault, gotOverride := cfg.minutesFor("other"), cfg.minutesFor("acct")
		if gotDefault != tc.wantDefault || gotOverride != tc.wantOverride {
			t.Fatalf("%s: minutesFor other=%d acct=%d, want %d/%d", tc.name, gotDefault, gotOverride, tc.wantDefault, tc.wantOverride)
		}
	}
}

// TestDeliverySLALevelBoundaries 覆盖 79/80/99/100 阈值边界与非法时长输入。
func TestDeliverySLALevelBoundaries(t *testing.T) {
	// sla 是统一的 100 分钟 SLA，百分比边界与分钟数一一对应。
	sla := 100 * time.Minute
	// cases 覆盖阈值边界与非法输入。
	cases := []struct {
		// name 是用例名称。
		name string
		// elapsed 是付款至今时长。
		elapsed time.Duration
		// sla 是生效 SLA 总时长。
		sla time.Duration
		// want 是期望阶段。
		want int
	}{
		{"百分之七十九正常", 79 * time.Minute, sla, deliverySLALevelNormal},
		{"百分之八十临期", 80 * time.Minute, sla, deliverySLALevelWarning},
		{"百分之九十九临期", 99 * time.Minute, sla, deliverySLALevelWarning},
		{"百分之百超时", 100 * time.Minute, sla, deliverySLALevelOverdue},
		{"超过百分之百超时", 150 * time.Minute, sla, deliverySLALevelOverdue},
		{"未启用恒为正常", 200 * time.Minute, 0, deliverySLALevelNormal},
		{"负时长按正常处理", -time.Minute, sla, deliverySLALevelNormal},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// got 保存实际阶段判定。
		got := deliverySLALevel(tc.elapsed, tc.sla)
		if got != tc.want {
			t.Fatalf("%s: deliverySLALevel = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestDeliverySLAMinutesCeil 覆盖分钟向上取整：零、负、不足一分钟与整分钟。
func TestDeliverySLAMinutesCeil(t *testing.T) {
	// cases 覆盖取整边界。
	cases := []struct {
		// name 是用例名称。
		name string
		// d 是输入时长。
		d time.Duration
		// want 是期望分钟数。
		want int
	}{
		{"零时长为零", 0, 0},
		{"负时长为零", -time.Minute, 0},
		{"不足一分钟向上取整", time.Second, 1},
		{"整分钟保持原值", 5 * time.Minute, 5},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// got 保存取整结果。
		got := deliverySLAMinutesCeil(tc.d)
		if got != tc.want {
			t.Fatalf("%s: deliverySLAMinutesCeil = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestIsPendingShipOrderStatus 覆盖待发货类状态识别与非待发货状态。
func TestIsPendingShipOrderStatus(t *testing.T) {
	// cases 覆盖归一别名、历史别名与非待发货状态。
	cases := []struct {
		// name 是用例名称。
		name string
		// status 是订单状态文本。
		status string
		// want 是期望判定。
		want bool
	}{
		{"pending_ship 待发货", "pending_ship", true},
		{"paid 归一为待发货", "paid", true},
		{"数字 2 归一为待发货", "2", true},
		{"历史别名 pending_delivery", "pending_delivery", true},
		{"历史别名 partial_success", "partial_success", true},
		{"历史别名 partial_pending_finalize", "partial_pending_finalize", true},
		{"已发货不监控", "shipped", false},
		{"已完成不监控", "completed", false},
		{"空状态不监控", "", false},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// got 保存实际判定。
		got := isPendingShipOrderStatus(tc.status)
		if got != tc.want {
			t.Fatalf("%s: isPendingShipOrderStatus = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestBuildDeliverySLANotification 覆盖临期与超时文案：标题含订单号与分钟数，正文不含敏感字段。
func TestBuildDeliverySLANotification(t *testing.T) {
	// sla 是 100 分钟配置总量。
	sla := 100 * time.Minute
	// warnType、warnTitle、warnBody 保存临期文案。
	warnType, warnTitle, warnBody := buildDeliverySLANotification("order-1", deliverySLALevelWarning, 85*time.Minute, sla)
	if warnType != deliverySLAWarningEventType {
		t.Fatalf("临期事件类型 = %s, want %s", warnType, deliverySLAWarningEventType)
	}
	if !strings.Contains(warnTitle, "order-1") || !strings.Contains(warnTitle, "剩余 15 分钟") {
		t.Fatalf("临期标题未含订单号与剩余分钟: %s", warnTitle)
	}
	if !strings.Contains(warnBody, "order-1") || !strings.Contains(warnBody, "100") {
		t.Fatalf("临期正文未含订单号与 SLA 配置: %s", warnBody)
	}
	// overType、overTitle、overBody 保存超时文案。
	overType, overTitle, overBody := buildDeliverySLANotification("order-2", deliverySLALevelOverdue, 130*time.Minute, sla)
	if overType != deliverySLAOverdueEventType {
		t.Fatalf("超时事件类型 = %s, want %s", overType, deliverySLAOverdueEventType)
	}
	if !strings.Contains(overTitle, "order-2") || !strings.Contains(overTitle, "超时 30 分钟") {
		t.Fatalf("超时标题未含订单号与超时分钟: %s", overTitle)
	}
	if !strings.Contains(overBody, "order-2") || !strings.Contains(overBody, "100") {
		t.Fatalf("超时正文未含订单号与 SLA 配置: %s", overBody)
	}
}

// TestDeliverySLANotifierFrom 覆盖通知能力提取：双接口成功、单接口降级与 nil 降级。
func TestDeliverySLANotifierFrom(t *testing.T) {
	// dual 是同时实现两种接口的通知器。
	dual := &slaDualNotifier{}
	if deliverySLANotifierFrom(dual) == nil {
		t.Fatal("双接口通知器应提取出账号事件能力")
	}
	// only 旧接口通知器应降级为 nil。
	var only Notifier = slaAutomationOnlyNotifier{}
	if deliverySLANotifierFrom(only) != nil {
		t.Fatal("单接口通知器应降级为 nil")
	}
	// nilNotifier 是未装配通知器的空接口值。
	var nilNotifier Notifier
	if deliverySLANotifierFrom(nilNotifier) != nil {
		t.Fatal("nil 通知器应降级为 nil")
	}
}

// TestFetchPendingShipOrdersForSLA 覆盖默认扫描：未装配、空表、取消错误与多页游标。
func TestFetchPendingShipOrdersForSLA(t *testing.T) {
	// ctx 是查询生命周期上下文。
	ctx := context.Background()
	// nilRules、nilErr 保存未装配仓储时的空集合结果。
	nilRules, nilErr := fetchPendingShipOrdersForSLA(ctx, nil, 2)
	if nilErr != nil || len(nilRules) != 0 {
		t.Fatalf("未装配仓储应返回空集合, got %v %v", nilRules, nilErr)
	}
	// store、cleanup 保存真实 SQLite 仓储及清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// admin 是夹具内置的管理员账号。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、ruleErr 保存启用的 order_paid 规则写入结果；商品留空表示匹配全部订单。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "sla-scan-rule", TriggerType: TriggerOrderPaid, Enabled: true,
	})
	if ruleErr != nil {
		t.Fatalf("写入规则夹具失败: %v", ruleErr)
	}
	if ruleID <= 0 {
		t.Fatalf("规则主键应为正数, got %d", ruleID)
	}
	// orderID 表示当前遍历过程中的订单号。
	for _, orderID := range []string{"sla-scan-o1", "sla-scan-o2", "sla-scan-o3"} {
		// upsertErr 保存订单夹具写入错误。
		if upsertErr := store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
			CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", OrderStatus: "pending_ship",
		}); upsertErr != nil {
			t.Fatalf("写入订单夹具失败: %v", upsertErr)
		}
		// paidErr 保存付款时间补写错误；SLA 扫描只收集已付款订单。
		if _, paidErr := store.DB.ExecContext(ctx, `UPDATE orders SET paid_at=? WHERE order_id=?`, "2026-09-30T01:00:00Z", orderID); paidErr != nil {
			t.Fatalf("写入付款时间失败: %v", paidErr)
		}
	}
	// multiPage、multiErr 保存小分页扫描结果，覆盖游标续页分支。
	multiPage, multiErr := fetchPendingShipOrdersForSLA(ctx, store.Automation, 2)
	if multiErr != nil {
		t.Fatalf("小分页扫描失败: %v", multiErr)
	}
	if len(multiPage) != 3 {
		t.Fatalf("小分页扫描应收集 3 笔订单, got %d", len(multiPage))
	}
	// defaultPage、defaultErr 保存默认单页大小的扫描结果，覆盖页未满提前返回分支。
	defaultPage, defaultErr := fetchPendingShipOrdersForSLA(ctx, store.Automation, 0)
	if defaultErr != nil || len(defaultPage) != 3 {
		t.Fatalf("默认分页扫描应收集 3 笔订单, got %d %v", len(defaultPage), defaultErr)
	}
	// cancelCtx、cancel 构造已取消上下文，驱动查询错误分支。
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	// errOrders、errResult 保存查询失败路径的返回值。
	errOrders, errResult := fetchPendingShipOrdersForSLA(cancelCtx, store.Automation, 2)
	if errResult == nil {
		t.Fatalf("已取消上下文应返回查询错误, got %v", errOrders)
	}
}

// TestNewDeliverySLAWatchdogVariants 覆盖构造分支：空仓储、缺设置、缺订单仓储与缺省日志器。
func TestNewDeliverySLAWatchdogVariants(t *testing.T) {
	// dual 是满足账号事件能力的测试通知器。
	dual := &slaDualNotifier{}
	// nilStoreWatchdog 用空仓储构造，设置与订单读取都应为空。
	nilStoreWatchdog := newDeliverySLAWatchdog(nil, dual, silentTestLogger())
	if nilStoreWatchdog.getSetting != nil || nilStoreWatchdog.readOrders != nil {
		t.Fatal("空仓储不应装配设置与订单读取")
	}
	// emptyStore 是只含空字段的仓储，模拟缺设置与缺订单仓储。
	emptyStoreWatchdog := newDeliverySLAWatchdog(&db.Store{}, dual, silentTestLogger())
	if emptyStoreWatchdog.getSetting != nil || emptyStoreWatchdog.readOrders != nil {
		t.Fatal("缺字段仓储不应装配设置与订单读取")
	}
	// store、cleanup 保存真实仓储及清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// fullWatchdog 用真实仓储与 nil 日志器构造，两路读取都应装配且日志器回落默认。
	fullWatchdog := newDeliverySLAWatchdog(store, nil, nil)
	if fullWatchdog.getSetting == nil || fullWatchdog.readOrders == nil {
		t.Fatal("真实仓储应装配设置与订单读取")
	}
	if fullWatchdog.logger == nil {
		t.Fatal("缺省日志器应回落默认实现")
	}
	if fullWatchdog.notifier != nil {
		t.Fatal("未注入通知器时应保持 nil 以便降级")
	}
}

// TestDeliverySLAWatchdogScanThresholds 通过 scanOnce 验证 79/80/99/100 阈值只触发对应阶段提醒。
func TestDeliverySLAWatchdogScanThresholds(t *testing.T) {
	// config 是 100 分钟默认 SLA 配置原文。
	config := `{"default_minutes":100}`
	// cases 覆盖阈值边界期望。
	cases := []struct {
		// name 是用例名称。
		name string
		// elapsed 是付款至今分钟数。
		elapsed time.Duration
		// wantType 是期望事件类型；空串表示不发通知。
		wantType string
	}{
		{"百分之七十九不提醒", 79 * time.Minute, ""},
		{"百分之八十临期提醒", 80 * time.Minute, deliverySLAWarningEventType},
		{"百分之九十九仍为临期", 99 * time.Minute, deliverySLAWarningEventType},
		{"百分之百超时提醒", 100 * time.Minute, deliverySLAOverdueEventType},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// settings、orders、notifier、clock 是该用例独立的替身，避免去重串扰。
		settings := &slaTestSettings{raw: config}
		// orders 返回单笔按 elapsed 夹具构造的待发货订单。
		orders := &slaTestOrders{orders: []db.Order{slaOrderAtElapsed("order-th", "cid", tc.elapsed)}}
		// notifier 记录本用例的通知。
		notifier := &slaTestNotifier{}
		// clock 固定在基准时刻，elapsed 由夹具付款时间倒推。
		clock := &slaTestClock{current: slaBaseTime}
		// watchdog 是本用例被测对象。
		watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
		watchdog.scanOnce(context.Background())
		if tc.wantType == "" {
			if len(notifier.calls) != 0 {
				t.Fatalf("%s: 不应提醒, got %d 次", tc.name, len(notifier.calls))
			}
			continue
		}
		if len(notifier.calls) != 1 {
			t.Fatalf("%s: 应提醒一次, got %d 次", tc.name, len(notifier.calls))
		}
		// call 是唯一一条通知记录。
		call := notifier.calls[0]
		if call.eventType != tc.wantType || call.level != deliverySLANotifyLevel || call.cookieID != "cid" {
			t.Fatalf("%s: 通知参数 = %+v, want type=%s level=%s", tc.name, call, tc.wantType, deliverySLANotifyLevel)
		}
		if !strings.Contains(call.title, "order-th") {
			t.Fatalf("%s: 标题未含订单号: %s", tc.name, call.title)
		}
	}
}

// TestDeliverySLAWatchdogConfigModes 通过 scanOnce 验证默认、覆盖、单账号关闭与整体关闭。
func TestDeliverySLAWatchdogConfigModes(t *testing.T) {
	// cases 覆盖配置模式对扫描行为的影响。
	cases := []struct {
		// name 是用例名称。
		name string
		// raw 是设置原文。
		raw string
		// elapsed 是付款至今分钟数。
		elapsed time.Duration
		// wantCalls 是期望通知次数。
		wantCalls int
	}{
		{"默认配置按默认阈值提醒", `{"default_minutes":100}`, 85 * time.Minute, 1},
		{"账号覆盖拉长阈值后不提醒", `{"default_minutes":100,"per_account":{"cid":200}}`, 85 * time.Minute, 0},
		{"账号覆盖缩短阈值后提醒", `{"default_minutes":200,"per_account":{"cid":50}}`, 45 * time.Minute, 1},
		{"覆盖为零关闭单账号", `{"default_minutes":100,"per_account":{"cid":0}}`, 120 * time.Minute, 0},
		{"默认零且无有效覆盖整体关闭", `{"default_minutes":0}`, 200 * time.Minute, 0},
		{"非法 JSON 整体关闭", `{bad`, 200 * time.Minute, 0},
		{"缺省配置整体关闭", ``, 200 * time.Minute, 0},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// settings、orders、notifier、clock 是该用例独立的替身。
		settings := &slaTestSettings{raw: tc.raw}
		// orders 返回单笔待发货订单。
		orders := &slaTestOrders{orders: []db.Order{slaOrderAtElapsed("order-cfg", "cid", tc.elapsed)}}
		// notifier 记录本用例的通知。
		notifier := &slaTestNotifier{}
		// clock 固定在基准时刻。
		clock := &slaTestClock{current: slaBaseTime}
		// watchdog 是本用例被测对象。
		watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
		watchdog.scanOnce(context.Background())
		if len(notifier.calls) != tc.wantCalls {
			t.Fatalf("%s: 通知次数 = %d, want %d", tc.name, len(notifier.calls), tc.wantCalls)
		}
	}
}

// TestDeliverySLAWatchdogDedupAcrossCycles 验证双周期不重复发送，且临期与超时各自只发一次。
func TestDeliverySLAWatchdogDedupAcrossCycles(t *testing.T) {
	// settings 是 100 分钟默认配置。
	settings := &slaTestSettings{raw: `{"default_minutes":100}`}
	// orders 返回单笔待发货订单，付款时间固定。
	orders := &slaTestOrders{orders: []db.Order{slaOrderAtElapsed("order-dedup", "cid", 85*time.Minute)}}
	// notifier 记录全部通知。
	notifier := &slaTestNotifier{}
	// clock 从基准时刻开始推进。
	clock := &slaTestClock{current: slaBaseTime}
	// watchdog 是被测对象。
	watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
	// ctx 是扫描生命周期上下文。
	ctx := context.Background()

	// 第一、二轮同处临期区间，应只发一条临期提醒。
	watchdog.scanOnce(ctx)
	watchdog.scanOnce(ctx)
	if len(notifier.calls) != 1 {
		t.Fatalf("双周期临期应只提醒一次, got %d 次", len(notifier.calls))
	}

	// 推进到超时区间后应补发一条超时提醒，且不重复临期。
	clock.current = slaBaseTime.Add(105 * time.Minute)
	watchdog.scanOnce(ctx)
	watchdog.scanOnce(ctx)
	if len(notifier.calls) != 2 {
		t.Fatalf("临期与超时合计应提醒两次, got %d 次", len(notifier.calls))
	}
	if notifier.calls[0].eventType != deliverySLAWarningEventType || notifier.calls[1].eventType != deliverySLAOverdueEventType {
		t.Fatalf("提醒顺序应为先临期后超时, got %+v", notifier.calls)
	}
}

// TestDeliverySLAWatchdogSkipsIneligibleOrders 验证不可监控订单被跳过：缺单号、状态、付款、已发货与账号关闭。
func TestDeliverySLAWatchdogSkipsIneligibleOrders(t *testing.T) {
	// base 是构造无效夹具的待发货订单基线。
	base := slaOrderAtElapsed("order-skip", "cid", 120*time.Minute)
	// cases 覆盖各类不可监控订单。
	cases := []struct {
		// name 是用例名称。
		name string
		// order 是输入订单夹具。
		order db.Order
		// raw 是设置原文，允许同时覆盖账号关闭。
		raw string
	}{
		{"缺订单号跳过", db.Order{CookieID: "cid", OrderStatus: "pending_ship", PaidAt: base.PaidAt}, `{"default_minutes":100}`},
		{"非待发货状态跳过", db.Order{OrderID: "order-skip", CookieID: "cid", OrderStatus: "shipped", PaidAt: base.PaidAt}, `{"default_minutes":100}`},
		{"缺付款时间跳过", db.Order{OrderID: "order-skip", CookieID: "cid", OrderStatus: "pending_ship"}, `{"default_minutes":100}`},
		{"已发货跳过", db.Order{OrderID: "order-skip", CookieID: "cid", OrderStatus: "pending_ship", PaidAt: base.PaidAt, ShippedAt: slaBaseTime.Format(time.RFC3339Nano)}, `{"default_minutes":100}`},
		{"付款时间不可解析跳过", db.Order{OrderID: "order-skip", CookieID: "cid", OrderStatus: "pending_ship", PaidAt: "not-a-time"}, `{"default_minutes":100}`},
		{"账号未启用 SLA 跳过", base, `{"default_minutes":100,"per_account":{"cid":0}}`},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// settings、orders、notifier、clock 是该用例独立的替身。
		settings := &slaTestSettings{raw: tc.raw}
		// orders 返回单笔候选订单。
		orders := &slaTestOrders{orders: []db.Order{tc.order}}
		// notifier 记录本用例的通知。
		notifier := &slaTestNotifier{}
		// clock 固定在基准时刻。
		clock := &slaTestClock{current: slaBaseTime}
		// watchdog 是本用例被测对象。
		watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
		watchdog.scanOnce(context.Background())
		if len(notifier.calls) != 0 {
			t.Fatalf("%s: 不应提醒, got %+v", tc.name, notifier.calls)
		}
	}
}

// TestDeliverySLAWatchdogScanDegradesOnErrors 验证设置失败、订单查询失败、缺读取器与 nil 接收者的降级路径。
func TestDeliverySLAWatchdogScanDegradesOnErrors(t *testing.T) {
	// ctx 是扫描生命周期上下文。
	ctx := context.Background()

	// 设置读取失败时不应触发订单扫描。
	settings := &slaTestSettings{err: errors.New("设置读取失败")}
	// orders 在设置失败用例中不应被调用。
	orders := &slaTestOrders{orders: []db.Order{slaOrderAtElapsed("order-err", "cid", 120*time.Minute)}}
	// notifier 记录通知。
	notifier := &slaTestNotifier{}
	// clock 固定在基准时刻。
	clock := &slaTestClock{current: slaBaseTime}
	// watchdog 是被测对象。
	watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
	watchdog.scanOnce(ctx)
	if settings.calls != 1 || orders.calls != 0 || len(notifier.calls) != 0 {
		t.Fatalf("设置失败应跳过本轮, settings=%d orders=%d calls=%d", settings.calls, orders.calls, len(notifier.calls))
	}

	// 订单查询失败时只记日志不发通知。
	failingOrders := &slaTestOrders{err: errors.New("订单查询失败")}
	// settingsOK 是合法配置读取器。
	settingsOK := &slaTestSettings{raw: `{"default_minutes":100}`}
	// watchdog2 是订单查询失败的被测对象。
	watchdog2 := newTestSLAWatchdog(settingsOK, failingOrders, notifier, clock)
	watchdog2.scanOnce(ctx)
	if failingOrders.calls != 1 || len(notifier.calls) != 0 {
		t.Fatalf("订单查询失败不应提醒, orders=%d calls=%d", failingOrders.calls, len(notifier.calls))
	}

	// 缺订单读取器时配置合法也不扫描。
	noReader := newTestSLAWatchdog(settingsOK, &slaTestOrders{}, notifier, clock)
	noReader.readOrders = nil
	noReader.scanOnce(ctx)

	// 缺设置读取器时视为缺省配置，直接关闭。
	noSetting := newTestSLAWatchdog(&slaTestSettings{raw: `{"default_minutes":100}`}, &slaTestOrders{}, notifier, clock)
	noSetting.getSetting = nil
	noSetting.scanOnce(ctx)
	if len(notifier.calls) != 0 {
		t.Fatalf("缺读取器降级不应提醒, calls=%d", len(notifier.calls))
	}

	// nil 接收者扫描必须为空操作。
	var nilWatchdog *deliverySLAWatchdog
	nilWatchdog.scanOnce(ctx)
}

// TestDeliverySLAWatchdogNotificationOmitsSensitiveFields 验证通知正文不含地址、手机与卡密。
func TestDeliverySLAWatchdogNotificationOmitsSensitiveFields(t *testing.T) {
	// order 是带收货敏感字段的待发货订单；正文只允许使用订单号与分钟数。
	order := slaOrderAtElapsed("order-privacy", "cid", 120*time.Minute)
	order.ReceiverName = "张三"
	order.ReceiverPhone = "13800001111"
	order.ReceiverAddr = "某市某区某街道 1 号"
	// settings、orders、notifier、clock 是被测替身。
	settings := &slaTestSettings{raw: `{"default_minutes":100}`}
	// orders 返回带敏感字段的订单。
	orders := &slaTestOrders{orders: []db.Order{order}}
	// notifier 记录通知内容。
	notifier := &slaTestNotifier{}
	// clock 固定在基准时刻。
	clock := &slaTestClock{current: slaBaseTime}
	// watchdog 是被测对象。
	watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
	watchdog.scanOnce(context.Background())
	if len(notifier.calls) != 1 {
		t.Fatalf("应发送一次超时提醒, got %d", len(notifier.calls))
	}
	// call 是唯一通知记录。
	call := notifier.calls[0]
	// sensitive 是禁止出现在通知中的敏感片段。
	sensitive := []string{order.ReceiverName, order.ReceiverPhone, order.ReceiverAddr, "卡密", "card"}
	// fragment 表示当前遍历过程中的敏感片段。
	for _, fragment := range sensitive {
		if strings.Contains(call.title, fragment) || strings.Contains(call.body, fragment) {
			t.Fatalf("通知包含敏感片段 %q: title=%s body=%s", fragment, call.title, call.body)
		}
	}
}

// TestDeliverySLAWatchdogStartStopLifecycle 验证 Start/Stop 幂等、nil 防护与重复启动不派生第二协程。
func TestDeliverySLAWatchdogStartStopLifecycle(t *testing.T) {
	// ctx 是调度器生命周期上下文。
	ctx := context.Background()

	// nil 接收者的 Start 与 Stop 必须为空操作。
	var nilWatchdog *deliverySLAWatchdog
	nilWatchdog.Start(ctx)
	nilWatchdog.Stop()

	// nil Context 拒绝启动，避免创建无法回收的协程。
	settings := &slaTestSettings{raw: `{"default_minutes":100}`}
	// scanned 是扫描信号通道，缓冲保证不阻塞扫描协程。
	scanned := make(chan struct{}, 8)
	// orders 返回可提醒订单并广播扫描信号。
	orders := &slaTestOrders{
		orders:  []db.Order{slaOrderAtElapsed("order-life", "cid", 120*time.Minute)},
		scanned: scanned,
	}
	// notifier 记录通知。
	notifier := &slaTestNotifier{}
	// clock 固定在基准时刻。
	clock := &slaTestClock{current: slaBaseTime}
	// watchdog 是被测对象；周期设为 1 小时，仅启动首轮会扫描，便于证明幂等。
	watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
	watchdog.period = time.Hour
	// 必须显式传入 nil 才能覆盖 Start 的防御分支，故局部豁免 staticcheck 的 nil Context 提示。
	watchdog.Start(nil) //nolint:staticcheck
	if watchdog.running {
		t.Fatal("nil Context 不应启动扫描协程")
	}

	// 重复 Start 只应派生一个扫描协程：只出现一轮扫描信号。
	watchdog.Start(ctx)
	watchdog.Start(ctx)
	// firstSignal 等待首轮扫描信号到达。
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("启动后应立即完成首轮扫描")
	}
	// 短暂等待后不应出现第二轮信号，证明重复 Start 被忽略。
	select {
	case <-scanned:
		t.Fatal("重复 Start 不应派生第二个扫描协程")
	case <-time.After(30 * time.Millisecond):
	}

	// 重复 Stop 幂等：第一次等待退出，第二次空操作返回。
	watchdog.Stop()
	watchdog.Stop()
	if watchdog.running {
		t.Fatal("Stop 后不应仍处于运行状态")
	}

	// 未启动时 Stop 空操作。
	idle := newTestSLAWatchdog(settings, &slaTestOrders{}, notifier, clock)
	idle.Stop()

	// 停止后可再次启动，验证生命周期可重入。
	watchdog.Start(ctx)
	// restartSignal 等待重启后的首轮扫描信号。
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("重启后应再次完成首轮扫描")
	}
	watchdog.Stop()
}

// TestDeliverySLAWatchdogRunLoopTicks 验证周期驱动分支真实触发扫描，并可在运行中关停。
func TestDeliverySLAWatchdogRunLoopTicks(t *testing.T) {
	// settings 是合法配置读取器。
	settings := &slaTestSettings{raw: `{"default_minutes":100}`}
	// scanned 是扫描信号通道。
	scanned := make(chan struct{}, 8)
	// orders 返回可提醒订单并广播扫描信号。
	orders := &slaTestOrders{
		orders:  []db.Order{slaOrderAtElapsed("order-tick", "cid", 120*time.Minute)},
		scanned: scanned,
	}
	// notifier 记录通知。
	notifier := &slaTestNotifier{}
	// clock 固定在基准时刻。
	clock := &slaTestClock{current: slaBaseTime}
	// watchdog 使用 5ms 周期，保证测试内能走到 ticker 分支。
	watchdog := newTestSLAWatchdog(settings, orders, notifier, clock)
	watchdog.period = 5 * time.Millisecond
	// ctx 是调度器生命周期上下文。
	ctx := context.Background()
	watchdog.Start(ctx)
	// firstSignal 等待首轮扫描信号。
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("启动后应立即完成首轮扫描")
	}
	// tickSignal 等待至少一次周期扫描信号，覆盖 ticker 分支。
	select {
	case <-scanned:
	case <-time.After(time.Second):
		t.Fatal("周期驱动应继续触发扫描")
	}
	watchdog.Stop()
	if len(notifier.calls) != 1 {
		t.Fatalf("去重后多轮扫描应只提醒一次, got %d", len(notifier.calls))
	}
}

// TestDeliverySLAWatchdogEndToEndWithStore 用真实仓储走通配置读取、候选扫描、过滤与通知全链路。
func TestDeliverySLAWatchdogEndToEndWithStore(t *testing.T) {
	// store、cleanup 保存真实 SQLite 仓储及清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是扫描生命周期上下文。
	ctx := context.Background()
	// admin 是夹具内置的管理员账号。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、ruleErr 保存启用的 order_paid 规则写入结果；商品留空表示匹配全部订单。
	ruleID, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", Name: "sla-e2e-rule", TriggerType: TriggerOrderPaid, Enabled: true,
	})
	if ruleErr != nil || ruleID <= 0 {
		t.Fatalf("写入规则夹具失败: %v", ruleErr)
	}
	// upsertErr 保存待发货订单夹具写入错误。
	if upsertErr := store.Orders.Upsert(ctx, "sla-e2e-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", OrderStatus: "pending_ship",
		ReceiverName: "李四", ReceiverPhone: "13900002222", ReceiverAddr: "某省某市某路 2 号",
	}); upsertErr != nil {
		t.Fatalf("写入订单夹具失败: %v", upsertErr)
	}
	// paidAt 是可控的付款时刻，直接写库保证阈值确定。
	paidAt := slaBaseTime.Add(-85 * time.Minute)
	// updateErr 保存付款时间写入错误。
	if _, updateErr := store.DB.ExecContext(ctx, `UPDATE orders SET paid_at=? WHERE order_id=?`,
		paidAt.Format(time.RFC3339Nano), "sla-e2e-order"); updateErr != nil {
		t.Fatalf("写入付款时间失败: %v", updateErr)
	}
	// setErr 保存发货 SLA 配置写入错误。
	if setErr := store.Settings.Set(ctx, deliverySLASettingKey, `{"default_minutes":100}`); setErr != nil {
		t.Fatalf("写入 SLA 配置失败: %v", setErr)
	}
	// notifier 记录全链路通知。
	notifier := &slaTestNotifier{}
	// watchdog 用真实仓储构造，走默认设置读取与默认候选扫描。
	watchdog := newDeliverySLAWatchdog(store, notifier, silentTestLogger())
	watchdog.now = func() time.Time { return slaBaseTime }
	watchdog.scanOnce(ctx)
	if len(notifier.calls) != 1 {
		t.Fatalf("全链路应发送一次临期提醒, got %+v", notifier.calls)
	}
	// call 是唯一通知记录。
	call := notifier.calls[0]
	if call.eventType != deliverySLAWarningEventType || call.cookieID != "cid" || !strings.Contains(call.title, "sla-e2e-order") {
		t.Fatalf("全链路通知参数不符: %+v", call)
	}
	if !strings.Contains(call.title, "剩余 15 分钟") {
		t.Fatalf("全链路标题应含剩余分钟: %s", call.title)
	}
	// sensitive 是禁止出现在通知中的敏感片段。
	sensitive := []string{"李四", "13900002222", "某省某市某路 2 号"}
	// fragment 表示当前遍历过程中的敏感片段。
	for _, fragment := range sensitive {
		if strings.Contains(call.title, fragment) || strings.Contains(call.body, fragment) {
			t.Fatalf("全链路通知包含敏感片段 %q", fragment)
		}
	}
}
