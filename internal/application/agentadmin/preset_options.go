// preset_options.go 平台级档位的展示元数据。
//
// 档位名单的权威来源是 internal/capability 的常量与 capability.Preset.Valid()；本文件
// 只补界面文案，不重复判定合法性。测试断言两者取值集合一致，避免出现「界面能选但内核
// 不认」的档位。

package agentadmin

import "xianyu-go/internal/capability"

// PresetOption 是一个档位的展示元数据，供界面渲染有序的档位选择器。
type PresetOption struct {
	// Value 是档位的稳定标识，写入配置时必须使用它。
	Value capability.Preset
	// Label 是界面显示名。
	Label string
	// Description 是一句话说明该档位放开到哪一步。
	Description string
}

// presetOptions 是平台级档位定义，切片顺序即由严到宽的展示顺序。
var presetOptions = []PresetOption{
	{Value: capability.PresetReadonly, Label: "只读", Description: "只查询与答复，不产生对外动作"},
	{Value: capability.PresetStandard, Label: "标准", Description: "在只读之上允许生成报价等对外承诺"},
	{Value: capability.PresetAdvanced, Label: "高级", Description: "在标准之上允许可逆的数据写入"},
}

// PresetOptions 返回平台级档位定义的有序副本，供界面渲染选择器。
//
// 返回副本而不是内部切片：调用方改写返回值不得影响全局档位定义。
func PresetOptions() []PresetOption {
	// out 是档位定义的可写副本。
	out := make([]PresetOption, len(presetOptions))
	copy(out, presetOptions)
	return out
}

// PresetValue 把外部输入的档位文本转为档位值，不做合法性判断。
//
// 之所以提供它：传输层不应为了构造请求参数而自行实现一份档位类型转换，更不应顺手
// 复制一份合法取值判断；合法性统一由写入路径判定，HTTP 层只负责搬运。
func PresetValue(raw string) capability.Preset {
	return capability.Preset(raw)
}
