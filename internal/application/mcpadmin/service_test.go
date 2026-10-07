package mcpadmin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRepository 是 MCP 管理仓储的确定性测试替身。
type fakeRepository struct {
	// enabled 是当前启用状态。
	enabled bool
	// allowNonLoopback 是当前网络策略。
	allowNonLoopback bool
	// token 是令牌配置态。
	token TokenStatus
	// rotated 是本次轮换返回的明文。
	rotated string
	// rotateAt 记录轮换调用收到的时刻。
	rotateAt time.Time
	// page 是审计查询返回结果。
	page AuditPage
	// lastFilter 记录审计查询收到的过滤条件。
	lastFilter AuditFilter
	// err 是任一方法返回的注入错误。
	err error
	// enabledSets 记录 SetEnabled 收到的值。
	enabledSets []bool
	// policySets 记录 SetAllowNonLoopback 收到的值。
	policySets []bool
	// revokeCalls 记录吊销调用次数。
	revokeCalls int
}

// Enabled 返回注入的启用状态。
func (f *fakeRepository) Enabled(context.Context) (bool, error) { return f.enabled, f.err }

// AllowNonLoopback 返回注入的网络策略。
func (f *fakeRepository) AllowNonLoopback(context.Context) (bool, error) {
	return f.allowNonLoopback, f.err
}

// SetEnabled 记录启用状态写入值。
func (f *fakeRepository) SetEnabled(_ context.Context, enabled bool) error {
	if f.err != nil {
		return f.err
	}
	f.enabledSets = append(f.enabledSets, enabled)
	return nil
}

// SetAllowNonLoopback 记录网络策略写入值。
func (f *fakeRepository) SetAllowNonLoopback(_ context.Context, allowed bool) error {
	if f.err != nil {
		return f.err
	}
	f.policySets = append(f.policySets, allowed)
	return nil
}

// TokenStatus 返回注入的令牌配置态。
func (f *fakeRepository) TokenStatus(context.Context) (TokenStatus, error) { return f.token, f.err }

// RotateToken 记录轮换时刻并返回注入的明文。
func (f *fakeRepository) RotateToken(_ context.Context, now time.Time) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.rotateAt = now
	return f.rotated, nil
}

// RevokeTokens 记录吊销调用。
func (f *fakeRepository) RevokeTokens(context.Context) error {
	if f.err != nil {
		return f.err
	}
	f.revokeCalls++
	return nil
}

// ListAudit 记录过滤条件并返回注入分页。
func (f *fakeRepository) ListAudit(_ context.Context, filter AuditFilter) (AuditPage, error) {
	if f.err != nil {
		return AuditPage{}, f.err
	}
	f.lastFilter = filter
	return f.page, nil
}

// fakeInvalidator 记录缓存失效调用次数。
type fakeInvalidator struct {
	// calls 是 Invalidate 被调用的次数。
	calls int
}

// Invalidate 记录一次缓存失效。
func (f *fakeInvalidator) Invalidate() { f.calls++ }

// TestStatusAggregatesNonSensitiveState 验证状态查询聚合三个非敏感字段且不触发缓存失效。
func TestStatusAggregatesNonSensitiveState(t *testing.T) {
	// repository 是注入的假仓储。
	repository := &fakeRepository{enabled: true, allowNonLoopback: false, token: TokenStatus{HasCurrent: true, CurrentCreatedAt: 100}}
	// invalidator 是失效计数器。
	invalidator := &fakeInvalidator{}
	// service 是待测应用服务。
	service := NewService(repository, invalidator)
	// status、err 是状态查询结果及错误。
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Enabled || status.AllowNonLoopback || !status.Token.HasCurrent || status.Token.CurrentCreatedAt != 100 {
		t.Fatalf("状态聚合异常: %+v", status)
	}
	if invalidator.calls != 0 {
		t.Fatalf("只读状态查询不应触发缓存失效: %d", invalidator.calls)
	}
}

// TestUpdateSettingsWritesBothAndInvalidates 验证更新同时写入开关与策略并立即失效缓存。
func TestUpdateSettingsWritesBothAndInvalidates(t *testing.T) {
	// repository 是注入的假仓储。
	repository := &fakeRepository{}
	// invalidator 是失效计数器。
	invalidator := &fakeInvalidator{}
	// service 是待测应用服务。
	service := NewService(repository, invalidator)
	// err 是更新设置用例的错误。
	if err := service.UpdateSettings(context.Background(), SettingsUpdate{Enabled: true, AllowNonLoopback: true}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if len(repository.enabledSets) != 1 || !repository.enabledSets[0] {
		t.Fatalf("启用状态未写入: %+v", repository.enabledSets)
	}
	if len(repository.policySets) != 1 || !repository.policySets[0] {
		t.Fatalf("网络策略未写入: %+v", repository.policySets)
	}
	if invalidator.calls != 1 {
		t.Fatalf("设置变更应失效缓存一次: %d", invalidator.calls)
	}
}

// TestRotateTokenUsesInjectedClockAndInvalidates 验证轮换使用注入时钟并返回一次性明文。
func TestRotateTokenUsesInjectedClockAndInvalidates(t *testing.T) {
	// repository 是携带明文响应的假仓储。
	repository := &fakeRepository{rotated: "plain-token"}
	// invalidator 是失效计数器。
	invalidator := &fakeInvalidator{}
	// fixed 是注入的确定时刻。
	fixed := time.Unix(1_700_000_000, 0)
	// service 是待测应用服务。
	service := NewServiceWithClock(repository, invalidator, func() time.Time { return fixed })
	// token、err 是轮换结果及错误。
	token, err := service.RotateToken(context.Background())
	if err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
	if token != "plain-token" {
		t.Fatalf("轮换明文异常: %q", token)
	}
	if !repository.rotateAt.Equal(fixed) {
		t.Fatalf("轮换未使用注入时钟: %v", repository.rotateAt)
	}
	if invalidator.calls != 1 {
		t.Fatalf("轮换应失效缓存一次: %d", invalidator.calls)
	}
}

// TestRevokeTokenInvalidates 验证吊销立即失效缓存。
func TestRevokeTokenInvalidates(t *testing.T) {
	// repository 是注入的假仓储。
	repository := &fakeRepository{}
	// invalidator 是失效计数器。
	invalidator := &fakeInvalidator{}
	// service 是待测应用服务。
	service := NewService(repository, invalidator)
	// err 是吊销令牌用例的错误。
	if err := service.RevokeToken(context.Background()); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if repository.revokeCalls != 1 || invalidator.calls != 1 {
		t.Fatalf("吊销行为异常: revoke=%d invalidate=%d", repository.revokeCalls, invalidator.calls)
	}
}

// TestListAuditPassesFilterThrough 验证审计查询透传过滤条件。
func TestListAuditPassesFilterThrough(t *testing.T) {
	// repository 携带固定分页结果。
	repository := &fakeRepository{page: AuditPage{Total: 3, Records: []AuditEntry{{ID: 1, Name: "system_ping"}}}}
	// service 是待测应用服务。
	service := NewService(repository, nil)
	// filter 是待透传的过滤条件。
	filter := AuditFilter{Limit: 20, Offset: 40, Category: "tool", SuccessState: 1}
	// page、err 是查询结果及错误。
	page, err := service.ListAudit(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if page.Total != 3 || len(page.Records) != 1 || page.Records[0].Name != "system_ping" {
		t.Fatalf("审计结果异常: %+v", page)
	}
	if repository.lastFilter != filter {
		t.Fatalf("过滤条件未透传: %+v", repository.lastFilter)
	}
}

// TestServiceReturnsNotConfiguredWithoutRepository 验证未装配仓储时所有用例返回统一错误。
func TestServiceReturnsNotConfiguredWithoutRepository(t *testing.T) {
	// service 是未注入仓储的应用服务。
	service := NewService(nil, nil)
	// err 是状态查询返回的未装配错误。
	if _, err := service.Status(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Status 应返回 ErrNotConfigured: %v", err)
	}
	// err 是轮换令牌返回的未装配错误。
	if _, err := service.RotateToken(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("RotateToken 应返回 ErrNotConfigured: %v", err)
	}
	// err 是吊销令牌返回的未装配错误。
	if err := service.RevokeToken(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("RevokeToken 应返回 ErrNotConfigured: %v", err)
	}
	// err 是更新设置返回的未装配错误。
	if err := service.UpdateSettings(context.Background(), SettingsUpdate{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("UpdateSettings 应返回 ErrNotConfigured: %v", err)
	}
	// err 是审计查询返回的未装配错误。
	if _, err := service.ListAudit(context.Background(), AuditFilter{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ListAudit 应返回 ErrNotConfigured: %v", err)
	}
}

// TestMutationPropagatesRepositoryError 验证仓储错误原样上抛。
func TestMutationPropagatesRepositoryError(t *testing.T) {
	// repository 注入固定错误。
	repository := &fakeRepository{err: errors.New("仓储失败")}
	// service 是待测应用服务。
	service := NewService(repository, nil)
	// err 是更新设置返回的仓储错误。
	if err := service.UpdateSettings(context.Background(), SettingsUpdate{}); err == nil {
		t.Fatal("更新应上抛仓储错误")
	}
}

// TestPageBoundsAndTotalPages 验证分页归一化与总页数计算。
func TestPageBoundsAndTotalPages(t *testing.T) {
	// page、size、offset 是归一化后的分页参数。
	page, size, offset := PageBounds(0, 0)
	if page != 1 || size != defaultPageSize || offset != 0 {
		t.Fatalf("默认分页异常: page=%d size=%d offset=%d", page, size, offset)
	}
	page, size, offset = PageBounds(3, 10)
	if page != 3 || size != 10 || offset != 20 {
		t.Fatalf("正常分页异常: page=%d size=%d offset=%d", page, size, offset)
	}
	page, size, _ = PageBounds(1, 1000)
	if size != maxPageSize {
		t.Fatalf("页大小未收敛: %d", size)
	}
	if TotalPages(0, 20) != 0 || TotalPages(21, 20) != 2 || TotalPages(20, 0) != 0 {
		t.Fatalf("总页数计算异常")
	}
}
