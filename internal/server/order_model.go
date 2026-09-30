package server

import (
	"encoding/json"
	"strings"
	"time"
)

// deliverySLAConfig 是发货 SLA 设置的解析结果。
type deliverySLAConfig struct {
	// DefaultMinutes 是默认发货 SLA 分钟数；0 表示未启用。
	DefaultMinutes int
	// PerAccount 是按账号覆盖的 SLA 分钟数，键为 cookie_id。
	PerAccount map[string]int
}

// parseDeliverySLAConfig 解析 delivery_sla_config 设置值。
// 空串、非法 JSON 或字段类型不匹配时返回空配置（SLA 关闭），不返回错误以避免阻断列表。
func parseDeliverySLAConfig(raw string) deliverySLAConfig {
	// trimmed 是去除首尾空白后的设置原文。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return deliverySLAConfig{}
	}
	// parsed 是按契约形状解析的中间结构；default_minutes 用指针区分缺省与零值。
	var parsed struct {
		DefaultMinutes *int           `json:"default_minutes"`
		PerAccount     map[string]int `json:"per_account"`
	}
	// err 是设置值 JSON 解析错误；非法内容直接降级为空配置。
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return deliverySLAConfig{}
	}
	// cfg 保存最终配置结果。
	cfg := deliverySLAConfig{PerAccount: parsed.PerAccount}
	if parsed.DefaultMinutes != nil && *parsed.DefaultMinutes > 0 {
		cfg.DefaultMinutes = *parsed.DefaultMinutes
	}
	// 过滤 per_account 中的非正值，保持与默认值相同的关闭语义。
	if cfg.PerAccount != nil {
		// filtered 保存仅含正整数覆盖的映射。
		filtered := make(map[string]int, len(cfg.PerAccount))
		// id 是账号标识，minutes 是覆盖的分钟数。
		for id, minutes := range cfg.PerAccount {
			if minutes > 0 {
				filtered[id] = minutes
			}
		}
		cfg.PerAccount = filtered
	}
	return cfg
}

// nullableOrderTime 将订单时间文本转换为可空响应字段。
// 空白值映射为 nil（JSON null），非空值经时间归一后返回指针。
func nullableOrderTime(raw string) *string {
	// trimmed 是去除首尾空白后的时间文本。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	// normalized 是归一到 RFC3339Nano 的时间文本；无法识别时保留原值。
	normalized := normalizeOrderTimestamp(trimmed)
	return &normalized
}

// applyDeliverySLA 按 SLA 配置为订单响应补充 sla_minutes 与 sla_deadline。
// 未启用、paid_at 缺失或时间解析失败时两字段均降级为 0/空。
func applyDeliverySLA(dto *orderDTO, cfg deliverySLAConfig) {
	// minutes 是按订单账号取覆盖后的生效 SLA 分钟数。
	minutes := cfg.DefaultMinutes
	if cfg.PerAccount != nil {
		// override、ok 读取当前账号的 SLA 覆盖值及其存在性。
		override, ok := cfg.PerAccount[dto.CookieID]
		if ok {
			minutes = override
		}
	}
	// SLA 关闭或无付款时间时直接清零。
	if minutes <= 0 || dto.PaidAt == nil {
		dto.SLAMinutes = 0
		dto.SLADeadline = nil
		return
	}
	// paid 是解析后的付款时刻；解析失败时降级为未启用。
	paid, ok := parseOrderTime(*dto.PaidAt)
	if !ok {
		dto.SLAMinutes = 0
		dto.SLADeadline = nil
		return
	}
	// deadline 是付款时刻加上 SLA 分钟数后的截止时刻，按数据库存储格式输出。
	deadline := paid.Add(time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339Nano)
	dto.SLAMinutes = minutes
	dto.SLADeadline = &deadline
}

// parseOrderTime 解析订单时间文本，兼容数据库旧格式与 RFC3339 系列。
// 解析失败时返回 false，由调用方降级处理。
func parseOrderTime(raw string) (time.Time, bool) {
	// value 是去除首尾空白后的时间文本。
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	// layouts 是兼容数据库旧格式和已带时区时间的解析模板集合。
	layouts := []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"}
	// layout 是当前尝试解析的模板。
	for _, layout := range layouts {
		// parsed 是按数据库 UTC 约定或原始时区解析后的时间值。
		var parsed time.Time
		// parseErr 保存当前模板解析失败的原因。
		var parseErr error
		if strings.Contains(layout, "Z07:00") {
			parsed, parseErr = time.Parse(layout, value)
		} else {
			parsed, parseErr = time.ParseInLocation(layout, value, time.UTC)
		}
		if parseErr == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
