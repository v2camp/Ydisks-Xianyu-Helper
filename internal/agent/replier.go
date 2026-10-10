// replier.go 把客服 Agent 循环适配成 engine 的模型生成接缝。

package agent

import (
	"context"
	"log/slog"

	"xianyu-go/internal/engine"
)

// Replier 实现 engine.AIGenerator，让账号级回复链路可以把「一次问答」换成「带工具的 Agent 循环」。
//
// 兜底策略：循环失败（预算耗尽、模型异常、工具链路故障）时回落到单次问答生成器，
// 保证买家始终收到一条回复而不是静默失败。回落不会削弱安全约束——
// 回复正文最终仍由 engine 的价格守卫与外发护栏校验。
type Replier struct {
	loop     *Loop
	session  Session
	fallback engine.AIGenerator
	logger   *slog.Logger
}

// 编译期断言：Replier 必须能直接注入 engine.NewAIReplier。
var _ engine.AIGenerator = (*Replier)(nil)

// NewReplier 构造适配器。
// fallback 为 nil 时使用 engine 的默认单次问答实现，确保兜底路径始终存在。
func NewReplier(loop *Loop, session Session, fallback engine.AIGenerator, logger *slog.Logger) *Replier {
	if logger == nil {
		logger = slog.Default()
	}
	if fallback == nil {
		fallback = engine.NewDefaultAIGenerator()
	}
	return &Replier{
		loop:     loop,
		session:  session,
		fallback: fallback,
		logger:   logger.With("account", session.CookieID, "subsys", "support-agent"),
	}
}

// Generate 先尝试 Agent 循环，失败则回落单次问答。
// ctx 与 req 的原样透传保证提示词、历史与模型配置与控制流无关。
func (r *Replier) Generate(ctx context.Context, req engine.GenerateRequest) (string, error) {
	// content、err 分别是 Agent 循环的答复与失败原因。
	content, err := r.loop.Run(ctx, r.session, req)
	if err == nil {
		return content, nil
	}
	r.logger.Warn("客服 Agent 循环失败，回落单次问答", "err", err)
	return r.fallback.Generate(ctx, req)
}
