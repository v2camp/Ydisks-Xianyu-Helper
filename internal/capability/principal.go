package capability

// PrincipalKind 是调用方类型，决定策略求值走哪条规则分支。
//
// 三种类型的差异不在权限大小，而在「确认通道」与「信任边界」：
// 外部 Harness 的操作者在场可即时确认；运营 Agent 的确认只能异步回执；
// 客服 Agent 无人监督，只能靠档位与作用域约束。
type PrincipalKind string

const (
	// PrincipalRemoteHarness 表示外部 Harness 经 /mcp 调用，操作者在场、可同步确认。
	PrincipalRemoteHarness PrincipalKind = "remote_harness"
	// PrincipalOpsAgent 表示运营 Agent 经 QQ 通道调用，管理员不在场、只能异步确认。
	PrincipalOpsAgent PrincipalKind = "ops_agent"
	// PrincipalSupportAgent 表示客服 Agent 进程内调用，无人监督、作用域锁定单账号。
	PrincipalSupportAgent PrincipalKind = "support_agent"
)

// Principal 是一次调用的身份视图，由各传输层在鉴权完成后构造。
//
// 零值 Principal 不合法，求值一律拒绝；调用方必须在执行前确认 Valid 为真。
type Principal struct {
	// Kind 是调用方类型，必填且必须为本包登记的取值。
	Kind PrincipalKind
	// UserID 是数据归属用户标识，用于应用层归属校验；0 表示未知，一律不合法。
	UserID int64
	// CookieID 是账号作用域标识。客服 Agent 必须非空以锁定单账号；
	// 管理端与运营端留空表示可跨账号，非空时表示本次调用限定在该账号内。
	CookieID string
	// Preset 是客服 Agent 生效档位；管理端与运营端不使用该字段，留空即可。
	Preset Preset
}

// Valid 报告身份是否满足该类型的最小约束。
//
// 客服 Agent 额外要求账号作用域与合法档位同时具备，缺一则不合法：
// 缺少账号会退化为跨账号操作，缺少档位会退化为无能力上限。
func (p Principal) Valid() bool {
	switch p.Kind {
	case PrincipalRemoteHarness, PrincipalOpsAgent:
		return p.UserID != 0
	case PrincipalSupportAgent:
		return p.UserID != 0 && p.CookieID != "" && p.Preset.Valid()
	default:
		return false
	}
}

// CrossAccount 报告该身份是否可以作用于任意账号。
//
// 仅管理端与运营端可跨账号；客服 Agent 恒为 false，其作用域由 ResolveAccountScope 强制。
func (p Principal) CrossAccount() bool {
	switch p.Kind {
	case PrincipalRemoteHarness, PrincipalOpsAgent:
		return true
	default:
		return false
	}
}
