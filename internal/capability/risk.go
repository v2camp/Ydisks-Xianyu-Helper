package capability

// Risk 是能力的危险等级，决定该能力对各类调用方是否放行以及需要何种确认。
//
// 等级按影响面递增：只读 < 报价 < 写入 < 账号 < 不可逆。
// 等级是能力的一等属性，必须由平台在能力声明处指定，不得由调用方传入或推断。
type Risk string

const (
	// RiskRead 表示只读能力：不产生任何业务数据变更，重复调用无副作用。
	RiskRead Risk = "read"
	// RiskQuote 表示生成承诺类能力：产出报价、预留或对外承诺，但不直接改写业务数据。
	RiskQuote Risk = "quote"
	// RiskWrite 表示数据写入能力：变更业务数据，通常可逆或可用后续操作补偿。
	RiskWrite Risk = "write"
	// RiskAccount 表示账号与凭证能力：改动账号启用状态、登录态、令牌或平台凭证。
	RiskAccount Risk = "account"
	// RiskDestructive 表示不可逆能力：删除、作废或无法用后续操作补偿的破坏性动作。
	RiskDestructive Risk = "destructive"
)

// elevated 报告等级是否属于高危档（账号与不可逆）。
//
// 高危能力的共同特征是一旦误执行无法靠业务操作回退，因此客服 Agent 在任何档位下
// 都不得触达，其余调用方也必须先取得显式确认。
func (r Risk) elevated() bool {
	return r == RiskAccount || r == RiskDestructive
}

// Valid 报告等级是否为本包定义的合法取值。
//
// 未登记的等级按非法处理，由调用方 fail-closed 拒绝，避免新增等级时旧代码静默放行。
func (r Risk) Valid() bool {
	switch r {
	case RiskRead, RiskQuote, RiskWrite, RiskAccount, RiskDestructive:
		return true
	default:
		return false
	}
}
