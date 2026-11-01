package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
)

// aiReadinessIntervalEnv 是自检周期的环境变量名（秒）；未配置时用默认周期。
const aiReadinessIntervalEnv = "XIANYU_AI_READINESS_INTERVAL_SECONDS"

// defaultAIReadinessInterval 是默认自检周期；一小时足够发现配置漂移，又不至于打扰日志。
const defaultAIReadinessInterval = time.Hour

// minAIReadinessInterval 是自检周期下限，避免误配置成高频扫描。
const minAIReadinessInterval = time.Minute

// AIReadinessReport 是一次 AI 启用链路自检的结果。
// 只记录判定结论与账号标识，不含任何凭据内容。
type AIReadinessReport struct {
	// MasterEnabled 表示 AI 全局总闸（system_settings.ai_master_enabled）是否开启；未配置视为开启。
	MasterEnabled bool
	// EnabledCookieIDs 是账号级启用 AI 的账号列表：不区分是否已显式保存设置行，
	// 只要没有被显式关闭就算启用；是否真正生效还取决于 MasterEnabled。
	EnabledCookieIDs []string
	// ScopeConfigured 表示 ai_scope_config 是否已写入；未写入时回落内置边界，不算阻断。
	ScopeConfigured bool
	// CredentialIssues 是凭据侧的阻断说明（未配置密钥、或配置无法读取）。
	CredentialIssues []string
}

// Ready 表示可判定部分均无阻断：全局总闸开启，且没有凭据问题。
// 账号是否「已显式启用」不再是前置条件——无设置行的账号默认跟随总闸。
func (r AIReadinessReport) Ready() bool {
	return r.MasterEnabled && len(r.CredentialIssues) == 0
}

// Problems 把阻断项整理成人读说明，供启动日志与巡检共用。
// 「没有任何账号启用 AI」不再是故障：默认开启语义下只要账号未被显式关闭即视为启用。
func (r AIReadinessReport) Problems() []string {
	// problems 是累积的阻断说明。
	problems := make([]string, 0, 2+len(r.CredentialIssues))
	if !r.MasterEnabled {
		problems = append(problems, "AI 总闸已关闭（system_settings.ai_master_enabled），所有账号的 AI 回复都不会工作")
	}
	problems = append(problems, r.CredentialIssues...)
	return problems
}

// CheckAIReadiness 自检 AI 启用链路中可离线判定的三道闸门。
//
// 对应 ai.go 的四闸门：G2 账号开关、G3 边界配置、G4 API 凭证。
// G1（商品归属）依赖具体消息，无法在启动期判定，故不在自检范围。
// 返回的错误只表示查询本身失败；闸门未通过以 Problems 表达，不当作错误。
// 密钥读取复用进程级密钥缓存，避免周期自检每小时都写一行审计。
func CheckAIReadiness(ctx context.Context, store *db.Store) (AIReadinessReport, error) {
	return CheckAIReadinessWithProvider(ctx, store, DefaultAIKeyProvider())
}

// CheckAIReadinessWithProvider 使用指定密钥缓存执行自检；provider 为 nil 时回落进程级共享实例。
// 抽成带参数的版本便于测试注入可观测的缓存实例，同时保证自检不绕过缓存。
func CheckAIReadinessWithProvider(ctx context.Context, store *db.Store, provider *AIKeyProvider) (AIReadinessReport, error) {
	// report 是本次自检结果。
	var report AIReadinessReport
	if store == nil {
		return report, errors.New("自检需要已初始化的存储")
	}
	// master、masterErr 是全局总闸的生效值；查询失败必须显式返回，不能当作「总闸关闭」。
	master, masterErr := store.AIReply.MasterEnabled(ctx)
	if masterErr != nil {
		return report, fmt.Errorf("读取 AI 全局总闸失败: %w", masterErr)
	}
	report.MasterEnabled = master
	// ids、err 是账号级启用 AI 的账号列表；查询失败必须显式返回，不能当作「没有账号启用」。
	ids, err := store.AIReply.ListEnabledCookieIDs(ctx)
	if err != nil {
		return report, fmt.Errorf("查询启用 AI 的账号失败: %w", err)
	}
	report.EnabledCookieIDs = ids
	// raw、err 是边界配置原文；为空表示使用内置边界（合法状态，不算阻断）。
	raw, err := store.Settings.Get(ctx, settingAIScopeKey)
	if err != nil {
		return report, fmt.Errorf("读取 AI 边界配置失败: %w", err)
	}
	report.ScopeConfigured = strings.TrimSpace(raw) != ""
	// AI 模型密钥是全局单键，与账号数量无关：只按第一个启用账号读取一次，
	// 避免账号数变全量后每次自检都刷出成百条 security_audit_logs。
	if len(ids) > 0 {
		report.checkCredential(ctx, store, provider, ids[0])
	}
	return report, nil
}

// checkCredential 检查一次全局 AI 模型密钥是否可用，并把阻断原因累积到 report 上。
// 参数 ctx 是自检作用域；store 是只读查询边界；provider 是密钥缓存（nil 时回落进程级实例）；
// cookieID 是任一启用账号，仅用于解析密钥审计归属的所有者。
// 该方法只读取密钥是否存在，不保存也不记录密钥内容；命中缓存时不写审计。
func (r *AIReadinessReport) checkCredential(ctx context.Context, store *db.Store, provider *AIKeyProvider, cookieID string) {
	if provider == nil {
		provider = DefaultAIKeyProvider()
	}
	// key、keyErr 是解密后的全局 AI 密钥；失败说明存储或审计写入异常。
	// 走 provider 而不是直接读库：与回复链共享缓存，自检不绕过缓存刷审计。
	key, keyErr := provider.Get(ctx, store, cookieID, "ai_api_key", "settings.use", "ai_readiness")
	if keyErr != nil {
		r.CredentialIssues = append(r.CredentialIssues,
			fmt.Sprintf("读取 ai_api_key 失败：%v", keyErr))
		return
	}
	if strings.TrimSpace(key) == "" {
		r.CredentialIssues = append(r.CredentialIssues,
			"未配置 ai_api_key，AI 回复会被静默跳过")
	}
}

// AIReadinessProbe 周期执行 AI 启用链路自检，把「配了但没生效」从静默变成告警。
//
// 并发与生命周期（AGENTS 1.6）：
//   - 归属：由组合根登记为生命周期组件，StartFunc 起 goroutine、CloseFunc 等待退出。
//   - 取消：Run 持有共享生命周期 ctx，ctx 取消即退出并关闭 done。
//   - 等待：WaitContext 在 done 关闭前阻塞，关闭流程据此 Join，不留 goroutine。
//   - 失败语义：自检查询失败只记告警，绝不阻塞业务循环。
type AIReadinessProbe struct {
	// store 是只读查询边界；为 nil 时探针视为未装配。
	store *db.Store
	// interval 是自检周期，构造期固化。
	interval time.Duration
	// logger 记录自检结论；不含任何密钥内容。
	logger *slog.Logger
	// keyProvider 是自检读取密钥时使用的带 TTL 缓存；nil 时回落进程级共享实例。
	keyProvider *AIKeyProvider
	// runOnce 保证只启动一个由 ctx 管理的循环。
	runOnce sync.Once
	// done 在循环退出后关闭，供关闭流程等待。
	done chan struct{}
}

// NewAIReadinessProbe 构造自检探针；store 为 nil 或周期显式关闭时返回 nil。
// 周期从 XIANYU_AI_READINESS_INTERVAL_SECONDS 读取：0 表示关闭，非法值回落默认，低于下限抬到下限。
// providers 是可选密钥缓存，最多只取第一个：不传或传 nil 时使用进程级共享实例，
// 从而让既有调用点无需改动也能共享同一份缓存。
func NewAIReadinessProbe(store *db.Store, logger *slog.Logger, providers ...*AIKeyProvider) *AIReadinessProbe {
	if store == nil {
		return nil
	}
	// interval 是本次构造确定的自检周期；0 表示关闭。
	interval := aiReadinessIntervalFromEnv(os.Getenv)
	if interval <= 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	// keyProvider 是本次自检使用的密钥缓存；未显式传入时回落进程级共享实例。
	keyProvider := DefaultAIKeyProvider()
	if len(providers) > 0 && providers[0] != nil {
		keyProvider = providers[0]
	}
	return &AIReadinessProbe{
		store:       store,
		interval:    interval,
		logger:      logger.With("subsys", "ai-readiness"),
		keyProvider: keyProvider,
		done:        make(chan struct{}),
	}
}

// aiReadinessIntervalFromEnv 从环境变量解析自检周期；空值与非法值回落默认，0 表示关闭。
func aiReadinessIntervalFromEnv(getenv func(string) string) time.Duration {
	// raw 是环境变量原文；空串表示未配置。
	raw := strings.TrimSpace(getenv(aiReadinessIntervalEnv))
	if raw == "" {
		return defaultAIReadinessInterval
	}
	// seconds 是解析出的秒数；非数字按未配置处理。
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return defaultAIReadinessInterval
	}
	if seconds == 0 {
		return 0
	}
	if seconds < 0 {
		return defaultAIReadinessInterval
	}
	// interval 是换算出的周期；过短时抬到下限。
	interval := time.Duration(seconds) * time.Second
	if interval < minAIReadinessInterval {
		return minAIReadinessInterval
	}
	return interval
}

// Run 启动自检循环；调用方应在 goroutine 中启动并用 ctx 控制生命周期。
func (p *AIReadinessProbe) Run(ctx context.Context) {
	if p == nil {
		return
	}
	p.runOnce.Do(func() {
		// 无论循环因何退出都关闭 done，供 WaitContext 解除等待。
		defer close(p.done)
		if ctx.Err() != nil {
			return
		}
		// 启动即自检一次：把「部署完才发现没启用」提前到进程启动的第一秒。
		p.check(ctx)
		// ticker 按周期复检，覆盖运行期配置被清空的情况。
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.check(ctx)
			}
		}
	})
}

// check 执行一次自检并记录结论。
func (p *AIReadinessProbe) check(ctx context.Context) {
	// report、err 是本次自检结果与查询失败原因。
	report, err := CheckAIReadinessWithProvider(ctx, p.store, p.keyProvider)
	if err != nil {
		p.logger.Warn("AI 启用链路自检失败", "err", err)
		return
	}
	if report.Ready() {
		if !report.ScopeConfigured {
			p.logger.Info("AI 已启用但未配置边界（ai_scope_config），当前使用内置边界", "accounts", len(report.EnabledCookieIDs))
		}
		return
	}
	p.logger.Warn("AI 启用链路自检未通过", "enabled_accounts", len(report.EnabledCookieIDs), "problems", report.Problems())
}

// WaitContext 在 ctx 约束内等待自检循环退出；探针未装配时立即返回。
func (p *AIReadinessProbe) WaitContext(ctx context.Context) error {
	if p == nil || p.done == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("等待 AI 自检探针需要关闭 Context")
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
