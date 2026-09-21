// 脱敏快照核心：从真实主号 SQLite 复制业务行到全新快照库，并对凭证、
// 平台账号标识、买家卖家身份、卡密内容与聊天正文中的明显个人信息做脱敏。
// 本文件只实现复制与脱敏逻辑，命令行装配见 main.go。
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"xianyu-go/internal/db"
)

// snapshotResult 汇总脱敏快照各表的复制行数，供 main 打印完成摘要。
type snapshotResult struct {
	Cookies  int // Cookies 是复制的账号行数。
	Items    int // Items 是复制的商品行数。
	Orders   int // Orders 是复制的订单行数。
	Cards    int // Cards 是复制并替换内容的卡密行数。
	Sessions int // Sessions 是复制的聊天会话行数。
	Messages int // Messages 是复制的聊天消息行数。
}

// 脱敏占位常量：全部是公开的 fixture 值，绝不来自源库真实数据。
const (
	// fixtureCookieValue 是快照账号统一凭证值，风格与 dbseed 的 fixture Cookie 一致。
	fixtureCookieValue = "unb=snap-fixture; _m_h5_tk=fixture_1;"
	// snapshotDisableReason 是快照账号统一停用原因，确保测试环境永不触发续期或真实外呼。
	snapshotDisableReason = "脱敏快照账号，禁止启用"
	// fixtureCardText 是文本卡密内容的统一脱敏占位。
	fixtureCardText = "快照脱敏测试发货内容"
	// fixtureCardAPIConfig 是 API 卡密配置的统一占位 JSON，不含真实地址或密钥。
	fixtureCardAPIConfig = `{"url":"","method":"GET","headers":{}}`
	// redactPhonePlaceholder 是聊天正文手机号的脱敏占位。
	redactPhonePlaceholder = "[已脱敏手机号]"
	// redactIDCardPlaceholder 是聊天正文身份证号的脱敏占位。
	redactIDCardPlaceholder = "[已脱敏证件号]"
	// redactEmailPlaceholder 是聊天正文邮箱地址的脱敏占位。
	redactEmailPlaceholder = "[已脱敏邮箱]"
)

// 聊天正文 PII 脱敏正则：先匹配更长更唯一的身份证号，再处理手机号与邮箱，
// 固定顺序保证输出确定性。
var (
	// redactIDCardPattern 匹配 18 位大陆身份证号，末位允许 X/x。
	redactIDCardPattern = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	// redactPhonePattern 匹配中国大陆 11 位手机号。
	redactPhonePattern = regexp.MustCompile(`1[3-9]\d{9}`)
	// redactEmailPattern 匹配常见邮箱地址。
	redactEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// snapshotFromSQLite 在单个事务内按外键顺序把源库业务行复制为脱敏行。
// source 是只读打开的真实主号连接；target 是已迁移的空快照库连接；
// options 提供快照 fixture 账号参数；返回各表复制行数与首个错误。
func snapshotFromSQLite(ctx context.Context, source, target *sql.DB, options snapshotOptions) (snapshotResult, error) {
	// result 汇总复制行数，各表复制成功时逐步累加。
	result := snapshotResult{}
	// tx 是目标库写入事务，保证快照整体原子。
	tx, err := target.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("开启快照事务: %w", err)
	}
	// committed 记录是否已提交，避免失败路径重复回滚或误提交。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// fixtureUserID 是新建 fixture 登录用户主键，账号与卡密的 user_id 外键都指向它。
	fixtureUserID, err := insertFixtureUser(ctx, tx, options)
	if err != nil {
		return result, err
	}
	// sessionKeys 记录已复制会话的“fixture账号\x00会话ID”键，供消息表过滤孤儿行。
	sessionKeys := make(map[string]bool)

	// 按外键依赖顺序复制：账号与停用状态 → 商品 → 卡密 → 订单 → 会话 → 消息。
	if result.Cookies, err = copyCookies(ctx, source, tx, fixtureUserID); err != nil {
		return result, err
	}
	if result.Items, err = copyItems(ctx, source, tx); err != nil {
		return result, err
	}
	if result.Cards, err = copyCards(ctx, source, tx, fixtureUserID); err != nil {
		return result, err
	}
	if result.Orders, err = copyOrders(ctx, source, tx); err != nil {
		return result, err
	}
	if result.Sessions, err = copyChatSessions(ctx, source, tx, sessionKeys); err != nil {
		return result, err
	}
	if result.Messages, err = copyChatMessages(ctx, source, tx, sessionKeys); err != nil {
		return result, err
	}

	// err 保存快照事务提交结果，失败时返回并触发统一回滚。
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("提交快照事务: %w", err)
	}
	committed = true
	return result, nil
}

// insertFixtureUser 在目标库创建唯一 fixture 登录用户并返回其自增主键。
// 真实用户及其密码摘要一律不复制，测试环境只保留这一个可登录账号。
func insertFixtureUser(ctx context.Context, tx *sql.Tx, options snapshotOptions) (int64, error) {
	// passwordHash 是 fixture 密码的 bcrypt 摘要，复用 internal/db 的哈希实现。
	passwordHash, err := db.HashPassword(options.Password)
	if err != nil {
		return 0, fmt.Errorf("生成 fixture 密码摘要: %w", err)
	}
	// res 保存插入结果，用于读取自增主键。
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_active, is_admin) VALUES (?,?,?,1,0)`,
		options.Username, options.Username+"@snapshot.test", passwordHash)
	if err != nil {
		return 0, fmt.Errorf("创建 fixture 用户: %w", err)
	}
	// userID 是新建用户的主键，供账号与卡密外键使用。
	userID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("读取 fixture 用户主键: %w", err)
	}
	return userID, nil
}

// copyCookies 复制账号表并为每个账号强制写入停用的 cookie_status 行。
// 真实 Cookie 值、平台用户名、密码、昵称、头像与凭证元数据全部替换为 fixture；
// 返回复制账号数。
func copyCookies(ctx context.Context, source *sql.DB, tx *sql.Tx, fixtureUserID int64) (int, error) {
	// rows 是源库账号表的只读游标。
	rows, err := source.QueryContext(ctx, `SELECT id, value, user_id, COALESCE(auto_confirm,1),
		COALESCE(remark,''), COALESCE(pause_duration,10), COALESCE(username,''), COALESCE(password,''),
		COALESCE(show_browser,0), COALESCE(nickname,''), COALESCE(avatar_url,''), COALESCE(metadata_json,''),
		COALESCE(last_refresh_at,0), COALESCE(auto_consign,0), COALESCE(auto_bargain,0),
		COALESCE(bargain_soothe_template,''), COALESCE(created_at,''), COALESCE(updated_at,created_at,'')
		FROM cookies`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 累计复制的账号数。
	count := 0
	for rows.Next() {
		// id、value、remark、username、password、nickname、avatarURL、metadataJSON、sootheTemplate 是账号表文本列。
		var id, value, remark, username, password, nickname, avatarURL, metadataJSON, sootheTemplate string
		// createdAt、updatedAt 是账号的创建与更新时间。
		var createdAt, updatedAt string
		// userID、autoConfirm、pauseDuration、showBrowser、lastRefreshAt、autoConsign、autoBargain 是账号表数值列。
		var userID, autoConfirm, pauseDuration, showBrowser, lastRefreshAt, autoConsign, autoBargain int64
		// err 表示当前账号行的扫描结果，读取失败时终止快照。
		if err := rows.Scan(&id, &value, &userID, &autoConfirm, &remark, &pauseDuration, &username, &password,
			&showBrowser, &nickname, &avatarURL, &metadataJSON, &lastRefreshAt, &autoConsign, &autoBargain,
			&sootheTemplate, &createdAt, &updatedAt); err != nil {
			return count, fmt.Errorf("读取源账号: %w", err)
		}
		// fixtureID 是当前账号映射后的确定性 fixture 标识。
		fixtureID := fixtureAccountID(id)
		// err 表示快照账号写入结果，外键或约束失败时终止本次复制。
		if _, err := tx.ExecContext(ctx, `INSERT INTO cookies
			(id, value, user_id, auto_confirm, remark, pause_duration, username, password, show_browser,
			 nickname, avatar_url, metadata_json, last_refresh_at, auto_consign, auto_bargain,
			 bargain_soothe_template, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			fixtureID, fixtureCookieValue, fixtureUserID, autoConfirm, remark, pauseDuration, "", "",
			showBrowser, "", "", "", 0, autoConsign, autoBargain, sootheTemplate, createdAt, updatedAt); err != nil {
			return count, fmt.Errorf("写入快照账号: %w", err)
		}
		// 每个快照账号强制停用，disable_reason 统一说明，杜绝测试环境触发续期或外呼。
		if _, err := tx.ExecContext(ctx, `INSERT INTO cookie_status (cookie_id, enabled, updated_at, disable_reason)
			VALUES (?,0,?,?)`, fixtureID, updatedAt, snapshotDisableReason); err != nil {
			return count, fmt.Errorf("写入快照账号停用状态: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

// copyItems 复制商品表行，只把账号外键映射为 fixture 账号，商品字段原样保留；
// 返回复制商品数。
func copyItems(ctx context.Context, source *sql.DB, tx *sql.Tx) (int, error) {
	// rows 是源库商品表的只读游标。
	rows, err := source.QueryContext(ctx, `SELECT id, cookie_id, item_id, COALESCE(item_title,''),
		COALESCE(item_description,''), COALESCE(item_category,''), COALESCE(item_price,''),
		COALESCE(item_detail,''), COALESCE(is_multi_spec,0), COALESCE(multi_quantity_delivery,0),
		COALESCE(created_at,''), COALESCE(updated_at,created_at,''), COALESCE(deleted_at,'') FROM item_info`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 累计复制的商品数。
	count := 0
	for rows.Next() {
		// id 是商品行主键，显式复制保证回放引用稳定。
		var id int64
		// cookieID、itemID、title、description、category、price、detail、createdAt、updatedAt、deletedAt 是商品文本列。
		var cookieID, itemID, title, description, category, price, detail, createdAt, updatedAt, deletedAt string
		// isMultiSpec、multiQuantityDelivery 是商品的多规格与多件发货标记。
		var isMultiSpec, multiQuantityDelivery int64
		// err 表示当前商品行的扫描结果，读取失败时终止快照。
		if err := rows.Scan(&id, &cookieID, &itemID, &title, &description, &category, &price, &detail,
			&isMultiSpec, &multiQuantityDelivery, &createdAt, &updatedAt, &deletedAt); err != nil {
			return count, fmt.Errorf("读取源商品: %w", err)
		}
		// err 表示快照商品写入结果，外键或约束失败时终止本次复制。
		if _, err := tx.ExecContext(ctx, `INSERT INTO item_info
			(id, cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail,
			 is_multi_spec, multi_quantity_delivery, created_at, updated_at, deleted_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, fixtureAccountID(cookieID), itemID, title, description, category, price, detail,
			isMultiSpec, multiQuantityDelivery, createdAt, updatedAt, deletedAt); err != nil {
			return count, fmt.Errorf("写入快照商品: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

// copyCards 复制卡密表行，正文、API 配置与图片替换为脱敏占位，名称与规格等
// 业务元数据保留；返回复制卡密数。
func copyCards(ctx context.Context, source *sql.DB, tx *sql.Tx, fixtureUserID int64) (int, error) {
	// rows 是源库卡密表的只读游标。
	rows, err := source.QueryContext(ctx, `SELECT id, COALESCE(name,''), type, COALESCE(api_config,''),
		COALESCE(text_content,''), COALESCE(data_content,''), COALESCE(image_url,''), COALESCE(description,''),
		COALESCE(enabled,1), COALESCE(delay_seconds,0), COALESCE(is_multi_spec,0), COALESCE(spec_name,''),
		COALESCE(spec_value,''), user_id, COALESCE(created_at,''), COALESCE(updated_at,created_at,'') FROM cards`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 累计复制的卡密数。
	count := 0
	for rows.Next() {
		// id 是卡密行主键。
		var id int64
		// name、cardType、apiConfig、textContent、dataContent、imageURL、description、specName、specValue 是卡密文本列。
		var name, cardType, apiConfig, textContent, dataContent, imageURL, description, specName, specValue string
		// enabled、delaySeconds、isMultiSpec、createdAt、updatedAt 是卡密状态与时间列。
		var enabled, delaySeconds, isMultiSpec int64
		// createdAt、updatedAt 是卡密的创建与更新时间。
		var createdAt, updatedAt string
		// userID 是源卡密归属用户主键，快照中统一指向 fixture 用户。
		var userID int64
		// err 表示当前卡密行的扫描结果，读取失败时终止快照。
		if err := rows.Scan(&id, &name, &cardType, &apiConfig, &textContent, &dataContent, &imageURL,
			&description, &enabled, &delaySeconds, &isMultiSpec, &specName, &specValue, &userID,
			&createdAt, &updatedAt); err != nil {
			return count, fmt.Errorf("读取源卡密: %w", err)
		}
		// fixtureData 是逐行递增的数据卡占位，模仿 dbseed 的 DOCKER-FIXTURE 模式。
		fixtureData := fmt.Sprintf("DOCKER-FIXTURE-%03d-A\nDOCKER-FIXTURE-%03d-B", count+1, count+1)
		// err 表示快照卡密写入结果，外键或约束失败时终止本次复制。
		if _, err := tx.ExecContext(ctx, `INSERT INTO cards
			(id, name, type, api_config, text_content, data_content, image_url, description, enabled,
			 delay_seconds, is_multi_spec, spec_name, spec_value, user_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, name, cardType, fixtureCardAPIConfig, fixtureCardText, fixtureData, "", description,
			enabled, delaySeconds, isMultiSpec, specName, specValue, fixtureUserID, createdAt, updatedAt); err != nil {
			return count, fmt.Errorf("写入快照卡密: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

// copyOrders 复制订单表行，买家标识映射为 fixture，收件人姓名/电话/地址清空，
// 其余订单字段与时间原样保留；返回复制订单数。
func copyOrders(ctx context.Context, source *sql.DB, tx *sql.Tx) (int, error) {
	// rows 是源库订单表的只读游标。
	rows, err := source.QueryContext(ctx, `SELECT order_id, COALESCE(item_id,''), COALESCE(buyer_id,''),
		COALESCE(spec_name,''), COALESCE(spec_value,''), COALESCE(quantity,'1'), COALESCE(amount,'0'),
		COALESCE(order_status,'unknown'), cookie_id, COALESCE(is_bargain,0), COALESCE(receiver_name,''),
		COALESCE(receiver_phone,''), COALESCE(receiver_address,''), COALESCE(receiver_city,''),
		COALESCE(version,1), COALESCE(chat_id,''), COALESCE(system_shipped,0), COALESCE(created_at,''),
		COALESCE(updated_at,created_at,''), COALESCE(deleted_at,'') FROM orders`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 累计复制的订单数。
	count := 0
	for rows.Next() {
		// orderID、itemID、buyerID、specName、specValue、quantity、amount、status、cookieID 是订单业务列。
		var orderID, itemID, buyerID, specName, specValue, quantity, amount, status, cookieID string
		// receiverName、receiverPhone、receiverAddress、receiverCity 是订单收件人列，含明显个人信息。
		var receiverName, receiverPhone, receiverAddress, receiverCity string
		// version、chatID 是订单版本号与会话标识。
		var version, chatID string
		// systemShipped、isBargain 是订单的系统发货与砍价标记。
		var systemShipped, isBargain int64
		// createdAt、updatedAt、deletedAt 是订单时间与软删除标记。
		var createdAt, updatedAt, deletedAt string
		// err 表示当前订单行的扫描结果，读取失败时终止快照。
		if err := rows.Scan(&orderID, &itemID, &buyerID, &specName, &specValue, &quantity, &amount, &status,
			&cookieID, &isBargain, &receiverName, &receiverPhone, &receiverAddress, &receiverCity,
			&version, &chatID, &systemShipped, &createdAt, &updatedAt, &deletedAt); err != nil {
			return count, fmt.Errorf("读取源订单: %w", err)
		}
		// err 表示快照订单写入结果，外键或约束失败时终止本次复制。
		if _, err := tx.ExecContext(ctx, `INSERT INTO orders
			(order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount, order_status, cookie_id,
			 is_bargain, receiver_name, receiver_phone, receiver_address, receiver_city, version, chat_id,
			 system_shipped, created_at, updated_at, deleted_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			orderID, itemID, fixtureBuyerID(buyerID), specName, specValue, quantity, amount, status,
			fixtureAccountID(cookieID), isBargain, "", "", "", "", version, chatID, systemShipped,
			createdAt, updatedAt, deletedAt); err != nil {
			return count, fmt.Errorf("写入快照订单: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

// copyChatSessions 复制聊天会话行，买家卖家身份映射为 fixture，头像清空，
// 最后消息正文做 PII 脱敏；同时把已复制会话键写入 sessionKeys 供消息过滤；
// 返回复制会话数。
func copyChatSessions(ctx context.Context, source *sql.DB, tx *sql.Tx, sessionKeys map[string]bool) (int, error) {
	// rows 是源库会话表的只读游标。
	rows, err := source.QueryContext(ctx, `SELECT cookie_id, chat_id, COALESCE(buyer_id,''),
		COALESCE(buyer_name,''), COALESCE(buyer_avatar_url,''), COALESCE(item_id,''), COALESCE(item_title,''),
		COALESCE(item_image_url,''), COALESCE(last_message,''), COALESCE(last_message_at,0),
		COALESCE(unread_count,0), COALESCE(created_at,0), COALESCE(updated_at,0),
		COALESCE(user_hidden_at,0), COALESCE(messages_cleared_at,0), COALESCE(is_visible,1),
		COALESCE(local_messages_cleared_at,0), COALESCE(account_role,'unknown'), COALESCE(buyer_user_id,''),
		COALESCE(seller_user_id,''), COALESCE(role_item_id,''), COALESCE(role_source,'') FROM chat_sessions`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 累计复制的会话数。
	count := 0
	for rows.Next() {
		// cookieID、chatID、buyerID、buyerName、buyerAvatar、itemID、itemTitle、itemImageURL、lastMessage 是会话文本列。
		var cookieID, chatID, buyerID, buyerName, buyerAvatar, itemID, itemTitle, itemImageURL, lastMessage string
		// lastMessageAt、unreadCount、createdAt、updatedAt、userHiddenAt、messagesClearedAt、isVisible、localClearedAt 是会话状态列。
		var lastMessageAt, unreadCount, createdAt, updatedAt, userHiddenAt, messagesClearedAt, isVisible, localClearedAt int64
		// accountRole、buyerUserID、sellerUserID、roleItemID、roleSource 是会话角色证据列。
		var accountRole, buyerUserID, sellerUserID, roleItemID, roleSource string
		// err 表示当前会话行的扫描结果，读取失败时终止快照。
		if err := rows.Scan(&cookieID, &chatID, &buyerID, &buyerName, &buyerAvatar, &itemID, &itemTitle,
			&itemImageURL, &lastMessage, &lastMessageAt, &unreadCount, &createdAt, &updatedAt, &userHiddenAt,
			&messagesClearedAt, &isVisible, &localClearedAt, &accountRole, &buyerUserID, &sellerUserID,
			&roleItemID, &roleSource); err != nil {
			return count, fmt.Errorf("读取源会话: %w", err)
		}
		// fixtureCookieID 是当前会话映射后的账号标识。
		fixtureCookieID := fixtureAccountID(cookieID)
		// peerName 优先使用买家标识生成确定性昵称，避免空昵称导致身份分布失真。
		peerName := buyerName
		if strings.TrimSpace(peerName) == "" {
			peerName = buyerID
		}
		// err 表示快照会话写入结果，外键或约束失败时终止本次复制。
		if _, err := tx.ExecContext(ctx, `INSERT INTO chat_sessions
			(cookie_id, chat_id, buyer_id, buyer_name, buyer_avatar_url, item_id, item_title, item_image_url,
			 last_message, last_message_at, unread_count, created_at, updated_at, user_hidden_at,
			 messages_cleared_at, is_visible, local_messages_cleared_at, account_role, buyer_user_id,
			 seller_user_id, role_item_id, role_source)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			fixtureCookieID, chatID, fixtureBuyerID(buyerID), fixtureName(peerName), "", itemID, itemTitle,
			itemImageURL, sanitizeMessage(lastMessage), lastMessageAt, unreadCount, createdAt, updatedAt,
			userHiddenAt, messagesClearedAt, isVisible, localClearedAt, accountRole,
			fixtureBuyerID(buyerUserID), fixtureSellerID(sellerUserID), roleItemID, roleSource); err != nil {
			return count, fmt.Errorf("写入快照会话: %w", err)
		}
		// 记录已复制会话键，供 chat_messages 过滤外键孤儿行。
		sessionKeys[fixtureCookieID+"\x00"+chatID] = true
		count++
	}
	return count, rows.Err()
}

// copyChatMessages 复制聊天消息行，发送者身份映射为 fixture，正文做 PII 脱敏；
// 外键不在已复制会话中的孤儿消息直接跳过，避免破坏目标库外键约束；
// 返回复制消息数。
func copyChatMessages(ctx context.Context, source *sql.DB, tx *sql.Tx, sessionKeys map[string]bool) (int, error) {
	// rows 是源库消息表的只读游标。
	rows, err := source.QueryContext(ctx, `SELECT id, cookie_id, chat_id, COALESCE(message_key,''),
		COALESCE(direction,''), COALESCE(sender_id,''), COALESCE(sender_name,''), COALESCE(message_type,'text'),
		COALESCE(content,''), COALESCE(status,'received'), COALESCE(sent_at,0), COALESCE(created_at,0),
		COALESCE(read_status,0), COALESCE(read_at,0), COALESCE(media_duration,0) FROM chat_messages`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// count 累计复制的消息数。
	count := 0
	for rows.Next() {
		// id 是消息行主键。
		var id int64
		// cookieID、chatID、messageKey、direction、senderID、senderName、messageType、content、status 是消息文本列。
		var cookieID, chatID, messageKey, direction, senderID, senderName, messageType, content, status string
		// sentAt、createdAt、readStatus、readAt、mediaDuration 是消息状态与时间列。
		var sentAt, createdAt, readStatus, readAt, mediaDuration int64
		// err 表示当前消息行的扫描结果，读取失败时终止快照。
		if err := rows.Scan(&id, &cookieID, &chatID, &messageKey, &direction, &senderID, &senderName,
			&messageType, &content, &status, &sentAt, &createdAt, &readStatus, &readAt, &mediaDuration); err != nil {
			return count, fmt.Errorf("读取源消息: %w", err)
		}
		// fixtureCookieID 是当前消息映射后的账号标识。
		fixtureCookieID := fixtureAccountID(cookieID)
		// 目标库外键要求消息必须属于已复制会话，源库孤儿行不进入快照。
		if !sessionKeys[fixtureCookieID+"\x00"+chatID] {
			continue
		}
		// err 表示快照消息写入结果，外键或约束失败时终止本次复制。
		if _, err := tx.ExecContext(ctx, `INSERT INTO chat_messages
			(id, cookie_id, chat_id, message_key, direction, sender_id, sender_name, message_type, content,
			 status, sent_at, created_at, read_status, read_at, media_duration)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, fixtureCookieID, chatID, messageKey, direction, fixtureSenderID(senderID),
			fixtureName(senderName), messageType, sanitizeMessage(content), status, sentAt, createdAt,
			readStatus, readAt, mediaDuration); err != nil {
			return count, fmt.Errorf("写入快照消息: %w", err)
		}
		count++
	}
	return count, rows.Err()
}

// fixtureAccountID 把真实账号标识映射为确定性 fixture 账号 ID，空值原样返回。
// 相同真实 ID 在不同表与多次运行中映射结果一致，保证快照引用关系形状不变。
func fixtureAccountID(real string) string {
	return fixturePrefixedID("snap-acc-", real)
}

// fixtureBuyerID 把真实买家标识映射为确定性 fixture 买家 ID，空值原样返回。
func fixtureBuyerID(real string) string {
	return fixturePrefixedID("snap-buyer-", real)
}

// fixtureSellerID 把真实卖家（主号）平台标识映射为确定性 fixture 卖家 ID，空值原样返回。
func fixtureSellerID(real string) string {
	return fixturePrefixedID("snap-seller-", real)
}

// fixtureSenderID 把消息发送者平台标识映射为确定性 fixture 发送者 ID，
// 相同真实发送者映射结果一致，保留消息的身份分布；空值原样返回。
func fixtureSenderID(real string) string {
	return fixturePrefixedID("snap-sender-", real)
}

// fixtureName 把真实昵称映射为确定性 fixture 昵称，空值原样返回。
func fixtureName(real string) string {
	return fixturePrefixedID("snap-name-", real)
}

// fixturePrefixedID 用短哈希前缀构造确定性脱敏标识，空输入返回空串。
// prefix 是标识类别前缀；real 是待脱敏的真实标识。
func fixturePrefixedID(prefix, real string) string {
	// trimmed 是去除首尾空白后的真实标识，空白标识不生成占位。
	trimmed := strings.TrimSpace(real)
	if trimmed == "" {
		return ""
	}
	return prefix + shortHash(trimmed)
}

// shortHash 返回输入值前 6 字节 SHA-256 的十六进制摘要，作为确定性脱敏键。
func shortHash(value string) string {
	// sum 是输入值的 SHA-256 摘要，取前 6 字节足够区分快照内的标识。
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}

// sanitizeMessage 替换聊天正文中明显的个人身份信息（证件号、手机号、邮箱），
// 其余正文原样保留，供回放与评测使用。
func sanitizeMessage(content string) string {
	// sanitized 逐条应用脱敏正则，固定顺序保证输出确定性。
	sanitized := redactIDCardPattern.ReplaceAllString(content, redactIDCardPlaceholder)
	sanitized = redactPhonePattern.ReplaceAllString(sanitized, redactPhonePlaceholder)
	return redactEmailPattern.ReplaceAllString(sanitized, redactEmailPlaceholder)
}
