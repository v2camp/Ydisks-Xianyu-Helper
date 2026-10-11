// judge.go 对单条样本做判定：优先规则断言，规则不覆盖时回落到 LLM judge。
//
// 判定分层（与 docs/traffic-replay-evaluation-plan.md 的分层门禁对应）：
//   - structure：结构层，只检查重建的 system 提示，确定性、零成本；
//   - rule：规则层，检查模型回复是否满足可机检期望，确定性；
//   - judge：模型层，用判定模型评估回复是否恰当，有噪声与成本。
//
// 敏感约束：判定提示中只出现商品与对话内容，不出现任何密钥。
package evalharness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xianyu-go/internal/engine"
)

// 判定层标识。
const (
	// layerStructure 是结构层：不调用模型，只检查提示词组装结果。
	layerStructure = "structure"
	// layerRule 是规则层：检查回复是否满足可机检期望。
	layerRule = "rule"
	// layerJudge 是模型层：由判定模型评估回复质量。
	layerJudge = "judge"
)

// Verdict 是一条样本的判定结果。
type Verdict struct {
	// Pass 表示判定通过。
	Pass bool
	// Layer 是做出该判定的层。
	Layer string
	// Reason 是判定理由，用于报告与排障。
	Reason string
	// Reply 是被测模型的回复文本；结构层判定时为空。
	Reply string
	// JudgeFailed 表示 LLM judge 未能给出可解析结论，该次判定不计入指标。
	JudgeFailed bool
}

// GeneratorFunc 是模型调用签名，便于在测试中注入替身。
type GeneratorFunc func(ctx context.Context, cfg ModelConfig, system string, history []ConversationTurn, current string) (string, error)

// CallModel 是默认的模型调用实现：复用引擎的 OpenAI 兼容生成器。
// 复用的意义在于评测与生产走同一套客户端构造、消息拼装与超时预算，避免行为漂移。
func CallModel(ctx context.Context, cfg ModelConfig, system string, history []ConversationTurn, current string) (string, error) {
	// turns 是把样本历史转换为引擎请求结构的结果。
	turns := make([]engine.ConversationTurn, 0, len(history))
	// turn 是当前遍历到的历史轮次。
	for _, turn := range history {
		turns = append(turns, engine.ConversationTurn{Role: turn.Role, Content: turn.Content})
	}
	// content、err 是模型回复文本与调用失败原因。
	content, err := engine.NewDefaultAIGenerator().Generate(ctx, engine.GenerateRequest{
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Temperature: cfg.Temperature,
		System:      system,
		History:     turns,
		Current:     current,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(content), nil
}

// CheckPromptStructure 做结构层断言，只检查重建的 system 提示，不调用模型。
// 返回的 ok 为 false 时表示该样本没有结构层断言，应继续走后续层。
func CheckPromptStructure(sample Sample, exp *Expectation) (Verdict, bool) {
	if sample.Snapshot == nil || exp == nil || len(exp.PromptMustContain) == 0 {
		return Verdict{}, false
	}
	// missing 是提示中缺失的必需片段。
	var missing []string
	// want 是当前遍历到的必需片段。
	for _, want := range exp.PromptMustContain {
		if !strings.Contains(sample.Snapshot.SystemPrompt, want) {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return Verdict{
			Pass:   false,
			Layer:  layerStructure,
			Reason: "系统提示缺失必需片段：" + strings.Join(missing, "、"),
		}, true
	}
	return Verdict{Pass: true, Layer: layerStructure, Reason: "结构层断言通过"}, true
}

// DecideByRule 用规则断言判定回复。
// 返回的 ok 为 false 时表示没有可判定的规则，调用方应回落到 LLM judge。
func DecideByRule(exp *Expectation, reply string) (Verdict, bool) {
	if exp == nil {
		return Verdict{}, false
	}
	if len(exp.MustContainAny) == 0 && len(exp.MustNotContain) == 0 {
		return Verdict{}, false
	}
	// banned 是当前遍历到的禁止片段。
	for _, banned := range exp.MustNotContain {
		if strings.Contains(reply, banned) {
			return Verdict{
				Pass:   false,
				Layer:  layerRule,
				Reason: "回复出现禁止话术：" + banned,
				Reply:  reply,
			}, true
		}
	}
	if len(exp.MustContainAny) > 0 {
		// matched 表示回复是否命中任一必需片段。
		matched := false
		// want 是当前遍历到的必需片段。
		for _, want := range exp.MustContainAny {
			if strings.Contains(reply, want) {
				matched = true
				break
			}
		}
		if !matched {
			return Verdict{
				Pass:   false,
				Layer:  layerRule,
				Reason: "回复未给出必需内容，期望其一：" + strings.Join(exp.MustContainAny, "、"),
				Reply:  reply,
			}, true
		}
	}
	return Verdict{Pass: true, Layer: layerRule, Reason: "规则断言通过", Reply: reply}, true
}

// judgeSystemPrompt 是判定模型的系统提示，固定温度为零以保证判定端可复现。
const judgeSystemPrompt = `你是严格的客服回复评审员。判断给定的客服回复是否恰当回答了买家的当前问题。

评判标准：
1. 回复必须正面回答买家问的事，不得答非所问；
2. 若提供了「该商品确定事实」，回复必须与之一致，不得含糊或回避；
3. 不得编造确定事实中没有的信息；
4. 语气友好、简洁，符合卖家身份。

只输出一个 JSON 对象，不要输出其它任何内容：
{"pass": true 或 false, "reason": "一句话理由"}`

// judgeUserPrompt 拼装判定模型的用户输入。
func judgeUserPrompt(sample Sample, reply string) string {
	// b 是判定输入的拼装缓冲。
	var b strings.Builder
	fmt.Fprintf(&b, "买家当前问题：%s\n", sample.Snapshot.MessageText)
	if sample.Snapshot.KnowledgeContext != "" {
		fmt.Fprintf(&b, "\n知识上下文：\n%s\n", sample.Snapshot.KnowledgeContext)
	}
	fmt.Fprintf(&b, "\n客服回复：%s", reply)
	return b.String()
}

// judgeResponse 是判定模型返回的结构化结论。
type judgeResponse struct {
	// Pass 表示判定模型认为回复达标。
	Pass bool `json:"pass"`
	// Reason 是判定模型给出的一句话理由。
	Reason string `json:"reason"`
}

// JudgeByModel 用判定模型评估回复是否恰当。
// 判定模型无法给出可解析结论时，返回 JudgeFailed 为真的结果，该样本不计入指标分母。
func JudgeByModel(ctx context.Context, gen GeneratorFunc, cfg ModelConfig, sample Sample, reply string) Verdict {
	// raw、err 是判定模型输出与调用失败原因。
	raw, err := gen(ctx, cfg, judgeSystemPrompt, nil, judgeUserPrompt(sample, reply))
	if err != nil {
		return Verdict{Pass: false, Layer: layerJudge, Reply: reply, JudgeFailed: true, Reason: "判定模型调用失败: " + err.Error()}
	}
	// payload 是提取出的 JSON 文本。
	payload, ok := extractJSONObject(raw)
	if !ok {
		return Verdict{Pass: false, Layer: layerJudge, Reply: reply, JudgeFailed: true, Reason: "判定模型未返回可解析的 JSON"}
	}
	// parsed 是解析后的判定结论。
	var parsed judgeResponse
	// unmarshalErr 是判定结论的 JSON 解析失败原因。
	if unmarshalErr := json.Unmarshal([]byte(payload), &parsed); unmarshalErr != nil {
		return Verdict{Pass: false, Layer: layerJudge, Reply: reply, JudgeFailed: true, Reason: "判定结论解析失败"}
	}
	return Verdict{Pass: parsed.Pass, Layer: layerJudge, Reply: reply, Reason: strings.TrimSpace(parsed.Reason)}
}

// extractJSONObject 从模型输出中提取第一个完整的 JSON 对象，容忍前后的解释性文字与代码围栏。
func extractJSONObject(raw string) (string, bool) {
	// start 是第一个左花括号的位置。
	start := strings.Index(raw, "{")
	// end 是最后一个右花括号的位置。
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", false
	}
	return raw[start : end+1], true
}
