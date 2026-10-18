// ai_item_facts_test.go 锁定商品事实整理行为：平台 JSON 不进提示词，标题规格可直答。
package engine

import (
	"strings"
	"testing"
)

// TestSanitizeItemDescription 验证描述清洗：人工文案保留、平台 JSON 降级为中性描述。
func TestSanitizeItemDescription(t *testing.T) {
	// platformJSON 是生产 item_detail 的典型形态：图片、标签、埋点参数。
	const platformJSON = `{"auction_type":"b","detail_params":{"imageInfos":"[{\"url\":\"http://img.alicdn.com/x.jpg\"}]","postInfo":"包邮","title":"某商品"},"item_label_data":{"labelBucketId":3}}`
	// cases 覆盖人工文案、空描述、纯 JSON 详情与普通详情文本四种输入。
	cases := []struct {
		// name 是用例名称。
		name string
		// description 是人工填写的描述。
		description string
		// detail 是平台详情原文。
		detail string
		// want 是期望结果。
		want string
	}{
		{name: "人工描述原样保留", description: "拍下自动发货", detail: platformJSON, want: "拍下自动发货"},
		{name: "空描述且详情为平台JSON", description: "", detail: platformJSON, want: defaultItemDescription},
		{name: "空描述且详情为空", description: "", detail: "", want: defaultItemDescription},
		{name: "空描述且详情是普通文本", description: "", detail: "1-4季全集，网盘发货", want: "1-4季全集，网盘发货"},
	}
	// tc 是当前遍历到的用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是清洗后的描述。
			if got := sanitizeItemDescription(tc.description, tc.detail); got != tc.want {
				t.Fatalf("描述清洗不符 got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestSanitizeItemDescriptionNeverLeaksPlatformJSON 平台 JSON 的任一形态都不得进入提示词。
func TestSanitizeItemDescriptionNeverLeaksPlatformJSON(t *testing.T) {
	// inputs 是平台结构化数据的两种开头形态。
	inputs := []string{`{"a":1}`, `[{"b":2}]`, "  \n{\"c\":3}"}
	// input 是当前遍历到的平台详情原文。
	for _, input := range inputs {
		// got 是清洗后的描述。
		got := sanitizeItemDescription("", input)
		if strings.Contains(got, "imageInfos") || strings.Contains(got, `{"`) {
			t.Fatalf("平台 JSON 泄漏进描述: %q", got)
		}
		if got != defaultItemDescription {
			t.Fatalf("平台 JSON 应降级为中性描述 got=%q", got)
		}
	}
}

// TestExtractItemSpecFacts 验证从标题抽取规格事实。
func TestExtractItemSpecFacts(t *testing.T) {
	// cases 覆盖区间、全量、N集全、无规格与边界输入。
	cases := []struct {
		// title 是商品标题。
		title string
		// want 是期望抽出的规格事实。
		want []string
	}{
		{title: "糯糯下山师兄们都慌了 1-4 季全集｜热门短句", want: []string{"1-4 季（共 4 季）"}},
		{title: "Campbell Busy系列英文推拉机关绘本，全 8 册", want: []string{"全 8 册"}},
		{title: "余红旧事 47集全 4K高清", want: []string{"47 集"}},
		{title: "30到34集 打包", want: []string{"30-34 集（共 5 集）"}},
		{title: "普通商品没有规格", want: nil},
		{title: "", want: nil},
		{title: "反序区间 9-3 季不成立", want: nil},
	}
	// tc 是当前遍历到的用例。
	for _, tc := range cases {
		// got 是抽取结果。
		got := extractItemSpecFacts(tc.title)
		if len(got) != len(tc.want) {
			t.Fatalf("规格事实数量不符 title=%q got=%v want=%v", tc.title, got, tc.want)
		}
		// i 是当前比较的下标。
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("规格事实不符 title=%q got=%v want=%v", tc.title, got, tc.want)
			}
		}
	}
}

// TestExtractItemSpecFactsDedupeAndCap 重复规格只保留一条，且最多输出 3 条。
func TestExtractItemSpecFactsDedupeAndCap(t *testing.T) {
	// got 是含重复与超量规格的标题抽取结果。
	got := extractItemSpecFacts("1-4季 1-4季 全8册 47集全 12期 5本")
	if len(got) > 3 {
		t.Fatalf("规格事实应最多 3 条，got=%v", got)
	}
	// seen 记录已出现的规格，用于断言无重复。
	seen := map[string]struct{}{}
	// fact 是当前遍历到的规格事实。
	for _, fact := range got {
		// ok 表示该规格是否已出现过。
		if _, ok := seen[fact]; ok {
			t.Fatalf("规格事实重复: %v", got)
		}
		seen[fact] = struct{}{}
	}
}

// TestExtractTradePostInfo 只承认平台明确写出的运费标注。
func TestExtractTradePostInfo(t *testing.T) {
	// cases 覆盖三种合法取值、缺失字段与非 JSON 输入。
	cases := []struct {
		// name 是用例名称。
		name string
		// detail 是平台详情原文。
		detail string
		// want 是期望的标注文本。
		want string
	}{
		{name: "包邮", detail: `{"detail_params":{"postInfo":"包邮"}}`, want: "包邮"},
		{name: "仅自提", detail: `{"detail_params":{"postInfo":"仅自提"}}`, want: "仅自提"},
		{name: "不包邮", detail: `{"detail_params":{"postInfo":"不包邮"}}`, want: "不包邮"},
		{name: "无该字段", detail: `{"detail_params":{"title":"x"}}`, want: ""},
		{name: "非JSON", detail: "包邮哦", want: ""},
		{name: "未知取值", detail: `{"detail_params":{"postInfo":"按距离计费"}}`, want: ""},
	}
	// tc 是当前遍历到的用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是识别出的平台运费标注。
			if got := extractTradePostInfo(tc.detail); got != tc.want {
				t.Fatalf("运费标注不符 got=%q want=%q", got, tc.want)
			}
		})
	}
}
