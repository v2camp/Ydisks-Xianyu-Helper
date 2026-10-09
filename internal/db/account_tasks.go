package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// accountTaskMaxRetries 是账号自动任务在首次执行失败后允许继续尝试的最大次数。
const accountTaskMaxRetries = 5

// accountTaskMaxAttempts 是包含首次执行在内的账号自动任务最大执行次数。
const accountTaskMaxAttempts = accountTaskMaxRetries + 1

// defaultAccountDelistTime 是账号未配置时每日自动下架使用的默认本地时间。
const defaultAccountDelistTime = "09:00"

// ErrAccountTaskRetryLimit 表示账号任务已经达到自动与人工共用的重试上限。
var ErrAccountTaskRetryLimit = errors.New("账号任务已达到最大重试次数")

// AccountTaskSettings 用于本次流程后续判断的账号任务设置
type AccountTaskSettings struct {
	CookieID          string `json:"account_id"`
	AutoRateEnabled   bool   `json:"auto_rate_enabled"`
	RateContent       string `json:"rate_content"`
	AutoPolishEnabled bool   `json:"auto_polish_enabled"`
	PolishTime        string `json:"polish_time"`
	LastRateScanAt    int64  `json:"last_rate_scan_at"`
	LastPolishDate    string `json:"last_polish_date"`
	LastPolishAt      int64  `json:"last_polish_at"`
	// AutoDelistEnabled 表示是否启用每日定时下架，启用后按 DelistTime 对白名单商品执行下架。
	AutoDelistEnabled bool `json:"auto_delist_enabled"`
	// DelistTime 是每天执行下架的本地时间，格式为 HH:mm。
	DelistTime string `json:"delist_time"`
	// DelistItemIDs 是参与下架的商品白名单；空名单表示该账号不下架任何商品。
	DelistItemIDs []string `json:"delist_item_ids"`
	// LastDelistDate 是上次完成下架的日期，使用 YYYY-MM-DD 格式，用于每日去重。
	LastDelistDate string `json:"last_delist_date"`
	// LastDelistAt 是上次完成下架的 Unix 秒时间。
	LastDelistAt int64 `json:"last_delist_at"`
}

// AccountTaskRun 用于本次流程后续判断的账号任务运行
type AccountTaskRun struct {
	ID           int64  `json:"id"`
	RunKey       string `json:"run_key"`
	CookieID     string `json:"account_id"`
	TaskType     string `json:"task_type"`
	TargetID     string `json:"target_id"`
	RunDate      string `json:"run_date"`
	Status       string `json:"status"`
	SuccessCount int    `json:"success_count"`
	FailedCount  int    `json:"failed_count"`
	ErrorMessage string `json:"error_message"`
	// AttemptCount 是包含首次执行在内的累计执行次数，用于限制自动和人工重试。
	AttemptCount int   `json:"-"`
	NextRetryAt  int64 `json:"next_retry_at"`
	StartedAt    int64 `json:"started_at"`
	FinishedAt   int64 `json:"finished_at"`
}

// AccountTaskStore 用于本次流程后续判断的账号任务Store
type AccountTaskStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// defaultAccountTaskSettings 封装default账号任务设置业务协调。
func defaultAccountTaskSettings(cookieID string) AccountTaskSettings {
	return AccountTaskSettings{CookieID: cookieID, RateContent: "不错的买家，交易愉快",
		PolishTime: "03:00", DelistTime: defaultAccountDelistTime, DelistItemIDs: []string{}}
}

// Get 读取当前值。
func (s *AccountTaskStore) Get(ctx context.Context, cookieID string) (AccountTaskSettings, error) {
	// result 用于本次流程后续判断的结果
	result := defaultAccountTaskSettings(cookieID)
	// rateEnabled、polishEnabled、delistEnabled 保存三类账号任务的整数开关，零值表示关闭。
	var rateEnabled, polishEnabled, delistEnabled int
	// delistItemRaw 保存商品白名单的原始 JSON 文本，空串表示该账号未配置白名单。
	var delistItemRaw string
	// err 用于本次流程后续判断的err
	err := s.DB.QueryRowContext(ctx, `SELECT auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,
		last_rate_scan_at,last_polish_date,last_polish_at,auto_delist_enabled,delist_time,delist_item_ids,
		last_delist_date,last_delist_at FROM account_task_settings WHERE cookie_id=?`, cookieID).Scan(
		&rateEnabled, &result.RateContent, &polishEnabled, &result.PolishTime, &result.LastRateScanAt,
		&result.LastPolishDate, &result.LastPolishAt, &delistEnabled, &result.DelistTime, &delistItemRaw,
		&result.LastDelistDate, &result.LastDelistAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	result.AutoRateEnabled = rateEnabled != 0
	result.AutoPolishEnabled = polishEnabled != 0
	result.AutoDelistEnabled = delistEnabled != 0
	// 白名单解析失败时保持空名单，绝不把非法 JSON 当作可下架商品。
	if delistItemRaw != "" {
		_ = json.Unmarshal([]byte(delistItemRaw), &result.DelistItemIDs)
	}
	if strings.TrimSpace(result.DelistTime) == "" {
		result.DelistTime = defaultAccountDelistTime
	}
	return result, err
}

// Upsert 封装Upsert业务协调。
func (s *AccountTaskStore) Upsert(ctx context.Context, settings AccountTaskSettings) error {
	settings.RateContent = strings.TrimSpace(settings.RateContent)
	if settings.RateContent == "" {
		settings.RateContent = "不错的买家，交易愉快"
	}
	if settings.PolishTime == "" {
		settings.PolishTime = "03:00"
	}
	if strings.TrimSpace(settings.DelistTime) == "" {
		settings.DelistTime = defaultAccountDelistTime
	}
	// delistItemRaw 保存商品白名单序列化后的 JSON 文本；空名单写入空串避免存储无效 JSON。
	delistItemRaw := ""
	if len(settings.DelistItemIDs) > 0 {
		// encoded、marshalErr 保存白名单序列化结果与序列化错误。
		encoded, marshalErr := json.Marshal(settings.DelistItemIDs)
		if marshalErr != nil {
			return marshalErr
		}
		delistItemRaw = string(encoded)
	}
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// query 用于本次流程后续判断的查询
	query := `INSERT INTO account_task_settings
		(cookie_id,auto_rate_enabled,rate_content,auto_polish_enabled,polish_time,last_rate_scan_at,last_polish_date,last_polish_at,
		auto_delist_enabled,delist_time,delist_item_ids,last_delist_date,last_delist_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectUpsert(s.Dialect, []string{"cookie_id"}, map[string]string{
		"auto_rate_enabled": "EXCLUDED.auto_rate_enabled", "rate_content": "EXCLUDED.rate_content",
		"auto_polish_enabled": "EXCLUDED.auto_polish_enabled", "polish_time": "EXCLUDED.polish_time",
		"auto_delist_enabled": "EXCLUDED.auto_delist_enabled", "delist_time": "EXCLUDED.delist_time",
		"delist_item_ids": "EXCLUDED.delist_item_ids",
		"updated_at":      "EXCLUDED.updated_at",
	})
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, query, settings.CookieID, boolInt(settings.AutoRateEnabled), settings.RateContent,
		boolInt(settings.AutoPolishEnabled), settings.PolishTime, settings.LastRateScanAt, settings.LastPolishDate,
		settings.LastPolishAt, boolInt(settings.AutoDelistEnabled), settings.DelistTime, delistItemRaw,
		settings.LastDelistDate, settings.LastDelistAt, now, now)
	return err
}

// Enabled 封装启用状态业务协调。
func (s *AccountTaskStore) Enabled(ctx context.Context) ([]AccountTaskSettings, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, `SELECT s.cookie_id,s.auto_rate_enabled,s.rate_content,s.auto_polish_enabled,s.polish_time,
		s.last_rate_scan_at,s.last_polish_date,s.last_polish_at,
		s.auto_delist_enabled,s.delist_time,s.delist_item_ids,s.last_delist_date,s.last_delist_at
		FROM account_task_settings s JOIN cookies c ON c.id=s.cookie_id
		WHERE s.auto_rate_enabled=1 OR s.auto_polish_enabled=1 OR s.auto_delist_enabled=1 ORDER BY s.cookie_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 用于本次流程后续判断的结果
	var result []AccountTaskSettings
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row AccountTaskSettings
		// rate、polish、delist 保存三类账号任务的整数开关。
		var rate, polish, delist int
		// delistItemRaw 保存商品白名单的原始 JSON 文本。
		var delistItemRaw string
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.CookieID, &rate, &row.RateContent, &polish, &row.PolishTime,
			&row.LastRateScanAt, &row.LastPolishDate, &row.LastPolishAt,
			&delist, &row.DelistTime, &delistItemRaw, &row.LastDelistDate, &row.LastDelistAt); err != nil {
			return nil, err
		}
		row.AutoRateEnabled, row.AutoPolishEnabled, row.AutoDelistEnabled = rate != 0, polish != 0, delist != 0
		// 白名单解析失败时保持空名单，避免扫描器对非法配置执行下架。
		if delistItemRaw != "" {
			_ = json.Unmarshal([]byte(delistItemRaw), &row.DelistItemIDs)
		}
		if strings.TrimSpace(row.DelistTime) == "" {
			row.DelistTime = defaultAccountDelistTime
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// DueAutoRateOrderIDs 返回由买家确认收货 WebSocket 标记、尚可由本地自动评价任务消费的订单号。
// 它只读取本地 orders 表，绝不为了发现待评价订单向平台发起列表请求。
func (s *AccountTaskStore) DueAutoRateOrderIDs(ctx context.Context, cookieID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	// rows、err 分别保存按确认收货时间排序的本地候选订单和查询错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT order_id FROM orders
		WHERE cookie_id=? AND deleted_at IS NULL AND order_status='completed' AND completed_at<>''
		ORDER BY completed_at,order_id LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// orderIDs 保存本轮允许尝试评价的本地已确认收货订单标识。
	orderIDs := make([]string, 0)
	for rows.Next() {
		// orderID 保存当前已确认收货订单的稳定业务标识。
		var orderID string
		// err 保存读取当前订单标识时的数据库扫描错误。
		if err := rows.Scan(&orderID); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	return orderIDs, rows.Err()
}

// ClaimRun creates a run or atomically reclaims a due failed run.
// ClaimRun 封装Claim运行业务协调。
func (s *AccountTaskStore) ClaimRun(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, false)
}

// ClaimRunImmediately creates a run or immediately reclaims a failed run. It is
// intended for an explicit user retry; scheduled workers should keep using
// ClaimRun so repeated platform failures still honor their retry delay.
// ClaimRunImmediately 封装Claim运行Immediately业务协调。
func (s *AccountTaskStore) ClaimRunImmediately(ctx context.Context, run AccountTaskRun, now int64) (bool, error) {
	return s.claimRun(ctx, run, now, true)
}

// claimRun 封装claim运行业务协调。
func (s *AccountTaskStore) claimRun(ctx context.Context, run AccountTaskRun, now int64, immediate bool) (bool, error) {
	// retryCondition 限制累计执行次数、冷却时间，并排除已明确标记为永久失败的运行记录。
	retryCondition := "attempt_count<? AND next_retry_at<=? AND error_message NOT LIKE ?"
	// args 保存更新时间、运行键、最大执行次数、当前时间和永久失败前缀参数。
	args := []any{now, run.RunKey, accountTaskMaxAttempts, now, NoRetryErrorPrefix + "%"}
	if immediate {
		retryCondition = "attempt_count<? AND error_message NOT LIKE ?"
		args = []any{now, run.RunKey, accountTaskMaxAttempts, NoRetryErrorPrefix + "%"}
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status='running',attempt_count=attempt_count+1,started_at=?,finished_at=0,error_message=''
		WHERE run_key=? AND status='failed' AND `+retryCondition, args...)
	if err != nil {
		return false, err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n > 0 {
		return true, nil
	}
	// query 用于本次流程后续判断的查询
	query := dialectInsertIgnorePrefix(s.Dialect) + ` INTO account_task_runs
		(run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,error_message,attempt_count,next_retry_at,started_at,finished_at)
		VALUES(?,?,?,?,?,'running',0,0,'',1,0,?,0)` + dialectInsertIgnore(s.Dialect, []string{"run_key"})
	res, err = s.DB.ExecContext(ctx, query, run.RunKey, run.CookieID, run.TaskType, run.TargetID, run.RunDate, now)
	if err != nil {
		return false, err
	}
	// n 用于本次流程后续判断的n
	n, _ := res.RowsAffected()
	if n == 0 && immediate {
		// attemptCount、status 保存人工重试未抢占时的累计次数和运行状态，用于判断是否达到上限。
		var (
			attemptCount int
			status       string
		)
		// queryErr 保存读取人工重试状态时的数据库错误。
		if queryErr := s.DB.QueryRowContext(ctx, `SELECT attempt_count,status FROM account_task_runs WHERE run_key=?`, run.RunKey).Scan(&attemptCount, &status); queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
			return false, queryErr
		}
		if status == "failed" && attemptCount >= accountTaskMaxAttempts {
			return false, ErrAccountTaskRetryLimit
		}
	}
	return n > 0, nil
}

// FinishRun 封装Finish运行业务协调。
func (s *AccountTaskStore) FinishRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_runs SET status=?,success_count=?,failed_count=?,error_message=?,next_retry_at=CASE WHEN ?='failed' AND attempt_count>=? THEN 0 ELSE ? END,finished_at=? WHERE run_key=?`,
		status, success, failed, message, status, accountTaskMaxAttempts, nextRetryAt, time.Now().UTC().Unix(), runKey)
	return err
}

// MarkRateScan 封装MarkRateScan业务协调。
func (s *AccountTaskStore) MarkRateScan(ctx context.Context, cookieID string, at int64) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_rate_scan_at=?,updated_at=? WHERE cookie_id=?`, at, at, cookieID)
	return err
}

// MarkPolished 封装MarkPolished业务协调。
func (s *AccountTaskStore) MarkPolished(ctx context.Context, cookieID, date string, at int64) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_polish_date=?,last_polish_at=?,updated_at=? WHERE cookie_id=?`, date, at, at, cookieID)
	return err
}

// MarkDelisted 记录该账号当日已完成定时下架，供每日去重判断使用；date 使用 YYYY-MM-DD 格式。
func (s *AccountTaskStore) MarkDelisted(ctx context.Context, cookieID, date string, at int64) error {
	// err 保存写入当日下架完成标记时产生的数据库错误。
	_, err := s.DB.ExecContext(ctx, `UPDATE account_task_settings SET last_delist_date=?,last_delist_at=?,updated_at=? WHERE cookie_id=?`, date, at, at, cookieID)
	return err
}

// RecentRuns 封装Recent运行记录业务协调。
func (s *AccountTaskStore) RecentRuns(ctx context.Context, cookieID string, limit int) ([]AccountTaskRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, `SELECT id,run_key,cookie_id,task_type,target_id,run_date,status,success_count,failed_count,
		error_message,attempt_count,next_retry_at,started_at,finished_at FROM account_task_runs WHERE cookie_id=? ORDER BY id DESC LIMIT ?`, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 用于本次流程后续判断的结果
	var result []AccountTaskRun
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row AccountTaskRun
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.ID, &row.RunKey, &row.CookieID, &row.TaskType, &row.TargetID, &row.RunDate,
			&row.Status, &row.SuccessCount, &row.FailedCount, &row.ErrorMessage, &row.AttemptCount, &row.NextRetryAt,
			&row.StartedAt, &row.FinishedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// boolInt 封装boolInt业务协调。
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
