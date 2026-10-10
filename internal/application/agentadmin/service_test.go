package agentadmin

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/capability"
)

// fakeRepository 是客服 Agent 配置仓储的确定性测试替身。
type fakeRepository struct {
	// tenant 保存各租户的 user_settings 键值。
	tenant map[int64]map[string]string
	// accounts 保存各账号已落库的覆盖。
	accounts map[string]AccountOverride
	// owners 保存账号到所属租户的映射；不在其中的账号视为不存在。
	owners map[string]int64
	// err 是任一方法返回的注入错误。
	err error
	// tenantWrites 记录租户级写入次数，用于断言非法输入没有落库。
	tenantWrites int
	// accountWrites 记录账号级写入次数，用于断言越权输入没有落库。
	accountWrites int
}

// TenantValues 返回租户设置的副本，避免调用方改动污染测试夹具。
func (f *fakeRepository) TenantValues(_ context.Context, userID int64) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	// out 是租户设置的浅拷贝，键数量与来源一致。
	out := make(map[string]string, len(f.tenant[userID]))
	// key、value 是当前待复制的设置键值对。
	for key, value := range f.tenant[userID] {
		out[key] = value
	}
	return out, nil
}

// SetTenantValues 记录并落库租户级写入。
func (f *fakeRepository) SetTenantValues(_ context.Context, userID int64, values map[string]string) error {
	if f.err != nil {
		return f.err
	}
	f.tenantWrites++
	if f.tenant[userID] == nil {
		f.tenant[userID] = make(map[string]string)
	}
	// key、value 是本次待落库的设置键值对。
	for key, value := range values {
		f.tenant[userID][key] = value
	}
	return nil
}

// AccountOverride 按归属返回账号覆盖；不属于该租户的账号一律 found=false。
func (f *fakeRepository) AccountOverride(_ context.Context, userID int64, cookieID string) (AccountOverride, bool, error) {
	if f.err != nil {
		return AccountOverride{}, false, f.err
	}
	if f.owners[cookieID] != userID {
		return AccountOverride{}, false, nil
	}
	// override、exists 是该账号已落库的覆盖与其存在性；无记录表示完全继承。
	override, exists := f.accounts[cookieID]
	if !exists {
		return AccountOverride{}, true, nil
	}
	return override, true, nil
}

// SetAccountOverride 按归属写入账号覆盖；不属于该租户时不落库。
func (f *fakeRepository) SetAccountOverride(_ context.Context, userID int64, cookieID string, override AccountOverride) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.owners[cookieID] != userID {
		return false, nil
	}
	f.accountWrites++
	f.accounts[cookieID] = override
	return true, nil
}

// newTestService 构造预置一个租户与三个账号的配置服务。
// cookie-a 与 cookie-b 属于租户 1，cookie-foreign 属于租户 2。
func newTestService() (*Service, *fakeRepository) {
	// repo 是带归属关系的替身仓储。
	repo := &fakeRepository{
		tenant:   map[int64]map[string]string{},
		accounts: map[string]AccountOverride{},
		owners:   map[string]int64{"cookie-a": 1, "cookie-b": 1, "cookie-foreign": 2},
	}
	return NewService(repo), repo
}

// boolPtr 返回布尔值的地址，用于构造显式覆盖的开关。
func boolPtr(value bool) *bool { return &value }

// stringPtr 返回字符串的地址，用于构造显式覆盖的档位。
func stringPtr(value string) *string { return &value }

// TestTenantConfigDefaultsToDisabledReadonly 验证未配置的租户收敛为
// 「关闭 + 只读档」，即平台默认，不给新租户任何隐含权限。
func TestTenantConfigDefaultsToDisabledReadonly(t *testing.T) {
	// service 是只需读取断言、不需要检查落库的配置服务。
	service, _ := newTestService()
	// config、err 是未配置租户的默认配置读取结果与错误。
	config, err := service.TenantConfig(context.Background(), 1)
	if err != nil {
		t.Fatalf("读取租户默认失败: %v", err)
	}
	if config.Enabled {
		t.Fatal("未配置时租户默认开关必须为关闭")
	}
	if config.Preset != capability.PresetReadonly {
		t.Fatalf("未配置时租户默认档位=%q，期望只读档", config.Preset)
	}
}

// TestTenantConfigReadsConfiguredValues 验证已配置的租户开关与档位被原样读出。
func TestTenantConfigReadsConfiguredValues(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{
		KeySupportEnabled: "true",
		KeySupportPreset:  string(capability.PresetStandard),
	}
	// config、err 是已配置租户的默认配置读取结果与错误。
	config, err := service.TenantConfig(context.Background(), 1)
	if err != nil {
		t.Fatalf("读取租户默认失败: %v", err)
	}
	if !config.Enabled || config.Preset != capability.PresetStandard {
		t.Fatalf("租户默认 enabled=%v preset=%q，期望开启与标准档", config.Enabled, config.Preset)
	}
}

// TestTenantConfigFallsBackOnIllegalPreset 验证库中遗留的非法档位在读取路径上
// 回落到只读档，而不是让整条配置链路失败。
func TestTenantConfigFallsBackOnIllegalPreset(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{
		KeySupportEnabled: "true",
		KeySupportPreset:  "unlimited",
	}
	// config、err 是非法档位租户的默认配置读取结果与错误。
	config, err := service.TenantConfig(context.Background(), 1)
	if err != nil {
		t.Fatalf("读取租户默认失败: %v", err)
	}
	if config.Preset != capability.PresetReadonly {
		t.Fatalf("非法档位必须回落到只读档，实际 %q", config.Preset)
	}
	if !config.Enabled {
		t.Fatal("非法档位不应连带否决已开启的开关")
	}
}

// TestTenantConfigTreatsIllegalBooleanAsDisabled 验证开关原文非法时按关闭处理，
// 避免出现「看不懂的值等于开启」这类危险约定。
func TestTenantConfigTreatsIllegalBooleanAsDisabled(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{KeySupportEnabled: "maybe"}
	// config、err 是非法开关租户的默认配置读取结果与错误。
	config, err := service.TenantConfig(context.Background(), 1)
	if err != nil {
		t.Fatalf("读取租户默认失败: %v", err)
	}
	if config.Enabled {
		t.Fatal("非法开关原文必须按关闭处理")
	}
}

// TestUpdateTenantConfigPersistsBothKeys 验证租户级保存会同时写入开关与档位两项。
func TestUpdateTenantConfigPersistsBothKeys(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	// err 是租户级配置保存错误。
	err := service.UpdateTenantConfig(context.Background(), 1, TenantConfig{Enabled: true, Preset: capability.PresetAdvanced})
	if err != nil {
		t.Fatalf("保存租户默认失败: %v", err)
	}
	// stored 是本次写入后的租户设置。
	stored := repo.tenant[1]
	if stored[KeySupportEnabled] != "true" {
		t.Fatalf("租户开关落库值=%q，期望 true", stored[KeySupportEnabled])
	}
	if stored[KeySupportPreset] != string(capability.PresetAdvanced) {
		t.Fatalf("租户档位落库值=%q，期望高级档", stored[KeySupportPreset])
	}
}

// TestUpdateTenantConfigRejectsIllegalPreset 验证非法档位在写入路径被拒绝，
// 且不会留下半份配置。
func TestUpdateTenantConfigRejectsIllegalPreset(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	// err 是非法档位的写入错误，必须是 ErrInvalidPreset。
	err := service.UpdateTenantConfig(context.Background(), 1, TenantConfig{Enabled: true, Preset: "root"})
	if !errors.Is(err, ErrInvalidPreset) {
		t.Fatalf("非法档位写入错误=%v，期望 ErrInvalidPreset", err)
	}
	if repo.tenantWrites != 0 {
		t.Fatalf("非法档位不得落库，实际写入 %d 次", repo.tenantWrites)
	}
}

// TestAccountConfigInheritsTenantDefault 验证账号没有覆盖时完全继承租户默认，
// 并且两项都标注为「非账号级」。
func TestAccountConfigInheritsTenantDefault(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{
		KeySupportEnabled: "true",
		KeySupportPreset:  string(capability.PresetStandard),
	}
	// config、err 是该账号的生效配置与解析错误。
	config, err := service.AccountConfig(context.Background(), 1, "cookie-a")
	if err != nil {
		t.Fatalf("解析账号配置失败: %v", err)
	}
	if config.CookieID != "cookie-a" || !config.Enabled || config.Preset != capability.PresetStandard {
		t.Fatalf("继承结果=%+v，期望账号 a 开启且为标准档", config)
	}
	if config.AccountEnabled != nil || config.AccountPreset != nil {
		t.Fatal("继承租户默认时不得标注为账号级覆盖")
	}
}

// TestAccountConfigAppliesAccountOverride 验证账号级覆盖优先于租户默认。
func TestAccountConfigAppliesAccountOverride(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{
		KeySupportEnabled: "true",
		KeySupportPreset:  string(capability.PresetStandard),
	}
	repo.accounts["cookie-a"] = AccountOverride{Enabled: boolPtr(false), Preset: stringPtr(string(capability.PresetReadonly))}
	// config、err 是该账号的生效配置与解析错误。
	config, err := service.AccountConfig(context.Background(), 1, "cookie-a")
	if err != nil {
		t.Fatalf("解析账号配置失败: %v", err)
	}
	if config.Enabled || config.Preset != capability.PresetReadonly {
		t.Fatalf("账号级覆盖结果=%+v，期望关闭且只读档", config)
	}
	if config.AccountEnabled == nil || config.AccountPreset == nil {
		t.Fatal("账号级覆盖必须标注来源")
	}
}

// TestAccountConfigKeepsUncoveredDimension 验证账号只覆盖一个维度时，
// 另一个维度仍然继承租户默认。
func TestAccountConfigKeepsUncoveredDimension(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{
		KeySupportEnabled: "true",
		KeySupportPreset:  string(capability.PresetStandard),
	}
	repo.accounts["cookie-b"] = AccountOverride{Preset: stringPtr(string(capability.PresetAdvanced))}
	// config、err 是该账号的生效配置与解析错误。
	config, err := service.AccountConfig(context.Background(), 1, "cookie-b")
	if err != nil {
		t.Fatalf("解析账号配置失败: %v", err)
	}
	if !config.Enabled {
		t.Fatal("账号未覆盖开关时必须继承租户的开启状态")
	}
	if config.Preset != capability.PresetAdvanced {
		t.Fatalf("账号覆盖档位=%q，期望高级档", config.Preset)
	}
	if config.AccountEnabled != nil || config.AccountPreset == nil {
		t.Fatal("来源标注必须与覆盖范围一致")
	}
}

// TestAccountConfigRejectsForeignAccount 验证跨租户账号读取被直接拒绝，
// 不允许回落到继承值继续执行。
func TestAccountConfigRejectsForeignAccount(t *testing.T) {
	// service 是只需读取断言、不需要检查落库的配置服务。
	service, _ := newTestService()
	// err 是跨租户账号的解析错误，必须是 ErrAccountNotOwned。
	_, err := service.AccountConfig(context.Background(), 1, "cookie-foreign")
	if !errors.Is(err, ErrAccountNotOwned) {
		t.Fatalf("跨租户读取错误=%v，期望 ErrAccountNotOwned", err)
	}
}

// TestAccountConfigFallsBackOnIllegalAccountPreset 验证账号级非法档位回落到只读档，
// 使账号无法借坏值拿到超出平台定义的档位。
func TestAccountConfigFallsBackOnIllegalAccountPreset(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.tenant[1] = map[string]string{KeySupportPreset: string(capability.PresetAdvanced)}
	repo.accounts["cookie-a"] = AccountOverride{Preset: stringPtr("god-mode")}
	// config、err 是该账号的生效配置与解析错误。
	config, err := service.AccountConfig(context.Background(), 1, "cookie-a")
	if err != nil {
		t.Fatalf("解析账号配置失败: %v", err)
	}
	if config.Preset != capability.PresetReadonly {
		t.Fatalf("账号级非法档位必须回落只读档，实际 %q", config.Preset)
	}
	if config.AccountPreset == nil || *config.AccountPreset != capability.PresetReadonly {
		t.Fatal("回落结果仍应标注为账号级覆盖，便于界面如实展示")
	}
}

// TestUpdateAccountOverrideRejectsIllegalPreset 验证账号级非法档位不落库。
func TestUpdateAccountOverrideRejectsIllegalPreset(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	// err 是非法档位的账号级写入错误，必须是 ErrInvalidPreset。
	err := service.UpdateAccountOverride(context.Background(), 1, "cookie-a", AccountOverride{Preset: stringPtr("destructive")})
	if !errors.Is(err, ErrInvalidPreset) {
		t.Fatalf("账号级非法档位错误=%v，期望 ErrInvalidPreset", err)
	}
	if repo.accountWrites != 0 {
		t.Fatalf("非法档位不得落库，实际写入 %d 次", repo.accountWrites)
	}
}

// TestUpdateAccountOverrideRejectsForeignAccount 验证跨租户账号写入被拒绝。
func TestUpdateAccountOverrideRejectsForeignAccount(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	// err 是跨租户账号的写入错误，必须是 ErrAccountNotOwned。
	err := service.UpdateAccountOverride(context.Background(), 1, "cookie-foreign", AccountOverride{Enabled: boolPtr(true)})
	if !errors.Is(err, ErrAccountNotOwned) {
		t.Fatalf("跨租户写入错误=%v，期望 ErrAccountNotOwned", err)
	}
	if repo.accountWrites != 0 {
		t.Fatalf("跨租户写入不得落库，实际写入 %d 次", repo.accountWrites)
	}
}

// TestUpdateAccountOverrideClearsToInherit 验证两个字段都为空表示恢复完全继承。
func TestUpdateAccountOverrideClearsToInherit(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	repo.accounts["cookie-a"] = AccountOverride{Enabled: boolPtr(true), Preset: stringPtr(string(capability.PresetAdvanced))}
	// err 是恢复继承的写入错误。
	err := service.UpdateAccountOverride(context.Background(), 1, "cookie-a", AccountOverride{})
	if err != nil {
		t.Fatalf("恢复继承失败: %v", err)
	}
	// stored 是恢复继承后的账号覆盖记录。
	stored := repo.accounts["cookie-a"]
	if stored.Enabled != nil || stored.Preset != nil {
		t.Fatalf("恢复继承后覆盖记录=%+v，期望两个维度均为空", stored)
	}
	repo.tenant[1] = map[string]string{
		KeySupportEnabled: "true",
		KeySupportPreset:  string(capability.PresetStandard),
	}
	// config、configErr 是恢复继承后的生效配置与解析错误。
	config, configErr := service.AccountConfig(context.Background(), 1, "cookie-a")
	if configErr != nil {
		t.Fatalf("解析账号配置失败: %v", configErr)
	}
	if !config.Enabled || config.Preset != capability.PresetStandard {
		t.Fatalf("恢复继承后应回到租户默认，实际 %+v", config)
	}
}

// TestServiceWithoutRepositoryReportsNotConfigured 验证未装配仓储时用例整体不可用，
// 而不是静默返回零值配置。
func TestServiceWithoutRepositoryReportsNotConfigured(t *testing.T) {
	// service 是未装配仓储的配置服务。
	service := NewService(nil)
	// tenantErr 是未装配仓储时的租户配置读取错误。
	_, tenantErr := service.TenantConfig(context.Background(), 1)
	if !errors.Is(tenantErr, ErrNotConfigured) {
		t.Fatalf("未装配仓储的读取错误=%v，期望 ErrNotConfigured", tenantErr)
	}
	// accountErr 是未装配仓储时的账号配置读取错误。
	_, accountErr := service.AccountConfig(context.Background(), 1, "cookie-a")
	if !errors.Is(accountErr, ErrNotConfigured) {
		t.Fatalf("未装配仓储的账号读取错误=%v，期望 ErrNotConfigured", accountErr)
	}
	// updateErr 是未装配仓储时的账号级写入错误。
	updateErr := service.UpdateAccountOverride(context.Background(), 1, "cookie-a", AccountOverride{})
	if !errors.Is(updateErr, ErrNotConfigured) {
		t.Fatalf("未装配仓储的写入错误=%v，期望 ErrNotConfigured", updateErr)
	}
}

// TestRepositoryErrorPropagates 验证仓储错误被原样上抛，不被收敛成默认值。
func TestRepositoryErrorPropagates(t *testing.T) {
	// service、repo 是配置服务与预置归属关系的替身仓储。
	service, repo := newTestService()
	// boom 是注入的仓储故障。
	boom := errors.New("仓储不可用")
	repo.err = boom
	// err 是故障期间的租户配置读取错误。
	_, err := service.TenantConfig(context.Background(), 1)
	if !errors.Is(err, boom) {
		t.Fatalf("仓储错误=%v，期望原样上抛 %v", err, boom)
	}
}
