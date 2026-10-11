// runner.go 逐条回放样本并产出判定结果。
//
// 回放输入完全取自样本快照（system 提示 + 历史 + 当前消息），
// 因此同一份样本在修复前后、任何人运行，输入都是可比的。
package evalharness

import (
	"context"
	"strings"
)

// RunOptions 是一次回放运行的选项。
type RunOptions struct {
	// SamplesPerCase 是每个样本的采样次数，取多数以抑制模型非确定性。
	SamplesPerCase int
	// DryRun 为真时不调用被测模型，改用样本中的历史真实回复做链路自检。
	DryRun bool
	// Limit 限制参与回放的样本数；小于等于 0 表示不限制。
	Limit int
}

// Run 回放全部样本并返回逐条结果。
// gen 为 nil 时使用默认的引擎调用实现；dryRun 下不会触发任何模型调用。
func Run(ctx context.Context, samples []Sample, gen GeneratorFunc, genCfg, judgeCfg ModelConfig, opts RunOptions) []CaseResult {
	if gen == nil {
		gen = CallModel
	}
	// count 是本次实际回放的样本数。
	count := len(samples)
	if opts.Limit > 0 && opts.Limit < count {
		count = opts.Limit
	}
	// perCase 是每个样本的采样次数，至少为 1。
	perCase := opts.SamplesPerCase
	if perCase < 1 {
		perCase = 1
	}
	// results 是逐条回放结果。
	results := make([]CaseResult, 0, count)
	// sample 是当前遍历到的样本。
	for _, sample := range samples[:count] {
		results = append(results, evalOne(ctx, sample, gen, genCfg, judgeCfg, perCase, opts.DryRun))
	}
	return results
}

// evalOne 评测单条样本：先结构层，再取回复走规则层或模型层。
func evalOne(ctx context.Context, sample Sample, gen GeneratorFunc, genCfg, judgeCfg ModelConfig, perCase int, dryRun bool) CaseResult {
	// exp 是该样本的判定期望：人工标注优先，缺失时自动推导。
	exp := sample.ResolveExpectation()
	// result 是逐步填充的评测结果。
	result := CaseResult{Chat: sample.Chat, Verdicts: sample.Verdicts}

	// 决策层：显式声明是否应接管时，只校验接管结论，全程不调用模型。
	if exp != nil && exp.MustNotTakeover != nil {
		// want 是期望的接管状态；MustNotTakeover 为真表示期望不接管，故取反。
		want := !*exp.MustNotTakeover
		// got 是生产判定链对该消息的实际接管状态。
		got := sample.Snapshot != nil && sample.Snapshot.Takeover
		result.Layer = layerDecision
		result.Pass = want == got
		result.Reason = takeoverReason(want, got)
		return result
	}

	// 结构层优先：提示词组装本身不对时，模型再强也不该判通过。
	if verdict, ok := CheckPromptStructure(sample, exp); ok {
		result.Layer = verdict.Layer
		result.Reason = verdict.Reason
		result.Pass = verdict.Pass
		if !verdict.Pass {
			return result
		}
	}

	// 自检模式：用样本中的历史真实回复跑判定链路，全程不调用模型。
	if dryRun {
		// reply 是样本中最后一条 AI 回复。
		reply := historicalReply(sample)
		if reply == "" {
			result.Layer = layerRule
			result.JudgeFailed = true
			result.Reason = "自检模式：样本没有历史 AI 回复可用"
			return result
		}
		// verdict、ok 是规则判定结果与是否存在可机检期望；自检模式全程不调用判定模型。
		if verdict, ok := DecideByRule(exp, reply); ok {
			verdict.Reply = reply
			applyVerdict(&result, verdict)
			result.Layer = layerRule + "（自检）"
			return result
		}
		result.Layer = layerRule
		result.JudgeFailed = true
		result.Reason = "自检模式：该样本没有可机检期望，需人工标注期望或改用真实判定模型"
		return result
	}

	// 采样 perCase 次：真实模型非确定性，取多数可抑制噪声。
	verdicts := make([]Verdict, 0, perCase)
	// i 是当前采样轮次，无业务含义。
	for i := 0; i < perCase; i++ {
		// reply、err 是被测模型本次回复与调用失败原因。
		reply, err := gen(ctx, genCfg, sample.Snapshot.SystemPrompt, sample.Snapshot.History, sample.Snapshot.MessageText)
		if err != nil {
			verdicts = append(verdicts, Verdict{Layer: layerJudge, JudgeFailed: true, Reason: "被测模型调用失败: " + err.Error()})
			continue
		}
		verdicts = append(verdicts, judgeReply(ctx, exp, sample, reply, gen, judgeCfg))
	}
	aggregate(&result, verdicts)
	return result
}

// judgeReply 对一条回复做判定：规则可判时用规则，否则回落到判定模型。
func judgeReply(ctx context.Context, exp *Expectation, sample Sample, reply string, gen GeneratorFunc, judgeCfg ModelConfig) Verdict {
	// verdict、ok 是规则判定结果与规则是否可判。
	if verdict, ok := DecideByRule(exp, reply); ok {
		return verdict
	}
	return JudgeByModel(ctx, gen, judgeCfg, exp, sample, reply)
}

// takeoverReason 说明接管判定与期望的差异，供报告与排障使用。
// want 是期望的接管状态，got 是生产判定链的实际结果。
func takeoverReason(want, got bool) string {
	switch {
	case want == got:
		return "接管判定符合期望"
	case want:
		return "期望 AI 接管，但生产判定链判定为不接管"
	default:
		return "期望不接管（转人工或人工处理），但生产判定链判定为接管"
	}
}

// aggregate 把多次采样结果按多数表决聚合到一条结果上。
// 有效样本为零（全部判定失败或调用失败）时标记为判定失败，不计入指标分母。
func aggregate(result *CaseResult, verdicts []Verdict) {
	// valid 是判定有效（非判定失败）的采样次数。
	valid := 0
	// passed 是判为通过的采样次数。
	passed := 0
	// reasons 是各次判定的理由，用于展示分歧。
	reasons := make([]string, 0, len(verdicts))
	// lastReply 是最后一次可用的模型回复。
	lastReply := ""
	// verdict 是当前遍历到的采样判定。
	for _, verdict := range verdicts {
		if verdict.Reply != "" {
			lastReply = verdict.Reply
		}
		reasons = append(reasons, verdict.Reason)
		if verdict.JudgeFailed {
			continue
		}
		valid++
		if verdict.Pass {
			passed++
		}
	}
	result.Reply = lastReply
	result.Layer = dominantLayer(verdicts)
	if valid == 0 {
		result.JudgeFailed = true
		result.Reason = "全部采样判定失败：" + strings.Join(reasons, "；")
		return
	}
	result.Pass = passed*2 > valid
	result.Reason = strings.Join(reasons, "；")
	if valid < len(verdicts) {
		result.Reason = "部分采样判定失败；" + result.Reason
	}
}

// dominantLayer 返回本次采样中实际生效的判定层：规则可判时不应记为模型层。
func dominantLayer(verdicts []Verdict) string {
	// verdict 是当前遍历到的判定。
	for _, verdict := range verdicts {
		if !verdict.JudgeFailed {
			return verdict.Layer
		}
	}
	return layerJudge
}

// applyVerdict 把单次判定写入结果。
func applyVerdict(result *CaseResult, verdict Verdict) {
	result.Pass = verdict.Pass
	result.Layer = verdict.Layer
	result.JudgeFailed = verdict.JudgeFailed
	result.Reason = verdict.Reason
	result.Reply = verdict.Reply
}

// historicalReply 返回样本中最后一条 AI 助手回复，用于无模型自检。
func historicalReply(sample Sample) string {
	// i 是遍历下标，从后向前找最近一条 AI 回复。
	for i := len(sample.AITurns) - 1; i >= 0; i-- {
		if sample.AITurns[i].Role == "assistant" && strings.TrimSpace(sample.AITurns[i].Content) != "" {
			return strings.TrimSpace(sample.AITurns[i].Content)
		}
	}
	return ""
}
