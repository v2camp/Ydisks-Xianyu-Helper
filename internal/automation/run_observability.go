// run_observability.go 自动发货链路的步骤级留痕。
//
// 拆分原因：run_coordinator.go 承载发货状态机（游标、租约、检查点、不确定结果收口），
// 已接近单文件行数上限；归因逻辑职责独立，单独成文件便于后续扩展买家侧确认信号。

package automation

import (
	"context"
	"errors"
	"time"

	"xianyu-go/internal/db"
)

// 步骤结果状态简码。
const (
	// stepStatusSuccess 表示动作明确成功。
	stepStatusSuccess = "success"
	// stepStatusFailed 表示动作失败且可重试。
	stepStatusFailed = "failed"
	// stepStatusQuarantine 表示被门禁拦截或结果不确定，已隔离为人工核对。
	stepStatusQuarantine = "quarantine"
	// stepErrorMaxRunes 是失败原因摘要的最大字符数，避免超长正文把审计表撑大。
	stepErrorMaxRunes = 200
)

// recordRunStep 记录一次发货动作的步骤留痕，回答「这次发货卡在哪一步」。
//
// 为什么必须留痕：automation_runs 的终态 success 只代表动作调用返回成功，
// 不代表买家实际收到；平台静默拦截时系统记为成功，而丢件无法被发现。
// 有步骤记录后，至少能区分「动作没发出去」与「发出去了但没收到」。
// 埋点失败只记 Debug，绝不影响发货链路。
func (r automationRunCoordinator) recordRunStep(ctx context.Context, run *db.AutomationRun, ruleID int64, cursor int, action db.AutomationAction, actionErr error, latency time.Duration) {
	// status 是本次动作的结果状态。
	status := stepStatusSuccess
	// step 是阶段简码；被门禁或不确定结果收口时升级为隔离。
	step := db.RunStepAction
	// errText 是失败原因摘要。
	errText := ""
	if actionErr != nil {
		status = stepStatusFailed
		errText = actionErr.Error()
		// 发货内容门禁与「需人工核对」哨兵都会隔离为人工核对，单独标记便于按阶段排查。
		if isDeliveryGuardError(actionErr) || errors.Is(actionErr, errAutomationNeedsReview) {
			step = db.RunStepQuarantine
			status = stepStatusQuarantine
		}
	}
	// runes 是按字符计数的错误摘要，用于安全截断。
	if runes := []rune(errText); len(runes) > stepErrorMaxRunes {
		errText = string(runes[:stepErrorMaxRunes]) + "…"
	}
	// stepCtx 保证调用方取消后步骤仍能落库：动作刚跑完时 ctx 可能已超时，
	// 用原 ctx 会让最关键的归因记录恰好在最需要的时候丢失。
	stepCtx, stepCancel := newAutomationRunCompensationContext(ctx)
	// writeErr 是写入失败原因；失败只记 Debug。
	writeErr := r.store.Automation.AddRunStep(stepCtx, db.AutomationRunStep{
		RunID:        run.ID,
		RuleID:       ruleID,
		AttemptCount: run.AttemptCount,
		ActionCursor: cursor,
		Step:         step,
		ActionType:   action.ActionType,
		Status:       status,
		Error:        errText,
		LatencyMS:    latency.Milliseconds(),
	})
	stepCancel()
	if writeErr != nil {
		r.logger.Debug("写入发货步骤留痕失败", "run_id", run.ID, "err", writeErr)
	}
}