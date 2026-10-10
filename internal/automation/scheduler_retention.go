// scheduler_retention.go 计划调度器中的消息与日志保留清理：按统一设置的天数删除超期数据。
// 独立成文件是为了让 scheduler.go 只保留计划任务扫描骨架，避免单文件超过架构门禁上限。

package automation

import (
	"context"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// retentionCleanupInterval 是消息与日志保留清理的执行间隔：分钟级扫描只做节流判断，
// 真正执行 DELETE 的间隔为一天，避免高频清理争抢连接池。
const retentionCleanupInterval = 24 * time.Hour

// runRetentionCleanup 按配置天数清理超期消息与日志，未到执行间隔时直接返回。
// 设置项缺失或非法时使用默认保留天数；清理失败只记录告警，不阻断计划任务扫描。
func (s *Scheduler) runRetentionCleanup(ctx context.Context) {
	if s == nil || s.center == nil || s.center.store == nil {
		return
	}
	// retentionDays 是本次生效的保留天数。
	retentionDays := s.retentionDaysValue(ctx)
	s.retentionMu.Lock()
	// due 表示距上次清理是否已达执行间隔；首次运行视为到期。
	due := s.lastRetentionRun.IsZero() || time.Since(s.lastRetentionRun) >= retentionCleanupInterval
	if due {
		// 先占位再执行，避免慢清理期间被后续扫描重复触发。
		s.lastRetentionRun = time.Now()
	}
	s.retentionMu.Unlock()
	if !due {
		return
	}
	// report、err 是本次保留清理的逐表删除行数与失败原因。
	report, err := s.center.store.CleanupRetention(ctx, retentionDays)
	if err != nil {
		s.center.logger.Warn("清理超期消息与日志失败", "retention_days", retentionDays, "err", err)
		return
	}
	if report.Total() > 0 {
		s.center.logger.Info("已清理超期消息与日志", "retention_days", retentionDays,
			"chat_messages", report.ChatMessages, "chat_sessions", report.ChatSessions,
			"ws_messages", report.WSMessages, "login_logs", report.AccountLoginLogs,
			"risk_logs", report.RiskControlLogs, "audit_logs", report.SecurityAuditLogs,
			"mcp_audits", report.MCPCallAudit, "renewal_logs", report.RenewalLogs)
	}
}

// retentionDaysValue 读取统一的日志保留天数，缺失或非法时回落到默认值。
func (s *Scheduler) retentionDaysValue(ctx context.Context) int {
	// settings 是系统设置存储；未装配时直接使用默认保留天数。
	settings := s.center.store.Settings
	if settings == nil {
		return db.DefaultRetentionDays
	}
	// value、err 是保留天数设置的原始值与读取错误。
	value, err := settings.Get(ctx, db.RetentionPolicyDaysKey)
	if err != nil {
		return db.DefaultRetentionDays
	}
	// days、convErr 是解析后的保留天数与解析错误；非正数视为未配置。
	days, convErr := strconv.Atoi(strings.TrimSpace(value))
	if convErr != nil || days <= 0 {
		return db.DefaultRetentionDays
	}
	return days
}
