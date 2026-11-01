package engine

import (
	"context"
	"strings"
	"testing"
)

// TestGuardDeflection 覆盖事实兜底的可改写与不可改写边界。
// 核心契约：有确定事实且回复含回避话术才改写；没有事实时一律不动，
// 否则会把「本来就不该断言」的合法回避改成编造。
func TestGuardDeflection(t *testing.T) {
	// cases 是逐条用例：输入回复 + 事实集合，期望文本与是否改写。
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
			name:  "卡密渠道事实下回避话术被替换",
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
			reply: "具体以商品详情为准哦。",
			live: liveKnowledge{
				DeliverySource: deliverySourceNone,
			},
			want:        "具体以商品详情为准哦。",
			wantChanged: false,
		},
		{
			name:  "文本规则猜出的渠道不得用来断言",
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
			reply: "这款是百度网盘发货的，拍下后马上发链接。",
			live: liveKnowledge{
				DeliverySource:   deliverySourceCard,
				DeliveryChannels: []string{"百度网盘"},
			},
			want:        "这款是百度网盘发货的，拍下后马上发链接。",
			wantChanged: false,
		},
	}

	// c 是当前遍历到的用例。
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got、changed 是兜底后的文本与是否改写。
			got, changed := guardDeflection(c.reply, c.live)
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
	got, changed := guardDeflection(reply, live)
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

// TestGuardDeflection_RealDeflectionReplies 用评测里被机检判失败的 4 条真实话术做回归。
// 这批话术是 A4 的存在理由：模型在事实已注入时仍然把买家推回商品详情页。
func TestGuardDeflection_RealDeflectionReplies(t *testing.T) {
	// replies 是 2026-10 生产会话里被判失败的原始回复文本。
	replies := []string{
		"您好～这款是双盘发货的，具体是哪个网盘以商品详情和拍下后的发货消息为准哦。",
		"亲，具体用哪个网盘以商品详情和拍下后的发货消息为准哈，不同商品发货方式不一样～",
		"不同商品发货方式不太一样哦，具体以商品详情和拍下后的发货消息为准～",
		"具体集数商品详情里没标注，我这边也说不准，内容是全的完结版～",
	}
	// live 是按卡密识别出的渠道事实。
	live := liveKnowledge{DeliverySource: deliverySourceCard, DeliveryChannels: []string{"百度网盘", "夸克网盘"}}
	// reply 是当前遍历到的真实回复。
	for _, reply := range replies {
		// got、changed 是兜底后的文本与是否改写。
		got, changed := guardDeflection(reply, live)
		if !changed {
			t.Fatalf("真实回避话术必须被改写: %q", reply)
		}
		if containsDeflection(got) {
			t.Fatalf("改写后仍含回避话术: %q", got)
		}
		if !strings.Contains(got, "百度网盘") {
			t.Fatalf("改写后应给出确定渠道: %q", got)
		}
	}
}
