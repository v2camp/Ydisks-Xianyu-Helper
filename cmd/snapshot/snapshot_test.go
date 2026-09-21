package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// leakMarkers 是夹具源库中的真实形态敏感标记，脱敏后目标库任何单元格都不得出现。
var leakMarkers = []string{
	"real-secret-cookie", "realtoken123", "real-platform-user", "real-platform-pwd",
	"tb_main_account_8848", "tb_buyer_real_001", "真实买家昵称", "主号昵称",
	"13800138000", "13912345678", "SECRET-CARD-CODE-12345", "SECRET-TEXT-67890", "SECRET-KEY-ABC",
	"张三", "浙江省杭州市西湖区某路1号",
}

// TestSnapshotSanitizesIdentitiesAndPreservesShape 构造带真实形态数据的源库，
// 运行快照后断言：凭证与身份已脱敏、敏感内容已替换、业务行数与关键字段保留。
func TestSnapshotSanitizesIdentitiesAndPreservesShape(t *testing.T) {
	// ctx 贯穿源库构造、快照生成与断言查询。
	ctx := context.Background()
	// sourceDB 是带完整迁移 schema 的源库连接，用于写入“真实形态”夹具。
	sourceDB, _, err := db.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	// sourceUserID 是源库真实用户的主键，供账号与卡密外键引用。
	sourceUserID := insertTestUsers(t, ctx, sourceDB)

	// 依次写入真实形态的业务数据：账号、商品、订单、卡密、会话与消息。
	insertTestCookies(t, ctx, sourceDB, sourceUserID)
	insertTestItems(t, ctx, sourceDB)
	insertTestOrders(t, ctx, sourceDB)
	insertTestCards(t, ctx, sourceDB, sourceUserID)
	insertTestChats(t, ctx, sourceDB)

	// 泄漏扫描器本身必须有效：源库必须能检出真实标记，否则断言失去意义。
	if leaked := countMarkerLeaks(t, ctx, sourceDB); leaked == 0 {
		t.Fatal("泄漏扫描器未能在源库检出标记，夹具或扫描器失效")
	}

	// targetDB 是快照输出库，同样执行迁移以获得完整 schema。
	targetDB, _, err := db.Open(ctx, filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer targetDB.Close()
	// result 保存各业务表复制行数。
	result, err := snapshotFromSQLite(ctx, sourceDB, targetDB, snapshotOptions{Username: "snap_user", Password: "snap_pass"})
	if err != nil {
		t.Fatal(err)
	}
	// 行数必须与源库夹具一致，证明形状与分布保留。
	if result.Cookies != 1 || result.Items != 1 || result.Orders != 1 || result.Cards != 2 || result.Sessions != 1 || result.Messages != 2 {
		t.Fatalf("复制行数不符: %+v", result)
	}

	// 逐表校验脱敏结果与关键字段保留情况。
	assertSnapshotContents(t, ctx, targetDB)

	// 快照库任何单元格都不得出现真实标记。
	if leaked := countMarkerLeaks(t, ctx, targetDB); leaked != 0 {
		t.Fatalf("快照库泄漏 %d 处真实标记", leaked)
	}
}

// insertTestUsers 创建源库的一个真实登录用户并返回其主键。
func insertTestUsers(t *testing.T, ctx context.Context, database *sql.DB) int64 {
	t.Helper()
	// passwordHash 是夹具用户密码的 bcrypt 摘要。
	passwordHash, err := db.HashPassword("real-user-password")
	if err != nil {
		t.Fatal(err)
	}
	// res 保存用户插入结果，用于读取自增主键。
	res, err := database.ExecContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_active, is_admin) VALUES (?,?,?,1,0)`,
		"real_user", "real_user@example.com", passwordHash)
	if err != nil {
		t.Fatal(err)
	}
	// userID 是新建用户的主键。
	userID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return userID
}

// insertTestCookies 写入一条带真实形态凭证的账号及其启用状态行。
func insertTestCookies(t *testing.T, ctx context.Context, database *sql.DB, userID int64) {
	t.Helper()
	mustExec(t, ctx, database, `INSERT INTO cookies
		(id, value, user_id, auto_confirm, remark, pause_duration, username, password, show_browser,
		 nickname, avatar_url, metadata_json, last_refresh_at, auto_consign, auto_bargain, bargain_soothe_template)
		VALUES (?,?,?,1,?,10,?,?,0,?,?,?,1700000000,1,1,?)`,
		"tb_main_account_8848", "unb=real-secret-cookie; _m_h5_tk=realtoken123;", userID,
		"主号备注", "real-platform-user", "real-platform-pwd", "主号昵称", "", `{"sig":1}`, "你好，欢迎咨询")
	mustExec(t, ctx, database,
		`INSERT INTO cookie_status (cookie_id, enabled, disable_reason) VALUES (?,1,'')`, "tb_main_account_8848")
}

// insertTestItems 写入一条真实形态的商品。
func insertTestItems(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	mustExec(t, ctx, database, `INSERT INTO item_info
		(cookie_id, item_id, item_title, item_description, item_category, item_price, item_detail, is_multi_spec, multi_quantity_delivery)
		VALUES (?,?,?,?,?,?,?,1,0)`,
		"tb_main_account_8848", "item-1001", "经典教程激活码", "包更新一年", "教程", "19.90", "标准版与旗舰版")
}

// insertTestOrders 写入一条带真实买家与收件人信息的订单。
func insertTestOrders(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	mustExec(t, ctx, database, `INSERT INTO orders
		(order_id, item_id, buyer_id, spec_name, spec_value, quantity, amount, order_status, cookie_id,
		 is_bargain, receiver_name, receiver_phone, receiver_address, receiver_city, version, chat_id, system_shipped)
		VALUES (?,?,?,?,?,?,?,?,?,0,?,?,?,?,1,?,1)`,
		"order-9001", "item-1001", "tb_buyer_real_001", "标准版", "v1", "1", "19.90", "completed",
		"tb_main_account_8848", "张三", "13800138000", "浙江省杭州市西湖区某路1号", "杭州市", "chat-5001")
}

// insertTestCards 写入数据卡与 API 卡各一条，内容均为真实形态的敏感占位。
func insertTestCards(t *testing.T, ctx context.Context, database *sql.DB, userID int64) {
	t.Helper()
	mustExec(t, ctx, database, `INSERT INTO cards
		(name, type, text_content, data_content, api_config, description, enabled, delay_seconds, user_id)
		VALUES (?,?,?,?,?,?,1,0,?)`,
		"激活码-数据卡", "data", "SECRET-TEXT-67890", "SECRET-CARD-CODE-12345", "", "真实卡密", userID)
	mustExec(t, ctx, database, `INSERT INTO cards
		(name, type, text_content, data_content, api_config, description, enabled, delay_seconds, user_id)
		VALUES (?,?,?,?,?,?,1,0,?)`,
		"API 取卡", "api", "", "", `{"url":"https://secret.example/api","key":"SECRET-KEY-ABC"}`, "真实 API 卡", userID)
}

// insertTestChats 写入一条真实形态的会话与两条消息。
func insertTestChats(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	mustExec(t, ctx, database, `INSERT INTO chat_sessions
		(cookie_id, chat_id, buyer_id, buyer_name, buyer_avatar_url, item_id, item_title, item_image_url,
		 last_message, last_message_at, unread_count, account_role, buyer_user_id, seller_user_id, role_item_id, role_source)
		VALUES (?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?)`,
		"tb_main_account_8848", "chat-5001", "tb_buyer_real_001", "真实买家昵称",
		"https://avatar.example/u/tb_buyer_real_001", "item-1001", "经典教程激活码",
		"https://img.example/item-1001.jpg", "收货电话 13800138000", 1700000000000,
		"seller", "tb_buyer_real_001", "tb_main_account_8848", "item-1001", "platform_verified")
	mustExec(t, ctx, database, `INSERT INTO chat_messages
		(cookie_id, chat_id, message_key, direction, sender_id, sender_name, message_type, content, status, sent_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"tb_main_account_8848", "chat-5001", "msg-key-1", "incoming", "tb_buyer_real_001", "真实买家昵称",
		"text", "我的手机号是 13912345678 请尽快发货", "received", 1700000000100)
	mustExec(t, ctx, database, `INSERT INTO chat_messages
		(cookie_id, chat_id, message_key, direction, sender_id, sender_name, message_type, content, status, sent_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"tb_main_account_8848", "chat-5001", "msg-key-2", "outgoing", "tb_main_account_8848", "主号昵称",
		"text", "好的，马上安排", "sent", 1700000000200)
}

// assertSnapshotContents 逐表校验快照库的脱敏结果与关键字段保留情况。
func assertSnapshotContents(t *testing.T, ctx context.Context, target *sql.DB) {
	t.Helper()
	// cookieValue 是快照账号的凭证值，必须等于统一 fixture 值。
	var cookieValue string
	mustScan(t, ctx, target, `SELECT value FROM cookies`, &cookieValue)
	if cookieValue != fixtureCookieValue {
		t.Fatalf("cookie 值未脱敏: %q", cookieValue)
	}
	// cookieUsername、cookieNickname 是快照账号必须清空的平台用户名与昵称。
	var cookieUsername, cookieNickname string
	mustScan(t, ctx, target, `SELECT COALESCE(username,''),COALESCE(nickname,'') FROM cookies`, &cookieUsername, &cookieNickname)
	if cookieUsername != "" || cookieNickname != "" {
		t.Fatalf("账号用户名或昵称未脱敏: username=%q nickname=%q", cookieUsername, cookieNickname)
	}
	// cookieID 是映射后的 fixture 账号标识。
	var cookieID string
	mustScan(t, ctx, target, `SELECT id FROM cookies`, &cookieID)
	if !strings.HasPrefix(cookieID, "snap-acc-") {
		t.Fatalf("账号 ID 未映射为 fixture: %q", cookieID)
	}
	// enabled、disableReason 是快照账号的停用状态与原因。
	var enabled int64
	// disableReason 是快照账号写入的统一停用原因。
	var disableReason string
	mustScan(t, ctx, target, `SELECT enabled,COALESCE(disable_reason,'') FROM cookie_status`, &enabled, &disableReason)
	if enabled != 0 || disableReason == "" {
		t.Fatalf("快照账号未停用: enabled=%d reason=%q", enabled, disableReason)
	}
	// buyerID、receiverPhone、receiverAddress、amount、orderStatus 是订单脱敏与保留的关键字段。
	var buyerID, receiverPhone, receiverAddress, amount, orderStatus string
	mustScan(t, ctx, target, `SELECT buyer_id,COALESCE(receiver_phone,''),COALESCE(receiver_address,''),amount,order_status FROM orders`,
		&buyerID, &receiverPhone, &receiverAddress, &amount, &orderStatus)
	if !strings.HasPrefix(buyerID, "snap-buyer-") {
		t.Fatalf("买家 ID 未脱敏: %q", buyerID)
	}
	if receiverPhone != "" || receiverAddress != "" {
		t.Fatalf("收件人信息未脱敏: phone=%q address=%q", receiverPhone, receiverAddress)
	}
	if amount != "19.90" || orderStatus != "completed" {
		t.Fatalf("订单关键字段未保留: amount=%q status=%q", amount, orderStatus)
	}
	// itemTitle 是必须保留的商品标题。
	var itemTitle string
	mustScan(t, ctx, target, `SELECT item_title FROM item_info`, &itemTitle)
	if itemTitle != "经典教程激活码" {
		t.Fatalf("商品标题未保留: %q", itemTitle)
	}
	// dataContents 是快照卡密的全部正文列，用于断言敏感内容已替换。
	dataContents := queryAllStrings(t, ctx, target, `SELECT COALESCE(data_content,'') FROM cards
		UNION ALL SELECT COALESCE(text_content,'') FROM cards
		UNION ALL SELECT COALESCE(api_config,'') FROM cards`)
	// content 表示当前卡密字段值，含真实内容时立即失败。
	for _, content := range dataContents {
		if strings.Contains(content, "SECRET") {
			t.Fatalf("卡密内容泄漏: %q", content)
		}
	}
	// lastMessage 是会话最后一条消息，其中的手机号必须已脱敏。
	var lastMessage string
	mustScan(t, ctx, target, `SELECT last_message FROM chat_sessions`, &lastMessage)
	if strings.Contains(lastMessage, "13800138000") {
		t.Fatalf("会话正文泄漏手机号: %q", lastMessage)
	}
	// peerName 是会话中的买家昵称，必须映射为 fixture 昵称。
	var peerName string
	mustScan(t, ctx, target, `SELECT buyer_name FROM chat_sessions`, &peerName)
	if !strings.HasPrefix(peerName, "snap-name-") {
		t.Fatalf("买家昵称未脱敏: %q", peerName)
	}
	// senderID、messageContent 是入站消息的发送者标识与正文。
	var senderID, messageContent string
	mustScan(t, ctx, target, `SELECT sender_id,content FROM chat_messages WHERE direction='incoming'`, &senderID, &messageContent)
	if !strings.HasPrefix(senderID, "snap-sender-") {
		t.Fatalf("发送者 ID 未脱敏: %q", senderID)
	}
	if strings.Contains(messageContent, "13912345678") {
		t.Fatalf("消息正文泄漏手机号: %q", messageContent)
	}
}

// countMarkerLeaks 扫描库中所有表的所有单元格，返回包含任一真实标记的单元格数。
func countMarkerLeaks(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	// tables 是库中全部业务表名，排除 sqlite 内部表。
	tables := queryAllStrings(t, ctx, database,
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	// leaked 累计命中的单元格数。
	leaked := 0
	// table 表示当前扫描的业务表名。
	for _, table := range tables {
		// cells 是当前表全部单元格的字符串表示。
		cells := queryAllStrings(t, ctx, database, `SELECT * FROM "`+table+`"`)
		// cell 表示当前单元格内容。
		for _, cell := range cells {
			// marker 表示当前待匹配的真实标记。
			for _, marker := range leakMarkers {
				if strings.Contains(cell, marker) {
					leaked++
				}
			}
		}
	}
	return leaked
}

// queryAllStrings 执行查询并把结果集的全部值归一化为字符串返回。
func queryAllStrings(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) []string {
	t.Helper()
	// rows 是查询结果集游标。
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	// cols 是结果集列名。
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	// values 是用于 Scan 的任意值切片。
	values := make([]any, len(cols))
	// targets 是指向 values 各元素的指针切片。
	targets := make([]any, len(cols))
	// index 表示当前列下标。
	for index := range cols {
		targets[index] = &values[index]
	}
	// result 汇总全部单元格字符串。
	result := make([]string, 0)
	for rows.Next() {
		// err 表示当前行的扫描结果，失败时直接终止测试。
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		// value 表示当前单元格原始值，统一转换为字符串后收集。
		for _, value := range values {
			result = append(result, cellString(value))
		}
	}
	// err 表示遍历过程是否提前中断，失败时直接终止测试。
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

// cellString 把 sql 扫描值转换为字符串；字符串与字节切片之外的值为空串。
func cellString(value any) string {
	// text 是转换后的单元格文本。
	switch text := value.(type) {
	case string:
		return text
	case []byte:
		return string(text)
	default:
		return ""
	}
}

// mustExec 执行写语句，失败时直接终止测试。
func mustExec(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) {
	t.Helper()
	// result 保存执行结果；err 表示写语句失败原因。
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

// mustScan 执行单行查询并扫描到 targets，无行或失败时直接终止测试。
func mustScan(t *testing.T, ctx context.Context, database *sql.DB, query string, targets ...any) {
	t.Helper()
	// err 表示单行查询的扫描结果，失败时直接终止测试。
	if err := database.QueryRowContext(ctx, query).Scan(targets...); err != nil {
		t.Fatal(err)
	}
}

// TestSanitizeMessageRedactsPII 验证正文脱敏正则对证件号、手机号与邮箱的处理。
func TestSanitizeMessageRedactsPII(t *testing.T) {
	// cases 是正文样例与期望输出的映射。
	cases := map[string]string{
		"电话 13912345678 谢谢":     "电话 " + redactPhonePlaceholder + " 谢谢",
		"邮箱 a@b.com 联系我":        "邮箱 " + redactEmailPlaceholder + " 联系我",
		"证件 110105199003071234": "证件 " + redactIDCardPlaceholder,
		"普通文本保持不变":              "普通文本保持不变",
	}
	// input 表示当前正文样例；want 表示期望输出。
	for input, want := range cases {
		// got 表示当前样例的脱敏输出，与期望值不一致即失败。
		if got := sanitizeMessage(input); got != want {
			t.Fatalf("sanitizeMessage(%q)=%q want %q", input, got, want)
		}
	}
}

// TestFixtureIDMappingsAreDeterministic 验证身份映射的确定性与空值处理。
func TestFixtureIDMappingsAreDeterministic(t *testing.T) {
	// first 是真实买家标识的第一次映射结果。
	first := fixtureBuyerID("tb_buyer_001")
	// second 是同一真实标识的第二次映射结果，必须与 first 完全一致。
	second := fixtureBuyerID("tb_buyer_001")
	if first == "" || first != second {
		t.Fatalf("fixture 映射不稳定: %q vs %q", first, second)
	}
	if !strings.HasPrefix(first, "snap-buyer-") {
		t.Fatalf("买家前缀错误: %q", first)
	}
	// got 是空账号标识的映射结果，必须保持空串。
	if got := fixtureAccountID(""); got != "" {
		t.Fatalf("空账号映射应为空: %q", got)
	}
}
