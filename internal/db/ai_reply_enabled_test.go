package db

import (
	"context"
	"slices"
	"testing"
)

// TestMultiDB_ListEnabledCookieIDs 三方言下都必须只返回启用行并按账号升序。
// 该查询是 AI 启用链路自检的唯一数据来源：`ai_enabled = 1` 的写法若在某个方言上失效，
// 自检会静默判定「没有账号启用 AI」，所以必须在两种方言上都锁定。
func TestMultiDB_ListEnabledCookieIDs(t *testing.T) {
	// target 是独立迁移后的方言数据库，清理由该目标子测试负责。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 只用于当前测试的本地仓储调用。
			ctx := context.Background()
			// userID 是测试账号所有者；cookieID 仅作外键占位，不参与本用例断言。
			userID, cookieID := seedAccount(t, target.store)
			// 三个账号：两个启用、一个关闭；凭证留空，避免触达真实账号。
			for _, cid := range []string{cookieID, "ai-b", "ai-off"} {
				// saveErr 是当前账号写入失败原因。
				if saveErr := target.store.Cookies.Save(ctx, cid, "", userID); saveErr != nil {
					t.Fatal(saveErr)
				}
			}
			// row 是当前写入的开关状态：整数 1/0 与迁移中的 INTEGER 列一致。
			for _, row := range []struct {
				cid     string
				enabled int
			}{
				{cid: cookieID, enabled: 1},
				{cid: "ai-b", enabled: 1},
				{cid: "ai-off", enabled: 0},
			} {
				// err 是当前开关行写入失败原因。
				if _, err := target.store.DB.ExecContext(ctx,
					`INSERT INTO ai_reply_settings (cookie_id, ai_enabled) VALUES (?, ?)`, row.cid, row.enabled); err != nil {
					t.Fatal(err)
				}
			}
			// ids、err 是查询到的启用账号清单与失败原因。
			ids, err := target.store.AIReply.ListEnabledCookieIDs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// want 是按账号升序的期望清单，关闭账号不得出现。
			want := []string{"ai-b", cookieID}
			slices.Sort(want)
			if !slices.Equal(ids, want) {
				t.Fatalf("启用账号清单不符：got=%v want=%v", ids, want)
			}
		})
	}
}
