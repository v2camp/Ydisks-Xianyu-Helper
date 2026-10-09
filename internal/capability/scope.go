package capability

import "errors"

// ErrScopeViolation 表示调用方试图触达超出自身权限的账号或使用了非法身份。
//
// 该错误对外的呈现必须由调用方决定，不得直接透传给终端用户：
// 客服 Agent 场景下需转为对本方账号的自然语言答复，而非暴露内部结构。
var ErrScopeViolation = errors.New("能力作用域校验失败")

// ResolveAccountScope 解析本次调用实际作用的账号标识。
//
// 规则按能力作用域与调用方类型两级判定：
//   - 跨账号能力不绑定具体账号，返回空值；客服 Agent 调用它也不会因此获得跨账号数据；
//   - 账号作用域能力下，客服 Agent 被强制锁定在自身账号，请求其他账号一律拒绝，
//     未指定账号时回落自身账号；
//   - 管理端与运营端可跨账号，原样返回请求值，空值表示不限定账号。
//
// 身份或声明不合法时返回 ErrScopeViolation，调用方必须终止本次执行。
func ResolveAccountScope(p Principal, spec Spec, requestedCookieID string) (string, error) {
	if !p.Valid() || !spec.Valid() {
		return "", ErrScopeViolation
	}
	if spec.Scope == ScopeGlobal {
		return "", nil
	}
	if p.CrossAccount() {
		return requestedCookieID, nil
	}
	if requestedCookieID != "" && requestedCookieID != p.CookieID {
		return "", ErrScopeViolation
	}
	return p.CookieID, nil
}
