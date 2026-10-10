// metric.go 汇总评测指标、与基线对照并给出门禁结论。
//
// 门禁原则（与 docs/traffic-replay-evaluation-plan.md §七 一致）：
//   - 总分做门禁：通过率跌破「基线 - 容差」即拦截；
//   - 单条做告警：单条样本由通过转失败只记录并报告，不直接拦截，
//     因为真实模型本身存在噪声，逐条严格判定会产生大量误报。
package evalharness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CaseResult 是一条样本的评测结果。
type CaseResult struct {
	// Chat 是哈希后的会话标识，作为样本唯一键。
	Chat string
	// Verdicts 是该样本的事故标签集合。
	Verdicts []string
	// Pass 表示该样本是否通过。
	Pass bool
	// Layer 是最终生效的判定层。
	Layer string
	// Reason 是判定理由。
	Reason string
	// Reply 是被测模型的回复文本。
	Reply string
	// JudgeFailed 表示判定失败，该样本不计入指标分子分母。
	JudgeFailed bool
}

// VerdictStat 是单个事故标签的统计。
type VerdictStat struct {
	// Total 是该标签下的样本数。
	Total int
	// Passed 是该标签下通过的样本数。
	Passed int
}

// LayerStat 是单个判定层的统计。
type LayerStat struct {
	// Total 是该层做出的判定数。
	Total int
	// Passed 是该层判为通过的数量。
	Passed int
}

// Baseline 是基线文件结构，用于跨版本对照。
type Baseline struct {
	// RecordedAt 是基线记录时间。
	RecordedAt string `json:"recorded_at"`
	// Model 是记录基线时使用的模型标识。
	Model string `json:"model"`
	// Total 是参与统计的样本数。
	Total int `json:"total"`
	// PassRate 是基线通过率。
	PassRate float64 `json:"pass_rate"`
	// Cases 是每条样本的通过状态，用于识别单条回归。
	Cases map[string]bool `json:"cases"`
	// SamplesPerCase 是记录基线时的采样次数。
	SamplesPerCase int `json:"samples_per_case"`
	// Tolerance 是记录基线时的劣化容差。
	Tolerance float64 `json:"tolerance"`
}

// Report 是一次评测的完整结果。
type Report struct {
	// Model 是被测模型标识。
	Model string
	// JudgeModel 是判定模型标识。
	JudgeModel string
	// ModelSource 是被测模型的配置来源。
	ModelSource string
	// RecordedAt 是本次评测时间。
	RecordedAt string
	// SamplesPerCase 是每个样本的采样次数。
	SamplesPerCase int
	// Total 是参与统计的样本数。
	Total int
	// Skipped 是因不可回放而跳过的样本数。
	Skipped int
	// JudgeFailures 是判定失败的样本数。
	JudgeFailures int
	// Passed 是通过的样本数。
	Passed int
	// PassRate 是通过率，区间 0 到 1。
	PassRate float64
	// ByVerdict 是各事故标签的通过情况。
	ByVerdict map[string]VerdictStat
	// ByLayer 是各判定层的命中与通过情况。
	ByLayer map[string]LayerStat
	// Cases 是逐条评测结果，按会话标识排序。
	Cases []CaseResult
	// Baseline 是加载到的基线；首次运行为 nil。
	Baseline *Baseline
	// Delta 是通过率相对基线的变化；无基线时为 0。
	Delta float64
	// Regressions 是基线通过而本次失败的样本标识。
	Regressions []string
	// Improvements 是基线失败而本次通过的样本标识。
	Improvements []string
	// GatePassed 是门禁结论。
	GatePassed bool
	// GateReasons 是门禁判定说明。
	GateReasons []string
}

// ComputeReport 汇总逐条结果并与基线对照。
// baseline 为 nil 时表示首次建立基线，门禁默认通过并在说明中标注。
func ComputeReport(cases []CaseResult, baseline *Baseline, model, judgeModel, modelSource string, samplesPerCase, skipped int, tolerance float64) *Report {
	// rep 是逐步填充的报告。
	rep := &Report{
		Model:          model,
		JudgeModel:     judgeModel,
		ModelSource:    modelSource,
		RecordedAt:     time.Now().Format(time.RFC3339),
		SamplesPerCase: samplesPerCase,
		Skipped:        skipped,
		ByVerdict:      map[string]VerdictStat{},
		ByLayer:        map[string]LayerStat{},
		Cases:          cases,
		Baseline:       baseline,
	}
	// caseResult 是当前遍历到的样本结果。
	for _, caseResult := range cases {
		if caseResult.JudgeFailed {
			rep.JudgeFailures++
			continue
		}
		rep.Total++
		if caseResult.Pass {
			rep.Passed++
		}
		// verdict 是当前遍历到的事故标签。
		for _, verdict := range caseResult.Verdicts {
			// stat 是该标签的累计统计。
			stat := rep.ByVerdict[verdict]
			stat.Total++
			if caseResult.Pass {
				stat.Passed++
			}
			rep.ByVerdict[verdict] = stat
		}
		// layerStat 是判定层的累计统计。
		layerStat := rep.ByLayer[caseResult.Layer]
		layerStat.Total++
		if caseResult.Pass {
			layerStat.Passed++
		}
		rep.ByLayer[caseResult.Layer] = layerStat
	}
	if rep.Total > 0 {
		rep.PassRate = float64(rep.Passed) / float64(rep.Total)
	}
	rep.evaluateGate(tolerance)
	return rep
}

// evaluateGate 依据基线与容差给出门禁结论。
// 总分跌破容差即拦截；单条回归只记录为告警，不单独拦截。
func (r *Report) evaluateGate(tolerance float64) {
	if r.Baseline == nil {
		r.GatePassed = true
		r.GateReasons = append(r.GateReasons, "未找到基线，本次结果可作为新基线记录")
		return
	}
	// floor 是允许的最低通过率。
	floor := r.Baseline.PassRate - tolerance
	r.Delta = r.PassRate - r.Baseline.PassRate

	// chat、pass 是基线中当前遍历到的样本通过状态。
	for chat, pass := range r.Baseline.Cases {
		// now 是本次该样本的通过状态；本次未覆盖的样本不参与回归判定。
		now, covered := r.lookup(chat)
		if !covered {
			continue
		}
		if pass && !now {
			r.Regressions = append(r.Regressions, chat)
		}
		if !pass && now {
			r.Improvements = append(r.Improvements, chat)
		}
	}
	sort.Strings(r.Regressions)
	sort.Strings(r.Improvements)

	if r.PassRate+1e-9 < floor {
		r.GatePassed = false
		r.GateReasons = append(r.GateReasons, fmt.Sprintf(
			"通过率 %.1f%% 低于基线 %.1f%% 减去容差 %.1f 个百分点后的下限 %.1f%%",
			r.PassRate*100, r.Baseline.PassRate*100, tolerance*100, floor*100))
	} else {
		r.GatePassed = true
		r.GateReasons = append(r.GateReasons, fmt.Sprintf(
			"通过率 %.1f%% 未跌破下限 %.1f%%（基线 %.1f%%，容差 %.1f 个百分点）",
			r.PassRate*100, floor*100, r.Baseline.PassRate*100, tolerance*100))
	}
	if len(r.Regressions) > 0 {
		r.GateReasons = append(r.GateReasons, fmt.Sprintf(
			"告警：%d 条样本较基线退化，按设计不单独拦截，请人工确认", len(r.Regressions)))
	}
	if r.JudgeFailures > 0 {
		r.GateReasons = append(r.GateReasons, fmt.Sprintf(
			"告警：%d 条样本判定失败，未计入分母，请检查判定模型输出", r.JudgeFailures))
	}
}

// lookup 查询本次评测中指定样本的通过状态。
func (r *Report) lookup(chat string) (bool, bool) {
	// caseResult 是当前遍历到的样本结果。
	for _, caseResult := range r.Cases {
		if caseResult.Chat == chat {
			return caseResult.Pass, true
		}
	}
	return false, false
}

// ToBaseline 把报告转换为可落盘的基线。
func (r *Report) ToBaseline(tolerance float64) *Baseline {
	// cases 是每条样本的通过状态。
	cases := make(map[string]bool, len(r.Cases))
	// caseResult 是当前遍历到的样本结果。
	for _, caseResult := range r.Cases {
		if caseResult.JudgeFailed {
			continue
		}
		cases[caseResult.Chat] = caseResult.Pass
	}
	return &Baseline{
		RecordedAt:     r.RecordedAt,
		Model:          r.Model,
		Total:          r.Total,
		PassRate:       r.PassRate,
		Cases:          cases,
		SamplesPerCase: r.SamplesPerCase,
		Tolerance:      tolerance,
	}
}

// LoadBaseline 读取基线文件；文件不存在时返回 nil 且不报错。
func LoadBaseline(path string) (*Baseline, error) {
	// data、err 是基线文件内容与读取失败原因。
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取基线失败: %w", err)
	}
	// baseline 是解析出的基线。
	var baseline Baseline
	// unmarshalErr 是基线文件的 JSON 解析失败原因。
	if unmarshalErr := json.Unmarshal(data, &baseline); unmarshalErr != nil {
		return nil, fmt.Errorf("解析基线失败: %w", unmarshalErr)
	}
	return &baseline, nil
}

// SaveBaseline 把基线写入指定路径，父目录不存在时自动创建。
func SaveBaseline(path string, baseline *Baseline) error {
	// dir 是基线文件的父目录。
	dir := filepath.Dir(path)
	// mkdirErr 是创建目录的失败原因。
	if mkdirErr := os.MkdirAll(dir, 0o755); mkdirErr != nil {
		return fmt.Errorf("创建基线目录失败: %w", mkdirErr)
	}
	// blob、err 是序列化结果与失败原因。
	blob, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化基线失败: %w", err)
	}
	// writeErr 是写入文件的失败原因。
	if writeErr := os.WriteFile(path, blob, 0o644); writeErr != nil {
		return fmt.Errorf("写入基线失败: %w", writeErr)
	}
	return nil
}
