// tools_settings.go 注册系统设置与用户设置域工具：脱敏读取、批量/单项更新与用户偏好读写。
//
// 安全与语义红线：
//   - 敏感键（ai_api_key、smtp_password、qq_reply_secret_key、captcha.remote_secret_key、qqbot.app_secret）
//     只能经 secrets 三态命令写入，读取一律走应用服务的脱敏与审计路径；
//   - MCP 自身状态键（mcp.server.enabled、mcp.server.allow_non_loopback）禁止经通用设置入口修改，
//     必须走 MCP 专用用例，防止 Harness 自己改开关绕过安全门；
//   - 任何响应、错误与审计都不得包含敏感值原文。

package mcp

import (
	"context"
	"strings"

	settingsapp "xianyu-go/internal/application/settings"

	"xianyu-go/internal/capability"
)

// mcpStateSettingEnabled 是 MCP 服务启用开关的系统设置键，只能由 MCP 专用用例修改。
const mcpStateSettingEnabled = "mcp.server.enabled"

// mcpStateSettingAllowNonLoopback 是放行非本机来源的系统设置键，只能由 MCP 专用用例修改。
const mcpStateSettingAllowNonLoopback = "mcp.server.allow_non_loopback"

// systemSettingsResult 是系统设置读取返回：敏感键只以 configured 标记出现。
type systemSettingsResult struct {
	// Values 是脱敏后的系统设置键值表；敏感键值为 configured 或空。
	Values map[string]string `json:"values"`
	// SensitiveKeys 是当前系统的敏感键名清单（不含任何值）。
	SensitiveKeys []string `json:"sensitive_keys"`
	// Note 说明敏感字段的读写语义。
	Note string `json:"note"`
}

// systemSettingWriteResult 是系统设置写入的确认返回。
type systemSettingWriteResult struct {
	// UpdatedKeys 是本次写入的普通设置键。
	UpdatedKeys []string `json:"updated_keys,omitempty"`
	// SecretKeys 是本次写入的敏感设置键（只回传键名）。
	SecretKeys []string `json:"secret_keys,omitempty"`
	// Applied 固定为 true，表示写入已在服务端生效。
	Applied bool `json:"applied"`
}

// userSettingsResult 是用户偏好设置返回。
type userSettingsResult struct {
	// Values 是当前用户的偏好设置键值表。
	Values map[string]string `json:"values"`
	// Total 是设置项数量。
	Total int `json:"total"`
}

// userSettingValueResult 是单项用户设置返回。
type userSettingValueResult struct {
	// Key 是设置键。
	Key string `json:"key"`
	// Value 是设置值；未配置时为空串。
	Value string `json:"value"`
	// Configured 表示该键是否已显式配置。
	Configured bool `json:"configured"`
}

// RegisterSettingsTools 注册系统设置与用户设置域全部工具。
func (e *Endpoint) RegisterSettingsTools(p capability.SettingsPorts) {
	if p == nil {
		return
	}
	e.registerSystemSettingsTools(p)
	e.registerUserSettingsTools(p)
}

// registerSystemSettingsTools 注册系统设置读取与写入工具。
func (e *Endpoint) registerSystemSettingsTools(p capability.SettingsPorts) {
	e.RegisterTools(
		ToolDef{
			Name: "settings_get_system",
			Description: "读取系统设置（管理员可见）。敏感键（ai_api_key、smtp_password、qq_reply_secret_key、" +
				"captcha.remote_secret_key、qqbot.app_secret）只返回 configured 标记，绝不返回值。本次读取会记录敏感键访问审计。只读。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// values、readErr 是脱敏后的系统设置。
				values, readErr := p.GetSystem(ctx, identity.UserID)
				if readErr != nil {
					return nil, readErr
				}
				// keys 是敏感键名清单，用于提示调用方哪些键不可读。
				keys := sensitiveSettingKeys(p, values)
				return systemSettingsResult{
					Values: values, SensitiveKeys: keys,
					Note: "敏感键只可写入不可读取；需要使用已保存的密钥时请调用对应业务工具（如 settings_ai_test_connection）。",
				}, nil
			},
		},
		ToolDef{
			Name: "settings_update_system",
			Description: "批量更新系统设置：values 提交普通键值；secrets 提交敏感键的三态命令，" +
				"每项形如 {\"action\":\"retain|replace|clear\",\"value\":\"新值\"}（replace 必须带非空 value）。" +
				"敏感值只落库、不回传、不写入日志与审计。",
			Args: []ArgSpec{
				{Name: "values", Type: ArgObject, Description: "普通设置键值对象，例如 {\"log_level\":\"info\"}。"},
				{Name: "secrets", Type: ArgObject, Description: "敏感设置三态命令对象，例如 {\"ai_api_key\":{\"action\":\"replace\",\"value\":\"sk-...\"}}。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// values、valuesErr 是普通设置键值表。
				values, valuesErr := stringMapArg(args, "values")
				if valuesErr != nil {
					return nil, valuesErr
				}
				// secrets、secretsErr 是敏感设置三态命令表。
				secrets, secretsErr := secretChangeMapArg(args, "secrets")
				if secretsErr != nil {
					return nil, secretsErr
				}
				if len(values) == 0 && len(secrets) == 0 {
					return nil, InvalidArgument("请至少提供 values 或 secrets 中的一项")
				}
				// guardErr 表示写入包含 MCP 自身状态键。
				if guardErr := guardSystemSettingKeys(values, secrets); guardErr != nil {
					return nil, guardErr
				}
				// applyErr 是系统设置写入用例返回的错误。
				if applyErr := p.ApplySystemChanges(ctx, identity.UserID, values, secrets); applyErr != nil {
					return nil, applyErr
				}
				return systemSettingWriteResult{
					UpdatedKeys: sortedKeys(values), SecretKeys: sortedKeys(secrets), Applied: true,
				}, nil
			},
		},
		ToolDef{
			Name: "settings_set_system",
			Description: "写入单项系统设置：普通键直接提交 value；敏感键需提交 action（retain/replace/clear）。" +
				"MCP 自身状态键（mcp.server.enabled、mcp.server.allow_non_loopback）禁止经此修改。",
			Args: []ArgSpec{
				{Name: "key", Type: ArgString, Required: true, Description: "设置键。"},
				{Name: "value", Type: ArgString, Description: "普通设置的字符串值；敏感键 replace 时为新的秘密值。"},
				{Name: "action", Type: ArgString, Enum: []string{"retain", "replace", "clear"},
					Description: "仅敏感键使用：retain 保留、replace 覆盖、clear 清空。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// key 是设置键。
				key, keyErr := args.String("key")
				if keyErr != nil {
					return nil, keyErr
				}
				// value 是可选设置值。
				value := args.OptionalString("value", "")
				// action 是可选敏感三态命令。
				action := args.OptionalString("action", "")
				// guardErr 表示写入的是 MCP 自身状态键。
				if guardErr := guardSingleSettingKey(p, key); guardErr != nil {
					return nil, guardErr
				}
				// setErr 是系统设置写入用例返回的错误。
				if setErr := p.SetSystem(ctx, identity.UserID, key, value, action); setErr != nil {
					return nil, setErr
				}
				// result 是写入确认；敏感键只回传键名。
				result := systemSettingWriteResult{UpdatedKeys: []string{key}, Applied: true}
				if p.IsSensitiveSettingKey(strings.TrimSpace(key)) && !strings.EqualFold(action, "retain") {
					result.UpdatedKeys = nil
					result.SecretKeys = []string{strings.TrimSpace(key)}
				}
				return result, nil
			},
		},
	)
}

// registerUserSettingsTools 注册用户偏好设置工具。
func (e *Endpoint) registerUserSettingsTools(p capability.SettingsPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "settings_user_list",
			Description: "读取当前管理员的全部用户偏好设置。只读，不含任何系统级秘密。",
			Handler: func(ctx context.Context, identity *CallIdentity, _ Arguments) (any, error) {
				// values、readErr 是用户偏好设置。
				values, readErr := p.ListUser(ctx, identity.UserID)
				if readErr != nil {
					return nil, readErr
				}
				return userSettingsResult{Values: values, Total: len(values)}, nil
			},
		},
		ToolDef{
			Name:        "settings_user_get",
			Description: "读取当前管理员的一项用户偏好设置；未配置时返回空值与 configured=false。只读。",
			Args:        []ArgSpec{{Name: "key", Type: ArgString, Required: true, Description: "设置键。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// key 是设置键。
				key, keyErr := args.String("key")
				if keyErr != nil {
					return nil, keyErr
				}
				// value、readErr 是设置值。
				value, readErr := p.GetUser(ctx, identity.UserID, key)
				if readErr != nil {
					return nil, readErr
				}
				return userSettingValueResult{Key: key, Value: value, Configured: value != ""}, nil
			},
		},
		ToolDef{
			Name:        "settings_user_set",
			Description: "保存当前管理员的一项用户偏好设置。",
			Args: []ArgSpec{
				{Name: "key", Type: ArgString, Required: true, Description: "设置键。"},
				{Name: "value", Type: ArgString, Required: true, Description: "设置值。"},
			},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// key 是设置键。
				key, keyErr := args.String("key")
				if keyErr != nil {
					return nil, keyErr
				}
				// value 是设置值。
				value, valueErr := args.String("value")
				if valueErr != nil {
					return nil, valueErr
				}
				// setErr 是用户设置写入用例返回的错误。
				if setErr := p.SetUser(ctx, identity.UserID, key, value); setErr != nil {
					return nil, setErr
				}
				return userSettingValueResult{Key: key, Value: value, Configured: true}, nil
			},
		},
	)
}

// guardSingleSettingKey 拒绝经通用设置入口修改 MCP 自身状态键。
// 敏感键允许通过：应用服务会按其三态命令语义校验并记录审计。
func guardSingleSettingKey(_ capability.SettingsPorts, key string) error {
	// normalized 是去除空白的设置键。
	normalized := strings.TrimSpace(key)
	if normalized == mcpStateSettingEnabled || normalized == mcpStateSettingAllowNonLoopback {
		return InvalidArgument("MCP 服务开关必须通过专门的 MCP 管理接口修改，不能在系统设置中直接写入")
	}
	return nil
}

// guardSystemSettingKeys 拒绝批量写入中包含 MCP 自身状态键。
func guardSystemSettingKeys(values map[string]string, secrets map[string]settingsapp.SecretChange) error {
	// key 是当前待写入的普通设置键。
	for key := range values {
		if strings.TrimSpace(key) == mcpStateSettingEnabled || strings.TrimSpace(key) == mcpStateSettingAllowNonLoopback {
			return InvalidArgument("MCP 服务开关必须通过专门的 MCP 管理接口修改，不能在系统设置中直接写入")
		}
	}
	// key 是当前待写入的敏感设置键。
	for key := range secrets {
		if strings.TrimSpace(key) == mcpStateSettingEnabled || strings.TrimSpace(key) == mcpStateSettingAllowNonLoopback {
			return InvalidArgument("MCP 服务开关必须通过专门的 MCP 管理接口修改，不能在系统设置中直接写入")
		}
	}
	return nil
}

// sensitiveSettingKeys 从脱敏设置值中识别敏感键名；失败时返回空切片。
func sensitiveSettingKeys(p capability.SettingsPorts, values map[string]string) []string {
	// keys 收集当前系统中存在的敏感键名。
	keys := make([]string, 0, len(values))
	// key 是当前遍历到的设置键名。
	for key := range values {
		if p.IsSensitiveSettingKey(key) {
			keys = append(keys, key)
		}
	}
	return sortedStrings(keys)
}

// stringMapArg 读取字符串键值对象入参；缺省返回空表，值非字符串时返回中文参数错误。
func stringMapArg(args Arguments, name string) (map[string]string, error) {
	// raw、ok 是原始对象值及其存在性。
	raw, ok := args[name]
	if !ok {
		return map[string]string{}, nil
	}
	// object 是对象形态的通用值。
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, InvalidArgument("参数 " + name + " 必须是对象")
	}
	// values 是类型化后的字符串键值表。
	values := make(map[string]string, len(object))
	// key、rawValue 是当前键与其原始值。
	for key, rawValue := range object {
		// text、isText 是字符串形态的值及其类型断言结果。
		text, isText := rawValue.(string)
		if !isText {
			return nil, InvalidArgument("参数 " + name + "." + key + " 必须是字符串")
		}
		values[key] = text
	}
	return values, nil
}

// secretChangeMapArg 读取敏感设置三态命令对象；命令形状非法时返回中文参数错误。
func secretChangeMapArg(args Arguments, name string) (map[string]settingsapp.SecretChange, error) {
	// raw、ok 是原始对象值及其存在性。
	raw, ok := args[name]
	if !ok {
		return map[string]settingsapp.SecretChange{}, nil
	}
	// object 是对象形态的通用值。
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, InvalidArgument("参数 " + name + " 必须是对象")
	}
	// changes 是类型化后的三态命令表。
	changes := make(map[string]settingsapp.SecretChange, len(object))
	// key、rawChange 是当前敏感键与其命令对象。
	for key, rawChange := range object {
		// command 是单个敏感键的命令对象。
		command, isObject := rawChange.(map[string]any)
		if !isObject {
			return nil, InvalidArgument("参数 " + name + "." + key + " 必须是对象")
		}
		// action、isText 是命令动作及其类型断言结果。
		action, isText := command["action"].(string)
		if !isText {
			return nil, InvalidArgument("参数 " + name + "." + key + ".action 必须是字符串")
		}
		// value 是可选的新秘密值；非字符串或缺失时按空值处理，由应用服务校验 replace 必须非空。
		value, _ := command["value"].(string)
		changes[key] = settingsapp.SecretChange{Action: action, Value: value}
	}
	return changes, nil
}

// sortedKeys 返回泛型键值表的稳定排序键名切片。
func sortedKeys[V any](values map[string]V) []string {
	// keys 是全部键名。
	keys := make([]string, 0, len(values))
	// key 是当前键名。
	for key := range values {
		keys = append(keys, key)
	}
	return sortedStrings(keys)
}

// sortedStrings 返回按字典序升序排列的字符串副本，不修改入参。
func sortedStrings(values []string) []string {
	// sorted 是入参副本，避免影响调用方持有的切片。
	sorted := append([]string(nil), values...)
	// index 是插入排序的当前下标，规模很小无需引入排序依赖。
	for index := 1; index < len(sorted); index++ {
		// current 是待插入的键名。
		current := sorted[index]
		// position 是向前比较的位置。
		position := index - 1
		for position >= 0 && sorted[position] > current {
			sorted[position+1] = sorted[position]
			position--
		}
		sorted[position+1] = current
	}
	return sorted
}
