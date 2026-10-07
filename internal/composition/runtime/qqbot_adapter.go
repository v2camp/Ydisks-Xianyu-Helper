package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	accountapp "xianyu-go/internal/application/account"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/application/lifecycle"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/db"
	"xianyu-go/internal/money"
	qqbot "xianyu-go/internal/qqbot"
)

const (
	// settingQQBotCommandsEnabled 是入站命令总开关的系统设置键。
	settingQQBotCommandsEnabled = "qqbot.commands_enabled"
	// settingQQBotCommandOpenIDs 是入站命令发送者白名单的系统设置键。
	settingQQBotCommandOpenIDs = "qqbot.command_openids"
	// settingQQBotAppID 是 QQ 连接器机器人的 AppID 设置键。
	settingQQBotAppID = "qqbot.app_id"
	// settingQQBotAppSecret 是 QQ 连接器机器人的 AppSecret 设置键，属敏感键只可带审计读取。
	settingQQBotAppSecret = "qqbot.app_secret"
	// settingSilenceAlertMinutes 是业务静默告警阈值设置键，与健康看门狗同源。
	settingSilenceAlertMinutes = "silence_alert_minutes"
)

const (
	// qqSalesOrderLimit 是「销量」命令单次扫描的订单明细上限，超出部分不再计入。
	qqSalesOrderLimit = 500
	// qqChatTurnLimit 是「会话」命令单个会话提取的消息条数。
	qqChatTurnLimit = 6
	// qqChatPerAccountMax 是「会话」命令每个账号允许的会话数上限。
	qqChatPerAccountMax = 10
)

// QQBotDependencies 是装配 QQ 入站命令所需的窄依赖集合，全部由组合根注入。
type QQBotDependencies struct {
	// Store 提供系统设置、管理员身份与业务活动时间戳的只读访问。
	Store *db.Store
	// Analytics 提供订单分析能力，用于「销量」命令。
	Analytics *analyticsapp.Service
	// Summaries 提供账号摘要与所有权列表，用于账号展示名与分组。
	Summaries *accountapp.SummaryService
	// Chat 提供聊天会话与消息读取，用于「会话」命令。
	Chat *chatapp.Service
	// AccountRuntime 提供账号运行时状态，用于「健康」命令的账号维度。
	AccountRuntime *accountapp.RuntimeService
	// Issues 提供自动化异常列表，用于「健康」命令的交易维度。
	Issues *automationapp.IssueService
	// DatabaseHealth 探测数据库连通性；nil 时健康快照的数据库维度记为异常。
	DatabaseHealth func(context.Context) error
	// Logger 记录入站命令装配与运行期告警。
	Logger *slog.Logger
	// Clock 返回当前时刻，便于测试固定时间。
	Clock func() time.Time
}

// qqSalesReader 把订单分析服务适配为 QQ 入站命令的当日销量端口。
type qqSalesReader struct {
	// analytics 是订单分析应用服务。
	analytics *analyticsapp.Service
	// summaries 提供账号展示名。
	summaries *accountapp.SummaryService
	// clock 返回当前时刻。
	clock func() time.Time
}

// TodaySales 读取服务器本地时区当日、按账号分组的有效销量。
func (r qqSalesReader) TodaySales(ctx context.Context, adminUserID int64) (qqbot.SalesSnapshot, error) {
	if r.analytics == nil {
		return qqbot.SalesSnapshot{}, fmt.Errorf("订单分析服务未装配")
	}
	// now 是本次统计的当前时刻，决定「今日」的日期边界。
	now := currentClockTime(r.clock)
	// date 是服务器本地时区的当日日期文本。
	date := now.Format("2006-01-02")
	// query 是按本地时区收口到当日的分析查询条件。
	query := analyticsapp.Query{UserID: adminUserID, StartDate: date, EndDate: date, Location: now.Location()}
	// page 是当日有效订单明细；queryErr 表示查询失败。
	page, queryErr := r.analytics.ValidOrders(ctx, query, 1, qqSalesOrderLimit)
	if queryErr != nil {
		return qqbot.SalesSnapshot{}, fmt.Errorf("查询当日销量失败: %w", queryErr)
	}
	// names 是账号标识到展示名的映射。
	names := r.accountNames(ctx, adminUserID)
	// grouped 是按账号标识聚合的销量，键为账号标识。
	grouped := make(map[string]*qqbot.AccountSales)
	// totalFen 是全部账号金额之和，单位为分。
	var totalFen int64
	// order 是当前遍历到的有效订单。
	for _, order := range page.Orders {
		// amountFen 是订单金额换算后的分值；解析失败按 0 计入，不阻断统计。
		amountFen, parseErr := money.ParseYuanToCents(order.Amount)
		if parseErr != nil {
			amountFen = 0
		}
		// bucket 是当前账号的聚合桶，首次出现时创建。
		bucket, exists := grouped[order.CookieID]
		if !exists {
			bucket = &qqbot.AccountSales{AccountID: order.CookieID, AccountName: names[order.CookieID]}
			grouped[order.CookieID] = bucket
		}
		bucket.Orders++
		bucket.AmountFen += amountFen
		totalFen += amountFen
	}
	// accounts 是聚合后的账号销量列表。
	accounts := make([]qqbot.AccountSales, 0, len(grouped))
	// bucket 是当前待收集的账号聚合结果。
	for _, bucket := range grouped {
		accounts = append(accounts, *bucket)
	}
	// 按金额降序排列，金额相同时按账号标识升序保证输出稳定。
	sort.Slice(accounts /* 排序比较器：金额降序、标识升序。 */, func(i, j int) bool {
		if accounts[i].AmountFen != accounts[j].AmountFen {
			return accounts[i].AmountFen > accounts[j].AmountFen
		}
		return accounts[i].AccountID < accounts[j].AccountID
	})
	return qqbot.SalesSnapshot{Date: date, Accounts: accounts, TotalOrders: len(page.Orders), AmountFen: totalFen}, nil
}

// accountNames 读取账号标识到展示名的映射；查询失败时返回空映射，不影响销量统计。
func (r qqSalesReader) accountNames(ctx context.Context, adminUserID int64) map[string]string {
	// names 是账号标识到展示名的映射。
	names := make(map[string]string)
	if r.summaries == nil {
		return names
	}
	// summaries 是当前管理员拥有的账号摘要列表。
	summaries, err := r.summaries.ListSummaries(ctx, adminUserID)
	if err != nil {
		return names
	}
	// summary 是当前遍历到的账号摘要。
	for _, summary := range summaries {
		names[summary.ID] = firstNonBlank(summary.Remark, summary.Nickname)
	}
	return names
}

// qqHealthReader 把运行时状态、自动化异常与业务活动适配为健康快照端口。
type qqHealthReader struct {
	// store 提供业务活动时间戳与静默阈值读取。
	store *db.Store
	// summaries 提供账号展示名。
	summaries *accountapp.SummaryService
	// runtime 提供账号运行时状态。
	runtime *accountapp.RuntimeService
	// issues 提供自动化异常列表。
	issues *automationapp.IssueService
	// ping 探测数据库连通性。
	ping func(context.Context) error
	// clock 返回当前时刻。
	clock func() time.Time
}

// Snapshot 读取数据库、账号、交易与业务静默四个维度的健康状态。
func (r qqHealthReader) Snapshot(ctx context.Context, adminUserID int64) (qqbot.HealthSnapshot, error) {
	// snapshot 是待填充的健康快照。
	var snapshot qqbot.HealthSnapshot
	// pingErr 为空表示数据库连通性探测通过。
	var pingErr error
	if r.ping != nil {
		pingErr = r.ping(ctx)
	}
	snapshot.DatabaseOK = pingErr == nil
	// names 是账号标识到展示名的映射。
	names := r.healthAccountNames(ctx, adminUserID)
	// account 是当前遍历到的账号运行时状态。
	for accountID, status := range r.runtimeStatuses(ctx) {
		snapshot.Accounts = append(snapshot.Accounts, qqbot.AccountHealth{
			AccountID: accountID, AccountName: names[accountID], State: status.State, Connected: status.Connected, Failures: status.Failures,
		})
	}
	// 按账号标识升序排列，保证多次查询输出顺序稳定。
	sort.Slice(snapshot.Accounts /* 排序比较器：按账号标识升序。 */, func(i, j int) bool { return snapshot.Accounts[i].AccountID < snapshot.Accounts[j].AccountID })
	// runs、deferred 分别是待处理异常与死信任务列表。
	runs, deferred, issuesErr := r.issueCounts(ctx, adminUserID)
	snapshot.PendingIssues, snapshot.PendingDeferred = runs, deferred
	// needsIssuesError 表示异常查询失败，必须让命令失败而不是静默报「零异常」。
	needsIssuesError := issuesErr != nil && r.issues != nil
	snapshot.SilenceMinutes, snapshot.SilenceThreshold = r.silence(ctx)
	if needsIssuesError {
		return qqbot.HealthSnapshot{}, fmt.Errorf("查询自动化异常失败: %w", issuesErr)
	}
	return snapshot, nil
}

// runtimeStatuses 读取账号运行时状态；服务未装配时返回空映射。
func (r qqHealthReader) runtimeStatuses(ctx context.Context) map[string]accountapp.RuntimeStatus {
	if r.runtime == nil {
		return nil
	}
	// statuses 是账号运行时状态快照；读取失败时返回空映射。
	statuses, err := r.runtime.RuntimeStatuses(ctx)
	if err != nil {
		return nil
	}
	return statuses
}

// issueCounts 统计待处理异常与死信任务数量。
func (r qqHealthReader) issueCounts(ctx context.Context, adminUserID int64) (int, int, error) {
	if r.issues == nil {
		return 0, 0, nil
	}
	// runs、deferred 分别是待处理异常与死信任务列表。
	runs, deferred, err := r.issues.ListIssues(ctx, adminUserID)
	if err != nil {
		return 0, 0, err
	}
	return len(runs), len(deferred), nil
}

// healthAccountNames 读取账号展示名；查询失败时返回空映射。
func (r qqHealthReader) healthAccountNames(ctx context.Context, adminUserID int64) map[string]string {
	// names 是账号标识到展示名的映射。
	names := make(map[string]string)
	if r.summaries == nil {
		return names
	}
	// summaries 是当前管理员拥有的账号摘要列表。
	summaries, err := r.summaries.ListSummaries(ctx, adminUserID)
	if err != nil {
		return names
	}
	// summary 是当前遍历到的账号摘要。
	for _, summary := range summaries {
		names[summary.ID] = firstNonBlank(summary.Remark, summary.Nickname)
	}
	return names
}

// silence 计算距最近一次业务活动的分钟数与告警阈值；无活动记录时分钟数为 -1。
func (r qqHealthReader) silence(ctx context.Context) (int64, int64) {
	// threshold 是业务静默告警阈值分钟数，读取失败时回退 0 表示关闭。
	threshold := r.silenceThreshold(ctx)
	if r.store == nil || r.store.Analytics == nil {
		return -1, threshold
	}
	// last 是最近一次业务活动时刻；读取失败或零值时按无记录处理。
	last, err := r.store.Analytics.LatestBusinessActivityAt(ctx)
	if err != nil || last.IsZero() {
		return -1, threshold
	}
	// minutes 是距最近业务活动的分钟数，负数按 0 处理。
	minutes := int64(currentClockTime(r.clock).Sub(last).Minutes())
	if minutes < 0 {
		minutes = 0
	}
	return minutes, threshold
}

// silenceThreshold 读取业务静默告警阈值设置；缺失或非法时回退 0 表示看门狗关闭。
func (r qqHealthReader) silenceThreshold(ctx context.Context) int64 {
	if r.store == nil || r.store.Settings == nil {
		return 0
	}
	// raw 是设置原文；读取失败时按未配置处理。
	raw, err := r.store.Settings.Get(ctx, settingSilenceAlertMinutes)
	if err != nil {
		return 0
	}
	// minutes 是解析后的阈值分钟数；非法值回退 0。
	minutes, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if parseErr != nil || minutes < 0 {
		return 0
	}
	return minutes
}

// qqChatReader 把聊天应用服务适配为分组会话提取端口。
type qqChatReader struct {
	// chat 是聊天历史应用服务。
	chat *chatapp.Service
	// summaries 提供账号列表与展示名。
	summaries *accountapp.SummaryService
}

// RecentChats 按账号分组提取近期会话，perAccount 控制每个账号返回的会话数。
func (r qqChatReader) RecentChats(ctx context.Context, adminUserID int64, perAccount int) (qqbot.ChatDigest, error) {
	if r.chat == nil || r.summaries == nil {
		return qqbot.ChatDigest{}, fmt.Errorf("聊天服务未装配")
	}
	// limit 是收敛后的每账号会话数，限制在 1 到上限之间。
	limit := perAccount
	if limit < 1 {
		limit = 1
	}
	if limit > qqChatPerAccountMax {
		limit = qqChatPerAccountMax
	}
	// summaries 是当前管理员拥有的账号摘要列表。
	summaries, err := r.summaries.ListSummaries(ctx, adminUserID)
	if err != nil {
		return qqbot.ChatDigest{}, fmt.Errorf("查询账号列表失败: %w", err)
	}
	// digest 是待填充的分组会话摘要。
	digest := qqbot.ChatDigest{}
	// summary 是当前遍历到的账号摘要。
	for _, summary := range summaries {
		// sessions 是该账号下按最近消息倒序排列的会话。
		sessions, sessionsErr := r.chat.ListSessions(ctx, adminUserID, summary.ID, limit)
		if sessionsErr != nil || len(sessions) == 0 {
			continue
		}
		// group 是当前账号的会话集合。
		group := qqbot.AccountChats{AccountID: summary.ID, AccountName: firstNonBlank(summary.Remark, summary.Nickname)}
		// session 是当前遍历到的会话摘要。
		for _, session := range sessions {
			// page 是该会话的近期消息页。
			page, messagesErr := r.chat.ListStoredMessages(ctx, adminUserID, summary.ID, session.ChatID, 0, qqChatTurnLimit)
			if messagesErr != nil {
				continue
			}
			// brief 是当前会话的摘要与发言片段。
			brief := qqbot.ChatBrief{ChatID: session.ChatID, PeerName: session.PeerName, ItemTitle: session.ItemTitle}
			// message 是当前遍历到的会话消息。
			for _, message := range page.Messages {
				brief.Turns = append(brief.Turns, qqbot.ChatTurn{Direction: message.Direction, Content: message.Content, SentAt: message.SentAt})
			}
			group.Chats = append(group.Chats, brief)
		}
		if len(group.Chats) > 0 {
			digest.Accounts = append(digest.Accounts, group)
		}
	}
	return digest, nil
}

// qqIdentityResolver 把管理员用户表适配为固定管理员身份端口。
type qqIdentityResolver struct {
	// store 提供管理员用户查询。
	store *db.Store
}

// AdminUserID 返回用于数据隔离的管理员用户标识。
func (r qqIdentityResolver) AdminUserID(ctx context.Context) (int64, error) {
	if r.store == nil || r.store.Users == nil {
		return 0, fmt.Errorf("用户仓储未装配")
	}
	// admin 是系统内的管理员账户记录。
	admin, err := r.store.Users.GetAdmin(ctx)
	if err != nil {
		return 0, fmt.Errorf("查询管理员失败: %w", err)
	}
	if admin == nil || admin.ID <= 0 {
		return 0, fmt.Errorf("系统内不存在管理员账户")
	}
	return admin.ID, nil
}

// qqAuthorizer 依据系统设置判断入站命令是否对当前发送者开放。
type qqAuthorizer struct {
	// store 提供系统设置读取。
	store *db.Store
	// logger 记录设置读取失败，避免静默拒绝。
	logger *slog.Logger
}

// Enabled 表示入站命令总开关是否打开。
func (a qqAuthorizer) Enabled(ctx context.Context) bool {
	// raw 是开关设置原文；读取失败时按关闭处理。
	raw, err := a.readSetting(ctx, settingQQBotCommandsEnabled)
	if err != nil {
		return false
	}
	return isTruthySetting(raw)
}

// Allowed 表示指定 openid 是否在命令白名单内；白名单为空时拒绝所有发送者。
func (a qqAuthorizer) Allowed(ctx context.Context, openID string) bool {
	// sender 是去空白后的发送者标识。
	sender := strings.TrimSpace(openID)
	if sender == "" {
		return false
	}
	// raw 是白名单设置原文；读取失败时按空名单处理。
	raw, err := a.readSetting(ctx, settingQQBotCommandOpenIDs)
	if err != nil || strings.TrimSpace(raw) == "" {
		return false
	}
	// entry 是当前遍历到的白名单条目。
	for _, entry := range strings.FieldsFunc(raw /* 分隔函数：逗号、分号与换行均视为分隔符。 */, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }) {
		if strings.TrimSpace(entry) == sender {
			return true
		}
	}
	return false
}

// readSetting 读取系统设置并在失败时记录告警。
func (a qqAuthorizer) readSetting(ctx context.Context, key string) (string, error) {
	if a.store == nil || a.store.Settings == nil {
		return "", fmt.Errorf("系统设置仓储未装配")
	}
	// raw 是设置原文。
	raw, err := a.store.Settings.Get(ctx, key)
	if err != nil && a.logger != nil {
		a.logger.Warn("QQ 入站命令读取设置失败", "key", key, "err", err)
	}
	return raw, err
}

// NewQQBotGateway 装配 QQ 入站命令网关。
// ctx 是装配期读取系统设置使用的进程生命周期上下文，禁止传入裸 Background。
// 返回 nil 表示入站命令未启用或凭据不完整，调用方不得登记其生命周期组件。
func NewQQBotGateway(ctx context.Context, dependencies QQBotDependencies) *qqbot.Gateway {
	if ctx == nil || dependencies.Store == nil {
		return nil
	}
	// clock 是归一化后的时间源。
	clock := dependencies.Clock
	if clock == nil {
		clock = time.Now
	}
	// authorizer 依据系统设置决定是否开放命令。
	authorizer := qqAuthorizer{store: dependencies.Store, logger: dependencies.Logger}
	if !authorizer.Enabled(ctx) {
		return nil
	}
	// userID 是命令使用的固定管理员身份；解析失败时不得启动网关。
	userID, err := qqIdentityResolver{store: dependencies.Store}.AdminUserID(ctx)
	if err != nil {
		if dependencies.Logger != nil {
			dependencies.Logger.Warn("QQ 入站命令未启用：缺少管理员身份", "err", err)
		}
		return nil
	}
	// appID 是机器人接入标识；缺失时不得启动网关。
	appID, _ := dependencies.Store.Settings.Get(ctx, settingQQBotAppID)
	// appSecret 是机器人接入密钥，属敏感键必须带审计读取，禁止写入日志。
	appSecret, secretErr := dependencies.Store.ReadSensitiveSetting(ctx, userID, settingQQBotAppSecret, "settings.use", "qqbot")
	if strings.TrimSpace(appID) == "" || secretErr != nil || strings.TrimSpace(appSecret) == "" {
		if dependencies.Logger != nil {
			dependencies.Logger.Warn("QQ 入站命令未启用：机器人凭据不完整")
		}
		return nil
	}
	// service 是装配完成的入站命令服务。
	service := qqbot.NewService(
		qqSalesReader{analytics: dependencies.Analytics, summaries: dependencies.Summaries, clock: clock},
		qqHealthReader{store: dependencies.Store, summaries: dependencies.Summaries, runtime: dependencies.AccountRuntime, issues: dependencies.Issues, ping: dependencies.DatabaseHealth, clock: clock},
		qqChatReader{chat: dependencies.Chat, summaries: dependencies.Summaries},
		qqIdentityResolver{store: dependencies.Store},
		authorizer,
	)
	service.SetClock(clock)
	return qqbot.NewGateway(appID, appSecret, service)
}

// addQQBotGatewayComponent 在入站命令启用时把 QQ 网关登记为生命周期组件。
// coordinator 提供进程生命周期上下文与组件登记能力；infrastructure 提供数据库与日志；
// ports 是已完成构造的应用服务引用；ping 探测数据库连通性。
// 未启用或凭据不完整时不登记任何组件，返回 nil，保证默认零外部连接。
func addQQBotGatewayComponent(coordinator *lifecycle.Coordinator, infrastructure RuntimeInfrastructure, ports composition.TransportPorts, ping func(context.Context) error) error {
	if coordinator == nil || infrastructure.Store == nil {
		return nil
	}
	// ctx 是本次装配使用的进程生命周期上下文。
	ctx := coordinator.Context()
	if ctx == nil {
		return nil
	}
	// gateway 是装配出的入站网关；nil 表示未启用。
	gateway := NewQQBotGateway(ctx, QQBotDependencies{
		Store: infrastructure.Store, Analytics: ports.Analytics, Summaries: ports.AccountSummaries,
		Chat: ports.Chat, AccountRuntime: ports.AccountRuntime, Issues: ports.AutomationIssues,
		DatabaseHealth: ping, Logger: infrastructure.Logger,
	})
	if gateway == nil {
		return nil
	}
	// addErr 是生命周期组件登记失败原因；网关自带阻塞循环，故用 goroutine 启动。
	if addErr := coordinator.Add(lifecycle.NamedComponent{Name: "qqbot-gateway", Component: lifecycle.FuncComponent{
		StartFunc: /* 网关在生命周期上下文内持续重连运行，直到进程关闭。 */ func(ctx context.Context) error {
			go func() {
				// 单次 Run 退出后由退避守护接管重连；否则一次网络抖动或凭据错误会让入站命令永久失效。
				serveWithRestart(ctx, "QQ 入站网关", gateway, infrastructure.Logger)
			}()
			return nil
		},
		CloseFunc:/* 网关随生命周期上下文取消而退出，无额外资源需要释放。 */ func(context.Context) error { return nil },
	}}); addErr != nil {
		return fmt.Errorf("登记 QQ 入站网关生命周期组件失败: %w", addErr)
	}
	return nil
}

// currentClockTime 返回当前时刻；未注入时间源时回落系统时间。
func currentClockTime(clock func() time.Time) time.Time {
	if clock == nil {
		return time.Now()
	}
	return clock()
}

// firstNonBlank 返回首个去空白后非空的候选值；全部为空时返回空串。
func firstNonBlank(candidates ...string) string {
	// candidate 是当前待判断的候选值。
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

// isTruthySetting 判断设置原文是否表示启用，兼容布尔与旧式 0/1 文本。
func isTruthySetting(raw string) bool {
	// normalized 是去空白并转小写后的设置原文。
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}
