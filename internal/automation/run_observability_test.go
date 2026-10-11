// automation_run_steps_test.go 验证发货步骤留痕能回答「这次卡在哪一步」。
//
// 价值前提：automation_runs 的终态 success 只代表动作调用返回成功，
// 不代表买家收到。没有步骤记录时，丢件与「根本没发出去」无法区分。
package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// newStepRecorderCoordinator 构造一个只带存储与日志的运行协调器，
// 用于直连验证步骤留痕，不触发任何外部动作。
func newStepRecorderCoordinator(store *db.Store) automationRunCoordinator {
	return automationRunCoordinator{store: store, logger: slog.Default()}
}

// TestRecordRunStepStatuses 步骤留痕必须区分成功、可重试失败与隔离三类结果。
func TestRecordRunStepStatuses(t *testing.T) {
	// store、cleanup 是带账号的测试存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// r 是被测协调器。
	r := newStepRecorderCoordinator(store)
	// ctx 是夹具上下文。
	ctx := context.Background()
	// run 是被观测的运行记录。
	run := &db.AutomationRun{ID: 9001, AttemptCount: 1}
	// action 是本次动作定义。
	action := db.AutomationAction{ActionType: ActionSendCard}

	// 1) 明确成功。
	r.recordRunStep(ctx, run, 7, 0, action, nil, 120*time.Millisecond)
	// 2) 可重试失败。
	r.recordRunStep(ctx, run, 7, 1, action, errors.New("网络超时"), 30*time.Millisecond)
	// 3) 发货内容门禁拦截导致隔离。
	r.recordRunStep(ctx, run, 7, 2, action, fmt.Errorf("%w: %s", errContentBlocked, "链接已失效"), 5*time.Millisecond)

	// rows 是按游标读取的步骤记录。
	rows, err := store.DB.QueryContext(ctx,
		`SELECT action_cursor, step, action_type, status, error, latency_ms
		 FROM automation_run_steps WHERE run_id = 9001 ORDER BY action_cursor`)
	if err != nil {
		t.Fatalf("读取步骤留痕失败: %v", err)
	}
	defer rows.Close()

	// expectation 是单条步骤的期望值。
	type expectation struct {
		step       string
		status     string
		errorMatch string
		latency    int64
	}
	// wants 是逐游标的期望。
	wants := []expectation{
		{db.RunStepAction, stepStatusSuccess, "", 120},
		{db.RunStepAction, stepStatusFailed, "网络超时", 30},
		{db.RunStepQuarantine, stepStatusQuarantine, "发货内容门禁拦截: 链接已失效", 5},
	}
	// index 是当前读到的步骤下标。
	index := 0
	for rows.Next() {
		if index >= len(wants) {
			t.Fatalf("步骤记录多于预期")
		}
		// cursor、step、actionType、status、errText、latency、scanErr 是该条步骤的字段与扫描失败原因。
		var (
			cursor     int
			step       string
			actionType string
			status     string
			errText    string
			latency    int64
			scanErr    error
		)
		if scanErr = rows.Scan(&cursor, &step, &actionType, &status, &errText, &latency); scanErr != nil {
			t.Fatalf("扫描步骤记录失败: %v", scanErr)
		}
		// want 是该游标的期望。
		want := wants[index]
		if cursor != index {
			t.Errorf("游标顺序错误：got %d want %d", cursor, index)
		}
		if step != want.step || status != want.status {
			t.Errorf("游标 %d 的阶段或状态错误：got %s/%s want %s/%s", cursor, step, status, want.step, want.status)
		}
		if errText != want.errorMatch {
			t.Errorf("游标 %d 的错误摘要错误：got %q want %q", cursor, errText, want.errorMatch)
		}
		if latency != want.latency {
			t.Errorf("游标 %d 的耗时错误：got %d want %d", cursor, latency, want.latency)
		}
		index++
	}
	if index != len(wants) {
		t.Fatalf("步骤记录条数错误：got %d want %d", index, len(wants))
	}
}

// TestRecordRunStepTruncatesLongError 超长错误必须按字符截断，避免审计表被正文撑大。
func TestRecordRunStepTruncatesLongError(t *testing.T) {
	// store、cleanup 是带账号的测试存储。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// r 是被测协调器。
	r := newStepRecorderCoordinator(store)
	// ctx 是夹具上下文。
	ctx := context.Background()

	// longError 是刻意构造的超长失败原因。
	longError := errors.New(repeatRunes("错", 500))
	r.recordRunStep(ctx, &db.AutomationRun{ID: 9002, AttemptCount: 1}, 7, 0,
		db.AutomationAction{ActionType: ActionSendText}, longError, time.Millisecond)

	// errText 是落库的错误摘要。
	var errText string
	// err 是读取失败原因。
	if err := store.DB.QueryRowContext(ctx,
		`SELECT error FROM automation_run_steps WHERE run_id = 9002`).Scan(&errText); err != nil {
		t.Fatalf("读取步骤留痕失败: %v", err)
	}
	// runes 是按字符计数的摘要长度；截断后应恰好多出一个省略号字符。
	if runes := []rune(errText); len(runes) != stepErrorMaxRunes+1 {
		t.Fatalf("超长错误未按字符截断：长度 %d", len(runes))
	}
}

// repeatRunes 生成由单个字符重复 n 次组成的字符串。
func repeatRunes(ch string, n int) string {
	// b 是构造缓冲。
	b := make([]byte, 0, len(ch)*n)
	// i 是当前重复下标。
	for i := 0; i < n; i++ {
		b = append(b, ch...)
	}
	return string(b)
}