package adapter

import (
	"context"
	"testing"

	analyticsapp "xianyu-go/internal/application/analytics"
	"xianyu-go/internal/db"
)

// TestAnalyticsQueryItemPrefersOrderTitleSnapshot 验证商品排行会带上订单自带的商品标题。
// 商品排行此前只按 item_id 聚合，前端再拿商品表补名，商品从商品表消失后排行只剩编号；
// 现在排行直接读取订单快照，商品表缺失也能显示商品名，商品表已有记录时继续回退取用。
func TestAnalyticsQueryItemPrefersOrderTitleSnapshot(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// snapshotErr 保存商品表无记录但订单带快照的写入错误。
	if snapshotErr := store.Orders.Upsert(ctx, "snapshot-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "gone-item", ItemTitle: "已删除商品", OrderStatus: "paid", Amount: "10.00", CreatedAt: "2024-01-02 10:00:00",
	}); snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	// itemErr 保存商品表记录的写入错误，用于验证无快照订单仍能回退到商品表。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "live-item", ItemTitle: "在售商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// legacyErr 保存无快照订单的写入错误。
	if legacyErr := store.Orders.Upsert(ctx, "legacy-order", db.OrderUpsertOpts{
		CookieID: "cid", ItemID: "live-item", OrderStatus: "paid", Amount: "20.00", CreatedAt: "2024-01-03 10:00:00",
	}); legacyErr != nil {
		t.Fatal(legacyErr)
	}
	// repository 使用真实仓储执行商品聚合查询。
	repository := NewAnalyticsRepository(store)
	// records、queryErr 保存商品排行聚合结果及查询错误。
	records, queryErr := repository.QueryItem(ctx, analyticsapp.Filter{})
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	if len(records) != 2 {
		t.Fatalf("商品排行应包含两个商品 records=%+v", records)
	}
	// titles 按商品标识收集排行里的商品名。
	titles := make(map[string]string, len(records))
	// record 是当前商品的聚合记录。
	for _, record := range records {
		titles[record.ItemID] = record.ItemTitle
	}
	if titles["gone-item"] != "已删除商品" {
		t.Fatalf("商品表缺失时排行必须使用订单快照 got=%q", titles["gone-item"])
	}
	if titles["live-item"] != "在售商品" {
		t.Fatalf("订单无快照时排行必须回退商品表 got=%q", titles["live-item"])
	}
}
