package adapter

import (
	"context"
	"errors"
	"testing"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// TestNotificationEventRepositoryPaths 验证站内事件适配器的租户与系统路径。
func TestNotificationEventRepositoryPaths(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// admin、adminErr 保存测试管理员身份。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试用户失败: %v", adminErr)
	}
	// repository 是站内事件数据库适配器。
	repository := NewNotificationEventRepository(store)
	if repository == nil {
		t.Fatal("站内事件适配器不应为空")
	}
	// 写入一条租户事件与一条系统事件。
	// tenantRecordErr 表示写入租户事件失败。
	if tenantRecordErr := store.Notifications.RecordEvent(ctx, db.NotificationEventInput{
		Scope: db.NotificationScopeTenant, OwnerUserID: admin.ID, AccountID: "cid",
		EventType: "account_offline", Level: "warn", Title: "掉线", Body: "正文",
	}); tenantRecordErr != nil {
		t.Fatal(tenantRecordErr)
	}
	// systemRecordErr 表示写入系统事件失败。
	if systemRecordErr := store.Notifications.RecordEvent(ctx, db.NotificationEventInput{
		Scope: db.NotificationScopeSystem, EventType: "business_silence", Level: "warn", Title: "静默", Body: "正文",
	}); systemRecordErr != nil {
		t.Fatal(systemRecordErr)
	}
	// items、listErr 保存租户事件列表与错误。
	items, listErr := repository.ListForUser(ctx, admin.ID, 20)
	if listErr != nil || len(items) != 1 || items[0].Scope != db.NotificationScopeTenant || items[0].AccountID != "cid" {
		t.Fatalf("租户事件=%+v err=%v", items, listErr)
	}
	// unread、unreadErr 保存租户未读数。
	unread, unreadErr := repository.CountUnreadForUser(ctx, admin.ID)
	if unreadErr != nil || unread != 1 {
		t.Fatalf("租户未读=%d err=%v", unread, unreadErr)
	}
	// marked、markErr 保存租户单条已读结果。
	marked, markErr := repository.MarkReadForUser(ctx, admin.ID, items[0].ID)
	if markErr != nil || !marked {
		t.Fatalf("租户标记=%v err=%v", marked, markErr)
	}
	// systemItems、systemErr 保存系统事件列表。
	systemItems, systemErr := repository.ListForAdmin(ctx, 20)
	if systemErr != nil || len(systemItems) != 1 || systemItems[0].Scope != db.NotificationScopeSystem {
		t.Fatalf("系统事件=%+v err=%v", systemItems, systemErr)
	}
	// systemUnread、systemUnreadErr 保存系统未读数。
	systemUnread, systemUnreadErr := repository.CountUnreadForAdmin(ctx)
	if systemUnreadErr != nil || systemUnread != 1 {
		t.Fatalf("系统未读=%d err=%v", systemUnread, systemUnreadErr)
	}
	// systemMarked、systemMarkErr 保存系统单条已读结果。
	systemMarked, systemMarkErr := repository.MarkReadForAdmin(ctx, systemItems[0].ID)
	if systemMarkErr != nil || !systemMarked {
		t.Fatalf("系统标记=%v err=%v", systemMarked, systemMarkErr)
	}
	// all、allErr 保存租户全部已读结果；已无未读时应为 0。
	all, allErr := repository.MarkAllReadForUser(ctx, admin.ID)
	if allErr != nil || all != 0 {
		t.Fatalf("租户全部已读=%d err=%v", all, allErr)
	}
	// allAdmin、allAdminErr 保存系统全部已读结果。
	allAdmin, allAdminErr := repository.MarkAllReadForAdmin(ctx)
	if allAdminErr != nil || allAdmin != 0 {
		t.Fatalf("系统全部已读=%d err=%v", allAdmin, allAdminErr)
	}
	if NewNotificationEventRepository(nil) != nil {
		t.Fatal("nil store 应返回 nil 站内事件适配器")
	}
}

// TestSystemAlertChannelRepositoryPaths 验证系统告警渠道适配器的 CRUD 与错误归一。
func TestSystemAlertChannelRepositoryPaths(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// admin、adminErr 保存测试管理员身份。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil || admin == nil {
		t.Fatalf("读取测试用户失败: %v", adminErr)
	}
	// repository 是系统告警渠道数据库适配器。
	repository := NewSystemAlertChannelRepository(store)
	if repository == nil {
		t.Fatal("系统告警渠道适配器不应为空")
	}
	// channelID、createErr 保存系统渠道创建结果。
	channelID, createErr := repository.CreateSystemChannel(ctx, admin.ID, notificationsapp.ChannelInput{
		Name: "系统渠道", Type: "webhook", Config: `{"webhook_url":"https://system.example.com"}`, EventTypes: "business_silence", Enabled: true,
	})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// summaries、listErr 保存系统渠道摘要。
	summaries, listErr := repository.ListSystemChannels(ctx)
	if listErr != nil || len(summaries) != 1 || summaries[0].ID != channelID {
		t.Fatalf("系统渠道=%+v err=%v", summaries, listErr)
	}
	// record、recordErr 保存系统渠道完整记录；配置必须解密成功。
	record, recordErr := repository.GetSystemChannel(ctx, channelID)
	if recordErr != nil || record == nil || record.Config != `{"webhook_url":"https://system.example.com"}` {
		t.Fatalf("系统渠道记录=%+v err=%v", record, recordErr)
	}
	// updateErr 表示系统渠道更新结果。
	if updateErr := repository.UpdateSystemChannel(ctx, admin.ID, notificationsapp.ChannelRecord{
		ID: channelID, Name: "系统渠道", Type: "webhook", Config: `{"webhook_url":"https://system2.example.com"}`, EventTypes: "business_silence", Enabled: true,
	}); updateErr != nil {
		t.Fatal(updateErr)
	}
	// updated、updatedErr 保存更新后的系统渠道记录。
	updated, updatedErr := repository.GetSystemChannel(ctx, channelID)
	if updatedErr != nil || updated == nil || updated.Config != `{"webhook_url":"https://system2.example.com"}` {
		t.Fatalf("更新后系统渠道=%+v err=%v", updated, updatedErr)
	}
	// deleteErr 表示系统渠道删除结果。
	if deleteErr := repository.DeleteSystemChannel(ctx, channelID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// missingDeleteErr 验证重复删除映射为应用层未找到错误。
	if missingDeleteErr := repository.DeleteSystemChannel(ctx, channelID); !errors.Is(missingDeleteErr, notificationsapp.ErrChannelNotFound) {
		t.Fatalf("重复删除 err=%v", missingDeleteErr)
	}
	// missingRecord、missingRecordErr 验证不存在渠道返回 nil。
	missingRecord, missingRecordErr := repository.GetSystemChannel(ctx, channelID)
	if missingRecordErr != nil || missingRecord != nil {
		t.Fatalf("不存在渠道=%+v err=%v", missingRecord, missingRecordErr)
	}
	if NewSystemAlertChannelRepository(nil) != nil {
		t.Fatal("nil store 应返回 nil 系统告警渠道适配器")
	}
}
