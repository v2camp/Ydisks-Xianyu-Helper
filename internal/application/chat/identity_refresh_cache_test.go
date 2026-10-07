package chat

import (
	"context"
	"sync/atomic"
	"testing"
)

// countingIdentityResolver 统计平台身份查询次数，用于验证已解析会话不再重复查询。
type countingIdentityResolver struct {
	// calls 记录 Resolve 被调用的次数。
	calls *atomic.Int32
	// identity 是返回给调用方的展示身份。
	identity Identity
}

// Resolve 累加计数并返回预设身份，不访问真实平台。
func (r countingIdentityResolver) Resolve(context.Context, string, string) (Identity, error) {
	r.calls.Add(1)
	return r.identity, nil
}

// TestResolveSessionIdentitySkipsAlreadyResolvedSession 验证本地已有名称与头像时不再请求平台。
func TestResolveSessionIdentitySkipsAlreadyResolvedSession(t *testing.T) {
	// calls 统计平台身份查询次数。
	calls := &atomic.Int32{}
	// service 是带计数身份替身的聊天应用服务。
	service := NewWithIdentity(&fakeRepository{}, countingIdentityResolver{calls: calls, identity: Identity{PeerName: "新名称", PeerAvatar: "新头像"}})
	// session 是本地已缓存完整展示身份的会话。
	session := Session{AccountID: "account-1", ChatID: "chat-1", PeerUserID: "buyer-1", PeerName: "旧名称", PeerAvatar: "旧头像"}
	// resolved、resolveErr 保存补全后的会话与错误；必须原样返回且不触发平台查询。
	resolved, resolveErr := service.ResolveSessionIdentity(context.Background(), session)
	if resolveErr != nil || resolved != session {
		t.Fatalf("已解析会话被改动 resolved=%+v err=%v", resolved, resolveErr)
	}
	// skippedCalls 是已解析会话触发的平台查询次数；必须为零。
	skippedCalls := calls.Load()
	if skippedCalls != 0 {
		t.Fatalf("已解析会话仍发起 %d 次平台身份查询", skippedCalls)
	}
}

// TestResolveSessionIdentityStillFetchesWhenIdentityIncomplete 验证名称或头像缺失时仍会补全。
func TestResolveSessionIdentityStillFetchesWhenIdentityIncomplete(t *testing.T) {
	// calls 统计平台身份查询次数。
	calls := &atomic.Int32{}
	// service 是带计数身份替身的聊天应用服务。
	service := NewWithIdentity(&fakeRepository{}, countingIdentityResolver{calls: calls, identity: Identity{PeerName: "新名称", PeerAvatar: "新头像"}})
	// session 只有名称、缺少头像，仍属于需要补全的会话。
	session := Session{AccountID: "account-1", ChatID: "chat-1", PeerUserID: "buyer-1", PeerName: "旧名称"}
	// resolved、resolveErr 保存补全后的会话与错误。
	resolved, resolveErr := service.ResolveSessionIdentity(context.Background(), session)
	if resolveErr != nil || resolved.PeerAvatar != "新头像" {
		t.Fatalf("缺头像会话未补全 resolved=%+v err=%v", resolved, resolveErr)
	}
	// incompleteCalls 是缺头像会话触发的平台查询次数；必须恰好一次。
	incompleteCalls := calls.Load()
	if incompleteCalls != 1 {
		t.Fatalf("缺头像会话平台查询次数=%d want 1", incompleteCalls)
	}
}

// TestRefreshSessionIdentitiesOnlyQueriesIncompleteSessions 验证批量刷新只查询需要补全的会话。
func TestRefreshSessionIdentitiesOnlyQueriesIncompleteSessions(t *testing.T) {
	// calls 统计平台身份查询次数。
	calls := &atomic.Int32{}
	// service 是带计数身份替身的聊天应用服务。
	service := NewWithIdentity(&fakeRepository{}, countingIdentityResolver{calls: calls, identity: Identity{PeerName: "新名称", PeerAvatar: "新头像"}})
	// sessions 混入已完整解析与需要补全的会话，模拟线上存在大量历史会话的列表。
	sessions := []Session{
		{AccountID: "account-1", ChatID: "chat-1", PeerUserID: "buyer-1", PeerName: "已解析", PeerAvatar: "已解析头像"},
		{AccountID: "account-1", ChatID: "chat-2", PeerUserID: "buyer-2", PeerName: "已解析", PeerAvatar: "已解析头像"},
		{AccountID: "account-1", ChatID: "chat-3", PeerUserID: "buyer-3"},
	}
	// refreshed、refreshErr 保存批量补全结果与首个平台错误。
	refreshed, refreshErr := service.RefreshSessionIdentities(context.Background(), "account-1", sessions)
	if refreshErr != nil || len(refreshed) != len(sessions) {
		t.Fatalf("批量补全异常 count=%d err=%v", len(refreshed), refreshErr)
	}
	// batchCalls 是批量刷新触发的平台查询次数；只有待补全会话会被查询。
	batchCalls := calls.Load()
	if batchCalls != 1 {
		t.Fatalf("批量刷新平台查询次数=%d want 1", batchCalls)
	}
	if refreshed[2].PeerName != "新名称" {
		t.Fatalf("待补全会话未被补全 %+v", refreshed[2])
	}
}
