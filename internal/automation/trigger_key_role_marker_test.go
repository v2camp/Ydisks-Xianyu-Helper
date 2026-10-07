package automation

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestBuildTriggerKeyAndTaskKeyReuseRoleVerificationMarker 验证触发键与待核验键都优先沿用未知角色任务的稳定防重键。
func TestBuildTriggerKeyAndTaskKeyReuseRoleVerificationMarker(t *testing.T) {
	// baseTask 是带订单号的基础任务；marker 命中时订单号键必须被让位。
	baseTask := Task{TriggerType: TriggerOrderPaid, OrderID: "order-1"}

	// cases 覆盖四种输入形态：命中沿用、类型不符回退、空白回退与无原始数据回退。
	cases := []struct {
		// name 描述当前用例的业务场景。
		name string
		// raw 是注入任务原始字段的稳定防重键候选值。
		raw map[string]any
		// wantKey 是 buildTriggerKey 与 roleVerificationTaskKey 应共同返回的结果。
		wantKey string
	}{
		{name: "沿用已固化的字符串标记", raw: map[string]any{roleVerificationTaskKeyField: "  ws-key-1  "}, wantKey: "ws-key-1"},
		{name: "非字符串标记回退到订单键", raw: map[string]any{roleVerificationTaskKeyField: 42}, wantKey: "order_paid:order-1"},
		{name: "空白标记回退到订单键", raw: map[string]any{roleVerificationTaskKeyField: "   "}, wantKey: "order_paid:order-1"},
		{name: "无原始数据回退到订单键", raw: nil, wantKey: "order_paid:order-1"},
	}

	// tc 在闭包中捕获当前用例，两个入口共用同一断言。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// task 是按当前用例装配的待键任务。
			task := baseTask
			task.Raw = tc.raw
			// gotKey 是触发键入口的实际结果。
			gotKey := buildTriggerKey(task)
			// gotTaskKey 是待核验键入口的实际结果。
			gotTaskKey := roleVerificationTaskKey(task)
			if gotKey != tc.wantKey {
				t.Fatalf("buildTriggerKey = %q, want %q", gotKey, tc.wantKey)
			}
			if gotTaskKey != tc.wantKey {
				t.Fatalf("roleVerificationTaskKey = %q, want %q", gotTaskKey, tc.wantKey)
			}
		})
	}
}

// TestRoleVerificationTaskKeyHashFallback 验证原始报文不可序列化且无业务键时，待核验键按脱敏事实拼接后哈希。
func TestRoleVerificationTaskKeyHashFallback(t *testing.T) {
	// badTask 不带订单号且 Raw 含 channel 值：json.Marshal 必然失败，强制进入哈希回退分支。
	badTask := Task{TriggerType: TriggerOrderPaid, Raw: map[string]any{"bad": make(chan int)}}
	// fallbackDigest 复算回退分支的期望指纹：七个脱敏字段以空字节拼接，仅触发类型非空。
	fallbackDigest := sha256.Sum256([]byte("order_paid\x00\x00\x00\x00\x00\x00"))
	// wantKey 是回退分支应返回的完整待核验键。
	wantKey := "role_verification:order_paid:" + hex.EncodeToString(fallbackDigest[:])
	// gotKey 是实际生成的待核验键。
	gotKey := roleVerificationTaskKey(badTask)
	if gotKey != wantKey {
		t.Fatalf("roleVerificationTaskKey = %q, want %q", gotKey, wantKey)
	}
}
