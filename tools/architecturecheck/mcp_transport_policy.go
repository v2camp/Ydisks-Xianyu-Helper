// mcp_transport_policy.go 定义对外开放 MCP 传输层的架构门禁：
//   - internal/mcp 只允许依赖标准库、mcp-go 与 internal/application/* 的应用模型，禁止依赖实现层与装配根；
//   - internal/server 只负责通用挂载缝，禁止反向承载 MCP 协议实现或 JSON-RPC 语义。
//
// 两条规则都 fail-closed：命中即失败，禁止白名单、基线或降级告警绕过。

package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// mcpForbiddenImports 是 MCP 传输包禁止依赖的实现层与装配根包前缀。
// 依赖方向必须由组合层投影后的包内最小端口提供，传输包不得直接触达 SQL、平台协议、浏览器或业务实现。
var mcpForbiddenImports = []string{
	"internal/server", "internal/db", "internal/xianyu", "internal/browser",
	"internal/automation", "internal/engine", "internal/adapter", "internal/composition",
}

// mcpProtocolMarkers 是 HTTP Server 禁止出现的 MCP 协议实现标记；命中说明协议语义倒灌进传输层。
var mcpProtocolMarkers = []string{"mcp-go", "jsonrpc", "json-rpc"}

// isForbiddenMCPTransportImport 判断 MCP 传输包是否依赖了禁止的实现层或装配根包。
func isForbiddenMCPTransportImport(filePath, importedPath string) bool {
	if !strings.HasPrefix(filePath, "internal/mcp/") || strings.HasSuffix(filePath, "_test.go") {
		return false
	}
	// forbidden 是当前待比对的禁止包前缀。
	for _, forbidden := range mcpForbiddenImports {
		if importedPath == forbidden || strings.HasPrefix(importedPath, forbidden+"/") {
			return true
		}
	}
	return false
}

// checkServerMCPProtocolIsolation 拒绝 HTTP Server 反向实现 MCP 协议或引入 JSON-RPC 语义。
// 参数 relativePath 是仓库相对路径；syntax 是已解析文件；source 是文件原文；fset 提供行号映射。
func checkServerMCPProtocolIsolation(relativePath string, syntax *ast.File, source []byte, fset *token.FileSet) []violation {
	if !strings.HasPrefix(relativePath, "internal/server/") || strings.HasSuffix(relativePath, "_test.go") {
		return nil
	}
	// violations 保存当前 Server 文件的协议越界记录。
	var violations []violation
	// imp 是当前 Server 文件的导入声明；协议实现必须留在 internal/mcp。
	for _, imp := range syntax.Imports {
		// importedPath、unquoteErr 分别是去引号后的导入路径与其解析错误。
		importedPath, unquoteErr := strconv.Unquote(imp.Path.Value)
		if unquoteErr != nil {
			continue
		}
		if strings.Contains(importedPath, "mcp-go") {
			violations = append(violations, violation{
				file:    relativePath,
				line:    fset.Position(imp.Pos()).Line,
				message: fmt.Sprintf("Server 只保留通用挂载缝，禁止导入 MCP 协议实现 %q", importedPath),
			})
		}
	}
	// lowered 是用于大小写无关标记扫描的文件原文。
	lowered := strings.ToLower(string(source))
	// marker 是当前待扫描的协议标记。
	for _, marker := range mcpProtocolMarkers {
		// index 是标记首次出现的位置；-1 表示未出现。
		index := strings.Index(lowered, marker)
		if index < 0 {
			continue
		}
		violations = append(violations, violation{
			file:    relativePath,
			line:    lineAtOffset(source, index),
			message: fmt.Sprintf("Server 禁止出现 MCP 协议或 JSON-RPC 标记 %q；协议语义必须留在 internal/mcp", marker),
		})
	}
	return violations
}

// lineAtOffset 把字节偏移换算为一基行号，供文本级门禁定位违规位置。
func lineAtOffset(source []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	return 1 + strings.Count(string(source[:offset]), "\n")
}
