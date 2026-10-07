// confirm.go 提供破坏性工具的统一显式确认守卫：
// 破坏性工具入参必须携带布尔 confirm=true，否则在触达任何应用用例前被拒绝。

package mcp

// confirmArgumentName 是破坏性工具 schema 中统一的显式确认参数名。
const confirmArgumentName = "confirm"

// RequireConfirm 报告入参中的 confirm 是否显式为布尔真。
// 缺失、非布尔或 false 一律视为未确认；调用方必须据此拒绝执行破坏性动作。
func RequireConfirm(arguments map[string]any) bool {
	if arguments == nil {
		return false
	}
	// confirmed 是 confirm 参数的布尔值，只有显式 true 才放行。
	confirmed, ok := arguments[confirmArgumentName].(bool)
	return ok && confirmed
}
