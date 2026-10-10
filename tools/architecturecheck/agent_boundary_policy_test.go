package main

import (
	"path/filepath"
	"testing"
)

// TestAgentBoundaryRejectsImplementationAndTransportImports 验证客服 Agent 运行时禁止依赖
// 实现层、其它传输层与装配根，并放行能力内核、应用模型与模型 SDK。
func TestAgentBoundaryRejectsImplementationAndTransportImports(t *testing.T) {
	// forbidden 是客服 Agent 运行时禁止依赖的包前缀。
	forbidden := []string{
		"internal/db", "internal/xianyu", "internal/browser", "internal/automation",
		"internal/mcp", "internal/qqbot", "internal/server",
		"internal/adapter", "internal/composition", "internal/composition/runtime",
	}
	// path 是当前待验证的禁止导入路径。
	for _, path := range forbidden {
		if !isForbiddenAgentImport("internal/agent/loop.go", path) {
			t.Fatalf("客服 Agent 运行时导入 %s 应被拒绝", path)
		}
	}
	// allowed 是客服 Agent 运行时允许的依赖：标准库、模型 SDK、能力内核、应用模型与生成接缝。
	allowed := []string{
		"context", "encoding/json",
		"github.com/sashabaranov/go-openai",
		"internal/capability", "internal/netguard",
		"internal/application/orders", "internal/application/items",
		"internal/engine",
	}
	// path 是当前待验证的允许导入路径。
	for _, path := range allowed {
		if isForbiddenAgentImport("internal/agent/loop.go", path) {
			t.Fatalf("客服 Agent 运行时导入 %s 不应被拒绝", path)
		}
	}
	if isForbiddenAgentImport(filepath.ToSlash("internal/engine/ai.go"), "internal/db") {
		t.Fatal("非 Agent 包不应套用 Agent 依赖规则")
	}
	if isForbiddenAgentImport("internal/agent/loop_test.go", "internal/db") {
		t.Fatal("测试文件不应被 Agent 依赖规则阻断")
	}
}
