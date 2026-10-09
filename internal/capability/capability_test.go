package capability

import (
	"errors"
	"testing"
)

// testCatalog 构造一个覆盖五级危险等级、两种作用域与三档要求的测试用能力目录。
//
// 目录刻意包含两类边界声明：高危能力不设客服档位（account_op、destroy_item），
// 以及管理端专用能力（global_write）——它们都必须对客服 Agent 恒拒绝。
func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	// catalog 是构造完成的测试目录。
	catalog, err := NewCatalog(
		Spec{Name: "read_global", Risk: RiskRead, Scope: ScopeGlobal, MinPreset: PresetReadonly},
		Spec{Name: "read_account", Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly},
		Spec{Name: "quote_account", Risk: RiskQuote, Scope: ScopeAccount, MinPreset: PresetStandard},
		Spec{Name: "write_account", Risk: RiskWrite, Scope: ScopeAccount, MinPreset: PresetAdvanced},
		Spec{Name: "account_op", Risk: RiskAccount, Scope: ScopeAccount},
		Spec{Name: "destroy_item", Risk: RiskDestructive, Scope: ScopeAccount},
		Spec{Name: "global_write", Risk: RiskWrite, Scope: ScopeGlobal},
	)
	if err != nil {
		t.Fatalf("构造测试能力目录失败：%v", err)
	}
	return catalog
}

// TestEvaluateRemoteHarness 覆盖外部 Harness 的放行与同步确认规则。
func TestEvaluateRemoteHarness(t *testing.T) {
	// catalog 是被测能力目录。
	catalog := testCatalog(t)
	// principal 是在场操作者的身份视图。
	principal := Principal{Kind: PrincipalRemoteHarness, UserID: 7}
	// cases 是逐个断言的求值场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// capability 是目标能力名。
		capability string
		// confirmed 表示调用方是否已提供同步确认。
		confirmed bool
		// want 是期望决策。
		want Decision
	}{
		{name: "只读能力直接放行", capability: "read_account", want: DecisionAllow},
		{name: "报价能力直接放行", capability: "quote_account", want: DecisionAllow},
		{name: "写入能力直接放行", capability: "write_account", want: DecisionAllow},
		{name: "账号能力未确认要求确认", capability: "account_op", want: DecisionRequireConfirm},
		{name: "账号能力已确认放行", capability: "account_op", confirmed: true, want: DecisionAllow},
		{name: "不可逆能力未确认要求确认", capability: "destroy_item", want: DecisionRequireConfirm},
		{name: "不可逆能力已确认放行", capability: "destroy_item", confirmed: true, want: DecisionAllow},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是实际求值结果。
			got := Evaluate(catalog, Request{Principal: principal, Capability: tc.capability, Confirmed: tc.confirmed})
			if got != tc.want {
				t.Fatalf("Evaluate 结果不符：got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestEvaluateOpsAgent 覆盖运营 Agent 的「变更即异步确认」规则。
func TestEvaluateOpsAgent(t *testing.T) {
	// catalog 是被测能力目录。
	catalog := testCatalog(t)
	// principal 是运营 Agent 的身份视图，权限等同管理员但无法同步确认。
	principal := Principal{Kind: PrincipalOpsAgent, UserID: 7}
	// cases 是逐个断言的求值场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// capability 是目标能力名。
		capability string
		// want 是期望决策。
		want Decision
	}{
		{name: "只读能力直接放行", capability: "read_account", want: DecisionAllow},
		{name: "报价能力直接放行", capability: "quote_account", want: DecisionAllow},
		{name: "写入能力转异步确认", capability: "write_account", want: DecisionRequireAsyncConfirm},
		{name: "账号能力转异步确认", capability: "account_op", want: DecisionRequireAsyncConfirm},
		{name: "不可逆能力转异步确认", capability: "destroy_item", want: DecisionRequireAsyncConfirm},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是实际求值结果。
			got := Evaluate(catalog, Request{Principal: principal, Capability: tc.capability})
			if got != tc.want {
				t.Fatalf("Evaluate 结果不符：got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestEvaluateSupportAgentPresetMatrix 覆盖客服 Agent 的档位矩阵与高危恒定拒绝。
func TestEvaluateSupportAgentPresetMatrix(t *testing.T) {
	// catalog 是被测能力目录。
	catalog := testCatalog(t)
	// cases 是逐个断言的求值场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// preset 是账号生效档位。
		preset Preset
		// capability 是目标能力名。
		capability string
		// want 是期望决策。
		want Decision
	}{
		{name: "只读档可用只读能力", preset: PresetReadonly, capability: "read_account", want: DecisionAllow},
		{name: "只读档不可用报价能力", preset: PresetReadonly, capability: "quote_account", want: DecisionDeny},
		{name: "只读档不可用写入能力", preset: PresetReadonly, capability: "write_account", want: DecisionDeny},
		{name: "标准档可用只读能力", preset: PresetStandard, capability: "read_account", want: DecisionAllow},
		{name: "标准档可用报价能力", preset: PresetStandard, capability: "quote_account", want: DecisionAllow},
		{name: "标准档不可用写入能力", preset: PresetStandard, capability: "write_account", want: DecisionDeny},
		{name: "高级档可用写入能力", preset: PresetAdvanced, capability: "write_account", want: DecisionAllow},
		{name: "只读档拒绝账号能力", preset: PresetReadonly, capability: "account_op", want: DecisionDeny},
		{name: "标准档拒绝账号能力", preset: PresetStandard, capability: "account_op", want: DecisionDeny},
		{name: "高级档拒绝账号能力", preset: PresetAdvanced, capability: "account_op", want: DecisionDeny},
		{name: "高级档拒绝不可逆能力", preset: PresetAdvanced, capability: "destroy_item", want: DecisionDeny},
		{name: "高级档拒绝无档位能力", preset: PresetAdvanced, capability: "global_write", want: DecisionDeny},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// principal 是客服 Agent 的身份视图，账号作用域锁定在自身账号。
			principal := Principal{Kind: PrincipalSupportAgent, UserID: 7, CookieID: "acc-1", Preset: tc.preset}
			// got 是实际求值结果。
			got := Evaluate(catalog, Request{Principal: principal, Capability: tc.capability})
			if got != tc.want {
				t.Fatalf("Evaluate 结果不符：got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestEvaluateFailsClosed 覆盖各类非法输入必须一律拒绝，防止新增取值时静默放行。
func TestEvaluateFailsClosed(t *testing.T) {
	// catalog 是被测能力目录。
	catalog := testCatalog(t)
	// validSupport 是完全合法的客服身份，仅用于隔离单一变量的非法性。
	validSupport := Principal{Kind: PrincipalSupportAgent, UserID: 7, CookieID: "acc-1", Preset: PresetAdvanced}
	// cases 是逐个断言的非法输入场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// catalog 是该场景使用的目录，nil 表示未装配。
		catalog *Catalog
		// principal 是调用方身份。
		principal Principal
		// capability 是目标能力名。
		capability string
	}{
		{name: "未装配目录一律拒绝", catalog: nil, principal: validSupport, capability: "read_account"},
		{name: "未登记能力一律拒绝", catalog: catalog, principal: validSupport, capability: "not_registered"},
		{name: "能力名为空一律拒绝", catalog: catalog, principal: validSupport, capability: ""},
		{name: "零值身份一律拒绝", catalog: catalog, principal: Principal{}, capability: "read_account"},
		{name: "未知调用方类型一律拒绝", catalog: catalog, principal: Principal{Kind: "unknown", UserID: 7}, capability: "read_account"},
		{name: "客服缺账号作用域一律拒绝", catalog: catalog, principal: Principal{Kind: PrincipalSupportAgent, UserID: 7, Preset: PresetAdvanced}, capability: "read_account"},
		{name: "客服缺档位一律拒绝", catalog: catalog, principal: Principal{Kind: PrincipalSupportAgent, UserID: 7, CookieID: "acc-1"}, capability: "read_account"},
		{name: "客服档位非法一律拒绝", catalog: catalog, principal: Principal{Kind: PrincipalSupportAgent, UserID: 7, CookieID: "acc-1", Preset: "vip"}, capability: "read_account"},
		{name: "管理端缺用户标识一律拒绝", catalog: catalog, principal: Principal{Kind: PrincipalRemoteHarness}, capability: "read_account"},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是实际求值结果。
			got := Evaluate(tc.catalog, Request{Principal: tc.principal, Capability: tc.capability})
			if got != DecisionDeny {
				t.Fatalf("非法输入未被拒绝：got=%q", got)
			}
		})
	}
}

// TestPresetAllows 覆盖档位比较，重点是未知档位必须恒不满足。
func TestPresetAllows(t *testing.T) {
	// cases 是逐个断言的档位比较场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// current 是账号当前档位。
		current Preset
		// required 是能力要求的最低档位。
		required Preset
		// want 是期望结果。
		want bool
	}{
		{name: "只读档满足只读要求", current: PresetReadonly, required: PresetReadonly, want: true},
		{name: "只读档不满足标准要求", current: PresetReadonly, required: PresetStandard, want: false},
		{name: "标准档满足只读要求", current: PresetStandard, required: PresetReadonly, want: true},
		{name: "标准档不满足高级要求", current: PresetStandard, required: PresetAdvanced, want: false},
		{name: "高级档满足全部要求", current: PresetAdvanced, required: PresetAdvanced, want: true},
		{name: "空档位不满足只读要求", current: "", required: PresetReadonly, want: false},
		{name: "未知档位不满足只读要求", current: "vip", required: PresetReadonly, want: false},
		{name: "空要求不成立", current: PresetAdvanced, required: "", want: false},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是实际比较结果。
			got := tc.current.Allows(tc.required)
			if got != tc.want {
				t.Fatalf("Allows 结果不符：got=%v want=%v", got, tc.want)
			}
		})
	}
}

// TestSpecValidRejectsContradiction 覆盖高危能力不得声明客服档位的自洽性约束。
func TestSpecValidRejectsContradiction(t *testing.T) {
	// cases 是逐个断言的声明校验场景。
	cases := []struct {
		// name 是场景名称。
		name string
		// spec 是待校验的声明。
		spec Spec
		// want 是期望结果。
		want bool
	}{
		{name: "只读能力带档位合法", spec: Spec{Name: "a", Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly}, want: true},
		{name: "写入能力无档位合法", spec: Spec{Name: "b", Risk: RiskWrite, Scope: ScopeGlobal}, want: true},
		{name: "高危能力无档位合法", spec: Spec{Name: "c", Risk: RiskAccount, Scope: ScopeAccount}, want: true},
		{name: "高危能力带档位非法", spec: Spec{Name: "d", Risk: RiskAccount, Scope: ScopeAccount, MinPreset: PresetAdvanced}, want: false},
		{name: "不可逆能力带档位非法", spec: Spec{Name: "e", Risk: RiskDestructive, Scope: ScopeAccount, MinPreset: PresetReadonly}, want: false},
		{name: "名称为空非法", spec: Spec{Risk: RiskRead, Scope: ScopeGlobal}, want: false},
		{name: "未知危险等级非法", spec: Spec{Name: "f", Risk: "unknown", Scope: ScopeGlobal}, want: false},
		{name: "未知作用域非法", spec: Spec{Name: "g", Risk: RiskRead, Scope: "tenant"}, want: false},
		{name: "未知档位要求非法", spec: Spec{Name: "h", Risk: RiskRead, Scope: ScopeGlobal, MinPreset: "vip"}, want: false},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是实际校验结果。
			got := tc.spec.Valid()
			if got != tc.want {
				t.Fatalf("Valid 结果不符：got=%v want=%v", got, tc.want)
			}
		})
	}
}

// TestNewCatalogRejectsInvalid 覆盖注册期错误必须显式暴露。
func TestNewCatalogRejectsInvalid(t *testing.T) {
	t.Run("名称重复报错", func(t *testing.T) {
		// duplicate 是两项同名的声明。
		duplicate := []Spec{
			{Name: "same", Risk: RiskRead, Scope: ScopeGlobal},
			{Name: "same", Risk: RiskWrite, Scope: ScopeGlobal},
		}
		// err 是登记同名能力时返回的错误。
		if _, err := NewCatalog(duplicate...); err == nil {
			t.Fatal("同名能力未报错")
		}
	})
	t.Run("声明非法报错", func(t *testing.T) {
		// invalid 是危险等级非法的声明。
		invalid := Spec{Name: "bad", Risk: "unknown", Scope: ScopeGlobal}
		// err 是登记非法声明时返回的错误。
		if _, err := NewCatalog(invalid); err == nil {
			t.Fatal("非法声明未报错")
		}
	})
	t.Run("空目录可用", func(t *testing.T) {
		// catalog 是空目录。
		catalog, err := NewCatalog()
		if err != nil {
			t.Fatalf("空目录构造失败：%v", err)
		}
		if catalog.Len() != 0 {
			t.Fatalf("空目录长度应为 0：got=%d", catalog.Len())
		}
	})
}

// TestCatalogLookupAndNames 覆盖目录查询与稳定排序输出。
func TestCatalogLookupAndNames(t *testing.T) {
	// catalog 是被测能力目录。
	catalog := testCatalog(t)
	t.Run("命中查询返回声明", func(t *testing.T) {
		// spec、ok 分别是查询结果与命中标志。
		spec, ok := catalog.Lookup("write_account")
		if !ok || spec.MinPreset != PresetAdvanced {
			t.Fatalf("查询结果不符：ok=%v spec=%+v", ok, spec)
		}
	})
	t.Run("未命中查询返回假", func(t *testing.T) {
		// ok 表示未登记能力是否被命中；恒为假才符合预期。
		if _, ok := catalog.Lookup("not_registered"); ok {
			t.Fatal("未登记能力不应命中")
		}
	})
	t.Run("空目录查询不 panic", func(t *testing.T) {
		// empty 是未装配的目录指针。
		var empty *Catalog
		// ok 表示空目录查询是否命中；恒为假才符合预期。
		if _, ok := empty.Lookup("any"); ok {
			t.Fatal("nil 目录不应命中")
		}
		if empty.Len() != 0 || empty.Names() != nil {
			t.Fatal("nil 目录应返回空结果")
		}
	})
	t.Run("名称按字典序输出", func(t *testing.T) {
		// names 是目录输出的能力名列表。
		names := catalog.Names()
		// want 是期望的字典序结果。
		want := []string{"account_op", "destroy_item", "global_write", "quote_account", "read_account", "read_global", "write_account"}
		if len(names) != len(want) {
			t.Fatalf("名称数量不符：got=%d want=%d", len(names), len(want))
		}
		// i 是当前比对的下标。
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("第 %d 个名称不符：got=%q want=%q", i, names[i], want[i])
			}
		}
	})
}

// TestResolveAccountScope 覆盖账号作用域断言，重点是客服端不可跨账号。
func TestResolveAccountScope(t *testing.T) {
	// support 是锁定在 acc-1 的客服身份。
	support := Principal{Kind: PrincipalSupportAgent, UserID: 7, CookieID: "acc-1", Preset: PresetAdvanced}
	// ops 是可跨账号的运营身份。
	ops := Principal{Kind: PrincipalOpsAgent, UserID: 7}
	// accountSpec 是账号作用域能力的声明。
	accountSpec := Spec{Name: "read_account", Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly}
	// globalSpec 是跨账号能力的声明。
	globalSpec := Spec{Name: "read_global", Risk: RiskRead, Scope: ScopeGlobal, MinPreset: PresetReadonly}
	t.Run("客服未指定账号回落自身账号", func(t *testing.T) {
		// scope、err 分别是解析结果与错误。
		scope, err := ResolveAccountScope(support, accountSpec, "")
		if err != nil || scope != "acc-1" {
			t.Fatalf("解析结果不符：scope=%q err=%v", scope, err)
		}
	})
	t.Run("客服指定自身账号通过", func(t *testing.T) {
		// scope、err 分别是解析结果与错误。
		scope, err := ResolveAccountScope(support, accountSpec, "acc-1")
		if err != nil || scope != "acc-1" {
			t.Fatalf("解析结果不符：scope=%q err=%v", scope, err)
		}
	})
	t.Run("客服指定他人账号被拒", func(t *testing.T) {
		// scope、err 分别是解析结果与错误。
		scope, err := ResolveAccountScope(support, accountSpec, "acc-2")
		if !errors.Is(err, ErrScopeViolation) || scope != "" {
			t.Fatalf("跨账号未被拒绝：scope=%q err=%v", scope, err)
		}
	})
	t.Run("跨账号能力不绑定账号", func(t *testing.T) {
		// scope、err 分别是解析结果与错误。
		scope, err := ResolveAccountScope(support, globalSpec, "")
		if err != nil || scope != "" {
			t.Fatalf("跨账号能力不应绑定账号：scope=%q err=%v", scope, err)
		}
	})
	t.Run("运营可指定任意账号", func(t *testing.T) {
		// scope、err 分别是解析结果与错误。
		scope, err := ResolveAccountScope(ops, accountSpec, "acc-9")
		if err != nil || scope != "acc-9" {
			t.Fatalf("运营跨账号解析失败：scope=%q err=%v", scope, err)
		}
	})
	t.Run("非法身份被拒", func(t *testing.T) {
		// scope、err 分别是解析结果与错误。
		scope, err := ResolveAccountScope(Principal{}, accountSpec, "")
		if !errors.Is(err, ErrScopeViolation) || scope != "" {
			t.Fatalf("非法身份未被拒绝：scope=%q err=%v", scope, err)
		}
	})
}
