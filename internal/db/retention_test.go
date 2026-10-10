// retention_test.go 锁定统一保留清理的边界：只删除超出保留期的数据，保留期内数据不受影响；
// 各表按自身时间单位（毫秒、秒与时间列）正确比较，且只在会话已无消息时才删除该会话。

package db

import (
	"context"
	"testing"
	"time"
)

// TestCleanupRetentionDeletesOnlyExpired 验证 30 天保留清理只删超期行，并保留仍可回看的数据。
func TestCleanupRetentionDeletesOnlyExpired(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// _, cid 是测试账号的用户标识与账号标识；账号用于满足各表外键。
	_, cid := seedAccount(t, s)
	// expired 是超出 30 天保留期的历史时间点。
	expired := time.Now().AddDate(0, 0, -40)
	// fresh 是保留期内的近期时间点。
	fresh := time.Now().AddDate(0, 0, -1)
	// expiredMillis、freshMillis 是毫秒时间列使用的整数值。
	expiredMillis, freshMillis := expired.UnixMilli(), fresh.UnixMilli()
	// expiredSeconds、freshSeconds 是秒时间列使用的整数值。
	expiredSeconds, freshSeconds := expired.Unix(), fresh.Unix()

	// exec 是插入夹具行的通用执行函数；query 与 args 为待执行的 SQL 与参数。
	exec := func(query string, args ...any) {
		t.Helper()
		// err 表示夹具行插入失败的原因。
		if _, err := s.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("插入夹具失败 %q: %v", query, err)
		}
	}

	// —— 聊天域：时间列为 Unix 毫秒 ——
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		cid, "s-empty-old", "b1", "买家1", expiredMillis, expiredMillis, expiredMillis)
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		cid, "s-old-msg", "b2", "买家2", expiredMillis, expiredMillis, expiredMillis)
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		cid, "s-keep", "b3", "买家3", expiredMillis, expiredMillis, expiredMillis)
	exec(`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		cid, "s-fresh", "b4", "买家4", freshMillis, freshMillis, freshMillis)

	exec(`INSERT INTO chat_messages (cookie_id,chat_id,message_key,direction,sent_at,created_at) VALUES (?,?,?,?,?,?)`,
		cid, "s-old-msg", "m-old", "incoming", expiredMillis, expiredMillis)
	exec(`INSERT INTO chat_messages (cookie_id,chat_id,message_key,direction,sent_at,created_at) VALUES (?,?,?,?,?,?)`,
		cid, "s-keep", "m-keep", "incoming", freshMillis, freshMillis)
	exec(`INSERT INTO chat_messages (cookie_id,chat_id,message_key,direction,sent_at,created_at) VALUES (?,?,?,?,?,?)`,
		cid, "s-fresh", "m-fresh", "incoming", freshMillis, freshMillis)

	// —— 其余日志表：每张各一条超期与一条保留 ——
	exec(`INSERT INTO ws_messages (cookie_id,raw_text,created_at) VALUES (?,?,?)`, cid, "ws-old", expired)
	exec(`INSERT INTO ws_messages (cookie_id,raw_text,created_at) VALUES (?,?,?)`, cid, "ws-fresh", fresh)
	exec(`INSERT INTO account_login_logs (cookie_id,status,created_at) VALUES (?,?,?)`, cid, "success", expiredSeconds)
	exec(`INSERT INTO account_login_logs (cookie_id,status,created_at) VALUES (?,?,?)`, cid, "success", freshSeconds)
	exec(`INSERT INTO risk_control_logs (cookie_id,event_type,created_at) VALUES (?,?,?)`, cid, "slider_captcha", expired)
	exec(`INSERT INTO risk_control_logs (cookie_id,event_type,created_at) VALUES (?,?,?)`, cid, "slider_captcha", fresh)
	exec(`INSERT INTO security_audit_logs (user_id,action,resource,created_at) VALUES (?,?,?,?)`, 1, "read", "ai_api_key", expiredSeconds)
	exec(`INSERT INTO security_audit_logs (user_id,action,resource,created_at) VALUES (?,?,?,?)`, 1, "read", "ai_api_key", freshSeconds)
	exec(`INSERT INTO mcp_call_audit (created_at,token_source,category,name,success) VALUES (?,?,?,?,?)`, expiredSeconds, "current", "tool", "search", 1)
	exec(`INSERT INTO mcp_call_audit (created_at,token_source,category,name,success) VALUES (?,?,?,?,?)`, freshSeconds, "current", "tool", "search", 1)
	exec(`INSERT INTO scheduled_cookies_refresh_log (cookie_id,status,created_at) VALUES (?,?,?)`, cid, "success", expired)
	exec(`INSERT INTO scheduled_cookies_refresh_log (cookie_id,status,created_at) VALUES (?,?,?)`, cid, "success", fresh)

	// report、err 是本次清理的逐表删除行数与失败原因。
	report, err := s.CleanupRetention(ctx, 30)
	if err != nil {
		t.Fatalf("CleanupRetention: %v", err)
	}
	// 聊天域只删超期消息；会话删「超期空会话」与「超期且消息已删」两条，仍可回看的会话保留。
	if report.ChatMessages != 1 || report.ChatSessions != 2 {
		t.Fatalf("聊天域删除行数异常: %+v", report)
	}
	// 其余各表各删一条超期行。
	if report.WSMessages != 1 || report.AccountLoginLogs != 1 || report.RiskControlLogs != 1 ||
		report.SecurityAuditLogs != 1 || report.MCPCallAudit != 1 || report.RenewalLogs != 1 {
		t.Fatalf("日志表删除行数异常: %+v", report)
	}
	if report.Total() != 9 {
		t.Fatalf("总删除行数=%d want 9", report.Total())
	}

	// 保留期内数据与仍可回看的会话必须原样保留。
	countRows(t, s, `SELECT COUNT(*) FROM chat_sessions WHERE chat_id='s-fresh'`, 1, "近期会话应保留")
	countRows(t, s, `SELECT COUNT(*) FROM chat_sessions WHERE chat_id='s-keep'`, 1, "仍有消息的会话应保留")
	countRows(t, s, `SELECT COUNT(*) FROM chat_sessions WHERE chat_id IN ('s-empty-old','s-old-msg')`, 0, "超期空会话应删除")
	countRows(t, s, `SELECT COUNT(*) FROM chat_messages`, 2, "仅保留两条近期消息")
	countRows(t, s, `SELECT COUNT(*) FROM ws_messages`, 1, "仅保留一条近期诊断帧")
	countRows(t, s, `SELECT COUNT(*) FROM account_login_logs`, 1, "仅保留一条近期登录审计")
	countRows(t, s, `SELECT COUNT(*) FROM risk_control_logs`, 1, "仅保留一条近期风控日志")
	countRows(t, s, `SELECT COUNT(*) FROM security_audit_logs`, 1, "仅保留一条近期敏感访问审计")
	countRows(t, s, `SELECT COUNT(*) FROM mcp_call_audit`, 1, "仅保留一条近期 MCP 调用审计")
	countRows(t, s, `SELECT COUNT(*) FROM scheduled_cookies_refresh_log`, 1, "仅保留一条近期续期日志")
}

// TestCleanupRetentionSkipsWhenDisabled 验证非正保留天数整体跳过，不做任何删除。
func TestCleanupRetentionSkipsWhenDisabled(t *testing.T) {
	// s、cleanup 是隔离的 SQLite 测试库与关闭函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// _, cid 是测试账号的用户标识与账号标识。
	_, cid := seedAccount(t, s)
	// oldMillis 是必然超期的时间戳，用于确认保留期设为 0 时不会被清理。
	oldMillis := time.Now().AddDate(0, 0, -400).UnixMilli()
	// err 表示夹具会话插入失败的原因；会话用于满足消息表的外键约束。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO chat_sessions (cookie_id,chat_id,buyer_id,buyer_name,last_message_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		cid, "s-disabled", "b1", "买家", oldMillis, oldMillis, oldMillis); err != nil {
		t.Fatalf("插入夹具会话: %v", err)
	}
	// err 表示夹具消息插入失败的原因。
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO chat_messages (cookie_id,chat_id,message_key,direction,sent_at,created_at) VALUES (?,?,?,?,?,?)`,
		cid, "s-disabled", "m-old", "incoming", oldMillis, oldMillis); err != nil {
		t.Fatalf("插入夹具消息: %v", err)
	}
	// report、err 是禁用态下的清理结果，应为空报告且无错误。
	report, err := s.CleanupRetention(ctx, 0)
	if err != nil {
		t.Fatalf("CleanupRetention: %v", err)
	}
	if report.Total() != 0 {
		t.Fatalf("保留期为 0 时不应删除任何行: %+v", report)
	}
	countRows(t, s, `SELECT COUNT(*) FROM chat_messages`, 1, "禁用态下消息应保留")
}

// countRows 断言查询返回的计数等于期望值，不等则终止当前测试。
func countRows(t *testing.T, s *Store, query string, want int, message string) {
	t.Helper()
	// got 是查询返回的计数；err 表示计数查询失败的原因。
	var got int
	// err 是计数查询失败的原因。
	if err := s.DB.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("%s: 计数查询失败 %v", message, err)
	}
	if got != want {
		t.Fatalf("%s: 计数=%d want %d", message, got, want)
	}
}
