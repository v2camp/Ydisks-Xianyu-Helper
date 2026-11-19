// chat_takeover.go 提供客服 Agent 接管过的聊天会话查询。
//
// 接管标记只取自 ai_conversations 的 assistant 记录：ai_decision_log 会被保留策略清理，
// 不能作为长期会话历史依据。归属过滤与聊天列表一致，始终由 cookies.user_id 保证。

package db

import "context"

// ChatTakeoverSession 是客服 Agent 接管过的会话摘要，内嵌普通会话并附带接管轮次近似值。
type ChatTakeoverSession struct {
	// ChatSession 是会话的非敏感摘要；字段语义与聊天列表一致。
	ChatSession
	// TakeoverCount 是该会话中 AI 生成过回复的轮次数，来自 ai_conversations 的 assistant 记录。
	TakeoverCount int64
}

// ChatTakeoverSessionPage 是客服接管会话的键集分页结果。
type ChatTakeoverSessionPage struct {
	// Sessions 是按最后消息时间与会话标识倒序排列的当前页结果。
	Sessions []ChatTakeoverSession
	// HasMore 表示是否还有下一页会话。
	HasMore bool
	// NextCursor 是继续读取下一页所需的最后排序键；没有下一页时为 nil。
	NextCursor *ChatSessionCursor
}

// ListTakeoverSessions 查询当前用户范围内被 AI 生成过回复的会话，附带 assistant 轮次数。
// cursor 为空时读取首页；limit 的默认值和最大值均为 200。
// 接管标记只取自 ai_conversations：ai_decision_log 会被保留策略清理，不能作为长期历史依据。
func (s *ChatStore) ListTakeoverSessions(ctx context.Context, userID int64, cursor *ChatSessionCursor, limit int) (ChatTakeoverSessionPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	// query 保存归属过滤、接管聚合、可选键集条件和稳定排序共同构成的会话分页 SQL。
	query := `SELECT cs.cookie_id,cs.chat_id,cs.buyer_id,cs.buyer_name,cs.buyer_avatar_url,
		cs.item_id,cs.item_title,cs.item_image_url,cs.last_message,cs.last_message_at,cs.unread_count,
		cs.account_role,cs.buyer_user_id,cs.seller_user_id,cs.role_item_id,cs.role_source,
		COUNT(ac.id) AS takeover_count
		FROM chat_sessions cs
		JOIN cookies c ON c.id=cs.cookie_id
		JOIN ai_conversations ac ON ac.cookie_id=cs.cookie_id AND ac.chat_id=cs.chat_id AND ac.role='assistant'
		WHERE c.user_id=? AND cs.is_visible=? AND cs.user_hidden_at=0`
	// args 保存与会话分页 SQL 占位符严格对应的非敏感查询参数。
	args := []any{userID, true}
	if cursor != nil {
		query += ` AND (cs.last_message_at<? OR (cs.last_message_at=? AND cs.chat_id<?))`
		args = append(args, cursor.LastMessageAt, cursor.LastMessageAt, cursor.ChatID)
	}
	query += ` GROUP BY cs.cookie_id,cs.chat_id ORDER BY cs.last_message_at DESC,cs.chat_id DESC LIMIT ?`
	args = append(args, limit+1)
	// rows、queryErr 保存数据库游标及执行键集查询时的错误。
	rows, queryErr := s.DB.QueryContext(ctx, query, args...)
	if queryErr != nil {
		return ChatTakeoverSessionPage{}, queryErr
	}
	defer rows.Close()
	// sessions 保存最多 limit 加一条的扫描结果，用额外记录判断是否还有下一页。
	sessions := make([]ChatTakeoverSession, 0, limit+1)
	for rows.Next() {
		// session 保存当前扫描出的单个接管会话摘要。
		var session ChatTakeoverSession
		// scanErr 保存当前数据库行映射到接管会话时的错误。
		if scanErr := rows.Scan(&session.CookieID, &session.ChatID, &session.BuyerID, &session.BuyerName, &session.BuyerAvatar,
			&session.ItemID, &session.ItemTitle, &session.ItemImageURL, &session.LastMessage, &session.LastMessageAt, &session.UnreadCount,
			&session.AccountRole, &session.BuyerUserID, &session.SellerUserID, &session.RoleItemID, &session.RoleSource,
			&session.TakeoverCount); scanErr != nil {
			return ChatTakeoverSessionPage{}, scanErr
		}
		sessions = append(sessions, session)
	}
	// iterationErr 保存数据库驱动在耗尽行流时报告的错误。
	if iterationErr := rows.Err(); iterationErr != nil {
		return ChatTakeoverSessionPage{}, iterationErr
	}
	// page 保存裁剪额外探测行后的分页响应。
	page := ChatTakeoverSessionPage{Sessions: sessions}
	if len(sessions) > limit {
		page.HasMore = true
		page.Sessions = sessions[:limit]
	}
	if len(page.Sessions) > 0 && page.HasMore {
		// last 保存当前页最后一条会话，其排序键作为下一页的游标来源。
		last := page.Sessions[len(page.Sessions)-1]
		page.NextCursor = &ChatSessionCursor{LastMessageAt: last.LastMessageAt, ChatID: last.ChatID}
	}
	return page, nil
}
