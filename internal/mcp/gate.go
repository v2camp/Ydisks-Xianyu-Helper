// gate.go 把启用门、loopback 网络策略、Bearer 鉴权与失败限流串成 /mcp 专用 http 中间件。
// 顺序固定为：端点隐藏（404）→ 网络来源（403）→ 失败限流与令牌鉴权（401/429）→ 注入调用身份。

package mcp

import (
	"context"
	"net/http"
	"sync"
	"time"
)

const (
	// stateCacheTTL 是启用状态快照的缓存时长；设置变更后由 Invalidate 立即失效，不依赖 TTL 到期。
	stateCacheTTL = 5 * time.Second
	// retryAfterHeader 是触发来源封禁时下发的建议重试间隔响应头。
	retryAfterHeader = "Retry-After"
)

// endpointState 是 MCP 端点存活判定所需的非敏感状态快照。
type endpointState struct {
	// enabled 表示管理员是否已启用 MCP 服务。
	enabled bool
	// hasToken 表示是否存在任一可用持久化令牌。
	hasToken bool
	// allowNonLoopback 表示是否显式放行非本机来源。
	allowNonLoopback bool
}

// available 报告端点是否应提供协议服务：启用且至少存在一枚持久化令牌或环境引导令牌。
func (s endpointState) available(hasEnvironmentToken bool) bool {
	return s.enabled && (s.hasToken || hasEnvironmentToken)
}

// Guard 拥有 /mcp 的安全中间件状态：配置快照缓存、环境令牌、限流器与固定管理员身份解析。
// Guard 不持有业务 worker；其状态仅服务传输层判定，所有方法可并发调用。
type Guard struct {
	// config 提供启用状态、网络策略与持久化令牌校验。
	config ConfigPort
	// identity 提供固定管理员用户标识。
	identity IdentityPort
	// environmentToken 是进程启动时注入的 XIANYU_MCP_TOKEN 值；空串表示未配置环境引导令牌。
	environmentToken string
	// limiter 按来源 IP 限制鉴权失败尝试。
	limiter *sourceLimiter
	// now 是统一时钟，测试可注入以驱动缓存 TTL 与限流窗口。
	now func() time.Time
	// cacheTTL 是配置快照的缓存有效期。
	cacheTTL time.Duration

	// mu 保护 cached 与 fetchedAt 的读写；持锁期间禁止数据库或网络 I/O。
	mu sync.Mutex
	// cached 是最近一次成功获取的配置快照。
	cached endpointState
	// fetchedAt 是当前快照的获取时刻；零值表示尚未获取。
	fetchedAt time.Time
}

// GuardConfig 是构造 Guard 的必需配置集合；缺省项在 NewGuard 中按安全默认值补齐。
type GuardConfig struct {
	// Config 是 MCP 配置端口，必填。
	Config ConfigPort
	// Identity 是固定管理员身份端口，必填。
	Identity IdentityPort
	// EnvironmentToken 是环境引导令牌；空白视为未配置。
	EnvironmentToken string
	// Now 是可注入时钟；nil 时使用墙钟。
	Now func() time.Time
	// StateCacheTTL 是配置快照缓存时长；非正时使用默认 TTL。
	StateCacheTTL time.Duration
}

// NewGuard 校验必需依赖并构造 /mcp 安全守卫；配置缺失时构造失败，禁止半构造对象对外服务。
func NewGuard(cfg GuardConfig) (*Guard, error) {
	if cfg.Config == nil {
		return nil, errGuardConfigMissing("MCP 配置端口")
	}
	if cfg.Identity == nil {
		return nil, errGuardConfigMissing("MCP 身份端口")
	}
	// now 是守卫统一时钟。
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	// ttl 是配置快照缓存时长。
	ttl := cfg.StateCacheTTL
	if ttl <= 0 {
		ttl = stateCacheTTL
	}
	return &Guard{
		config:           cfg.Config,
		identity:         cfg.Identity,
		environmentToken: cfg.EnvironmentToken,
		limiter:          newSourceLimiter(now),
		now:              now,
		cacheTTL:         ttl,
	}, nil
}

// Middleware 按固定顺序执行隐藏、网络与鉴权检查，通过后把固定管理员身份注入请求上下文。
func (g *Guard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// now 是本次判定的统一时刻。
		now := g.now()
		// state、stateErr 是当前端点状态快照与其获取错误；获取失败时按关闭处理并附诊断头语义。
		state, stateErr := g.currentState(r.Context(), now)
		if stateErr != nil || !state.available(g.environmentToken != "") {
			// 关闭或无令牌时返回 404，不向未授权方暴露端点存在性。
			http.NotFound(w, r)
			return
		}
		// source 是请求的 TCP 来源主机标识，用于 loopback 判定与限流键。
		source := sourceKey(r)
		if !isLoopbackRequest(r) && !state.allowNonLoopback {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("仅允许本机访问 MCP 服务\n"))
			return
		}
		// allowed、retryAfter 是来源限流判定结果；封禁中直接返回 429 且不做令牌比较。
		allowed, retryAfter := g.limiter.allow(source, now)
		if !allowed {
			if retryAfter > 0 {
				w.Header().Set(retryAfterHeader, retryAfterSeconds(retryAfter))
			}
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// presented 是请求携带的 Bearer 令牌。
		presented := bearerToken(r)
		// tokenSource、ok 是令牌校验结果；失败记录来源计数并返回 401。
		tokenSource, ok := authenticate(r.Context(), g.config, g.environmentToken, presented)
		if !ok {
			g.limiter.recordFailure(source, now)
			w.Header().Set("WWW-Authenticate", `Bearer realm="ydisks-mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// userID、userErr 是固定管理员身份解析结果；失败时拒绝请求，不允许匿名进入协议层。
		userID, userErr := g.identity.AdminUserID(r.Context())
		if userErr != nil || userID <= 0 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// identity 是注入下游协议处理器的调用身份指针。
		identity := &CallIdentity{UserID: userID, Source: tokenSource}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), identity)))
	})
}

// Invalidate 立即丢弃缓存的配置快照，使管理员启用/停用/网络策略变更在下一次请求生效。
func (g *Guard) Invalidate() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cached = endpointState{}
	g.fetchedAt = time.Time{}
}

// currentState 返回有效期内的缓存快照，否则重新从配置端口读取；读库在锁外执行。
func (g *Guard) currentState(ctx context.Context, now time.Time) (endpointState, error) {
	g.mu.Lock()
	// cached、fetchedAt 是当前缓存快照及其获取时刻，锁内只做内存判定。
	cached, fetchedAt := g.cached, g.fetchedAt
	g.mu.Unlock()
	if !fetchedAt.IsZero() && now.Sub(fetchedAt) < g.cacheTTL {
		return cached, nil
	}
	// fresh 是从配置端口读取的最新状态；任一项读取失败都返回错误，由调用方按关闭处理。
	fresh := endpointState{}
	// enabled、enabledErr 是启用状态及读取错误。
	enabled, enabledErr := g.config.Enabled(ctx)
	if enabledErr != nil {
		return endpointState{}, enabledErr
	}
	// allowNonLoopback、policyErr 是网络策略及读取错误。
	allowNonLoopback, policyErr := g.config.AllowNonLoopback(ctx)
	if policyErr != nil {
		return endpointState{}, policyErr
	}
	// hasToken、tokenErr 是持久化令牌存在性及读取错误。
	hasToken, tokenErr := g.config.HasPersistedToken(ctx)
	if tokenErr != nil {
		return endpointState{}, tokenErr
	}
	fresh.enabled = enabled
	fresh.allowNonLoopback = allowNonLoopback
	fresh.hasToken = hasToken
	g.mu.Lock()
	g.cached = fresh
	g.fetchedAt = now
	g.mu.Unlock()
	return fresh, nil
}
