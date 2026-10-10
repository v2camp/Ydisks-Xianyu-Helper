package capability

// Decision 是策略求值结果，调用方必须严格按其执行。
//
// 四种结果互斥且穷尽：调用方必须能处理全部取值，禁止出现「未识别结果按允许处理」的分支。
type Decision string

const (
	// DecisionAllow 表示允许直接执行。
	DecisionAllow Decision = "allow"
	// DecisionRequireConfirm 表示需调用方提供同步显式确认后方可执行。
	DecisionRequireConfirm Decision = "require_confirm"
	// DecisionRequireAsyncConfirm 表示需异步二次确认，未取得确认前不得执行。
	DecisionRequireAsyncConfirm Decision = "require_async_confirm"
	// DecisionDeny 表示拒绝执行，调用方不得以任何降级方式绕行。
	DecisionDeny Decision = "deny"
)

// Request 是一次策略求值请求。
//
// 请求只携带「谁」与「要做什么」两项事实，不含能力元数据：
// 危险等级与档位要求一律来自能力目录，调用方无法自报，因而无法自行放宽限制。
type Request struct {
	// Principal 是调用方身份；不合法时求值恒为拒绝。
	Principal Principal
	// Capability 是目标能力名称，必须在能力目录中已登记。
	Capability string
	// Confirmed 表示调用方已提供同步显式确认，对应 MCP 的 confirm 入参。
	// 该字段只对可同步确认的调用方有意义，客服与运营路径忽略它。
	Confirmed bool
}

// Evaluate 在给定能力目录上对一次调用求值，返回调用方必须遵守的决策。
//
// 求值 fail-closed：目录缺失、能力未登记、身份非法或档位不足一律返回拒绝。
// 本函数是全部权限判断的唯一实现点，各传输层与 Agent 层不得自行推导结论。
func Evaluate(catalog *Catalog, req Request) Decision {
	// spec 和 ok 分别是目标能力的声明与命中标志。
	spec, ok := catalog.Lookup(req.Capability)
	if !ok {
		return DecisionDeny
	}
	if !req.Principal.Valid() {
		return DecisionDeny
	}
	switch req.Principal.Kind {
	case PrincipalRemoteHarness:
		return evaluateRemoteHarness(spec, req.Confirmed)
	case PrincipalOpsAgent:
		return evaluateOpsAgent(spec)
	case PrincipalSupportAgent:
		return evaluateSupportAgent(spec, req.Principal.Preset)
	default:
		return DecisionDeny
	}
}

// evaluateRemoteHarness 求值外部 Harness 调用。
//
// 操作者在场，因此高危能力（账号、不可逆）要求同步显式确认，其余能力直接放行：
// 外部 Harness 的目标是最大化自助操作能力，不宜额外设限。
func evaluateRemoteHarness(spec Spec, confirmed bool) Decision {
	if spec.Risk.elevated() && !confirmed {
		return DecisionRequireConfirm
	}
	return DecisionAllow
}

// evaluateOpsAgent 求值运营 Agent 调用。
//
// 管理员不在场（在手机上通过 QQ 下指令），无法同步确认，
// 因此凡会产生业务变更的能力一律转异步二次确认；只读与报价类直接放行。
func evaluateOpsAgent(spec Spec) Decision {
	switch spec.Risk {
	case RiskRead, RiskQuote:
		return DecisionAllow
	default:
		return DecisionRequireAsyncConfirm
	}
}

// evaluateSupportAgent 求值客服 Agent 调用。
//
// 无人监督，因此先按危险等级排除账号与不可逆能力（任何档位都不放行），
// 再按档位比较；声明未设置档位的能力同样视为不可触达。
func evaluateSupportAgent(spec Spec, preset Preset) Decision {
	if spec.Risk.elevated() {
		return DecisionDeny
	}
	if !spec.SupportAvailable() {
		return DecisionDeny
	}
	if !preset.Allows(spec.MinPreset) {
		return DecisionDeny
	}
	return DecisionAllow
}
