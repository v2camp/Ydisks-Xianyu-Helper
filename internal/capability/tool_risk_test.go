package capability

import "testing"

// TestNewToolRiskTableRejectsInvalidDeclarations 覆盖标注表的构造校验：空名、非法等级与重名都必须报错。
func TestNewToolRiskTableRejectsInvalidDeclarations(t *testing.T) {
	// cases 是逐个断言的非法声明组合。
	cases := []struct {
		// name 是场景名称。
		name string
		// specs 是该场景的声明集合。
		specs []ToolRiskSpec
	}{
		{name: "空工具名", specs: []ToolRiskSpec{{Name: "", Risk: RiskRead}}},
		{name: "非法危险等级", specs: []ToolRiskSpec{{Name: "system_ping", Risk: Risk("low")}}},
		{name: "未填危险等级", specs: []ToolRiskSpec{{Name: "system_ping"}}},
		{
			name: "工具名重复",
			specs: []ToolRiskSpec{
				{Name: "system_ping", Risk: RiskRead},
				{Name: "system_ping", Risk: RiskWrite},
			},
		},
	}
	// tc 是当前断言的场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// table、err 是构造结果与构造错误；非法声明必须报错，不能静默跳过。
			table, err := NewToolRiskTable(tc.specs...)
			if err == nil {
				t.Fatalf("非法声明应构造失败：got=%v", table)
			}
			if table != nil {
				t.Fatalf("构造失败时不应返回表：got=%v", table)
			}
		})
	}
}

// TestToolRiskTableLookupAndNames 覆盖查表、名称排序与 nil 接收者的 fail-closed 行为。
func TestToolRiskTableLookupAndNames(t *testing.T) {
	// table、err 是标注表及其构造错误。
	table, err := NewToolRiskTable(
		ToolRiskSpec{Name: "system_version", Risk: RiskRead},
		ToolRiskSpec{Name: "account_delete", Risk: RiskAccount},
	)
	if err != nil {
		t.Fatalf("构造标注表失败: %v", err)
	}
	if table.Len() != 2 {
		t.Fatalf("表长度不符：got=%d want=2", table.Len())
	}
	// names 是排序后的工具名，必须与字典序一致。
	names := table.Names()
	if len(names) != 2 || names[0] != "account_delete" || names[1] != "system_version" {
		t.Fatalf("工具名排序不符：got=%v", names)
	}
	// risk、ok 是命中结果与命中标志。
	risk, ok := table.Lookup("account_delete")
	if !ok || risk != RiskAccount {
		t.Fatalf("查表结果不符：got=(%q,%v)", risk, ok)
	}
	// _、missing 是未登记工具名的查表结果，必须未命中。
	if _, missing := table.Lookup("chat_send_text"); missing {
		t.Fatal("未登记的工具名不应命中")
	}
	// nilTable 是刻意留空的表；未装配时按「不注册任何工具」处理，而不是按只读放行。
	var nilTable *ToolRiskTable
	// ok 是未装配标注表的查表命中标志，必须为 false。
	if _, ok := nilTable.Lookup("system_ping"); ok {
		t.Fatal("nil 标注表不应命中任何工具")
	}
	if nilTable.Len() != 0 || nilTable.Names() != nil {
		t.Fatal("nil 标注表应表现为空表")
	}
}

// TestPlatformToolRisksGrading 覆盖平台标注表的落位约束。
//
// 这里只断言结构与边界，不断言逐条等级：定级是按业务语义人工判断的结果，
// 逐条值的守卫在 internal/mcp 侧与工具声明一起断言（越界组合在那里才有意义）。
func TestPlatformToolRisksGrading(t *testing.T) {
	// table、err 是平台标注表及其构造错误。
	table, err := PlatformToolRisks()
	if err != nil {
		t.Fatalf("构造平台工具危险等级表失败: %v", err)
	}
	if table.Len() == 0 {
		t.Fatal("平台工具危险等级表不得为空")
	}
	// counts 统计各等级的落位数量。
	counts := map[Risk]int{}
	// name 是表中的当前工具名。
	for _, name := range table.Names() {
		// risk、ok 是查得的等级与命中标志。
		risk, ok := table.Lookup(name)
		if !ok {
			t.Fatalf("表内工具 %q 查不到等级", name)
		}
		counts[risk]++
	}
	// 四个档位都必须有落位：只读档为空说明只读能力没被标注，高危档为空则多半是漏标。
	// quote 档允许为空——本期确实没有「只生成承诺、不改数据」的工具。
	// required 是必须有落位的最低要求集合。
	for _, required := range []Risk{RiskRead, RiskWrite, RiskAccount, RiskDestructive} {
		if counts[required] == 0 {
			t.Fatalf("危险等级 %q 没有任何工具落位，标注可能整体缺失", required)
		}
	}
	// total 是各档计数之和，必须与表长度相等，否则说明有等级未被统计到。
	total := counts[RiskRead] + counts[RiskWrite] + counts[RiskQuote] + counts[RiskAccount] + counts[RiskDestructive]
	if total != table.Len() {
		t.Fatalf("各档计数之和与表长度不一致：got=%d want=%d", total, table.Len())
	}
}
