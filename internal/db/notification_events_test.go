package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNotificationEventsTenantAndSystemIsolation 验证租户事件与系统事件的归属、未读与已读隔离。
func TestNotificationEventsTenantAndSystemIsolation(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存测试账号所属用户和账号标识。
	userID, cookieID := seedAccount(t, store)
	// owner、ownerErr 保存账号所有者查询结果与错误。
	owner, ownerErr := store.Notifications.OwnerOfAccount(ctx, cookieID)
	if ownerErr != nil || owner != userID {
		t.Fatalf("owner=%d err=%v", owner, ownerErr)
	}
	// missingOwner、missingErr 验证未知账号返回统一未找到错误。
	missingOwner, missingErr := store.Notifications.OwnerOfAccount(ctx, "missing-account")
	if missingOwner != 0 || !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing owner=%d err=%v", missingOwner, missingErr)
	}
	// tenantEvent 是待写入的租户事件。
	tenantEvent := NotificationEventInput{
		Scope: NotificationScopeTenant, OwnerUserID: userID, AccountID: cookieID,
		EventType: "account_offline", Level: "warn", Title: "账号掉线", Body: "账号已掉线",
		DedupeKey: "tenant-dedupe-1", CreatedAt: time.Now(),
	}
	// recordErr 表示写入租户事件失败。
	if recordErr := store.Notifications.RecordEvent(ctx, tenantEvent); recordErr != nil {
		t.Fatal(recordErr)
	}
	// duplicateErr 表示重复业务键写入结果；唯一索引应静默忽略。
	if duplicateErr := store.Notifications.RecordEvent(ctx, tenantEvent); duplicateErr != nil {
		t.Fatal(duplicateErr)
	}
	// systemEvent 是待写入的系统级事件，owner 必须为 0。
	systemEvent := NotificationEventInput{
		Scope: NotificationScopeSystem, EventType: "business_silence", Level: "warn",
		Title: "业务静默", Body: "长时间没有业务事件", CreatedAt: time.Now(),
	}
	// recordSystemErr 表示写入系统事件失败。
	if recordSystemErr := store.Notifications.RecordEvent(ctx, systemEvent); recordSystemErr != nil {
		t.Fatal(recordSystemErr)
	}
	// tenantEvents、tenantListErr 保存租户视图事件与读取错误；重复业务键只保留一条。
	tenantEvents, tenantListErr := store.Notifications.ListEventsForUser(ctx, userID, 50)
	if tenantListErr != nil || len(tenantEvents) != 1 || tenantEvents[0].Scope != NotificationScopeTenant || tenantEvents[0].Read {
		t.Fatalf("tenant events=%+v err=%v", tenantEvents, tenantListErr)
	}
	// systemEvents、systemListErr 保存系统视图事件与读取错误；不得混入租户事件。
	systemEvents, systemListErr := store.Notifications.ListSystemEvents(ctx, 50)
	if systemListErr != nil || len(systemEvents) != 1 || systemEvents[0].Scope != NotificationScopeSystem || systemEvents[0].AccountID != "" {
		t.Fatalf("system events=%+v err=%v", systemEvents, systemListErr)
	}
	// tenantUnread、tenantUnreadErr 保存租户未读数与统计错误。
	tenantUnread, tenantUnreadErr := store.Notifications.CountUnreadEventsForUser(ctx, userID)
	if tenantUnreadErr != nil || tenantUnread != 1 {
		t.Fatalf("tenant unread=%d err=%v", tenantUnread, tenantUnreadErr)
	}
	// systemUnread、systemUnreadErr 保存系统未读数与统计错误。
	systemUnread, systemUnreadErr := store.Notifications.CountUnreadSystemEvents(ctx)
	if systemUnreadErr != nil || systemUnread != 1 {
		t.Fatalf("system unread=%d err=%v", systemUnread, systemUnreadErr)
	}
	// foreignRead、foreignReadErr 验证其他用户不能标记该租户事件。
	foreignRead, foreignReadErr := store.Notifications.MarkEventReadForUser(ctx, userID+1, tenantEvents[0].ID)
	if foreignReadErr != nil || foreignRead {
		t.Fatalf("foreign read=%v err=%v", foreignRead, foreignReadErr)
	}
	// tenantRead、tenantReadErr 验证所有者可以标记事件已读。
	tenantRead, tenantReadErr := store.Notifications.MarkEventReadForUser(ctx, userID, tenantEvents[0].ID)
	if tenantReadErr != nil || !tenantRead {
		t.Fatalf("tenant read=%v err=%v", tenantRead, tenantReadErr)
	}
	// systemRead、systemReadErr 验证系统事件只能通过系统视图标记。
	systemRead, systemReadErr := store.Notifications.MarkSystemEventRead(ctx, systemEvents[0].ID)
	if systemReadErr != nil || !systemRead {
		t.Fatalf("system read=%v err=%v", systemRead, systemReadErr)
	}
	// tenantUnreadAfter、tenantUnreadAfterErr 保存标记后的租户未读数。
	tenantUnreadAfter, tenantUnreadAfterErr := store.Notifications.CountUnreadEventsForUser(ctx, userID)
	if tenantUnreadAfterErr != nil || tenantUnreadAfter != 0 {
		t.Fatalf("tenant unread after=%d err=%v", tenantUnreadAfter, tenantUnreadAfterErr)
	}
	// 再次写入一条未读事件，验证全部已读只影响当前用户。
	if recordAgainErr := store.Notifications.RecordEvent(ctx, NotificationEventInput{Scope: NotificationScopeTenant, OwnerUserID: userID, AccountID: cookieID, EventType: "token_renewal", Title: "续期", Body: "续期完成"}); recordAgainErr != nil {
		t.Fatal(recordAgainErr)
	}
	// allRead、allReadErr 保存批量已读影响行数与错误。
	allRead, allReadErr := store.Notifications.MarkAllEventsReadForUser(ctx, userID)
	if allReadErr != nil || allRead != 1 {
		t.Fatalf("all read=%d err=%v", allRead, allReadErr)
	}
	// 系统事件已读后再次批量标记应影响 0 条。
	allSystemRead, allSystemReadErr := store.Notifications.MarkAllSystemEventsRead(ctx)
	if allSystemReadErr != nil || allSystemRead != 0 {
		t.Fatalf("all system read=%d err=%v", allSystemRead, allSystemReadErr)
	}
}

// TestSystemChannelsScopeIsolation 验证系统告警渠道与租户渠道在查询、归属和加解密上完全隔离。
func TestSystemChannelsScopeIsolation(t *testing.T) {
	// store、cleanup 保存带完整迁移的临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID、cookieID 保存测试账号所属用户和账号标识。
	userID, cookieID := seedAccount(t, store)
	// tenantChannel、tenantErr 保存租户渠道及创建错误。
	tenantChannel, tenantErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "租户渠道", Type: "webhook", Config: `{"url":"https://tenant.example.com"}`, Enabled: true, UserID: userID,
	})
	if tenantErr != nil {
		t.Fatal(tenantErr)
	}
	// systemChannel、systemErr 保存系统渠道及创建错误；创建者同为一个管理员。
	systemChannel, systemErr := store.Notifications.CreateSystemChannel(ctx, userID, &NotificationChannelRow{
		Name: "系统告警渠道", Type: "webhook", Config: `{"url":"https://system.example.com"}`, Enabled: true,
	})
	if systemErr != nil {
		t.Fatal(systemErr)
	}
	// disabledSystem、disabledSystemErr 保存停用的系统渠道及创建错误。
	disabledSystem, disabledSystemErr := store.Notifications.CreateSystemChannel(ctx, userID, &NotificationChannelRow{
		Name: "停用系统渠道", Type: "webhook", Config: `{}`, Enabled: false,
	})
	if disabledSystemErr != nil {
		t.Fatal(disabledSystemErr)
	}
	// accountChannels、accountChannelsErr 保存账号应接收的渠道；系统渠道不得混入租户投递。
	accountChannels, accountChannelsErr := store.Notifications.AccountChannels(ctx, cookieID)
	if accountChannelsErr != nil || len(accountChannels) != 1 || accountChannels[0].ID != tenantChannel {
		t.Fatalf("account channels=%+v system=%d err=%v", accountChannels, systemChannel, accountChannelsErr)
	}
	// summaries、summariesErr 保存租户渠道摘要；系统渠道不得出现在租户列表。
	summaries, summariesErr := store.Notifications.ListChannelSummariesForUser(ctx, userID)
	if summariesErr != nil || len(summaries) != 1 || summaries[0].ID != tenantChannel {
		t.Fatalf("tenant summaries=%+v err=%v", summaries, summariesErr)
	}
	// systemSummaries、systemSummariesErr 保存系统渠道摘要；停用渠道仍应出现在管理列表。
	systemSummaries, systemSummariesErr := store.Notifications.ListSystemChannelSummaries(ctx)
	if systemSummariesErr != nil || len(systemSummaries) != 2 {
		t.Fatalf("system summaries=%+v disabled=%d err=%v", systemSummaries, disabledSystem, systemSummariesErr)
	}
	// systemRow、systemRowErr 保存系统渠道完整记录；配置必须用 system 归属解密成功。
	systemRow, systemRowErr := store.Notifications.GetSystemChannelRow(ctx, systemChannel)
	if systemRowErr != nil || systemRow == nil || systemRow.Config != `{"url":"https://system.example.com"}` {
		t.Fatalf("system row=%+v err=%v", systemRow, systemRowErr)
	}
	// foreignRow、foreignRowErr 验证租户归属查询看不到系统渠道。
	foreignRow, foreignRowErr := store.Notifications.GetChannelRowForUser(ctx, systemChannel, userID)
	if foreignRowErr != nil || foreignRow != nil {
		t.Fatalf("tenant lookup system channel=%+v err=%v", foreignRow, foreignRowErr)
	}
	// owns、ownsErr 验证租户归属判定拒绝系统渠道。
	owns, ownsErr := store.Notifications.OwnsChannel(ctx, systemChannel, userID)
	if ownsErr != nil || owns {
		t.Fatalf("tenant owns system channel=%v err=%v", owns, ownsErr)
	}
	// enabledSystem、enabledSystemErr 保存启用系统渠道，供通知器投递。
	enabledSystem, enabledSystemErr := store.Notifications.ListSystemAlertChannels(ctx)
	if enabledSystemErr != nil || len(enabledSystem) != 1 || enabledSystem[0].ID != systemChannel || enabledSystem[0].Scope != NotificationScopeSystem {
		t.Fatalf("system alert channels=%+v disabled=%d err=%v", enabledSystem, disabledSystem, enabledSystemErr)
	}
	// direct、directErr 验证按显式 ID 读取时系统渠道配置也能正确解密。
	direct, directErr := store.Notifications.GetChannel(ctx, systemChannel)
	if directErr != nil || direct == nil || direct.Scope != NotificationScopeSystem || direct.Config != `{"url":"https://system.example.com"}` {
		t.Fatalf("direct channel=%+v err=%v", direct, directErr)
	}
	// updateErr 表示系统渠道更新失败；更新后仍应保留 system 加密归属。
	updateErr := store.Notifications.UpdateSystemChannel(ctx, &NotificationChannelRow{
		ID: systemChannel, Name: "系统告警渠道", Type: "webhook", Config: `{"url":"https://system2.example.com"}`, EventTypes: "business_silence", Enabled: true,
	})
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// updatedRow、updatedRowErr 保存更新后的系统渠道记录。
	updatedRow, updatedRowErr := store.Notifications.GetSystemChannelRow(ctx, systemChannel)
	if updatedRowErr != nil || updatedRow == nil || updatedRow.EventTypes != "business_silence" || updatedRow.Config != `{"url":"https://system2.example.com"}` {
		t.Fatalf("updated row=%+v err=%v", updatedRow, updatedRowErr)
	}
	// tenantUpdateErr 验证租户更新入口不能修改系统渠道。
	if tenantUpdateErr := store.Notifications.UpdateChannelForUser(ctx, &NotificationChannelRow{ID: systemChannel, Name: "越权", Type: "webhook", Config: `{}`}, userID); !errors.Is(tenantUpdateErr, ErrNotFound) {
		t.Fatalf("tenant update system channel err=%v", tenantUpdateErr)
	}
	// deleteErr 表示删除系统渠道失败。
	if deleteErr := store.Notifications.DeleteSystemChannel(ctx, systemChannel); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// missingDeleteErr 验证重复删除返回统一未找到错误。
	if missingDeleteErr := store.Notifications.DeleteSystemChannel(ctx, systemChannel); !errors.Is(missingDeleteErr, ErrNotFound) {
		t.Fatalf("missing delete err=%v", missingDeleteErr)
	}
}
