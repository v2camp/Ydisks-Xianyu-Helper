// replier.go 把客服 Agent 循环适配成 engine 的模型生成接缝。

package agent

import (
	"context"
	"errors"
	"log/slog"

	"xianyu-go/internal/engine"
)

// Replier 实现 engine.AIGenerator，让账号级回复链路可以把「一次问答」换成「带工具的 Agent 循环」。
//
// 适配器本身不做业务判定，只在三种情形之间选路：
//
//  1. 账号未启用客服 Agent，或配置不可解析 → 直接走单次问答；
//  2. 启用且配置可解析 → 运行工具回合循环；
//  3. 循环未能产出最终答复 → 回落到单次问答。
//
// 三条路径的终点都是让买家收到一条回复，而不是静默失败。回落不削弱安全约束——
// 回复正文最终仍由 engine 的价格守卫与外发护栏校验，且回落路径根本不持有工具。
type Replier struct {
	loop     *Loop
	sessions SessionResolver
	cookieID string
	fallback engine.AIGenerator
	logger   *slog.Logger
}

// 编译期断言：Replier 必须能直接注入 engine.NewAIReplier。
var _ engine.AIGenerator = (*Replier)(nil)

// ReplierConfig 是适配器的构造依赖。
type ReplierConfig struct {
	// Loop 是工具回合循环，必填。
	Loop *Loop
	// Sessions 是账号级会话配置解析端口，必填。
	Sessions SessionResolver
	// CookieID 是适配器绑定的账号；engine 按账号构造生成器，因此这里只有一个固定值。
	CookieID string
	// Fallback 是回落生成器；nil 时使用 engine 的默认单次问答实现。
	Fallback engine.AIGenerator
	// Logger 是脱敏结构化日志器；nil 时使用进程默认日志器。
	Logger *slog.Logger
}

// NewReplier 构造适配器。
// 必需依赖缺失时返回错误而不是留到首次补全才发现：生成器在账号启动期构造，构造失败
// 应当立即表现为「该账号不启用 Agent」，而不是运行期每次问答都静默降级。
func NewReplier(cfg ReplierConfig) (*Replier, error) {
	if cfg.Loop == nil {
		return nil, errors.New("客服 Agent 循环未装配")
	}
	if cfg.Sessions == nil {
		return nil, errors.New("客服 Agent 会话配置端口未装配")
	}
	// logger 是本适配器使用的脱敏结构化日志器。
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// fallback 是回落生成器；未注入时使用 engine 的默认单次问答实现。
	fallback := cfg.Fallback
	if fallback == nil {
		fallback = engine.NewDefaultAIGenerator()
	}
	return &Replier{
		loop:     cfg.Loop,
		sessions: cfg.Sessions,
		cookieID: cfg.CookieID,
		fallback: fallback,
		logger:   logger.With("account", cfg.CookieID, "subsys", "support-agent"),
	}, nil
}

// Generate 按账号当前生效配置选择「Agent 循环」或「单次问答」，并保证最终一定有一条回复。
// ctx 与 req 的原样透传保证提示词、历史与模型配置与控制流无关。
func (r *Replier) Generate(ctx context.Context, req engine.GenerateRequest) (string, error) {
	// session、enabled、resolveErr 分别是本账号生效的会话配置、启用状态与解析失败原因。
	session, enabled, resolveErr := r.sessions.ResolveSession(ctx, r.cookieID)
	if resolveErr != nil {
		// 配置不可解析时按未启用处理：宁可不带工具答复，也不能凭未知档位运行工具回合。
		r.logger.Warn("客服 Agent 配置解析失败，回落单次问答", "err", resolveErr)
		return r.fallback.Generate(ctx, req)
	}
	if !enabled {
		return r.fallback.Generate(ctx, req)
	}
	// content、loopErr 分别是 Agent 循环的答复与失败原因。
	content, loopErr := r.loop.Run(ctx, session, req)
	if loopErr == nil {
		return content, nil
	}
	r.logger.Warn("客服 Agent 循环失败，回落单次问答", "err", loopErr)
	return r.fallback.Generate(ctx, req)
}
