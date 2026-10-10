package qqbot

import "xianyu-go/internal/capability"

// PolicyEvaluator 在命令执行前对该调用求值，返回调用方必须遵守的决策。
//
// 接口由本包声明、组合层实现：入站命令服务只描述「执行前要问一次策略」，
// 既不持有能力目录，也不自行推导任何判定结论——判定规则的唯一实现点是能力内核。
type PolicyEvaluator interface {
	// Evaluate 求值指定运营能力在给定管理员身份下的决策。
	Evaluate(adminUserID int64, capabilityName string) capability.Decision
}

// catalogPolicy 用只读能力目录实现 PolicyEvaluator。
type catalogPolicy struct {
	// catalog 是平台能力目录；为 nil 时求值恒拒绝。
	catalog *capability.Catalog
}

// Evaluate 直接把身份与能力名交给能力内核，本包不复制任何判定规则。
func (p catalogPolicy) Evaluate(adminUserID int64, capabilityName string) capability.Decision {
	return capability.Evaluate(p.catalog, capability.Request{
		// Principal 是运营 Agent 身份：管理员在手机上通过 QQ 下指令，无法同步确认。
		Principal:  capability.Principal{Kind: capability.PrincipalOpsAgent, UserID: adminUserID},
		Capability: capabilityName,
	})
}

// NewCatalogPolicy 用平台能力目录构造策略求值器；目录为 nil 时求值恒拒绝。
func NewCatalogPolicy(catalog *capability.Catalog) PolicyEvaluator {
	return catalogPolicy{catalog: catalog}
}

// capabilityForCommand 返回命令对应的平台能力名；帮助与未知命令不受策略管辖。
//
// 帮助文案只回显静态命令清单，不含任何业务数据，因此没有对应能力可求值。
func capabilityForCommand(command Command) (string, bool) {
	switch command {
	case CommandSales:
		return capability.CapOpsSalesSnapshot, true
	case CommandHealth:
		return capability.CapOpsHealthSnapshot, true
	case CommandChats:
		return capability.CapOpsChatDigest, true
	default:
		return "", false
	}
}

// allows 在命令执行前求值策略；未装配求值器时一律不放行，保持 fail-closed。
func (s *Service) allows(adminUserID int64, capabilityName string) bool {
	if s == nil || s.policy == nil {
		return false
	}
	return s.policy.Evaluate(adminUserID, capabilityName) == capability.DecisionAllow
}
