// evalharness_test.go 覆盖评测核心链路：.env 解析、期望推导、判定分层、指标与门禁。
// 全部用例确定性、零网络、零账号。
package evalharness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadDotenv 解析含注释、export 前缀与引号的环境文件。
func TestLoadDotenv(t *testing.T) {
	// dir 是临时目录。
	dir := t.TempDir()
	// content 是被写入的环境文件内容。
	content := "# 注释行\nEVAL_MODEL=deepseek-flash\nexport EVAL_BASE_URL=\"https://api.example.com/v1\"\nEVAL_API_KEY='sk-test'\n\nBAD_LINE\n"
	// writeErr 是写入临时环境文件的失败原因。
	if writeErr := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); writeErr != nil {
		t.Fatalf("写入临时 .env 失败: %v", writeErr)
	}
	// values、err 是解析结果与失败原因。
	values, err := LoadDotenv(dir)
	if err != nil {
		t.Fatalf("LoadDotenv: %v", err)
	}
	if values["EVAL_MODEL"] != "deepseek-flash" {
		t.Fatalf("EVAL_MODEL 解析错误: %q", values["EVAL_MODEL"])
	}
	if values["EVAL_BASE_URL"] != "https://api.example.com/v1" {
		t.Fatalf("双引号应被去掉: %q", values["EVAL_BASE_URL"])
	}
	if values["EVAL_API_KEY"] != "sk-test" {
		t.Fatalf("单引号应被去掉: %q", values["EVAL_API_KEY"])
	}
	// ok 表示该键是否存在于解析结果中。
	if _, ok := values["BAD_LINE"]; ok {
		t.Fatalf("无等号的行不应被解析")
	}
}

// TestLoadDotenvMissingFile 文件缺失时返回空映射且不报错。
func TestLoadDotenvMissingFile(t *testing.T) {
	// values、err 是解析结果与失败原因。
	values, err := LoadDotenv(t.TempDir())
	if err != nil {
		t.Fatalf("文件缺失不应报错: %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("文件缺失应返回空映射: %v", values)
	}
}

// evalSampleWithPrompt 构造一条带指定系统提示与历史回复的样本。
func evalSampleWithPrompt(prompt, historyReply string) Sample {
	return Sample{
		Chat:     "chat-1",
		Verdicts: []string{"delivery-deflection"},
		Snapshot: &Snapshot{
			SystemPrompt: prompt,
			MessageText:  "有度盘的吗",
			Model:        "deepseek-flash",
		},
		AITurns: []AITurn{{Role: "assistant", Content: historyReply}},
	}
}

// TestDeriveExpectationFromPrompt 从提示中的确定渠道声明推导可机检期望。
func TestDeriveExpectationFromPrompt(t *testing.T) {
	// sample 是含确定发货渠道声明的样本。
	sample := evalSampleWithPrompt("……【该商品确定发货方式】百度网盘、夸克网盘。以上是该商品的确定事实……", "")
	// exp 是推导出的期望。
	exp := sample.ResolveExpectation()
	if exp == nil {
		t.Fatal("应能推导出期望")
	}
	if !exp.Derived {
		t.Fatalf("推导出的期望应带 Derived 标记")
	}
	if len(exp.MustContainAny) != 2 || exp.MustContainAny[0] != "百度网盘" || exp.MustContainAny[1] != "夸克网盘" {
		t.Fatalf("渠道解析错误: %v", exp.MustContainAny)
	}
	if len(exp.MustNotContain) == 0 {
		t.Fatalf("应包含禁止回避话术")
	}
}

// TestDeriveExpectationNoChannel 无渠道声明时不推导，交由 LLM judge。
func TestDeriveExpectationNoChannel(t *testing.T) {
	// sample 是只含通用说明的样本。
	sample := evalSampleWithPrompt("你是闲鱼卖家的自动回复助手。", "")
	// exp 是推导结果，无渠道声明时应为空。
	if exp := sample.ResolveExpectation(); exp != nil {
		t.Fatalf("无确定渠道时不应推导期望: %+v", exp)
	}
}

// TestDecideByRule 规则判定：命中、缺渠道、出现禁止话术三种结果。
func TestDecideByRule(t *testing.T) {
	// exp 是待测期望。
	exp := &Expectation{MustContainAny: []string{"百度网盘", "夸克网盘"}, MustNotContain: []string{"以商品详情为准"}}

	// hit 是给出渠道的回复。
	hit, ok := DecideByRule(exp, "这款发百度网盘，可以直接拍～")
	if !ok || !hit.Pass {
		t.Fatalf("给出渠道应判通过: ok=%v verdict=%+v", ok, hit)
	}

	// banned 是出现禁止话术的回复，应优先命中禁止分支。
	banned, ok := DecideByRule(exp, "具体以商品详情为准哈")
	if !ok || banned.Pass {
		t.Fatalf("出现禁止话术应判失败: ok=%v verdict=%+v", ok, banned)
	}
	if !strings.Contains(banned.Reason, "禁止话术") {
		t.Fatalf("应命中禁止话术分支: %s", banned.Reason)
	}

	// missed 是未给出渠道、也没踩禁止话术的回复。
	missed, ok := DecideByRule(exp, "可以的，直接拍下就行")
	if !ok || missed.Pass {
		t.Fatalf("未给出渠道应判失败: ok=%v verdict=%+v", ok, missed)
	}
	if !strings.Contains(missed.Reason, "必需内容") {
		t.Fatalf("应命中缺失渠道分支: %s", missed.Reason)
	}

	// noRule 是空期望。
	if _, ok := DecideByRule(&Expectation{}, "任意回复"); ok {
		t.Fatal("空期望不应产出规则判定")
	}
}

// TestCheckPromptStructure 结构层断言只在片段缺失时失败。
func TestCheckPromptStructure(t *testing.T) {
	// sample 是系统提示缺少必需片段的样本。
	sample := evalSampleWithPrompt("普通提示，没有优先级声明", "")
	sample.Expectation = &Expectation{PromptMustContain: []string{"优先于店铺通用说明"}}
	// verdict、ok 是结构层判定结果与是否执行。
	verdict, ok := CheckPromptStructure(sample, sample.Expectation)
	if !ok || verdict.Pass {
		t.Fatalf("缺少必需片段应判失败: ok=%v verdict=%+v", ok, verdict)
	}
	if verdict.Layer != layerStructure {
		t.Fatalf("判定层应为结构层: %s", verdict.Layer)
	}
}

// TestExtractJSONObject 容忍代码围栏与解释性文字。
func TestExtractJSONObject(t *testing.T) {
	// payload、ok 是提取结果。
	payload, ok := extractJSONObject("判定如下：\n```json\n{\"pass\": true, \"reason\": \"到位\"}\n```")
	if !ok {
		t.Fatal("应能提取出 JSON 对象")
	}
	if !strings.Contains(payload, "\"pass\"") {
		t.Fatalf("提取结果错误: %s", payload)
	}
	// ok 表示是否提取成功，无 JSON 时应为假。
	if _, ok := extractJSONObject("没有 JSON"); ok {
		t.Fatal("无 JSON 时应返回 false")
	}
}

// TestRunDryRunUsesHistoricalReply 自检模式用历史回复判定，不调用模型。
func TestRunDryRunUsesHistoricalReply(t *testing.T) {
	// sample 的历史回复是回避话术，应被判失败。
	sample := evalSampleWithPrompt(
		"……【该商品确定发货方式】百度网盘、夸克网盘。以上是该商品的确定事实……",
		"具体以商品详情和拍下后的发货消息为准哈")
	// results 是自检回放结果。
	results := Run(context.Background(), []Sample{sample}, nil, ModelConfig{}, ModelConfig{}, RunOptions{DryRun: true})
	if len(results) != 1 {
		t.Fatalf("应产出 1 条结果: %d", len(results))
	}
	if results[0].Pass {
		t.Fatalf("回避回复应判失败: %+v", results[0])
	}
	if !strings.Contains(results[0].Layer, "自检") {
		t.Fatalf("自检模式应标注判定层: %s", results[0].Layer)
	}
}

// TestComputeReportGateNoBaseline 首次运行无基线时门禁通过并提示记录基线。
func TestComputeReportGateNoBaseline(t *testing.T) {
	// cases 是两条全通过的样本。
	cases := []CaseResult{{Chat: "a", Pass: true, Layer: layerRule, Verdicts: []string{"delivery-deflection"}}}
	// rep 是无基线时的报告。
	rep := ComputeReport(cases, nil, "m", "j", "env", 1, 0, 0.05)
	if !rep.GatePassed {
		t.Fatalf("无基线应默认通过: %v", rep.GateReasons)
	}
	if rep.PassRate != 1 {
		t.Fatalf("通过率应为 1: %v", rep.PassRate)
	}
}

// TestComputeReportGateBlocksRegression 通过率跌破容差时门禁拦截，单条回归只告警。
func TestComputeReportGateBlocksRegression(t *testing.T) {
	// baseline 是 10 条全通过的基线。
	baseline := &Baseline{PassRate: 1.0, Cases: map[string]bool{}, SamplesPerCase: 1, Tolerance: 0.05}
	// i 是样本编号。
	for i := 0; i < 10; i++ {
		baseline.Cases[string(rune('a'+i))] = true
	}
	// cases 是 7 通过 3 失败的本次结果。
	cases := make([]CaseResult, 0, 10)
	// i 是样本编号。
	for i := 0; i < 10; i++ {
		// pass 表示第 i 条是否通过。
		pass := i < 7
		cases = append(cases, CaseResult{Chat: string(rune('a' + i)), Pass: pass, Layer: layerRule, Verdicts: []string{"delivery-deflection"}})
	}
	// rep 是本次报告。
	rep := ComputeReport(cases, baseline, "m", "j", "env", 1, 0, 0.05)
	if rep.GatePassed {
		t.Fatalf("通过率跌破容差应拦截: %v", rep.GateReasons)
	}
	if len(rep.Regressions) != 3 {
		t.Fatalf("应识别出 3 条回归: %v", rep.Regressions)
	}
	if !strings.Contains(strings.Join(rep.GateReasons, " "), "不单独拦截") {
		t.Fatalf("单条回归应明确为告警而非拦截: %v", rep.GateReasons)
	}
}

// TestComputeReportJudgeFailureExcluded 判定失败的样本不计入分母。
func TestComputeReportJudgeFailureExcluded(t *testing.T) {
	// cases 是 1 条通过、1 条判定失败。
	cases := []CaseResult{
		{Chat: "a", Pass: true, Layer: layerRule, Verdicts: []string{"delivery-deflection"}},
		{Chat: "b", JudgeFailed: true, Layer: layerJudge, Verdicts: []string{"delivery-deflection"}},
	}
	// rep 是汇总结果。
	rep := ComputeReport(cases, nil, "m", "j", "env", 1, 0, 0.05)
	if rep.Total != 1 || rep.Passed != 1 {
		t.Fatalf("判定失败不应计入分母: total=%d passed=%d", rep.Total, rep.Passed)
	}
	if rep.JudgeFailures != 1 {
		t.Fatalf("应单独统计判定失败数: %d", rep.JudgeFailures)
	}
}

// TestValidateSamplesSkipsUnplayable 缺少快照的样本被跳过，全部不可回放时报错。
func TestValidateSamplesSkipsUnplayable(t *testing.T) {
	// withSnapshot 是可回放样本。
	withSnapshot := evalSampleWithPrompt("提示", "回复")
	// withoutSnapshot 是缺少快照的样本。
	withoutSnapshot := Sample{Chat: "b"}
	// playable、skipped、err 是校验结果。
	playable, skipped, err := ValidateSamples([]Sample{withSnapshot, withoutSnapshot})
	if err != nil {
		t.Fatalf("混有可回放样本时不应报错: %v", err)
	}
	if len(playable) != 1 || skipped != 1 {
		t.Fatalf("校验结果错误: playable=%d skipped=%d", len(playable), skipped)
	}
	// err 是全部样本都不可回放时的报错。
	if _, _, err := ValidateSamples([]Sample{withoutSnapshot}); err == nil {
		t.Fatal("全部不可回放时应报错")
	}
}

// TestDeriveExpectationLegacyFormat 旧格式提示也必须推导出同样的期望。
// 这是跨版本可比性的前提：新格式若能推导而旧格式不能，提示词改动会同时改变判定口径，
// 修复前后的评测结果将不可对照。
func TestDeriveExpectationLegacyFormat(t *testing.T) {
	// sample 使用修复前的旧格式提示。
	sample := evalSampleWithPrompt(
		"……该商品发货方式（按该商品卡密内容确定，可直接回答）：百度网盘、夸克网盘……", "")
	// exp 是推导出的期望。
	exp := sample.ResolveExpectation()
	if exp == nil {
		t.Fatal("旧格式也应能推导期望，否则判定口径随提示词格式漂移")
	}
	if len(exp.MustContainAny) != 2 || exp.MustContainAny[0] != "百度网盘" {
		t.Fatalf("旧格式渠道解析错误: %v", exp.MustContainAny)
	}
}

// TestDeriveExpectationLegacyNonAuthoritative 旧格式标注「仅参考」时不推导强期望，
// 避免把推断当成确定事实去要求模型。
func TestDeriveExpectationLegacyNonAuthoritative(t *testing.T) {
	// sample 使用旧格式但依据为「仅参考」的提示。
	sample := evalSampleWithPrompt("……该商品发货方式（按描述推断，仅参考）：夸克网盘……", "")
	// exp 是推导结果，标注「仅参考」时应为空。
	if exp := sample.ResolveExpectation(); exp != nil {
		t.Fatalf("仅参考的推断不应推导强期望: %+v", exp)
	}
}
