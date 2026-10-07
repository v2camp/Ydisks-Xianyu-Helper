// 本文件集中存放自动化中心的延迟任务与触发键辅助逻辑，均为同包内纯搬移，行为不变。
package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"xianyu-go/internal/db"
)

// taskAutomationRunID 从任务原始字段中解析关联的自动化运行 ID；无原始字段时返回 0。
func taskAutomationRunID(task Task) int64 {
	if task.Raw == nil {
		return 0
	}
	// value 用于本次流程后续判断的值
	value := fmt.Sprint(task.Raw["automation_run_id"])
	// id 用于本次流程后续判断的标识
	id, _ := strconv.ParseInt(value, 10, 64)
	return id
}

// taskDelayCursor 从任务原始字段中解析延迟游标；缺失或非法时返回 -1。
func taskDelayCursor(task Task) int {
	if task.Raw == nil {
		return -1
	}
	// value 用于本次流程后续判断的值
	value := fmt.Sprint(task.Raw["automation_delay_cursor"])
	// cursor、err 用于本次流程后续判断的cursor、err
	cursor, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return cursor
}

// isDeferredReplay 判断当前任务是否为延迟队列回放，回放任务不再重复入队。
func isDeferredReplay(task Task) bool {
	return task.Raw != nil && task.Raw["automation_deferred_replay"] == true
}

// deferTask 将任务以指定到期时间写入延迟队列，错误信息留空。
func (c *Center) deferTask(ctx context.Context, task Task, dueAt int64) error {
	return c.deferTaskWithError(ctx, task, dueAt, "")
}

// deferTaskWithError 将任务连同错误说明写入延迟队列，用触发键做持久化防重。
func (c *Center) deferTaskWithError(ctx context.Context, task Task, dueAt int64, errMsg string) error {
	// key 用于本次流程后续判断的key
	key := buildTriggerKey(task)
	if key == "" {
		return fmt.Errorf("暂停期间的自动化事件缺少可持久化防重键")
	}
	task.CookieStr = ""
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return c.store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
		TaskKey: task.AccountID + ":" + key, CookieID: task.AccountID,
		TriggerType: task.TriggerType, TaskJSON: string(raw), DueAt: dueAt, ErrorMessage: errMsg,
	})
}

// buildTriggerKey 生成任务的持久化防重键，优先沿用未知角色事件的稳定标记。
func buildTriggerKey(task Task) string {
	// marker 是未知角色事件在订单号补齐前生成的稳定防重键；恢复时必须优先沿用它。
	if marker := roleVerificationMarker(task); marker != "" {
		return marker
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		if // attempt、ok 用于本次流程后续判断的attempt、ok
		attempt, ok := task.Raw["attempt"]; ok {
			return fmt.Sprintf("%s:%s:%v", task.TriggerType, task.OrderID, attempt)
		}
	}
	if task.OrderID != "" {
		return task.TriggerType + ":" + task.OrderID
	}
	if task.UpdateKey != "" {
		return task.TriggerType + ":" + task.UpdateKey
	}
	return ""
}
