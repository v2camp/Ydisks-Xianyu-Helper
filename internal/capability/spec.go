package capability

// ScopeKind 是能力的作用域类型，说明该能力是否需要绑定单一账号执行。
type ScopeKind string

const (
	// ScopeGlobal 表示跨账号能力：作用于系统整体或全部账号，不需要账号作用域。
	ScopeGlobal ScopeKind = "global"
	// ScopeAccount 表示账号作用域能力：必须绑定到某个具体账号执行；
	// 客服 Agent 调用时只能作用于自身账号，由 ResolveAccountScope 强制。
	ScopeAccount ScopeKind = "account"
)

// Spec 是一个能力的静态声明，是策略求值的唯一输入依据。
//
// 声明由平台在能力注册处一次性给出，不接受调用方覆盖：
// 若允许调用方自报危险等级，权限判断即失去意义。
type Spec struct {
	// Name 是能力的稳定标识，使用 snake_case，与各传输层对外暴露的名称一致。
	Name string
	// Risk 是危险等级，决定放行与确认规则。
	Risk Risk
	// Scope 是作用域类型，决定是否需要账号绑定。
	Scope ScopeKind
	// MinPreset 是客服 Agent 可调用该能力所需的最低档位。
	// 留空表示客服 Agent 在任何档位下都不可调用，仅管理端与运营端可触达。
	MinPreset Preset
}

// Valid 报告能力声明是否自洽。
//
// 除字段取值合法外，额外拒绝「高危能力却声明了客服端档位」的矛盾声明：
// 账号与不可逆能力对客服 Agent 恒为拒绝，为其配置档位只会制造误读。
func (s Spec) Valid() bool {
	if s.Name == "" || !s.Risk.Valid() {
		return false
	}
	if s.Scope != ScopeGlobal && s.Scope != ScopeAccount {
		return false
	}
	if s.MinPreset != "" && !s.MinPreset.Valid() {
		return false
	}
	if s.Risk.elevated() && s.MinPreset != "" {
		return false
	}
	return true
}

// SupportAvailable 报告该能力是否可能被客服 Agent 调用。
//
// 返回 true 仅表示存在可调用的档位，不代表具体账号已达标；
// 实际放行仍由 Evaluate 结合账号档位判定。
func (s Spec) SupportAvailable() bool {
	return s.Valid() && s.MinPreset != ""
}
