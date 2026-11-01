package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrAIBargainQuoteAlreadyClaimed 表示同一订单已绑定过 AI 报价，重复事件不得再次执行外部改价。
var ErrAIBargainQuoteAlreadyClaimed = errors.New("AI 议价报价已被该订单领取")

// aiMasterEnabledKey 是 system_settings 中 AI 全局总闸的键名。
// 该键是普通键（非敏感），可用 SystemSettings.Get 直接读取而不写 security_audit_logs。
const aiMasterEnabledKey = "ai_master_enabled"

// aiMasterEnabledOnValues 是全局总闸的开启取值集合（按小写归一后比较）。
// 空串键对应「键不存在」：无配置时总闸视为开启，保证默认开启的语义不会被误读成关闭。
var aiMasterEnabledOnValues = map[string]bool{
	"":        true,
	"true":    true,
	"1":       true,
	"yes":     true,
	"on":      true,
	"enabled": true,
}

// aiReplyDefaultMaxDiscountPercent 是账号尚无 ai_reply_settings 行时的默认最大折扣比例。
// 取值必须与迁移 00001 的列默认值以及 GET /ai-reply-settings 的历史默认响应保持一致。
const aiReplyDefaultMaxDiscountPercent = 10

// aiReplyDefaultMaxDiscountAmount 是账号尚无设置行时的默认最大折扣金额（单位：元）。
const aiReplyDefaultMaxDiscountAmount = 100

// aiReplyDefaultMaxBargainRounds 是账号尚无设置行时的默认最大砍价轮次。
const aiReplyDefaultMaxBargainRounds = 3

// aiMasterEnabledFromValue 把全局总闸的原始配置值归一为布尔结论。
// 参数 raw 是 system_settings 原文，允许前后空白与任意大小写；
// 返回 true 表示总闸开启（含键不存在），false 表示总闸显式关闭。
// 该函数是纯函数，不做 I/O，可在每条消息的判定路径上反复调用。
func aiMasterEnabledFromValue(raw string) bool {
	return aiMasterEnabledOnValues[strings.ToLower(strings.TrimSpace(raw))]
}

// defaultAIReplySettings 返回账号尚无显式设置行时的默认护栏与「跟随全局总闸」的开关。
// 参数 cookieID 是调用方正在读取的账号标识，用于填充返回值，避免上层误用其他账号的默认值。
// 返回值中的 AIEnabled 为 true 表示跟随总闸；AIEnabledExplicit 为 false 表示没有显式行。
func defaultAIReplySettings(cookieID string) AIReplySettings {
	return AIReplySettings{
		// CookieID 与调用方查询的账号一致，便于上层直接回显。
		CookieID: cookieID,
		// AIEnabled 为 true：无显式行即跟随全局总闸，总闸开启时视为启用。
		AIEnabled: true,
		// AIEnabledExplicit 为 false：数据库中没有该账号的显式开关，展示层据此给出 source=global。
		AIEnabledExplicit: false,
		// AutoAdjustPriceEnabled 为 false：真实改价涉及资金，默认必须由账号显式开启。
		AutoAdjustPriceEnabled: false,
		// MaxDiscountPercent、MaxDiscountAmount、MaxBargainRounds 使用迁移列的默认护栏。
		MaxDiscountPercent: aiReplyDefaultMaxDiscountPercent,
		MaxDiscountAmount:  aiReplyDefaultMaxDiscountAmount,
		MaxBargainRounds:   aiReplyDefaultMaxBargainRounds,
		// CustomPrompts 为空：没有显式行时不存在账号自定义提示词。
		CustomPrompts: "",
	}
}

// AIConversationMessage 是一个账号会话中的 AI 对话消息。
type AIConversationMessage struct {
	Role         string
	Content      string
	Intent       string
	BargainCount int
}

// AIReplySettings 对应 ai_reply_settings 表，只承载账号级开关与砍价护栏。
// 模型名、API 地址与密钥不在此表：它们统一来自 system_settings 的全局 AI 配置（00003 建立，
// 由 00063 从本表删除模型列），因此本结构体刻意不含任何模型字段。
type AIReplySettings struct {
	CookieID string `json:"cookie_id"`
	// AIEnabled 是账号级 AI 开关：未显式保存（AIEnabledExplicit 为 false）时恒为 true，表示跟随全局总闸。
	AIEnabled bool `json:"ai_enabled"`
	// AIEnabledExplicit 表示账号是否已显式保存过开关。
	// 读路径由「ai_reply_settings 是否存在该账号行」填充；写路径不使用该字段。
	AIEnabledExplicit bool `json:"ai_enabled_explicit"`
	// SkipAIEnabled 表示本次写入不改动 ai_enabled 列，保留数据库中的已有值。
	// 默认 false 表示照常写入，保证既有调用方（含 MCP 工具）的显式开关语义不变；
	// 仅「只改砍价护栏、不提交开关」的保存路径应置为 true。
	SkipAIEnabled          bool   `json:"-"`
	AutoAdjustPriceEnabled bool   `json:"auto_adjust_price_enabled"`
	MaxDiscountPercent     int    `json:"max_discount_percent"`
	MaxDiscountAmount      int    `json:"max_discount_amount"`
	MaxBargainRounds       int    `json:"max_bargain_rounds"`
	CustomPrompts          string `json:"custom_prompts"`
}

// AIBargainQuote 保存一条已经成功发送给买家的、可用于订单改价的 AI 报价。
type AIBargainQuote struct {
	// ID 是报价持久化主键。
	ID int64
	// CookieID 是卖家账号标识。
	CookieID string
	// ChatID 是产生报价的会话标识。
	ChatID string
	// BuyerID 是收到报价的买家标识。
	BuyerID string
	// ItemID 是报价对应的商品标识。
	ItemID string
	// PriceCents 是以分为单位的单件成交报价；订单执行方负责按购买数量折算总价。
	PriceCents int64
	// OrderID 是领取该报价的订单标识；待领取时为空。
	OrderID string
}

// AIReply 操作。
type AIReply struct {
	DB      *sql.DB
	Dialect Dialect
}

// MasterEnabled 读取 AI 全局总闸的生效值：总闸未配置或取开启值时返回 true。
//
// 并发与敏感性约束：走普通读 SystemSettings.Get（无行返回空串），
// 不使用 ReadSensitiveSetting/ReadSensitiveSettingForAccount——总闸是布尔开关、不含秘密，
// 而回复链路会按消息量读取它，审计读会让 security_audit_logs 随之膨胀。
// 返回的错误只表示数据库查询失败，调用方应显式上抛而不是静默按关闭处理。
func (a *AIReply) MasterEnabled(ctx context.Context) (bool, error) {
	if a == nil || a.DB == nil {
		// 存储未装配时按开启处理：避免把「装配缺失」静默变成全局关闭，导致线上 AI 全部停摆。
		return true, nil
	}
	// settings 是仅用于读取普通键的只读门面；ai_master_enabled 不在敏感白名单，
	// 因此这里不需要装配密文编解码器。
	settings := &SystemSettings{DB: a.DB, Dialect: a.Dialect}
	// raw 是总闸原文；键不存在时为空串，由 aiMasterEnabledFromValue 归一为开启。
	raw, err := settings.Get(ctx, aiMasterEnabledKey)
	if err != nil {
		return false, err
	}
	return aiMasterEnabledFromValue(raw), nil
}

// IsEnabled 返回账号 AI 回复的生效值：全局总闸开启，且账号没有被显式关闭。
//
// 「生效值」是三条回复通道唯一的运行期口径：无 ai_reply_settings 行表示跟随总闸，
// 因此无行且总闸开启时必须判定为启用。
// 返回值只表达结论，不读取模型密钥或其他敏感字段。
func (a *AIReply) IsEnabled(ctx context.Context, cookieID string) (bool, error) {
	// master 是全局总闸；关闭时无论账号如何配置都不会生效。
	master, err := a.MasterEnabled(ctx)
	if err != nil {
		return false, err
	}
	if !master {
		return false, nil
	}
	// enabled 是账号级开关的整数值；列值为 NULL 时按「跟随总闸」视为 1。
	var enabled int
	// err 是开关查询错误；无行表示未显式设置，按跟随总闸视为启用。
	err = a.DB.QueryRowContext(ctx, `SELECT COALESCE(ai_enabled, 1) FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return enabled != 0, nil
}

// ListEnabledCookieIDs 返回账号级启用 AI 的账号列表，按账号升序；无启用账号时返回空切片。
//
// 口径：以 cookies 为主表左连设置表，`COALESCE(ai_enabled, 1) = 1` 即「未显式关闭」，
// 因此没有设置行的账号同样出现在清单里——这正是「默认开启」的落地方式。
// 本方法只看账号级开关，不含全局总闸；需要判断「AI 是否真的会运行」的调用方
// 必须另外调用 MasterEnabled 并取与。
// ai_enabled 以 INTEGER 存储，跨方言都用「= 1」比较，不依赖驱动对布尔的编码。
func (a *AIReply) ListEnabledCookieIDs(ctx context.Context) ([]string, error) {
	// rows 是账号级启用 AI 的账号查询结果集。
	rows, err := a.DB.QueryContext(ctx, `
		SELECT c.id
		  FROM cookies c
		  LEFT JOIN ai_reply_settings s ON s.cookie_id = c.id
		 WHERE COALESCE(s.ai_enabled, 1) = 1
		 ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// ids 是按账号升序累积的 cookie_id。
	ids := make([]string, 0, 4)
	// rows.Next 遍历到结果集结束；扫描失败直接返回错误，避免返回半份清单。
	for rows.Next() {
		// id 是当前行读出的 cookie_id。
		var id string
		// scanErr 是当前行扫描失败原因；失败时立即返回，不返回半份清单。
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, scanErr
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PricingMode 只读取 AI 议价的生效值及其真实改价开关，供订单事件选择互斥执行路径。
//
// 返回的第一个值复用 IsEnabled 的生效口径（总闸 + 账号开关）；
// 第二个值只在第一个值为真时有意义，无设置行时按关闭处理（真实改价必须显式开启）。
func (a *AIReply) PricingMode(ctx context.Context, cookieID string) (bool, bool, error) {
	// effective 是 AI 议价的生效值；未生效时真实改价也不应执行。
	effective, err := a.IsEnabled(ctx, cookieID)
	if err != nil || !effective {
		return false, false, err
	}
	// autoAdjustEnabled 是真实自动改价开关的整数值；列值为 NULL 时按关闭处理。
	var autoAdjustEnabled int
	// err 是开关查询错误；无行表示没有显式设置，自动改价按关闭处理。
	err = a.DB.QueryRowContext(ctx, `SELECT COALESCE(auto_adjust_price_enabled, 0) FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&autoAdjustEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return true, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, autoAdjustEnabled != 0, nil
}

// Get 取某账号 AI 回复配置。模型名、API 地址与密钥一律不在此表读取：
// 模型配置的权威来源是 system_settings 的全局 AI 配置，本表三列已由 00063 删除。
//
// 契约：账号没有显式设置行时返回 defaultAIReplySettings 填充的默认结构而不再返回 ErrNotFound，
// 并且永不返回 (nil, nil)。这样调用方无需各自处理「无行」分支，也不会把「跟随总闸」
// 误判成「配置缺失」。AIEnabledExplicit 为 false 表示该值是跟随总闸的默认值。
func (a *AIReply) Get(ctx context.Context, cookieID string) (*AIReplySettings, error) {
	// s 是本账号的 AI 设置结果。
	var s AIReplySettings
	// enabled 是账号级开关整数值；列值为 NULL 时按 1（跟随总闸）读取。
	var enabled int
	// autoAdjustEnabled 表示 AI 报价是否允许在订单创建后触发真实改价；NULL 时按关闭读取。
	var autoAdjustEnabled int
	// customPrompts 是账号自定义提示词，数据库允许为空。
	var customPrompts sql.NullString
	// err 是配置查询错误；ErrNoRows 表示账号尚未保存显式设置。
	err := a.DB.QueryRowContext(ctx,
		`SELECT cookie_id, COALESCE(ai_enabled, 1), COALESCE(auto_adjust_price_enabled, 0),
		        COALESCE(max_discount_percent, ?), COALESCE(max_discount_amount, ?), COALESCE(max_bargain_rounds, ?),
		        COALESCE(custom_prompts, '')
		 FROM ai_reply_settings WHERE cookie_id=?`,
		aiReplyDefaultMaxDiscountPercent, aiReplyDefaultMaxDiscountAmount, aiReplyDefaultMaxBargainRounds, cookieID).Scan(
		&s.CookieID, &enabled, &autoAdjustEnabled,
		&s.MaxDiscountPercent, &s.MaxDiscountAmount, &s.MaxBargainRounds, &customPrompts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 无显式行：返回跟随总闸的默认护栏，而不是 ErrNotFound。
			settings := defaultAIReplySettings(cookieID)
			return &settings, nil
		}
		return nil, err
	}
	s.AIEnabled = enabled != 0
	s.AIEnabledExplicit = true
	s.AutoAdjustPriceEnabled = autoAdjustEnabled != 0
	s.CustomPrompts = customPrompts.String
	return &s, nil
}

// ListForUser 查询用户全部账号的 AI 回复配置，不读取或返回 API 密钥。
//
// 以 cookies 为主表左连设置表：没有显式行的账号同样返回一条「跟随总闸」的默认设置，
// 否则默认开启的账号会从列表接口与 MCP 列表工具里整条消失。
// 返回顺序按账号标识升序，便于前端稳定渲染。
func (a *AIReply) ListForUser(ctx context.Context, userID int64) ([]AIReplySettings, error) {
	// rows、err 是本用户全部账号的设置查询结果集及失败原因。
	rows, err := a.DB.QueryContext(ctx, `
		SELECT c.id,
		       COALESCE(s.ai_enabled, 1), COALESCE(s.auto_adjust_price_enabled, 0),
		       COALESCE(s.max_discount_percent, ?), COALESCE(s.max_discount_amount, ?), COALESCE(s.max_bargain_rounds, ?),
		       COALESCE(s.custom_prompts, ''),
		       CASE WHEN s.cookie_id IS NULL THEN 0 ELSE 1 END
		  FROM cookies c
		  LEFT JOIN ai_reply_settings s ON s.cookie_id = c.id
		 WHERE c.user_id = ?
		 ORDER BY c.id`,
		aiReplyDefaultMaxDiscountPercent, aiReplyDefaultMaxDiscountAmount, aiReplyDefaultMaxBargainRounds, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 是本用户的账号设置列表；无账号时返回 nil 切片，调用方按空列表处理。
	var out []AIReplySettings
	for rows.Next() {
		// item 是当前行的账号设置。
		var item AIReplySettings
		// enabled 是账号级开关整数值；autoAdjustEnabled 是真实改价开关整数值。
		var enabled, autoAdjustEnabled int
		// explicit 是该账号是否已有显式行的整数值标记：1 表示有显式开关，0 表示跟随总闸。
		var explicit int
		if // scanErr 是当前行扫描失败原因。
		scanErr := rows.Scan(&item.CookieID, &enabled, &autoAdjustEnabled, &item.MaxDiscountPercent, &item.MaxDiscountAmount, &item.MaxBargainRounds, &item.CustomPrompts, &explicit); scanErr != nil {
			return nil, scanErr
		}
		item.AIEnabled = enabled != 0
		item.AIEnabledExplicit = explicit != 0
		item.AutoAdjustPriceEnabled = autoAdjustEnabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertSettings 保存指定账号的 AI 回复开关和砍价约束。
//
// 三态约定：settings.SkipAIEnabled 为 true 时本次写入完全不改动 ai_enabled 列
// （保留数据库中已有的显式值；首次插入时写入 1，即「跟随总闸」在总闸开启下的等价显式值）。
// 这让「只改砍价护栏」的保存不会把原本启用的账号悄悄写成显式关闭。
// 布尔列一律经 boolToInt 写入：pgx 的二进制编码会拒绝把 Go bool 写进 int4 列。
func (a *AIReply) UpsertSettings(ctx context.Context, cookieID string, settings AIReplySettings) error {
	// insertEnabled 是首次插入行的 ai_enabled 初值。
	insertEnabled := boolToInt(settings.AIEnabled)
	if settings.SkipAIEnabled {
		// 不提交开关时插入 1：没有历史值时，「跟随总闸」在总闸开启下等价于显式启用。
		insertEnabled = 1
	}
	// assignments 是冲突时需要覆盖的列及其 SQL 表达式。
	assignments := map[string]string{
		"auto_adjust_price_enabled": "EXCLUDED.auto_adjust_price_enabled",
		"max_discount_percent":      "EXCLUDED.max_discount_percent",
		"max_discount_amount":       "EXCLUDED.max_discount_amount",
		"max_bargain_rounds":        "EXCLUDED.max_bargain_rounds",
		"custom_prompts":            "EXCLUDED.custom_prompts",
		"updated_at":                "CURRENT_TIMESTAMP",
	}
	if settings.SkipAIEnabled {
		// ai_enabled=ai_enabled 是两种方言都支持的「保留已有值」写法（右值指向更新前的行）。
		assignments["ai_enabled"] = "ai_enabled"
	} else {
		assignments["ai_enabled"] = "EXCLUDED.ai_enabled"
	}
	// err 是配置写入失败原因。
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings
		 (cookie_id, ai_enabled, auto_adjust_price_enabled, max_discount_percent, max_discount_amount,
		  max_bargain_rounds, custom_prompts, updated_at)
		 VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(a.Dialect, []string{"cookie_id"}, assignments),
		cookieID, insertEnabled, boolToInt(settings.AutoAdjustPriceEnabled), settings.MaxDiscountPercent,
		settings.MaxDiscountAmount, settings.MaxBargainRounds, nullableAIString(settings.CustomPrompts))
	return err
}

// ReplacePendingQuote 使同一买家和商品的旧报价失效，并保存最新的可执行报价。
func (a *AIReply) ReplacePendingQuote(ctx context.Context, quote AIBargainQuote, expiresAt int64) error {
	// tx 保证旧报价失效与新报价写入不会留下并行的有效报价；err 是事务创建错误。
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE ai_bargain_quotes
		SET status='superseded',updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND buyer_id=? AND item_id=? AND status='pending'`, quote.CookieID, quote.BuyerID, quote.ItemID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_bargain_quotes
		(cookie_id,chat_id,buyer_id,item_id,price_cents,status,expires_at,error_message)
		VALUES (?,?,?,?,?,'pending',?,'')`, quote.CookieID, quote.ChatID, quote.BuyerID, quote.ItemID, quote.PriceCents, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimPendingQuote 按订单事实原子领取最新有效报价；重复订单事件不会领取第二条报价。
func (a *AIReply) ClaimPendingQuote(ctx context.Context, cookieID, chatID, buyerID, itemID, orderID string, now int64) (*AIBargainQuote, error) {
	// tx 串联订单防重检查、报价选择和领取状态更新；err 是事务创建错误。
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// existingCount 是已经绑定到当前订单的报价数量，非零表示重复事件。
	var existingCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_bargain_quotes WHERE cookie_id=? AND order_id=?`, cookieID, orderID).Scan(&existingCount); err != nil {
		return nil, err
	}
	if existingCount > 0 {
		return nil, ErrAIBargainQuoteAlreadyClaimed
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_bargain_quotes SET status='expired',updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND status='pending' AND expires_at<?`, cookieID, now); err != nil {
		return nil, err
	}
	// quote 保存与账号、买家、商品和会话完全匹配的最新报价。
	quote := AIBargainQuote{CookieID: cookieID, ChatID: chatID, BuyerID: buyerID, ItemID: itemID, OrderID: orderID}
	if err = tx.QueryRowContext(ctx, `SELECT id,price_cents FROM ai_bargain_quotes
		WHERE cookie_id=? AND chat_id=? AND buyer_id=? AND item_id=? AND status='pending' AND expires_at>=?
		ORDER BY id DESC LIMIT 1`, cookieID, chatID, buyerID, itemID, now).Scan(&quote.ID, &quote.PriceCents); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// result 是带状态前置条件的领取结果，避免同一报价被并发订单重复消费。
	result, err := tx.ExecContext(ctx, `UPDATE ai_bargain_quotes
		SET status='processing',order_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'`, orderID, quote.ID)
	if err != nil {
		return nil, err
	}
	// affected 是成功领取的报价行数；零表示另一个执行者已经抢先领取。
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, nil
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &quote, nil
}

// FinishQuote 保存 AI 报价改价的确定终态或人工核对状态。
func (a *AIReply) FinishQuote(ctx context.Context, quoteID int64, status, errorMessage string) error {
	// result 是仅允许 processing 状态收口的更新结果；err 是数据库更新错误。
	result, err := a.DB.ExecContext(ctx, `UPDATE ai_bargain_quotes
		SET status=?,error_message=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing'`, status, errorMessage, quoteID)
	if err != nil {
		return err
	}
	// affected 是成功收口的报价行数，非一表示执行状态已被意外修改。
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

// nullableAIString 将空的自定义提示词转换为数据库 NULL。
func nullableAIString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// ConversationHistory 返回最近的会话消息，结果按时间正序排列。
func (a *AIReply) ConversationHistory(ctx context.Context, cookieID, chatID, itemID string, limit int) ([]AIConversationMessage, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
		SELECT role, content, COALESCE(intent,''), COALESCE(bargain_count,0)
		  FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?
		 ORDER BY id DESC LIMIT ?`, cookieID, chatID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// reversed 用于本次流程后续判断的reversed
	var reversed []AIConversationMessage
	for rows.Next() {
		// message 用于本次流程后续判断的消息
		var message AIConversationMessage
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&message.Role, &message.Content, &message.Intent, &message.BargainCount); err != nil {
			return nil, err
		}
		reversed = append(reversed, message)
	}
	if // err 用于本次流程后续判断的err
	err := rows.Err(); err != nil {
		return nil, err
	}
	// result 用于本次流程后续判断的结果
	result := make([]AIConversationMessage, len(reversed))
	// i 表示当前遍历过程中的i
	for i := range reversed {
		result[len(reversed)-1-i] = reversed[i]
	}
	return result, nil
}

// CurrentBargainCount 返回会话目前的砍价轮次。
func (a *AIReply) CurrentBargainCount(ctx context.Context, cookieID, chatID, itemID string) (int, error) {
	// count 用于本次流程后续判断的数量
	var count int
	// err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(bargain_count),0) FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, itemID).Scan(&count)
	return count, err
}

// AddConversation 追加一条会话消息。
func (a *AIReply) AddConversation(ctx context.Context, cookieID, chatID, userID, itemID string, message AIConversationMessage) error {
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount)
	return err
}

// AddConversationExchange 原子保存一轮用户消息与 AI 回复，避免上游调用失败时
// 留下半轮历史并错误消耗砍价轮次。
// AddConversationExchange 新增ConversationExchange。
func (a *AIReply) AddConversationExchange(ctx context.Context, cookieID, chatID, userID, itemID string, userMessage, assistantMessage AIConversationMessage) error {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// query 用于本次流程后续判断的查询
	query := `INSERT INTO ai_conversations
		(cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`
	// message 表示当前遍历过程中的消息
	for _, message := range []AIConversationMessage{userMessage, assistantMessage} {
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, query, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AIDecisionRecord 是一次接管决策的结构化记录，用于回答「这条消息为什么被放行或拦下」。
// 消息原文只保留短哈希与截断预览，避免把完整对话复制一份到审计表。
type AIDecisionRecord struct {
	// CookieID 是账号标识。
	CookieID string
	// ChatID 是会话标识。
	ChatID string
	// ItemID 是商品标识。
	ItemID string
	// MessageHash 是买家消息的短哈希。
	MessageHash string
	// MessagePreview 是买家消息的截断预览，仅供人读定位。
	MessagePreview string
	// Takeover 表示该消息最终是否允许 AI 接管。
	Takeover bool
	// HitIntents 是边界判定命中的意图名列表。
	HitIntents []string
	// NegativeHit 表示是否被内置或配置负向词表拦截。
	NegativeHit bool
	// ExtraHit 表示是否被扩展负向词表拦截。
	ExtraHit bool
	// IntentLabel 是写入对话历史的意图标签。
	IntentLabel string
	// Reason 是判定原因简码，取值与 internal/engine 的 AIReason* 常量一一对应；
	// db 包不能反向依赖 engine，故此处以字符串清单登记，清单缺项即代表口径漂移：
	//   - 边界判定：taken(AIReasonTaken) / no-intent(AIReasonNoIntent) /
	//     negative(AIReasonNegative) / extra-negative(AIReasonExtraNegative)；
	//   - 闸门提前拦下：master-off(AIReasonMasterOff) / account-off(AIReasonAccountOff)；
	//   - 商品回补链路：item-sync-requested(AIReasonItemSyncRequested) / item-sync-failed(AIReasonItemSyncFailed)。
	Reason string
	// ConfigFingerprint 是判定所用边界配置的指纹。
	ConfigFingerprint string
}

// AddDecisionLog 写入一条接管决策归因记录。
// 失败只影响可观测性，调用方须自行吞掉错误，不得让埋点失败阻断回复链路。
//
// 布尔列必须显式转 int：takeover / negative_hit / extra_hit 在两方言里都是 INTEGER，
// SQLite 能接受 bool 自动转换，PostgreSQL 的 pgx 二进制编码会直接拒绝
// （unable to encode bool into int4），导致归因表在线上一行都写不进去。
func (a *AIReply) AddDecisionLog(ctx context.Context, rec AIDecisionRecord) error {
	// intents 是命中意图的逗号拼接结果。
	intents := strings.Join(rec.HitIntents, ",")
	// err 是写入失败原因。
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO ai_decision_log
		(cookie_id,chat_id,item_id,message_hash,message_preview,takeover,hit_intents,negative_hit,extra_hit,intent_label,reason,config_fingerprint)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.CookieID, rec.ChatID, rec.ItemID, rec.MessageHash, rec.MessagePreview,
		boolToInt(rec.Takeover), intents, boolToInt(rec.NegativeHit), boolToInt(rec.ExtraHit),
		rec.IntentLabel, rec.Reason, rec.ConfigFingerprint)
	return err
}

// AIGenerationRecord 是一次生成的归因记录。
// 两个正交维度分别回答「谁答的」与「依据什么」：
// AnswerSource 区分模型与策略话术，GroundingSource 区分事实与文本规则。
type AIGenerationRecord struct {
	// CookieID 是账号标识。
	CookieID string
	// ChatID 是会话标识。
	ChatID string
	// ItemID 是商品标识。
	ItemID string
	// MessageHash 是买家消息的短哈希。
	MessageHash string
	// MessagePreview 是买家消息的截断预览。
	MessagePreview string
	// AnswerSource 是答案来源简码，见 answerSourceLLM 等常量。
	AnswerSource string
	// Guarded 表示是否被价格守卫整段替换。
	Guarded bool
	// GuardReason 是拦截原因简码；未拦截时为空。
	GuardReason string
	// GroundingSource 是事实来源简码：卡密事实、发货配置、描述关键词或无。
	GroundingSource string
	// GroundingDetail 是具体依据内容，如识别出的渠道名。
	GroundingDetail string
	// Model 是生成使用的模型标识。
	Model string
	// PromptHash 是系统提示的哈希；不落全文，避免放大体积并复制敏感内容。
	PromptHash string
	// LatencyMS 是本次生成的耗时（毫秒）。
	LatencyMS int64
	// ReplyPreview 是回复的截断预览。
	ReplyPreview string
}

// AddGenerationLog 写入一条生成归因记录。
// 失败只影响可观测性，调用方须自行吞掉错误。
// guarded 是 INTEGER 列，必须显式转 int，理由同 AddDecisionLog。
func (a *AIReply) AddGenerationLog(ctx context.Context, rec AIGenerationRecord) error {
	// err 是写入失败原因。
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO ai_generation_log
		(cookie_id,chat_id,item_id,message_hash,message_preview,answer_source,guarded,guard_reason,grounding_source,grounding_detail,model,prompt_hash,latency_ms,reply_preview)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.CookieID, rec.ChatID, rec.ItemID, rec.MessageHash, rec.MessagePreview,
		rec.AnswerSource, boolToInt(rec.Guarded), rec.GuardReason, rec.GroundingSource, rec.GroundingDetail,
		rec.Model, rec.PromptHash, rec.LatencyMS, rec.ReplyPreview)
	return err
}
