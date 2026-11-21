package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// TestNotificationChannelRepositoryCRUD 覆盖通知渠道适配器的完整 CRUD 转换路径。
func TestNotificationChannelRepositoryCRUDAndBindings(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及用户查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 保存通知渠道数据库适配器。
	repository := NewNotificationChannelRepository(store)
	// channelID、createErr 保存渠道创建结果和数据库错误。
	channelID, createErr := repository.CreateChannel(ctx, owner.ID, notificationsapp.ChannelInput{Name: "测试渠道", Type: "webhook", Config: `{}`, EventTypes: "order", Enabled: true})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// owned、ownedErr 保存渠道归属检查结果和数据库错误。
	owned, ownedErr := repository.OwnsChannel(ctx, channelID, owner.ID)
	if ownedErr != nil || !owned {
		t.Fatalf("owned=%v err=%v", owned, ownedErr)
	}
	// summaries、listErr 保存渠道摘要列表和数据库错误。
	summaries, listErr := repository.ListChannels(ctx, owner.ID)
	if listErr != nil || len(summaries) != 1 || summaries[0].ID != channelID {
		t.Fatalf("summaries=%#v err=%v", summaries, listErr)
	}
	// record、getErr 保存渠道完整更新记录和数据库错误。
	record, getErr := repository.GetChannelForUpdate(ctx, channelID, owner.ID)
	if getErr != nil || record == nil || record.Config != `{}` {
		t.Fatalf("record=%#v err=%v", record, getErr)
	}
	// updateErr 保存渠道更新结果。
	if updateErr := repository.UpdateChannel(ctx, owner.ID, notificationsapp.ChannelRecord{ID: channelID, Name: "更新渠道", Type: "webhook", Config: `{"updated":true}`, EventTypes: "message", Enabled: false}); updateErr != nil {
		t.Fatal(updateErr)
	}
	// deleteErr 保存渠道删除结果。
	if deleteErr := repository.DeleteChannel(ctx, channelID, owner.ID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
}

// TestNotificationChannelRepositoryErrorMapping 验证渠道适配器对资源错误与缺失记录的归一化。
func TestNotificationChannelRepositoryErrorMapping(t *testing.T) {
	// store、cleanup 是隔离的渠道适配器测试仓储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试数据库操作的生命周期。
	ctx := context.Background()
	// owner 是测试用户；ownerErr 是查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil || owner == nil {
		t.Fatalf("读取测试用户失败: err=%v", ownerErr)
	}
	// repository 是通知渠道数据库适配器。
	repository := NewNotificationChannelRepository(store)
	// missingRecord、missingErr 验证不存在的渠道返回空指针而非错误。
	missingRecord, missingErr := repository.GetChannelForUpdate(ctx, 999999, owner.ID)
	if missingErr != nil || missingRecord != nil {
		t.Fatalf("缺失渠道异常: record=%v err=%v", missingRecord, missingErr)
	}
	// nilRecord、nilRecordErr 验证无记录时返回空指针而非错误。
	nilRecord, nilRecordErr := repository.GetChannelForUpdate(ctx, 999999, 0)
	if nilRecordErr != nil || nilRecord != nil {
		t.Fatalf("无归属记录异常: record=%v err=%v", nilRecord, nilRecordErr)
	}
	// deleteErr 验证删除不存在的渠道返回未找到。
	if deleteErr := repository.DeleteChannel(ctx, 999999, owner.ID); !errors.Is(deleteErr, notificationsapp.ErrChannelNotFound) {
		t.Fatalf("删除缺失渠道错误异常: %v", deleteErr)
	}
	// nilStore 验证依赖不完整时构造函数返回 nil。
	if nilRepo := NewNotificationChannelRepository(nil); nilRepo != nil {
		t.Fatal("空存储应返回 nil 适配器")
	}
	// closeErr 表示关闭测试数据库失败。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// listErr 验证关闭数据库后的列表读取错误。
	if _, listErr := repository.ListChannels(ctx, owner.ID); listErr == nil {
		t.Fatal("关闭数据库后列表读取应失败")
	}
}

// TestNotificationUncertainRepositoryMapsSummaries 覆盖不确定通知摘要的用户/管理员查询和模型转换路径。
func TestNotificationUncertainRepositoryMapsSummaries(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// channelID、channelErr 保存通知渠道及创建错误。
	channelID, channelErr := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{Name: "不确定渠道", Type: "webhook", Config: `{}`, UserID: owner.ID, Enabled: true})
	if channelErr != nil {
		t.Fatal(channelErr)
	}
	// enqueueErr 保存通知出站记录写入错误。
	if enqueueErr := store.Notifications.EnqueueOutbox(ctx, []db.NotificationOutboxInput{{ChannelID: channelID, EventType: "test", Body: "内部正文"}}); enqueueErr != nil {
		t.Fatal(enqueueErr)
	}
	// claimed、claimErr 保存通知租约领取结果和数据库错误。
	claimed, claimErr := store.Notifications.ClaimOutbox(ctx, "uncertain-adapter-worker", time.Now(), 1)
	if claimErr != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, claimErr)
	}
	// uncertain、uncertainErr 保存通知进入不确定状态的结果和数据库错误。
	uncertain, uncertainErr := store.Notifications.MarkOutboxUncertain(ctx, claimed[0].ID, "uncertain-adapter-worker", "确认失败")
	if uncertainErr != nil || !uncertain {
		t.Fatalf("uncertain=%v err=%v", uncertain, uncertainErr)
	}
	// repository 保存不确定通知摘要适配器。
	repository := NewNotificationUncertainRepository(store)
	// userSummaries、userListErr 保存用户范围摘要及查询错误。
	userSummaries, userListErr := repository.ListUncertainForUser(ctx, owner.ID, 0)
	if userListErr != nil || len(userSummaries) != 1 || userSummaries[0].ChannelID != channelID || !userSummaries[0].HasError {
		t.Fatalf("user summaries=%#v err=%v", userSummaries, userListErr)
	}
	// userCount、userCountErr 保存用户范围不确定通知数量及数据库错误。
	userCount, userCountErr := repository.CountUncertainForUser(ctx, owner.ID)
	if userCountErr != nil || userCount != 1 {
		t.Fatalf("user count=%d err=%v", userCount, userCountErr)
	}
	// adminSummaries、adminListErr 保存管理员范围摘要及查询错误。
	adminSummaries, adminListErr := repository.ListUncertainForAdmin(ctx, 0)
	if adminListErr != nil || len(adminSummaries) != 1 || adminSummaries[0].OwnerUserID != owner.ID {
		t.Fatalf("admin summaries=%#v err=%v", adminSummaries, adminListErr)
	}
	// adminCount、adminCountErr 保存管理员范围不确定通知数量及数据库错误。
	adminCount, adminCountErr := repository.CountUncertainForAdmin(ctx)
	if adminCountErr != nil || adminCount != 1 {
		t.Fatalf("admin count=%d err=%v", adminCount, adminCountErr)
	}
}
