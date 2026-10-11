// automation_observability.go 发货链路的步骤级留痕仓储。
//
// 拆分原因：automation.go 承载规则、动作、运行与重试语义，已接近单文件行数上限；
// 步骤留痕职责独立，单独成文件便于后续扩展买家侧确认信号。

package db

import "context"

// 发货步骤阶段简码。区分「执行」与「拦截」两类，便于按阶段聚合排查。
const (
	// RunStepAction 表示一次外部动作（发卡、模板或文本）的执行结果。
	RunStepAction = "action"
	// RunStepQuarantine 表示动作结果不确定或被门禁拦截，已隔离为人工核对。
	RunStepQuarantine = "quarantine"
)

// AutomationRunStep 是一次发货链路步骤的留痕记录。
// 定位键是 (RunID, ActionCursor, AttemptCount)：延迟会让同一动作多次尝试，
// 用自增 id 判断「是否已记录」会漏掉重试轨迹。
type AutomationRunStep struct {
	// RunID 是运行主键。
	RunID int64
	// RuleID 是规则主键。
	RuleID int64
	// AttemptCount 是本次运行的重试代次。
	AttemptCount int
	// ActionCursor 是动作在计划中的位置；-1 表示非动作类步骤。
	ActionCursor int
	// Step 是阶段简码，见 RunStepAction 等常量。
	Step string
	// ActionType 是动作类型（send_card / send_template / send_text 等）。
	ActionType string
	// Status 是结果状态：success / failed / quarantine。
	Status string
	// Error 是失败原因摘要；成功时为空。
	Error string
	// LatencyMS 是该步骤耗时（毫秒）。
	LatencyMS int64
}

// AddRunStep 写入一条发货步骤留痕。
// 失败只影响可观测性，调用方须自行吞掉错误，不得让埋点失败阻断发货链路。
func (a *AutomationRules) AddRunStep(ctx context.Context, step AutomationRunStep) error {
	// err 是写入失败原因。
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO automation_run_steps
		(run_id,rule_id,attempt_count,action_cursor,step,action_type,status,error,latency_ms)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		step.RunID, step.RuleID, step.AttemptCount, step.ActionCursor,
		step.Step, step.ActionType, step.Status, step.Error, step.LatencyMS)
	return err
}

