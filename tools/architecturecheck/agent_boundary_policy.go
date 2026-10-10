// agent_boundary_policy.go 定义客服 Agent 运行时的架构门禁：
//   - internal/agent 只允许依赖标准库、模型 SDK、应用层模型、internal/capability
//     与 internal/engine 的模型生成接缝；
//   - 禁止依赖实现层（db/平台/浏览器/自动化）、其它传输层（mcp/qqbot/server）与装配根。
//
// 规则 fail-closed：命中即失败，禁止白名单、基线或降级告警绕过。
// 只实现 engine 的生成接缝、不触达其消息与凭证链路属于评审级约束，门禁只能保证包级隔离。

package main

import "strings"

// agentForbiddenImports 是客服 Agent 运行时禁止依赖的实现层、其它传输层与装配根包前缀。
// Agent 的能力必须经 internal/capability 求值、用例必须由组合层投影实现，不得直接触达
// SQL、平台协议、浏览器、自动化或其它会话通道。
var agentForbiddenImports = []string{
	"internal/db", "internal/xianyu", "internal/browser", "internal/automation",
	"internal/mcp", "internal/qqbot", "internal/server", "internal/adapter", "internal/composition",
}

// isForbiddenAgentImport 判断客服 Agent 运行时是否依赖了禁止的实现层、其它传输层或装配根包。
// filePath 是仓库相对路径；importedPath 是已去除模块前缀的内部导入路径。
func isForbiddenAgentImport(filePath, importedPath string) bool {
	if !strings.HasPrefix(filePath, "internal/agent/") || strings.HasSuffix(filePath, "_test.go") {
		return false
	}
	// forbidden 是当前待比对的禁止包前缀。
	for _, forbidden := range agentForbiddenImports {
		if importedPath == forbidden || strings.HasPrefix(importedPath, forbidden+"/") {
			return true
		}
	}
	return false
}
