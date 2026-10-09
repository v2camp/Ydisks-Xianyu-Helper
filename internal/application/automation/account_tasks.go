// Package automation 提供自动化业务用例的纯应用层编排。
package automation

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// ErrUnavailable 表示账号任务运行所需的自动化中心尚未装配。
var ErrUnavailable = errors.New("自动化中心未启用")

// ErrInvalidTaskType 表示请求未使用受支持的账号任务类型。
var ErrInvalidTaskType = errors.New("不支持的任务类型")

// TaskAutoRate 表示自动评价任务。
const TaskAutoRate = "auto_rate"

// TaskAutoPolish 表示商品擦亮任务。
const TaskAutoPolish = "auto_polish"

// TaskAutoDelist 表示每日定时下架任务。
const TaskAutoDelist = "auto_delist"

// accountTaskTimePattern 校验账号任务每天执行的本地时间格式。
var accountTaskTimePattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

// accountTaskDelistTimeDefault 是下架时间缺省时使用的默认本地时间，与数据库默认值保持一致。
const accountTaskDelistTimeDefault = "09:00"

// accountTaskDelistMaxItems 限制单个账号的下架白名单规模，避免一次任务对平台发起过多请求。
const accountTaskDelistMaxItems = 200

// AccountTaskSettings 是不含数据库模型的账号任务设置。
type AccountTaskSettings struct {
	// CookieID 是设置所属的账号标识。
	CookieID string `json:"account_id"`
	// AutoRateEnabled 表示是否启用自动评价。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是自动评价使用的文字内容。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 表示是否启用商品擦亮。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是每天执行擦亮的本地时间，格式为 HH:mm。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是上次扫描待评价订单的 Unix 秒时间。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是上次擦亮日期，使用 YYYY-MM-DD 格式。
	LastPolishDate string `json:"last_polish_date"`
	// LastPolishAt 是上次擦亮完成的 Unix 秒时间。
	LastPolishAt int64 `json:"last_polish_at"`
	// AutoDelistEnabled 表示是否启用每日定时下架。
	AutoDelistEnabled bool `json:"auto_delist_enabled"`
	// DelistTime 是每天执行下架的本地时间，格式为 HH:mm。
	DelistTime string `json:"delist_time"`
	// DelistItemIDs 是参与下架的商品白名单；空名单表示不下架任何商品。
	DelistItemIDs []string `json:"delist_item_ids"`
	// LastDelistDate 是上次下架日期，使用 YYYY-MM-DD 格式。
	LastDelistDate string `json:"last_delist_date"`
	// LastDelistAt 是上次下架完成的 Unix 秒时间。
	LastDelistAt int64 `json:"last_delist_at"`
}

// AccountTaskRun 是面向应用层的账号任务运行记录。
type AccountTaskRun struct {
	// ID 是运行记录的持久化标识。
	ID int64 `json:"id"`
	// RunKey 是用于幂等去重的运行键。
	RunKey string `json:"run_key"`
	// CookieID 是运行所属的账号标识。
	CookieID string `json:"account_id"`
	// TaskType 是账号任务类型。
	TaskType string `json:"task_type"`
	// TargetID 是本次运行对应的平台对象标识。
	TargetID string `json:"target_id"`
	// RunDate 是任务业务日期。
	RunDate string `json:"run_date"`
	// Status 是运行当前状态。
	Status string `json:"status"`
	// SuccessCount 是外部动作成功数量。
	SuccessCount int `json:"success_count"`
	// FailedCount 是外部动作失败数量。
	FailedCount int `json:"failed_count"`
	// ErrorMessage 是供人工核对的非敏感错误摘要。
	ErrorMessage string `json:"error_message"`
	// NextRetryAt 是下一次重试的 Unix 秒时间。
	NextRetryAt int64 `json:"next_retry_at"`
	// StartedAt 是运行开始的 Unix 秒时间。
	StartedAt int64 `json:"started_at"`
	// FinishedAt 是运行结束的 Unix 秒时间。
	FinishedAt int64 `json:"finished_at"`
}

// TaskSummary 是手动执行账号任务后的统计结果。
type TaskSummary struct {
	// TaskType 是本次执行的任务类型。
	TaskType string `json:"task_type"`
	// Found 是发现的待处理对象数量。
	Found int `json:"found"`
	// Success 是外部动作成功数量。
	Success int `json:"success"`
	// Failed 是外部动作失败数量。
	Failed int `json:"failed"`
	// Skipped 是因状态或幂等检查而跳过的数量。
	Skipped int `json:"skipped"`
	// Message 是面向操作人员的非敏感执行说明。
	Message string `json:"message,omitempty"`
}

// Repository 定义账号任务设置和历史记录所需的最小持久化能力。
type Repository interface {
	// GetSettings 读取账号任务设置，未创建设置时返回默认值。
	GetSettings(ctx context.Context, accountID string) (AccountTaskSettings, error)
	// SaveSettings 保存账号任务设置。
	SaveSettings(ctx context.Context, settings AccountTaskSettings) error
	// ListRuns 返回账号最近的任务运行记录。
	ListRuns(ctx context.Context, accountID string, limit int) ([]AccountTaskRun, error)
}

// Runner 定义手动触发账号任务所需的运行能力。
type Runner interface {
	// RunAccountTask 执行指定账号的一个自动化任务。
	RunAccountTask(ctx context.Context, accountID, taskType string) (TaskSummary, error)
}

// Service 编排账号任务的设置、历史和手动执行用例。
type Service struct {
	// repository 提供账号任务持久化能力。
	repository Repository
	// runner 提供自动化中心的手动执行能力。
	runner Runner
}

// NewService 构造账号任务应用服务。
func NewService(repository Repository, runner Runner) *Service {
	return &Service{repository: repository, runner: runner}
}

// GetSettings 读取指定账号的账号任务设置。
func (s *Service) GetSettings(ctx context.Context, accountID string) (AccountTaskSettings, error) {
	if s == nil || s.repository == nil {
		return AccountTaskSettings{}, errors.New("账号任务存储未初始化")
	}
	return s.repository.GetSettings(ctx, strings.TrimSpace(accountID))
}

// UpdateSettings 校验并保存账号任务设置，再读取数据库中的最终值。
func (s *Service) UpdateSettings(ctx context.Context, settings AccountTaskSettings) (AccountTaskSettings, error) {
	if s == nil || s.repository == nil {
		return AccountTaskSettings{}, errors.New("账号任务存储未初始化")
	}
	settings.CookieID = strings.TrimSpace(settings.CookieID)
	settings.RateContent = strings.TrimSpace(settings.RateContent)
	if settings.CookieID == "" {
		return AccountTaskSettings{}, errors.New("账号标识不能为空")
	}
	if settings.AutoRateEnabled && settings.RateContent == "" {
		return AccountTaskSettings{}, errors.New("启用自动评价时评价内容不能为空")
	}
	if len([]rune(settings.RateContent)) > 500 {
		return AccountTaskSettings{}, errors.New("评价内容不能超过 500 个字符")
	}
	if !accountTaskTimePattern.MatchString(settings.PolishTime) {
		return AccountTaskSettings{}, errors.New("擦亮时间格式必须为 HH:mm")
	}
	// 下架时间缺省时回落到默认值；只有显式填写非法时间才拒绝。
	if strings.TrimSpace(settings.DelistTime) == "" {
		settings.DelistTime = accountTaskDelistTimeDefault
	} else if !accountTaskTimePattern.MatchString(settings.DelistTime) {
		return AccountTaskSettings{}, errors.New("下架时间格式必须为 HH:mm")
	}
	// normalizedDelist 保存清洗后的商品白名单，去除空项和重复项并限制规模。
	normalizedDelist, delistErr := normalizeAccountDelistItemIDs(settings.DelistItemIDs)
	if delistErr != nil {
		return AccountTaskSettings{}, delistErr
	}
	settings.DelistItemIDs = normalizedDelist
	// err 表示账号任务设置写入失败。
	if err := s.repository.SaveSettings(ctx, settings); err != nil {
		return AccountTaskSettings{}, err
	}
	return s.repository.GetSettings(ctx, settings.CookieID)
}

// normalizeAccountDelistItemIDs 清洗下架白名单：去除空白与重复项，并限制最大条目数。
func normalizeAccountDelistItemIDs(itemIDs []string) ([]string, error) {
	// result 保存去重后的商品标识，保持输入顺序以保证前端展示稳定。
	result := make([]string, 0, len(itemIDs))
	// seen 记录已收录的商品标识，避免重复下架同一商品。
	seen := make(map[string]struct{}, len(itemIDs))
	// itemID 是当前待清洗的白名单项。
	for _, itemID := range itemIDs {
		// trimmed 保存去除首尾空白后的商品标识。
		trimmed := strings.TrimSpace(itemID)
		if trimmed == "" {
			continue
		}
		if // exists 表示该商品标识是否已经收录。
		_, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
		if len(result) > accountTaskDelistMaxItems {
			return nil, errors.New("下架商品白名单不能超过 200 个")
		}
	}
	return result, nil
}

// ListRuns 查询指定账号最近的任务运行记录。
func (s *Service) ListRuns(ctx context.Context, accountID string, limit int) ([]AccountTaskRun, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("账号任务存储未初始化")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repository.ListRuns(ctx, strings.TrimSpace(accountID), limit)
}

// Run 手动执行指定账号的账号任务。
func (s *Service) Run(ctx context.Context, accountID, taskType string) (TaskSummary, error) {
	if s == nil || s.runner == nil {
		return TaskSummary{}, ErrUnavailable
	}
	if taskType != TaskAutoRate && taskType != TaskAutoPolish && taskType != TaskAutoDelist {
		return TaskSummary{}, ErrInvalidTaskType
	}
	return s.runner.RunAccountTask(ctx, strings.TrimSpace(accountID), taskType)
}
