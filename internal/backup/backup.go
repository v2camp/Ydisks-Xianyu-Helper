// Package backup 实现卡密与自动化规则的定时快照备份。
//
// 备份对象只有两张业务资产：cards（卡密，api_config 保留密文原样）与
// automation_rules（规则及动作，config_json 原样）。备份文件落在本机数据目录
// 的 backups 子目录下，保留最近若干份；不做任何平台（闲鱼）请求，也不上传。
//
// 红线：备份文件包含加密 API 模板与规则配置，属于敏感数据——绝不写日志、
// 绝不进入 HTTP 响应；文件权限 0600，仅本进程与管理员可读。
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Snapshot 是一次备份快照的 JSON 形状；Version 用于未来格式演进时识别。
type Snapshot struct {
	// Version 是快照格式版本；当前恒为 1。
	Version int `json:"version"`
	// GeneratedAt 是快照生成时刻（UTC）；由可注入时钟提供。
	GeneratedAt time.Time `json:"generated_at"`
	// Cards 是卡密原始行；APIConfig 为密文原样，本包不做解密。
	Cards []CardRecord `json:"cards"`
	// AutomationRules 是自动化规则及动作；ConfigJSON 原样保留。
	AutomationRules []RuleRecord `json:"automation_rules"`
}

// CardRecord 是卡密备份记录；字段与数据库列一一对应，恢复时可直接写回。
type CardRecord struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	APIConfig    string `json:"api_config"`
	TextContent  string `json:"text_content"`
	DataContent  string `json:"data_content"`
	ImageURL     string `json:"image_url"`
	Description  string `json:"description"`
	Enabled      bool   `json:"enabled"`
	DelaySeconds int    `json:"delay_seconds"`
	IsMultiSpec  bool   `json:"is_multi_spec"`
	SpecName     string `json:"spec_name"`
	SpecValue    string `json:"spec_value"`
	UserID       int64  `json:"user_id"`
}

// RuleRecord 是自动化规则备份记录；Actions 是该规则下的有序动作。
type RuleRecord struct {
	ID                 int64          `json:"id"`
	UserID             int64          `json:"user_id"`
	CookieID           string         `json:"cookie_id"`
	ItemID             string         `json:"item_id"`
	Name               string         `json:"name"`
	TriggerType        string         `json:"trigger_type"`
	Enabled            bool           `json:"enabled"`
	Priority           int            `json:"priority"`
	ConfigJSON         string         `json:"config_json"`
	SKUMigrationStatus string         `json:"sku_migration_status"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	Actions            []ActionRecord `json:"actions"`
}

// ActionRecord 是规则动作备份记录；字段与动作表列对应。
type ActionRecord struct {
	ID              int64  `json:"id"`
	ActionType      string `json:"action_type"`
	CardID          int64  `json:"card_id"`
	CardName        string `json:"card_name"`
	DeliveryCount   int    `json:"delivery_count"`
	MessageTemplate string `json:"message_template"`
	DelaySeconds    int    `json:"delay_seconds"`
	ConfigJSON      string `json:"config_json"`
	Enabled         bool   `json:"enabled"`
	SortOrder       int    `json:"sort_order"`
}

// Source 定义备份服务所需的最小读取能力；由组合层用 db 仓储投影实现，测试注入假实现。
// 接口刻意不暴露数据库连接，保证备份服务只有读取与写本机文件两类副作用。
type Source interface {
	// Cards 导出全部卡密原始行（APIConfig 密文原样）。
	Cards(ctx context.Context) ([]CardRecord, error)
	// AutomationRules 导出全部自动化规则及动作。
	AutomationRules(ctx context.Context) ([]RuleRecord, error)
	// IntervalHours 读取用户配置的备份间隔（小时）；0 表示关闭定时备份。
	IntervalHours(ctx context.Context) (int, error)
}

// checkInterval 是备份服务检查是否到点的心跳周期；间隔变更最迟一个周期内生效。
const checkInterval = time.Hour

// defaultIntervalHours 是设置缺失或非法时的默认备份间隔（小时）。
const defaultIntervalHours = 24

// keepFiles 是本地保留的备份文件数量上限；超出时按文件名（即时间）淘汰最旧的。
const keepFiles = 14

// filePrefix 是备份文件名前缀；文件名即按时间排序的依据。
const filePrefix = "cards-rules-"

// Service 周期性把卡密与自动化规则快照写入本机数据目录。
//
// 并发与生命周期（AGENTS 1.6）：
//   - 归属：由组合根登记为生命周期组件，FuncComponent 的 StartFunc 启 goroutine、
//     CloseFunc 等待退出；本服务不自行创建无人管的协程。
//   - 取消：Run 持有共享生命周期 ctx，ctx 取消即停止循环并关闭 done。
//   - 等待：WaitContext 在 done 关闭前阻塞，关闭流程据此 Join。
//   - 时钟：now 可注入，测试用假时钟驱动判定。
//   - 失败语义：单次备份失败只记告警（fail-open），不影响业务与下次重试。
//   - 检查粒度：每 checkInterval 醒来一次读取间隔设置，间隔变更最迟一个周期生效；
//     设为 0 表示用户关闭定时备份，服务保持运行但不产出文件。
type Service struct {
	// source 是卡密与规则的读取边界；为 nil 时服务视为未装配，Run 直接返回。
	source Source
	// dir 是备份文件的落盘目录；空串表示未装配。
	dir string
	// runOnce 保证一个 Service 只启动一个由 ctx 管理的循环。
	runOnce sync.Once
	// done 在备份循环退出后关闭，供关闭流程等待 goroutine 收束。
	done chan struct{}
	// now 返回当前时间；生产为 time.Now，测试注入假时钟。
	now func() time.Time
	// checkEvery 是到点检查周期；生产为 checkInterval，测试注入更短周期驱动间隔判定。
	checkEvery time.Duration
	// logger 记录备份失败；不包含备份内容与任何凭证。
	logger *slog.Logger
}

// NewService 构造定时备份服务；source 为 nil 或 dir 为空时返回 nil 表示未装配。
// 间隔本身来自 Source.IntervalHours 的运行期读取，不在构造期固化，便于设置即时生效。
func NewService(source Source, dir string, logger *slog.Logger) *Service {
	if source == nil || dir == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		source: source,
		dir:    dir,
		done:   make(chan struct{}),
		now:    time.Now,
		logger: logger,
	}
}

// Run 启动定时备份循环；调用方应在 goroutine 中启动并用 ctx 控制生命周期。
// 启动即先备份一次，保证新部署或刚恢复数据后立刻有一份离线快照。
func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	if s.source == nil || s.dir == "" {
		// 未装配时视作「已结束」：关闭 done 让关闭流程的 Join 立即返回，
		// 否则 WaitContext 会永久阻塞在一个永远不会开始的循环上。
		s.runOnce.Do(func() {
			if s.done != nil {
				close(s.done)
			}
		})
		return
	}
	s.runOnce.Do(func() {
		// 无论循环因何退出，都必须关闭 done，供 WaitContext 解除等待避免 goroutine 泄漏。
		defer close(s.done)
		if ctx.Err() != nil {
			return
		}
		// last 记录上次成功备份时刻；启动视为零值，首个检查点即满足间隔。
		var last time.Time
		// 首次运行立即产出快照，与心跳写者的「启动即打点」策略一致。
		if s.snapshot(ctx) {
			last = s.now()
		}
		// checkEvery 是到点检查周期；未显式配置时使用默认值，测试可注入更短周期。
		checkEvery := s.checkEvery
		if checkEvery <= 0 {
			checkEvery = checkInterval
		}
		// ticker 按检查周期推进；到点后再按用户间隔判断是否真正执行备份。
		ticker := time.NewTicker(checkEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// ctx 取消即停止备份循环并退出 goroutine，不做任何阻塞等待。
				return
			case <-ticker.C:
				// intervalHours 是当前配置的备份间隔；0 表示用户关闭定时备份。
				intervalHours, err := s.source.IntervalHours(ctx)
				if err != nil {
					// 读不到设置按默认间隔处理，避免一次数据库抖动导致备份停摆。
					s.logger.Warn("读取备份间隔失败，本轮按默认间隔判断", "err", err)
					intervalHours = defaultIntervalHours
				}
				// due 表示本轮是否已到备份时刻；intervalHours<=0 关闭时永不触发。
				due := intervalHours > 0 && s.now().Sub(last) >= time.Duration(intervalHours)*time.Hour
				if due && s.snapshot(ctx) {
					last = s.now()
				}
			}
		}
	})
}

// snapshot 执行一次备份并落盘；返回是否成功。失败只记告警，不向循环上抛。
func (s *Service) snapshot(ctx context.Context) bool {
	// cards、rules 是两份业务资产；任一读取失败即放弃本次快照，等待下个周期重试。
	cards, err := s.source.Cards(ctx)
	if err != nil {
		s.logger.Warn("读取卡密备份失败", "err", err)
		return false
	}
	// rules 是自动化规则读取结果；失败同样放弃本次快照。
	rules, err := s.source.AutomationRules(ctx)
	if err != nil {
		s.logger.Warn("读取自动化规则备份失败", "err", err)
		return false
	}
	// snap 是组装出的快照对象；GeneratedAt 使用可注入时钟，测试断言稳定。
	snap := Snapshot{Version: 1, GeneratedAt: s.now().UTC(), Cards: cards, AutomationRules: rules}
	// payload 是序列化后的备份内容；含加密模板与规则配置，绝不写入日志。
	payload, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		s.logger.Warn("序列化备份快照失败", "err", err)
		return false
	}
	// name 是备份文件名；精确到秒避免同秒覆盖。
	name := filePrefix + s.now().Format("20060102-150405") + ".json"
	// writeErr 是写文件错误；0600 限制只有本进程与管理员可读。
	writeErr := os.WriteFile(filepath.Join(s.dir, name), payload, 0o600)
	if writeErr != nil {
		s.logger.Warn("写入备份文件失败", "file", name, "err", writeErr)
		return false
	}
	// pruneErr 是历史备份淘汰错误；淘汰失败不影响本次备份有效性。
	if pruneErr := s.prune(); pruneErr != nil {
		s.logger.Warn("清理历史备份失败", "err", pruneErr)
	}
	s.logger.Info("卡密与自动化规则备份完成", "file", name, "cards", len(cards), "rules", len(rules))
	return true
}

// prune 按 keepFiles 上限淘汰最旧的备份文件；目录不存在时静默返回。
func (s *Service) prune() error {
	// entries 是备份目录的全部条目；目录尚不存在视为无可清理。
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// names 收集本服务产出的备份文件名；其他文件一律不动。
	var names []string
	// entry 是当前遍历的目录条目；仅收集本服务产出的 json 备份文件。
	for _, entry := range entries {
		if entry.Type().IsRegular() && len(entry.Name()) > len(filePrefix) &&
			filepath.Ext(entry.Name()) == ".json" && entry.Name()[:len(filePrefix)] == filePrefix {
			names = append(names, entry.Name())
		}
	}
	// 超量时按文件名升序淘汰（文件名含时间戳，升序即最旧在前）。
	if len(names) <= keepFiles {
		return nil
	}
	// sortNames 是按字典序整理后的文件名切片；文件名含时间戳，字典序即时间序。
	sort.Strings(names)
	// overflow 是需要删除的最旧文件数量。
	overflow := len(names) - keepFiles
	// name 是当前待删除的过期备份文件名；删除失败只记录不影响其余淘汰。
	for _, name := range names[:overflow] {
		// removeErr 是删除单个过期备份的文件系统错误。
		if removeErr := os.Remove(filepath.Join(s.dir, name)); removeErr != nil {
			return fmt.Errorf("删除过期备份 %s: %w", name, removeErr)
		}
	}
	return nil
}

// WaitContext 在 ctx 约束内等待备份循环退出，避免关闭流程无限阻塞。
// Service 未装配或已退出时立即返回 nil。
func (s *Service) WaitContext(ctx context.Context) error {
	if s == nil || s.done == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("等待备份服务需要关闭 Context")
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
