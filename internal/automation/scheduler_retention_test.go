// scheduler_retention_test.go 锁定计划调度器里的保留清理：读取统一设置、按天节流，
// 并真实删除超期行；节流与设置项边界都在此覆盖，避免分钟级扫描反复清理或漏清理。

package automation

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestRunRetentionCleanupThrottlesAndUsesSettings 验证保留清理读取统一设置、按天节流并删除超期行。
func TestRunRetentionCleanupThrottlesAndUsesSettings(t *testing.T) {
	// store、cleanup 保存本次测试使用的自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是清理用例共用的数据库上下文。
	ctx := context.Background()
	// center 是持有存储的自动化中心。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// scheduler 是待验证的计划任务调度器。
	scheduler := NewScheduler(center)
	// expiredSeconds 是超出保留期的审计时间戳（秒）；敏感访问审计使用秒级 BIGINT。
	expiredSeconds := time.Now().AddDate(0, 0, -40).Unix()

	// insertAudit 写入一条超期敏感访问审计，返回插入失败原因。
	insertAudit := func() error {
		// err 是审计行插入失败的原因。
		_, err := store.DB.ExecContext(ctx,
			`INSERT INTO security_audit_logs (user_id,action,resource,created_at) VALUES (?,?,?,?)`,
			1, "read", "ai_api_key", expiredSeconds)
		return err
	}
	// countAudits 返回当前敏感访问审计行数，用于观察清理效果。
	countAudits := func() int {
		// n 是当前敏感访问审计行数。
		var n int
		// err 是计数查询失败的原因。
		if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_logs`).Scan(&n); err != nil {
			t.Fatalf("计数敏感访问审计: %v", err)
		}
		return n
	}
	// expireThrottle 把上次执行时间前移到间隔之外，使下一次调用立即生效。
	expireThrottle := func() {
		scheduler.retentionMu.Lock()
		scheduler.lastRetentionRun = time.Now().Add(-retentionCleanupInterval - time.Minute)
		scheduler.retentionMu.Unlock()
	}

	// 默认保留天数来自迁移写入的 log_retention_days=30，40 天前的行应被清理。
	// err 是夹具审计行插入失败的原因。
	if err := insertAudit(); err != nil {
		t.Fatalf("插入超期审计: %v", err)
	}
	scheduler.runRetentionCleanup(ctx)
	// remaining 是首次清理后的审计行数，应归零。
	remaining := countAudits()
	if remaining != 0 {
		t.Fatalf("首次清理应删除超期审计，剩余=%d", remaining)
	}
	// 第二次清理被按天节流跳过，新插入的超期行保持存在。
	// err 是夹具审计行插入失败的原因。
	if err := insertAudit(); err != nil {
		t.Fatalf("再次插入超期审计: %v", err)
	}
	scheduler.runRetentionCleanup(ctx)
	// throttled 是节流期内清理后的审计行数，应仍为 1。
	throttled := countAudits()
	if throttled != 1 {
		t.Fatalf("节流期内不应清理，剩余=%d，want 1", throttled)
	}
	// 把上次执行时间前移超过一个间隔后，清理重新生效。
	expireThrottle()
	scheduler.runRetentionCleanup(ctx)
	// afterInterval 是重新清理后的审计行数，应归零。
	afterInterval := countAudits()
	if afterInterval != 0 {
		t.Fatalf("超过间隔后应重新清理，剩余=%d", afterInterval)
	}
	// 保留天数改为 60 天后，40 天前的行属于保留期内，不应被删除。
	// err 是保留天数设置写入失败的原因。
	if err := store.Settings.Set(ctx, db.RetentionPolicyDaysKey, "60"); err != nil {
		t.Fatalf("设置保留天数: %v", err)
	}
	// err 是夹具审计行插入失败的原因。
	if err := insertAudit(); err != nil {
		t.Fatalf("插入超期审计: %v", err)
	}
	expireThrottle()
	scheduler.runRetentionCleanup(ctx)
	// kept 是放宽保留期后的审计行数，应保持 1。
	kept := countAudits()
	if kept != 1 {
		t.Fatalf("保留期 60 天时不应删除 40 天前的行，剩余=%d，want 1", kept)
	}
	// 设置项非法时回落到默认保留天数。
	// err 是非法设置写入失败的原因。
	if err := store.Settings.Set(ctx, db.RetentionPolicyDaysKey, "abc"); err != nil {
		t.Fatalf("设置非法保留天数: %v", err)
	}
	// fallbackDays 是非法设置下解析出的保留天数。
	fallbackDays := scheduler.retentionDaysValue(ctx)
	if fallbackDays != db.DefaultRetentionDays {
		t.Fatalf("非法设置应回落默认值，got=%d want %d", fallbackDays, db.DefaultRetentionDays)
	}
}
