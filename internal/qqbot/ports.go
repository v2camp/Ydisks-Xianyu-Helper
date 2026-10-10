// Package qqbot 提供 QQ 机器人入站命令的应用服务：解析管理员在 QQ 里发出的只读命令，
// 读取系统数据并把结果渲染成单条中文文本，由入站传输层被动回复。
//
// 边界：
//   - 本包只依赖下方声明的窄端口与 internal/capability 的决策类型，不直接依赖 application 或 db，
//     适配器在 composition 层完成；
//   - 命令执行前必须先经策略求值放行；未装配求值器时一律不执行（fail-closed），
//     判定规则本身不在本包，由能力内核给出；
//   - 命令一律只读，不得产生任何写库或对外副作用；
//   - 命令格式化是纯函数，时间由调用方注入，便于聚焦测试。
package qqbot

import "context"

// AccountSales 是单个账号在统计区间内的销量汇总。
type AccountSales struct {
	// AccountID 是账号的平台标识（cookie id）。
	AccountID string
	// AccountName 是账号的展示名称；为空时展示标识前缀。
	AccountName string
	// Orders 是该账号的有效订单数。
	Orders int
	// AmountFen 是该账号有效订单金额之和，单位为分。
	AmountFen int64
}

// SalesSnapshot 是「销量」命令所需的只读数据快照。
type SalesSnapshot struct {
	// Date 是统计日期，格式为 YYYY-MM-DD，取自服务器本地时区。
	Date string
	// Accounts 是按金额降序排列的账号销量；无账号有单时为空切片。
	Accounts []AccountSales
	// TotalOrders 是全部账号的有效订单数合计。
	TotalOrders int
	// AmountFen 是全部账号有效订单金额之和，单位为分。
	AmountFen int64
}

// AccountHealth 是单个账号的运行时健康状态。
type AccountHealth struct {
	// AccountID 是账号的平台标识。
	AccountID string
	// AccountName 是账号的展示名称。
	AccountName string
	// State 是运行时状态，例如 online、error、auth_expired。
	State string
	// Connected 表示长连接当前是否在线。
	Connected bool
	// Failures 是当前连续失败次数。
	Failures int
}

// HealthSnapshot 是「健康」命令所需的四维只读快照。
type HealthSnapshot struct {
	// DatabaseOK 表示数据库连通性探测是否通过。
	DatabaseOK bool
	// Accounts 是账号运行时健康列表。
	Accounts []AccountHealth
	// PendingIssues 是待人工处理的自动化异常数量。
	PendingIssues int
	// PendingDeferred 是死信队列中的延迟任务数量。
	PendingDeferred int
	// SilenceMinutes 是距最近一次业务活动的分钟数；-1 表示无活动记录。
	SilenceMinutes int64
	// SilenceThreshold 是业务静默告警阈值分钟数；0 表示看门狗关闭。
	SilenceThreshold int64
}

// ChatTurn 是会话中的一条发言，用于审查机器人回复是否合理。
type ChatTurn struct {
	// Direction 是消息方向：incoming 表示对端（买家），outgoing 表示本机（机器人）。
	Direction string
	// Content 是消息文本内容，已由适配器脱敏裁剪。
	Content string
	// SentAt 是消息发送时间的 Unix 毫秒时间戳。
	SentAt int64
}

// ChatBrief 是一个会话的摘要与近期发言片段。
type ChatBrief struct {
	// ChatID 是平台会话标识。
	ChatID string
	// PeerName 是对端展示名称。
	PeerName string
	// ItemTitle 是会话关联商品标题。
	ItemTitle string
	// Turns 是按时间正序排列的近期发言。
	Turns []ChatTurn
}

// AccountChats 是单个账号下的近期会话集合。
type AccountChats struct {
	// AccountID 是账号的平台标识。
	AccountID string
	// AccountName 是账号的展示名称。
	AccountName string
	// Chats 是该账号下按最近消息时间倒序排列的会话。
	Chats []ChatBrief
}

// ChatDigest 是「会话」命令所需的只读数据快照。
type ChatDigest struct {
	// Accounts 是按账号分组的近期会话；无账号或无会话时为空切片。
	Accounts []AccountChats
}

// SalesReader 提供「销量」命令所需的当日账号级销量读取能力。
type SalesReader interface {
	// TodaySales 读取服务器本地时区当日、按账号分组的有效销量；adminUserID 用于数据隔离。
	TodaySales(ctx context.Context, adminUserID int64) (SalesSnapshot, error)
}

// HealthReader 提供「健康」命令所需的系统健康快照读取能力。
type HealthReader interface {
	// Snapshot 读取数据库、账号、交易与业务静默四个维度的健康状态。
	Snapshot(ctx context.Context, adminUserID int64) (HealthSnapshot, error)
}

// ChatReader 提供「会话」命令所需的分组会话提取能力。
type ChatReader interface {
	// RecentChats 按账号分组提取近期会话；perAccount 控制每个账号返回的会话数。
	RecentChats(ctx context.Context, adminUserID int64, perAccount int) (ChatDigest, error)
}

// IdentityResolver 解析入站命令使用的固定管理员身份，与 MCP 的 AdminUserID 同层。
type IdentityResolver interface {
	// AdminUserID 返回用于数据隔离的管理员用户标识；不存在时返回错误。
	AdminUserID(ctx context.Context) (int64, error)
}

// Authorizer 判断入站命令是否对当前发送者开放。
type Authorizer interface {
	// Enabled 表示入站命令总开关是否打开；关闭时传输层不应启动网关。
	Enabled(ctx context.Context) bool
	// Allowed 表示指定 openid 是否在命令白名单内。
	Allowed(ctx context.Context, openID string) bool
}
