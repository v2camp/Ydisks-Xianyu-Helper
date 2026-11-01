// ai_master_gate_test.go 锁定「无设置行 = 跟随全局总闸」的生效口径。
//
// 旧口径是「无 ai_reply_settings 行 = 该账号 AI 关闭」，新口径是
// effective = ai_master_enabled && (无行 || 行.ai_enabled == 1)。
// 本文件按四种组合分别断言引擎行为与 ai_decision_log.reason，
// 确保运维能从归因表直接区分「总闸关了」和「这个账号关了」。
package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// aiGateFixture 是生效口径测试的最小仓储：一个账号、一件商品、可用的模型配置。
// settingsRow 为 nil 表示该账号没有 ai_reply_settings 行。
type aiGateFixture struct {
	// store 是测试仓储。
	store *db.Store
	// cleanup 释放临时数据库。
	cleanup func()
	// ctx 是本用例共用的上下文。
	ctx context.Context
}

// newAIGateFixture 构造生效口径测试仓储。
// 参数 explicitEnabled 为 nil 时不写入设置行（无行语义）；非 nil 时写入该显式开关值。
// 参数 master 为 nil 时不写总闸配置（等价于开启）；非 nil 时写入 true/false 原文。
func newAIGateFixture(t *testing.T, explicitEnabled *int, master *bool) aiGateFixture {
	t.Helper()
	// store、cleanup 是共享的 AI 测试仓储及清理函数。
	store, cleanup := newAIStore(t)
	// ctx 是准备测试数据使用的上下文。
	ctx := context.Background()
	if explicitEnabled != nil {
		// insertErr 表示写入账号设置行时不应出现的数据库错误。
		if _, insertErr := store.DB.ExecContext(ctx,
			`INSERT INTO ai_reply_settings (cookie_id,ai_enabled,max_bargain_rounds,custom_prompts)
			 VALUES ('cid',?,3,'')`, *explicitEnabled); insertErr != nil {
			cleanup()
			t.Fatal(insertErr)
		}
	}
	// itemErr 表示写入测试商品时不应出现的数据库错误。
	if _, itemErr := store.DB.ExecContext(ctx, `INSERT INTO item_info
		(cookie_id,item_id,item_title,item_price,item_description) VALUES ('cid','item-gate','商品','100','描述')`); itemErr != nil {
		cleanup()
		t.Fatal(itemErr)
	}
	// keyErr 表示写入测试密钥时不应出现的数据库错误。
	if keyErr := store.Settings.Set(ctx, "ai_api_key", "sk-gate"); keyErr != nil {
		cleanup()
		t.Fatal(keyErr)
	}
	// urlErr 表示写入不可达模型地址时不应出现的数据库错误；接缝生效时该地址永不被访问。
	if urlErr := store.Settings.Set(ctx, "ai_api_url", "http://127.0.0.1:1"); urlErr != nil {
		cleanup()
		t.Fatal(urlErr)
	}
	if master != nil {
		// masterErr 表示写入总闸配置时不应出现的数据库错误。
		if masterErr := store.Settings.Set(ctx, "ai_master_enabled", aiGateBoolText(*master)); masterErr != nil {
			cleanup()
			t.Fatal(masterErr)
		}
	}
	return aiGateFixture{store: store, cleanup: cleanup, ctx: ctx}
}

// aiGateBoolText 把布尔值转换为 system_settings 的文本取值。
func aiGateBoolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// aiGateLastReason 读取指定会话最近一条归因记录的原因简码与接管标记。
// 缺失记录时返回空原因，便于调用方给出「根本没有留痕」的失败信息。
func aiGateLastReason(t *testing.T, fixture aiGateFixture, chatID string) (string, int) {
	t.Helper()
	// reason 是归因原因简码。
	var reason string
	// takeover 是接管标记的整数值（1 表示放行，0 表示未接管）。
	var takeover int
	// err 是读取失败原因；没有记录时保持零值由调用方断言。
	err := fixture.store.DB.QueryRowContext(fixture.ctx,
		`SELECT reason, takeover FROM ai_decision_log WHERE chat_id=? ORDER BY id DESC LIMIT 1`, chatID).Scan(&reason, &takeover)
	if err != nil {
		t.Fatalf("读取会话 %s 的归因记录失败: %v", chatID, err)
	}
	return reason, takeover
}

// TestReplyGateUsesMasterSwitch 覆盖「总闸 × 账号显式开关」的四种组合。
func TestReplyGateUsesMasterSwitch(t *testing.T) {
	// explicitOn、explicitOff 分别是显式启用与显式关闭的账号开关取值。
	explicitOn, explicitOff := 1, 0
	// masterOn、masterOff 分别是总闸开启与关闭的配置取值。
	masterOn, masterOff := true, false
	// cases 是四种组合的期望：是否产出回复、归因原因简码。
	cases := []struct {
		// name 是用例名，说明组合语义。
		name string
		// accountEnabled 为 nil 表示账号没有显式设置行。
		accountEnabled *int
		// master 为 nil 表示总闸未配置。
		master *bool
		// wantReply 表示是否应产出回复。
		wantReply bool
		// wantReason 是期望写入 ai_decision_log 的原因简码。
		wantReason string
	}{
		{name: "无行+总闸开", accountEnabled: nil, master: nil, wantReply: true, wantReason: "taken"},
		{name: "无行+总闸关", accountEnabled: nil, master: &masterOff, wantReply: false, wantReason: "master-off"},
		{name: "显式关+总闸开", accountEnabled: &explicitOff, master: &masterOn, wantReply: false, wantReason: "account-off"},
		{name: "显式开+总闸关", accountEnabled: &explicitOn, master: &masterOff, wantReply: false, wantReason: "master-off"},
	}
	// current 是当前待验证的组合。
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			// fixture 是本用例独立的测试仓储。
			fixture := newAIGateFixture(t, current.accountEnabled, current.master)
			defer fixture.cleanup()
			// stub 是替代真实模型调用的生成器替身。
			stub := &recordingAIGenerator{content: "在的，可以聊"}
			// chatID 是本用例唯一的会话标识，用于回查归因记录。
			chatID := "chat-gate"

			// result、err 分别是本次回复结果与调用错误。
			result, err := NewAIReplier("cid", fixture.store, nil, stub).Reply(fixture.ctx,
				chatMsg("能便宜点吗", "item-gate", chatID))
			if err != nil {
				t.Fatalf("回复链路不应报错: %v", err)
			}
			if current.wantReply {
				if result == nil || result.Text != "在的，可以聊" {
					t.Fatalf("应产出回复: %+v", result)
				}
				if stub.calls != 1 {
					t.Fatalf("生效时应调用一次模型: calls=%d", stub.calls)
				}
			} else if result != nil || stub.calls != 0 {
				t.Fatalf("闸门关闭时不得产出回复也不得调用模型: result=%+v calls=%d", result, stub.calls)
			}
			// reason、takeover 是本用例写入的归因结论。
			reason, takeover := aiGateLastReason(t, fixture, chatID)
			if reason != current.wantReason {
				t.Fatalf("归因原因不符: got=%q want=%q", reason, current.wantReason)
			}
			// wantTakeover 是期望落库的接管标记整数值：放行记 1，闸门关闭记 0。
			wantTakeover := 0
			if current.wantReply {
				wantTakeover = 1
			}
			if takeover != wantTakeover {
				t.Fatalf("接管标记不符: got=%d want=%d", takeover, wantTakeover)
			}
		})
	}
}

// TestReplyWithoutSettingsRowUsesDefaultGuards 验证无设置行时使用默认护栏而非零值护栏。
// 若默认最大砍价轮次为 0，首条砍价消息会直接被轮次上限挡住，默认开启等于没开。
func TestReplyWithoutSettingsRowUsesDefaultGuards(t *testing.T) {
	// fixture 是不写设置行的测试仓储，总闸未配置（视为开启）。
	fixture := newAIGateFixture(t, nil, nil)
	defer fixture.cleanup()
	// cfg、cfgErr 是无设置行时读出的默认护栏。
	cfg, cfgErr := fixture.store.AIReply.Get(fixture.ctx, "cid")
	if cfgErr != nil || cfg == nil {
		t.Fatalf("无行读取应返回默认护栏: cfg=%+v err=%v", cfg, cfgErr)
	}
	if cfg.AIEnabledExplicit {
		t.Fatal("无行时不得标记为显式开关")
	}
	if cfg.MaxBargainRounds != 3 || cfg.MaxDiscountPercent != 10 || cfg.MaxDiscountAmount != 100 {
		t.Fatalf("默认护栏应与迁移列默认值一致: %+v", cfg)
	}
	// result、err 是首条砍价消息的回复结果。
	result, err := NewAIReplier("cid", fixture.store, nil, &recordingAIGenerator{content: "可以聊"}).
		Reply(fixture.ctx, chatMsg("能便宜点吗", "item-gate", "chat-gate-default"))
	if err != nil || result == nil {
		t.Fatalf("默认护栏下首条砍价消息应被接管: result=%+v err=%v", result, err)
	}
}
