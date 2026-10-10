// report.go 把评测结果渲染为人类可读的 Markdown 报告与机器可读的指标 JSON。
package evalharness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RenderMarkdown 渲染一次评测的完整报告。
func RenderMarkdown(rep *Report) string {
	// b 是报告文本的拼装缓冲。
	var b strings.Builder
	b.WriteString("# 客服 Agent 发布前评测报告\n\n")
	fmt.Fprintf(&b, "> 评测时间：%s\n", rep.RecordedAt)
	fmt.Fprintf(&b, "> 被测模型：`%s`（配置来源：%s）\n", rep.Model, rep.ModelSource)
	fmt.Fprintf(&b, "> 判定模型：`%s`（温度 0）\n", rep.JudgeModel)
	fmt.Fprintf(&b, "> 每样本采样：%d 次\n", rep.SamplesPerCase)
	fmt.Fprintf(&b, "> 样本：参与 %d 条，跳过 %d 条，判定失败 %d 条\n", rep.Total, rep.Skipped, rep.JudgeFailures)
	fmt.Fprintf(&b, "\n> **合规警示**：本文件含生产对话与模型回复原文，属敏感数据，禁止提交进版本库或外发。\n\n")

	// 门禁结论。
	if rep.GatePassed {
		b.WriteString("## 门禁结论：通过\n\n")
	} else {
		b.WriteString("## 门禁结论：未通过\n\n")
	}
	// reason 是当前遍历到的门禁说明。
	for _, reason := range rep.GateReasons {
		fmt.Fprintf(&b, "- %s\n", reason)
	}
	b.WriteString("\n")

	// 指标总览。
	b.WriteString("## 指标\n\n")
	b.WriteString("| 指标 | 值 |\n|---|---|\n")
	fmt.Fprintf(&b, "| 通过率 | %.1f%%（%d/%d） |\n", rep.PassRate*100, rep.Passed, rep.Total)
	if rep.Baseline != nil {
		fmt.Fprintf(&b, "| 基线通过率 | %.1f%% |\n", rep.Baseline.PassRate*100)
		fmt.Fprintf(&b, "| 变化 | %+.1f 个百分点 |\n", rep.Delta*100)
		fmt.Fprintf(&b, "| 单条回归 | %d 条 |\n", len(rep.Regressions))
		fmt.Fprintf(&b, "| 单条提升 | %d 条 |\n", len(rep.Improvements))
	} else {
		b.WriteString("| 基线 | 无（首次运行） |\n")
	}
	b.WriteString("\n")

	// 分层指标：体现「结构层零成本可进门禁、模型层只做报告」的分层设计。
	if len(rep.ByLayer) > 0 {
		b.WriteString("### 分层结果\n\n")
		b.WriteString("| 判定层 | 通过 | 合计 | 通过率 |\n|---|---|---|---|\n")
		// layer 是当前遍历到的判定层名。
		for _, layer := range sortedLayerKeys(rep.ByLayer) {
			// stat 是该层的统计。
			stat := rep.ByLayer[layer]
			fmt.Fprintf(&b, "| %s | %d | %d | %.1f%% |\n", layer, stat.Passed, stat.Total, rate(stat.Passed, stat.Total)*100)
		}
		b.WriteString("\n")
	}

	// 分类指标：定位是哪类事故在退化。
	if len(rep.ByVerdict) > 0 {
		b.WriteString("### 按事故标签\n\n")
		b.WriteString("| 标签 | 通过 | 合计 | 通过率 |\n|---|---|---|---|\n")
		// verdict 是当前遍历到的事故标签。
		for _, verdict := range sortedVerdictKeys(rep.ByVerdict) {
			// stat 是该标签的统计。
			stat := rep.ByVerdict[verdict]
			fmt.Fprintf(&b, "| %s | %d | %d | %.1f%% |\n", verdict, stat.Passed, stat.Total, rate(stat.Passed, stat.Total)*100)
		}
		b.WriteString("\n")
	}

	// 回归与提升清单。
	if len(rep.Regressions) > 0 {
		b.WriteString("### 较基线退化（需人工确认）\n\n")
		// chat 是当前遍历到的样本标识。
		for _, chat := range rep.Regressions {
			fmt.Fprintf(&b, "- `%s`\n", chat)
		}
		b.WriteString("\n")
	}
	if len(rep.Improvements) > 0 {
		b.WriteString("### 较基线提升\n\n")
		// chat 是当前遍历到的样本标识。
		for _, chat := range rep.Improvements {
			fmt.Fprintf(&b, "- `%s`\n", chat)
		}
		b.WriteString("\n")
	}

	// 逐条明细。
	b.WriteString("## 逐条明细\n\n")
	// caseResult 是当前遍历到的样本结果。
	for index, caseResult := range rep.Cases {
		// status 是该条的结论标签。
		status := "通过"
		if caseResult.JudgeFailed {
			status = "判定失败"
		} else if !caseResult.Pass {
			status = "未通过"
		}
		fmt.Fprintf(&b, "### %d. %s · %s · %s\n\n", index+1, caseResult.Chat, strings.Join(caseResult.Verdicts, ", "), status)
		fmt.Fprintf(&b, "- 判定层：%s\n", caseResult.Layer)
		fmt.Fprintf(&b, "- 理由：%s\n", orDash(caseResult.Reason))
		if caseResult.Reply != "" {
			fmt.Fprintf(&b, "\n模型回复：\n\n> %s\n\n", indentQuote(caseResult.Reply))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// WriteArtifacts 落盘报告与指标，返回两个文件的路径。
func WriteArtifacts(outDir string, rep *Report) (string, string, error) {
	// mkdirErr 是创建输出目录的失败原因。
	if mkdirErr := os.MkdirAll(outDir, 0o755); mkdirErr != nil {
		return "", "", fmt.Errorf("创建输出目录失败: %w", mkdirErr)
	}
	// stamp 是文件名中的时间戳。
	stamp := strings.ReplaceAll(strings.ReplaceAll(rep.RecordedAt, ":", ""), "-", "")
	// mdPath 是 Markdown 报告的落盘路径。
	mdPath := filepath.Join(outDir, "report-"+stamp+".md")
	// jsonPath 是指标 JSON 的落盘路径。
	jsonPath := filepath.Join(outDir, "metrics-"+stamp+".json")

	// mdErr 是报告写出的失败原因。
	if mdErr := os.WriteFile(mdPath, []byte(RenderMarkdown(rep)), 0o644); mdErr != nil {
		return "", "", fmt.Errorf("写入报告失败: %w", mdErr)
	}
	// blob、err 是指标序列化结果与失败原因。
	blob, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("序列化指标失败: %w", err)
	}
	// jsonErr 是指标写出的失败原因。
	if jsonErr := os.WriteFile(jsonPath, blob, 0o644); jsonErr != nil {
		return "", "", fmt.Errorf("写入指标失败: %w", jsonErr)
	}
	return mdPath, jsonPath, nil
}

// rate 计算通过率，分母为零时返回零。
func rate(passed, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(passed) / float64(total)
}

// sortedLayerKeys 返回稳定排序的判定层名，保证报告可复现。
func sortedLayerKeys(stats map[string]LayerStat) []string {
	// keys 是全部判定层名。
	keys := make([]string, 0, len(stats))
	// key 是当前遍历到的层名。
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedVerdictKeys 返回稳定排序的事故标签，保证报告可复现。
func sortedVerdictKeys(stats map[string]VerdictStat) []string {
	// keys 是全部标签名。
	keys := make([]string, 0, len(stats))
	// key 是当前遍历到的标签名。
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// orDash 在值为空时返回占位符。
func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// indentQuote 把多行文本转为 Markdown 引用块，保留换行结构。
func indentQuote(text string) string {
	// lines 是文本按行切分的结果。
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return strings.Join(lines, "\n> ")
}
