package db

import (
	"context"
	"testing"
)

// TestOrderListPrefersItemTitleSnapshot 验证订单商品名的取值顺序：优先订单自带快照，缺失时回退商品表。
// 商品同步只填充商品表，凡是商品同步没抓到过的商品（下架、删除、分页未覆盖）在表里永远没有记录，
// 只有订单自带的快照能让这些订单显示商品名；商品表已有的商品则继续沿用旧行为。
func TestOrderListPrefersItemTitleSnapshot(t *testing.T) {
	// s、cleanup 是本次测试独占的数据库及其清理函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本次测试数据库调用的生命周期。
	ctx := context.Background()
	// uid、cid 是测试账号的用户标识与账号标识。
	uid, cid := seedAccount(t, s)

	// upsertErr 保存「商品表无记录但订单带快照」的写入错误。
	if upsertErr := s.Orders.Upsert(ctx, "snapshot-order", OrderUpsertOpts{
		CookieID: cid, ItemID: "gone-item", ItemTitle: "快照商品", OrderStatus: "pending_ship",
	}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// itemErr 保存商品表记录的写入错误，用于验证无快照订单仍能回退到商品表。
	if itemErr := s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cid, ItemID: "live-item", ItemTitle: "在售商品"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// legacyErr 保存无快照订单的写入错误。
	if legacyErr := s.Orders.Upsert(ctx, "legacy-order", OrderUpsertOpts{
		CookieID: cid, ItemID: "live-item", OrderStatus: "pending_ship",
	}); legacyErr != nil {
		t.Fatal(legacyErr)
	}

	// rows、total、listErr 保存订单列表查询结果、总数与查询错误。
	rows, total, listErr := s.Orders.ListForUser(ctx, OrderListFilter{UserID: uid, Limit: 10})
	if listErr != nil || total != 2 {
		t.Fatalf("订单列表查询异常 total=%d err=%v", total, listErr)
	}
	// titles 按商品标识收集订单展示的商品名。
	titles := make(map[string]string, len(rows))
	// row 是当前订单行。
	for _, row := range rows {
		titles[row.ItemID] = row.ItemTitle
	}
	if titles["gone-item"] != "快照商品" {
		t.Fatalf("商品表缺失时必须使用订单快照 got=%q", titles["gone-item"])
	}
	if titles["live-item"] != "在售商品" {
		t.Fatalf("订单无快照时必须回退商品表 got=%q", titles["live-item"])
	}

	// searched、searchTotal、searchErr 保存按快照标题搜索的结果。
	searched, searchTotal, searchErr := s.Orders.ListForUser(ctx, OrderListFilter{UserID: uid, Search: "快照商品", Limit: 10})
	if searchErr != nil || searchTotal != 1 || len(searched) != 1 || searched[0].OrderID != "snapshot-order" {
		t.Fatalf("搜索必须命中订单快照标题 total=%d rows=%+v err=%v", searchTotal, searched, searchErr)
	}
}

// TestOrderItemTitleSnapshotIsPersistedOnInsert 验证新订单首次插入就会带上商品标题快照。
func TestOrderItemTitleSnapshotIsPersistedOnInsert(t *testing.T) {
	// s、cleanup 是本次测试独占的数据库及其清理函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本次测试数据库调用的生命周期。
	ctx := context.Background()
	// uid、cid 是测试账号的用户标识与账号标识。
	uid, cid := seedAccount(t, s)
	// upsertErr 保存带标题快照的订单写入错误。
	if upsertErr := s.Orders.Upsert(ctx, "fresh-order", OrderUpsertOpts{
		CookieID: cid, ItemID: "fresh-item", ItemTitle: "全新商品",
	}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// rows、total、listErr 保存订单列表查询结果。
	rows, total, listErr := s.Orders.ListForUser(ctx, OrderListFilter{UserID: uid, Limit: 10})
	if listErr != nil || total != 1 {
		t.Fatalf("订单列表查询异常 total=%d err=%v", total, listErr)
	}
	if rows[0].ItemTitle != "全新商品" {
		t.Fatalf("新订单必须落库商品标题快照 got=%q", rows[0].ItemTitle)
	}
}

// TestItemsCountActiveIgnoresSoftDeleted 验证在售商品计数只统计未被标记删除的商品。
func TestItemsCountActiveIgnoresSoftDeleted(t *testing.T) {
	// s、cleanup 是本次测试独占的数据库及其清理函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本次测试数据库调用的生命周期。
	ctx := context.Background()
	// cid 是测试账号标识。
	_, cid := seedAccount(t, s)
	// upsertErr 保存两条商品的写入错误。
	if upsertErr := s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cid, ItemID: "live", ItemTitle: "在售"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// upsertErr 保存随后会被逻辑删除的商品写入错误。
	if upsertErr := s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cid, ItemID: "gone", ItemTitle: "已删"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// deleteErr 保存逻辑删除结果。
	if deleteErr := s.Items.Delete(ctx, cid, "gone"); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// active、countErr 保存在售商品数量与查询错误。
	active, countErr := s.Items.CountActive(ctx, cid)
	if countErr != nil || active != 1 {
		t.Fatalf("在售商品计数应排除软删除商品 active=%d err=%v", active, countErr)
	}
}
