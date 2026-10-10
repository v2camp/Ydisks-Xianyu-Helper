// netpan_test.go 网盘渠道识别的表驱动测试，覆盖域名判定、关键词兜底、双盘与编码解码。

package netpan

import (
	"reflect"
	"testing"
)

// TestDetectByDomain 验证分享域名命中即确定渠道，优先于任何关键词。
func TestDetectByDomain(t *testing.T) {
	// cases 覆盖各渠道的域名形态与大小写变化。
	cases := []struct {
		// name 是用例名称。
		name string
		// text 是待判定的正文。
		text string
		// want 是期望的渠道列表。
		want []string
	}{
		{name: "百度域名", text: "链接: https://pan.baidu.com/s/1abc 提取码: abcd", want: []string{ChannelBaidu}},
		{name: "夸克域名", text: "链接：https://pan.quark.cn/s/85f8", want: []string{ChannelQuark}},
		{name: "迅雷域名", text: "https://pan.xunlei.com/s/xxxx", want: []string{ChannelXunlei}},
		{name: "阿里域名", text: "https://www.alipan.com/s/xxx", want: []string{ChannelAliyun}},
		{name: "天翼域名", text: "https://cloud.189.cn/t/xxx", want: []string{ChannelTianyi}},
		{name: "115域名", text: "https://115.com/s/xxx", want: []string{Channel115}},
		{name: "UC域名", text: "https://drive.uc.cn/s/xxx", want: []string{ChannelUC}},
		{name: "域名大小写", text: "HTTPS://PAN.BAIDU.COM/s/1abc", want: []string{ChannelBaidu}},
		{name: "双盘百度加夸克", text: "百度链接: https://pan.baidu.com/s/1a 夸克链接：https://pan.quark.cn/s/85", want: []string{ChannelBaidu, ChannelQuark}},
	}
	// testCase 是当前遍历到的输入样例。
	for _, testCase := range cases {
		// got 是实际识别结果。
		got := Detect(testCase.text)
		if !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("%s: Detect() = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// TestDetectKeywordFallback 验证没有链接时的关键词兜底，以及无线索时返回空。
func TestDetectKeywordFallback(t *testing.T) {
	// 关键词兜底：只有文字说明没有链接。
	if got := Detect("--来自百度网盘超级会员v3的分享"); !reflect.DeepEqual(got, []string{ChannelBaidu}) {
		t.Fatalf("百度关键词兜底失败: %v", got)
	}
	// 中文数字与口语表达同样命中。
	if got := Detect("我用夸克网盘给你分享了「测试」"); !reflect.DeepEqual(got, []string{ChannelQuark}) {
		t.Fatalf("夸克关键词兜底失败: %v", got)
	}
	// 无任何渠道线索时返回空，调用方按无线索处理。
	if got := Detect("你好，这个还有货吗"); got != nil {
		t.Fatalf("无渠道线索应返回 nil，实际 %v", got)
	}
	// 空正文与全空白同样返回空。
	if got := Detect("", "   "); got != nil {
		t.Fatalf("空正文应返回 nil，实际 %v", got)
	}
}

// TestDetectMultiSegment 验证多段正文拼接判定：卡密正文与发货文本分开传入仍可识别双盘。
func TestDetectMultiSegment(t *testing.T) {
	// got 是拼接两段正文后的识别结果：第一段只有百度，第二段只有夸克。
	got := Detect("通过百度网盘分享的文件：测试\n链接: https://pan.baidu.com/s/1a", "夸克链接：https://pan.quark.cn/s/85f8")
	if !reflect.DeepEqual(got, []string{ChannelBaidu, ChannelQuark}) {
		t.Fatalf("多段正文应识别为双盘，实际 %v", got)
	}
}

// TestEncodeDecode 验证渠道列表与落库值的双向转换，含空值与空白段清理。
func TestEncodeDecode(t *testing.T) {
	// 空列表编码为空串。
	if got := Encode(nil); got != "" {
		t.Fatalf("空列表应编码为空串，实际 %q", got)
	}
	// 双盘编码为逗号连接。
	if got := Encode([]string{ChannelBaidu, ChannelQuark}); got != "百度网盘,夸克网盘" {
		t.Fatalf("双盘编码错误: %q", got)
	}
	// 空值解码为空切片。
	if got := Decode(""); got != nil {
		t.Fatalf("空值应解码为 nil，实际 %v", got)
	}
	// 解码还原渠道列表。
	if got := Decode("百度网盘,夸克网盘"); !reflect.DeepEqual(got, []string{ChannelBaidu, ChannelQuark}) {
		t.Fatalf("解码还原失败: %v", got)
	}
	// 空白段被跳过，首尾空白被清理。
	if got := Decode(" 百度网盘 , ,夸克网盘 "); !reflect.DeepEqual(got, []string{ChannelBaidu, ChannelQuark}) {
		t.Fatalf("空白段应被跳过: %v", got)
	}
	// 仅含分隔符时返回空。
	if got := Decode(" , "); got != nil {
		t.Fatalf("仅分隔符应返回 nil，实际 %v", got)
	}
}
