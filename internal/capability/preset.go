package capability

// Preset 是客服 Agent 的能力档位，决定单个账号可触达的能力子集。
//
// 档位是平台级常量，由代码定义、不可由用户增删或自定义：用户只需理解三个有序档位，
// 而不必逐个判断每个工具的危险程度。
type Preset string

const (
	// PresetReadonly 是只读档：仅允许只读能力，用于最保守的自动答复场景。
	PresetReadonly Preset = "readonly"
	// PresetStandard 是标准档：在只读之上放开报价与状态同步类能力，是租户的推荐默认值。
	PresetStandard Preset = "standard"
	// PresetAdvanced 是高级档：在标准之上放开可逆的数据写入能力，需管理员显式开启。
	PresetAdvanced Preset = "advanced"
)

// DefaultPreset 是租户与账号均未配置时生效的兜底档位，取最保守值。
const DefaultPreset = PresetReadonly

// order 返回档位序数，用于比较「能力要求的最低档位」与「账号当前档位」的高低。
//
// 未知档位返回 -1，使比较结果恒为不满足，保证 fail-closed。
func (p Preset) order() int {
	switch p {
	case PresetReadonly:
		return 0
	case PresetStandard:
		return 1
	case PresetAdvanced:
		return 2
	default:
		return -1
	}
}

// Valid 报告档位是否为本包定义的合法取值。
func (p Preset) Valid() bool {
	return p.order() >= 0
}

// Allows 报告当前档位是否达到能力要求的最低档位 required。
//
// 任一档位非法（含空串）时返回 false，因此未配置档位的账号不会意外获得能力。
func (p Preset) Allows(required Preset) bool {
	// current、needed 分别是账号当前档位与能力最低要求档位的序数。
	current, needed := p.order(), required.order()
	if current < 0 || needed < 0 {
		return false
	}
	return current >= needed
}
