package db

import (
	"context"
	"testing"
)

// TestNotificationsAccountChannelsPaths 覆盖用户级通知渠道查询、静音过滤与缺失账号错误。
func TestNotificationsAccountChannelsPaths(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存测试账号所属用户和账号标识。
	userID, cookieID := seedAccount(t, store)
	// created、createErr 保存第二个用户的创建结果和数据库错误。
	created, createErr := store.Users.Create(ctx, "binding-other", "binding-other@example.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("create other user: created=%v err=%v", created, createErr)
	}
	// otherUser、otherUserErr 保存第二个用户对象及读取错误。
	otherUser, otherUserErr := store.Users.GetByUsername(ctx, "binding-other")
	if otherUserErr != nil {
		t.Fatal(otherUserErr)
	}
	// ownedChannel、ownedErr 保存当前用户启用通知渠道及创建错误。
	ownedChannel, ownedErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "owned", Type: "webhook", Config: `{}`, EventTypes: "message", Enabled: true, UserID: userID,
	})
	if ownedErr != nil {
		t.Fatal(ownedErr)
	}
	// disabledChannel、disabledErr 保存当前用户停用通知渠道及创建错误。
	disabledChannel, disabledErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "disabled", Type: "webhook", Config: `{}`, Enabled: false, UserID: userID,
	})
	if disabledErr != nil {
		t.Fatal(disabledErr)
	}
	// foreignChannel、foreignErr 保存其他用户通知渠道及创建错误。
	foreignChannel, foreignErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "foreign", Type: "webhook", Config: `{}`, Enabled: true, UserID: otherUser.ID,
	})
	if foreignErr != nil {
		t.Fatal(foreignErr)
	}
	// owns、ownsErr 保存渠道归属检查结果及数据库错误。
	owns, ownsErr := store.Notifications.OwnsChannel(ctx, ownedChannel, userID)
	if ownsErr != nil || !owns {
		t.Fatalf("owned channel check: owns=%v err=%v", owns, ownsErr)
	}
	// foreignOwns、foreignOwnsErr 保存跨用户渠道归属检查结果及数据库错误。
	foreignOwns, foreignOwnsErr := store.Notifications.OwnsChannel(ctx, foreignChannel, userID)
	if foreignOwnsErr != nil || foreignOwns {
		t.Fatalf("foreign channel check: owns=%v err=%v", foreignOwns, foreignOwnsErr)
	}
	// channels、channelsErr 保存账号应接收的启用渠道及查询错误；停用渠道与其他用户渠道都被排除。
	channels, channelsErr := store.Notifications.AccountChannels(ctx, cookieID)
	if channelsErr != nil || len(channels) != 1 || channels[0].ID != ownedChannel {
		t.Fatalf("channels=%#v disabled=%d err=%v", channels, disabledChannel, channelsErr)
	}
	// muteErr 保存账号级静音开关写入错误。
	if _, muteErr := store.Cookies.UpdateSettings(ctx, cookieID, AccountSettingsUpdate{UserID: userID, NotificationsDisabled: boolPtr(true)}); muteErr != nil {
		t.Fatal(muteErr)
	}
	// mutedChannels、mutedErr 保存静音后的账号渠道及查询错误。
	mutedChannels, mutedErr := store.Notifications.AccountChannels(ctx, cookieID)
	if mutedErr != nil || len(mutedChannels) != 0 {
		t.Fatalf("muted channels=%#v err=%v", mutedChannels, mutedErr)
	}
	// 未知账号必须返回未找到，避免调用方误判为空渠道。
	if _, missingErr := store.Notifications.AccountChannels(ctx, "missing-account"); missingErr != ErrNotFound {
		t.Fatalf("missing account err=%v", missingErr)
	}
}

// TestAnalyticsQueriesDelegatesToDatabase 验证分析查询边界透传单行和多行查询。
func TestAnalyticsQueriesDelegatesToDatabase(t *testing.T) {
	// store、cleanup 保存测试数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试数据库操作的生命周期。
	ctx := context.Background()
	// one、oneErr 保存单行查询结果及数据库错误。
	var one int
	// oneErr 保存单行分析查询的数据库错误。
	oneErr := store.Analytics.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	if oneErr != nil || one != 1 {
		t.Fatalf("single-row query: one=%d err=%v", one, oneErr)
	}
	// rows、rowsErr 保存多行查询结果及数据库错误。
	rows, rowsErr := store.Analytics.QueryContext(ctx, "SELECT 1 AS value UNION ALL SELECT 2 AS value")
	if rowsErr != nil {
		t.Fatal(rowsErr)
	}
	defer rows.Close()
	// values 保存分析查询返回的整数行。
	values := make([]int, 0, 2)
	for rows.Next() {
		// value 保存当前分析结果行的数值。
		var value int
		// scanErr 保存当前分析结果行的扫描错误。
		if scanErr := rows.Scan(&value); scanErr != nil {
			t.Fatal(scanErr)
		}
		values = append(values, value)
	}
	// rowsErrAfterIteration 保存遍历结束后的驱动错误。
	rowsErrAfterIteration := rows.Err()
	if rowsErrAfterIteration != nil || len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("multi-row query: values=%v err=%v", values, rowsErrAfterIteration)
	}
}
