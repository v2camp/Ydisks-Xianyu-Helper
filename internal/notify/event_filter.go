package notify

import (
	"encoding/json"
	"strings"
)

// eventAllowed 判断渠道订阅是否允许当前事件类型；空订阅表示接收全部事件。
func eventAllowed(raw, eventType string) (bool, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return true, nil
	}
	// events、err 用于本次流程后续判断的events、err
	events, err := parseEventTypes(raw)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return true, nil
	}
	if events[eventType] {
		return true, nil
	}
	// delivery_result 是旧版统一交易开关；保留它对新四类自动化事件、人工发货结果和人工处理告警的兼容放行。
	if events[EventDeliveryResult] && (isAutomationEventType(eventType) || eventType == EventManualDeliveryResult || eventType == EventManualInterventionRequired) {
		return true, nil
	}
	return false, nil
}

// eventAllowedWithFallbacks 先匹配事件实际类别，再匹配来源类别，保证新增人工处理分类不会切断旧自动化订阅。
func eventAllowedWithFallbacks(raw, eventType string, fallbackTypes []string) (bool, error) {
	// allowed、err 保存实际事件类别的订阅判断结果。
	allowed, err := eventAllowed(raw, eventType)
	if err != nil || allowed {
		return allowed, err
	}
	// fallbackType 表示当前兼容检查的来源事件类别。
	for _, fallbackType := range fallbackTypes {
		allowed, err = eventAllowed(raw, fallbackType)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

// isAutomationEventType 判断事件是否属于四类可单独控制的自动化任务。
func isAutomationEventType(eventType string) bool {
	switch eventType {
	case EventAutomationOrderCreated, EventAutomationOrderPaid, EventAutomationBuyerReviewed, EventAutomationReviewMissingTimeout:
		return true
	default:
		return false
	}
}

// parseEventTypes 解析渠道订阅配置，兼容 JSON 数组与逗号/分号/空白分隔的历史文本。
func parseEventTypes(raw string) (map[string]bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// arr 用于本次流程后续判断的arr
	var arr []string
	if strings.HasPrefix(raw, "[") {
		// err 用于本次流程后续判断的err
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
	} else {
		arr = strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
		})
	}
	if len(arr) == 0 {
		return nil, nil
	}
	// out 用于本次流程后续判断的out
	out := make(map[string]bool, len(arr))
	// v 表示当前遍历过程中的v
	for _, v := range arr {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = true
		}
	}
	return out, nil
}

// sortStrings 原地排序字符串切片，保证格式化字段顺序稳定。
func sortStrings(values []string) {
	// i 是待插入元素的下标。
	for i := 1; i < len(values); i++ {
		// j 是当前向前比较的位置。
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}
