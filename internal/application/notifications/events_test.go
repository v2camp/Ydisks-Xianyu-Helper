package notifications

import (
	"context"
	"errors"
	"testing"
)

// eventRepositoryStub 为站内通知事件用例提供可控的查询与标记结果。
type eventRepositoryStub struct {
	// userItems 是租户视图返回的事件。
	userItems []EventSummary
	// userUnread 是租户视图未读数。
	userUnread int
	// userLimit 记录租户列表实际收到的上限。
	userLimit int
	// adminItems 是系统视图返回的事件。
	adminItems []EventSummary
	// adminUnread 是系统视图未读数。
	adminUnread int
	// adminLimit 记录系统列表实际收到的上限。
	adminLimit int
	// markUserResult 是租户单条已读的预置结果。
	markUserResult bool
	// markUserID、markUserEventID 记录租户单条已读参数。
	markUserID, markUserEventID int64
	// markAllUserCount、markAllUserID 记录租户全部已读结果与参数。
	markAllUserCount int64
	// markAllUserID 记录租户全部已读参数。
	markAllUserID int64
	// markAdminResult 是系统单条已读的预置结果。
	markAdminResult bool
	// markAdminEventID 记录系统单条已读参数。
	markAdminEventID int64
	// markAllAdminCount 是系统全部已读的预置结果。
	markAllAdminCount int64
	// listErr 是列表查询的预置故障。
	listErr error
}

// ListForUser 返回预置租户事件并记录上限。
func (r *eventRepositoryStub) ListForUser(_ context.Context, _ int64, limit int) ([]EventSummary, error) {
	r.userLimit = limit
	return r.userItems, r.listErr
}

// CountUnreadForUser 返回预置租户未读数。
func (r *eventRepositoryStub) CountUnreadForUser(context.Context, int64) (int, error) {
	return r.userUnread, nil
}

// MarkReadForUser 记录租户单条已读参数并返回预置结果。
func (r *eventRepositoryStub) MarkReadForUser(_ context.Context, userID, eventID int64) (bool, error) {
	r.markUserID, r.markUserEventID = userID, eventID
	return r.markUserResult, nil
}

// MarkAllReadForUser 记录租户全部已读参数并返回预置结果。
func (r *eventRepositoryStub) MarkAllReadForUser(_ context.Context, userID int64) (int64, error) {
	r.markAllUserID = userID
	return r.markAllUserCount, nil
}

// ListForAdmin 返回预置系统事件并记录上限。
func (r *eventRepositoryStub) ListForAdmin(_ context.Context, limit int) ([]EventSummary, error) {
	r.adminLimit = limit
	return r.adminItems, r.listErr
}

// CountUnreadForAdmin 返回预置系统未读数。
func (r *eventRepositoryStub) CountUnreadForAdmin(context.Context) (int, error) {
	return r.adminUnread, nil
}

// MarkReadForAdmin 记录系统单条已读参数并返回预置结果。
func (r *eventRepositoryStub) MarkReadForAdmin(_ context.Context, eventID int64) (bool, error) {
	r.markAdminEventID = eventID
	return r.markAdminResult, nil
}

// MarkAllReadForAdmin 返回预置的系统全部已读结果。
func (r *eventRepositoryStub) MarkAllReadForAdmin(context.Context) (int64, error) {
	return r.markAllAdminCount, nil
}

// TestEventInboxServiceUserAndAdminPaths 验证租户与系统视图的列表、未读与已读路径。
func TestEventInboxServiceUserAndAdminPaths(t *testing.T) {
	// repository 是本次测试使用的事件仓储替身。
	repository := &eventRepositoryStub{
		userItems: []EventSummary{{ID: 1, Scope: "tenant"}}, userUnread: 3,
		adminItems: []EventSummary{{ID: 2, Scope: "system"}}, adminUnread: 1,
		markUserResult: true, markAllUserCount: 4, markAdminResult: true, markAllAdminCount: 2,
	}
	// service 是待验证的站内通知中心应用服务。
	service := NewEventInboxService(repository)
	// items、unread、listErr 保存租户列表结果与错误；limit=0 应收口为默认 50。
	items, unread, listErr := service.ListForUser(context.Background(), 7, 0)
	if listErr != nil || len(items) != 1 || unread != 3 || repository.userLimit != 50 {
		t.Fatalf("tenant list items=%+v unread=%d limit=%d err=%v", items, unread, repository.userLimit, listErr)
	}
	// markErr 表示租户单条已读结果。
	if markErr := service.MarkReadForUser(context.Background(), 7, 1); markErr != nil || repository.markUserID != 7 || repository.markUserEventID != 1 {
		t.Fatalf("tenant mark err=%v user=%d event=%d", markErr, repository.markUserID, repository.markUserEventID)
	}
	// updated、markAllErr 保存租户全部已读结果。
	updated, markAllErr := service.MarkAllReadForUser(context.Background(), 7)
	if markAllErr != nil || updated != 4 || repository.markAllUserID != 7 {
		t.Fatalf("tenant mark all updated=%d user=%d err=%v", updated, repository.markAllUserID, markAllErr)
	}
	// adminItems、adminUnread、adminErr 保存系统列表结果；limit=500 应收口为默认 50。
	adminItems, adminUnread, adminErr := service.ListForAdmin(context.Background(), 500)
	if adminErr != nil || len(adminItems) != 1 || adminUnread != 1 || repository.adminLimit != 50 {
		t.Fatalf("admin list items=%+v unread=%d limit=%d err=%v", adminItems, adminUnread, repository.adminLimit, adminErr)
	}
	// adminMarkErr 表示系统单条已读结果。
	if adminMarkErr := service.MarkReadForAdmin(context.Background(), 2); adminMarkErr != nil || repository.markAdminEventID != 2 {
		t.Fatalf("admin mark err=%v event=%d", adminMarkErr, repository.markAdminEventID)
	}
	// adminUpdated、adminMarkAllErr 保存系统全部已读结果。
	adminUpdated, adminMarkAllErr := service.MarkAllReadForAdmin(context.Background())
	if adminMarkAllErr != nil || adminUpdated != 2 {
		t.Fatalf("admin mark all updated=%d err=%v", adminUpdated, adminMarkAllErr)
	}
}

// TestEventInboxServiceNotFoundAndInvalidInput 验证未命中事件与非法参数统一返回应用错误。
func TestEventInboxServiceNotFoundAndInvalidInput(t *testing.T) {
	// repository 是单条已读未命中的仓储替身。
	repository := &eventRepositoryStub{}
	// service 是待验证的站内通知中心应用服务。
	service := NewEventInboxService(repository)
	// userMarkErr 验证租户事件不存在时返回统一未找到错误。
	if userMarkErr := service.MarkReadForUser(context.Background(), 7, 1); !errors.Is(userMarkErr, ErrEventNotFound) {
		t.Fatalf("tenant mark missing err=%v", userMarkErr)
	}
	// adminMarkErr 验证系统事件不存在时返回统一未找到错误。
	if adminMarkErr := service.MarkReadForAdmin(context.Background(), 1); !errors.Is(adminMarkErr, ErrEventNotFound) {
		t.Fatalf("admin mark missing err=%v", adminMarkErr)
	}
	// invalidCases 保存所有应返回参数错误的调用。
	invalidCases := []struct {
		// name 是当前用例名称。
		name string
		// call 执行一次应失败的调用。
		call func() error
	}{
		{name: "nil service", call: func() error {
			// err 是空服务列表参数校验错误。
			_, _, err := NewEventInboxService(nil).ListForUser(context.Background(), 7, 10)
			return err
		}},
		{name: "zero user", call: func() error {
			// err 是零用户列表参数校验错误。
			_, _, err := service.ListForUser(context.Background(), 0, 10)
			return err
		}},
		{name: "zero mark user", call: func() error { return service.MarkReadForUser(context.Background(), 0, 1) }},
		{name: "zero event", call: func() error { return service.MarkReadForUser(context.Background(), 7, 0) }},
		{name: "zero admin event", call: func() error { return service.MarkReadForAdmin(context.Background(), 0) }},
		{name: "nil admin repository", call: func() error {
			// err 是空仓储管理员列表参数校验错误。
			_, _, err := NewEventInboxService(nil).ListForAdmin(context.Background(), 10)
			return err
		}},
	}
	// invalidCase 是当前待执行的非法参数用例。
	for _, invalidCase := range invalidCases {
		// err 是当前用例返回的应用错误。
		if err := invalidCase.call(); !errors.Is(err, ErrEventInvalidInput) {
			t.Fatalf("%s err=%v", invalidCase.name, err)
		}
	}
}

// TestClampEventLimit 验证事件列表上限的默认值与边界。
func TestClampEventLimit(t *testing.T) {
	// cases 保存输入到期望上限的映射。
	cases := map[int]int{-1: 50, 0: 50, 1: 1, 100: 100, 101: 50}
	// input 是原始上限；want 是期望的收口结果。
	for input, want := range cases {
		// got 是实际收口后的上限。
		if got := clampEventLimit(input); got != want {
			t.Fatalf("clamp(%d)=%d want=%d", input, got, want)
		}
	}
}
