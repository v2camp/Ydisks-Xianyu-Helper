// agent_support_repository_test.go 覆盖客服 Agent 配置端口在适配器层的投影：
// 构造期缺仓储时返回未包装的 nil，租户级与账号级配置按归属往返，跨租户账号如实报 found=false。

package adapter

import (
	"context"
	"testing"

	agentadminapp "xianyu-go/internal/application/agentadmin"
	"xianyu-go/internal/db"
)

// TestAgentSupportRepositoryWriteKeepsAIEnabled 验证通过端口写账号级档位不会影响 AI 开关：
// 账号原本没有设置行，写档位后 AI 生效值必须仍为「跟随总闸（开）」。
//
// 这是 E9 的核心边界——档位配置与 AI 开关彻底正交，否则「配一次 Agent 档位」会把账号静默关闭。
func TestAgentSupportRepositoryWriteKeepsAIEnabled(t *testing.T) {
	// store、closeStore 是带管理员与归属账号的测试数据库及其释放函数。
	store, closeStore := newAdapterTestStore(t)
	defer closeStore()
	// ctx 是本用例的调用上下文。
	ctx := context.Background()
	// admin、adminErr 是账号归属用户行与读取失败原因。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败: %v", adminErr)
	}
	// preset 是待写入的账号级档位覆盖值。
	preset := "standard"
	// writeFound、writeErr 是该次写入的归属判定与失败原因。
	writeFound, writeErr := NewAgentSupportRepository(store).SetAccountOverride(ctx, admin.ID, "cid", agentadminapp.AccountOverride{Preset: &preset})
	if writeErr != nil || !writeFound {
		t.Fatalf("写入账号级档位失败: found=%v err=%v", writeFound, writeErr)
	}
	// enabled、enabledErr 是该账号写入档位后的 AI 生效值。
	enabled, enabledErr := store.AIReply.IsEnabled(ctx, "cid")
	if enabledErr != nil {
		t.Fatalf("读取 AI 生效值失败: %v", enabledErr)
	}
	if !enabled {
		t.Fatal("写客服 Agent 档位不得把账号的 AI 静默关闭")
	}
}

// TestNewAgentSupportRepositoryRejectsIncompleteStore 验证 Store 或其成员仓储缺失时返回 nil 端口。
func TestNewAgentSupportRepositoryRejectsIncompleteStore(t *testing.T) {
	// cases 是两种缺少必需仓储的构造输入。
	cases := []struct {
		// name 是场景名称。
		name string
		// store 是该场景的数据库入口。
		store *db.Store
	}{
		{name: "缺 Store", store: nil},
		{name: "缺成员仓储", store: &db.Store{}},
	}
	// current 是当前待验证的场景。
	for _, current := range cases {
		// repository 是当前场景构造出的端口。
		repository := NewAgentSupportRepository(current.store)
		if repository != nil {
			t.Fatalf("场景[%s]应返回 nil 端口: %T", current.name, repository)
		}
	}
}

// TestAgentSupportRepositoryProjectsTenantAndAccountScope 验证投影按归属往返租户级与账号级配置，
// 并把跨租户账号如实报为 found=false——调用方对 found=false 必须按未授权处理，不得降级为继承值。
func TestAgentSupportRepositoryProjectsTenantAndAccountScope(t *testing.T) {
	// store、closeStore 是带管理员与归属账号的测试数据库及其释放函数。
	store, closeStore := newAdapterTestStore(t)
	defer closeStore()
	// ctx 是本用例的调用上下文。
	ctx := context.Background()
	// admin、adminErr 是账号归属用户行与读取失败原因。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败: %v", adminErr)
	}
	// repository 是待测试的客服 Agent 配置端口。
	repository := NewAgentSupportRepository(store)
	if repository == nil {
		t.Fatal("完整 Store 应构造出客服 Agent 配置端口")
	}

	// values 是待写入的租户级配置项；客服 Agent 已不再有独立开关键，只治理档位。
	values := map[string]string{agentadminapp.KeySupportPreset: "standard"}
	// setErr 是租户级配置写入失败原因。
	if setErr := repository.SetTenantValues(ctx, admin.ID, values); setErr != nil {
		t.Fatalf("写入租户级配置失败: %v", setErr)
	}
	// stored、readErr 是读回的租户级配置与读取失败原因。
	stored, readErr := repository.TenantValues(ctx, admin.ID)
	if readErr != nil {
		t.Fatalf("读取租户级配置失败: %v", readErr)
	}
	if stored[agentadminapp.KeySupportPreset] != "standard" {
		t.Fatalf("租户级配置往返不一致: %+v", stored)
	}

	// preset 是待写入的账号级档位覆盖值。
	preset := "advanced"
	// writeFound、writeErr 是账号级覆盖的归属判定与写入失败原因。
	writeFound, writeErr := repository.SetAccountOverride(ctx, admin.ID, "cid", agentadminapp.AccountOverride{Preset: &preset})
	if writeErr != nil || !writeFound {
		t.Fatalf("写入账号级覆盖失败: found=%v err=%v", writeFound, writeErr)
	}
	// override、overrideFound、overrideErr 是读回的账号级覆盖、归属判定与读取失败原因。
	override, overrideFound, overrideErr := repository.AccountOverride(ctx, admin.ID, "cid")
	if overrideErr != nil || !overrideFound {
		t.Fatalf("读取账号级覆盖失败: found=%v err=%v", overrideFound, overrideErr)
	}
	if override.Preset == nil || *override.Preset != "advanced" {
		t.Fatalf("账号级覆盖往返不一致: %+v", override)
	}

	// absentFound、absentErr 是缺失账号的读取结果；必须 found=false 且不报错。
	_, absentFound, absentErr := repository.AccountOverride(ctx, admin.ID, "cid-absent")
	if absentErr != nil || absentFound {
		t.Fatalf("缺失账号必须报 found=false: found=%v err=%v", absentFound, absentErr)
	}
	// foreignFound、foreignErr 是跨租户账号的写入结果；必须 found=false 且不落库。
	foreignFound, foreignErr := repository.SetAccountOverride(ctx, admin.ID, "cid-absent", agentadminapp.AccountOverride{})
	if foreignErr != nil || foreignFound {
		t.Fatalf("跨租户账号必须报 found=false: found=%v err=%v", foreignFound, foreignErr)
	}
}
