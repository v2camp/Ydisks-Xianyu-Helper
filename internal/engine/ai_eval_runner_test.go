// ai_eval_runner_test.go 决策层评测 runner 与门禁测试：读取评测集 v1，沿生产判定链
// （loadScope → decide → loadComplaintKeywords → intentBlockedByExtra → primaryIntentLabel）
// 逐条产出「接管/意图/负向拦截」三要素，汇总五类指标并作为默认门禁。
// runner 只复用现有公开判定入口，不修改 ai_scope.go / ai.go / aimatch 的任何逻辑。
package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// aiEvalMetrics 是决策层评测的汇总指标。
type aiEvalMetrics struct {
	// Total 是用例总数。
	Total int
	// TakeoverAccuracy 是接管准确率：接管决策与标注一致的比例。
	TakeoverAccuracy float64
	// NegativeBlockRate 是负向拦截率：标注负向的用例被拦截的比例。
	NegativeBlockRate float64
	// IntentAccuracy 是意图分类正确率：意图标签与标注一致的比例。
	IntentAccuracy float64
	// FalsePositiveRate 是误伤率：标注不接管的用例被误抢接管的占比。
	FalsePositiveRate float64
	// SpecialSemanticsRate 是特殊语义保持率：特殊语义标注用例按预期保持的比例。
	SpecialSemanticsRate float64
}

// aiEvalCaseResult 是单条用例沿判定链得到的实际结果，供指标对照与特殊语义检查使用。
type aiEvalCaseResult struct {
	// gotTakeover 表示最终是否允许 AI 接管（负向或扩展词表任一命中即不接管）。
	gotTakeover bool
	// gotIntent 表示实际意图标签；被负向拦截时统一为 complaint。
	gotIntent string
	// gotNegativeBlocked 表示实际是否被负向拦截（内置/配置/扩展词表任一命中）。
	gotNegativeBlocked bool
	// hitIDs 是边界判定的命中意图集合；负向否决或零命中时为空。
	hitIDs []string
}

// aiEvalReplier 构造带指定边界配置与扩展负向词表的 AI 实现，返回清理函数。
func aiEvalReplier(t *testing.T, scopeJSON, complaintKeywords string) (*AIReplierImpl, func()) {
	t.Helper()
	// s、cleanup 是复用 scopeStore 构造的临时 SQLite store。
	s, cleanup := scopeStore(t, scopeJSON, "", "")
	// ctx 用于写入扩展负向词表设置。
	ctx := context.Background()
	if strings.TrimSpace(complaintKeywords) != "" {
		// err 是写入 ai_complaint_keywords 设置的数据库错误。
		if err := s.Settings.Set(ctx, "ai_complaint_keywords", complaintKeywords); err != nil {
			t.Fatalf("写入扩展负向词表失败: %v", err)
		}
	}
	return NewAIReplier("cid", s, nil), cleanup
}

// decideEvalCase 沿生产判定链计算单条文本的实际决策结果。
// 判定顺序与生产 Reply 一致：先负向（内置/配置）否决，再按意图白名单命中，最后叠加扩展负向词表。
func decideEvalCase(scope *aiScope, extra []*regexp.Regexp, text string) aiEvalCaseResult {
	// negHit 表示内置/配置负向组合词命中。
	negHit := scope.negative(text)
	// extraHit 表示扩展负向词表命中。
	extraHit := intentBlockedByExtra(extra, text)
	// takeover、hitIDs 是边界判定结果；负向命中时接管为假且命中集为空。
	takeover, hitIDs := scope.decide(text)
	// gotTakeover 与生产「负向或扩展词表任一命中即不接管」的条件保持一致。
	gotTakeover := takeover && !extraHit
	// gotNegativeBlocked 表示该条被负向语义拦截。
	gotNegativeBlocked := negHit || extraHit
	// gotIntent 是意图标签；被负向拦截时统一归入 complaint 意图。
	gotIntent := primaryIntentLabel(hitIDs)
	if gotNegativeBlocked {
		gotIntent = IntentComplaint
	}
	return aiEvalCaseResult{
		gotTakeover:        gotTakeover,
		gotIntent:          gotIntent,
		gotNegativeBlocked: gotNegativeBlocked,
		hitIDs:             hitIDs,
	}
}

// checkEvalSpecial 按用例标注执行对应的特殊语义检查；未标注时视为通过。
func checkEvalSpecial(c aiEvalCase, r aiEvalCaseResult) bool {
	switch c.Special {
	case aiEvalSpecialLabelOnly:
		// ai=false 意图应命中标注但不接管。
		return len(r.hitIDs) > 0 && !r.gotTakeover
	case aiEvalSpecialMergeAmbiguous:
		// 多意图命中应归并为 ambiguous，且未被负向拦截干扰。
		return !r.gotNegativeBlocked && r.gotIntent == IntentAmbiguous
	case aiEvalSpecialExtraKeywords:
		// 扩展负向词表应只拦截标注为负向的用例，不改变其它用例行为。
		return r.gotNegativeBlocked == c.WantNegative
	case aiEvalSpecialRealPhrase:
		// 真实口语负向话术必须被拦截：负向命中与标注一致才算保持。
		return r.gotNegativeBlocked == c.WantNegative
	default:
		return true
	}
}

// rate 计算分子/分母的比例；分母为零时返回 0 避免除零。
func rate(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

// runAIDecisionEvaluation 遍历评测集分组，沿生产判定链产出每条用例的决策结果并汇总五类指标。
// 返回汇总指标与失败用例描述列表；失败描述包含分组/用例名与期望实际对照。
func runAIDecisionEvaluation(t *testing.T, groups []aiEvalGroup) (aiEvalMetrics, []string) {
	t.Helper()
	// 五类指标的计数：接管准确/负向拦截/意图正确/误伤/特殊语义保持。
	var (
		takeoverTotal, takeoverCorrect int
		negTotal, negCaught            int
		intentTotal, intentCorrect     int
		nonTakeoverTotal, fpWrong      int
		specialTotal, specialOKTotal   int
	)
	// failures 是期望与实际不一致的用例描述。
	var failures []string
	// group 表示当前遍历到的配置场景。
	for _, group := range groups {
		// a、cleanup 是当前分组的 AI 实现。
		a, cleanup := aiEvalReplier(t, group.ScopeConfig, group.ComplaintKeywords)
		// ctx 是当前分组共享的判定上下文。
		ctx := context.Background()
		// scope、err 是当前分组的编译后边界；未配置或配置非法时回落内置。
		scope, err := a.loadScope(ctx)
		if err != nil {
			t.Fatalf("加载边界配置失败[%s]: %v", group.Name, err)
		}
		// extra、kwErr 是扩展负向词表；读取失败时返回空列表并记告警。
		extra, kwErr := a.loadComplaintKeywords(ctx)
		if kwErr != nil {
			t.Fatalf("读取扩展负向词表失败[%s]: %v", group.Name, kwErr)
		}
		// groupOK 是当前分组内三要素与特殊语义全部一致的用例数，用于分组日志。
		groupOK := 0
		// c 表示当前遍历到的评测用例。
		for _, c := range group.Cases {
			// result 是当前用例沿判定链得到的实际结果。
			result := decideEvalCase(scope, extra, c.Text)
			// specialOK 是当前用例的特殊语义检查结果。
			specialOK := checkEvalSpecial(c, result)
			// record 表示接管/负向/意图三要素与特殊语义是否全部符合标注。
			record := result.gotTakeover == c.WantTakeover &&
				result.gotNegativeBlocked == c.WantNegative &&
				result.gotIntent == c.WantIntent && specialOK
			if record {
				groupOK++
			} else {
				// fail 是期望与实际对照的失败描述。
				fail := fmt.Sprintf("[%s/%s] 文本=%q 期望(接管=%v 意图=%s 负向=%v) 实际(接管=%v 意图=%s 负向=%v)",
					group.Name, c.Name, c.Text, c.WantTakeover, c.WantIntent, c.WantNegative,
					result.gotTakeover, result.gotIntent, result.gotNegativeBlocked)
				if c.Special != "" && !specialOK {
					fail += fmt.Sprintf(" 特殊语义[%s]未保持", c.Special)
				}
				failures = append(failures, fail)
			}
			// 各指标计数累加。
			takeoverTotal++
			if result.gotTakeover == c.WantTakeover {
				takeoverCorrect++
			}
			if c.WantNegative {
				negTotal++
				if result.gotNegativeBlocked {
					negCaught++
				}
			}
			intentTotal++
			if result.gotIntent == c.WantIntent {
				intentCorrect++
			}
			if !c.WantTakeover {
				nonTakeoverTotal++
				if result.gotTakeover {
					fpWrong++
				}
			}
			if c.Special != "" {
				specialTotal++
				if specialOK {
					specialOKTotal++
				}
			}
		}
		t.Logf("评测分组[%s]：用例 %d 条，一致 %d 条", group.Name, len(group.Cases), groupOK)
		cleanup()
	}
	// m 是本次评测的汇总指标。
	m := aiEvalMetrics{
		Total:                takeoverTotal,
		TakeoverAccuracy:     rate(takeoverCorrect, takeoverTotal),
		NegativeBlockRate:    rate(negCaught, negTotal),
		IntentAccuracy:       rate(intentCorrect, intentTotal),
		FalsePositiveRate:    rate(fpWrong, nonTakeoverTotal),
		SpecialSemanticsRate: rate(specialOKTotal, specialTotal),
	}
	return m, failures
}

// TestAIDecisionEvaluation 决策层评测门禁：跑评测集 v1，断言五类指标达标。
// 评测集为当前判定链的确定性标注，理论满分为 1.0；阈值按 0.05 容错设置，
// 逐条失败清单负责兜底，防止单条回归被整体平均掩盖。
func TestAIDecisionEvaluation(t *testing.T) {
	// m、fails 是评测集 v1 的汇总指标与失败用例描述。
	m, fails := runAIDecisionEvaluation(t, aiEvalSetV1)
	// 汇总输出为可读的测试日志，供门禁报告引用。
	t.Logf("决策层评测汇总：用例 %d 条；接管准确率 %.3f；负向拦截率 %.3f；意图分类正确率 %.3f；误伤率 %.3f；特殊语义保持率 %.3f",
		m.Total, m.TakeoverAccuracy, m.NegativeBlockRate, m.IntentAccuracy, m.FalsePositiveRate, m.SpecialSemanticsRate)
	// fail 表示当前遍历到的失败用例描述。
	for _, fail := range fails {
		t.Logf("评测用例不一致: %s", fail)
	}
	// 门禁阈值：接管准确率/负向拦截率/意图分类正确率/特殊语义保持率不低于 0.95，误伤率不高于 0.05。
	if m.TakeoverAccuracy < 0.95 {
		t.Errorf("接管准确率 %.3f 低于 0.95", m.TakeoverAccuracy)
	}
	if m.NegativeBlockRate < 0.95 {
		t.Errorf("负向拦截率 %.3f 低于 0.95", m.NegativeBlockRate)
	}
	if m.IntentAccuracy < 0.95 {
		t.Errorf("意图分类正确率 %.3f 低于 0.95", m.IntentAccuracy)
	}
	if m.FalsePositiveRate > 0.05 {
		t.Errorf("误伤率 %.3f 高于 0.05", m.FalsePositiveRate)
	}
	if m.SpecialSemanticsRate < 0.95 {
		t.Errorf("特殊语义保持率 %.3f 低于 0.95", m.SpecialSemanticsRate)
	}
	if len(fails) > 0 {
		t.Errorf("评测集有 %d 条用例与标注不一致", len(fails))
	}
}
