package ws

import (
	"context"
	"encoding/base64"
	"testing"
)

// TestHandleSyncExtraDispatchesCompensationPayload 验证同步状态补偿载荷会被解码并分发。
//
// 回归背景：2026-10-06 19:57 重连后平台把 sync stream 40 序号重置为 0，断线期间的付款卡片
// （seq 36）永久丢失。getState 返回的补偿载荷必须进入与正常推送相同的分发路径，否则本地
// 无从恢复这张卡片。
func TestHandleSyncExtraDispatchesCompensationPayload(t *testing.T) {
	// compensation 是 getState 状态体中携带的补偿业务载荷。
	compensation := `{"event":"paid","order_id":"o1"}`
	// encodedCompensation 是补偿载荷的 base64 编码。
	encodedCompensation := base64.StdEncoding.EncodeToString([]byte(compensation))
	// stateBody 模拟平台状态体：内嵌一个 syncPushPackage。
	stateBody := map[string]any{"syncPushPackage": map[string]any{
		"data": []any{map[string]any{"data": encodedCompensation}},
	}}
	// connection 返回该状态体作为 getState 响应。
	connection, _ := newAPIResponseConn(t, stateBody, 200)
	// received 记录分发到 onMessage 的补偿业务对象。
	received := make([]map[string]any, 0, 1)
	// err 保存同步状态处理的返回错误。
	err := connection.handleSyncExtra(context.Background(), map[string]any{"body": map[string]any{"syncExtraType": map[string]any{"type": 1}}}, func(decrypted map[string]any) {
		received = append(received, decrypted)
	})
	if err != nil {
		t.Fatalf("handleSyncExtra err=%v", err)
	}
	if len(received) != 1 || received[0]["event"] != "paid" || received[0]["order_id"] != "o1" {
		t.Fatalf("补偿载荷未按预期分发: %#v", received)
	}
}

// TestDispatchSyncStateBodySkipsUndispatchablePayloads 验证补偿分发对不可用载荷的跳过分支。
func TestDispatchSyncStateBodySkipsUndispatchablePayloads(t *testing.T) {
	// connection 是仅用于补偿分发分支的本地连接。
	connection, _ := newAPIResponseConn(t, nil, 200)
	// nilCallbackCount 记录 nil 回调场景下不应发生的分发。
	nilCallbackCount := 0
	// nil 回调时直接返回，不 panic。
	connection.dispatchSyncStateBody(map[string]any{"body": map[string]any{}}, nil)
	// 非对象状态体不携带可分发载荷。
	connection.dispatchSyncStateBody(map[string]any{"body": "not-an-object"}, func(map[string]any) { nilCallbackCount++ })
	// 对象状态体但不含 syncPushPackage 时静默跳过。
	connection.dispatchSyncStateBody(map[string]any{"body": map[string]any{"other": true}}, func(map[string]any) { nilCallbackCount++ })
	// 无效条目与解密失败条目都必须被跳过，且不阻断调用。
	invalidBody := map[string]any{"syncPushPackage": map[string]any{"data": []any{map[string]any{"noData": true}, map[string]any{"data": "not-base64"}}}}
	connection.dispatchSyncStateBody(map[string]any{"body": invalidBody}, func(map[string]any) { nilCallbackCount++ })
	if nilCallbackCount != 0 {
		t.Fatalf("不可用载荷不应触发分发: %d", nilCallbackCount)
	}
}
