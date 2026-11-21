package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// 通知事件作用域常量：tenant 表示租户级账号事件，system 表示实例级系统告警。
const (
	// NotificationScopeTenant 是租户级事件作用域，接收人由账号所有者决定。
	NotificationScopeTenant = "tenant"
	// NotificationScopeSystem 是系统级事件作用域，接收人由管理员系统告警配置决定。
	NotificationScopeSystem = "system"
)

// NotificationEventInput 是一条待写入站内通知中心的事件。
type NotificationEventInput struct {
	// Scope 是事件作用域；空值按 tenant 处理，保证旧调用方兼容。
	Scope string
	// OwnerUserID 是租户事件所属本地用户；系统事件必须为 0。
	OwnerUserID int64
	// AccountID 是租户事件关联的闲鱼账号；系统事件为空字符串。
	AccountID string
	// EventType 是通知事件编码，用于展示分类和渠道订阅过滤。
	EventType string
	// Level 是事件级别，取 warn/info 等展示级别。
	Level string
	// Title 是事件标题，供站内列表直接展示。
	Title string
	// Body 是事件正文；可能包含订单或买家信息，禁止写入日志。
	Body string
	// DedupeKey 是可选的业务幂等键；非空时重复事件只保留一条记录。
	DedupeKey string
	// CreatedAt 是事件发生时间；零值时使用当前时间。
	CreatedAt time.Time
}

// NotificationEventSummary 是站内通知中心返回的非敏感事件摘要。
type NotificationEventSummary struct {
	// ID 是事件稳定标识，用于标记已读。
	ID int64
	// Scope 是事件作用域，区分租户视图与系统视图。
	Scope string
	// AccountID 是租户事件关联的闲鱼账号；系统事件为空。
	AccountID string
	// EventType 是通知事件编码。
	EventType string
	// Level 是事件级别。
	Level string
	// Title 是事件标题。
	Title string
	// Body 是事件正文；只返回给有权查看该事件范围的用户。
	Body string
	// Read 表示事件是否已被标记为已读。
	Read bool
	// CreatedAt 是事件发生时间的 Unix 秒时间戳。
	CreatedAt int64
}

// RecordEvent 幂等写入一条站内通知事件；DedupeKey 冲突时静默忽略重复事件。
func (n *Notifications) RecordEvent(ctx context.Context, input NotificationEventInput) error {
	// scope 是归一化后的事件作用域；空值兼容为租户事件。
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = NotificationScopeTenant
	}
	// createdAt 是事件发生时间；零值由存储层补当前时间。
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	// dedupeKey 是写入唯一索引的业务幂等键；空键写 NULL，允许多条非幂等事件共存。
	var dedupeKey any
	// key 是去除空白后的业务幂等键候选值。
	if key := strings.TrimSpace(input.DedupeKey); key != "" {
		dedupeKey = key
	}
	// query 是带冲突忽略语义的事件插入语句，重复业务键不会产生第二条记录。
	query := `INSERT INTO notification_events
			(scope,owner_user_id,account_id,event_type,level,title,body,dedupe_key,read_at,created_at)
			VALUES (?,?,?,?,?,?,?,?,0,?)` + dialectInsertIgnore(n.Dialect, []string{"dedupe_key"})
	// err 表示写入事件失败；调用方按最佳努力处理，不应阻断外部渠道投递。
	_, err := n.DB.ExecContext(ctx, query, scope, input.OwnerUserID, input.AccountID, input.EventType,
		input.Level, input.Title, input.Body, dedupeKey, createdAt.Unix())
	return err
}

// OwnerOfAccount 返回闲鱼账号所属的本地用户，仅用于租户事件归属解析。
func (n *Notifications) OwnerOfAccount(ctx context.Context, cookieID string) (int64, error) {
	// userID 是账号所有者主键。
	var userID int64
	// err 表示读取账号所有者失败；账号不存在时返回统一未找到错误。
	if err := n.DB.QueryRowContext(ctx, `SELECT user_id FROM cookies WHERE id=?`, cookieID).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return userID, nil
}

// ListEventsForUser 查询指定租户用户可见的站内通知事件，按时间倒序返回。
func (n *Notifications) ListEventsForUser(ctx context.Context, userID int64, limit int) ([]NotificationEventSummary, error) {
	return n.listNotificationEvents(ctx, NotificationScopeTenant, &userID, limit)
}

// ListSystemEvents 查询管理员可见的系统级站内通知事件，按时间倒序返回。
func (n *Notifications) ListSystemEvents(ctx context.Context, limit int) ([]NotificationEventSummary, error) {
	return n.listNotificationEvents(ctx, NotificationScopeSystem, nil, limit)
}

// listNotificationEvents 按作用域与可选所有者读取站内事件；limit 限制在 1 到 100 之间。
func (n *Notifications) listNotificationEvents(ctx context.Context, scope string, ownerUserID *int64, limit int) ([]NotificationEventSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	// query 保存按作用域隔离的事件查询语句；系统事件通过 scope 过滤，不读取租户所有者。
	query := `SELECT id, scope, account_id, event_type, level, title, body, read_at, created_at
		FROM notification_events WHERE scope=?`
	// args 保存作用域、可选所有者与分页参数。
	args := make([]any, 0, 3)
	args = append(args, scope)
	if ownerUserID != nil {
		query += ` AND owner_user_id=?`
		args = append(args, *ownerUserID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	// rows、err 保存事件查询结果与读取错误。
	rows, err := n.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// events 保存脱离数据库模型的事件摘要。
	events := make([]NotificationEventSummary, 0)
	for rows.Next() {
		// event 是当前遍历到的事件摘要；readAt 是已读时间戳，0 表示未读。
		var event NotificationEventSummary
		// readAt 是事件已读 Unix 秒时间戳，仅用于换算布尔状态。
		var readAt int64
		// scanErr 表示当前事件行读取失败。
		if scanErr := rows.Scan(&event.ID, &event.Scope, &event.AccountID, &event.EventType, &event.Level,
			&event.Title, &event.Body, &readAt, &event.CreatedAt); scanErr != nil {
			return nil, scanErr
		}
		event.Read = readAt != 0
		events = append(events, event)
	}
	return events, rows.Err()
}

// CountUnreadEventsForUser 统计指定租户用户未读的站内通知事件数量。
func (n *Notifications) CountUnreadEventsForUser(ctx context.Context, userID int64) (int, error) {
	return n.countUnreadNotificationEvents(ctx, NotificationScopeTenant, &userID)
}

// CountUnreadSystemEvents 统计管理员可见的未读系统级通知事件数量。
func (n *Notifications) CountUnreadSystemEvents(ctx context.Context) (int, error) {
	return n.countUnreadNotificationEvents(ctx, NotificationScopeSystem, nil)
}

// countUnreadNotificationEvents 按作用域与可选所有者统计未读事件数量。
func (n *Notifications) countUnreadNotificationEvents(ctx context.Context, scope string, ownerUserID *int64) (int, error) {
	// query 保存未读事件统计语句；只返回数量，不读取正文。
	query := `SELECT COUNT(*) FROM notification_events WHERE scope=? AND read_at=0`
	// args 保存作用域与可选所有者参数。
	args := make([]any, 0, 2)
	args = append(args, scope)
	if ownerUserID != nil {
		query += ` AND owner_user_id=?`
		args = append(args, *ownerUserID)
	}
	// count 是满足条件的未读事件数量。
	var count int
	// err 表示统计未读事件失败。
	if err := n.DB.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// MarkEventReadForUser 标记指定租户用户的单条事件为已读；重复标记保持幂等成功。
func (n *Notifications) MarkEventReadForUser(ctx context.Context, userID, eventID int64) (bool, error) {
	return n.markNotificationEventRead(ctx, NotificationScopeTenant, &userID, eventID)
}

// MarkSystemEventRead 标记单条系统级事件为已读；重复标记保持幂等成功。
func (n *Notifications) MarkSystemEventRead(ctx context.Context, eventID int64) (bool, error) {
	return n.markNotificationEventRead(ctx, NotificationScopeSystem, nil, eventID)
}

// markNotificationEventRead 按作用域与所有者标记单条事件已读，返回是否命中记录。
func (n *Notifications) markNotificationEventRead(ctx context.Context, scope string, ownerUserID *int64, eventID int64) (bool, error) {
	// query 保存带作用域与归属条件的已读更新语句。
	query := `UPDATE notification_events SET read_at=? WHERE id=? AND scope=?`
	// args 保存已读时间、事件主键与作用域。
	args := []any{time.Now().Unix(), eventID, scope}
	if ownerUserID != nil {
		query += ` AND owner_user_id=?`
		args = append(args, *ownerUserID)
	}
	// res、err 保存更新结果与数据库错误。
	res, err := n.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	// affected 是命中并更新的事件数量。
	affected, err := res.RowsAffected()
	return err == nil && affected == 1, err
}

// MarkAllEventsReadForUser 标记指定租户用户的全部未读事件为已读，返回更新数量。
func (n *Notifications) MarkAllEventsReadForUser(ctx context.Context, userID int64) (int64, error) {
	return n.markAllNotificationEventsRead(ctx, NotificationScopeTenant, &userID)
}

// MarkAllSystemEventsRead 标记全部未读系统事件为已读，返回更新数量。
func (n *Notifications) MarkAllSystemEventsRead(ctx context.Context) (int64, error) {
	return n.markAllNotificationEventsRead(ctx, NotificationScopeSystem, nil)
}

// markAllNotificationEventsRead 按作用域与所有者批量标记未读事件，返回受影响行数。
func (n *Notifications) markAllNotificationEventsRead(ctx context.Context, scope string, ownerUserID *int64) (int64, error) {
	// query 保存批量已读更新语句，只更新仍未读的记录。
	query := `UPDATE notification_events SET read_at=? WHERE scope=? AND read_at=0`
	// args 保存已读时间与作用域。
	args := []any{time.Now().Unix(), scope}
	if ownerUserID != nil {
		query += ` AND owner_user_id=?`
		args = append(args, *ownerUserID)
	}
	// res、err 保存批量更新结果与数据库错误。
	res, err := n.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
