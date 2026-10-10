// ai_intent_test.go 意图标签收敛与投诉扩展词表解析的表驱动测试。
// 意图判定与接管策略由 ai_scope.go 的边界层负责，因此「命中什么意图」「是否接管」
// 两类断言统一基于 defaultScope.decide，不再重复维护一套注册表测试。

package engine

import "testing"

// TestDefaultScopeDecideBargainTakeover 验证内置边界下砍价放行、其余正向意图只标注不接管。
// 该断言替代原「注册表与历史砍价正则一致」的保护，确保接管范围不回退。
func TestDefaultScopeDecideBargainTakeover(t *testing.T) {
	// cases 覆盖放行、只标注、负向拦截与零命中四类形态。
	cases := []struct {
		// name 是用例名称。
		name string
		// text 是买家消息样例。
		text string
		// wantTakeover 是期望的接管结论。
		wantTakeover bool
		// wantHits 是期望的命中意图名；nil 表示负向短路或未命中。
		wantHits []string
	}{
		{name: "纯砍价放行", text: "60 块能卖吗，能便宜点吗", wantTakeover: true, wantHits: []string{IntentBargain}},
		{name: "发货咨询只标注不接管", text: "付款后什么时候发货，提取码怎么用", wantTakeover: false, wantHits: []string{IntentOrder, IntentConsult}},
		{name: "询价只标注不接管", text: "这套资料怎么卖", wantTakeover: false, wantHits: []string{IntentInquiry}},
		{name: "投诉负向短路", text: "这根本不是正品，我要举报你卖假货", wantTakeover: false, wantHits: nil},
		{name: "砍价叠加投诉仍被拦截", text: "再便宜点，不然我差评举报", wantTakeover: false, wantHits: nil},
		{name: "零命中", text: "你好在吗", wantTakeover: false, wantHits: nil},
	}
	// testCase 是当前遍历到的输入样例。
	for _, testCase := range cases {
		// gotTakeover 与 gotHits 是边界层的实际判定结果。
		gotTakeover, gotHits := defaultScope.decide(testCase.text)
		if gotTakeover != testCase.wantTakeover {
			t.Fatalf("%s: decide(%q) 接管=%v，期望 %v（命中 %v）", testCase.name, testCase.text, gotTakeover, testCase.wantTakeover, gotHits)
		}
		if !equalStringSlice(gotHits, testCase.wantHits) {
			t.Fatalf("%s: decide(%q) 命中=%v，期望 %v", testCase.name, testCase.text, gotHits, testCase.wantHits)
		}
	}
}

// TestDefaultScopeDecideMatchesLegacyBargainGate 验证内置边界的砍价覆盖不窄于历史正则：
// bargainMessageRe 命中的表达，边界也必须放行，保证接管范围不回退。
func TestDefaultScopeDecideMatchesLegacyBargainGate(t *testing.T) {
	// samples 是历史上会交给 AI 的砍价表达样例。
	samples := []string{
		"便宜点", "少点呗", "最低多少", "砍价", "降价", "打折",
		"能不能 50 元", "50 块卖不卖", "可以便宜吗",
	}
	// sample 是当前遍历到的历史样例。
	for _, sample := range samples {
		// takeover 表示样例是否被内置边界放行。
		takeover, hits := defaultScope.decide(sample)
		if !takeover {
			t.Fatalf("历史砍价样例 %q 应被放行，实际命中 %v", sample, hits)
		}
		// found 表示命中集合里是否含砍价意图。
		found := false
		// intent 是当前遍历到的命中意图。
		for _, intent := range hits {
			if intent == IntentBargain {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("历史砍价样例 %q 应命中砍价意图，实际 %v", sample, hits)
		}
	}
}

// TestDefaultScopeDecideComplaintUnion 验证扩展负向词表与内置负向词取并集而非替换：
// 内置说法仍拦截、扩展新增说法也拦截，且不得因扩展而放行砍价。
// 判定链路与 ai.go 的生产写法一致：先 scope.decide，再 intentBlockedByExtra。
func TestDefaultScopeDecideComplaintUnion(t *testing.T) {
	// extra 是运维新增的投诉说法（货不对板）。
	extra := ParseComplaintKeywords("货不对板", nil)
	// blocked 模拟生产判定：内置负向或扩展负向任一命中即不接管。
	blocked := func(text string) bool {
		// takeover 是边界层的接管结论。
		takeover, _ := defaultScope.decide(text)
		return !takeover || intentBlockedByExtra(extra, text)
	}
	// 内置说法仍应拦截（证明内置未被扩展替换）。
	if !blocked("我要退款") {
		t.Fatal("内置负向词表应仍生效，不应接管")
	}
	// 扩展说法也应拦截。
	if !blocked("这东西货不对板") {
		t.Fatal("扩展负向词表应生效，不应接管")
	}
	// 扩展词表不得误伤：含砍价表达且命中扩展投诉时应被拦截，而非当作砍价放行。
	if !blocked("货不对板，再便宜点") {
		t.Fatal("投诉叠加砍价应被拦截，而非当作砍价放行")
	}
	// 反向验证：不含任何投诉表达的正常砍价仍应放行，证明扩展词表没有过度拦截。
	if take, _ := defaultScope.decide("再便宜点"); !take || intentBlockedByExtra(extra, "再便宜点") {
		t.Fatal("正常砍价不应被扩展词表拦截")
	}
}

// TestParseComplaintKeywords 验证扩展词表解析：空集回落、非法正则忽略、正常正则编译、分隔符兼容。
func TestParseComplaintKeywords(t *testing.T) {
	// 空值回落 nil。
	if got := ParseComplaintKeywords("", nil); got != nil {
		t.Fatalf("空词表应返回 nil，实际 %v", got)
	}
	// 仅空白同样回落 nil。
	if got := ParseComplaintKeywords("   ；  ", nil); got != nil {
		t.Fatalf("纯空白词表应返回 nil，实际 %v", got)
	}
	// 非法正则被忽略，合法项保留；同时验证半角与全角分隔符、首尾空白。
	got := ParseComplaintKeywords(" 货不对板 ; [非法( ; 维权补 , 包退 ", nil)
	// want 是期望保留的合法正则数量（货不对板、维权补、包退 三项，[非法( 被忽略）。
	want := 3
	if len(got) != want {
		t.Fatalf("非法正则应被忽略，保留 %d 项，实际 %d 项: %v", want, len(got), got)
	}
	// re 表示当前遍历到的已编译正则，必须非 nil。
	for _, re := range got {
		if re == nil {
			t.Fatal("不应出现 nil 正则")
		}
	}
	// 两个分隔符之间夹纯空白段（空格非分隔符）会得到空项，应被跳过不计入结果。
	if empty := ParseComplaintKeywords("a; ;b", nil); len(empty) != 2 {
		t.Fatalf("分隔符间纯空白段应跳过，期望 2 项，实际 %d 项: %v", len(empty), empty)
	}
}

// TestPrimaryIntentLabel 验证命中集合到历史标签的收敛语义：零命中/单命中/多命中的取值。
func TestPrimaryIntentLabel(t *testing.T) {
	// 零命中归并到 chitchat（旧值 "chat" 的更名）。
	if got := primaryIntentLabel(nil); got != IntentChitchat {
		t.Fatalf("零命中应为 chitchat，实际 %q", got)
	}
	// 单一命中直接写该意图。
	if got := primaryIntentLabel([]string{IntentBargain}); got != IntentBargain {
		t.Fatalf("单一命中应写该意图，实际 %q", got)
	}
	// 多命中归并到 ambiguous。
	if got := primaryIntentLabel([]string{IntentBargain, IntentOrder}); got != IntentAmbiguous {
		t.Fatalf("多命中应为 ambiguous，实际 %q", got)
	}
}

// equalStringSlice 比较两个字符串切片是否等长且逐项相等；nil 与空切片视为相等。
func equalStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	// index 是当前比较的下标。
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
