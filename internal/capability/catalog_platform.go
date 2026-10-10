package capability

// 平台能力名常量。
//
// 命名与各传输层对外暴露的名称保持一致，使「同一个动作」在 MCP 工具、客服 Agent 工具集
// 与运营 Agent 命令之间可直接对应，避免同一动作出现多个名字后策略无法统一。
//
// 客服可读能力（order_*/item_*）沿用 MCP 既有工具名；
// 运营命令能力（ops_*）是 QQ 通道特有的汇总视图，不对应 MCP 工具。
const (
	// CapOrderList 是分页查询归属账号订单列表的只读能力。
	CapOrderList = "order_list"
	// CapOrderGet 是读取单个归属订单详情的只读能力。
	CapOrderGet = "order_get"
	// CapItemList 是分页查询归属账号商品列表的只读能力。
	CapItemList = "item_list"
	// CapItemGet 是读取单个归属商品详情的只读能力。
	CapItemGet = "item_get"
	// CapOpsSalesSnapshot 是运营用的当日销量汇总能力，跨账号。
	CapOpsSalesSnapshot = "ops_sales_snapshot"
	// CapOpsHealthSnapshot 是运营用的系统健康快照能力，跨账号。
	CapOpsHealthSnapshot = "ops_health_snapshot"
	// CapOpsChatDigest 是运营用的近期会话摘要能力，跨账号。
	CapOpsChatDigest = "ops_chat_digest"
)

// PlatformCatalog 返回平台内置能力目录。
//
// 目录只登记平台确实提供的能力，声明处即危险等级的唯一来源：
// 消费方只能按名引用，不能自行声明或放宽等级。
//
// 本期只登记只读能力。写能力（write）与账号/不可逆能力（account/destructive）
// 在接入前不得登记——未登记的能力求值恒为拒绝，因此「尚未接入」在结构上等价于「不可调用」。
//
// 客服可读能力的 MinPreset 取只读档，意味着客服 Agent 在只读档即可使用；
// 运营命令能力的 MinPreset 留空，表示客服 Agent 在任何档位下都不可调用（它们是跨账号汇总视图）。
//
// 目录构造失败只在声明自相矛盾时发生（如高危能力却声明了客服档位），属编程错误，
// 调用方必须显式处理而不应静默忽略。
func PlatformCatalog() (*Catalog, error) {
	return NewCatalog(
		Spec{Name: CapOrderList, Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly},
		Spec{Name: CapOrderGet, Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly},
		Spec{Name: CapItemList, Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly},
		Spec{Name: CapItemGet, Risk: RiskRead, Scope: ScopeAccount, MinPreset: PresetReadonly},
		Spec{Name: CapOpsSalesSnapshot, Risk: RiskRead, Scope: ScopeGlobal},
		Spec{Name: CapOpsHealthSnapshot, Risk: RiskRead, Scope: ScopeGlobal},
		Spec{Name: CapOpsChatDigest, Risk: RiskRead, Scope: ScopeGlobal},
	)
}
