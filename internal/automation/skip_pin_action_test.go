// skip_pin_action_test.go 覆盖「直接免拼」动作的安抚消息发送与免拼主流程。
package automation

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// skipPinTestServer 返回模拟免拼接口的 HTTP 服务，成功响应按线上格式返回 true。
func skipPinTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	// server 是模拟免拼端点的本地服务，测试用 SkipPinURL 注入。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"data":true},"ret":["SUCCESS::调用成功"]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// newSkipPinExecutor 构造带账号 Cookie、在线发送器和本地免拼端点的免拼动作执行器。
func newSkipPinExecutor(t *testing.T, sender *testSender, server *httptest.Server) *automationActionExecutor {
	t.Helper()
	// store、cleanup 保存免拼测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	t.Cleanup(cleanup)
	// ctx 保存数据库写入使用的测试上下文。
	ctx := context.Background()
	// initial 是免拼请求使用的账号 Cookie，必须带 MTOP 签名令牌。
	initial := "unb=123; _m_h5_tk=tk_1;"
	// err 保存账号 Cookie 写入结果。
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initial, `{"origin":"test"}`, 1); err != nil {
		t.Fatal(err)
	}
	return &automationActionExecutor{
		store:   store,
		senders: testSenderProvider{sender: sender},
		mtop: func() mtop.Client {
			return &mtop.ClientImpl{HTTPClient: server.Client(), SkipPinURL: server.URL + "/"}
		},
		logger: slog.Default(),
	}
}

// skipPinTestTask 返回免拼动作使用的任务事实，带完整的会话与买卖双方标识。
func skipPinTestTask() Task {
	return Task{
		AccountID: "cid",
		OrderID:   "order-1",
		ItemID:    "item-1",
		BuyerID:   "buyer-1",
		ChatID:    "chat-1",
	}
}

// TestSkipPinOrderSendsSoothMessageBeforeFreeShipping 验证配置安抚文案时，
// 免拼动作先向买家发送安抚消息，再调用免拼接口，两者都成功。
func TestSkipPinOrderSendsSoothMessageBeforeFreeShipping(t *testing.T) {
	// sender 记录发送给买家的安抚消息。
	sender := &testSender{}
	// executor 是注入账号凭证、在线发送器和本地免拼端点的执行器。
	executor := newSkipPinExecutor(t, sender, skipPinTestServer(t))
	// soothe 是规则配置的安抚文案。
	soothe := "请检查界面上有没有【直接拼成】按钮，如果有点击这个按钮即可成单，如果没有请等我帮你操作免拼。"
	// runErr 保存免拼动作执行结果。
	runErr := executor.skipPinOrder(context.Background(), skipPinTestTask(), db.AutomationAction{ActionType: ActionSkipPin, MessageTemplate: soothe}, true)
	if runErr != nil {
		t.Fatalf("免拼动作执行失败: %v", runErr)
	}
	if len(sender.texts) != 1 || sender.texts[0] != soothe {
		t.Fatalf("安抚消息未发送或内容不符: %v", sender.texts)
	}
}

// TestSkipPinOrderSkipsSoothMessageWhenNotConfigured 验证未配置安抚文案时，
// 免拼动作只执行免拼，不向买家发送任何消息。
func TestSkipPinOrderSkipsSoothMessageWhenNotConfigured(t *testing.T) {
	// sender 记录发送给买家的消息。
	sender := &testSender{}
	// executor 是注入账号凭证、在线发送器和本地免拼端点的执行器。
	executor := newSkipPinExecutor(t, sender, skipPinTestServer(t))
	// runErr 保存免拼动作执行结果。
	runErr := executor.skipPinOrder(context.Background(), skipPinTestTask(), db.AutomationAction{ActionType: ActionSkipPin, MessageTemplate: ""}, true)
	if runErr != nil {
		t.Fatalf("免拼动作执行失败: %v", runErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("未配置安抚文案不应发送消息: %v", sender.texts)
	}
}

// TestSkipPinOrderProceedsWhenSoothMessageFails 验证安抚消息发送失败时，
// 免拼主流程不被阻断，仍会继续调用免拼接口并成功。
func TestSkipPinOrderProceedsWhenSoothMessageFails(t *testing.T) {
	// sender 返回确定发送失败的发送器。
	sender := &testSender{err: fmt.Errorf("%w: websocket 尚未就绪", ErrMessageNotSent)}
	// executor 是注入账号凭证、失败发送器和本地免拼端点的执行器。
	executor := newSkipPinExecutor(t, sender, skipPinTestServer(t))
	// runErr 保存免拼动作执行结果。
	runErr := executor.skipPinOrder(context.Background(), skipPinTestTask(), db.AutomationAction{ActionType: ActionSkipPin, MessageTemplate: "安抚文案"}, true)
	if runErr != nil {
		t.Fatalf("安抚发送失败不应阻断免拼: %v", runErr)
	}
}

// TestSkipPinOrderSkipsSoothMessageWithoutChat 验证任务缺少会话标识时，
// 安抚消息不发送且不报错，免拼继续执行。
func TestSkipPinOrderSkipsSoothMessageWithoutChat(t *testing.T) {
	// sender 记录发送给买家的消息。
	sender := &testSender{}
	// executor 是注入账号凭证、在线发送器和本地免拼端点的执行器。
	executor := newSkipPinExecutor(t, sender, skipPinTestServer(t))
	// task 是缺少会话标识的免拼任务（订单同步兜底路径）。
	task := skipPinTestTask()
	task.ChatID = ""
	// runErr 保存免拼动作执行结果。
	runErr := executor.skipPinOrder(context.Background(), task, db.AutomationAction{ActionType: ActionSkipPin, MessageTemplate: "安抚文案"}, true)
	if runErr != nil {
		t.Fatalf("缺少会话标识不应阻断免拼: %v", runErr)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("缺少会话标识不应发送安抚消息: %v", sender.texts)
	}
}
