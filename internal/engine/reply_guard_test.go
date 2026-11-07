package engine

import (
	"context"
	"strings"
	"testing"
)

// TestGuardDeflection 覆盖事实兜底的可改写与不可改写边界。
// 核心契约：有确定性事实、回复含回避话术、且买家问句的话题与事实同属性时才改写；
// 任一条不成立都不得动，否则会把「事实答不了」的合法回避改成答非所问。
func TestGuardDeflection(t *testing.T) {
	// cases 是逐条用例：买家问句 + 输入回复 + 事实集合，期望文本与是否改写。
	cases := []struct {
		// name 是用例名。
		name string
		// buyer 是买家问句，话题门控的输入。
		buyer string
		// reply 是模型产出的回复。
		reply string
		// live 是本次注入的事实集合。
		live liveKnowledge
		// want 是期望得到的回复。
		want string
		// wantChanged 表示是否期望发生改写。
		wantChanged bool
	}{
		{
			name:  "卡密渠道事实下回避话术被替换",
			buyer: "有夸克网盘吗",
			reply: "这款是双盘发货的，具体是哪个网盘以商品详情和拍下后的发货消息为准哦。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘", "夸克网盘"},
			},
			want:        "这款是双盘发货的。本商品发货渠道为：百度网盘、夸克网盘。",
			wantChanged: true,
		},
		{
			name:  "整段都是回避时只留事实句",
			buyer: "发什么网盘",
			reply: "具体以商品详情为准哈。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘"},
			},
			want:        "本商品发货渠道为：百度网盘。",
			wantChanged: true,
		},
		{
			name:  "规格事实下说不准被纠正",
			buyer: "一共多少集",
			reply: "具体集数商品详情里没标注，我这边也说不准，内容是全的完结版。",
			live: liveKnowledge{
				DeliverySource: deliverySourceNone,
				ItemFacts:      []string{"1-4季", "全集"},
			},
			want:        "内容是全的完结版。本商品确定信息：1-4季；全集。",
			wantChanged: true,
		},
		{
			name:  "没有任何事实时不得改写",
			buyer: "有夸克网盘吗",
			reply: "具体以商品详情为准哦。",
			live: liveKnowledge{
				DeliverySource: deliverySourceNone,
			},
			want:        "具体以商品详情为准哦。",
			wantChanged: false,
		},
		{
			name:  "文本规则猜出的渠道不得用来断言",
			buyer: "有夸克网盘吗",
			reply: "具体以商品详情为准哦。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceItemText,
				DeliveryChannels: []string{"百度网盘"},
			},
			want:        "具体以商品详情为准哦。",
			wantChanged: false,
		},
		{
			name:  "正常回复不受影响",
			buyer: "有夸克网盘吗",
			reply: "这款是百度网盘发货的，拍下后马上发链接。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘"},
			},
			want:        "这款是百度网盘发货的，拍下后马上发链接。",
			wantChanged: false,
		},
		{
			name:  "发票类问句不得被改成渠道结论",
			buyer: "能开发票吗",
			reply: "发票相关我这边给不了准信儿，具体以商品详情页和平台规则为准哈～",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘"},
			},
			want:        "发票相关我这边给不了准信儿，具体以商品详情页和平台规则为准哈～",
			wantChanged: false,
		},
		{
			name:  "赠品类问句不得被改成渠道结论",
			buyer: "有赠品或者优惠吗",
			reply: "赠品这块暂时没有相关信息，具体以商品详情页和平台规则为准哈。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘", "夸克网盘"},
			},
			want:        "赠品这块暂时没有相关信息，具体以商品详情页和平台规则为准哈。",
			wantChanged: false,
		},
		{
			name:  "格式问句不得被规格事实以外的渠道事实顶替",
			buyer: "是PDF还是视频",
			reply: "具体是PDF还是视频，以商品详情页和平台规则为准哈～",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘", "夸克网盘"},
				ItemFacts:        []string{"1-4季", "全集"},
			},
			want:        "具体是PDF还是视频，以商品详情页和平台规则为准哈～",
			wantChanged: false,
		},
		{
			name:  "格式问句命中格式事实时改写成该事实",
			buyer: "是PDF还是视频",
			reply: "具体格式以商品详情页和平台规则为准哈～",
			live: liveKnowledge{
				DeliverySource: deliverySourceCard,
				ItemFacts:      []string{"1-4季", "视频"},
			},
			want:        "本商品确定信息：视频。",
			wantChanged: true,
		},
		{
			name:  "设备兼容问句不得被改成渠道结论",
			buyer: "手机和电脑都能看吗",
			reply: "至于具体播放设备和格式兼容，具体以商品详情页和平台规则为准哈～",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘", "夸克网盘"},
			},
			want:        "至于具体播放设备和格式兼容，具体以商品详情页和平台规则为准哈～",
			wantChanged: false,
		},
		{
			name:  "发货进度问句不得被改成渠道结论",
			buyer: "发货凭证在哪看",
			reply: "这个以拍下后的发货消息为准哈。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘", "夸克网盘"},
			},
			want:        "这个以拍下后的发货消息为准哈。",
			wantChanged: false,
		},
	}

	// c 是当前遍历到的用例。
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got、changed 是兜底后的文本与是否改写。
			got, changed := guardDeflection(c.reply, c.live, c.buyer)
			if changed != c.wantChanged {
				t.Fatalf("改写判定不符：got=%v want=%v（文本 %q）", changed, c.wantChanged, got)
			}
			if got != c.want {
				t.Fatalf("改写结果不符：\n got=%q\nwant=%q", got, c.want)
			}
		})
	}
}

// TestGuardDeflectionNeverLeavesDeflectionPhrase 改写后的文本不得残留任何回避话术。
func TestGuardDeflectionNeverLeavesDeflectionPhrase(t *testing.T) {
	// reply 是一段包含多处回避的回复。
	reply := "不同商品发货方式不太一样哦，具体以商品详情为准～我这边也说不准。"
	// live 是带卡密渠道的事实集合。
	live := liveKnowledge{DeliverySource: deliverySourceCard, DeliveryChannels: []string{"夸克网盘"}}
	// got 是兜底后的文本。
	got, changed := guardDeflection(reply, live, "有夸克网盘吗")
	if !changed {
		t.Fatalf("应发生改写")
	}
	// phrase 是当前遍历到的回避话术。
	for _, phrase := range replyDeflectionPhrases {
		if strings.Contains(got, phrase) {
			t.Fatalf("改写后仍残留回避话术 %q：%q", phrase, got)
		}
	}
	if !strings.Contains(got, "夸克网盘") {
		t.Fatalf("改写后应给出事实渠道：%q", got)
	}
}

// TestSplitReplySentences 句末标点与换行都要参与切分，空白片段不得留空句。
func TestSplitReplySentences(t *testing.T) {
	// got 是切分结果。
	got := splitReplySentences("第一句。第二句！\n\n第三句？  ")
	if len(got) != 3 {
		t.Fatalf("应切成 3 句，got=%d（%v）", len(got), got)
	}
}

// TestAIReply_DeflectionGuardRewritesReply 端到端锁住兜底生效：
// 商品标题已给出规格事实、模型仍答「详情里没标注」，最终外发文本必须是事实结论，
// 且归因里 answer_source 记为 policy-guard——否则事后会把兜底产出当成模型能力。
func TestAIReply_DeflectionGuardRewritesReply(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// srv、_ 是固定返回回避话术的 mock 模型。
	srv, _ := aiContentMockServer(t, "具体集数商品详情里没标注，我这边也说不准。")
	// err 是启用行写入失败原因。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`); err != nil {
		t.Fatalf("插入启用行: %v", err)
	}
	// err 是商品写入失败原因；标题决定规格事实能否被抽出来。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_price, item_description, item_detail) VALUES ('cid','item-guard','动画全集 1-4季','9.90','','')`); err != nil {
		t.Fatalf("插入商品: %v", err)
	}
	// err 是密钥写入失败原因；缺密钥会直接跳过 AI 通道。
	if err := s.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatalf("写入密钥: %v", err)
	}
	// err 是模型地址写入失败原因；不指向 mock 会打到真实端点。
	if err := s.Settings.Set(ctx, "ai_api_url", srv.URL); err != nil {
		t.Fatalf("写入模型地址: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// res、err 是完整回复链路结果。
	res, err := a.Reply(ctx, chatMsg("一共多少集呀", "item-guard", "chat-guard"))
	if err != nil || res == nil {
		t.Fatalf("应接管并返回结果: res=%v err=%v", res, err)
	}
	if containsDeflection(res.Text) {
		t.Fatalf("外发文本不得残留回避话术: %q", res.Text)
	}
	if !strings.Contains(res.Text, "本商品确定信息") {
		t.Fatalf("外发文本应给出确定事实: %q", res.Text)
	}
	// source 是归因表里记录的答案来源。
	var source string
	// err 是归因行读取失败原因；查不到说明埋点没写或条件写错。
	if err := s.DB.QueryRowContext(ctx, `SELECT answer_source FROM ai_generation_log WHERE chat_id='chat-guard'`).Scan(&source); err != nil {
		t.Fatalf("读取生成归因: %v", err)
	}
	if source != answerSourcePolicyGuard {
		t.Fatalf("答案来源应为 %s，got=%s", answerSourcePolicyGuard, source)
	}
}

// TestGuardDeflection_RealDeflectionReplies 用评测里被机检判失败的 6 条真实话术做回归。
// 这批话术是 A4 的存在理由：模型在事实已注入时仍然把买家推回商品详情页。
func TestGuardDeflection_RealDeflectionReplies(t *testing.T) {
	// cases 是「真实回避话术 + 该话术对应的买家问句」组合，问句决定事实类型。
	cases := []struct {
		// buyer 是触发该回避回复的买家问句。
		buyer string
		// reply 是 2026-10 生产会话与 11-07 dry-run 里被判失败的原始回复文本。
		reply string
	}{
		{buyer: "有度盘的吗", reply: "您好～这款是双盘发货的，具体是哪个网盘以商品详情和拍下后的发货消息为准哦。"},
		{buyer: "夸克网盘有吗", reply: "亲，具体用哪个网盘以商品详情和拍下后的发货消息为准哈，不同商品发货方式不一样～"},
		{buyer: "支持网盘发货吗", reply: "不同商品发货方式不太一样哦，具体以商品详情和拍下后的发货消息为准～"},
		{buyer: "一共多少集", reply: "具体集数商品详情里没标注，我这边也说不准，内容是全的完结版～"},
		{buyer: "这款发什么网盘", reply: "亲，具体网盘类型以商品详情和拍下后的发货消息为准哈，我这边先帮您确认下，您拍下后备注一下夸克，我看能不能安排~"},
		{buyer: "怎么发货", reply: "不同商品发货方式不一样哈，具体以商品详情页说明和拍下后的发货消息为准～"},
	}
	// live 是同时带渠道事实与规格事实的事实集合，按问句话题各取所需。
	live := liveKnowledge{
		DeliverySource:   deliverySourceCard,
		DeliveryChannels: []string{"百度网盘", "夸克网盘"},
		ItemFacts:        []string{"1-4季", "全集"},
	}
	// c 是当前遍历到的真实用例。
	for _, c := range cases {
		// got、changed 是兜底后的文本与是否改写。
		got, changed := guardDeflection(c.reply, live, c.buyer)
		if !changed {
			t.Fatalf("真实回避话术必须被改写: %q（问句 %q）", c.reply, c.buyer)
		}
		if containsDeflection(got) {
			t.Fatalf("改写后仍含回避话术: %q", got)
		}
		if !strings.Contains(got, "百度网盘") && !strings.Contains(got, "全集") {
			t.Fatalf("改写后应给出确定事实: %q", got)
		}
	}
}

// TestContainsDeflection_Cooccurrence 锁定「指向对象 + 推诿判断词」共现规则：// 插入式变体（以商品详情页说明和拍下后的发货消息为准）必须命中，
// 而「详情页写了 1-4 季」这类正常陈述必须放行——否则会把陈述误改写成渠道结论。
func TestContainsDeflection_Cooccurrence(t *testing.T) {
	// cases 是逐条用例：文本 + 是否期望判为回避。
	cases := []struct {
		// name 是用例名。
		name string
		// text 是待判定文本。
		text string
		// want 表示是否期望判为回避。
		want bool
	}{
		{name: "插入页说明的变体", text: "具体以商品详情页说明和拍下后的发货消息为准～", want: true},
		{name: "发货消息为准", text: "以拍下后的发货消息为准", want: true},
		{name: "详情里没标注", text: "具体集数商品详情里没标注", want: true},
		{name: "无指向对象的说不准", text: "我这边也说不准", want: true},
		{name: "正常陈述详情页有规格", text: "详情页写了 1-4 季，拍下后马上发链接。", want: false},
		{name: "正常表达不确定", text: "拍下后不确定能不能改地址", want: false},
		{name: "正常给出渠道", text: "这款是百度网盘发货的，拍下后马上发链接。", want: false},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got 是判定结果。
			if got := containsDeflection(c.text); got != c.want {
				t.Fatalf("回避判定不符：got=%v want=%v（文本 %q）", got, c.want, c.text)
			}
		})
	}
}

// TestIsDeliveryQuestion 锁定渠道问句的判定边界。
// 「发货」单独出现时多指时效或进度（今天能发吗 / 发货了吗），不能被判成渠道问句，
// 否则守卫又会把时效、履约类问句改写成渠道结论。
func TestIsDeliveryQuestion(t *testing.T) {
	// cases 是逐条用例：买家问句 + 是否期望判为渠道问句。
	cases := []struct {
		// text 是买家问句。
		text string
		// want 表示是否期望判为渠道问句。
		want bool
	}{
		{text: "有度盘的吗", want: true},
		{text: "夸克？", want: true},
		{text: "这款发什么网盘", want: true},
		{text: "怎么发货", want: true},
		{text: "支持网盘发货吗", want: true},
		{text: "百度和夸克两个网盘都有吗", want: true},
		{text: "今天下单今天能发吗", want: false},
		{text: "我的订单发货了吗", want: false},
		{text: "发货凭证在哪看", want: false},
		{text: "能开发票吗", want: false},
		{text: "手机和电脑都能看吗", want: false},
		{text: "在吗", want: false},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		// got 是判定结果。
		if got := isDeliveryQuestion(c.text); got != c.want {
			t.Fatalf("渠道问句判定不符：got=%v want=%v（问句 %q）", got, c.want, c.text)
		}
	}
}

// TestMatchSpecFacts 锁定规格事实的属性组匹配：同属性才算能回答。
func TestMatchSpecFacts(t *testing.T) {
	// facts 是商品标题抽出的规格事实。
	facts := []string{"1-4季", "全集", "视频"}
	// cases 是逐条用例：问句 + 期望命中的事实。
	cases := []struct {
		// text 是买家问句。
		text string
		// want 是期望命中的事实集。
		want []string
	}{
		{text: "一共多少集", want: []string{"1-4季", "全集"}},
		{text: "是PDF还是视频", want: []string{"视频"}},
		{text: "能开发票吗", want: nil},
		{text: "包邮吗", want: nil},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		// got 是命中的事实集。
		got := matchSpecFacts(facts, c.text)
		if len(got) != len(c.want) {
			t.Fatalf("命中事实数不符：got=%v want=%v（问句 %q）", got, c.want, c.text)
		}
		// i 是当前比较的下标。
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("命中事实不符：got=%v want=%v（问句 %q）", got, c.want, c.text)
			}
		}
	}
}

// TestGuardDeflection_RealTopicMismatchRegressions 锁定 2026-11-07 真实跑分里
// 由守卫自己造成的答非所问回归：这些回复在守卫之前的版本是通过的，
// 加上渠道结论后才被判失败。事实与问句不同属性时，守卫必须原样放行。
func TestGuardDeflection_RealTopicMismatchRegressions(t *testing.T) {
	// live 是这些会话共同注入的事实：卡密渠道 + 标题规格。
	live := liveKnowledge{
		DeliverySource:   deliverySourceCard,
		DeliveryChannels: []string{"百度网盘", "夸克网盘"},
	}
	// cases 是逐条真实回归用例：买家问句 + 守卫前通过的原始回复。
	cases := []struct {
		// name 是用例名，带评测用例 ID 便于回查报告。
		name string
		// buyer 是买家问句。
		buyer string
		// reply 是守卫之前被判通过的原始回复。
		reply string
	}{
		{
			name:  "catalog：问格式答渠道",
			buyer: "是PDF还是视频",
			reply: "具体是PDF还是视频，以商品详情页和平台规则为准哈～发货方式是百度网盘和夸克网盘双盘发货，内容34集全。",
		},
		{
			name:  "compliance：问赠品答渠道",
			buyer: "有赠品或者优惠吗",
			reply: "赠品这块暂时没有相关信息，具体以商品详情页和平台规则为准哈。价格就是标价 2.13 元，已经是实价了，拍下就能发～",
		},
		{
			name:  "link-code：问设备兼容答渠道",
			buyer: "手机和电脑都能看吗",
			reply: "这款是发百度网盘和夸克网盘，手机上、电脑上登录网盘都能看；至于具体播放设备和格式兼容，具体以商品详情页和平台规则为准哈～",
		},
	}
	// c 是当前遍历到的用例。
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got、changed 是兜底后的文本与是否改写。
			got, changed := guardDeflection(c.reply, live, c.buyer)
			if changed {
				t.Fatalf("话题不匹配时不得改写：%q", got)
			}
			if got != c.reply {
				t.Fatalf("原文必须原样保留：\n got=%q\nwant=%q", got, c.reply)
			}
		})
	}
}

// TestGuardCompliance 覆盖三类合规改写的边界：
// 禁语必须改写、无依据包邮必须加限定、已确认包邮或已带否定的表述不得误改。
func TestGuardCompliance(t *testing.T) {
	// confirmedFreeShipping 是平台明确标注包邮的事实文案。
	confirmedFreeShipping := "平台标注运费信息：包邮，运费由卖家承担。"
	// cases 是逐条用例：输入回复 + 事实集合 + 期望文本与是否改写。
	cases := []struct {
		// name 是用例名。
		name string
		// reply 是模型产出的回复。
		reply string
		// live 是本次注入的事实集合。
		live liveKnowledge
		// want 是期望得到的回复。
		want string
		// wantChanged 表示是否期望发生改写。
		wantChanged bool
	}{
		{
			name:        "退款禁语被替换为默认话术",
			reply:       "已经发出的电子资料，非质量问题不支持退款。",
			live:        liveKnowledge{},
			want:        complianceRefundDefault,
			wantChanged: true,
		},
		{
			name:        "无运费事实时包邮承诺加通用限定",
			reply:       "这款包邮的，不用另付运费~",
			live:        liveKnowledge{},
			want:        shippingDefaultQualifier,
			wantChanged: true,
		},
		{
			name:        "已确认包邮时不改写",
			reply:       "这款包邮的，拍下后发百度网盘。",
			live:        liveKnowledge{ShippingFact: confirmedFreeShipping},
			want:        "这款包邮的，拍下后发百度网盘。",
			wantChanged: false,
		},
		{
			name:        "包邮承诺只删该分句并保留其余内容",
			reply:       "在的，亲～这款双盘发货，包邮的，2.13元",
			live:        liveKnowledge{},
			want:        "在的，亲～这款双盘发货，2.13元。运费以下单页面显示为准。",
			wantChanged: true,
		},
		{
			name:        "不包邮表述不得误改",
			reply:       "这款不包邮，运费以拍下页面为准。",
			live:        liveKnowledge{},
			want:        "这款不包邮，运费以拍下页面为准。",
			wantChanged: false,
		},
		{
			name:        "正版否定断言被替换",
			reply:       "这不是正版。您考虑清楚。",
			live:        liveKnowledge{},
			want:        complianceAuthenticityDefault + "您考虑清楚。",
			wantChanged: true,
		},
		{
			name:        "正常回复不受影响",
			reply:       "这款发百度网盘和夸克网盘，拍下后留意发货消息。",
			live:        liveKnowledge{},
			want:        "这款发百度网盘和夸克网盘，拍下后留意发货消息。",
			wantChanged: false,
		},
	}

	// c 是当前遍历到的用例。
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got、changed 是合规兜底后的文本与是否改写。
			got, changed := guardCompliance(c.reply, c.live)
			if changed != c.wantChanged {
				t.Fatalf("改写判定不符：got=%v want=%v（文本 %q）", changed, c.wantChanged, got)
			}
			if got != c.want {
				t.Fatalf("改写结果不符：\n got=%q\nwant=%q", got, c.want)
			}
		})
	}
}

// TestAIReply_ComplianceGuardRewritesForbiddenRefund 端到端锁住合规兜底：
// 模型输出「不支持退款」时，外发文本必须是默认合规话术，且归因记为 policy-compliance。
func TestAIReply_ComplianceGuardRewritesForbiddenRefund(t *testing.T) {
	// s、cleanup 是带账号的测试 store。
	s, cleanup := newAIStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// srv、_ 是固定返回退款禁语的 mock 模型。
	srv, _ := aiContentMockServer(t, "已经发出的电子资料，非质量问题不支持退款。")
	// err 是启用行写入失败原因。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled, custom_prompts) VALUES ('cid', 1, '')`); err != nil {
		t.Fatalf("插入启用行: %v", err)
	}
	// err 是商品写入失败原因。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_price, item_description, item_detail) VALUES ('cid','item-compliance','动画全集','9.90','','')`); err != nil {
		t.Fatalf("插入商品: %v", err)
	}
	// err 是密钥写入失败原因；缺密钥会直接跳过 AI 通道。
	if err := s.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatalf("写入密钥: %v", err)
	}
	// err 是模型地址写入失败原因；不指向 mock 会打到真实端点。
	if err := s.Settings.Set(ctx, "ai_api_url", srv.URL); err != nil {
		t.Fatalf("写入模型地址: %v", err)
	}
	// a 是待测 AI 实现。
	a := NewAIReplier("cid", s, nil)
	// res、err 是完整回复链路结果。
	res, err := a.Reply(ctx, chatMsg("这个能退吗", "item-compliance", "chat-compliance"))
	if err != nil || res == nil {
		t.Fatalf("应接管并返回结果: res=%v err=%v", res, err)
	}
	if strings.Contains(res.Text, "不支持退款") {
		t.Fatalf("外发文本不得残留退款禁语: %q", res.Text)
	}
	if res.Text != complianceRefundDefault {
		t.Fatalf("外发文本应为默认合规话术: %q", res.Text)
	}
	// source 是归因表里记录的答案来源。
	var source string
	// err 是归因行读取失败原因；查不到说明埋点没写或条件写错。
	if err := s.DB.QueryRowContext(ctx, `SELECT answer_source FROM ai_generation_log WHERE chat_id='chat-compliance'`).Scan(&source); err != nil {
		t.Fatalf("读取生成归因: %v", err)
	}
	if source != answerSourcePolicyCompliance {
		t.Fatalf("答案来源应为 %s，got=%s", answerSourcePolicyCompliance, source)
	}
}
