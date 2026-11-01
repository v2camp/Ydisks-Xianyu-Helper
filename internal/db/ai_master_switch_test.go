// ai_master_switch_test.go 锁定「无设置行 = 跟随全局总闸」的数据库口径。
//
// 三个必须锁死的点：
//  1. 无行账号默认启用，显式关闭的账号被排除，显式开启的账号保留；
//  2. 保存砍价护栏但不提交开关时，已有显式值必须原样保留；
//  3. 总闸读取走普通读，不得在 security_audit_logs 留下记录。
package db

import (
	"context"
	"slices"
	"testing"
)

// TestMultiDB_ListEnabledCookieIDsFollowsMasterDefault 三方言下「无行账号」必须算启用。
//
// 用例放了三种账号：无设置行、显式开启、显式关闭。旧 SQL（`WHERE ai_enabled = 1`）
// 会漏掉无行账号，导致自检与评测误判「没有账号启用 AI」。
func TestMultiDB_ListEnabledCookieIDsFollowsMasterDefault(t *testing.T) {
	// target 是独立迁移后的方言数据库，清理由该目标子测试负责。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 只用于当前测试的本地仓储调用。
			ctx := context.Background()
			// userID 是测试账号所有者；cookieID 由 seedAccount 生成的既有账号。
			userID, cookieID := seedAccount(t, target.store)
			// 账号集合：既有账号保持无设置行，另外两个分别写入显式开启与显式关闭。
			for _, cid := range []string{"ai-explicit-on", "ai-explicit-off"} {
				// saveErr 是当前账号写入失败原因。
				if saveErr := target.store.Cookies.Save(ctx, cid, "", userID); saveErr != nil {
					t.Fatal(saveErr)
				}
			}
			// row 是当前写入的显式开关行；整数 1/0 与迁移中的 INTEGER 列一致。
			for _, row := range []struct {
				cid     string
				enabled int
			}{
				{cid: "ai-explicit-on", enabled: 1},
				{cid: "ai-explicit-off", enabled: 0},
			} {
				// err 是当前开关行写入失败原因；必须写整数，pgx 不接受布尔二进制编码。
				if _, err := target.store.DB.ExecContext(ctx,
					`INSERT INTO ai_reply_settings (cookie_id, ai_enabled) VALUES (?, ?)`, row.cid, row.enabled); err != nil {
					t.Fatal(err)
				}
			}
			// ids、err 是查询到的账号级启用清单与失败原因。
			ids, err := target.store.AIReply.ListEnabledCookieIDs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// want 是期望清单：无行账号与显式开启账号，显式关闭账号不得出现。
			want := []string{"ai-explicit-on", cookieID}
			slices.Sort(want)
			if !slices.Equal(ids, want) {
				t.Fatalf("启用账号清单不符：got=%v want=%v", ids, want)
			}
		})
	}
}

// TestMultiDB_AIReplyMasterSwitchMatrix 三方言下 Get/IsEnabled 的默认开启口径。
// 断言读回的实际整数值，避免只验证「查询无错误」而漏掉布尔编码问题。
func TestMultiDB_AIReplyMasterSwitchMatrix(t *testing.T) {
	// target 是独立迁移后的方言数据库。
	for _, target := range allTestTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 只用于当前测试的本地仓储调用。
			ctx := context.Background()
			// userID 是测试账号所有者；noRowID 是没有设置行的账号。
			userID, noRowID := seedAccount(t, target.store)
			// explicitOffID 是显式关闭的账号。
			const explicitOffID = "ai-off"
			// saveErr 是显式关闭账号写入失败原因。
			if saveErr := target.store.Cookies.Save(ctx, explicitOffID, "", userID); saveErr != nil {
				t.Fatal(saveErr)
			}
			// _, writeErr 是显式关闭行的写入结果；0 必须以 INTEGER 落库。
			if _, writeErr := target.store.DB.ExecContext(ctx,
				`INSERT INTO ai_reply_settings (cookie_id, ai_enabled) VALUES (?, 0)`, explicitOffID); writeErr != nil {
				t.Fatal(writeErr)
			}
			// noRowCfg、noRowErr 是无行账号读出的默认配置。
			noRowCfg, noRowErr := target.store.AIReply.Get(ctx, noRowID)
			if noRowErr != nil || noRowCfg == nil {
				t.Fatalf("无行读取应返回默认配置: cfg=%+v err=%v", noRowCfg, noRowErr)
			}
			if !noRowCfg.AIEnabled || noRowCfg.AIEnabledExplicit {
				t.Fatalf("无行账号应跟随总闸且非显式: %+v", noRowCfg)
			}
			// offCfg、offErr 是显式关闭账号读出的配置。
			offCfg, offErr := target.store.AIReply.Get(ctx, explicitOffID)
			if offErr != nil || offCfg == nil {
				t.Fatalf("显式关闭读取失败: cfg=%+v err=%v", offCfg, offErr)
			}
			if offCfg.AIEnabled || !offCfg.AIEnabledExplicit {
				t.Fatalf("显式关闭账号应读到 false 且标记显式: %+v", offCfg)
			}
			// noRowEnabled、noRowEnabledErr 是无行账号的生效判定。
			noRowEnabled, noRowEnabledErr := target.store.AIReply.IsEnabled(ctx, noRowID)
			if noRowEnabledErr != nil || !noRowEnabled {
				t.Fatalf("无行账号默认应生效: enabled=%v err=%v", noRowEnabled, noRowEnabledErr)
			}
			// offEnabled、offEnabledErr 是显式关闭账号的生效判定。
			offEnabled, offEnabledErr := target.store.AIReply.IsEnabled(ctx, explicitOffID)
			if offEnabledErr != nil || offEnabled {
				t.Fatalf("显式关闭账号不应生效: enabled=%v err=%v", offEnabled, offEnabledErr)
			}
			// listed、listErr 是用户维度的设置列表：无行账号同样必须出现。
			listed, listErr := target.store.AIReply.ListForUser(ctx, userID)
			if listErr != nil {
				t.Fatal(listErr)
			}
			// listedByID 是账号标识到设置摘要的索引，便于逐账号断言。
			listedByID := make(map[string]AIReplySettings, len(listed))
			// item 是当前待索引的账号设置摘要。
			for _, item := range listed {
				listedByID[item.CookieID] = item
			}
			// hasNoRow 表示无设置行的账号是否出现在列表里。
			if _, hasNoRow := listedByID[noRowID]; !hasNoRow {
				t.Fatalf("无行账号必须出现在列表里: %+v", listed)
			}
			// row 是显式关闭账号在列表里的值；found 表示该账号是否出现。
			row, found := listedByID[explicitOffID]
			if !found || row.AIEnabled || !row.AIEnabledExplicit {
				t.Fatalf("显式关闭账号列表值不符: %+v", row)
			}
		})
	}
}

// TestAIMasterEnabledTogglesEffectiveValue 总闸关闭时，连显式开启的账号也不得生效。
func TestAIMasterEnabledTogglesEffectiveValue(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 是写入显式开启设置行的账号。
	userID, cookieID := seedAccount(t, store)
	// saveErr 是显式开启行写入失败原因；显式值必须以 INTEGER 写入。
	if saveErr := store.AIReply.UpsertSettings(ctx, cookieID, AIReplySettings{AIEnabled: true, MaxBargainRounds: 3}); saveErr != nil {
		t.Fatal(saveErr)
	}
	// 默认（未配置总闸）应视为开启。
	if master, masterErr := store.AIReply.MasterEnabled(ctx); masterErr != nil || !master {
		t.Fatalf("未配置总闸应按开启处理: master=%v err=%v", master, masterErr)
	}
	// enabled、enabledErr 是总闸开启时的账号生效值。
	enabled, enabledErr := store.AIReply.IsEnabled(ctx, cookieID)
	if enabledErr != nil || !enabled {
		t.Fatalf("总闸开启时应生效: enabled=%v err=%v", enabled, enabledErr)
	}
	// setErr 是关闭总闸的写入错误；false 是唯一能明确表达「关闭」的取值。
	if setErr := store.Settings.Set(ctx, aiMasterEnabledKey, "false"); setErr != nil {
		t.Fatal(setErr)
	}
	// closed、closedErr 是总闸关闭后的账号生效值。
	closed, closedErr := store.AIReply.IsEnabled(ctx, cookieID)
	if closedErr != nil || closed {
		t.Fatalf("总闸关闭时不得生效: enabled=%v err=%v", closed, closedErr)
	}
	// pricingAI、pricingAuto、pricingErr 是总闸关闭时的议价模式判定。
	pricingAI, pricingAuto, pricingErr := store.AIReply.PricingMode(ctx, cookieID)
	if pricingErr != nil || pricingAI || pricingAuto {
		t.Fatalf("总闸关闭时议价模式必须整体关闭: %v/%v err=%v", pricingAI, pricingAuto, pricingErr)
	}
	// 账号本身没有变化：显式行仍然是启用，只是被总闸覆盖。
	if storedEnabled, storedErr := store.AIReply.Get(ctx, cookieID); storedErr != nil || !storedEnabled.AIEnabled || !storedEnabled.AIEnabledExplicit {
		t.Fatalf("总闸不得改写账号显式值: cfg=%+v err=%v", storedEnabled, storedErr)
	}
	_ = userID
}

// TestUpsertSettingsKeepsSwitchWhenSkipped 未提交开关时不得把已有显式值改写。
//
// 典型场景：前端只改砍价护栏，请求体不带 ai_enabled。旧实现会把 ai_enabled 写成 false，
// 让原本启用的账号被静默关闭。
func TestUpsertSettingsKeepsSwitchWhenSkipped(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// _, cookieID 是待写入的账号。
	_, cookieID := seedAccount(t, store)
	// 第一次保存：显式开启。
	// err 是首次写入显式开启设置的错误。
	if err := store.AIReply.UpsertSettings(ctx, cookieID, AIReplySettings{AIEnabled: true, MaxBargainRounds: 3}); err != nil {
		t.Fatal(err)
	}
	// err 是第二次写入（不提交开关）的错误。
	if err := store.AIReply.UpsertSettings(ctx, cookieID, AIReplySettings{
		SkipAIEnabled: true, AIEnabled: false, MaxBargainRounds: 5,
	}); err != nil {
		t.Fatal(err)
	}
	// storedEnabled 是数据库里 ai_enabled 的实际整数值；必须是 1。
	var storedEnabled int
	// err 是读取 ai_enabled 整数值的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT ai_enabled FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&storedEnabled); err != nil {
		t.Fatal(err)
	}
	if storedEnabled != 1 {
		t.Fatalf("未提交开关时不得改写显式值: ai_enabled=%d", storedEnabled)
	}
	// err 是第三次写入（再次只改护栏）的错误；用于确认更新确实执行。
	if err := store.AIReply.UpsertSettings(ctx, cookieID, AIReplySettings{
		SkipAIEnabled: true, AIEnabled: false, MaxBargainRounds: 7,
	}); err != nil {
		t.Fatal(err)
	}
	// cfg、cfgErr 是最终读回的账号配置。
	cfg, cfgErr := store.AIReply.Get(ctx, cookieID)
	if cfgErr != nil || cfg == nil || cfg.MaxBargainRounds != 7 || !cfg.AIEnabled {
		t.Fatalf("护栏应更新且开关保留: cfg=%+v err=%v", cfg, cfgErr)
	}
	// err 是显式提交关闭时的写入错误。
	if err := store.AIReply.UpsertSettings(ctx, cookieID, AIReplySettings{AIEnabled: false, MaxBargainRounds: 7}); err != nil {
		t.Fatal(err)
	}
	// err 是重新读取 ai_enabled 整数值的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT ai_enabled FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&storedEnabled); err != nil {
		t.Fatal(err)
	}
	if storedEnabled != 0 {
		t.Fatalf("显式关闭必须落库为 0: ai_enabled=%d", storedEnabled)
	}
}

// TestAIMasterEnabledReadDoesNotAudit 总闸读取必须走普通读，不得写 security_audit_logs。
//
// 总闸会被回复链路按消息量读取；若走审计读，审计表会随消息量膨胀。
func TestAIMasterEnabledReadDoesNotAudit(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// before 是读取前的审计行数。
	var before int
	// err 是统计审计行数的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_logs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// 连续读取多次，模拟回复链路的真实读取频率。
	for i := 0; i < 3; i++ {
		// readErr 是本次总闸读取失败原因；读取本身不应写审计行。
		if _, readErr := store.AIReply.MasterEnabled(ctx); readErr != nil {
			t.Fatal(readErr)
		}
	}
	// after 是读取后的审计行数，必须与读取前一致。
	var after int
	// err 是再次统计审计行数的错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_audit_logs`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("总闸读取不得产生审计行: before=%d after=%d", before, after)
	}
}

// TestAIMasterEnabledValueParsing 覆盖总闸取值归一：未配置与开启取值开启，其余关闭。
func TestAIMasterEnabledValueParsing(t *testing.T) {
	// cases 是原文到期望结论的映射。
	cases := map[string]bool{
		"":          true,
		"  ":        true,
		"true":      true,
		"TRUE":      true,
		"1":         true,
		"yes":       true,
		"on":        true,
		"enabled":   true,
		"false":     false,
		"0":         false,
		"no":        false,
		"off":       false,
		"disabled":  false,
		"something": false,
	}
	// raw、want 是当前遍历到的原文与期望结论。
	for raw, want := range cases {
		// got 是当前原文解析出的总闸结论。
		got := aiMasterEnabledFromValue(raw)
		if got != want {
			t.Fatalf("总闸取值解析不符: raw=%q got=%v want=%v", raw, got, want)
		}
	}
}
