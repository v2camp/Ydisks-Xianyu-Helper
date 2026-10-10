package agentadmin

import (
	"testing"

	"xianyu-go/internal/capability"
)

// TestPresetOptionsCoverEveryKnownPreset 验证展示元数据与内核档位常量一一对应。
//
// 该用例同时是「新增档位必须补文案」的护栏：内核新增一档而这里没跟上时会失败。
func TestPresetOptionsCoverEveryKnownPreset(t *testing.T) {
	// options 是平台级档位展示定义。
	options := PresetOptions()
	// seen 记录已出现的档位，用于检测重复。
	seen := make(map[capability.Preset]bool, len(options))
	// option 是当前遍历到的档位展示项。
	for _, option := range options {
		if !option.Value.Valid() {
			t.Fatalf("档位展示项 %q 不是内核认可的档位", option.Value)
		}
		if seen[option.Value] {
			t.Fatalf("档位 %q 重复出现", option.Value)
		}
		seen[option.Value] = true
		if option.Label == "" || option.Description == "" {
			t.Fatalf("档位 %q 缺少展示文案", option.Value)
		}
	}
	// expected 是内核当前定义的三个档位，顺序即由严到宽。
	expected := []capability.Preset{capability.PresetReadonly, capability.PresetStandard, capability.PresetAdvanced}
	if len(options) != len(expected) {
		t.Fatalf("档位展示项=%d 个，内核定义了 %d 个；新增档位必须同步补文案", len(options), len(expected))
	}
	// value 是当前期望必须出现的档位。
	for _, value := range expected {
		if !seen[value] {
			t.Fatalf("内核档位 %q 缺少展示元数据", value)
		}
	}
}

// TestPresetOptionsReturnsCopy 验证调用方改写返回值不会污染全局档位定义。
func TestPresetOptionsReturnsCopy(t *testing.T) {
	// options 是首次读取到的档位定义。
	options := PresetOptions()
	options[0].Label = "被改写"
	// again 是再次读取到的档位定义。
	again := PresetOptions()
	if again[0].Label == "被改写" {
		t.Fatal("档位定义被调用方改写污染")
	}
}
