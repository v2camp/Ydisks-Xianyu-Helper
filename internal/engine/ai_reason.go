package engine

// 本文件集中存放 AI 决策日志的原因简码，是 reason 取值的单一权威来源。
// 契约方（internal/db 的 AIDecisionRecord 字段说明）与写入方（engine）都必须引用本文件常量，
// 不得再写字面量：否则「统计口径里少一个取值」这类问题只能靠翻日志才能发现。
const (
	// AIReasonTaken 表示边界判定通过、AI 已接管本次回复。
	// 写入时机：takeover 为 true 且未命中任何否决分支；takeover 为 true。
	AIReasonTaken = "taken"
	// AIReasonNoIntent 表示到达 AI 层但没有任何正向意图命中，属「正常但不回复」。
	// 写入时机：takeover 为 false 且未命中负向词；与故障同形，排查时须先看本值再下结论。
	AIReasonNoIntent = "no-intent"
	// AIReasonNegative 表示命中负向词表（投诉、站外引流、退款动作、转人工等），被否决。
	// 写入时机：negHit 为 true；这是唯一代表「合规与安全拦截」的取值。
	AIReasonNegative = "negative"
	// AIReasonExtraNegative 表示命中商品级额外负向词，优先于全局负向词先判。
	// 写入时机：extraHit 为 true，且优先于 negative 判出，便于区分是商品定制还是全局规则拦下。
	AIReasonExtraNegative = "extra-negative"
	// AIReasonItemSyncRequested 表示准入层发现会话商品不在本地商品库，已异步请求单商品回补。
	// 写入时机：拿到回补执行权、任务启动之前；takeover 固定为 false（本条不回复）。
	AIReasonItemSyncRequested = "item-sync-requested"
	// AIReasonItemSyncFailed 表示单商品回补失败或超时，该商品进入失败负缓存期。
	// 写入时机：回补 goroutine 返回错误之后；负缓存期内不再重复请求平台。
	AIReasonItemSyncFailed = "item-sync-failed"
	// AIReasonMasterOff 表示全局总闸（system_settings.ai_master_enabled）关闭，AI 回复整体不工作。
	// 写入时机：Reply 闸门判定总闸关闭、尚未读取边界配置时；takeover 固定为 false。
	AIReasonMasterOff = "master-off"
	// AIReasonAccountOff 表示账号级开关被显式关闭（ai_reply_settings.ai_enabled = 0）。
	// 写入时机：总闸开启但该账号显式关闭时；takeover 固定为 false。
	AIReasonAccountOff = "account-off"
)
