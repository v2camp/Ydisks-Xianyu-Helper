// ops_chat.go 保存运营 Agent（QQ 通道）会话与消息的持久化实现。
//
// 本文件只负责 SQL 与方言差异：会话按 (channel, target_openid) 唯一，消息按 (conversation_id, platform_msg_id)
// 在平台标识非空时去重。业务校验、预览裁剪与发送顺序全部由应用层决定，这里不判断 HTTP 语义。
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OpsConversation 是运营会话的持久化模型，不直接序列化为 HTTP 响应。
type OpsConversation struct {
	// ID 是会话自增主键，用作消息归属与接口路径参数。
	ID int64
	// Channel 是渠道标识，当前固定为 qq。
	Channel string
	// TargetOpenID 是平台会话目标：群聊为群 openid，单聊为用户 openid。
	TargetOpenID string
	// IsGroup 表示目标是否为群聊；群聊与单聊的 openid 命名空间不同。
	IsGroup bool
	// LastMessageAt 是最近一条消息的 Unix 毫秒时间，用于会话列表倒序与清理判定。
	LastMessageAt int64
	// LastMessagePreview 是最近一条消息的截断预览，写入方负责控制长度。
	LastMessagePreview string
	// CreatedAt 是会话首次创建时的 Unix 毫秒时间。
	CreatedAt int64
	// UpdatedAt 是会话最近一次写入摘要时的 Unix 毫秒时间。
	UpdatedAt int64
}

// OpsMessage 是运营消息的持久化模型，不直接序列化为 HTTP 响应。
type OpsMessage struct {
	// ID 是消息自增主键，也是消息游标。
	ID int64
	// ConversationID 是所属会话主键。
	ConversationID int64
	// PlatformMsgID 是平台消息标识；入站缺失或出站被动回复时为空。
	PlatformMsgID string
	// Direction 是消息方向：inbound 入站、outbound 出站。
	Direction string
	// Source 是消息来源：inbound 入站、command 命令回复、operator 人工发送。
	Source string
	// Content 是消息正文，属于业务内容而非凭证。
	Content string
	// CreatedAt 是消息写入时的 Unix 毫秒时间。
	CreatedAt int64
}

// OpsInboundRecord 是一次入站记录的写入输入。
type OpsInboundRecord struct {
	// Channel 是渠道标识，空白时按 qq 处理。
	Channel string
	// TargetOpenID 是会话目标 openid。
	TargetOpenID string
	// PlatformMsgID 是平台消息标识；非空时才参与去重。
	PlatformMsgID string
	// Direction 是消息方向，入站记录固定为 inbound。
	Direction string
	// Source 是消息来源，入站记录固定为 inbound。
	Source string
	// Content 是消息正文，由应用层完成校验与裁剪。
	Content string
	// IsGroup 表示目标是否为群聊。
	IsGroup bool
	// At 是消息时间（Unix 毫秒）；零值时由仓储回退本机时间。
	At int64
}

// OpsConversationPage 是运营会话分页查询结果。
type OpsConversationPage struct {
	// Conversations 是按最近消息时间倒序排列的当前页会话。
	Conversations []OpsConversation
	// Total 是全部会话总数。
	Total int
	// Page 是当前页码，从 1 开始。
	Page int
	// PageSize 是本次请求的页大小。
	PageSize int
}

// OpsMessagePage 是运营消息游标查询结果。
type OpsMessagePage struct {
	// Messages 是按时间正序排列的当前页消息。
	Messages []OpsMessage
	// HasMore 表示是否还有更早的消息。
	HasMore bool
}

// OpsChatStore 是运营会话与消息的仓储。
type OpsChatStore struct {
	// DB 是本仓储使用的数据库连接池。
	DB *sql.DB
	// Dialect 决定主键回读与冲突子句的写法。
	Dialect Dialect
}

// normalizeOpsChannel 返回去除空白后的渠道标识；空白时回退 qq。
func normalizeOpsChannel(channel string) string {
	// normalized 是去除首尾空白后的渠道标识。
	normalized := strings.TrimSpace(channel)
	if normalized == "" {
		return "qq"
	}
	return normalized
}

// RecordInbound 幂等写入一条入站消息，并只在消息为新记录时推进会话摘要。
// 返回写入后的消息模型与是否为新增记录；重复平台消息返回零主键且 inserted 为 false。
func (s *OpsChatStore) RecordInbound(ctx context.Context, record OpsInboundRecord) (OpsMessage, bool, error) {
	if s == nil || s.DB == nil {
		return OpsMessage{}, false, errors.New("运营会话存储未初始化")
	}
	// channel、target 是归一化后的会话定位字段。
	channel, target := normalizeOpsChannel(record.Channel), strings.TrimSpace(record.TargetOpenID)
	if target == "" {
		return OpsMessage{}, false, errors.New("运营会话缺少目标 openid")
	}
	// platformMsgID 是仅用于去重的平台消息标识，保持原始值。
	platformMsgID := strings.TrimSpace(record.PlatformMsgID)
	// at 是本次写入使用的 Unix 毫秒时间；零值时回退本机时间。
	at := record.At
	if at <= 0 {
		at = time.Now().UTC().UnixMilli()
	}
	// message 是待返回的消息模型，主键在插入成功后补齐。
	message := OpsMessage{PlatformMsgID: platformMsgID, Direction: "inbound", Source: "inbound", Content: record.Content, CreatedAt: at}
	// tx 保证会话建立、消息去重与会话摘要推进要么同时成功，要么全部回滚。
	tx, beginErr := s.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return OpsMessage{}, false, beginErr
	}
	defer tx.Rollback()
	// upsertSession 只在会话不存在时新建，重复入站不覆盖已有摘要。
	upsertSession := dialectInsertIgnorePrefix(s.Dialect) + ` INTO ops_conversations
		(channel,target_openid,is_group,last_message_at,last_message_preview,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"channel", "target_openid"})
	// upsertErr 是会话建立语句的执行错误。
	_, upsertErr := tx.ExecContext(ctx, upsertSession, channel, target, record.IsGroup, 0, "", at, at)
	if upsertErr != nil {
		return OpsMessage{}, false, fmt.Errorf("建立运营会话失败: %w", upsertErr)
	}
	// conversationID 是本次消息归属的会话主键。
	var conversationID int64
	// selectErr 是会话主键读取错误。
	selectErr := tx.QueryRowContext(ctx, `SELECT id FROM ops_conversations WHERE channel=? AND target_openid=?`, channel, target).Scan(&conversationID)
	if selectErr != nil {
		return OpsMessage{}, false, fmt.Errorf("读取运营会话失败: %w", selectErr)
	}
	message.ConversationID = conversationID
	// inserted、messageID、insertErr 表示消息是否新增、自增主键及写入失败原因。
	inserted, messageID, insertErr := s.insertOpsMessage(ctx, tx, message)
	if insertErr != nil {
		return OpsMessage{}, false, insertErr
	}
	if !inserted {
		return OpsMessage{ConversationID: conversationID, PlatformMsgID: platformMsgID, Direction: "inbound", Source: "inbound", Content: record.Content, CreatedAt: at}, false, nil
	}
	message.ID = messageID
	// summaryErr 是会话摘要更新错误。
	summaryErr := updateOpsConversationSummary(ctx, tx, conversationID, at, opsPreviewFromContent(record.Content, 0), at)
	if summaryErr != nil {
		return OpsMessage{}, false, summaryErr
	}
	// commitErr 是入站写入事务的提交错误。
	commitErr := tx.Commit()
	if commitErr != nil {
		return OpsMessage{}, false, commitErr
	}
	return message, true, nil
}

// insertOpsMessage 在事务内插入一条消息并回读自增主键，返回是否为新记录与主键。
// 平台消息标识非空时按部分唯一索引去重：冲突即视为重复入站。
func (s *OpsChatStore) insertOpsMessage(ctx context.Context, tx *sql.Tx, message OpsMessage) (bool, int64, error) {
	// insertSQL 是消息插入语句；只有平台标识非空时才追加冲突目标。
	insertSQL := `INSERT INTO ops_messages (conversation_id,platform_msg_id,direction,source,content,created_at) VALUES (?,?,?,?,?,?)`
	if strings.TrimSpace(message.PlatformMsgID) != "" {
		insertSQL += ` ON CONFLICT (conversation_id, platform_msg_id) WHERE platform_msg_id <> '' DO NOTHING`
	}
	// args 是与插入语句占位符严格对应的非敏感参数。
	args := []any{message.ConversationID, message.PlatformMsgID, message.Direction, message.Source, message.Content, message.CreatedAt}
	if s.Dialect == DialectPostgres {
		// messageID 是 RETURNING 回读的自增主键；冲突时不会返回任何行。
		var messageID int64
		// scanErr 是 RETURNING 回读结果；sql.ErrNoRows 表示命中冲突目标。
		scanErr := tx.QueryRowContext(ctx, insertSQL+" RETURNING id", args...).Scan(&messageID)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return false, 0, nil
		}
		if scanErr != nil {
			return false, 0, fmt.Errorf("写入运营消息失败: %w", scanErr)
		}
		return true, messageID, nil
	}
	// result、execErr 是 SQLite 插入结果及执行错误。
	result, execErr := tx.ExecContext(ctx, insertSQL, args...)
	if execErr != nil {
		return false, 0, fmt.Errorf("写入运营消息失败: %w", execErr)
	}
	// insertedRows 表示本次插入是否真正新增了一条记录。
	insertedRows, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return false, 0, rowsErr
	}
	if insertedRows == 0 {
		return false, 0, nil
	}
	// messageID 是 SQLite 自增主键。
	messageID, idErr := result.LastInsertId()
	if idErr != nil {
		return false, 0, idErr
	}
	return true, messageID, nil
}

// updateOpsConversationSummary 用一条新消息推进会话摘要；只有新消息才会调用。
func updateOpsConversationSummary(ctx context.Context, tx *sql.Tx, conversationID, at int64, preview string, updatedAt int64) error {
	// err 是会话摘要更新失败原因。
	_, err := tx.ExecContext(ctx, `UPDATE ops_conversations SET last_message_at=?,last_message_preview=?,updated_at=? WHERE id=?`,
		at, preview, updatedAt, conversationID)
	if err != nil {
		return fmt.Errorf("更新运营会话摘要失败: %w", err)
	}
	return nil
}

// opsPreviewFromContent 截断消息正文作为会话预览；limit 非正数时使用 60 个字符。
func opsPreviewFromContent(content string, limit int) string {
	if limit <= 0 {
		limit = 60
	}
	// runes 是按字符切分的正文，避免按字节截断中文。
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit])
}

// AppendOutbound 写入一条 Web 人工出站消息并更新会话摘要，返回消息与会话的最新快照。
func (s *OpsChatStore) AppendOutbound(ctx context.Context, conversationID int64, content, preview string, at int64) (OpsMessage, OpsConversation, error) {
	return s.appendOutbound(ctx, conversationID, "operator", content, preview, at)
}

// appendOutbound 以指定来源写入出站消息，并更新会话摘要。
// source 区分命令回复（command）与 Web 人工发送（operator）。
func (s *OpsChatStore) appendOutbound(ctx context.Context, conversationID int64, source, content, preview string, at int64) (OpsMessage, OpsConversation, error) {
	if s == nil || s.DB == nil {
		return OpsMessage{}, OpsConversation{}, errors.New("运营会话存储未初始化")
	}
	if conversationID <= 0 {
		return OpsMessage{}, OpsConversation{}, ErrNotFound
	}
	// now 是本次写入使用的 Unix 毫秒时间；零值时回退本机时间。
	now := at
	if now <= 0 {
		now = time.Now().UTC().UnixMilli()
	}
	// tx 保证消息插入、摘要更新与快照读取处于同一事务。
	tx, beginErr := s.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return OpsMessage{}, OpsConversation{}, beginErr
	}
	defer tx.Rollback()
	// message 是待写入的出站消息模型。
	message := OpsMessage{ConversationID: conversationID, Direction: "outbound", Source: source, Content: content, CreatedAt: now}
	// messageID、insertErr 是自增主键及插入失败原因。
	messageID, insertErr := insertReturningID(ctx, tx, s.Dialect,
		`INSERT INTO ops_messages (conversation_id,platform_msg_id,direction,source,content,created_at) VALUES (?,?,?,?,?,?)`,
		message.ConversationID, "", message.Direction, message.Source, message.Content, message.CreatedAt)
	if insertErr != nil {
		return OpsMessage{}, OpsConversation{}, fmt.Errorf("写入运营出站消息失败: %w", insertErr)
	}
	message.ID = messageID
	// summaryErr 是会话摘要更新错误。
	summaryErr := updateOpsConversationSummary(ctx, tx, conversationID, now, preview, now)
	if summaryErr != nil {
		return OpsMessage{}, OpsConversation{}, summaryErr
	}
	// conversation、readErr 是事务内读取的会话最新快照。
	conversation, readErr := readOpsConversation(ctx, tx, conversationID)
	if readErr != nil {
		return OpsMessage{}, OpsConversation{}, readErr
	}
	// commitErr 是出站写入事务的提交错误。
	commitErr := tx.Commit()
	if commitErr != nil {
		return OpsMessage{}, OpsConversation{}, commitErr
	}
	return message, conversation, nil
}

// AppendOutboundByTarget 按 (channel,target_openid) 定位会话后写入命令回复出站消息。
// 目标会话不存在时返回 ErrNotFound，调用方据此决定是否忽略记录。
func (s *OpsChatStore) AppendOutboundByTarget(ctx context.Context, channel, targetOpenID, content, preview string, at int64) (OpsMessage, OpsConversation, error) {
	if s == nil || s.DB == nil {
		return OpsMessage{}, OpsConversation{}, errors.New("运营会话存储未初始化")
	}
	// normalizedChannel、normalizedTarget 是去除空白后的会话定位字段。
	normalizedChannel, normalizedTarget := normalizeOpsChannel(channel), strings.TrimSpace(targetOpenID)
	if normalizedTarget == "" {
		return OpsMessage{}, OpsConversation{}, ErrNotFound
	}
	// conversationID 是命中的会话主键。
	var conversationID int64
	// lookupErr 是会话定位结果；不存在时返回统一 ErrNotFound。
	lookupErr := s.DB.QueryRowContext(ctx, `SELECT id FROM ops_conversations WHERE channel=? AND target_openid=?`,
		normalizedChannel, normalizedTarget).Scan(&conversationID)
	if errors.Is(lookupErr, sql.ErrNoRows) {
		return OpsMessage{}, OpsConversation{}, ErrNotFound
	}
	if lookupErr != nil {
		return OpsMessage{}, OpsConversation{}, lookupErr
	}
	// message、conversation、appendErr 是命令回复写入后的消息、会话快照及失败原因。
	message, conversation, appendErr := s.appendOutbound(ctx, conversationID, "command", content, preview, at)
	if appendErr != nil {
		return OpsMessage{}, OpsConversation{}, appendErr
	}
	return message, conversation, nil
}

// GetConversation 按主键读取一条会话；不存在时返回 ErrNotFound。
func (s *OpsChatStore) GetConversation(ctx context.Context, id int64) (OpsConversation, error) {
	if s == nil || s.DB == nil {
		return OpsConversation{}, errors.New("运营会话存储未初始化")
	}
	return readOpsConversation(ctx, s.DB, id)
}

// readOpsConversation 从指定执行器读取会话快照；不存在时返回 ErrNotFound。
func readOpsConversation(ctx context.Context, exec DBTX, id int64) (OpsConversation, error) {
	// conversation 是待扫描的会话快照。
	var conversation OpsConversation
	// err 是会话读取结果；不存在时转换为统一 ErrNotFound。
	err := exec.QueryRowContext(ctx, `SELECT id,channel,target_openid,is_group,last_message_at,last_message_preview,created_at,updated_at
		FROM ops_conversations WHERE id=?`, id).Scan(&conversation.ID, &conversation.Channel, &conversation.TargetOpenID,
		&conversation.IsGroup, &conversation.LastMessageAt, &conversation.LastMessagePreview, &conversation.CreatedAt, &conversation.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OpsConversation{}, ErrNotFound
	}
	if err != nil {
		return OpsConversation{}, err
	}
	return conversation, nil
}

// ListConversations 按最近消息时间倒序分页读取会话并返回总数。
// page 从 1 开始；pageSize 非正数时按 20 处理。
func (s *OpsChatStore) ListConversations(ctx context.Context, page, pageSize int) (OpsConversationPage, error) {
	if s == nil || s.DB == nil {
		return OpsConversationPage{}, errors.New("运营会话存储未初始化")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	// total 是全部会话总数，用于响应 total_pages。
	var total int
	// countErr 是会话总数查询错误。
	countErr := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ops_conversations`).Scan(&total)
	if countErr != nil {
		return OpsConversationPage{}, countErr
	}
	// offset 是当前页的跳过行数。
	offset := (page - 1) * pageSize
	// rows、queryErr 是当前页会话记录及查询错误。
	rows, queryErr := s.DB.QueryContext(ctx, `SELECT id,channel,target_openid,is_group,last_message_at,last_message_preview,created_at,updated_at
		FROM ops_conversations ORDER BY last_message_at DESC,id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if queryErr != nil {
		return OpsConversationPage{}, queryErr
	}
	defer rows.Close()
	// conversations 保存当前页扫描结果。
	conversations := make([]OpsConversation, 0, pageSize)
	for rows.Next() {
		// conversation 是当前待扫描的会话快照。
		var conversation OpsConversation
		// scanErr 是当前行字段映射错误。
		if scanErr := rows.Scan(&conversation.ID, &conversation.Channel, &conversation.TargetOpenID, &conversation.IsGroup,
			&conversation.LastMessageAt, &conversation.LastMessagePreview, &conversation.CreatedAt, &conversation.UpdatedAt); scanErr != nil {
			return OpsConversationPage{}, scanErr
		}
		conversations = append(conversations, conversation)
	}
	// iterationErr 是行流耗尽时的数据库错误，不能把不完整页面视为成功。
	if iterationErr := rows.Err(); iterationErr != nil {
		return OpsConversationPage{}, iterationErr
	}
	return OpsConversationPage{Conversations: conversations, Total: total, Page: page, PageSize: pageSize}, nil
}

// ListOpsMessages 按会话读取消息页：先倒序多取一条判断 HasMore，再反转为时间正序。
// beforeID 为零时读取最新一页；limit 非正数时按 50 处理。
func (s *OpsChatStore) ListOpsMessages(ctx context.Context, conversationID, beforeID int64, limit int) (OpsMessagePage, error) {
	if s == nil || s.DB == nil {
		return OpsMessagePage{}, errors.New("运营会话存储未初始化")
	}
	if limit <= 0 {
		limit = 50
	}
	// query 是消息查询语句；beforeID 非零时追加更早消息条件。
	query := `SELECT id,conversation_id,platform_msg_id,direction,source,content,created_at
		FROM ops_messages WHERE conversation_id=?`
	// args 是与查询占位符严格对应的参数。
	args := []any{conversationID}
	if beforeID > 0 {
		query += ` AND id<?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)
	// rows、queryErr 是倒序消息记录及查询错误。
	rows, queryErr := s.DB.QueryContext(ctx, query, args...)
	if queryErr != nil {
		return OpsMessagePage{}, queryErr
	}
	defer rows.Close()
	// messages 保存最多 limit+1 条倒序消息，用多取一条判断是否还有更早页。
	messages := make([]OpsMessage, 0, limit+1)
	for rows.Next() {
		// message 是当前待扫描的消息记录。
		var message OpsMessage
		// scanErr 是当前行字段映射错误。
		if scanErr := rows.Scan(&message.ID, &message.ConversationID, &message.PlatformMsgID, &message.Direction,
			&message.Source, &message.Content, &message.CreatedAt); scanErr != nil {
			return OpsMessagePage{}, scanErr
		}
		messages = append(messages, message)
	}
	// iterationErr 是行流耗尽时的数据库错误。
	if iterationErr := rows.Err(); iterationErr != nil {
		return OpsMessagePage{}, iterationErr
	}
	// page 是裁剪探测行并反转后的分页结果。
	page := OpsMessagePage{Messages: messages}
	if len(messages) > limit {
		page.HasMore = true
		page.Messages = messages[:limit]
	}
	// 数据库按倒序返回，接口契约要求时间正序。
	for i, j := 0, len(page.Messages)-1; i < j; i, j = i+1, j-1 {
		page.Messages[i], page.Messages[j] = page.Messages[j], page.Messages[i]
	}
	return page, nil
}
