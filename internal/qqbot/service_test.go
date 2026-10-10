package qqbot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// salesReaderFake 是销量端口的测试替身。
type salesReaderFake struct {
	// snapshot 是预设返回的销量快照。
	snapshot SalesSnapshot
	// err 是预设返回的错误。
	err error
	// gotUserID 记录最近一次调用传入的管理员标识。
	gotUserID int64
	// calls 记录调用次数，用于断言策略拒绝时不触达读取端口。
	calls int
}

// TodaySales 返回预设快照并记录调用参数。
func (f *salesReaderFake) TodaySales(_ context.Context, adminUserID int64) (SalesSnapshot, error) {
	f.gotUserID = adminUserID
	f.calls++
	return f.snapshot, f.err
}

// healthReaderFake 是健康端口的测试替身。
type healthReaderFake struct {
	// snapshot 是预设返回的健康快照。
	snapshot HealthSnapshot
	// err 是预设返回的错误。
	err error
	// calls 记录调用次数，用于断言策略拒绝时不触达读取端口。
	calls int
}

// Snapshot 返回预设快照。
func (f *healthReaderFake) Snapshot(_ context.Context, _ int64) (HealthSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

// chatsReaderFake 是会话端口的测试替身。
type chatsReaderFake struct {
	// digest 是预设返回的会话摘要。
	digest ChatDigest
	// err 是预设返回的错误。
	err error
	// gotPerAccount 记录最近一次调用传入的每账号会话数。
	gotPerAccount int
	// calls 记录调用次数，用于断言策略拒绝时不触达读取端口。
	calls int
}

// RecentChats 返回预设摘要并记录调用参数。
func (f *chatsReaderFake) RecentChats(_ context.Context, _ int64, perAccount int) (ChatDigest, error) {
	f.gotPerAccount = perAccount
	f.calls++
	return f.digest, f.err
}

// identityFake 是管理员身份端口的测试替身。
type identityFake struct {
	// userID 是预设返回的管理员标识。
	userID int64
	// err 是预设返回的错误。
	err error
}

// AdminUserID 返回预设管理员标识。
func (f *identityFake) AdminUserID(_ context.Context) (int64, error) {
	return f.userID, f.err
}

// authorizerFake 是授权端口的测试替身。
type authorizerFake struct {
	// enabled 是预设的总开关状态。
	enabled bool
	// allowed 是预设的白名单命中结果。
	allowed bool
	// gotOpenID 记录最近一次校验的发送者标识。
	gotOpenID string
}

// Enabled 返回预设的总开关状态。
func (f *authorizerFake) Enabled(_ context.Context) bool { return f.enabled }

// Allowed 返回预设的白名单命中结果并记录发送者标识。
func (f *authorizerFake) Allowed(_ context.Context, openID string) bool {
	f.gotOpenID = openID
	return f.allowed
}

// newTestService 构造注入全部替身的命令服务，返回服务与各替身便于断言。
func newFixtureService() (*Service, *salesReaderFake, *healthReaderFake, *chatsReaderFake) {
	// sales 是销量替身。
	sales := &salesReaderFake{}
	// health 是健康替身。
	health := &healthReaderFake{}
	// chats 是会话替身。
	chats := &chatsReaderFake{}
	// service 是注入替身后构造的命令服务。
	service := newTestService(sales, health, chats, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true})
	// fixed 是固定时间源，保证健康文案时刻稳定。
	fixed := time.Date(2026, 10, 8, 0, 55, 0, 0, time.Local)
	service.now = /* 固定时间源避免测试受运行时钟影响。 */ func() time.Time { return fixed }
	return service, sales, health, chats
}

// TestHandleDisabledReturnsErrCommandsDisabled 验证开关关闭时不产出任何回复。
func TestHandleDisabledReturnsErrCommandsDisabled(t *testing.T) {
	// service 是开关关闭的命令服务。
	service := newTestService(&salesReaderFake{}, &healthReaderFake{}, &chatsReaderFake{}, &identityFake{userID: 7}, &authorizerFake{enabled: false, allowed: true})
	// reply 是返回的回复文本。
	// err 是返回的错误。
	reply, err := service.Handle(context.Background(), "openid-1", "销量")
	if !errors.Is(err, ErrCommandsDisabled) || reply != "" {
		t.Fatalf("开关关闭应返回 ErrCommandsDisabled 且无回复，实际 reply=%q err=%v", reply, err)
	}
}

// TestHandleUnauthorizedReturnsErrUnauthorized 验证白名单外的发送者被拒绝且不下读数据。
func TestHandleUnauthorizedReturnsErrUnauthorized(t *testing.T) {
	// service 是白名单不命中的命令服务。
	service, sales, _, _ := newFixtureService()
	service.authorizer = &authorizerFake{enabled: true, allowed: false}
	// reply 是返回的回复文本。
	// err 是返回的错误。
	reply, err := service.Handle(context.Background(), "openid-1", "销量")
	if !errors.Is(err, ErrUnauthorized) || reply != "" {
		t.Fatalf("未授权应返回 ErrUnauthorized 且无回复，实际 reply=%q err=%v", reply, err)
	}
	if sales.gotUserID != 0 {
		t.Fatalf("未授权时不应读取销量，实际读到 userID=%d", sales.gotUserID)
	}
}

// TestHandleSalesCommand 验证销量命令透传管理员身份并渲染快照。
func TestHandleSalesCommand(t *testing.T) {
	// service 是注入替身的命令服务。
	service, sales, _, _ := newFixtureService()
	sales.snapshot = SalesSnapshot{Date: "2026-10-08", Accounts: []AccountSales{{AccountID: "a", AccountName: "主力号", Orders: 2, AmountFen: 9900}}, TotalOrders: 2, AmountFen: 9900}
	// reply 是返回的回复文本。
	reply, err := service.Handle(context.Background(), "openid-1", "销量")
	if err != nil {
		t.Fatalf("销量命令不应失败，实际 err=%v", err)
	}
	if !strings.Contains(reply, "· 主力号：2 单 ¥99.00") || !strings.Contains(reply, "合计：2 单 ¥99.00") {
		t.Fatalf("销量回复内容不符，实际=%q", reply)
	}
	if sales.gotUserID != 7 {
		t.Fatalf("销量命令应透传管理员标识 7，实际=%d", sales.gotUserID)
	}
}

// TestHandleHealthCommand 验证健康命令渲染四维快照。
func TestHandleHealthCommand(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, health, _ := newFixtureService()
	health.snapshot = HealthSnapshot{DatabaseOK: true, Accounts: []AccountHealth{{AccountID: "a", AccountName: "主力号", State: "online", Connected: true}}, SilenceMinutes: 5, SilenceThreshold: 180}
	// reply 是返回的回复文本。
	reply, err := service.Handle(context.Background(), "openid-1", "健康度")
	if err != nil {
		t.Fatalf("健康命令不应失败，实际 err=%v", err)
	}
	if !strings.Contains(reply, "数据库：正常") || !strings.Contains(reply, "系统健康（10-08 00:55）") {
		t.Fatalf("健康回复内容不符，实际=%q", reply)
	}
}

// TestHandleChatsCommandUsesDefaultPerAccount 验证会话命令使用默认的每账号会话数。
func TestHandleChatsCommandUsesDefaultPerAccount(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, _, chats := newFixtureService()
	chats.digest = ChatDigest{Accounts: []AccountChats{{AccountID: "a", AccountName: "主力号", Chats: []ChatBrief{{ChatID: "c", PeerName: "张三", ItemTitle: "商品", Turns: []ChatTurn{{Direction: "incoming", Content: "在吗"}}}}}}}
	// reply 是返回的回复文本。
	reply, err := service.Handle(context.Background(), "openid-1", "会话")
	if err != nil {
		t.Fatalf("会话命令不应失败，实际 err=%v", err)
	}
	if !strings.Contains(reply, "【主力号】") || !strings.Contains(reply, "买：在吗") {
		t.Fatalf("会话回复内容不符，实际=%q", reply)
	}
	if chats.gotPerAccount != defaultChatsPerAccount {
		t.Fatalf("会话命令应请求默认 %d 个会话，实际=%d", defaultChatsPerAccount, chats.gotPerAccount)
	}
}

// TestHandleUnknownCommandReturnsHelp 验证未知输入回退到命令清单。
func TestHandleUnknownCommandReturnsHelp(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, _, _ := newFixtureService()
	// reply 是返回的回复文本。
	reply, err := service.Handle(context.Background(), "openid-1", "今天天气怎么样")
	if err != nil {
		t.Fatalf("未知命令不应失败，实际 err=%v", err)
	}
	if !strings.Contains(reply, "未识别的命令") || !strings.Contains(reply, "销量 / 今日销量") {
		t.Fatalf("未知命令应回退命令清单，实际=%q", reply)
	}
}

// TestHandleAdminResolveFailure 验证管理员身份不可用时不产出回复。
func TestHandleAdminResolveFailure(t *testing.T) {
	// service 是注入替身的命令服务。
	service, _, _, _ := newFixtureService()
	service.identity = &identityFake{userID: 0, err: errors.New("管理员不存在")}
	// reply 是返回的回复文本。
	// err 是返回的错误。
	reply, err := service.Handle(context.Background(), "openid-1", "销量")
	if err == nil || reply != "" {
		t.Fatalf("身份不可用应返回错误且无回复，实际 reply=%q err=%v", reply, err)
	}
}

// TestHandleReaderErrorPropagates 验证数据读取失败时错误透传给调用方。
func TestHandleReaderErrorPropagates(t *testing.T) {
	// service 是注入替身的命令服务。
	service, sales, _, _ := newFixtureService()
	sales.err = errors.New("查询失败")
	// reply 是返回的回复文本。
	// err 是返回的错误。
	reply, err := service.Handle(context.Background(), "openid-1", "销量")
	if err == nil || reply != "" {
		t.Fatalf("读取失败应返回错误且无回复，实际 reply=%q err=%v", reply, err)
	}
}

// TestHandleMissingPortsReportUnavailable 验证端口缺失时返回明确错误而不是 panic。
func TestHandleMissingPortsReportUnavailable(t *testing.T) {
	// cases 是缺失端口构造出的服务与其应当失败的命令。
	cases := map[string]struct {
		// service 是缺失某一端口的命令服务。
		service *Service
		// command 是该服务必然执行失败的命令文本。
		command string
	}{
		"缺销量": {newTestService(nil, &healthReaderFake{}, &chatsReaderFake{}, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true}), "销量"},
		"缺健康": {newTestService(&salesReaderFake{}, nil, &chatsReaderFake{}, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true}), "健康"},
		"缺会话": {newTestService(&salesReaderFake{}, &healthReaderFake{}, nil, &identityFake{userID: 7}, &authorizerFake{enabled: true, allowed: true}), "会话"},
		"缺身份": {newTestService(&salesReaderFake{}, &healthReaderFake{}, &chatsReaderFake{}, nil, &authorizerFake{enabled: true, allowed: true}), "销量"},
		"缺授权": {newTestService(&salesReaderFake{}, &healthReaderFake{}, &chatsReaderFake{}, &identityFake{userID: 7}, nil), "销量"},
	}
	// name 是用例名称。
	// item 是当前待验证的服务与命令组合。
	for name, item := range cases {
		// err 是返回的错误。
		if _, err := item.service.Handle(context.Background(), "openid-1", item.command); err == nil {
			t.Fatalf("%s 执行 %s 应返回错误", name, item.command)
		}
	}
}

// TestFormatUnauthorizedIncludesOpenID 验证未授权文案回显发送者标识，便于管理员加入白名单。
func TestFormatUnauthorizedIncludesOpenID(t *testing.T) {
	// got 是带发送者标识的未授权文案。
	got := FormatUnauthorized("openid-abc")
	if !strings.Contains(got, "openid-abc") || !strings.Contains(got, "未授权") {
		t.Fatalf("未授权文案应回显标识，实际=%q", got)
	}
	// gotEmpty 是空标识时的未授权文案。
	if gotEmpty := FormatUnauthorized("  "); strings.Contains(gotEmpty, "openid") {
		t.Fatalf("空标识不应回显 openid 字样，实际=%q", gotEmpty)
	}
}

// TestFormatFailureHidesInternalDetails 验证失败兜底文案不含内部错误细节。
func TestFormatFailureHidesInternalDetails(t *testing.T) {
	// got 是失败兜底文案。
	got := FormatFailure()
	if !strings.Contains(got, "命令执行失败") || strings.Contains(got, "sql") || strings.Contains(got, "SELECT") {
		t.Fatalf("失败文案不应泄露内部细节，实际=%q", got)
	}
}

// newTestService 用给定端口构造带恒放行策略的入站命令服务。
//
// 策略放行是既有命令行为不变的前提：本文件的断言全部针对授权、读取与渲染，
// 策略本身的判定单独由 policy_test.go 覆盖。
func newTestService(sales SalesReader, health HealthReader, chats ChatReader, identity IdentityResolver, authorizer Authorizer) *Service {
	return NewService(sales, health, chats, identity, authorizer, allowAllPolicy{})
}
