// support_agent_adapter_test.go 覆盖客服 Agent 接线的投影与 fail-closed 行为：
// 账号级配置解析（含未配置、归属失败、读取失败、非法档位回落）与工厂在依赖缺失时返回 nil。

package runtime

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/agent"
	agentadminapp "xianyu-go/internal/application/agentadmin"
	itemapp "xianyu-go/internal/application/items"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
)

// stubAgentConfigRepository 是客服 Agent 配置仓储替身，返回构造时给定的预设结果。
type stubAgentConfigRepository struct {
	// tenantValues 是租户级设置原文。
	tenantValues map[string]string
	// override 是账号级覆盖。
	override agentadminapp.AccountOverride
	// found 表示账号是否通过归属校验。
	found bool
	// readErr 是恒定返回的读取失败原因。
	readErr error
}

// TenantValues 返回预设的租户级设置原文。
func (r *stubAgentConfigRepository) TenantValues(_ context.Context, _ int64) (map[string]string, error) {
	return r.tenantValues, r.readErr
}

// SetTenantValues 在本替身中不落库。
func (r *stubAgentConfigRepository) SetTenantValues(_ context.Context, _ int64, _ map[string]string) error {
	return nil
}

// AccountOverride 返回预设的账号级覆盖与归属判定结果。
func (r *stubAgentConfigRepository) AccountOverride(_ context.Context, _ int64, _ string) (agentadminapp.AccountOverride, bool, error) {
	return r.override, r.found, r.readErr
}

// SetAccountOverride 在本替身中不落库。
func (r *stubAgentConfigRepository) SetAccountOverride(_ context.Context, _ int64, _ string, _ agentadminapp.AccountOverride) (bool, error) {
	return r.found, nil
}

// sessionsWithRepository 构造注入了指定配置仓储的账号级解析端口。
func sessionsWithRepository(repository agentadminapp.Repository) agent.SessionResolver {
	// ports 是恒定返回同一份服务快照的端口提供者。
	ports := func() composition.TransportPorts {
		return composition.TransportPorts{AgentSupport: agentadminapp.NewService(repository)}
	}
	// ownerOf 是恒定归属同一租户的账号归属解析函数。
	ownerOf := func(_ context.Context, _ string) (int64, error) { return 42, nil }
	return newSupportAgentSessions(ports, ownerOf)
}

// TestSupportAgentSessionsReturnsDisabledWhenConfigServiceMissing 验证配置服务未装配时按未启用返回。
func TestSupportAgentSessionsReturnsDisabledWhenConfigServiceMissing(t *testing.T) {
	// sessions 是配置服务缺失的解析端口。
	sessions := newSupportAgentSessions(func() composition.TransportPorts { return composition.TransportPorts{} },
		func(_ context.Context, _ string) (int64, error) { return 42, nil })

	// session、enabled、err 分别是解析结果、启用状态与失败原因。
	session, enabled, err := sessions.ResolveSession(context.Background(), "cid-1")
	if err != nil || enabled || session.CookieID != "" {
		t.Fatalf("配置服务缺失时必须按未启用返回: session=%+v enabled=%v err=%v", session, enabled, err)
	}
}

// TestNewSupportAgentSessionsRequiresDependencies 验证任一依赖缺失时返回未包装的 nil。
func TestNewSupportAgentSessionsRequiresDependencies(t *testing.T) {
	// ports 是占位端口提供者。
	ports := func() composition.TransportPorts { return composition.TransportPorts{} }
	// ownerOf 是占位归属解析函数。
	ownerOf := func(_ context.Context, _ string) (int64, error) { return 42, nil }
	if newSupportAgentSessions(nil, ownerOf) != nil {
		t.Fatal("缺少端口提供者时应返回 nil")
	}
	if newSupportAgentSessions(ports, nil) != nil {
		t.Fatal("缺少归属解析函数时应返回 nil")
	}
}

// TestSupportAgentSessionsResolvesAccountPreset 验证账号级覆盖优先于租户默认，并带上归属信息。
func TestSupportAgentSessionsResolvesAccountPreset(t *testing.T) {
	// records 是当前待验证的生效配置场景。
	records := []struct {
		// name 是场景名称。
		name string
		// repository 是该场景使用的配置仓储。
		repository agentadminapp.Repository
		// wantPreset 是该场景期望生效的档位。
		wantPreset capability.Preset
		// wantEnabled 是该场景期望的启用状态。
		wantEnabled bool
	}{
		{
			name: "租户默认开启只读档",
			repository: &stubAgentConfigRepository{
				tenantValues: map[string]string{agentadminapp.KeySupportEnabled: "true", agentadminapp.KeySupportPreset: "readonly"},
				found:        true,
			},
			wantPreset: capability.PresetReadonly, wantEnabled: true,
		},
		{
			name: "账号覆盖为关闭",
			repository: &stubAgentConfigRepository{
				tenantValues: map[string]string{agentadminapp.KeySupportEnabled: "true"},
				override:     agentadminapp.AccountOverride{Enabled: boolPointer(false)},
				found:        true,
			},
			wantPreset: capability.PresetReadonly, wantEnabled: false,
		},
		{
			name: "账号覆盖为高档位",
			repository: &stubAgentConfigRepository{
				tenantValues: map[string]string{agentadminapp.KeySupportEnabled: "true", agentadminapp.KeySupportPreset: "readonly"},
				override:     agentadminapp.AccountOverride{Preset: stringPointer("advanced")},
				found:        true,
			},
			wantPreset: capability.PresetAdvanced, wantEnabled: true,
		},
		{
			name: "库中档位非法时回落只读档",
			repository: &stubAgentConfigRepository{
				tenantValues: map[string]string{agentadminapp.KeySupportEnabled: "true", agentadminapp.KeySupportPreset: "unlimited"},
				found:        true,
			},
			wantPreset: capability.PresetReadonly, wantEnabled: true,
		},
	}
	// current 是当前待验证的场景。
	for _, current := range records {
		// sessions 是当前场景的解析端口。
		sessions := sessionsWithRepository(current.repository)
		// session、enabled、err 分别是解析结果、启用状态与失败原因。
		session, enabled, err := sessions.ResolveSession(context.Background(), "cid-1")
		if err != nil {
			t.Fatalf("场景[%s]解析失败: %v", current.name, err)
		}
		if enabled != current.wantEnabled || session.Preset != current.wantPreset {
			t.Fatalf("场景[%s]解析结果不符: enabled=%v preset=%s", current.name, enabled, session.Preset)
		}
		if session.UserID != 42 || session.CookieID != "cid-1" {
			t.Fatalf("场景[%s]作用域不符: %+v", current.name, session)
		}
	}
}

// TestSupportAgentSessionsSurfacesHardFailures 验证归属失败与配置读取失败都上抛，
// 由适配器按「未知档位不得运行工具回合」的规则回落到单次问答。
func TestSupportAgentSessionsSurfacesHardFailures(t *testing.T) {
	// ports 是提供配置服务的端口提供者。
	ports := func() composition.TransportPorts {
		return composition.TransportPorts{AgentSupport: agentadminapp.NewService(&stubAgentConfigRepository{found: true})}
	}
	// brokenOwner 是归属解析失败的替身。
	brokenOwner := func(_ context.Context, _ string) (int64, error) { return 0, errors.New("账号不存在") }
	// session、enabled、err 分别是归属失败场景的解析结果。
	session, enabled, err := newSupportAgentSessions(ports, brokenOwner).ResolveSession(context.Background(), "cid-1")
	if err == nil || enabled || session.CookieID != "" {
		t.Fatalf("归属失败必须上抛且不得启用: session=%+v enabled=%v err=%v", session, enabled, err)
	}

	// readFailure 是配置读取失败的解析端口。
	readFailure := newSupportAgentSessions(func() composition.TransportPorts {
		return composition.TransportPorts{AgentSupport: agentadminapp.NewService(&stubAgentConfigRepository{readErr: errors.New("读库失败")})}
	}, func(_ context.Context, _ string) (int64, error) { return 42, nil })
	// readEnabled、readErr 分别是配置读取失败场景的启用状态与失败原因。
	_, readEnabled, readErr := readFailure.ResolveSession(context.Background(), "cid-1")
	if readErr == nil || readEnabled {
		t.Fatalf("配置读取失败必须上抛且不得启用: enabled=%v err=%v", readEnabled, readErr)
	}
}

// TestNewSupportAgentFactoryRequiresDependencies 验证工厂依赖缺失时返回 nil，engine 沿用默认单次问答。
func TestNewSupportAgentFactoryRequiresDependencies(t *testing.T) {
	// ports 是占位端口提供者。
	ports := func() composition.TransportPorts { return composition.TransportPorts{} }
	// lifecycleContext 是占位生命周期 Context 提供者。
	lifecycleContext := func() context.Context { return context.Background() }
	// sessions 是占位会话解析端口。
	sessions := newSupportAgentSessions(ports, func(_ context.Context, _ string) (int64, error) { return 42, nil })

	if newSupportAgentFactory(nil, ports, lifecycleContext, nil) != nil {
		t.Fatal("缺少会话解析端口时应返回 nil 工厂")
	}
	if newSupportAgentFactory(sessions, nil, lifecycleContext, nil) != nil {
		t.Fatal("缺少端口提供者时应返回 nil 工厂")
	}
	if newSupportAgentFactory(sessions, ports, nil, nil) != nil {
		t.Fatal("缺少生命周期 Context 时应返回 nil 工厂")
	}
}

// TestBuildSupportAgentFactoryReturnsNilWithoutAccountStore 验证缺少账号仓储时整体不启用。
func TestBuildSupportAgentFactoryReturnsNilWithoutAccountStore(t *testing.T) {
	if buildSupportAgentFactory(nil, func() *composition.Services { return nil }, nil) != nil {
		t.Fatal("缺少账号仓储时应返回 nil 工厂")
	}
}

// TestNewSupportAgentLoopReturnsNilOnIncompletePorts 验证应用服务未装配时工具层返回 nil。
func TestNewSupportAgentLoopReturnsNilOnIncompletePorts(t *testing.T) {
	// loop 是应用服务为空时的工具回合循环。
	loop := newSupportAgentLoop(func() composition.TransportPorts { return composition.TransportPorts{} },
		func() context.Context { return context.Background() }, nil)
	if loop != nil {
		t.Fatal("订单与商品端口缺失时不得构造出可运行的工具层")
	}
}

// TestSupportAgentFactoryReturnsGeneratorWhenDependenciesComplete 验证端口齐备时工厂真的产出生成器。
//
// 这是接线里唯一「配错了就永久静默失效」的地方：任何一处判空写错都会让所有账号都拿到 nil 生成器，
// 而现象只是「Agent 没生效」。因此这里用零值应用服务把判空链走通，只断言装配结果不触发任何用例调用。
func TestSupportAgentFactoryReturnsGeneratorWhenDependenciesComplete(t *testing.T) {
	// snapshot 是字段非空、内部服务为零值的端口快照。
	snapshot := composition.TransportPorts{
		Orders: &orderapp.ServiceSet{}, OrderRefreshJobs: &orderapp.RefreshJobService{},
		ItemCatalog: &itemapp.CatalogService{}, ItemCatalogMutation: &itemapp.CatalogMutationService{},
		ItemSync: &itemapp.SyncService{}, ItemSinglePublish: &itemapp.Service{},
		ItemCategoryRecommendation:  &itemapp.CategoryRecommendationService{},
		ItemBatchPreview:            &itemapp.BatchPreviewService{},
		ItemBatchPreviewPersistence: &itemapp.BatchPreviewPersistenceService{},
		ItemBatchManagement:         &itemapp.BatchManagementService{},
		AgentSupport:                agentadminapp.NewService(&stubAgentConfigRepository{found: true}),
	}
	// ports 是恒定返回该快照的端口提供者。
	ports := func() composition.TransportPorts { return snapshot }
	// sessions 是依赖齐备的账号级解析端口。
	sessions := newSupportAgentSessions(ports, func(_ context.Context, _ string) (int64, error) { return 42, nil })
	// factory 是待验证的账号级生成器工厂。
	factory := newSupportAgentFactory(sessions, ports, func() context.Context { return context.Background() }, nil)
	if factory == nil {
		t.Fatal("依赖齐备时应返回非 nil 工厂")
	}
	// generator、isReplier 分别是工厂为该账号产出的生成器及其是否为客服 Agent 适配器。
	generator, isReplier := factory("cid-1").(*agent.Replier)
	if !isReplier || generator == nil {
		t.Fatalf("依赖齐备时应为账号产出客服 Agent 生成器: isReplier=%v", isReplier)
	}
}

// boolPointer 返回指向给定布尔值的指针，用于表达「账号级覆盖了开关」。
func boolPointer(value bool) *bool { return &value }

// stringPointer 返回指向给定字符串的指针，用于表达「账号级覆盖了档位」。
func stringPointer(value string) *string { return &value }
