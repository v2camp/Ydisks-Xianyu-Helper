// budget.go 定义客服 Agent 循环的预算与预算耗尽错误。

package agent

import (
	"errors"
	"time"
)

// ErrBudgetExhausted 表示 Agent 循环用尽了回合预算仍未得到最终答复。
// 调用方必须回落到单次问答生成器，禁止把该错误透传给买家。
var ErrBudgetExhausted = errors.New("客服 Agent 回合预算耗尽")

const (
	// defaultMaxRounds 是允许执行工具的最大回合数。
	defaultMaxRounds = 4
	// defaultMaxCallsPerRound 是单个回合内允许执行的工具调用上限；超出的调用只回执不执行。
	defaultMaxCallsPerRound = 6
	// defaultTotalTimeout 是整次 Agent 循环的总预算，必须小于外层单次问答的 30 秒调用预算。
	defaultTotalTimeout = 25 * time.Second
	// defaultToolTimeout 是单次工具调用的预算。
	defaultToolTimeout = 8 * time.Second
)

// Budget 是客服 Agent 循环的步数与时长预算。
// 零值字段在 NewLoop 中回落到内置默认值，便于调用方只覆盖关心的项。
type Budget struct {
	// MaxRounds 是允许执行工具的最大回合数；每个回合对应一次模型补全。
	MaxRounds int
	// MaxCallsPerRound 是单个回合内允许执行的工具调用上限。
	MaxCallsPerRound int
	// TotalTimeout 是整次 Agent 循环的总预算。
	TotalTimeout time.Duration
	// ToolTimeout 是单次工具调用的预算。
	ToolTimeout time.Duration
}

// normalized 返回补齐零值后的预算，保证每次循环都有确定的上界。
func (b Budget) normalized() Budget {
	// result 是补齐零值后的预算。
	result := b
	if result.MaxRounds <= 0 {
		result.MaxRounds = defaultMaxRounds
	}
	if result.MaxCallsPerRound <= 0 {
		result.MaxCallsPerRound = defaultMaxCallsPerRound
	}
	if result.TotalTimeout <= 0 {
		result.TotalTimeout = defaultTotalTimeout
	}
	if result.ToolTimeout <= 0 {
		result.ToolTimeout = defaultToolTimeout
	}
	return result
}
