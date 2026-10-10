// ports.go 汇总平台能力的领域用例契约。
//
// 这些接口是「能力如何执行」的契约：能力内核只回答是否允许，允许之后由消费者调用
// 这里的用例完成动作。契约放在内核而不是各传输层，是因为同一个动作会被多个消费者调用
// ——外部 Harness 经 MCP 工具、运营 Agent 经 QQ 命令、客服 Agent 经工具集——
// 各自定义一份必然导致同名用例出现多个签名，策略与执行随之漂移。
//
// 接口只引用 internal/application/* 的应用模型，实现由 internal/composition 投影提供；
// 本包只声明契约，不实现、不调用、不触达业务数据。
//
// 消费者不在这里的端口才算「本传输层专用」：传输协议对象、鉴权、审计、资源与提示等
// 只服务单一入口的端口，仍由各自的包自行定义。

package capability

import (
	"context"

	accountapp "xianyu-go/internal/application/account"
	adminapp "xianyu-go/internal/application/admin"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	chatapp "xianyu-go/internal/application/chat"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	itemapp "xianyu-go/internal/application/items"
	keywordsapp "xianyu-go/internal/application/keywords"
	notificationsapp "xianyu-go/internal/application/notifications"
	orderapp "xianyu-go/internal/application/orders"
	settingsapp "xianyu-go/internal/application/settings"
)

// AccountPorts 聚合账号域工具所需的账号用例集合。
type AccountPorts interface {
	// 摘要与归属。
	ListAdminSummaries(ctx context.Context) ([]accountapp.AdminAccountSummary, error)
	ListSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error)
	GetOwnedSummary(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error)
	RequireOwnership(ctx context.Context, userID int64, cookieID string) error
	// 本地配置写入。
	UpdateAccountSettings(ctx context.Context, input accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error)
	SetAccountStatus(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.StatusResult, error)
	SetAccountPause(ctx context.Context, userID int64, cookieID string, minutes int) (accountapp.SettingsResult, error)
	GetAccountPause(ctx context.Context, userID int64, cookieID string) (accountapp.PauseState, error)
	SetAutoConfirm(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetAutoConsign(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetAutoBargain(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.SettingsResult, error)
	SetRemark(ctx context.Context, userID int64, cookieID, remark string) (accountapp.SettingsResult, error)
	// 平台触达与运行时。
	QueryLongLogin(ctx context.Context, userID int64, cookieID string) (accountapp.LongLoginResult, error)
	SetLongLogin(ctx context.Context, userID int64, cookieID string, enabled bool) (accountapp.LongLoginResult, error)
	RuntimeStatuses(ctx context.Context) (map[string]accountapp.RuntimeStatus, error)
	RestartAccount(ctx context.Context, cookieID string) error
	RefreshProfile(ctx context.Context, userID int64, cookieID string) (accountapp.ProfileResult, error)
	DeleteAccount(ctx context.Context, userID int64, cookieID string) error
	// 自动评价/擦亮任务。
	GetAccountTaskSettings(ctx context.Context, cookieID string) (automationapp.AccountTaskSettings, error)
	UpdateAccountTaskSettings(ctx context.Context, settings automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error)
	ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]automationapp.AccountTaskRun, error)
	RunAccountTask(ctx context.Context, cookieID, taskType string) (automationapp.TaskSummary, error)
}

// OrderPorts 聚合订单查询、刷新、手动发货与刷新任务用例。
type OrderPorts interface {
	// List 按条件分页查询订单。
	List(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error)
	// Get 读取单个归属订单详情。
	Get(ctx context.Context, userID int64, orderID string) (*orderapp.Order, error)
	// RefreshSingle 向平台刷新单个订单。
	RefreshSingle(ctx context.Context, userID int64, orderID string) (orderapp.SingleRefreshResult, error)
	// Refresh 按账号或状态批量向平台刷新订单。
	Refresh(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshResult, error)
	// ManualShip 执行手动发货；只改本地状态或执行完整发货链路由 shipMode 决定。
	ManualShip(ctx context.Context, request orderapp.ManualShipRequest) (orderapp.ManualShipResult, error)
	// CreateRefreshJob 创建并启动后台批量刷新任务。
	CreateRefreshJob(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshJobStartResult, error)
	// GetRefreshJob 查询刷新任务快照。
	GetRefreshJob(ctx context.Context, userID int64, jobID string) (*orderapp.RefreshJob, error)
	// CancelRefreshJob 取消排队或运行中的刷新任务。
	CancelRefreshJob(ctx context.Context, userID int64, jobID string) (orderapp.RefreshJobCancelResult, error)
}

// AnalyticsPorts 聚合仪表盘与订单分析用例。
type AnalyticsPorts interface {
	// DashboardStats 返回首页非敏感计数；不接受日期范围。
	DashboardStats(ctx context.Context, userID int64) (analyticsapp.DashboardStats, error)
	// OrderAnalytics 返回收益、按日与按状态聚合。
	OrderAnalytics(ctx context.Context, query analyticsapp.Query) (analyticsapp.OrderAnalytics, error)
	// ValidOrders 分页返回参与分析的有效订单明细。
	ValidOrders(ctx context.Context, query analyticsapp.Query, page, pageSize int) (analyticsapp.ValidOrders, error)
}

// IssuePorts 聚合自动化异常与死信延期任务的查询和人工处理用例。
type IssuePorts interface {
	// ListIssues 返回待人工处理的运行异常与死信任务。
	ListIssues(ctx context.Context, userID int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error)
	// ResolveRunIssue 对异常运行执行人工处理动作。
	ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error
	// ResolveDeferredIssue 对死信延期任务执行 retry 或 dismiss。
	ResolveDeferredIssue(ctx context.Context, userID, taskID int64, resolution string) error
}

// ItemPorts 聚合商品货架、同步、发布与批量发布用例。
// 本地商品写入方法的账号归属由调用方经账号归属用例复核后调用。
type ItemPorts interface {
	// 本地商品目录。
	ListItems(ctx context.Context, userID int64, cookieID string) ([]itemapp.CatalogItem, error)
	GetItem(ctx context.Context, cookieID, itemID string) (itemapp.CatalogItem, error)
	CreateItem(ctx context.Context, cookieID string, input itemapp.CatalogWriteInput) error
	UpdateItem(ctx context.Context, cookieID, itemID string, patch itemapp.CatalogPatchInput) error
	DeleteItem(ctx context.Context, cookieID, itemID string) error
	SetItemMultiSpec(ctx context.Context, cookieID, itemID string, enabled bool) error
	SetItemMultiQuantity(ctx context.Context, cookieID, itemID string, enabled bool) error
	// 平台同步与发布。
	SyncItemsAll(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncAllResult, error)
	SyncItemsPage(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncPageResult, error)
	PublishSingle(ctx context.Context, input SinglePublishInput) (itemapp.PublishOutcome, error)
	RecommendCategory(ctx context.Context, userID int64, cookieID, keyword string) (itemapp.BatchPreviewCategory, error)
	// 批量发布预检与管理。
	PreviewBatch(ctx context.Context, input itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error)
	PersistBatch(ctx context.Context, batch itemapp.BatchPreviewPersistenceBatch, rows []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error)
	StartBatch(ctx context.Context, userID int64, batchID string) (string, error)
	ListBatches(ctx context.Context, userID int64, limit int) ([]itemapp.BatchInfo, error)
	GetBatch(ctx context.Context, userID int64, batchID string) (itemapp.BatchDetails, error)
	CancelBatch(ctx context.Context, userID int64, batchID string) (string, error)
	RetryBatch(ctx context.Context, userID int64, batchID string) (string, error)
	DeleteBatch(ctx context.Context, userID int64, batchID string) error
}

// SinglePublishInput 是单商品发布输入；图片只接受公网 URL，由组合层下载并校验后交给应用层。
type SinglePublishInput struct {
	// UserID 是发起发布的用户标识。
	UserID int64
	// CookieID 是发布账号标识。
	CookieID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述。
	Description string
	// PriceCents 是售价（分）。
	PriceCents int64
	// OriginalPriceCents 是原价（分）。
	OriginalPriceCents int64
	// Quantity 是库存数量。
	Quantity int
	// PostageMode 是邮费模式 free/fixed。
	PostageMode string
	// PostageCents 是固定邮费（分）。
	PostageCents int64
	// ImageURLs 是商品图片公网 URL。
	ImageURLs []string
	// CatID 是可选指定闲鱼类目；为空走自动推荐。
	CatID string
}

// CardPorts 聚合卡券库存 CRUD、追加卡密与 API 配置连通性测试用例。
type CardPorts interface {
	// ListCards 返回用户全部卡券组（应用模型含明文，MCP 层负责裁剪）。
	ListCards(ctx context.Context, userID int64) ([]cardsapp.Card, error)
	// GetCard 返回单个归属卡券组。
	GetCard(ctx context.Context, userID, cardID int64) (cardsapp.Card, error)
	// CreateCard 创建卡券组并返回新标识。
	CreateCard(ctx context.Context, userID int64, draft cardsapp.Draft) (int64, error)
	// UpdateCard 更新卡券组；库存正文仅在 draft.DataContentSet 时覆盖。
	UpdateCard(ctx context.Context, userID, cardID int64, draft cardsapp.Draft) error
	// DeleteCard 删除卡券组。
	DeleteCard(ctx context.Context, userID, cardID int64) error
	// AppendCardData 向 data 卡券组追加逐行卡密，返回新增行数。
	AppendCardData(ctx context.Context, userID, cardID int64, content string) (int, error)
	// TestCardAPI 用已保存的完整配置对归属 API 卡券组发起一次受控连通性测试；
	// 完整请求模板由应用层内部读取，不经过 MCP 层，诊断结果只含非敏感字段。
	TestCardAPI(ctx context.Context, userID, cardID int64) (cardsapp.APIRequestTestResult, error)
}

// RulePorts 聚合自动化规则查询、规范化预览与写入用例。
// 规则校验完全由 RuleService 的 Normalize/NormalizeForUpdate 负责，MCP 层不复制任何业务规则。
type RulePorts interface {
	// ListRules 返回用户全部自动化规则。
	ListRules(ctx context.Context, userID int64) ([]automationapp.Rule, error)
	// ListRulesPage 按过滤条件分页返回规则与总数。
	ListRulesPage(ctx context.Context, filter automationapp.RuleFilter) ([]automationapp.Rule, int, error)
	// CountRulesByTrigger 返回按触发类型统计的规则数量。
	CountRulesByTrigger(ctx context.Context, filter automationapp.RuleFilter) (map[string]int, error)
	// NormalizeRule 校验并规范化创建草稿；只返回校验结果，不产生任何写入。
	NormalizeRule(ctx context.Context, userID int64, draft automationapp.RuleDraft) (automationapp.RuleInput, error)
	// NormalizeRuleForUpdate 校验并规范化更新草稿，允许保留规则已引用的停用模板。
	NormalizeRuleForUpdate(ctx context.Context, userID, ruleID int64, draft automationapp.RuleDraft) (automationapp.RuleInput, error)
	// CreateRule 持久化已规范化的规则并返回新标识。
	CreateRule(ctx context.Context, input automationapp.RuleInput) (int64, error)
	// UpdateRule 更新用户拥有的规则。
	UpdateRule(ctx context.Context, userID, ruleID int64, input automationapp.RuleInput) error
	// DeleteRule 删除用户拥有的规则。
	DeleteRule(ctx context.Context, userID, ruleID int64) error
}

// DeliveryTemplatePorts 聚合发货模板 CRUD 用例。
type DeliveryTemplatePorts interface {
	// ListTemplates 返回用户全部发货模板。
	ListTemplates(ctx context.Context, userID int64) ([]deliveryapp.Template, error)
	// GetTemplate 读取单个归属发货模板。
	GetTemplate(ctx context.Context, userID, templateID int64) (deliveryapp.Template, error)
	// CreateTemplate 创建发货模板并返回新标识。
	CreateTemplate(ctx context.Context, userID int64, draft deliveryapp.Draft) (int64, error)
	// UpdateTemplate 更新用户拥有的发货模板。
	UpdateTemplate(ctx context.Context, userID, templateID int64, draft deliveryapp.Draft) error
	// DeleteTemplate 删除用户拥有的发货模板。
	DeleteTemplate(ctx context.Context, userID, templateID int64) error
}

// DefaultReplyPorts 聚合默认回复配置读写与投递记录清理用例。
type DefaultReplyPorts interface {
	// ListDefaultReplies 返回用户全部账号的默认回复配置。
	ListDefaultReplies(ctx context.Context, userID int64) ([]defaultreplyapp.Summary, error)
	// GetDefaultReply 读取指定账号的默认回复配置。
	GetDefaultReply(ctx context.Context, userID int64, cookieID string) (defaultreplyapp.Reply, error)
	// UpsertDefaultReply 保存或覆盖指定账号的默认回复配置。
	UpsertDefaultReply(ctx context.Context, userID int64, cookieID string, reply defaultreplyapp.Reply) error
	// DeleteDefaultReply 删除指定账号的默认回复配置。
	DeleteDefaultReply(ctx context.Context, userID int64, cookieID string) error
	// ClearDefaultReplyRecords 清空指定账号的默认回复投递记录。
	ClearDefaultReplyRecords(ctx context.Context, userID int64, cookieID string) error
}

// KeywordPorts 聚合关键词回复与指定商品回复用例。
type KeywordPorts interface {
	// ListKeywords 返回指定账号的关键词回复列表。
	ListKeywords(ctx context.Context, userID int64, cookieID string) ([]keywordsapp.Keyword, error)
	// AddKeyword 新增一条关键词回复并返回标识。
	AddKeyword(ctx context.Context, userID int64, cookieID string, draft keywordsapp.Draft) (int64, error)
	// ReplaceKeywords 原子替换指定账号的全部关键词回复。
	ReplaceKeywords(ctx context.Context, userID int64, cookieID string, drafts []keywordsapp.Draft) error
	// UpdateKeyword 更新指定标识的关键词回复。
	UpdateKeyword(ctx context.Context, userID int64, cookieID string, keywordID int64, draft keywordsapp.Draft) error
	// DeleteKeywordByID 按标识删除关键词回复。
	DeleteKeywordByID(ctx context.Context, userID int64, cookieID string, keywordID int64) error
	// DeleteKeywordByIndex 按列表零基索引删除关键词回复。
	DeleteKeywordByIndex(ctx context.Context, userID int64, cookieID string, index int) error
	// ListItemReplies 返回用户全部账号的指定商品回复。
	ListItemReplies(ctx context.Context, userID int64) ([]keywordsapp.ItemReply, error)
	// GetItemReply 读取指定账号与商品的回复。
	GetItemReply(ctx context.Context, userID int64, cookieID, itemID string) (keywordsapp.ItemReply, error)
	// SetItemReply 覆盖指定账号与商品的回复。
	SetItemReply(ctx context.Context, userID int64, cookieID, itemID, content string) error
	// DeleteItemReply 删除指定账号与商品的回复。
	DeleteItemReply(ctx context.Context, userID int64, cookieID, itemID string) error
}

// ChatPorts 聚合会话、消息、发送、快捷回复、买家备注与聊天商品用例。
// 平台回显、结果不确定与代次隔离语义完全由 chat 应用服务负责，MCP 层只做透传与脱敏。
type ChatPorts interface {
	// ListSessionPage 按用户归属读取账号本地会话的稳定键集分页页面。
	ListSessionPage(ctx context.Context, userID int64, accountID string, cursor *chatapp.SessionCursor, limit int) (chatapp.SessionPage, error)
	// FindSession 读取指定账号下的单个归属会话；找不到时返回零值会话。
	FindSession(ctx context.Context, userID int64, accountID, chatID string) (chatapp.Session, error)
	// ListStoredMessages 查询本地已落库的聊天消息。
	ListStoredMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) (chatapp.Page, error)
	// RefreshConversations 向平台拉取并落库账号联系人页。
	RefreshConversations(ctx context.Context, accountID string, cursor int64, limit int) (chatapp.ConversationPage, error)
	// RefreshHistory 向平台拉取并落库指定会话消息页。
	RefreshHistory(ctx context.Context, accountID, chatID string, cursor int64, limit int, session chatapp.Session) (chatapp.HistoryPage, error)
	// SendText 创建并发送一条文字消息。
	SendText(ctx context.Context, input chatapp.OutgoingInput) (*chatapp.Message, error)
	// SendImage 上传并发送一条图片消息。
	SendImage(ctx context.Context, input chatapp.ImageInput) (*chatapp.Message, error)
	// MarkRead 将归属会话标记为已读并尽力上报平台。
	MarkRead(ctx context.Context, userID int64, accountID, chatID string) error
	// DeleteConversation 隐藏归属会话并清空其展示消息。
	DeleteConversation(ctx context.Context, userID int64, accountID, chatID string) error
	// FetchChatImage 从公网地址下载受大小与媒体类型限制的聊天图片，返回字节与媒体类型。
	FetchChatImage(ctx context.Context, rawURL string) ([]byte, string, error)
	// 快捷回复与买家备注。
	ListQuickReplies(ctx context.Context, userID int64, accountID string) ([]chatapp.QuickReply, error)
	CreateQuickReply(ctx context.Context, userID int64, accountID, content string) (chatapp.QuickReply, error)
	DeleteQuickReply(ctx context.Context, userID int64, accountID string, quickReplyID int64) error
	GetBuyerNote(ctx context.Context, userID int64, accountID, buyerID string) (chatapp.BuyerNote, error)
	SaveBuyerNote(ctx context.Context, userID int64, accountID, buyerID, content string) (chatapp.BuyerNote, error)
	// ListChatItems 按账号与会话查询可发送的聊天商品卡片。
	ListChatItems(ctx context.Context, input chatapp.ChatItemQuery) (chatapp.ChatItemPage, error)
}

// NotificationChannelPorts 聚合通知渠道与账号绑定用例。
// 渠道配置 JSON（SMTP 密码、机器人 secret 等）只经写入参数进入应用层，MCP 永不回传。
type NotificationChannelPorts interface {
	// ListChannels 返回用户全部通知渠道的非敏感摘要。
	ListChannels(ctx context.Context, userID int64) ([]notificationsapp.ChannelSummary, error)
	// GetChannelEditor 返回渠道编辑态；不含 SMTP 密码或机器人 secret。
	GetChannelEditor(ctx context.Context, userID, channelID int64) (notificationsapp.ChannelEditor, error)
	// CreateChannel 创建渠道并返回标识；Config 只写不读。
	CreateChannel(ctx context.Context, userID int64, input notificationsapp.ChannelInput) (int64, error)
	// UpdateChannel 部分更新渠道；只提交非空字段，Config 只写不读。
	UpdateChannel(ctx context.Context, userID, channelID int64, patch notificationsapp.ChannelPatch) error
	// DeleteChannel 删除用户拥有的渠道。
	DeleteChannel(ctx context.Context, userID, channelID int64) error
	// TestChannel 向渠道发送一条测试通知；发送时刻由适配器取墙钟。
	TestChannel(ctx context.Context, userID, channelID int64) error
	// ListBindings 返回用户全部账号与渠道的绑定摘要。
	ListBindings(ctx context.Context, userID int64) ([]notificationsapp.BindingSummary, error)
	// GetBindingIDs 返回账号当前启用的渠道标识。
	GetBindingIDs(ctx context.Context, userID int64, cookieID string) ([]int64, error)
	// SetBindings 覆盖保存账号的渠道绑定。
	SetBindings(ctx context.Context, userID int64, cookieID string, channelIDs []int64) error
	// SetSingleBinding 切换账号中单个渠道绑定的启用状态。
	SetSingleBinding(ctx context.Context, userID int64, cookieID string, channelID int64, enabled bool) error
	// DeleteBinding 删除一条绑定。
	DeleteBinding(ctx context.Context, userID, bindingID int64) error
	// DeleteAccountBindings 清空账号的全部绑定。
	DeleteAccountBindings(ctx context.Context, userID int64, cookieID string) error
}

// UncertainNotificationPorts 聚合通知不确定状态的用户视图与管理员全局视图。
type UncertainNotificationPorts interface {
	// ListUncertainForUser 返回当前用户渠道的不确定通知摘要与总数。
	ListUncertainForUser(ctx context.Context, userID int64, limit int) ([]notificationsapp.UncertainSummary, int, error)
	// ListUncertainForAdmin 返回全部用户渠道的不确定通知摘要与总数。
	ListUncertainForAdmin(ctx context.Context, limit int) ([]notificationsapp.UncertainSummary, int, error)
}

// SettingsPorts 聚合系统设置、用户设置、账号 AI 回复设置与 AI 连通性用例。
// 敏感设置的读写、审计与三态命令校验完全由 settings 应用服务负责；
// MCP 层只负责入参整形、在通用设置入口拦截 MCP 自身状态键，并保证秘密值不回传。
type SettingsPorts interface {
	// IsSensitiveSettingKey 判断设置键是否属于敏感白名单。
	IsSensitiveSettingKey(key string) bool
	// PublicSystem 读取无需认证展示的系统设置。
	PublicSystem(ctx context.Context) (map[string]string, error)
	// GetSystem 读取已脱敏的管理员系统设置，并记录敏感键读取审计。
	GetSystem(ctx context.Context, userID int64) (map[string]string, error)
	// ApplySystemChanges 原子保存普通设置与敏感三态命令。
	ApplySystemChanges(ctx context.Context, userID int64, values map[string]string, secrets map[string]settingsapp.SecretChange) error
	// SetSystem 保存单项系统设置，敏感键走三态命令。
	SetSystem(ctx context.Context, userID int64, key, value, action string) error
	// ListUser 读取当前用户的全部偏好设置。
	ListUser(ctx context.Context, userID int64) (map[string]string, error)
	// GetUser 读取当前用户的一项偏好设置。
	GetUser(ctx context.Context, userID int64, key string) (string, error)
	// SetUser 保存当前用户的一项偏好设置。
	SetUser(ctx context.Context, userID int64, key, value string) error
	// ListAIReply 读取用户范围内的账号 AI 设置摘要。
	ListAIReply(ctx context.Context, userID int64) ([]settingsapp.AIReplySettings, error)
	// GetAIReply 读取指定账号的 AI 设置摘要。
	GetAIReply(ctx context.Context, userID int64, cookieID string) (settingsapp.AIReplySettings, error)
	// UpsertAIReply 保存指定账号的 AI 设置摘要，冲突校验由应用服务负责。
	UpsertAIReply(ctx context.Context, userID int64, cookieID string, settings settingsapp.AIReplySettings) error
	// ListAIModels 读取远端模型目录；apiKey 仅用于本次请求，不落库不回传。
	ListAIModels(ctx context.Context, userID int64, baseURL, apiKey string) ([]string, error)
	// TestAIConnection 发送一次最小对话请求验证配置；诊断结果不含密钥。
	TestAIConnection(ctx context.Context, userID int64, baseURL, apiKey, model string) (settingsapp.AIConnectionTestResult, error)
}

// BackgroundTask 是进程后台任务的非敏感快照；不含任务参数与错误正文。
type BackgroundTask struct {
	// ID 是当前进程内唯一的任务标识。
	ID string
	// Name 是任务的稳定业务名称。
	Name string
	// State 是任务生命周期状态：running/succeeded/failed/canceled/timed_out。
	State string
	// StartedAtUnixMilli 是任务开始执行的 Unix 毫秒时间戳。
	StartedAtUnixMilli int64
	// FinishedAtUnixMilli 是任务完成或取消的 Unix 毫秒时间戳；仍运行时为零。
	FinishedAtUnixMilli int64
	// DeadlineAtUnixMilli 是任务截止时间的 Unix 毫秒时间戳；无截止时间时为零。
	DeadlineAtUnixMilli int64
}

// AdminPorts 聚合管理员全局统计、用户管理与进程后台任务只读视图用例。
// 删除用户复用 AdminService：运行实例收束与禁止自删由应用服务负责。
type AdminPorts interface {
	// ListUsers 返回不含密码与凭证的用户摘要。
	ListUsers(ctx context.Context) ([]adminapp.UserSummary, error)
	// DeleteUser 删除目标用户；当前管理员不能删除自身。
	DeleteUser(ctx context.Context, currentUserID, targetUserID int64) error
	// Stats 返回管理员仪表盘全局聚合计数。
	Stats(ctx context.Context) (adminapp.Stats, error)
	// BackgroundTasks 返回进程后台任务的非敏感状态快照。
	BackgroundTasks() []BackgroundTask
}
