package qqbot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/capability"
)

// allowAllPolicy 是恒放行的策略替身，用于验证放行路径下命令行为不变。
type allowAllPolicy struct{}

// Evaluate 对任何能力都返回放行。
func (allowAllPolicy) Evaluate(_ int64, _ string) capability.Decision {
	return capability.DecisionAllow
}

// denyAllPolicy 是恒拒绝的策略替身。
type denyAllPolicy struct{}

// Evaluate 对任何能力都返回拒绝。
func (denyAllPolicy) Evaluate(_ int64, _ string) capability.Decision {
	return capability.DecisionDeny
}

// recordingPolicy 记录求值入参并返回预设决策。
type recordingPolicy struct {
	// decision 是预设返回的决策。
	decision capability.Decision
	// capabilities 按顺序记录每次求值收到的能力名。
	capabilities []string
	// userIDs 按顺序记录每次求值收到的管理员标识。
	userIDs []int64
}

// Evaluate 记录入参并返回预设决策。
func (p *recordingPolicy) Evaluate(adminUserID int64, capabilityName string) capability.Decision {
	p.userIDs = append(p.userIDs, adminUserID)
	p.capabilities = append(p.capabilities, capabilityName)
	return p.decision
}

// governedCommands 是受策略管辖的命令与其平台能力名的对应表。
var governedCommands = []struct {
	// text 是触发该命令的入站文本。
	text string
	// capabilityName 是该命令对应的平台能力名。
	capabilityName string
}{
	{text: "销量", capabilityName: capability.CapOpsSalesSnapshot},
	{text: "健康", capabilityName: capability.CapOpsHealthSnapshot},
	{text: "会话", capabilityName: capability.CapOpsChatDigest},
}

// TestCapabilityForCommandMapsGovernedCommands 验证受管辖命令映射到平台已登记的能力名。
func TestCapabilityForCommandMapsGovernedCommands(t *testing.T) {
	// governed 是当前待校验的命令与其能力名的对应项。
	for _, governed := range governedCommands {
		// name、ok 分别是该命令映射出的能力名与是否受策略管辖。
		name, ok := capabilityForCommand(ParseCommand(governed.text))
		if !ok {
			t.Fatalf("命令 %q 应受策略管辖", governed.text)
		}
		if name != governed.capabilityName {
			t.Fatalf("命令 %q 映射到 %q，期望 %q", governed.text, name, governed.capabilityName)
		}
	}
	// helpCapability、helpGoverned 分别是帮助命令映射出的能力名与是否受管辖。
	helpCapability, helpGoverned := capabilityForCommand(CommandHelp)
	if helpGoverned {
		t.Fatalf("帮助命令只回显静态清单，不应受策略管辖，实际映射到 %q", helpCapability)
	}
	// unknownCapability、unknownGoverned 分别是未知命令映射出的能力名与是否受管辖。
	unknownCapability, unknownGoverned := capabilityForCommand(CommandUnknown)
	if unknownGoverned {
		t.Fatalf("未知命令只回显静态清单，不应受策略管辖，实际映射到 %q", unknownCapability)
	}
}

// TestHandleDeniesCommandBeforeReading 验证策略拒绝时命令拒答，且不触达任何读取端口。
func TestHandleDeniesCommandBeforeReading(t *testing.T) {
	// governed 是当前待求值的命令与其能力名的对应项。
	for _, governed := range governedCommands {
		// sales 是销量读取端口替身；策略拒绝时不得被调用。
		sales := &salesReaderFake{}
		// health 是健康读取端口替身；策略拒绝时不得被调用。
		health := &healthReaderFake{}
		// chats 是会话读取端口替身；策略拒绝时不得被调用。
		chats := &chatsReaderFake{}
		// service 是注入恒拒绝策略的命令服务。
		service := NewService(sales, health, chats, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true}, denyAllPolicy{})
		// reply 是命令回复文本；err 是本次调用的错误。
		reply, err := service.Handle(context.Background(), "openid-1", governed.text)
		if !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("命令 %q 应被策略拒绝，实际 error=%v", governed.text, err)
		}
		if reply != "" {
			t.Fatalf("命令 %q 被拒绝时不应产生回复文本，实际 %q", governed.text, reply)
		}
		if sales.calls != 0 || health.calls != 0 || chats.calls != 0 {
			t.Fatalf("命令 %q 被拒绝时不得触达读取端口，实际 sales=%d health=%d chats=%d", governed.text, sales.calls, health.calls, chats.calls)
		}
	}
}

// TestHandleWithoutPolicyDeniesCommand 验证未装配策略时命令 fail-closed。
func TestHandleWithoutPolicyDeniesCommand(t *testing.T) {
	// sales 是销量读取端口替身；未装配策略时不得被调用。
	sales := &salesReaderFake{}
	// service 是未注入策略求值器的命令服务。
	service := NewService(sales, &healthReaderFake{}, &chatsReaderFake{}, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true}, nil)
	// reply 是命令回复文本；err 是本次调用的错误。
	reply, err := service.Handle(context.Background(), "openid-1", "销量")
	if !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("未装配策略应 fail-closed，实际 error=%v", err)
	}
	if reply != "" || sales.calls != 0 {
		t.Fatal("未装配策略时不得产生回复文本或触达读取端口")
	}
}

// TestCatalogPolicyAllowsReadonlyOpsCommands 验证接入平台目录后三个只读汇总能力仍被放行，既有行为不变。
func TestCatalogPolicyAllowsReadonlyOpsCommands(t *testing.T) {
	// catalog 是平台内置能力目录；err 是目录构造错误。
	catalog, err := capability.PlatformCatalog()
	if err != nil {
		t.Fatalf("平台能力目录构造失败: %v", err)
	}
	// policy 是用平台目录实现的策略求值器。
	policy := NewCatalogPolicy(catalog)
	// governed 是当前待求值的只读汇总能力项。
	for _, governed := range governedCommands {
		// decision 是该只读汇总能力在运营 Agent 身份下的求值结果。
		decision := policy.Evaluate(7, governed.capabilityName)
		if decision != capability.DecisionAllow {
			t.Fatalf("运营只读能力 %q 应放行，实际 %q", governed.capabilityName, decision)
		}
	}
}

// TestCatalogPolicyWithoutCatalogDenies 验证未装配目录的策略求值恒拒绝。
func TestCatalogPolicyWithoutCatalogDenies(t *testing.T) {
	// decision 是未装配目录时的求值结果。
	decision := NewCatalogPolicy(nil).Evaluate(7, capability.CapOpsSalesSnapshot)
	if decision != capability.DecisionDeny {
		t.Fatalf("未装配目录应拒绝，实际 %q", decision)
	}
}

// TestHandlePassesIdentityAndCapabilityToPolicy 验证策略求值收到管理员身份与命令对应的能力名。
func TestHandlePassesIdentityAndCapabilityToPolicy(t *testing.T) {
	// policy 是记录入参的恒拒绝策略替身；拒绝可避免后续继续触达读取端口。
	policy := &recordingPolicy{decision: capability.DecisionDeny}
	// service 是注入记录型策略的命令服务。
	service := NewService(&salesReaderFake{}, &healthReaderFake{}, &chatsReaderFake{}, &identityFake{userID: 42}, &authorizerFake{enabled: true, allowed: true}, policy)
	// err 是本次调用的错误，应为策略拒绝。
	if _, err := service.Handle(context.Background(), "openid-1", "销量"); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("策略拒绝应返回 ErrPolicyDenied，实际 %v", err)
	}
	if len(policy.capabilities) != 1 || policy.capabilities[0] != capability.CapOpsSalesSnapshot {
		t.Fatalf("策略应收到命令对应的能力名，实际 %v", policy.capabilities)
	}
	if len(policy.userIDs) != 1 || policy.userIDs[0] != 42 {
		t.Fatalf("策略应收到管理员身份，实际 %v", policy.userIDs)
	}
}

// TestHandleSkipsPolicyForHelpCommand 验证帮助命令不经策略直接回显静态清单。
func TestHandleSkipsPolicyForHelpCommand(t *testing.T) {
	// policy 是恒拒绝策略替身；帮助命令不应触发求值。
	policy := &recordingPolicy{decision: capability.DecisionDeny}
	// service 是注入记录型策略的命令服务。
	service := NewService(&salesReaderFake{}, &healthReaderFake{}, &chatsReaderFake{}, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true}, policy)
	// reply 是帮助文案；err 是本次调用的错误。
	reply, err := service.Handle(context.Background(), "openid-1", "帮助")
	if err != nil {
		t.Fatalf("帮助命令不应失败，实际 %v", err)
	}
	if reply == "" {
		t.Fatal("帮助命令应回显命令清单")
	}
	if len(policy.capabilities) != 0 {
		t.Fatalf("帮助命令不应触发策略求值，实际 %v", policy.capabilities)
	}
}

// TestFormatDeniedStaysGeneric 验证拒绝文案不回显能力名或内部判定细节。
func TestFormatDeniedStaysGeneric(t *testing.T) {
	// message 是面向用户的拒绝文案。
	message := FormatDenied()
	if message == "" {
		t.Fatal("拒绝文案不应为空")
	}
	// internalName 是当前待检查的内部能力名。
	for _, internalName := range []string{capability.CapOpsSalesSnapshot, capability.CapOpsHealthSnapshot, capability.CapOpsChatDigest} {
		if strings.Contains(message, internalName) {
			t.Fatalf("拒绝文案不得回显内部能力名 %q：%q", internalName, message)
		}
	}
}
