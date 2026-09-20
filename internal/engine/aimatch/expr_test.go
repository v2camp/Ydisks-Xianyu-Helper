// aimatch 匹配器的聚焦测试：覆盖布尔优先级、相邻词「且」、取反、括号嵌套、
// 中文/正则词、空表达式与各类语法错误，保证低代码配置的判定行为可预期。
package aimatch

import "testing"

// match 是测试助手：编译表达式并断言对文本的判定结果。
func match(t *testing.T, expr, text string) bool {
	t.Helper()
	// fn 是编译产物；err 表示语法错误，这里期望编译成功。
	fn, err := Compile(expr)
	if err != nil {
		t.Fatalf("Compile(%q) 失败: %v", expr, err)
	}
	return fn(text)
}

// TestCompileOrAndPrecedence 验证或/且优先级：且优先于或。
func TestCompileOrAndPrecedence(t *testing.T) {
	// cases 是表达式与两组期望值：前为命中文本，后为不命中文本。
	cases := []struct {
		expr string
		hit  string
		miss string
	}{
		{"便宜 | 优惠", "能便宜点吗", "在吗"},
		{"便宜 | 优惠", "有什么优惠", "包邮吗"},
		{"砍价 & 元", "能砍价吗，5元行不行", "能砍价吗"},
		{"砍价 | 优惠 & 元", "这单优惠5元吗", "这单优惠吗"},
		{"砍价 | 优惠 & 元", "能砍价吗", "包邮吗"},
	}
	// c 表示当前遍历到的测试用例。
	for _, c := range cases {
		// got 是命中文本的判定结果，期望为真。
		if got := match(t, c.expr, c.hit); !got {
			t.Errorf("(%s) 对 %q 应为命中", c.expr, c.hit)
		}
		// got 是不命中文本的判定结果，期望为假。
		if got := match(t, c.expr, c.miss); got {
			t.Errorf("(%s) 对 %q 应不命中", c.expr, c.miss)
		}
	}
}

// TestCompileAdjacentWordsAsAnd 验证相邻词默认按「且」处理。
func TestCompileAdjacentWordsAsAnd(t *testing.T) {
	// 相邻无运算符的两个词等价于同时出现。
	if !match(t, "百度网盘 发货", "是百度网盘发货吗") {
		t.Error("相邻词应命中同时出现的文本")
	}
	if match(t, "百度网盘 发货", "夸克发货吗") {
		t.Error("相邻词不应命中只出现一个词的文本")
	}
}

// TestCompileNot 验证取反语义与优先级。
func TestCompileNot(t *testing.T) {
	// ! 作用于紧随其后的词或分组。
	if !match(t, "夸克 & !会员", "用夸克发") {
		t.Error("夸克且非会员应命中")
	}
	if match(t, "夸克 & !会员", "夸克会员能下载吗") {
		t.Error("夸克且非会员不应命中含会员的文本")
	}
	if match(t, "!(退款 | 退货)", "我想退款") {
		t.Error("取反分组不应命中退款文本")
	}
	if !match(t, "!(退款 | 退货)", "什么时候发货") {
		t.Error("取反分组应命中普通文本")
	}
}

// TestCompileNestedParens 验证括号嵌套与复合表达式。
func TestCompileNestedParens(t *testing.T) {
	// expr 模拟真实客服里「砍价或优惠，且提到元/块」的组合判定。
	expr := "(便宜 | 优惠 | 少点 | 砍价) & (元 | 块)"
	if !match(t, expr, "便宜5块行不") {
		t.Error("复合表达式应命中砍价+金额")
	}
	if match(t, expr, "便宜点吧") {
		t.Error("复合表达式不应命中无金额的纯砍价")
	}
}

// TestCompileRegexWord 验证双引号包裹的正则词可用（便于匹配变动数字）。
func TestCompileRegexWord(t *testing.T) {
	// 正则词必须用双引号包裹，内部 ( | 不会被当作语法符号。
	if !match(t, `"第\d季"`, "有第3季了吗") {
		t.Error("正则词应命中阿拉伯数字季数")
	}
	if match(t, `"第\d季"`, "第三季") {
		t.Error("\\d 不应命中中文数字季数")
	}
	// 中文数字与阿拉伯数字混排可用字符类表达。
	if !match(t, `"第[一二三\d]季"`, "有第三季了吗") || !match(t, `"第[一二三\d]季"`, "有第2季了吗") {
		t.Error("字符类正则词应同时命中中文与阿拉伯数字季数")
	}
	// 未加引号的含括号文本按字面包含匹配，不触发语法。
	if !match(t, "第\\d季", "(第\\d季说明这个括号是字面)") {
		t.Error("普通词的括号应按字面包含匹配")
	}
}

// TestCompileCaseInsensitive 验证英文词忽略大小写。
func TestCompileCaseInsensitive(t *testing.T) {
	// pdf/PDF 视为同一词。
	if !match(t, "pdf", "这是PDF版本吗") {
		t.Error("英文词应忽略大小写")
	}
}

// TestCompileEmptyExpression 验证空表达式永不命中。
func TestCompileEmptyExpression(t *testing.T) {
	// 空白表达式返回恒假函数，编译不报错。
	fn, err := Compile("   ")
	if err != nil {
		t.Fatalf("空表达式应编译成功: %v", err)
	}
	if fn("随便什么文本") {
		t.Error("空表达式应永不命中")
	}
}

// TestCompileSyntaxErrors 验证各类语法错误返回错误而不是静默出错。
func TestCompileSyntaxErrors(t *testing.T) {
	// badExprs 是期望编译失败的不合法表达式。
	badExprs := []string{
		"(",        // 只有左括号
		"(便宜 | 优惠", // 缺右括号
		"|",        // 空分支
		"便宜 |",     // 尾部运算符
		"! ",       // 取反缺操作数
		"(a",       // 分组缺右括号
		`"[a-z"`,   // 引号正则非法
		`"便宜`,      // 未闭合双引号
	}
	// expr 表示当前遍历到的不合法表达式。
	for _, expr := range badExprs {
		if // err 是编译返回的错误，期望非空。
		_, err := Compile(expr); err == nil {
			t.Errorf("表达式 %q 应编译失败", expr)
		}
	}
}

// TestCompileFullWidthCommaAndWhitespace 验证全角空格与空白容忍。
func TestCompileFullWidthCommaAndWhitespace(t *testing.T) {
	// 全角空格和普通空格都作为词间分隔。
	if !match(t, "资源码　鸿蒙", "鸿蒙系统没有资源码按钮") {
		t.Error("全角空格分隔的相邻词应按且命中")
	}
}
