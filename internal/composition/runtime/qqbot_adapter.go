package runtime

import (
	"context"
	"crypto/sha256"
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
	opschatapp "xianyu-go/internal/application/opschat"
	"xianyu-go/internal/capability"
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
	// OpsChat 是运营对话应用服务，用于记录入站/出站消息；nil 时只执行命令不留痕。
	OpsChat *opschatapp.Service
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

// qqAuthorizer 依据用户级设置判断入站命令是否对当前发送者开放。
type qqAuthorizer struct {
	// store 提供用户级设置读取。
	store *db.Store
	// userID 是网关所属本地用户，决定开关与白名单归属。
	userID int64
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

// readSetting 读取用户级设置并在失败时记录告警。
func (a qqAuthorizer) readSetting(ctx context.Context, key string) (string, error) {
	if a.store == nil || a.store.UserSettings == nil {
		return "", fmt.Errorf("用户设置仓储未装配")
	}
	// raw 是设置原文。
	raw, err := a.store.UserSettings.GetForUser(ctx, a.userID, key)
	if err != nil && a.logger != nil {
		a.logger.Warn("QQ 入站命令读取设置失败", "key", key, "user_id", a.userID, "err", err)
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
	// userID 是命令使用的固定本地用户身份；解析失败时不得启动网关。
	userID, err := qqIdentityResolver{store: dependencies.Store}.AdminUserID(ctx)
	if err != nil {
		if dependencies.Logger != nil {
			dependencies.Logger.Warn("QQ 入站命令未启用：缺少管理员身份", "err", err)
		}
		return nil
	}
	// authorizer 依据该用户的设置决定是否开放命令。
	authorizer := qqAuthorizer{store: dependencies.Store, userID: userID, logger: dependencies.Logger}
	if !authorizer.Enabled(ctx) {
		return nil
	}
	// appID 是机器人接入标识；缺失时不得启动网关。
	appID, _ := dependencies.Store.UserSettings.GetForUser(ctx, userID, settingQQBotAppID)
	// appSecret 是机器人接入密钥，属敏感键必须带审计读取，禁止写入日志。
	appSecret, secretErr := dependencies.Store.ReadSensitiveUserSetting(ctx, userID, settingQQBotAppSecret, "settings.use", "qqbot")
	if strings.TrimSpace(appID) == "" || secretErr != nil || strings.TrimSpace(appSecret) == "" {
		if dependencies.Logger != nil {
			dependencies.Logger.Warn("QQ 入站命令未启用：机器人凭据不完整")
		}
		return nil
	}
	// catalog 是平台内置能力目录；构造失败属声明自相矛盾的编程错误。
	catalog, catalogErr := capability.PlatformCatalog()
	if catalogErr != nil {
		// 目录构造失败时不启动网关：未受策略管辖的命令一律不得执行，静默放行比不可用更危险。
		if dependencies.Logger != nil {
			dependencies.Logger.Error("QQ 入站命令未启用：平台能力目录构造失败", "err", catalogErr)
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
		qqbot.NewCatalogPolicy(catalog),
	)
	service.SetClock(clock)
	// gateway 是装配完成的入站网关。
	gateway := qqbot.NewGateway(appID, appSecret, service)
	// 运营对话服务已装配时注入留痕端口；未装配时保持 nil，命令行为不变。
	if dependencies.OpsChat != nil {
		gateway.SetRecorder(opsChatRecorder{service: dependencies.OpsChat, userID: userID, logger: dependencies.Logger})
	}
	return gateway
}

// addQQBotGatewayComponent 把用户级 QQ 网关管理器登记为生命周期组件。
// 管理器按用户配置启停网关，未配置或关闭的用户不会产生任何外部连接。
func addQQBotGatewayComponent(coordinator *lifecycle.Coordinator, infrastructure RuntimeInfrastructure, ports composition.TransportPorts, ping func(context.Context) error) error {
	if coordinator == nil || infrastructure.Store == nil {
		return nil
	}
	// ctx 是本次装配使用的进程生命周期上下文。
	ctx := coordinator.Context()
	if ctx == nil {
		return nil
	}
	// dependencies 是每个用户网关共享的窄依赖集合。
	dependencies := QQBotDependencies{
		Store: infrastructure.Store, Analytics: ports.Analytics, Summaries: ports.AccountSummaries,
		Chat: ports.Chat, AccountRuntime: ports.AccountRuntime, Issues: ports.AutomationIssues,
		DatabaseHealth: ping, Logger: infrastructure.Logger, OpsChat: ports.OpsChat,
	}
	// manager 是用户级 QQ 网关的唯一拥有者。
	manager := qqbot.NewManager(
		qqUserConfigSource{store: infrastructure.Store},
		/* build 按用户配置构造独立网关。 */ func(ctx context.Context, cfg qqbot.ManagedUserConfig) (*qqbot.Gateway, error) {
			return buildQQBotGatewayForUser(ctx, cfg, dependencies)
		},
		infrastructure.Logger,
		15*time.Second,
	)
	if ports.Settings != nil {
		ports.Settings.SetUserIntegrationChangeListener(qqManagerListener{manager: manager})
	}
	// addErr 是生命周期组件登记失败原因。
	if addErr := coordinator.Add(lifecycle.NamedComponent{Name: "qqbot-manager", Component: lifecycle.FuncComponent{
		StartFunc: manager.Start,
		CloseFunc: /* 停止所有用户网关并等待运行协程退出。 */ func(context.Context) error {
			manager.Stop()
			return nil
		},
	}}); addErr != nil {
		return fmt.Errorf("登记 QQ 用户网关管理器失败: %w", addErr)
	}
	return nil
}

// fixedIdentity 把网关所属用户作为命令执行身份，避免回落到全局管理员。
type fixedIdentity int64

// AdminUserID 返回网关所属用户标识。
func (f fixedIdentity) AdminUserID(context.Context) (int64, error) {
	if f <= 0 {
		return 0, fmt.Errorf("QQ 网关缺少用户身份")
	}
	return int64(f), nil
}

// buildQQBotGatewayForUser 为单个本地用户构造独立 QQ 网关。
func buildQQBotGatewayForUser(ctx context.Context, cfg qqbot.ManagedUserConfig, dependencies QQBotDependencies) (*qqbot.Gateway, error) {
	if cfg.UserID <= 0 || strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("QQ 用户网关配置不完整")
	}
	// catalog 是平台内置能力目录；构造失败时不得启动网关。
	catalog, catalogErr := capability.PlatformCatalog()
	if catalogErr != nil {
		return nil, fmt.Errorf("平台能力目录构造失败: %w", catalogErr)
	}
	// authorizer 依据该用户的开关与白名单放行命令。
	authorizer := qqAuthorizer{store: dependencies.Store, userID: cfg.UserID, logger: dependencies.Logger}
	// service 是该用户独立命令服务。
	service := qqbot.NewService(
		qqSalesReader{analytics: dependencies.Analytics, summaries: dependencies.Summaries, clock: dependencies.Clock},
		qqHealthReader{store: dependencies.Store, summaries: dependencies.Summaries, runtime: dependencies.AccountRuntime, issues: dependencies.Issues, ping: dependencies.DatabaseHealth, clock: dependencies.Clock},
		qqChatReader{chat: dependencies.Chat, summaries: dependencies.Summaries},
		fixedIdentity(cfg.UserID),
		authorizer,
		qqbot.NewCatalogPolicy(catalog),
	)
	service.SetClock(dependencies.Clock)
	// gateway 是该用户独立入站网关。
	gateway := qqbot.NewGateway(cfg.AppID, cfg.AppSecret, service)
	if dependencies.OpsChat != nil {
		gateway.SetRecorder(opsChatRecorder{service: dependencies.OpsChat, userID: cfg.UserID, logger: dependencies.Logger})
	}
	return gateway, nil
}

// qqManagerListener 在用户集成配置写入后触发网关对齐。
type qqManagerListener struct {
	// manager 是用户网关管理器。
	manager *qqbot.Manager
}

// OnUserIntegrationChanged 只对 QQ 相关键触发对齐，避免无关设置写入重建网关。
func (l qqManagerListener) OnUserIntegrationChanged(ctx context.Context, _ int64, keys []string) {
	if l.manager == nil {
		return
	}
	// key 是当前待判定的设置键。
	for _, key := range keys {
		if strings.HasPrefix(key, "qqbot.") {
			// err 表示 QQ 网关对齐失败。
			if err := l.manager.Reconcile(ctx); err != nil {
				// 对齐失败由管理器的定期兜底重试，不向设置写入方返回错误。
				_ = err
			}
			return
		}
	}
}


// qqUserConfigSource 从用户级设置读取所有应运行的 QQ 网关期望状态。
type qqUserConfigSource struct {
	// store 提供用户列表与用户级设置读取。
	store *db.Store
}

// 编译期断言：配置源必须满足 qqbot 管理器的窄端口。
var _ qqbot.ConfigSource = qqUserConfigSource{}

// ListManagedUsers 返回已启用、凭据完整且白名单非空的用户网关配置。
func (s qqUserConfigSource) ListManagedUsers(ctx context.Context) ([]qqbot.ManagedUserConfig, error) {
	if s.store == nil || s.store.DB == nil || s.store.UserSettings == nil {
		return nil, fmt.Errorf("QQ 用户配置源未初始化")
	}
	// activeExpr 按方言归一用户启用状态。
	activeExpr := "COALESCE(is_active,1) <> 0"
	if s.store.Dialect == db.DialectPostgres {
		activeExpr = "COALESCE(is_active,TRUE) = TRUE"
	}
	// rows 是全部启用用户 ID。
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id FROM users WHERE `+activeExpr+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// userIDs 保存当前启用用户。
	userIDs := make([]int64, 0)
	for rows.Next() {
		// id 是当前启用用户主键。
		var id int64
		// err 表示扫描当前用户主键失败。
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, id)
	}
	// err 表示遍历用户结果集失败。
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// configs 保存满足条件的用户网关配置。
	configs := make([]qqbot.ManagedUserConfig, 0, len(userIDs))
	// userID 是当前待检查的启用用户。
	for _, userID := range userIDs {
		// enabled 是该用户的入站命令总开关。
		enabled, readErr := s.store.UserSettings.GetForUser(ctx, userID, settingQQBotCommandsEnabled)
		if readErr != nil || !isTruthySetting(enabled) {
			continue
		}
		// appID 是机器人接入标识；缺失时跳过该用户。
		appID, appIDErr := s.store.UserSettings.GetForUser(ctx, userID, settingQQBotAppID)
		if appIDErr != nil || strings.TrimSpace(appID) == "" {
			continue
		}
		// appSecret 是敏感密钥，只在本方法内短暂存在，禁止写入日志。
		appSecret, secretErr := s.store.ReadSensitiveUserSetting(ctx, userID, settingQQBotAppSecret, "settings.use", "qqbot_manager")
		if secretErr != nil || strings.TrimSpace(appSecret) == "" {
			continue
		}
		// openIDs 是白名单原文；空白名单仍启动网关但拒绝所有发送者，保持既有语义。
		openIDs, _ := s.store.UserSettings.GetForUser(ctx, userID, settingQQBotCommandOpenIDs)
		// fingerprint 只保存哈希，避免把密钥写入可观察状态。
		fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(appID)+"\x00"+strings.TrimSpace(appSecret)+"\x00"+openIDs)))
		configs = append(configs, qqbot.ManagedUserConfig{
			UserID: userID, AppID: strings.TrimSpace(appID), AppSecret: strings.TrimSpace(appSecret), Fingerprint: fingerprint,
		})
	}
	return configs, nil
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
