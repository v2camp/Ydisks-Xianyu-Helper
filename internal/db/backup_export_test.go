package db

import (
	"context"
	"testing"
)

// TestCardsAllForBackupKeepsCipherText 验证卡密备份导出保持 api_config 密文原样且包含全量行。
func TestCardsAllForBackupKeepsCipherText(t *testing.T) {
	// store、cleanup 是临时 SQLite 库及其清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是导出共用的请求上下文。
	ctx := context.Background()
	// 卡密表带 user 外键，先插入宿主用户行满足约束。
	if _, execErr := store.DB.Exec(`INSERT INTO users (id, username, email, password_hash) VALUES (1, 'backup-admin', 'backup@example.com', 'hash')`); execErr != nil {
		t.Fatalf("插入测试用户失败: %v", execErr)
	}
	// apiCfg 是写入数据库的密文形态模板；导出必须与它逐字节一致。
	apiCfg := "enc:v1:fake-cipher-text"
	// cardID 是插入的测试卡密标识。
	cardID, insertErr := store.Cards.Create(ctx, &CardFull{
		Name: "备份卡密", Type: "text", APIConfig: apiCfg, TextContent: "发货内容",
		Enabled: true, DelaySeconds: 2, UserID: 1,
	})
	if insertErr != nil {
		t.Fatalf("插入测试卡密失败: %v", insertErr)
	}
	// rows 是备份导出的原始行。
	rows, err := store.Cards.AllForBackup(ctx)
	if err != nil {
		t.Fatalf("备份导出失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应导出 1 行卡密, got %d", len(rows))
	}
	if rows[0].ID != cardID || rows[0].APIConfig != apiCfg {
		t.Fatalf("导出行应与写入行一致: id=%d apiConfig=%q", rows[0].ID, rows[0].APIConfig)
	}
	if !rows[0].Enabled || rows[0].DelaySeconds != 2 || rows[0].UserID != 1 {
		t.Fatalf("布尔与数值字段未正确还原: %+v", rows[0])
	}
}

// TestAutomationRulesAllRulesForBackup 验证自动化规则备份导出包含动作且按 id 升序。
func TestAutomationRulesAllRulesForBackup(t *testing.T) {
	// store、cleanup 是临时 SQLite 库及其清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是导出共用的请求上下文。
	ctx := context.Background()
	// 自动化规则表带 user 外键，先插入宿主用户行满足约束。
	if _, execErr := store.DB.Exec(`INSERT INTO users (id, username, email, password_hash) VALUES (1, 'backup-admin', 'backup@example.com', 'hash')`); execErr != nil {
		t.Fatalf("插入测试用户失败: %v", execErr)
	}
	// cookies 表是规则的外键宿主，先插入一条最小 cookie 行满足约束。
	if _, execErr := store.DB.Exec(`INSERT INTO cookies (id, value, user_id) VALUES ('cookie-a', 'enc:v1:fake', 1)`); execErr != nil {
		t.Fatalf("插入测试 cookie 失败: %v", execErr)
	}
	// rulesSQL 是最小化插入规则的语句；只填备份导出涉及的列。
	const rulesSQL = `INSERT INTO automation_rules
		(id, user_id, cookie_id, item_id, name, trigger_type, enabled, priority, config_json, sku_migration_status, created_at, updated_at)
		VALUES (?,?,?,?,'规则?','order_paid',1,10,'{}','migrated','2026-01-01 00:00:0?','2026-01-01 00:00:0?')`
	// actionsSQL 是最小化插入动作的语句。
	const actionsSQL = `INSERT INTO automation_rule_actions
		(rule_id, action_type, card_id, enabled, sort_order)
		VALUES (?,?,?,1,?)`
	// ruleID 是当前插入的规则标识；id 乱序写入以验证导出按 id 升序。
	for _, ruleID := range []int64{2, 1} {
		// err 是插入规则的数据库错误。
		_, err := store.DB.Exec(rulesSQL, ruleID, 1, "cookie-a", "item-1", ruleID)
		if err != nil {
			t.Fatalf("插入测试规则失败: %v", err)
		}
	}
	// sortIndex 是当前动作的排序序号。
	for _, sortIndex := range []int64{0, 1} {
		// err 是插入动作的数据库错误；card_id 传 NULL 规避卡密外键，导出断言不依赖该值。
		_, err := store.DB.Exec(actionsSQL, 1, "send_card", nil, sortIndex)
		if err != nil {
			t.Fatalf("插入测试动作失败: %v", err)
		}
	}
	// rows 是备份导出的规则列表。
	rows, err := store.Automation.AllRulesForBackup(ctx)
	if err != nil {
		t.Fatalf("备份导出失败: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != 1 || rows[1].ID != 2 {
		t.Fatalf("规则应按 id 升序导出: %+v", rows)
	}
	if len(rows[0].Actions) != 2 {
		t.Fatalf("首条规则应带 2 个动作, got %d", len(rows[0].Actions))
	}
	if rows[0].ConfigJSON != "{}" || !rows[0].Enabled {
		t.Fatalf("规则字段未正确还原: %+v", rows[0])
	}
}
