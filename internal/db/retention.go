// retention.go 统一的消息与日志保留策略：按配置天数清理超期数据，避免本地库无界增长。
// 各表时间列形态并不一致——聊天域是 Unix 毫秒，审计域是 Unix 秒，诊断与续期日志是
// CURRENT_TIMESTAMP 时间列，因此这里逐表使用对应形态的 cutoff，不让「同一字段两种单位」
// 的历史问题在清理侧重演。

package db

import (
	"context"
	"time"
)

// RetentionPolicyDaysKey 是系统设置中控制消息与日志保留天数的键。
const RetentionPolicyDaysKey = "log_retention_days"

// DefaultRetentionDays 是保留策略未配置时的默认天数，取一个月。
// 用户约定：至少保留 10 天以覆盖周趋势分析，存储无压力时保留 30 天。
const DefaultRetentionDays = 30

// RetentionReport 汇总一次保留清理的删除行数，供运行日志观测与测试断言。
type RetentionReport struct {
	// ChatMessages 是本次删除的超期聊天消息行数。
	ChatMessages int64
	// ChatSessions 是本次删除的无消息超期会话行数。
	ChatSessions int64
	// WSMessages 是本次删除的超期 WS 诊断帧行数。
	WSMessages int64
	// AccountLoginLogs 是本次删除的超期登录审计行数。
	AccountLoginLogs int64
	// RiskControlLogs 是本次删除的超期风控日志行数。
	RiskControlLogs int64
	// SecurityAuditLogs 是本次删除的超期敏感访问审计行数。
	SecurityAuditLogs int64
	// MCPCallAudit 是本次删除的超期 MCP 调用审计行数。
	MCPCallAudit int64
	// RenewalLogs 是本次删除的超期续期日志行数。
	RenewalLogs int64
}

// Total 返回本次清理删除的总行数。
func (r RetentionReport) Total() int64 {
	return r.ChatMessages + r.ChatSessions + r.WSMessages + r.AccountLoginLogs +
		r.RiskControlLogs + r.SecurityAuditLogs + r.MCPCallAudit + r.RenewalLogs
}

// CleanupRetention 按保留天数清理消息与全部日志表，返回逐表删除行数。
// retentionDays 非正数时整体跳过并返回空报告，便于把保留期设为 0 表示不清理。
// 清理是幂等的：任一步骤失败返回错误，已完成的删除不回滚，下次运行可继续。
func (s *Store) CleanupRetention(ctx context.Context, retentionDays int) (RetentionReport, error) {
	// report 汇总逐表删除行数。
	var report RetentionReport
	if s == nil || s.DB == nil || retentionDays <= 0 {
		return report, nil
	}
	// cutoff 是统一的保留时间下界，按需投影为毫秒、秒与时间三种形态。
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	// cutoffMillis 对应聊天域的 Unix 毫秒列（chat_messages.created_at、chat_sessions.last_message_at）。
	cutoffMillis := cutoff.UnixMilli()
	// cutoffSeconds 对应审计域的 Unix 秒列（登录、敏感访问与 MCP 审计）。
	cutoffSeconds := cutoff.Unix()

	// 聊天消息按毫秒列删除；先删消息再删空会话，保证会话判定读到删除后的真实消息集。
	messages, err := s.deleteBefore(ctx, `DELETE FROM chat_messages WHERE created_at < ?`, cutoffMillis)
	if err != nil {
		return report, err
	}
	report.ChatMessages = messages
	// 只删除「已无任何消息」且最后消息时间超期的会话，避免误删仍可回看的会话。
	sessions, err := s.deleteBefore(ctx, `DELETE FROM chat_sessions
 WHERE last_message_at > 0 AND last_message_at < ?
   AND NOT EXISTS (SELECT 1 FROM chat_messages m
                    WHERE m.cookie_id = chat_sessions.cookie_id
                      AND m.chat_id = chat_sessions.chat_id)`, cutoffMillis)
	if err != nil {
		return report, err
	}
	report.ChatSessions = sessions

	// WS 诊断帧的时间列由 CURRENT_TIMESTAMP 写入，cutoff 直接用时间值比较。
	wsMessages, err := s.deleteBefore(ctx, `DELETE FROM ws_messages WHERE created_at < ?`, cutoff)
	if err != nil {
		return report, err
	}
	report.WSMessages = wsMessages

	// 登录审计的 created_at 是 Unix 秒。
	loginLogs, err := s.deleteBefore(ctx, `DELETE FROM account_login_logs WHERE created_at < ?`, cutoffSeconds)
	if err != nil {
		return report, err
	}
	report.AccountLoginLogs = loginLogs

	// 风控日志的时间列由 CURRENT_TIMESTAMP 写入。
	riskLogs, err := s.deleteBefore(ctx, `DELETE FROM risk_control_logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return report, err
	}
	report.RiskControlLogs = riskLogs

	// 敏感访问审计的 created_at 是 Unix 秒。
	securityAudits, err := s.deleteBefore(ctx, `DELETE FROM security_audit_logs WHERE created_at < ?`, cutoffSeconds)
	if err != nil {
		return report, err
	}
	report.SecurityAuditLogs = securityAudits

	// MCP 调用审计的 created_at 是 Unix 秒。
	mcpAudits, err := s.deleteBefore(ctx, `DELETE FROM mcp_call_audit WHERE created_at < ?`, cutoffSeconds)
	if err != nil {
		return report, err
	}
	report.MCPCallAudit = mcpAudits

	// 续期三张日志表的时间列由 CURRENT_TIMESTAMP 写入，统一按时间下界清理。
	renewalLogs := int64(0)
	// table 表示当前遍历到的续期日志表名。
	for _, table := range []string{
		"scheduled_cookies_refresh_log",
		"scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log",
	} {
		// deleted 是当前续期日志表删除的行数；err 是删除失败的原因。
		deleted, err := s.deleteBefore(ctx, `DELETE FROM `+table+` WHERE created_at < ?`, cutoff)
		if err != nil {
			return report, err
		}
		renewalLogs += deleted
	}
	report.RenewalLogs = renewalLogs
	return report, nil
}

// deleteBefore 执行一条带单个时间下界参数的删除语句并返回删除行数。
func (s *Store) deleteBefore(ctx context.Context, query string, bound any) (int64, error) {
	// result 提供受影响行数；err 表示删除语句执行失败。
	result, err := s.DB.ExecContext(ctx, query, bound)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
