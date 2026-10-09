package automation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// hasContentHit 判断命中列表里是否包含指定规则且说明包含关键词的记录。
func hasContentHit(hits []ContentHit, rule, detailSubstr string) bool {
	// hit 是当前命中记录。
	for _, hit := range hits {
		if hit.Rule == rule && strings.Contains(hit.Detail, detailSubstr) {
			return true
		}
	}
	return false
}

// roundTripFunc 把函数适配成 RoundTripper，供链接探活测试注入稳定失败的传输层。
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 执行注入的传输逻辑。
func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// useStubLinkClient 在本测试内替换链接探活客户端工厂，并在测试结束时恢复。
func useStubLinkClient(t *testing.T, client *http.Client) {
	t.Helper()
	// original 保存测试前的客户端工厂，测试结束必须恢复以免污染其他用例。
	original := newDeliveryLinkHTTPClient
	t.Cleanup(func() { newDeliveryLinkHTTPClient = original })
	newDeliveryLinkHTTPClient = func(time.Duration) *http.Client { return client }
}

// TestScanDeliveryContentContactPatterns 逐类验证联系方式与站外引流词命中。
func TestScanDeliveryContentContactPatterns(t *testing.T) {
	// cases 覆盖任务清单里的每一条联系方式/引流词形态。
	cases := []struct {
		// name 是当前用例名称。
		name string
		// text 是待扫描文本。
		text string
		// want 是期望命中的词条标签。
		want string
	}{
		{name: "微信", text: "加我微信详聊", want: "微信"},
		{name: "加微", text: "加微发你", want: "加微"},
		{name: "加V", text: "加V领取", want: "加v"},
		{name: "维信", text: "维信同号", want: "维信"},
		{name: "薇信", text: "薇信联系", want: "薇信"},
		{name: "vx小写", text: "请加vx", want: "vx"},
		{name: "VX大写", text: "请加VX", want: "vx"},
		{name: "qq小写", text: "qq私聊", want: "qq"},
		{name: "QQ大写", text: "QQ群", want: "qq"},
		{name: "二维码", text: "扫下面的二维码", want: "二维码"},
		{name: "扫码", text: "扫码进群", want: "扫码"},
		{name: "站外", text: "站外优惠", want: "站外"},
		{name: "线下交易", text: "支持线下交易", want: "线下交易"},
		{name: "加我", text: "加我就行", want: "加我"},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// hits 是待扫描文本的命中结果。
		hits := ScanDeliveryContent(tc.text)
		if !hasContentHit(hits, "联系方式/引流", tc.want) {
			t.Fatalf("%s: 未命中 %q，hits=%+v", tc.name, tc.want, hits)
		}
	}
}

// TestScanDeliveryContentLatinContactWordBoundary 验证纯拉丁联系方式短词按词边界命中。
// 回归来源：生产卡密里的百度网盘分享码 G4TQQ 含 qq，曾被无边界子串匹配误判为「联系方式/引流」并转人工。
func TestScanDeliveryContentLatinContactWordBoundary(t *testing.T) {
	// netdiskText 是生产卡密原文形态的百度网盘分享内容，分享码内嵌 qq 属随机串而非联系方式。
	netdiskText := "通过网盘分享的文件：ai 写量化策略教程\n链接: https://pan.baidu.com/s/15pGOXRnr8ziFw_IIaG4TQQ?pwd=rs7a 提取码: rs7a\n--来自百度网盘超级会员v3的分享"
	// netdiskHits 是网盘分享内容的扫描命中结果；分享码内嵌 qq 属随机串，不应命中任何门禁规则。
	netdiskHits := ScanDeliveryContent(netdiskText)
	if len(netdiskHits) != 0 {
		t.Fatalf("网盘分享码内嵌 qq 不应命中任何门禁规则: hits=%+v", netdiskHits)
	}
	// adjacentHits 是词前紧邻字母形态的扫描命中结果，属同一类误杀，必须放行。
	adjacentHits := ScanDeliveryContent("下载地址 abcqq.zip")
	if hasContentHit(adjacentHits, "联系方式/引流", "qq") {
		t.Fatalf("字母紧邻的 qq 不应命中联系方式引流: hits=%+v", adjacentHits)
	}
	// positives 是真实引流形态；词边界判定不得把它们一起放过。
	positives := []string{"加qq", "qq123456", "wx:qq", "QQ群", "请加vx", "请加VX"}
	// text 是当前待验证的真实引流文案。
	for _, text := range positives {
		// positiveHits 是当前真实引流文案的扫描命中结果。
		positiveHits := ScanDeliveryContent(text)
		if !hasContentHit(positiveHits, "联系方式/引流", "") {
			t.Fatalf("%q 应仍命中联系方式引流: hits=%+v", text, positiveHits)
		}
	}
	// residualHits 是已知残余误杀形态的扫描命中结果；分享码恰以 qq 起止且相邻字符非字母时仍会命中。
	residualHits := ScanDeliveryContent("链接: https://pan.baidu.com/s/1abcd?pwd=qq88")
	if !hasContentHit(residualHits, "联系方式/引流", "qq") {
		t.Fatalf("已知残余误杀形态发生变化，需重新评估词边界策略: hits=%+v", residualHits)
	}
}

// TestScanDeliveryContentMobileNumber 验证恰好 11 位连续数字命中手机号，更长数字串不误杀。
func TestScanDeliveryContentMobileNumber(t *testing.T) {
	// hits 是11 位手机号文本的命中结果。
	hits := ScanDeliveryContent("联系方式 13812345678")
	if !hasContentHit(hits, "联系方式/引流", "手机号") {
		t.Fatalf("11 位手机号未命中: %+v", hits)
	}
	// longHits 是 12 位订单号文本的命中结果，不应被判为手机号。
	longHits := ScanDeliveryContent("订单号 123456789012 已发货")
	if hasContentHit(longHits, "联系方式/引流", "手机号") {
		t.Fatalf("12 位数字串被误判为手机号: %+v", longHits)
	}
	// shortHits 是 10 位数字文本的命中结果。
	shortHits := ScanDeliveryContent("编号 1234567890")
	if hasContentHit(shortHits, "联系方式/引流", "手机号") {
		t.Fatalf("10 位数字串被误判为手机号: %+v", shortHits)
	}
	// edgeHits 是数字段恰好贴着文本边界的 11 位形态命中结果。
	edgeHits := ScanDeliveryContent("13812345678")
	if !hasContentHit(edgeHits, "联系方式/引流", "手机号") {
		t.Fatalf("边界 11 位手机号未命中: %+v", edgeHits)
	}
}

// TestScanDeliveryContentURLRules 验证非网盘外链拦截而网盘交付链接放行。
func TestScanDeliveryContentURLRules(t *testing.T) {
	// cases 覆盖非网盘 http(s)/www 形态与常见网盘域放行。
	cases := []struct {
		// name 是当前用例名称。
		name string
		// text 是待扫描文本。
		text string
		// wantHit 表示期望是否命中非网盘外链规则。
		wantHit bool
	}{
		{name: "非网盘https", text: "资料在 https://example.com/share", wantHit: true},
		{name: "非网盘http", text: "见 http://evil.example.com/a", wantHit: true},
		{name: "非网盘www形态", text: "备用 www.example.com/s/1", wantHit: true},
		{name: "百度网盘", text: "链接 https://pan.baidu.com/s/1abc?pwd=1234", wantHit: false},
		{name: "百度云", text: "链接 https://yun.baidu.com/s/1abc", wantHit: false},
		{name: "夸克网盘", text: "链接 https://pan.quark.cn/s/abc", wantHit: false},
		{name: "夸克drive", text: "链接 https://drive.quark.cn/s/abc", wantHit: false},
		{name: "迅雷网盘", text: "链接 https://pan.xunlei.com/s/abc", wantHit: false},
		{name: "天翼云盘", text: "链接 https://cloud.189.cn/s/abc", wantHit: false},
		{name: "阿里云盘", text: "链接 https://www.alipan.com/s/abc", wantHit: false},
		{name: "无链接", text: "卡密如下：AAAA-BBBB", wantHit: false},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// hits 是待扫描文本的命中结果。
		hits := ScanDeliveryContent(tc.text)
		// gotHit 表示本次是否命中非网盘外链规则。
		gotHit := hasContentHit(hits, "非网盘外链", "非网盘域名")
		if gotHit != tc.wantHit {
			t.Fatalf("%s: 命中=%v 期望=%v hits=%+v", tc.name, gotHit, tc.wantHit, hits)
		}
	}
}

// TestScanDeliveryContentBuiltinWords 验证内置违禁词短表逐词命中。
func TestScanDeliveryContentBuiltinWords(t *testing.T) {
	// word 是内置违禁词短表中的词条。
	for _, word := range deliveryBuiltinBlockWords {
		// hits 是包含当前词条文本的命中结果。
		hits := ScanDeliveryContent("内容：" + word + "相关")
		if !hasContentHit(hits, "内置违禁词", word) {
			t.Fatalf("内置违禁词 %q 未命中: %+v", word, hits)
		}
	}
	// cleanHits 是干净文本的命中结果，应为空。
	cleanHits := ScanDeliveryContent("谢谢惠顾，欢迎再来")
	if len(cleanHits) != 0 {
		t.Fatalf("干净文本被误判: %+v", cleanHits)
	}
	// emptyHits 是空白文本的命中结果，应为空。
	emptyHits := ScanDeliveryContent("   ")
	if emptyHits != nil {
		t.Fatalf("空白文本应放行: %+v", emptyHits)
	}
}

// TestScanDeliveryContentWithConfig 验证额外违禁词追加命中与门禁关闭短路。
func TestScanDeliveryContentWithConfig(t *testing.T) {
	// defaultCfg 是默认启用的门禁配置。
	defaultCfg := DeliveryGuardConfig{Enabled: true, LinkCheck: true}
	// commaHits 是逗号分隔额外词的命中结果。
	commaHits := ScanDeliveryContentWithConfig("这段包含内部码内容", DeliveryGuardConfig{Enabled: true, ExtraBlockWords: "foo,内部码"})
	if !hasContentHit(commaHits, "额外违禁词", "内部码") {
		t.Fatalf("逗号分隔额外词未命中: %+v", commaHits)
	}
	// pipeHits 是竖线分隔额外词的命中结果。
	pipeHits := ScanDeliveryContentWithConfig("仅禁发词", DeliveryGuardConfig{Enabled: true, ExtraBlockWords: "禁发|其它"})
	if !hasContentHit(pipeHits, "额外违禁词", "禁发") {
		t.Fatalf("竖线分隔额外词未命中: %+v", pipeHits)
	}
	// emptyExtraHits 是额外词为空时的结果，只应包含内置命中。
	emptyExtraHits := ScanDeliveryContentWithConfig("加微信", defaultCfg)
	if len(emptyExtraHits) == 0 || hasContentHit(emptyExtraHits, "额外违禁词", "") {
		t.Fatalf("空额外词不应产生额外命中: %+v", emptyExtraHits)
	}
	// disabledHits 是门禁关闭时的结果，即使文本违规也必须放行。
	disabledHits := ScanDeliveryContentWithConfig("加微信 https://example.com 赌博", DeliveryGuardConfig{Enabled: false})
	if disabledHits != nil {
		t.Fatalf("门禁关闭仍被拦截: %+v", disabledHits)
	}
	// blankWordHits 验证分隔符之间的空白词条被忽略且不误命中。
	blankWordHits := ScanDeliveryContentWithConfig("普通文本", DeliveryGuardConfig{Enabled: true, ExtraBlockWords: " , | ,"})
	if len(blankWordHits) != 0 {
		t.Fatalf("空白词条被误判: %+v", blankWordHits)
	}
}

// TestParseDeliveryGuardConfig 验证门禁配置键缺失、非法 JSON 与显式开关的解析语义。
func TestParseDeliveryGuardConfig(t *testing.T) {
	// emptyCfg 是键缺失（空串）时的默认配置。
	emptyCfg := ParseDeliveryGuardConfig("")
	if !emptyCfg.Enabled || !emptyCfg.LinkCheck || emptyCfg.ExtraBlockWords != "" {
		t.Fatalf("空配置应按默认启用: %+v", emptyCfg)
	}
	// invalidCfg 是非法 JSON 时的默认配置。
	invalidCfg := ParseDeliveryGuardConfig("{not json")
	if !invalidCfg.Enabled || !invalidCfg.LinkCheck {
		t.Fatalf("非法 JSON 应按默认启用: %+v", invalidCfg)
	}
	// wrongTypeCfg 是字段类型非法时的默认配置。
	wrongTypeCfg := ParseDeliveryGuardConfig(`{"enabled":"yes"}`)
	if !wrongTypeCfg.Enabled {
		t.Fatalf("非法字段类型应按默认启用: %+v", wrongTypeCfg)
	}
	// offCfg 是显式关闭门禁的配置。
	offCfg := ParseDeliveryGuardConfig(`{"enabled":false,"link_check":false,"extra_block_words":"a|b"}`)
	if offCfg.Enabled || offCfg.LinkCheck || offCfg.ExtraBlockWords != "a|b" {
		t.Fatalf("显式关闭未生效: %+v", offCfg)
	}
	// partialCfg 是只写额外词的局部配置，未写开关必须保持默认启用。
	partialCfg := ParseDeliveryGuardConfig(`{"extra_block_words":"x"}`)
	if !partialCfg.Enabled || !partialCfg.LinkCheck || partialCfg.ExtraBlockWords != "x" {
		t.Fatalf("局部配置默认值错误: %+v", partialCfg)
	}
	// onlyLinkOff 是只关闭链接检查的配置。
	onlyLinkOff := ParseDeliveryGuardConfig(`{"link_check":false}`)
	if !onlyLinkOff.Enabled || onlyLinkOff.LinkCheck {
		t.Fatalf("只关链接检查未生效: %+v", onlyLinkOff)
	}
}

// TestURLHostAndNetdiskHost 验证主机名提取与网盘域放行判定。
func TestURLHostAndNetdiskHost(t *testing.T) {
	// host 是标准 https 地址的主机名提取结果。
	host := urlHost("https://Pan.Baidu.com/s/1?pwd=2")
	if host != "pan.baidu.com" {
		t.Fatalf("主机名提取错误: %q", host)
	}
	// wwwHost 是 www. 形态补全协议后的主机名。
	wwwHost := urlHost("www.example.com/path")
	if wwwHost != "www.example.com" {
		t.Fatalf("www 形态提取错误: %q", wwwHost)
	}
	// brokenHost 是无法解析地址的主机名提取结果，应为空串。
	brokenHost := urlHost("http://[::1")
	if brokenHost != "" {
		t.Fatalf("非法地址应返回空主机名: %q", brokenHost)
	}
	// cases 覆盖网盘前缀子域与普通域判定。
	cases := []struct {
		// name 是当前用例名称。
		name string
		// host 是待判定主机名。
		host string
		// want 表示期望是否放行。
		want bool
	}{
		{name: "空前缀", host: "", want: false},
		{name: "pan前缀", host: "pan.baidu.com", want: true},
		{name: "yun前缀", host: "yun.baidu.com", want: true},
		{name: "drive前缀", host: "drive.quark.cn", want: true},
		{name: "精确白名单", host: "cloud.189.cn", want: true},
		{name: "普通域", host: "example.com", want: false},
		{name: "伪装前缀", host: "notpan.example.com", want: false},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// got 表示当前主机名的实际放行结果。
		got := isNetdiskHost(tc.host)
		if got != tc.want {
			t.Fatalf("%s: isNetdiskHost(%q)=%v 期望 %v", tc.name, tc.host, got, tc.want)
		}
	}
}

// TestCheckDeliveryLinksHealthJudgments 验证 200/404/410/403/3xx/超时/网络错误的健康判定。
func TestCheckDeliveryLinksHealthJudgments(t *testing.T) {
	// okServer 返回 200，表示链接可达。
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okServer.Close()
	// deadServer 返回 404，表示链接已失效。
	deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer deadServer.Close()
	// goneServer 返回 410，表示链接已永久失效。
	goneServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer goneServer.Close()
	// suspectServer 返回 403，只告警不拦截。
	suspectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer suspectServer.Close()
	// redirectServer 返回无 Location 的 302，验证 3xx 按可达处理。
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer redirectServer.Close()
	// 本组用例统一注入不跟随重定向的客户端，保证 3xx 原样可见。
	useStubLinkClient(t, &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}})
	// cases 覆盖各状态码判定档位。
	cases := []struct {
		// name 是当前用例名称。
		name string
		// raw 是待探活地址。
		raw string
		// want 是期望的健康档位。
		want LinkHealth
	}{
		{name: "200可达", raw: okServer.URL + "/s/1", want: LinkHealthOK},
		{name: "404失效", raw: deadServer.URL + "/s/1", want: LinkHealthDead},
		{name: "410失效", raw: goneServer.URL + "/s/1", want: LinkHealthDead},
		{name: "403存疑", raw: suspectServer.URL + "/s/1", want: LinkHealthSuspect},
		{name: "302可达", raw: redirectServer.URL + "/s/1", want: LinkHealthOK},
	}
	// tc 表示当前遍历过程中的用例。
	for _, tc := range cases {
		// hits 是当前地址的探活结果。
		hits := CheckDeliveryLinks(context.Background(), "见 "+tc.raw)
		if len(hits) != 1 || hits[0].Health != tc.want {
			t.Fatalf("%s: hits=%+v 期望 %s", tc.name, hits, tc.want)
		}
	}
}

// TestCheckDeliveryLinksNetworkFailures 验证超时、网络错误与 HEAD 降级 GET 的判定。
func TestCheckDeliveryLinksNetworkFailures(t *testing.T) {
	// slowServer 故意拖延响应，用于触发探活超时。
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()
	// originalTimeout 保存默认探活超时，测试结束必须恢复。
	originalTimeout := deliveryLinkCheckTimeout
	t.Cleanup(func() { deliveryLinkCheckTimeout = originalTimeout })
	deliveryLinkCheckTimeout = 20 * time.Millisecond
	// timeoutClient 使用与探活超时一致的短超时客户端，保证超时分支可确定触发。
	useStubLinkClient(t, &http.Client{Timeout: deliveryLinkCheckTimeout})
	// timeoutHits 是超时地址的探活结果，应判定为不可达。
	timeoutHits := CheckDeliveryLinks(context.Background(), "见 "+slowServer.URL+"/s/1")
	if len(timeoutHits) != 1 || timeoutHits[0].Health != LinkHealthDead {
		t.Fatalf("超时应判定为不可达: %+v", timeoutHits)
	}
	// 断网场景注入必定失败的传输层，避免依赖真实网络。
	useStubLinkClient(t, &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})})
	// brokenHits 是网络错误地址的探活结果，应判定为不可达。
	brokenHits := CheckDeliveryLinks(context.Background(), "见 https://pan.baidu.com/s/1")
	if len(brokenHits) != 1 || brokenHits[0].Health != LinkHealthDead {
		t.Fatalf("网络错误应判定为不可达: %+v", brokenHits)
	}
	// headFailServer 对 HEAD 直接断开连接、对 GET 返回 200，验证降级路径。
	headFailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// hijacker 把底层连接立即断开，制造 HEAD 传输失败。
			hijacker, ok := w.(http.Hijacker)
			if ok {
				// conn、_, _ 保存被劫持的底层连接并立即关闭。
				conn, _, _ := hijacker.Hijack()
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer headFailServer.Close()
	// fallbackClient 使用普通客户端，让 HEAD 断连后真正降级 GET。
	fallbackClient := &http.Client{Timeout: 2 * time.Second}
	useStubLinkClient(t, fallbackClient)
	// fallbackHits 是 HEAD 失败后 GET 成功的探活结果。
	fallbackHits := CheckDeliveryLinks(context.Background(), "见 "+headFailServer.URL+"/s/1")
	if len(fallbackHits) != 1 || fallbackHits[0].Health != LinkHealthOK {
		t.Fatalf("HEAD 失败后 GET 应判定可达: %+v", fallbackHits)
	}
	// methodServer 对 HEAD 返回 405、对 GET 返回 200，验证不支持 HEAD 的降级路径。
	methodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer methodServer.Close()
	// methodHits 是 HEAD 405 后 GET 成功的探活结果。
	methodHits := CheckDeliveryLinks(context.Background(), "见 "+methodServer.URL+"/s/1")
	if len(methodHits) != 1 || methodHits[0].Health != LinkHealthOK {
		t.Fatalf("HEAD 405 后 GET 应判定可达: %+v", methodHits)
	}
	// notImplementedServer 对 HEAD 返回 501，覆盖另一种不支持 HEAD 的返回。
	notImplementedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer notImplementedServer.Close()
	// notImplementedHits 是 HEAD 501 后 GET 成功的探活结果。
	notImplementedHits := CheckDeliveryLinks(context.Background(), "见 "+notImplementedServer.URL+"/s/1")
	if len(notImplementedHits) != 1 || notImplementedHits[0].Health != LinkHealthOK {
		t.Fatalf("HEAD 501 后 GET 应判定可达: %+v", notImplementedHits)
	}
	// aliveServer 在整个用例期间保持可达，供 nil Context 探活使用。
	aliveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer aliveServer.Close()
	// nilCtxHits 验证历史 nil Context 调用被规范为可用上下文后仍能探活；
	// 该用例必须显式传入 nil 才能覆盖防御分支，故局部豁免 staticcheck 的 nil Context 提示。
	nilCtxHits := CheckDeliveryLinks(nil, "见 "+aliveServer.URL+"/s/1") //nolint:staticcheck
	if len(nilCtxHits) != 1 || nilCtxHits[0].Health != LinkHealthOK {
		t.Fatalf("nil Context 探活失败: %+v", nilCtxHits)
	}
}

// TestCheckDeliveryLinksTruncation 验证 URL 去重、数量上限与无链接短路。
func TestCheckDeliveryLinksTruncation(t *testing.T) {
	// 计数探活请求次数，确认上限截断真实生效。
	requests := 0
	// countingServer 统计被探活的请求次数。
	countingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer countingServer.Close()
	useStubLinkClient(t, &http.Client{Timeout: 2 * time.Second})
	// parts 保存 7 个不同 URL，用于验证最多只检查 5 个。
	parts := make([]string, 0, 7)
	// i 是当前候选 URL 的序号。
	for i := 0; i < 7; i++ {
		parts = append(parts, countingServer.URL+"/s/"+string(rune('a'+i)))
	}
	// joined 是含 7 个 URL 的发送文本。
	joined := strings.Join(parts, " ")
	// hits 是截断后的探活结果。
	hits := CheckDeliveryLinks(context.Background(), joined)
	if len(hits) != deliveryLinkCheckMaxURLs || requests != deliveryLinkCheckMaxURLs {
		t.Fatalf("截断失效 hits=%d requests=%d", len(hits), requests)
	}
	// dupHits 是同一 URL 重复出现时的探活结果，应只检查一次。
	dupHits := CheckDeliveryLinks(context.Background(), countingServer.URL+"/s/x "+countingServer.URL+"/s/x")
	if len(dupHits) != 1 {
		t.Fatalf("URL 未去重: %+v", dupHits)
	}
	// noneHits 是无 http(s) URL 文本的探活结果，应为空。
	noneHits := CheckDeliveryLinks(context.Background(), "只有文本没有链接")
	if noneHits != nil {
		t.Fatalf("无链接应返回空: %+v", noneHits)
	}
	// wwwOnlyHits 是只有 www. 形态候选时的探活结果，健康检查只覆盖 http(s) 地址。
	wwwOnlyHits := CheckDeliveryLinks(context.Background(), "见 www.example.com/s/1")
	if wwwOnlyHits != nil {
		t.Fatalf("www 形态不参与健康检查: %+v", wwwOnlyHits)
	}
	// parsed 确认测试 URL 的主机名提取与网盘判定解耦，避免误放行本地探活地址。
	parsed, _ := url.Parse(countingServer.URL)
	if isNetdiskHost(parsed.Hostname()) {
		t.Fatalf("本地探活地址不应被当作网盘域: %q", parsed.Hostname())
	}
}

// TestScanDeliveryContentUnresolvableURL 验证无法解析主机的伪 URL 仍被外链规则拦截且探活判死。
func TestScanDeliveryContentUnresolvableURL(t *testing.T) {
	// broken 是含非法转义的伪 URL 文本，提取会命中但主机名无法解析。
	hits := ScanDeliveryContent("下载 http://a%zz/x")
	if !hasContentHit(hits, "非网盘外链", "未知域名") {
		t.Fatalf("无法解析的伪 URL 未被拦截: %+v", hits)
	}
	// brokenHits 是伪 URL 的探活结果，请求构造失败必须判为不可达。
	brokenHits := CheckDeliveryLinks(context.Background(), "下载 http://a%zz/x")
	if len(brokenHits) != 1 || brokenHits[0].Health != LinkHealthDead {
		t.Fatalf("伪 URL 探活应判不可达: %+v", brokenHits)
	}
	// closeDeliveryLinkResponse 对 nil 响应必须安全返回。
	closeDeliveryLinkResponse(nil)
	// noBody 是没有响应体的响应，收尾必须安全返回而不 panic。
	noBody := &http.Response{}
	closeDeliveryLinkResponse(noBody)
}

// TestGuardErrorSemantics 验证门禁错误的不可重试标记、哨兵识别与消息不含正文。
func TestGuardErrorSemantics(t *testing.T) {
	// hits 是用于组装错误的命中记录。
	hits := []ContentHit{{Rule: "联系方式/引流", Detail: "联系方式/引流:微信"}}
	// blocked 是组装出的违禁词门禁错误。
	blocked := guardSendError(errContentBlocked, formatContentHits(hits))
	if !isDeliveryGuardError(blocked) {
		t.Fatalf("门禁错误未被识别: %v", blocked)
	}
	if !errors.Is(blocked, errContentBlocked) || !errors.Is(blocked, ErrMessageNotSent) {
		t.Fatalf("门禁错误链缺失哨兵: %v", blocked)
	}
	if !strings.HasPrefix(blocked.Error(), "[no_retry]") {
		t.Fatalf("门禁错误必须标记不可重试: %v", blocked)
	}
	if !strings.Contains(blocked.Error(), "联系方式/引流") {
		t.Fatalf("门禁错误缺少命中原因: %v", blocked)
	}
	if strings.Contains(blocked.Error(), "13812345678") {
		t.Fatalf("门禁错误不得回显用户正文: %v", blocked)
	}
	// dead 是组装出的链接失效门禁错误。
	dead := guardSendError(errLinkDead, "链接已失效(host=pan.baidu.com)")
	if !isDeliveryGuardError(dead) || !errors.Is(dead, errLinkDead) {
		t.Fatalf("链接错误未被识别: %v", dead)
	}
	// other 是普通错误，不应被误判为门禁拦截。
	other := errors.New("普通失败")
	if isDeliveryGuardError(other) {
		t.Fatalf("普通错误被误判为门禁拦截")
	}
	// joined 验证错误被 Join 包装后仍可识别门禁哨兵。
	joined := errors.Join(dead, errors.New("库存恢复失败"))
	if !isDeliveryGuardError(joined) {
		t.Fatalf("Join 包装后门禁哨兵丢失: %v", joined)
	}
	// none 是空命中的格式化结果。
	none := formatContentHits(nil)
	if none != "" {
		t.Fatalf("空命中应格式化为空串: %q", none)
	}
}
