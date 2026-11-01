package db

import (
	"context"
	"database/sql"
	"slices"
	"testing"
)

// TestMultiDB_ListEnabledCookieIDs 三方言下都必须排除显式关闭的账号并按账号升序。
// 该查询是 AI 启用链路自检的唯一数据来源：`COALESCE(ai_enabled, 1) = 1` 的写法若在某个方言上失效，
// 自检会静默判定「没有账号启用 AI」，所以必须在两种方言上都锁定。
// 「无设置行」的默认启用口径见 ai_master_switch_test.go。
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

// TestMultiDB_NonSwitchWritesKeepSourceGlobal 只保存护栏或 Agent 档位，不得让账号变成「显式设置开关」。
//
// 背景：接口契约里 source=account 的含义是「账号显式设置」（api/openapi.yaml）。
// 但写 ai_enabled 的通路有两条并不表达开关意愿：保存砍价护栏（SkipAIEnabled）与保存 Agent 档位。
// 这两条通路若把 ai_enabled 写成 1，行存在就会被误判成显式表态，
// 「只改过砍价护栏」的账号会被报成「账号显式开启」——这是接口对调用方撒谎。
// 因此未提交开关时必须落 NULL，显式标记按「列有值」判定。
func TestMultiDB_NonSwitchWritesKeepSourceGlobal(t *testing.T) {
	// target 是独立迁移后的方言数据库，清理由该目标子测试负责。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 只用于当前测试的本地仓储调用。
			ctx := context.Background()
			// userID 是该账号所有者；cookieID 是本用例的账号标识。
			userID, cookieID := seedAccount(t, target.store)

			// 通路一：只提交砍价护栏，不带开关。
			settings := defaultAIReplySettings(cookieID)
			settings.SkipAIEnabled = true
			settings.MaxDiscountPercent = 25
			// err 是护栏保存失败原因。
			if err := target.store.AIReply.UpsertSettings(ctx, cookieID, settings); err != nil {
				t.Fatalf("保存护栏: %v", err)
			}
			// raw 是 ai_enabled 列的原始值；Valid=false 表示落库为 NULL。
			var raw sql.NullInt64
			// err 是开关列读取失败原因。
			if err := target.store.DB.QueryRowContext(ctx,
				`SELECT ai_enabled FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&raw); err != nil {
				t.Fatalf("读取开关列: %v", err)
			}
			if raw.Valid {
				t.Fatalf("未提交开关时该列必须为 NULL，实际=%d", raw.Int64)
			}
			// cfg 是保存后的账号设置，用于断言生效值与显式标记。
			cfg, err := target.store.AIReply.Get(ctx, cookieID)
			if err != nil {
				t.Fatalf("读取设置: %v", err)
			}
			if !cfg.AIEnabled {
				t.Fatal("未提交开关时应按跟随总闸取 true，实际 false")
			}
			if cfg.AIEnabledExplicit {
				t.Fatal("未提交开关时应标记为跟随总闸，实际报成了显式设置")
			}
			if cfg.MaxDiscountPercent != 25 {
				t.Fatalf("护栏应已保存，max_discount_percent=%d", cfg.MaxDiscountPercent)
			}

			// 冲突分支：显式关过 AI 的账号再只保存护栏，开关值必须原样保留。
			// 这条同时守住 PostgreSQL 的方言陷阱：ON CONFLICT DO UPDATE 里若把 ai_enabled
			// 写进 SET 列表，PG 会因未限定列名报 SQLSTATE 42702，而 SQLite 能通过。
			if err := target.store.Cookies.Save(ctx, "keeps-off", "", userID); err != nil {
				t.Fatalf("写入账号: %v", err)
			}
			// offSettings 是只提交关闭开关的首次写入，建立显式 0 行。
			offSettings := defaultAIReplySettings("keeps-off")
			offSettings.AIEnabled = false
			// err 是显式关闭写入失败原因。
			if err := target.store.AIReply.UpsertSettings(ctx, "keeps-off", offSettings); err != nil {
				t.Fatalf("写入显式关闭: %v", err)
			}
			// skipSettings 是随后只改护栏、不带开关的写入。
			skipSettings := defaultAIReplySettings("keeps-off")
			skipSettings.SkipAIEnabled = true
			skipSettings.MaxBargainRounds = 5
			// err 是冲突分支写入失败原因。
			if err := target.store.AIReply.UpsertSettings(ctx, "keeps-off", skipSettings); err != nil {
				t.Fatalf("只改护栏时冲突分支失败: %v", err)
			}
			// kept 是冲突分支执行后的账号设置。
			kept, err := target.store.AIReply.Get(ctx, "keeps-off")
			if err != nil {
				t.Fatalf("读取保留值: %v", err)
			}
			if kept.AIEnabled || !kept.AIEnabledExplicit {
				t.Fatalf("用户显式关闭的决定必须保留：enabled=%v explicit=%v", kept.AIEnabled, kept.AIEnabledExplicit)
			}
			if kept.MaxBargainRounds != 5 {
				t.Fatalf("护栏应已更新，max_bargain_rounds=%d", kept.MaxBargainRounds)
			}

			// 通路二：只保存 Agent 档位（另一个账号，避免复用已有行）。
			// err 是第二个账号建档失败原因。
			if err := target.store.Cookies.Save(ctx, "agent-only", "", userID); err != nil {
				t.Fatalf("写入账号: %v", err)
			}
			// preset 是待写入的 Agent 档位。
			preset := "standard"
			// err 是档位保存失败原因。
			if _, err := target.store.AIReply.SetAgentConfig(ctx, userID, "agent-only", AgentAccountConfig{Preset: &preset}); err != nil {
				t.Fatalf("保存档位: %v", err)
			}
			// agentRaw 是档位通路写入后的开关列原始值。
			var agentRaw sql.NullInt64
			// err 是档位账号开关列读取失败原因。
			if err := target.store.DB.QueryRowContext(ctx,
				`SELECT ai_enabled FROM ai_reply_settings WHERE cookie_id=?`, "agent-only").Scan(&agentRaw); err != nil {
				t.Fatalf("读取档位账号开关列: %v", err)
			}
			if agentRaw.Valid {
				t.Fatalf("档位通路不得写入开关值，实际=%d", agentRaw.Int64)
			}

			// 列表接口与单条读取必须给出同一个显式标记。
			list, err := target.store.AIReply.ListForUser(ctx, userID)
			if err != nil {
				t.Fatalf("列取设置: %v", err)
			}
			// seen 记录各账号在列表中报出的显式标记。
			seen := map[string]bool{}
			// item 是列表中当前账号的设置项，其显式标记即接口要报出的 source 依据。
			for _, item := range list {
				seen[item.CookieID] = item.AIEnabledExplicit
			}
			// cid 是本用例要求「必须报为跟随总闸」的账号标识。
			for _, cid := range []string{cookieID, "agent-only"} {
				// explicit 是该账号在列表中报出的显式标记；ok 表示该账号是否出现在列表里。
				if explicit, ok := seen[cid]; !ok || explicit {
					t.Fatalf("账号 %s 在列表中应报为跟随总闸，actual=%v(存在=%v)", cid, explicit, ok)
				}
			}
		})
	}
}
