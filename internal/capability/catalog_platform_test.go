package capability

import "testing"

// TestPlatformCatalogRegistersExpectedCapabilities 覆盖平台目录的登记内容与非空约束。
func TestPlatformCatalogRegistersExpectedCapabilities(t *testing.T) {
	// catalog 是平台内置能力目录。
	catalog, err := PlatformCatalog()
	if err != nil {
		t.Fatalf("构造平台能力目录失败：%v", err)
	}
	// want 是本期必须登记的能力名清单。
	want := []string{
		CapItemGet, CapItemList, CapOpsChatDigest, CapOpsHealthSnapshot,
		CapOpsSalesSnapshot, CapOrderGet, CapOrderList,
	}
	// names 是目录实际输出的能力名（已按字典序排列）。
	names := catalog.Names()
	if len(names) != len(want) {
		t.Fatalf("能力数量不符：got=%d want=%d", len(names), len(want))
	}
	// i 是当前比对的下标。
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("第 %d 个能力名不符：got=%q want=%q", i, names[i], want[i])
		}
	}
	// name 是当前待校验的能力名；本期能力必须全部为只读，且不得含高危等级。
	for _, name := range names {
		// got 是当前能力的声明内容。
		if got := mustSpec(t, catalog, name); got.Risk != RiskRead {
			t.Fatalf("能力 %q 应为只读等级：got=%q", name, got.Risk)
		}
	}
}

// TestPlatformCatalogSupportAgentScope 覆盖客服 Agent 只能触达只读能力、不能触达运营汇总能力。
func TestPlatformCatalogSupportAgentScope(t *testing.T) {
	// catalog 是平台内置能力目录。
	catalog, err := PlatformCatalog()
	if err != nil {
		t.Fatalf("构造平台能力目录失败：%v", err)
	}
	// cases 是逐个断言的求值场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// preset 是账号生效档位。
		preset Preset
		// capabilityName 是目标能力名。
		capabilityName string
		// want 是期望决策。
		want Decision
	}{
		{name: "只读档可查订单详情", preset: PresetReadonly, capabilityName: CapOrderGet, want: DecisionAllow},
		{name: "只读档可查订单列表", preset: PresetReadonly, capabilityName: CapOrderList, want: DecisionAllow},
		{name: "只读档可查商品详情", preset: PresetReadonly, capabilityName: CapItemGet, want: DecisionAllow},
		{name: "只读档可查商品列表", preset: PresetReadonly, capabilityName: CapItemList, want: DecisionAllow},
		{name: "只读档不可用运营销量汇总", preset: PresetReadonly, capabilityName: CapOpsSalesSnapshot, want: DecisionDeny},
		{name: "高级档不可用运营销量汇总", preset: PresetAdvanced, capabilityName: CapOpsSalesSnapshot, want: DecisionDeny},
		{name: "高级档不可用运营健康快照", preset: PresetAdvanced, capabilityName: CapOpsHealthSnapshot, want: DecisionDeny},
		{name: "高级档不可用运营会话摘要", preset: PresetAdvanced, capabilityName: CapOpsChatDigest, want: DecisionDeny},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// principal 是客服身份，账号作用域锁定在自身账号。
			principal := Principal{Kind: PrincipalSupportAgent, UserID: 7, CookieID: "acc-1", Preset: tc.preset}
			// got 是实际求值结果。
			got := Evaluate(catalog, Request{Principal: principal, Capability: tc.capabilityName})
			if got != tc.want {
				t.Fatalf("Evaluate 结果不符：got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestPlatformCatalogOpsAgentScope 覆盖运营 Agent 对本平台只读能力一律直接放行。
//
// 这是「运营命令迁移到能力内核后行为不变」的结构保证：
// 只读等级在 ops_agent 分支下恒为 allow，不存在需要确认或拒绝的路径。
func TestPlatformCatalogOpsAgentScope(t *testing.T) {
	// catalog 是平台内置能力目录。
	catalog, err := PlatformCatalog()
	if err != nil {
		t.Fatalf("构造平台能力目录失败：%v", err)
	}
	// principal 是运营 Agent 身份，权限等同管理员且可跨账号。
	principal := Principal{Kind: PrincipalOpsAgent, UserID: 7}
	// name 是当前待校验的能力名。
	for _, name := range catalog.Names() {
		// got 是实际求值结果。
		got := Evaluate(catalog, Request{Principal: principal, Capability: name})
		if got != DecisionAllow {
			t.Fatalf("运营 Agent 对只读能力 %q 应直接放行：got=%q", name, got)
		}
	}
}

// TestPlatformCatalogRemoteHarnessScope 覆盖外部 Harness 对只读能力不需确认即可调用。
func TestPlatformCatalogRemoteHarnessScope(t *testing.T) {
	// catalog 是平台内置能力目录。
	catalog, err := PlatformCatalog()
	if err != nil {
		t.Fatalf("构造平台能力目录失败：%v", err)
	}
	// principal 是外部 Harness 身份。
	principal := Principal{Kind: PrincipalRemoteHarness, UserID: 7}
	// name 是当前待校验的能力名。
	for _, name := range catalog.Names() {
		// got 是实际求值结果。
		got := Evaluate(catalog, Request{Principal: principal, Capability: name})
		if got != DecisionAllow {
			t.Fatalf("外部 Harness 对只读能力 %q 应直接放行：got=%q", name, got)
		}
	}
}

// mustSpec 取出能力声明，缺失时终止用例。
func mustSpec(t *testing.T, catalog *Catalog, name string) Spec {
	t.Helper()
	// spec、ok 分别是查得的声明与命中标志。
	spec, ok := catalog.Lookup(name)
	if !ok {
		t.Fatalf("能力 %q 未登记", name)
	}
	return spec
}
