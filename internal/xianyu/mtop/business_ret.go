package mtop

import "strings"

// mtopBusinessRetCodes 列举平台已确认属于业务拒绝、但不带 FAIL_BIZ 前缀的稳定错误码。
// 线上实测确认发货接口会直接返回 ORDER_STATUS_ERROR::订单状态不正确；若把它当作系统错误，
// 调用方会把明确拒绝误判为「结果未知」并隔离运行，因此必须纳入业务终态语义。
// 只登记有实测证据的稳定错误码，避免把可重试的系统错误误判为终态业务拒绝。
var mtopBusinessRetCodes = []string{
	"ORDER_STATUS_ERROR",
	"ORDER_ALREADY_DELIVERY",
}

// isMTopBusinessRet 判断 ret 是否明确声明平台普通业务结果。
// FAIL_BIZ 前缀与已登记的稳定业务错误码都进入终态业务语义；
// 网关和 FAIL_SYS 系统错误仍保持 HTTP 或系统错误分类，以便调用方继续重试。
func isMTopBusinessRet(ret []string) bool {
	// value 表示当前待判断的 MTOP 返回标记。
	for _, value := range ret {
		// upper 保存大写后的返回标记，用于同时匹配前缀和稳定错误码。
		upper := strings.ToUpper(value)
		if strings.Contains(upper, "FAIL_BIZ_") {
			return true
		}
		// code 表示当前遍历到的已登记业务错误码。
		for _, code := range mtopBusinessRetCodes {
			if strings.Contains(upper, code) {
				return true
			}
		}
	}
	return false
}
