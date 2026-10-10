// support_agent_adapter.go 把客服 Agent 运行时接到账号回复链路上。
//
// 组合层在这里只做三件事：把应用服务投影为 Agent 的工具端口、把账号级配置投影为 Agent 的
// 会话配置、把适配器包装成 engine 的账号级生成器。能力放行判定不在这里——它只由
// capability.Evaluate 决定，本文件既不放大也不缩小权限。

package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"xianyu-go/internal/agent"
	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// supportAgentSessions 按账号解析客服 Agent 的生效配置。
type supportAgentSessions struct {
	// ports 返回已完成装配的只读应用服务快照；装配未完成时返回零值。
	ports func() composition.TransportPorts
	// ownerOf 按账号标识解析归属租户；账号不存在时返回错误。
	ownerOf func(ctx context.Context, cookieID string) (int64, error)
}

// 编译期断言：配置解析端口必须满足 Agent 运行时定义的会话解析契约。
var _ agent.SessionResolver = (*supportAgentSessions)(nil)

// ResolveSession 解析账号当前生效的开关与档位。
//
// 只有「账号归属不可解析」与「配置读取失败」两类硬失败返回错误；接口未装配、账号未配置
// 都按未启用返回。两种处置对调用方是同一件事——不带工具答复，因此不需要区分。
func (s *supportAgentSessions) ResolveSession(ctx context.Context, cookieID string) (agent.Session, bool, error) {
	if s == nil || s.ports == nil || s.ownerOf == nil {
		return agent.Session{}, false, nil
	}
	// configs 是客服 Agent 配置应用服务；未装配时视为该环境不提供客服 Agent 配置。
	configs := s.ports().AgentSupport
	if configs == nil {
		return agent.Session{}, false, nil
	}
	// userID、ownerErr 是账号归属租户及其解析失败原因。
	userID, ownerErr := s.ownerOf(ctx, cookieID)
	if ownerErr != nil {
		return agent.Session{}, false, fmt.Errorf("解析账号 %s 归属失败: %w", cookieID, ownerErr)
	}
	// effective、configErr 是该账号最终生效的配置及其读取失败原因；档位已由应用层按
	// 「账号覆盖 → 租户默认 → 只读档」解析完毕，这里不再重复收敛。
	effective, configErr := configs.AccountConfig(ctx, userID, cookieID)
	if configErr != nil {
		return agent.Session{}, false, fmt.Errorf("读取账号 %s 客服 Agent 配置失败: %w", cookieID, configErr)
	}
	// session 是本账号运行 Agent 时的作用域与档位来源。
	session := agent.Session{UserID: userID, CookieID: cookieID, Preset: effective.Preset}
	return session, effective.Enabled, nil
}

// buildSupportAgentFactory 装配账号级客服 Agent 生成器工厂。
//
// services 延迟读取已完成装配的应用服务集合：工厂只在账号启动时才被调用，读到它时组合早已结束。
// 返回 nil 表示依赖不完整，engine 对全部账号沿用默认单次问答。
func buildSupportAgentFactory(store *db.Store, services func() *composition.Services, logger *slog.Logger) engine.AIGeneratorFactory {
	// ports 延迟读取已完成装配的只读应用服务快照；集合未就绪时返回零值。
	ports := func() composition.TransportPorts {
		if services == nil {
			return composition.TransportPorts{}
		}
		return services().TransportPorts()
	}
	// lifecycleContext 延迟读取应用生命周期 Context；未就绪时返回 nil，工具层据此判定不可用。
	lifecycleContext := func() context.Context {
		if services == nil {
			return nil
		}
		return services().LifecycleContext()
	}
	// ownerOf 按账号标识解析归属租户；凭证仓储缺失时保持 nil，解析端口整体不可用。
	var ownerOf func(ctx context.Context, cookieID string) (int64, error)
	if store != nil && store.Cookies != nil {
		ownerOf = store.Cookies.GetOwnerID
	}
	return newSupportAgentFactory(newSupportAgentSessions(ports, ownerOf), ports, lifecycleContext, logger)
}

// newSupportAgentSessions 构造账号级配置解析端口；任一依赖缺失时返回未包装的 nil。
// 返回接口类型而不是具体指针，使调用方的 `== nil` 判空语义成立。
func newSupportAgentSessions(ports func() composition.TransportPorts, ownerOf func(ctx context.Context, cookieID string) (int64, error)) agent.SessionResolver {
	if ports == nil || ownerOf == nil {
		return nil
	}
	return &supportAgentSessions{ports: ports, ownerOf: ownerOf}
}

// newSupportAgentFactory 装配客服 Agent 的账号级模型生成器工厂。
//
// ports 与 lifecycleContext 由工厂在账号启动时才读取，因此可以在应用服务装配完成前创建：
// 账号运行时由生命周期协调器启动，读到它们时组合早已结束。
//
// 返回 nil 表示依赖不完整，engine 对全部账号沿用默认单次问答。客服 Agent 是可选增强，
// 缺依赖时少一个能力可用即可，不能让整条回复链路启动失败。
func newSupportAgentFactory(
	sessions agent.SessionResolver,
	ports func() composition.TransportPorts,
	lifecycleContext func() context.Context,
	logger *slog.Logger,
) engine.AIGeneratorFactory {
	if sessions == nil || ports == nil || lifecycleContext == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return func(cookieID string) engine.AIGenerator {
		// loop 是本账号使用的工具回合循环；工具层依赖缺失时保持 nil，本账号不启用 Agent。
		loop := newSupportAgentLoop(ports, lifecycleContext, logger)
		if loop == nil {
			logger.Warn("客服 Agent 工具层未装配，账号沿用单次问答", "account", cookieID)
			return nil
		}
		// replier、replierErr 分别是账号级生成器适配器与其构造失败原因。
		replier, replierErr := agent.NewReplier(agent.ReplierConfig{
			Loop: loop, Sessions: sessions, CookieID: cookieID, Logger: logger,
		})
		if replierErr != nil {
			logger.Error("构造客服 Agent 生成器失败，账号沿用单次问答", "account", cookieID, "err", replierErr)
			return nil
		}
		return replier
	}
}

// newSupportAgentLoop 构造工具回合循环；能力目录或任一工具端口缺失时返回 nil。
//
// 每次账号启动都会重新构造一次：循环本身无状态（作用域由 Session 逐次传入），
// 重建的成本是几个结构体分配，换取的是不必在工厂里维护共享状态。
func newSupportAgentLoop(ports func() composition.TransportPorts, lifecycleContext func() context.Context, logger *slog.Logger) *agent.Loop {
	// catalog、catalogErr 分别是平台能力目录与其构造失败原因；目录是代码常量，失败属编程错误。
	catalog, catalogErr := capability.PlatformCatalog()
	if catalogErr != nil {
		logger.Error("构造平台能力目录失败，客服 Agent 不可用", "err", catalogErr)
		return nil
	}
	// snapshot 是已完成装配的只读应用服务快照。
	snapshot := ports()
	// orders、items 是工具层所需的只读用例端口；任一缺失即视为工具层未装配。
	orders, items := newOrderPorts(snapshot, lifecycleContext), newItemPorts(snapshot)
	if orders == nil || items == nil {
		return nil
	}
	return agent.NewLoop(agent.NewDefaultModelClient(), agent.NewTools(catalog, orders, items), agent.Budget{}, logger)
}
