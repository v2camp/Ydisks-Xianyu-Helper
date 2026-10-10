// card_delivery_channels_test.go 锁定卡券发货渠道的落库与批量读取：渠道由应用层在贴入卡密时
// 提取并写入，这里保证 DB 层如实存储、跨卡券去重读取，且空值与未知标识不干扰结果。

package db

import (
	"context"
	"testing"
)

// TestCardsDeliveryChannelsByIDs 验证渠道批量读取：跨卡券去重、跳过无渠道卡券与未知标识。
func TestCardsDeliveryChannelsByIDs(t *testing.T) {
	// store、cleanup 是隔离的 SQLite 测试库与关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// userID 是卡券所属用户主键。
	var userID int64
	// err 表示创建测试用户时的数据库错误。
	if err := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"card-ch-owner", "card-ch-owner@example.com", "test-hash").Scan(&userID); err != nil {
		t.Fatalf("创建测试用户: %v", err)
	}
	// dualID 是双盘卡券标识，其渠道与单盘卡券有交集，用于验证跨卡券去重。
	dualID, err := store.Cards.Create(ctx, &CardFull{
		UserID: userID, Name: "双盘组", Type: "data", DataContent: "x", Enabled: true,
		DeliveryChannels: "百度网盘,夸克网盘",
	})
	if err != nil {
		t.Fatalf("创建双盘卡组: %v", err)
	}
	// quarkID 是仅含夸克渠道的卡券标识。
	quarkID, err := store.Cards.Create(ctx, &CardFull{
		UserID: userID, Name: "夸克组", Type: "data", DataContent: "y", Enabled: true,
		DeliveryChannels: "夸克网盘",
	})
	if err != nil {
		t.Fatalf("创建夸克卡组: %v", err)
	}
	// plainID 是没有渠道的卡券标识，读取时应被跳过。
	plainID, err := store.Cards.Create(ctx, &CardFull{
		UserID: userID, Name: "无渠道组", Type: "text", TextContent: "hello", Enabled: true,
	})
	if err != nil {
		t.Fatalf("创建无渠道卡组: %v", err)
	}
	// channels、readErr 是跨卡券读取的渠道结果与错误；999999 为不存在的标识。
	channels, readErr := store.Cards.DeliveryChannelsByIDs(ctx, []int64{dualID, plainID, quarkID, 999999})
	if readErr != nil {
		t.Fatalf("DeliveryChannelsByIDs: %v", readErr)
	}
	if len(channels) != 2 || channels[0] != "百度网盘" || channels[1] != "夸克网盘" {
		t.Fatalf("渠道读取去重失败: %v", channels)
	}
	// empty、emptyErr 是空入参的读取结果与错误，应为空且无错。
	empty, emptyErr := store.Cards.DeliveryChannelsByIDs(ctx, nil)
	if emptyErr != nil || empty != nil {
		t.Fatalf("空入参应返回空: channels=%v err=%v", empty, emptyErr)
	}
}
