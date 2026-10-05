// registry.go 提供 MCP 工具的统一注册框架：
//   - 依据工具定义生成 JSON Schema 与只读/破坏性注解；
//   - 破坏性工具自动补 confirm 参数并在调用用例前强制校验；
//   - 统一注入固定管理员身份、记录调用审计、把任意错误归一为中文 isError 结果。
//
// 域工具只声明业务入参与用例调用，不直接接触 mcp-go 协议类型与鉴权细节。

package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	mcpproto "github.com/mark3labs/mcp-go/mcp"
)

// ArgType 是工具入参的 JSON Schema 基础类型。
type ArgType string

const (
	// ArgString 是字符串类型入参。
	ArgString ArgType = "string"
	// ArgBoolean 是布尔类型入参。
	ArgBoolean ArgType = "boolean"
	// ArgInteger 是整数类型入参。
	ArgInteger ArgType = "integer"
	// ArgNumber 是数字类型入参。
	ArgNumber ArgType = "number"
	// ArgArray 是数组类型入参，元素类型由 ArgSpec.ItemType 指定。
	ArgArray ArgType = "array"
)

const (
	// defaultPageSize 是列表工具未指定 page_size 时的默认条数。
	defaultPageSize = 20
	// maxPageSize 是列表工具单页最大条数，防止 Harness 一次拉取过大数据集。
	maxPageSize = 200
	// auditCookieKeys 是按顺序尝试的账号归属审计键名。
	auditCookieKeys = "account_id|cid|cookie_id"
)

// ArgSpec 描述一个工具入参的名称、类型、中文说明、必填性与枚举值。
type ArgSpec struct {
	// Name 是入参 JSON 键名，使用稳定 snake_case。
	Name string
	// Type 是入参 JSON 类型。
	Type ArgType
	// Description 是给 Harness 的中文业务说明。
	Description string
	// Required 表示该入参是否必填。
	Required bool
	// Enum 非空时把字符串入参限制为枚举值之一。
	Enum []string
	// ItemType 在 Type=ArgArray 时指定元素类型，默认字符串。
	ItemType ArgType
}

// HandlerFunc 是域工具的业务处理器：接收调用身份与已解析入参，返回可 JSON 序列化的 MCP DTO。
// 返回的非分类错误会被统一归一为中文错误结果；处理器不得自行构造协议响应。
type HandlerFunc func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error)

// ToolDef 是一个工具的完整注册定义。
type ToolDef struct {
	// Name 是工具名，按域前缀蛇形命名（如 account_restart）。
	Name string
	// Description 是工具的中文用途与风险说明，供 Harness 自主选择。
	Description string
	// Destructive 为 true 时自动补 confirm 入参、标记 destructiveHint 且调用前强制确认。
	Destructive bool
	// Args 是工具业务入参定义（confirm 由框架自动追加，无需声明）。
	Args []ArgSpec
	// Handler 是工具业务处理器。
	Handler HandlerFunc
}

// Arguments 包装工具入参并提供带中文错误的类型化读取方法。
type Arguments map[string]any

// String 读取必填字符串入参；缺失或类型错误时返回中文参数错误。
func (a Arguments) String(name string) (string, error) {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return "", InvalidArgument("缺少必填参数 " + name)
	}
	// text 是字符串形态的值。
	text, ok := value.(string)
	if !ok {
		return "", InvalidArgument("参数 " + name + " 必须是字符串")
	}
	if text == "" {
		return "", InvalidArgument("参数 " + name + " 不能为空")
	}
	return text, nil
}

// OptionalString 读取可选字符串入参；缺失时返回默认值。
func (a Arguments) OptionalString(name, fallback string) string {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return fallback
	}
	// text 是字符串形态的值；类型不符时回落默认值，避免 Harness 因可选项类型问题中断。
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}

// Bool 读取必填布尔入参。
func (a Arguments) Bool(name string) (bool, error) {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return false, InvalidArgument("缺少必填参数 " + name)
	}
	// flag 是布尔形态的值。
	flag, ok := value.(bool)
	if !ok {
		return false, InvalidArgument("参数 " + name + " 必须是布尔值 true 或 false")
	}
	return flag, nil
}

// OptionalBool 读取可选布尔入参；缺失时返回默认值。
func (a Arguments) OptionalBool(name string, fallback bool) bool {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return fallback
	}
	// flag 是布尔形态的值。
	flag, ok := value.(bool)
	if !ok {
		return fallback
	}
	return flag
}

// Int 读取必填整数入参；JSON 数字以 float64 到达，拒绝非整数。
func (a Arguments) Int(name string) (int, error) {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return 0, InvalidArgument("缺少必填参数 " + name)
	}
	return asInt(name, value)
}

// OptionalInt 读取可选整数入参；缺失时返回默认值。
func (a Arguments) OptionalInt(name string, fallback int) int {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return fallback
	}
	// number、err 是整数化结果；可选项类型非法时回落默认值。
	number, err := asInt(name, value)
	if err != nil {
		return fallback
	}
	return number
}

// Page 从 page 与 page_size 入参归一为分页大小与偏移量；入参从 1 开始计数。
func (a Arguments) Page() (limit int, offset int) {
	// size 是页大小，做上下限收敛。
	size := a.OptionalInt("page_size", defaultPageSize)
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	// page 是从 1 开始的页码；非法值回落第一页。
	page := a.OptionalInt("page", 1)
	if page < 1 {
		page = 1
	}
	return size, (page - 1) * size
}

// StringArray 读取字符串数组入参；非数组或元素非字符串时返回中文错误。
func (a Arguments) StringArray(name string) ([]string, error) {
	// value、ok 是原始入参值及其存在性。
	value, ok := a[name]
	if !ok {
		return nil, InvalidArgument("缺少必填参数 " + name)
	}
	// raw 是数组形态的通用值切片。
	raw, ok := value.([]any)
	if !ok {
		return nil, InvalidArgument("参数 " + name + " 必须是字符串数组")
	}
	// items 收集类型化后的字符串元素。
	items := make([]string, 0, len(raw))
	// element 是当前数组元素。
	for _, element := range raw {
		// text 是字符串元素。
		text, ok := element.(string)
		if !ok {
			return nil, InvalidArgument("参数 " + name + " 的每个元素都必须是字符串")
		}
		items = append(items, text)
	}
	return items, nil
}

// asInt 把 JSON 反序列化产生的数字值安全转换为整数。
func asInt(name string, value any) (int, error) {
	// number 是通用数字形态；JSON 数字经 encoding/json 到达时为 float64。
	// number、ok 是 JSON 数字值及其类型断言结果。
	number, ok := value.(float64)
	if !ok {
		return 0, InvalidArgument("参数 " + name + " 必须是整数")
	}
	// truncated 是取整后的整数。
	truncated := int(number)
	if float64(truncated) != number {
		return 0, InvalidArgument("参数 " + name + " 必须是整数")
	}
	return truncated, nil
}

// RegisterTools 在协议服务器上批量注册工具，并把定义登记进清单供禁能力面对照测试。
func (e *Endpoint) RegisterTools(defs ...ToolDef) {
	// def 是当前注册的工具定义。
	for _, def := range defs {
		e.registerOne(def)
	}
}

// ToolDefs 返回已注册工具定义的按名排序快照。
func (e *Endpoint) ToolDefs() []ToolDef {
	e.registryMu.RLock()
	defer e.registryMu.RUnlock()
	// names 是已注册工具名，用于稳定排序。
	names := make([]string, 0, len(e.toolDefs))
	// name 是当前已注册工具名。
	for name := range e.toolDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	// defs 是按名排序的定义清单。
	defs := make([]ToolDef, 0, len(names))
	// name 是当前工具名。
	for _, name := range names {
		defs = append(defs, e.toolDefs[name])
	}
	return defs
}

// registerOne 注册单个工具：生成 schema、注解并包装确认/审计/错误归一逻辑。
func (e *Endpoint) registerOne(def ToolDef) {
	// options 是工具描述与入参 schema 选项。
	options := []mcpproto.ToolOption{mcpproto.WithDescription(def.Description)}
	// spec 是当前业务入参定义；必填标记作为属性级选项挂在该属性上。
	for _, spec := range def.Args {
		options = append(options, propertyOption(spec))
	}
	// 破坏性工具由框架统一追加 confirm 入参并要求必填。
	if def.Destructive {
		options = append(options,
			mcpproto.WithBoolean(confirmArgumentName,
				mcpproto.Description("必须显式传 true 才会执行此破坏性操作，请在确认影响后传入。"),
				mcpproto.Required()),
			mcpproto.WithDestructiveHintAnnotation(true),
		)
	} else {
		options = append(options, mcpproto.WithReadOnlyHintAnnotation(true))
	}
	// tool 是最终注册到 mcp-go 的工具元数据。
	tool := mcpproto.NewTool(def.Name, options...)
	// handler 是包装后的协议处理器，捕获 def 与端点审计依赖。
	handler := func(ctx context.Context, request mcpproto.CallToolRequest) (*mcpproto.CallToolResult, error) {
		return e.invokeTool(ctx, def, request)
	}
	e.mcpServer.AddTool(tool, handler)
	e.registryMu.Lock()
	e.toolDefs[def.Name] = def
	e.registryMu.Unlock()
}

// invokeTool 执行一次工具调用：确认校验、用例调用、审计落库与错误归一，永不向 SDK 抛业务错误。
func (e *Endpoint) invokeTool(ctx context.Context, def ToolDef, request mcpproto.CallToolRequest) (*mcpproto.CallToolResult, error) {
	// identity 是鉴权中间件注入的管理员身份；理论上不会缺失，缺失时按未认证拒绝。
	identity := IdentityFromContext(ctx)
	if identity == nil {
		// result、_ 是身份缺失时的协议错误结果。
		result, _ := ErrorResult(Fail(ClassUnauthorized, "调用身份缺失，请重新携带有效 MCP 令牌", nil))
		return result, nil
	}
	// rawArgs 是本次调用的原始入参映射；非对象入参按空参数处理并交由业务校验报错。
	rawArgs, _ := request.Params.Arguments.(map[string]any)
	// args 是本次调用的入参视图。
	args := Arguments(rawArgs)
	// 破坏性工具在触达任何应用用例前强制 confirm=true，保证缺参时零副作用。
	if def.Destructive && !RequireConfirm(args) {
		// denied 是确认缺失的协议错误结果。
		denied, class := ErrorResult(InvalidArgument("该操作有破坏性影响，必须在参数中显式传 confirm=true 后才会执行"))
		e.writeAudit(ctx, identity, def.Name, rawArgs, false, string(class), 0)
		return denied, nil
	}
	// started 是调用开始时刻，用于计算耗时。
	started := time.Now()
	// payload、callErr 是业务处理器结果；任何错误都归一为 isError 中文结果。
	payload, callErr := def.Handler(ctx, identity, args)
	// durationMS 是本次调用耗时（毫秒）。
	durationMS := time.Since(started).Milliseconds()
	if callErr != nil {
		// result、class 是归一后的协议错误结果与稳定类别。
		result, class := ErrorResult(callErr)
		e.writeAudit(ctx, identity, def.Name, rawArgs, false, string(class), durationMS)
		return result, nil
	}
	// result、marshalErr 是成功结果的协议形态与编码错误。
	result, marshalErr := JSONResult(payload)
	if marshalErr != nil {
		// failed、class 是结果编码失败的协议错误与类别。
		failed, class := ErrorResult(marshalErr)
		e.writeAudit(ctx, identity, def.Name, rawArgs, false, string(class), durationMS)
		return failed, nil
	}
	e.writeAudit(ctx, identity, def.Name, rawArgs, true, "", durationMS)
	return result, nil
}

// writeAudit 尽力写入调用审计；审计失败只记日志，不影响业务结果且不重放动作。
func (e *Endpoint) writeAudit(ctx context.Context, identity *CallIdentity, name string, rawArgs map[string]any, success bool, errorClass string, durationMS int64) {
	if e.audit == nil || identity == nil {
		return
	}
	// encoded 是原始入参 JSON；持久化层会做白名单脱敏。
	encoded, err := json.Marshal(rawArgs)
	if err != nil {
		encoded = nil
	}
	// argumentsJSON 是传给审计端口的参数文本；编码失败落空串。
	argumentsJSON := ""
	if encoded != nil {
		argumentsJSON = string(encoded)
	}
	// auditErr 是审计写入错误；只记录非敏感诊断。
	if auditErr := e.audit.AddAudit(ctx, AuditEntry{
		UserID:        identity.UserID,
		Source:        identity.Source,
		Category:      AuditCategoryTool,
		Name:          name,
		CookieID:      auditCookie(rawArgs),
		ArgumentsJSON: argumentsJSON,
		Success:       success,
		ErrorClass:    errorClass,
		DurationMS:    durationMS,
	}); auditErr != nil {
		slog.Default().Warn("写入 MCP 调用审计失败", "tool", name, "err", auditErr)
	}
}

// auditCookie 按约定键顺序提取账号归属标识，供审计关联；没有归属时返回空串。
func auditCookie(rawArgs map[string]any) string {
	// candidates 是可能携带账号标识的入参键。
	candidates := splitPipe(auditCookieKeys)
	// key 是当前候选键名。
	for _, key := range candidates {
		// value、ok 是入参值及其存在性。
		value, ok := rawArgs[key]
		if !ok {
			continue
		}
		// text 是字符串账号标识；非字符串归属键不记录。
		text, ok := value.(string)
		if ok && text != "" {
			return text
		}
	}
	return ""
}

// splitPipe 把常量中的管道分隔键名拆成切片，避免在热路径手写切片。
func splitPipe(raw string) []string {
	// parts 是拆分结果；本仓库所有调用输入都是常量，不做额外转义处理。
	parts := make([]string, 0, 4)
	// current 收集当前段字符。
	current := ""
	// char 是键名常量中的当前字符。
	for _, char := range raw {
		if char == '|' {
			parts = append(parts, current)
			current = ""
			continue
		}
		current += string(char)
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// propertyOption 依据入参定义生成对应的 mcp-go schema 选项。
func propertyOption(spec ArgSpec) mcpproto.ToolOption {
	// propertyOptions 是挂在属性上的选项，必填时包含 Required。
	propertyOptions := []mcpproto.PropertyOption{mcpproto.Description(spec.Description)}
	if spec.Required {
		propertyOptions = append(propertyOptions, mcpproto.Required())
	}
	switch spec.Type {
	case ArgBoolean:
		return mcpproto.WithBoolean(spec.Name, propertyOptions...)
	case ArgInteger:
		return mcpproto.WithInteger(spec.Name, propertyOptions...)
	case ArgNumber:
		return mcpproto.WithNumber(spec.Name, propertyOptions...)
	case ArgArray:
		// itemType 是数组元素类型，默认按字符串生成元素 schema。
		itemType := spec.ItemType
		if itemType == "" {
			itemType = ArgString
		}
		propertyOptions = append(propertyOptions, mcpproto.Items(map[string]any{"type": string(itemType)}))
		return mcpproto.WithArray(spec.Name, propertyOptions...)
	case ArgString:
		fallthrough
	default:
		if len(spec.Enum) > 0 {
			propertyOptions = append(propertyOptions, mcpproto.Enum(spec.Enum...))
		}
		return mcpproto.WithString(spec.Name, propertyOptions...)
	}
}
