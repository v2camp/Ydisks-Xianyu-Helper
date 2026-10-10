// tool_risk_projection_test.go 验证 MCP 工具的危险等级只有一个来源：平台标注表。
//
// 三个断言互不重叠：注册表与标注表名称双向一致、投影结果与表逐条相等、
// 等级与既有确认标记不出现矛盾组合。任何一条失败都说明「同一动作出现了两种说法」。

package mcp

import (
	"sort"
	"testing"

	"xianyu-go/internal/capability"
)

// TestToolRiskProjectionCoversInventory 对照注册表与平台标注表，双向要求名称集合一致。
func TestToolRiskProjectionCoversInventory(t *testing.T) {
	// endpoint、_、_ 是全域端点。
	endpoint, _, _ := newFullDomainEndpoint(t, newSecretFixture())
	// table、err 是平台标注表及其构造错误。
	table, err := capability.PlatformToolRisks()
	if err != nil {
		t.Fatalf("构造平台工具危险等级表失败: %v", err)
	}
	// want 是 FR-13 清单登记的工具名集合。
	want := map[string]bool{}
	// names 是当前域的工具名清单。
	for _, names := range fr13Inventory {
		// name 是当前域中的工具名。
		for _, name := range names {
			want[name] = true
		}
	}
	// defs 是注册表实际工具定义，按名排序。
	defs := endpoint.ToolDefs()
	if len(defs) != len(want) {
		t.Fatalf("工具数量与 FR-13 清单不一致：got=%d want=%d", len(defs), len(want))
	}
	// def 是注册表中的当前工具定义。
	for _, def := range defs {
		// risk、ok 是标注表给出的等级与命中标志。
		risk, ok := table.Lookup(def.Name)
		if !ok {
			t.Fatalf("工具 %s 未登记危险等级，不应出现在注册表里", def.Name)
		}
		if !def.Risk.Valid() {
			t.Fatalf("工具 %s 的危险等级非法：got=%q", def.Name, def.Risk)
		}
		// 投影必须逐条相等：声明处自填的值会被覆盖，出现不等说明来源分裂。
		if def.Risk != risk {
			t.Fatalf("工具 %s 的危险等级与标注表不符：got=%q want=%q", def.Name, def.Risk, risk)
		}
	}
	// extra 是标注表里有、FR-13 清单里没有的工具名，多半是改过名后漏删。
	extra := make([]string, 0)
	// name 是标注表中的当前工具名。
	for _, name := range table.Names() {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Fatalf("标注表存在 %d 个清单外工具名：%v", len(extra), extra)
	}
}

// TestToolRiskAnnotationsConsistentWithConfirm 验证危险等级与既有同步确认标记不出现矛盾组合。
//
// 两个方向都要成立：只读工具不应要求确认（否则每次查询都要 Harness 填 confirm），
// 高危工具必须要求确认（否则不可逆动作可以在无人确认时直接发生）。
// 反向不成立是有意为之：本地可逆写入（建卡券组、改设置）属 write 但不需要确认，
// 平台触达类动作（刷新、同步、发布、发送）同属 write 且要求确认。
func TestToolRiskAnnotationsConsistentWithConfirm(t *testing.T) {
	// endpoint、_、_ 是全域端点。
	endpoint, _, _ := newFullDomainEndpoint(t, newSecretFixture())
	// counts 统计各等级的落位数量，用于断言没有整档被漏标。
	counts := map[capability.Risk]int{}
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		counts[def.Risk]++
		if def.Risk == capability.RiskRead && def.Destructive {
			t.Fatalf("只读工具 %s 不应要求同步确认", def.Name)
		}
		// highRisk 表示该等级属高危档，与内核的 elevated 判定同义。
		highRisk := def.Risk == capability.RiskAccount || def.Risk == capability.RiskDestructive
		if highRisk && !def.Destructive {
			t.Fatalf("高危工具 %s 必须要求同步确认", def.Name)
		}
	}
	// 四个档位在同一批工具里都必须出现，否则说明整档标注缺失。
	// required 是必须有落位的最低要求集合。
	for _, required := range []capability.Risk{capability.RiskRead, capability.RiskWrite, capability.RiskAccount, capability.RiskDestructive} {
		if counts[required] == 0 {
			t.Fatalf("危险等级 %q 在注册表中没有任何落位", required)
		}
	}
}

// TestDestructiveToolSetSizeFrozen 冻结破坏性工具数量。
//
// 危险等级标注是新增维度，不得顺带改变既有确认行为：破坏性工具的集合与名称由
// 各域测试逐条断言，这里再钉住总数，任何一处误改都会同时打穿两层。
func TestDestructiveToolSetSizeFrozen(t *testing.T) {
	// endpoint、_、_ 是全域端点。
	endpoint, _, _ := newFullDomainEndpoint(t, newSecretFixture())
	// destructiveCount 是标记为破坏性的工具数量。
	destructiveCount := 0
	// def 是注册表中的当前工具定义。
	for _, def := range endpoint.ToolDefs() {
		if def.Destructive {
			destructiveCount++
		}
	}
	// want 是标注危险等级之前就有的破坏性工具数量，本阶段必须保持不变。
	const want = 39
	if destructiveCount != want {
		t.Fatalf("破坏性工具数量发生变化：got=%d want=%d", destructiveCount, want)
	}
}

// TestUnregisteredToolRiskSkipsRegistration 验证未登记危险等级的工具不注册。
func TestUnregisteredToolRiskSkipsRegistration(t *testing.T) {
	// partial、err 是刻意只登记一个自检工具的标注表及其构造错误。
	partial, err := capability.NewToolRiskTable(capability.ToolRiskSpec{Name: "system_ping", Risk: capability.RiskRead})
	if err != nil {
		t.Fatalf("构造部分标注表失败: %v", err)
	}
	// endpoint、endpointErr 是被测端点及其构造错误。
	endpoint, endpointErr := NewEndpoint(EndpointConfig{
		Config:    &fakeConfig{enabled: true, hasToken: true, validToken: "partial-table-secret"},
		Identity:  fakeIdentity{userID: 1},
		ToolRisks: partial,
	})
	if endpointErr != nil {
		t.Fatalf("构造端点失败: %v", endpointErr)
	}
	endpoint.RegisterSystemTools()
	// registered 是实际注册的工具名集合。
	registered := map[string]bool{}
	// name 是已注册的当前工具名。
	for name := range endpoint.mcpServer.ListTools() {
		registered[name] = true
	}
	if !registered["system_ping"] {
		t.Fatal("已登记危险等级的工具应正常注册")
	}
	// 未登记等级的工具必须缺席：宁可少一个工具，也不能暴露等级未知的动作。
	// name 是注册表中缺席的候选工具名。
	for _, name := range []string{"system_version", "auth_whoami"} {
		if registered[name] {
			t.Fatalf("未登记危险等级的工具 %s 不应注册", name)
		}
	}
}

// TestEndpointRequiresToolRisks 验证缺少标注表时端点构造失败。
func TestEndpointRequiresToolRisks(t *testing.T) {
	// _、err 是端点构造结果与构造错误。
	_, err := NewEndpoint(EndpointConfig{
		Config:   &fakeConfig{enabled: true, hasToken: true, validToken: "no-table-secret"},
		Identity: fakeIdentity{userID: 1},
	})
	if err == nil {
		t.Fatal("缺少工具危险等级标注表时应构造失败")
	}
}
