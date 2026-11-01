// agent_config_test.go 客服 Agent 账号级档位配置持久化的 sqlite 闭环测试。
//
// 重点覆盖「Agent 档位配置与 AI 开关彻底正交」：写档位既不得把默认开启的账号静默关闭，
// 也不得改写账号已有的显式开关；断言一律落在行为（AI 生效值）上，不锁列值形态。

package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// errAgentConfigFixtureNotOwned 表示测试夹具的账号归属校验未通过，属用例自身缺陷。
var errAgentConfigFixtureNotOwned = errors.New("测试夹具账号归属校验未通过")

// agentConfigFixture 是账号级 Agent 配置测试所需的依赖快照。
type agentConfigFixture struct {
	// ctx 是用例取消边界。
	ctx context.Context
	// store 是绑定 sqlite 方言的仓储集合。
	store *Store
	// ownerID 是配置归属租户的用户标识。
	ownerID int64
	// strangerID 是另一个租户的用户标识，用于验证跨租户拒绝。
	strangerID int64
}

// newAgentConfigFixture 建库并预置两个租户与各自的账号。
func newAgentConfigFixture(t *testing.T) agentConfigFixture {
	t.Helper()
	// ctx 是用例取消边界。
	ctx := context.Background()
	// database、err 是临时 sqlite 数据库与打开错误；Open 会自动应用全部迁移。
	database, _, err := Open(ctx, filepath.Join(t.TempDir(), "agent-config.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	// store 是绑定 sqlite 方言的仓储集合。
	store := NewStore(database, DialectSQLite)
	// ownerErr 是创建归属租户的结果。
	ownerErr := createTestUser(ctx, store, "owner")
	if ownerErr != nil {
		t.Fatalf("创建归属租户失败: %v", ownerErr)
	}
	// strangerErr 是创建另一租户的结果。
	strangerErr := createTestUser(ctx, store, "stranger")
	if strangerErr != nil {
		t.Fatalf("创建另一租户失败: %v", strangerErr)
	}
	// owner、ownerReadErr 是归属租户用户与读取错误。
	owner, ownerReadErr := store.Users.GetByUsername(ctx, "owner")
	if ownerReadErr != nil {
		t.Fatalf("读取归属租户失败: %v", ownerReadErr)
	}
	// stranger、strangerReadErr 是另一租户用户与读取错误。
	stranger, strangerReadErr := store.Users.GetByUsername(ctx, "stranger")
	if strangerReadErr != nil {
		t.Fatalf("读取另一租户失败: %v", strangerReadErr)
	}
	// mineErr 是归属租户下账号的写入结果。
	if mineErr := store.Cookies.Save(ctx, "cookie-mine", "unb=1;", owner.ID); mineErr != nil {
		t.Fatalf("写入归属账号失败: %v", mineErr)
	}
	// theirsErr 是另一租户下账号的写入结果。
	if theirsErr := store.Cookies.Save(ctx, "cookie-theirs", "unb=2;", stranger.ID); theirsErr != nil {
		t.Fatalf("写入另一租户账号失败: %v", theirsErr)
	}
	return agentConfigFixture{ctx: ctx, store: store, ownerID: owner.ID, strangerID: stranger.ID}
}

// createTestUser 建一个仅用于测试的租户账户。
func createTestUser(ctx context.Context, store *Store, username string) error {
	// err 是用户创建错误；测试夹具固定使用互不重复的用户名。
	_, err := store.Users.Create(ctx, username, username+"@example.invalid", "test-only")
	return err
}

// TestAgentConfigRoundTripKeepsInheritance 验证「覆盖」与「继承」两态都能原样往返，
// 且 NULL 不被误读成空档位。
func TestAgentConfigRoundTripKeepsInheritance(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// initial、found、readErr 是未写入时的读取结果、归属判定与错误。
	initial, found, readErr := fixture.store.AIReply.GetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-mine")
	if readErr != nil {
		t.Fatalf("读取初始配置失败: %v", readErr)
	}
	if !found {
		t.Fatal("归属租户的账号必须被识别为存在")
	}
	if initial.Preset != nil {
		t.Fatalf("从未配置时档位必须继承，实际 %+v", initial)
	}
	// preset 是显式只读档，覆盖租户默认。
	preset := "readonly"
	// writeErr 是账号级配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Preset: &preset})
	if writeErr != nil {
		t.Fatalf("写入账号级配置失败: %v", writeErr)
	}
	// stored、storedFound、storedErr 是回读结果、归属判定与错误。
	stored, storedFound, storedErr := fixture.store.AIReply.GetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-mine")
	if storedErr != nil {
		t.Fatalf("回读账号级配置失败: %v", storedErr)
	}
	if !storedFound {
		t.Fatal("回读时归属租户的账号必须被识别为存在")
	}
	if stored.Preset == nil || *stored.Preset != preset {
		t.Fatalf("显式档位必须读回 %q，实际 %+v", preset, stored.Preset)
	}
	// clearErr 是恢复完全继承的写入结果。
	clearErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine"})
	if clearErr != nil {
		t.Fatalf("恢复继承失败: %v", clearErr)
	}
	// cleared、clearedFound、clearedErr 是恢复继承后的回读结果、归属判定与错误。
	cleared, clearedFound, clearedErr := fixture.store.AIReply.GetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-mine")
	if clearedErr != nil {
		t.Fatalf("回读恢复结果失败: %v", clearedErr)
	}
	if !clearedFound || cleared.Preset != nil {
		t.Fatalf("恢复继承后档位必须回到 NULL，实际 %+v", cleared)
	}
}

// TestAgentConfigGetRejectsMissingAndForeignAccounts 验证账号不存在与跨租户读取
// 一律返回 found=false，使调用方无法把它当成「未配置」继续执行。
func TestAgentConfigGetRejectsMissingAndForeignAccounts(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// missing、missingFound、missingErr 是不存在账号的读取结果、归属判定与错误。
	missing, missingFound, missingErr := fixture.store.AIReply.GetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-absent")
	if missingErr != nil {
		t.Fatalf("读取不存在账号失败: %v", missingErr)
	}
	if missingFound || missing != nil {
		t.Fatalf("不存在的账号必须返回 found=false，实际 found=%v config=%+v", missingFound, missing)
	}
	// foreign、foreignFound、foreignErr 是跨租户账号的读取结果、归属判定与错误。
	foreign, foreignFound, foreignErr := fixture.store.AIReply.GetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-theirs")
	if foreignErr != nil {
		t.Fatalf("读取跨租户账号失败: %v", foreignErr)
	}
	if foreignFound || foreign != nil {
		t.Fatalf("跨租户账号必须返回 found=false，实际 found=%v config=%+v", foreignFound, foreign)
	}
}

// TestAgentConfigSetRejectsForeignAccount 验证跨租户写入不落库。
func TestAgentConfigSetRejectsForeignAccount(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// preset 是越权写入试图设置的档位。
	preset := "advanced"
	// found、err 是越权写入的归属判定与错误。
	found, err := fixture.store.AIReply.SetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-theirs", AgentAccountConfig{CookieID: "cookie-theirs", Preset: &preset})
	if err != nil {
		t.Fatalf("越权写入返回错误: %v", err)
	}
	if found {
		t.Fatal("跨租户写入必须返回 found=false")
	}
	// theirs、strangerReadErr 是该账号以其真实租户身份读回的配置与读取错误。
	theirs, strangerReadErr := readAgentConfig(fixture, fixture.strangerID, "cookie-theirs")
	if strangerReadErr != nil {
		t.Fatalf("以真实租户读取失败: %v", strangerReadErr)
	}
	if theirs.Preset != nil {
		t.Fatalf("越权写入不得落库，实际 %+v", theirs)
	}
}

// TestAgentConfigWriteKeepsAIReplyColumns 验证保存 Agent 配置不会改动同一行上的
// 模型凭证与业务护栏列。
func TestAgentConfigWriteKeepsAIReplyColumns(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// settings 是既有的账号级 AI 业务配置。
	settings := AIReplySettings{AIEnabled: true, AutoAdjustPriceEnabled: true, MaxDiscountPercent: 25, MaxDiscountAmount: 500, MaxBargainRounds: 5, CustomPrompts: "温和议价"}
	// saveErr 是 AI 业务配置写入结果。
	if saveErr := fixture.store.AIReply.UpsertSettings(fixture.ctx, "cookie-mine", settings); saveErr != nil {
		t.Fatalf("写入 AI 业务配置失败: %v", saveErr)
	}
	// preset 是本次要写入的 Agent 档位。
	preset := "standard"
	// writeErr 是 Agent 配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Preset: &preset})
	if writeErr != nil {
		t.Fatalf("写入 Agent 配置失败: %v", writeErr)
	}
	// after、readErr 是写入后的 AI 业务配置与读取错误。
	after, readErr := fixture.store.AIReply.Get(fixture.ctx, "cookie-mine")
	if readErr != nil {
		t.Fatalf("读取 AI 业务配置失败: %v", readErr)
	}
	if !after.AIEnabled || !after.AutoAdjustPriceEnabled {
		t.Fatalf("Agent 配置写入不得关闭 AI 开关，实际 %+v", after)
	}
	if after.MaxDiscountPercent != 25 || after.MaxDiscountAmount != 500 || after.MaxBargainRounds != 5 || after.CustomPrompts != "温和议价" {
		t.Fatalf("Agent 配置写入不得改动业务护栏，实际 %+v", after)
	}
}

// TestAIReplySettingsUpsertKeepsAgentConfig 验证保存 AI 业务配置不会覆盖 Agent 档位，
// 两条写入路径互不干扰。
func TestAIReplySettingsUpsertKeepsAgentConfig(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// preset 是本次要写入的 Agent 档位。
	preset := "advanced"
	// writeErr 是 Agent 配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Preset: &preset})
	if writeErr != nil {
		t.Fatalf("写入 Agent 配置失败: %v", writeErr)
	}
	// saveErr 是随后保存 AI 业务配置的结果。
	if saveErr := fixture.store.AIReply.UpsertSettings(fixture.ctx, "cookie-mine", AIReplySettings{AIEnabled: false, MaxDiscountPercent: 8}); saveErr != nil {
		t.Fatalf("写入 AI 业务配置失败: %v", saveErr)
	}
	// after、readErr 是保存业务配置后读回的 Agent 配置与错误。
	after, readErr := readAgentConfig(fixture, fixture.ownerID, "cookie-mine")
	if readErr != nil {
		t.Fatalf("读取 Agent 配置失败: %v", readErr)
	}
	if after.Preset == nil || *after.Preset != preset {
		t.Fatalf("AI 业务配置写入不得清空 Agent 档位，实际 %+v", after.Preset)
	}
}

// TestAgentConfigWriteKeepsAIEnabled 是本文件最关键的一条回归：
// 给一个原本没有 ai_reply_settings 行的账号写 Agent 档位后，该账号的 AI 生效值必须仍为
// 「总闸开启即接管」，且不得顺带放开自动改价。
//
// 背景：ai_reply_settings.ai_enabled 在建表时是 DEFAULT FALSE，而旧版「只写 agent_preset」
// 的 INSERT 会落到该默认值。E1 把语义反转成「无行即跟随总闸」后，这次静默落下的 0
// 就成了「账号被显式关闭 AI」——用户只改了一次 Agent 档位，AI 回复却停了。
//
// 断言口径刻意只看行为（生效值），不看列值：列值的具体形态会随清理迁移变化，
// 而「写档位之后账号照常接管」才是这条边界的契约。
func TestAgentConfigWriteKeepsAIEnabled(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// preset 是本次要写入的 Agent 档位。
	preset := "standard"
	// writeErr 是 Agent 配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Preset: &preset})
	if writeErr != nil {
		t.Fatalf("写入 Agent 配置失败: %v", writeErr)
	}
	// aiEnabled、autoAdjust、modeErr 是 AI 生效值与自动改价开关及其读取错误。
	aiEnabled, autoAdjust, modeErr := fixture.store.AIReply.PricingMode(fixture.ctx, "cookie-mine")
	if modeErr != nil {
		t.Fatalf("读取 AI 执行模式失败: %v", modeErr)
	}
	if !aiEnabled {
		t.Fatal("写 Agent 档位后 AI 生效值必须仍为「跟随总闸（开）」")
	}
	if autoAdjust {
		t.Fatalf("写 Agent 档位不得顺带开启自动改价，实际 %v", autoAdjust)
	}
	// enabled、enabledErr 是 IsEnabled 给出的账号生效值，与 PricingMode 互为印证。
	enabled, enabledErr := fixture.store.AIReply.IsEnabled(fixture.ctx, "cookie-mine")
	if enabledErr != nil {
		t.Fatalf("读取 AI 生效值失败: %v", enabledErr)
	}
	if !enabled {
		t.Fatal("写 Agent 档位不得把账号从「跟随总闸」改成关闭")
	}
}

// TestAgentConfigWriteKeepsExplicitAIDisabled 验证账号显式关闭 AI 之后，
// 保存 Agent 档位不会把这次「关闭」决定改写掉。
func TestAgentConfigWriteKeepsExplicitAIDisabled(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// saveErr 是显式关闭 AI 的写入结果。
	if saveErr := fixture.store.AIReply.UpsertSettings(fixture.ctx, "cookie-mine", AIReplySettings{AIEnabled: false}); saveErr != nil {
		t.Fatalf("写入 AI 业务配置失败: %v", saveErr)
	}
	// preset 是本次要写入的 Agent 档位。
	preset := "advanced"
	// writeErr 是 Agent 配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Preset: &preset})
	if writeErr != nil {
		t.Fatalf("写入 Agent 配置失败: %v", writeErr)
	}
	// aiEnabled、modeErr 是 AI 生效值与读取错误；显式关闭必须持续生效。
	aiEnabled, modeErr := fixture.store.AIReply.IsEnabled(fixture.ctx, "cookie-mine")
	if modeErr != nil {
		t.Fatalf("读取 AI 生效值失败: %v", modeErr)
	}
	if aiEnabled {
		t.Fatal("显式关闭 AI 的账号不得因写 Agent 档位而恢复启用")
	}
	// stored、storedErr 是账号级设置回读结果与错误；显式关闭同样必须被如实读出。
	stored, storedErr := fixture.store.AIReply.Get(fixture.ctx, "cookie-mine")
	if storedErr != nil {
		t.Fatalf("读取账号 AI 设置失败: %v", storedErr)
	}
	if stored.AIEnabled {
		t.Fatalf("Agent 档位写入不得改写显式关闭: %+v", stored)
	}
}

// TestUserSettingsSetManyForUserRoundTrip 验证批量保存在 sqlite 上完整落库并可读回。
func TestUserSettingsSetManyForUserRoundTrip(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// values 是本次批量写入的租户级设置；只用当前仍在维护的档位键，避免给已废弃的
	// agent.support.enabled 键制造「仍在被使用」的假象。
	values := map[string]string{"agent.support.preset": "standard", "ai.review.enabled": "true"}
	// writeErr 是批量写入结果。
	if writeErr := fixture.store.UserSettings.SetManyForUser(fixture.ctx, fixture.ownerID, values); writeErr != nil {
		t.Fatalf("批量写入租户设置失败: %v", writeErr)
	}
	// all、readErr 是读回的租户设置与该读取错误。
	all, readErr := fixture.store.UserSettings.AllForUser(fixture.ctx, fixture.ownerID)
	if readErr != nil {
		t.Fatalf("读取租户设置失败: %v", readErr)
	}
	if all["agent.support.preset"] != "standard" || all["ai.review.enabled"] != "true" {
		t.Fatalf("批量写入结果不符: %+v", all)
	}
	// overwriteErr 是用新值覆盖同键的结果。
	if overwriteErr := fixture.store.UserSettings.SetManyForUser(fixture.ctx, fixture.ownerID, map[string]string{"agent.support.preset": "advanced"}); overwriteErr != nil {
		t.Fatalf("覆盖写入租户设置失败: %v", overwriteErr)
	}
	// updated、updatedErr 是覆盖后的读回结果与错误。
	updated, updatedErr := fixture.store.UserSettings.GetForUser(fixture.ctx, fixture.ownerID, "agent.support.preset")
	if updatedErr != nil {
		t.Fatalf("读取覆盖结果失败: %v", updatedErr)
	}
	if updated != "advanced" {
		t.Fatalf("覆盖后档位=%q，期望 advanced", updated)
	}
}

// TestUserSettingsSetManyForUserIgnoresEmptyInput 验证空映射不开启事务也不报错。
func TestUserSettingsSetManyForUserIgnoresEmptyInput(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// err 是空映射写入结果，必须为 nil。
	if err := fixture.store.UserSettings.SetManyForUser(fixture.ctx, fixture.ownerID, nil); err != nil {
		t.Fatalf("空映射写入应成功返回: %v", err)
	}
}

// writeAgentConfig 写入账号级配置并断言租户归属成立，失败即终止用例。
func writeAgentConfig(fixture agentConfigFixture, config AgentAccountConfig) error {
	// found、err 是写入的归属判定与错误。
	found, err := fixture.store.AIReply.SetAgentConfig(fixture.ctx, fixture.ownerID, config.CookieID, config)
	if err != nil {
		return err
	}
	if !found {
		return errAgentConfigFixtureNotOwned
	}
	return nil
}

// readAgentConfig 以指定租户身份读取账号级配置，未命中归属时返回错误。
func readAgentConfig(fixture agentConfigFixture, userID int64, cookieID string) (*AgentAccountConfig, error) {
	// config、found、err 是读取结果、归属判定与错误。
	config, found, err := fixture.store.AIReply.GetAgentConfig(fixture.ctx, userID, cookieID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errAgentConfigFixtureNotOwned
	}
	return config, nil
}
