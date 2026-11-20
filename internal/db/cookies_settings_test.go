package db

import (
	"context"
	"errors"
	"testing"
)

// TestUpdateAccountSettingsIsAtomic 验证账号设置更新在越权与非法输入时不留下部分写入，成功时一次性生效。
func TestUpdateAccountSettingsIsAtomic(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// user 表示当前遍历过程中的用户
	for _, user := range []struct{ name, email string }{{"settings-owner", "settings-owner@example.com"}, {"settings-other", "settings-other@example.com"}} {
		if // ok、err 用于本次流程后续判断的ok、err
		ok, err := store.Users.Create(ctx, user.name, user.email, "pw"); err != nil || !ok {
			t.Fatalf("create %s: ok=%v err=%v", user.name, ok, err)
		}
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "settings-owner")
	// other 用于本次流程后续判断的other
	other, _ := store.Users.GetByUsername(ctx, "settings-other")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "settings-cookie", "old-cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	// newCookie、remark 用于本次流程后续判断的newCookie、remark
	newCookie, remark := "new-cookie", "new remark"
	// autoConfirm 用于本次流程后续判断的autoConfirm
	autoConfirm := false
	// 越权用户更新必须整条拒绝且不写入任何字段。
	if _, err := store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: other.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden account, got %v", err)
	}
	// detail 用于本次流程后续判断的detail
	detail, _ := store.Cookies.GetDetails(ctx, "settings-cookie")
	if detail.Value != "old-cookie" || detail.Remark != "" || !detail.AutoConfirm {
		t.Fatalf("failed aggregate update partially committed: %+v", detail)
	}

	// pause 用于本次流程后续判断的pause
	pause := 5
	if // err 用于本次流程后续判断的err
	_, err := store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, PauseDuration: &pause,
	}); err != nil {
		t.Fatal(err)
	}
	detail, _ = store.Cookies.GetDetails(ctx, "settings-cookie")
	if detail.Value != newCookie || detail.Remark != remark || detail.AutoConfirm || detail.PauseDuration != pause || detail.PausedUntil == 0 {
		t.Fatalf("aggregate settings not applied: %+v", detail)
	}
}
