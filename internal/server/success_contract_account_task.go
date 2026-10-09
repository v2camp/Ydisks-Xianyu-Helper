package server

// accountTaskSettingsResponse 是账号任务设置接口的具名 DTO。
type accountTaskSettingsResponse struct {
	// AccountID 是账号稳定标识。
	AccountID string `json:"account_id"`
	// AutoRateEnabled 表示自动评价是否启用。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是自动评价文案。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 表示自动擦亮是否启用。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是自动擦亮本地时间。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是最近一次评价扫描时间。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是最近一次擦亮日期。
	LastPolishDate string `json:"last_polish_date"`
	// LastPolishAt 是最近一次擦亮时间。
	LastPolishAt int64 `json:"last_polish_at"`
	// AutoDelistEnabled 表示每日定时下架是否启用。
	AutoDelistEnabled bool `json:"auto_delist_enabled"`
	// DelistTime 是每日定时下架的本地时间。
	DelistTime string `json:"delist_time"`
	// DelistItemIDs 是参与下架的商品白名单。
	DelistItemIDs []string `json:"delist_item_ids"`
	// LastDelistDate 是最近一次下架日期。
	LastDelistDate string `json:"last_delist_date"`
	// LastDelistAt 是最近一次下架时间。
	LastDelistAt int64 `json:"last_delist_at"`
}

// accountTaskRunResponse 是账号任务执行记录的具名 DTO。
type accountTaskRunResponse struct {
	// ID 是任务执行记录主键。
	ID int64 `json:"id"`
	// RunKey 是任务幂等键。
	RunKey string `json:"run_key"`
	// AccountID 是账号稳定标识。
	AccountID string `json:"account_id"`
	// TaskType 是任务类型。
	TaskType string `json:"task_type"`
	// TargetID 是任务目标标识。
	TargetID string `json:"target_id"`
	// RunDate 是任务业务日期。
	RunDate string `json:"run_date"`
	// Status 是任务执行状态。
	Status string `json:"status"`
	// SuccessCount 是任务成功数量。
	SuccessCount int `json:"success_count"`
	// FailedCount 是任务失败数量。
	FailedCount int `json:"failed_count"`
	// ErrorMessage 是任务失败说明。
	ErrorMessage string `json:"error_message"`
	// NextRetryAt 是下一次重试时间。
	NextRetryAt int64 `json:"next_retry_at"`
	// StartedAt 是任务开始时间。
	StartedAt int64 `json:"started_at"`
	// FinishedAt 是任务完成时间。
	FinishedAt int64 `json:"finished_at"`
}

// accountTaskSummaryResponse 是手动执行账号任务的统计 DTO。
type accountTaskSummaryResponse struct {
	// TaskType 是任务类型。
	TaskType string `json:"task_type"`
	// Found 是发现的目标数量。
	Found int `json:"found"`
	// Success 是成功处理数量。
	Success int `json:"success"`
	// Failed 是失败处理数量。
	Failed int `json:"failed"`
	// Skipped 是跳过数量。
	Skipped int `json:"skipped"`
	// Message 是任务结果说明。
	Message string `json:"message,omitempty"`
}

// accountTaskRunResponseEnvelope 是手动执行账号任务的具名响应 DTO。
type accountTaskRunResponseEnvelope struct {
	// Success 表示任务请求是否成功完成。
	Success bool `json:"success"`
	// Summary 是账号任务执行统计。
	Summary accountTaskSummaryResponse `json:"summary"`
}

// accountTaskRunsResponse 是账号任务执行记录列表的具名响应 DTO。
type accountTaskRunsResponse struct {
	// Runs 是当前账号最近的任务执行记录。
	Runs []accountTaskRunResponse `json:"runs"`
}
