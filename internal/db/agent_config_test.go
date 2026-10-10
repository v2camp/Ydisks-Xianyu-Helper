// agent_config_test.go 客服 Agent 账号级配置持久化的 sqlite 闭环测试。

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

// TestAgentConfigRoundTripKeepsInheritance 验证两个维度的「覆盖」与「继承」三态
// 都能原样往返，且 NULL 不被误读成 false 或空档位。
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
	if initial.Enabled != nil || initial.Preset != nil {
		t.Fatalf("从未配置时两个维度都必须继承，实际 %+v", initial)
	}
	// enabled 是显式关闭开关，用于验证 false 与 NULL 的区别。
	enabled := false
	// preset 是显式只读档，覆盖租户默认。
	preset := "readonly"
	// writeErr 是账号级配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Enabled: &enabled, Preset: &preset})
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
	if stored.Enabled == nil || *stored.Enabled {
		t.Fatalf("显式关闭开关必须读回 false，实际 %+v", stored.Enabled)
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
	if !clearedFound || cleared.Enabled != nil || cleared.Preset != nil {
		t.Fatalf("恢复继承后两个维度都必须回到 NULL，实际 %+v", cleared)
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
	// enabled 是越权写入试图开启的开关值。
	enabled := true
	// found、err 是越权写入的归属判定与错误。
	found, err := fixture.store.AIReply.SetAgentConfig(fixture.ctx, fixture.ownerID, "cookie-theirs", AgentAccountConfig{CookieID: "cookie-theirs", Enabled: &enabled})
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
	if theirs.Enabled != nil {
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

// TestAIReplySettingsUpsertKeepsAgentConfig 验证保存 AI 业务配置不会覆盖 Agent 配置，
// 两条写入路径互不干扰。
func TestAIReplySettingsUpsertKeepsAgentConfig(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// enabled 是本次要写入的 Agent 开关。
	enabled := true
	// preset 是本次要写入的 Agent 档位。
	preset := "advanced"
	// writeErr 是 Agent 配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Enabled: &enabled, Preset: &preset})
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
	if after.Enabled == nil || !*after.Enabled {
		t.Fatalf("AI 业务配置写入不得清空 Agent 开关，实际 %+v", after.Enabled)
	}
	if after.Preset == nil || *after.Preset != preset {
		t.Fatalf("AI 业务配置写入不得清空 Agent 档位，实际 %+v", after.Preset)
	}
}

// TestAgentConfigWriteDoesNotEnableAIReply 验证首次写 Agent 配置会创建配置行，
// 但该行上的 AI 开关与自动改价仍保持关闭，不会顺带放开资损路径。
func TestAgentConfigWriteDoesNotEnableAIReply(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// enabled 是本次要写入的 Agent 开关。
	enabled := true
	// writeErr 是 Agent 配置写入结果。
	writeErr := writeAgentConfig(fixture, AgentAccountConfig{CookieID: "cookie-mine", Enabled: &enabled})
	if writeErr != nil {
		t.Fatalf("写入 Agent 配置失败: %v", writeErr)
	}
	// aiEnabled、autoAdjust、modeErr 是 AI 开关、自动改价开关及其读取错误。
	aiEnabled, autoAdjust, modeErr := fixture.store.AIReply.PricingMode(fixture.ctx, "cookie-mine")
	if modeErr != nil {
		t.Fatalf("读取 AI 执行模式失败: %v", modeErr)
	}
	if aiEnabled || autoAdjust {
		t.Fatalf("写 Agent 配置不得顺带开启 AI 议价或自动改价，实际 ai=%v auto=%v", aiEnabled, autoAdjust)
	}
}

// TestUserSettingsSetManyForUserRoundTrip 验证批量保存在 sqlite 上完整落库并可读回。
func TestUserSettingsSetManyForUserRoundTrip(t *testing.T) {
	// fixture 是预置两个租户与各自账号的 sqlite 夹具。
	fixture := newAgentConfigFixture(t)
	// values 是本次批量写入的租户级设置。
	values := map[string]string{"agent.support.enabled": "true", "agent.support.preset": "standard"}
	// writeErr 是批量写入结果。
	if writeErr := fixture.store.UserSettings.SetManyForUser(fixture.ctx, fixture.ownerID, values); writeErr != nil {
		t.Fatalf("批量写入租户设置失败: %v", writeErr)
	}
	// all、readErr 是读回的租户设置与该读取错误。
	all, readErr := fixture.store.UserSettings.AllForUser(fixture.ctx, fixture.ownerID)
	if readErr != nil {
		t.Fatalf("读取租户设置失败: %v", readErr)
	}
	if all["agent.support.enabled"] != "true" || all["agent.support.preset"] != "standard" {
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
