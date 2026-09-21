// ai_content_eval_test.go 客服 Agent 内容层评测（确定性部分）：测「接管后回复质量」。
// 三类断言全部用 mock OpenAI server 走完整 Reply 链路，零账号可执行：
//  1. FAQ 命中率：命中的 FAQ 答案注入 system prompt，且回复文本包含知识答案；
//  2. 在售清单引用率：涉及库存/在售的回复引用了清单条目；
//  3. 策略话术覆盖率：报价越过折扣边界时回复套用 min_price_reply/no_discount_reply，
//     且 {amount} 占位被替换为实际最低价。
//
// 样本全部脱敏：文本按语义类别拟写的短句，不含真实聊天全文与凭证。
// 真实模型质量分（LLM-as-judge/人工抽样）不在此文件实现，接入位置见 runAIContentEvaluation 注释。
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"xianyu-go/internal/db"
)

// 内容层评测的断言类别，runner 按类别汇总对应指标。
const (
	// aiContentKindFAQ 是 FAQ 命中类：知识答案注入提示词且回复包含答案。
	aiContentKindFAQ = "faq"
	// aiContentKindCatalog 是在售清单引用类：回复引用了清单条目。
	aiContentKindCatalog = "catalog"
	// aiContentKindPolicy 是策略话术类：报价越界时套用配置话术。
	aiContentKindPolicy = "policy"
)

// aiContentEvalCase 是一条内容层评测用例。
type aiContentEvalCase struct {
	// Name 是用例名，用于失败定位与日志输出。
	Name string
	// Text 是脱敏后的买家消息文本。
	Text string
	// Kind 是断言类别（faq/catalog/policy）。
	Kind string
	// ModelReply 是 mock 模型对本次消息的固定回复。
	ModelReply string
	// WantReplyContains 是回复文本必须包含的片段；空串列表表示不检查。
	WantReplyContains []string
	// WantReplyAbsent 是回复文本必须不包含的片段（如未在清单内的商品名）。
	WantReplyAbsent []string
	// WantReplyExact 是策略话术类用例的整句期望（{amount} 已替换）；空串表示不检查。
	WantReplyExact string
	// WantSystemContains 是 system prompt 必须包含的片段，验证 FAQ/清单知识确实注入。
	WantSystemContains []string
	// WantSystemAbsent 是 system prompt 必须不包含的片段，验证未命中 FAQ 不注入。
	WantSystemAbsent []string
}

// aiContentEvalGroup 是一组共享同一配置场景的内容层评测用例。
type aiContentEvalGroup struct {
	// Name 是配置场景名。
	Name string
	// ScopeConfig 是写入 ai_scope_config 的 JSON（意图白名单 + 负向词）。
	ScopeConfig string
	// KnowledgeConfig 是写入 ai_knowledge_config 的 JSON（FAQ + 在售清单）。
	KnowledgeConfig string
	// PolicyConfig 是写入 ai_policy_config 的 JSON（min_price_reply/no_discount_reply）。
	PolicyConfig string
	// ItemPrice 是写入 item_info 的商品标价文本。
	ItemPrice string
	// MaxDiscountPercent 与 MaxDiscountAmount 是 AI 回复设置里的折扣上限，共同决定最低价。
	MaxDiscountPercent int
	MaxDiscountAmount  int
	// Cases 是该场景下的评测用例列表。
	Cases []aiContentEvalCase
}

// aiContentScopeJSON 是内容层评测的边界配置：砍价/商品/发货/库存四类意图接管，
// 覆盖全部评测消息的意图命中；负向词沿用生产种子配置，防止负向消息误入内容层。
const aiContentScopeJSON = `{"intents":[
	{"id":"bargain","enabled":true,"ai":true,"match":"便宜|优惠|少点|最低|砍价|降价|打折"},
	{"id":"product","enabled":true,"ai":true,"match":"正版|音频|文字版"},
	{"id":"delivery","enabled":true,"ai":true,"match":"发货|网盘|夸克|资源码|提取码|下载"},
	{"id":"stock","enabled":true,"ai":true,"match":"\"有.{0,4}季\"|全集|单买|在售"}
],"negative":"退款|退货|投诉|差评|举报|骗子|骗人|假货|被骗|维权|违规|扣分|封号|申诉"}`

// aiContentKnowledgeJSON 是内容层评测的语料配置：五条 FAQ 覆盖发货/资源码/下载/音频/文字版，
// 两条在售清单用于引用断言，与升级计划第六节语料形态一致。
const aiContentKnowledgeJSON = `{"faq":[
	{"category":"发货方式","match":"发货|网盘|夸克","answer":"本店支持百度网盘、夸克、迅雷发货。"},
	{"category":"资源码","match":"资源码|提取码","answer":"资源码在订单详情页查看，复制后到网盘输入即可。"},
	{"category":"下载方式","match":"下载|提取码","answer":"购买后可直接下载，提取码在订单详情页查看。"},
	{"category":"音频内容","match":"音频","answer":"有完整音频，下单后发放。"},
	{"category":"文字版","match":"文字版","answer":"文字版购买后同步发送。"}
],
"catalog":[
	{"title":"糯糯下山","detail":"1-3季全"},
	{"title":"梦遇崔郎","detail":"92集完整版"}
]}`

// aiContentPolicyJSON 是内容层评测的策略配置：最低价话术带 {amount} 占位，
// 拒绝话术为整句文案，供报价越界与零折扣两类场景断言。
const aiContentPolicyJSON = `{"min_price_reply":"亲，最低 {amount} 元哦","no_discount_reply":"抱歉，已经是最低价了，暂时不能再优惠了。"}`

// aiContentEvalSetV1 是内容层评测集 v1：FAQ 命中 6 条（含 1 条不应注入的负控制）、
// 在售清单引用 5 条（含 1 条未在清单内的负控制）、策略话术 6 条（最低价 4 + 拒绝 2），
// 共 17 条。分组设计：知识语料与最低价话术共享折扣设置，拒绝话术单独用零折扣场景。
var aiContentEvalSetV1 = []aiContentEvalGroup{
	{
		Name:               "知识语料与最低价话术",
		ScopeConfig:        aiContentScopeJSON,
		KnowledgeConfig:    aiContentKnowledgeJSON,
		PolicyConfig:       aiContentPolicyJSON,
		ItemPrice:          "100",
		MaxDiscountPercent: 10,
		MaxDiscountAmount:  20,
		Cases: []aiContentEvalCase{
			{Name: "FAQ发货方式命中", Text: "是百度网盘发货吗", Kind: aiContentKindFAQ,
				ModelReply:         "支持百度网盘、夸克、迅雷发货哦",
				WantReplyContains:  []string{"百度网盘、夸克、迅雷发货"},
				WantSystemContains: []string{"本店支持百度网盘、夸克、迅雷发货。"}},
			{Name: "FAQ资源码命中", Text: "资源码怎么用", Kind: aiContentKindFAQ,
				ModelReply:         "资源码在订单详情页查看哦",
				WantReplyContains:  []string{"订单详情页"},
				WantSystemContains: []string{"资源码在订单详情页查看"}},
			{Name: "FAQ下载方式命中", Text: "怎么下载啊", Kind: aiContentKindFAQ,
				ModelReply:         "购买后可以直接下载哦",
				WantReplyContains:  []string{"直接下载"},
				WantSystemContains: []string{"购买后可直接下载"}},
			{Name: "FAQ音频命中", Text: "有音频嘛", Kind: aiContentKindFAQ,
				ModelReply:         "有的，有完整音频哦",
				WantReplyContains:  []string{"完整音频"},
				WantSystemContains: []string{"有完整音频，下单后发放"}},
			{Name: "FAQ文字版命中", Text: "文字版在哪里", Kind: aiContentKindFAQ,
				ModelReply:         "文字版购买后同步发送哦",
				WantReplyContains:  []string{"文字版购买后同步发送"},
				WantSystemContains: []string{"文字版购买后同步发送。"}},
			{Name: "FAQ未命中不注入", Text: "是正版的吗", Kind: aiContentKindFAQ,
				ModelReply:        "是的，正版哦",
				WantReplyContains: []string{"正版"},
				WantSystemAbsent:  []string{"本店支持百度网盘"}},
			{Name: "清单季数引用", Text: "有第三季了吗", Kind: aiContentKindCatalog,
				ModelReply:         "有的，糯糯下山有 1-3 季全在售哦",
				WantReplyContains:  []string{"糯糯下山"},
				WantSystemContains: []string{"糯糯下山（1-3季全）"}},
			{Name: "清单单买引用", Text: "可以单买第二季吗", Kind: aiContentKindCatalog,
				ModelReply:         "可以单买哦，梦遇崔郎可以单买",
				WantReplyContains:  []string{"梦遇崔郎"},
				WantSystemContains: []string{"梦遇崔郎（92集完整版）"}},
			{Name: "清单全集引用", Text: "是全集吗", Kind: aiContentKindCatalog,
				ModelReply:         "糯糯下山是全集在售哦",
				WantReplyContains:  []string{"糯糯下山"},
				WantSystemContains: []string{"糯糯下山（1-3季全）"}},
			{Name: "清单夸克版引用", Text: "有夸克版的吗", Kind: aiContentKindCatalog,
				ModelReply:         "有的，梦遇崔郎有夸克版哦",
				WantReplyContains:  []string{"夸克", "梦遇崔郎"},
				WantSystemContains: []string{"梦遇崔郎（92集完整版）"}},
			{Name: "清单未在售不引用", Text: "有第五季了吗", Kind: aiContentKindCatalog,
				ModelReply:      "这个暂时没有哦",
				WantReplyAbsent: []string{"糯糯下山", "梦遇崔郎"}},
			{Name: "策略最低价话术一", Text: "能便宜点吗", Kind: aiContentKindPolicy,
				ModelReply:     "可以，80 元成交",
				WantReplyExact: "亲，最低 90.00 元哦"},
			{Name: "策略最低价话术二", Text: "再便宜点", Kind: aiContentKindPolicy,
				ModelReply:     "最低 85 元吧",
				WantReplyExact: "亲，最低 90.00 元哦"},
			{Name: "策略最低价话术三", Text: "最低多少钱", Kind: aiContentKindPolicy,
				ModelReply:     "60 元行不行",
				WantReplyExact: "亲，最低 90.00 元哦"},
			{Name: "策略最低价话术四", Text: "能打折吗", Kind: aiContentKindPolicy,
				ModelReply:     "88 元可以吗",
				WantReplyExact: "亲，最低 90.00 元哦"},
		},
	},
	{
		Name:               "拒绝话术",
		ScopeConfig:        aiContentScopeJSON,
		PolicyConfig:       aiContentPolicyJSON,
		ItemPrice:          "100",
		MaxDiscountPercent: 0,
		MaxDiscountAmount:  0,
		Cases: []aiContentEvalCase{
			{Name: "策略拒绝话术一", Text: "能便宜点吗", Kind: aiContentKindPolicy,
				ModelReply:     "可以，80 元成交",
				WantReplyExact: "抱歉，已经是最低价了，暂时不能再优惠了。"},
			{Name: "策略拒绝话术二", Text: "便宜点嘛", Kind: aiContentKindPolicy,
				ModelReply:     "70 块卖不卖",
				WantReplyExact: "抱歉，已经是最低价了，暂时不能再优惠了。"},
		},
	},
}

// aiContentMockRecorder 记录 mock 模型最近一次请求的 system prompt，用于断言知识注入。
type aiContentMockRecorder struct {
	// mu 保护 systemPrompt 的并发读写。
	mu sync.Mutex
	// systemPrompt 是最近一次 chat completion 请求中的 system 消息内容。
	systemPrompt string
}

// system 返回最近一次请求的 system prompt；尚无请求时返回空串。
func (r *aiContentMockRecorder) system() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.systemPrompt
}

// aiContentMockServer 启动返回固定回复并记录 system prompt 的 mock OpenAI 服务。
// 与 mockOpenAIServer 同构，额外解析请求体以便断言 FAQ/在售清单确实注入提示词。
func aiContentMockServer(t *testing.T, content string) (*httptest.Server, *aiContentMockRecorder) {
	t.Helper()
	// rec 是请求记录器。
	rec := &aiContentMockRecorder{}
	// srv 是本地 mock 服务。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// body 是 chat completion 请求体结构。
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		// 解析请求体并记录 system 消息供断言；解析失败不影响固定回复。
		if decErr := json.NewDecoder(r.Body).Decode(&body); decErr == nil {
			rec.mu.Lock()
			for _, msg := range body.Messages {
				if msg.Role == "system" {
					rec.systemPrompt = msg.Content
					break
				}
			}
			rec.mu.Unlock()
		}
		// resp 是固定的 chat completion 成功响应。
		resp := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// contentStore 装配内容层用例的运行环境：临时 SQLite store + AI 回复设置 + 商品 +
// 三层配置 + API 指向 mock 服务；空配置键跳过写入，与 loadScope/loadKnowledge/loadPolicy 的空值回落一致。
func contentStore(t *testing.T, g aiContentEvalGroup, apiURL string) *db.Store {
	t.Helper()
	// s、cleanup 是带 admin+cookie 的测试 store。
	s, cleanup := newAIStore(t)
	t.Cleanup(cleanup)
	// ctx 是配置写入上下文。
	ctx := context.Background()
	// 写入 AI 回复设置：启用接管并给定折扣上限与砍价轮次。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings
		(cookie_id, ai_enabled, max_discount_percent, max_discount_amount, max_bargain_rounds, custom_prompts)
		VALUES ('cid', 1, ?, ?, 3, '')`, g.MaxDiscountPercent, g.MaxDiscountAmount); err != nil {
		t.Fatalf("写入 AI 回复设置失败: %v", err)
	}
	// 写入商品信息，供最低价计算与提示词使用。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO item_info
		(cookie_id, item_id, item_title, item_price, item_description) VALUES ('cid', 'item-eval', '测试商品', ?, '测试描述')`, g.ItemPrice); err != nil {
		t.Fatalf("写入商品失败: %v", err)
	}
	// settings 是待写入的三层配置键值对；空值跳过。
	settings := map[string]string{
		settingAIScopeKey:     g.ScopeConfig,
		settingAIKnowledgeKey: g.KnowledgeConfig,
		settingAIPolicyKey:    g.PolicyConfig,
	}
	// key、val 表示当前遍历到的设置键与值。
	for key, val := range settings {
		if strings.TrimSpace(val) == "" {
			continue
		}
		// err 是写入系统设置的数据库错误。
		if err := s.Settings.Set(ctx, key, val); err != nil {
			t.Fatalf("写入设置 %s 失败: %v", key, err)
		}
	}
	// 写入模型调用配置：测试密钥与 mock 服务地址。
	if err := s.Settings.Set(ctx, "ai_api_key", "sk-test"); err != nil {
		t.Fatalf("写入 AI API Key 失败: %v", err)
	}
	if err := s.Settings.Set(ctx, "ai_api_url", apiURL); err != nil {
		t.Fatalf("写入 AI API 地址失败: %v", err)
	}
	return s
}

// checkAIContentCase 按用例断言类别校验回复质量，返回是否通过与失败原因。
// FAQ 类同时断言知识注入（system prompt 含/不含期望片段）与回复内容；
// 在售清单类断言清单条目已注入且回复引用了清单内容；
// 策略话术类断言回复整句等于 {amount} 占位替换后的配置模板。
func checkAIContentCase(c aiContentEvalCase, systemPrompt string, res *ReplyResult) (bool, string) {
	// 未接管或调用失败视为不通过。
	if res == nil {
		return false, "未接管"
	}
	// reply 是买家可见的最终回复文本。
	reply := res.Text
	// system 是最近一次请求的 system prompt。
	system := systemPrompt
	if c.Kind == aiContentKindFAQ || c.Kind == aiContentKindCatalog {
		// want 是必须注入提示词的期望片段。
		for _, want := range c.WantSystemContains {
			if !strings.Contains(system, want) {
				return false, fmt.Sprintf("知识未注入提示词，期望包含 %q", want)
			}
		}
		// absent 是不应注入提示词的片段（FAQ 负控制用例）。
		for _, absent := range c.WantSystemAbsent {
			if strings.Contains(system, absent) {
				return false, fmt.Sprintf("未命中 FAQ 却注入了提示词，包含 %q", absent)
			}
		}
	}
	// 策略话术类断言整句等值（含 {amount} 占位替换结果）。
	if c.WantReplyExact != "" && reply != c.WantReplyExact {
		return false, fmt.Sprintf("策略话术不符，期望 %q 实际 %q", c.WantReplyExact, reply)
	}
	// want 是回复必须包含的期望片段。
	for _, want := range c.WantReplyContains {
		if !strings.Contains(reply, want) {
			return false, fmt.Sprintf("回复缺少期望内容 %q，实际 %q", want, reply)
		}
	}
	// absent 是回复必须不包含的片段（如未在清单内的商品名）。
	for _, absent := range c.WantReplyAbsent {
		if strings.Contains(reply, absent) {
			return false, fmt.Sprintf("回复包含不应出现的内容 %q，实际 %q", absent, reply)
		}
	}
	return true, ""
}

// 真实模型质量分的接入位置说明（本文件不实现，仅供后续阶段参考）：
//  1. 接入点：把 aiContentMockServer 换成真实模型端点——contentStore 里 ai_api_url 指向
//     真实兼容端点（如 dashscope 兼容模式）、ai_api_key 用真实密钥，重跑 aiContentEvalSetV1，
//     逐条采集真实模型回复文本。
//  2. 打分方式：对采集的回复做 LLM-as-judge（另一模型按评分标准打分）或人工抽样评分，
//     得到回复质量分，驱动 FAQ/策略话术调优。
//  3. 不能进默认门禁的原因：真实模型输出非确定（Temperature 0.7 随机采样）、依赖真实
//     密钥与调用成本、质量判断主观需人工校准；默认门禁只放行确定性的 mock 断言。
//
// runAIContentEvaluation 遍历内容层评测集，逐条走完整 Reply 链路并断言回复质量，
// 汇总 FAQ 命中率/在售清单引用率/策略话术覆盖率三类指标，返回失败用例描述列表。
func runAIContentEvaluation(t *testing.T, groups []aiContentEvalGroup) (aiContentMetrics, []string) {
	t.Helper()
	// m 是本次评测的汇总指标。
	var m aiContentMetrics
	// failures 是期望与实际不一致的用例描述。
	var failures []string
	// ctx 是所有用例共享的判定上下文。
	ctx := context.Background()
	// g 表示当前遍历到的配置场景。
	for _, g := range groups {
		// c 表示当前遍历到的评测用例。
		for _, c := range g.Cases {
			// srv、rec 是当前用例的 mock 模型服务与 system prompt 记录器。
			srv, rec := aiContentMockServer(t, c.ModelReply)
			// s 是当前用例的临时 store。
			s := contentStore(t, g, srv.URL)
			// a 是当前用例的 AI 回复实现。
			a := NewAIReplier("cid", s, nil)
			// res、err 是完整 Reply 链路的返回结果。
			res, err := a.Reply(ctx, chatMsg(c.Text, "item-eval", "chat-"+c.Name))
			if err != nil {
				// 调用失败直接记为不通过。
				failures = append(failures, fmt.Sprintf("[%s/%s] 调用失败: %v", g.Name, c.Name, err))
			} else {
				// ok、fail 是当前用例的断言结果与失败原因。
				ok, fail := checkAIContentCase(c, rec.system(), res)
				if ok {
					// 按类别累加通过计数。
					switch c.Kind {
					case aiContentKindFAQ:
						m.FAQHit++
					case aiContentKindCatalog:
						m.CatalogRef++
					case aiContentKindPolicy:
						m.PolicyCovered++
					}
				} else {
					failures = append(failures, fmt.Sprintf("[%s/%s] %s", g.Name, c.Name, fail))
				}
			}
			// 按类别累加用例总数。
			switch c.Kind {
			case aiContentKindFAQ:
				m.FAQTotal++
			case aiContentKindCatalog:
				m.CatalogTotal++
			case aiContentKindPolicy:
				m.PolicyTotal++
			}
		}
	}
	return m, failures
}

// aiContentMetrics 是内容层评测的汇总指标。
type aiContentMetrics struct {
	// FAQTotal 与 FAQHit 是 FAQ 命中类用例的总数与通过数（知识注入且回复含答案）。
	FAQTotal, FAQHit int
	// CatalogTotal 与 CatalogRef 是在售清单引用类用例的总数与通过数（回复引用清单条目）。
	CatalogTotal, CatalogRef int
	// PolicyTotal 与 PolicyCovered 是策略话术类用例的总数与通过数（越界报价套用配置话术）。
	PolicyTotal, PolicyCovered int
}

// TestAIContentEvaluation 内容层评测门禁：跑评测集 v1，断言三类指标全部达标。
// 评测集为确定性标注（mock 模型），理论满分为 1.0；阈值按 0.05 容错设置，
// 逐条失败清单负责兜底，防止单条回归被整体平均掩盖。
func TestAIContentEvaluation(t *testing.T) {
	// m、fails 是评测集 v1 的汇总指标与失败用例描述。
	m, fails := runAIContentEvaluation(t, aiContentEvalSetV1)
	// 汇总输出三类指标，供门禁报告引用。
	t.Logf("内容层评测汇总：FAQ 命中率 %.3f（%d/%d）；在售清单引用率 %.3f（%d/%d）；策略话术覆盖率 %.3f（%d/%d）",
		rate(m.FAQHit, m.FAQTotal), m.FAQHit, m.FAQTotal,
		rate(m.CatalogRef, m.CatalogTotal), m.CatalogRef, m.CatalogTotal,
		rate(m.PolicyCovered, m.PolicyTotal), m.PolicyCovered, m.PolicyTotal)
	// fail 表示当前遍历到的失败用例描述。
	for _, fail := range fails {
		t.Logf("评测用例不达标: %s", fail)
	}
	// 门禁阈值：三类指标均不低于 0.95，且不允许任何一条用例失败。
	if rate(m.FAQHit, m.FAQTotal) < 0.95 {
		t.Errorf("FAQ 命中率 %.3f 低于 0.95", rate(m.FAQHit, m.FAQTotal))
	}
	if rate(m.CatalogRef, m.CatalogTotal) < 0.95 {
		t.Errorf("在售清单引用率 %.3f 低于 0.95", rate(m.CatalogRef, m.CatalogTotal))
	}
	if rate(m.PolicyCovered, m.PolicyTotal) < 0.95 {
		t.Errorf("策略话术覆盖率 %.3f 低于 0.95", rate(m.PolicyCovered, m.PolicyTotal))
	}
	if len(fails) > 0 {
		t.Errorf("评测集有 %d 条用例不达标", len(fails))
	}
}
