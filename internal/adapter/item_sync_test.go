package adapter

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestItemSyncRepositoryUsesListMultiSpecMarkers 验证商品同步只使用列表模型的多规格标记。
func TestItemSyncRepositoryUsesListMultiSpecMarkers(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// logs 收集商品同步日志，用于验证日志明确说明多规格判断来源。
	var logs bytes.Buffer
	// client 提供带有明确多规格标记的列表结果。
	client := &itemSyncListClient{
		allResult: &mtop.ItemListResult{Items: []mtop.ItemListItem{
			{ID: "list-multi", IsMultiSpec: true},
			{ID: "list-single", IsMultiSpec: false},
		}, PageNumber: 1, PageSize: 20, TotalPages: 1},
	}
	// repository 使用列表接口替身执行同步。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, slog.New(slog.NewTextHandler(&logs, nil)), nil, nil)
	// owner、ownerErr 保存测试账号归属，供同步接口执行所有权校验。
	owner, ownerErr := store.Users.GetByUsername(context.Background(), "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// result、syncErr 保存全量同步结果。
	result, syncErr := repository.SyncAll(context.Background(), itemapp.SyncQuery{UserID: owner.ID, CookieID: "cid", PageSize: 20, MaxPages: 1})
	if syncErr != nil || result.TotalCount != 2 || result.SavedCount != 2 {
		t.Fatalf("列表同步异常 result=%+v err=%v", result, syncErr)
	}
	// multi、multiErr 和 single、singleErr 保存同步后的两条商品记录。
	multi, multiErr := store.Items.Get(context.Background(), "cid", "list-multi")
	// single、singleErr 保存列表中普通单规格商品的同步结果及读取错误。
	single, singleErr := store.Items.Get(context.Background(), "cid", "list-single")
	if multiErr != nil || singleErr != nil || !multi.IsMultiSpec || single.IsMultiSpec {
		t.Fatalf("列表多规格标记落库异常 multi=%+v single=%+v multiErr=%v singleErr=%v", multi, single, multiErr, singleErr)
	}
	if !strings.Contains(logs.String(), "列表 isSKU 字段") || !strings.Contains(logs.String(), "全量商品同步完成") {
		t.Fatalf("商品同步日志未说明多规格判断来源：%s", logs.String())
	}
}

// TestItemSyncSkipsDeleteWhenRemoteListEmptyButLocalActive 验证远端空列表不会清空本地在售商品。
func TestItemSyncSkipsDeleteWhenRemoteListEmptyButLocalActive(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 贯穿本地写入与同步调用。
	ctx := context.Background()
	// index 是本地在售商品序号，构造成规模账号以触发防误删保护。
	for index := 0; index < 12; index++ {
		// upsertErr 保存当前本地商品的写入错误，用于构造「本地确实有大量在售商品」的前提。
		if upsertErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: fmt.Sprintf("kept-%02d", index), ItemTitle: "保留商品"}); upsertErr != nil {
			t.Fatal(upsertErr)
		}
	}
	// logs 收集商品同步日志，用于验证跳过删除时有明确告警。
	var logs bytes.Buffer
	// client 返回空商品列表，模拟接口异常或分页不完整。
	client := &itemSyncListClient{allResult: &mtop.ItemListResult{PageNumber: 1, PageSize: 20, TotalPages: 0}}
	// repository 使用列表接口替身执行同步。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, slog.New(slog.NewTextHandler(&logs, nil)), nil, nil)
	// owner、ownerErr 保存测试账号归属，供同步接口执行所有权校验。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// result、syncErr 保存全量同步结果及错误。
	result, syncErr := repository.SyncAll(ctx, itemapp.SyncQuery{UserID: owner.ID, CookieID: "cid", PageSize: 20, MaxPages: 1})
	if syncErr != nil {
		t.Fatalf("空列表同步不应失败: %v", syncErr)
	}
	if result.DeletedCount != 0 {
		t.Fatalf("远端为空时不得删除本地商品 deleted=%d", result.DeletedCount)
	}
	// active、countErr 保存同步后本地在售商品数量及查询错误。
	active, countErr := store.Items.CountActive(ctx, "cid")
	if countErr != nil || active != 12 {
		t.Fatalf("本地在售商品应被保留 active=%d err=%v", active, countErr)
	}
	if !strings.Contains(logs.String(), "跳过本地删除") {
		t.Fatalf("跳过删除必须留下告警日志：%s", logs.String())
	}
}

// TestItemSyncStillCleansUpWhenSmallAccountRemoteListEmpty 验证商品很少的空列表仍按真实下架清理。
// 成规模账号才启用防误删，否则商家在平台清空少量商品后本地会长期残留已下架商品。
func TestItemSyncStillCleansUpWhenSmallAccountRemoteListEmpty(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 贯穿本地写入与同步调用。
	ctx := context.Background()
	// upsertErr 保存本地单个在售商品的写入错误。
	if upsertErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "stale-item", ItemTitle: "已下架商品"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// client 返回明确为空的商品列表，模拟商家在平台清空商品。
	client := &itemSyncListClient{allResult: &mtop.ItemListResult{PageNumber: 1, PageSize: 20, TotalPages: 0}}
	// repository 使用列表接口替身执行同步。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, nil, nil, nil)
	// owner、ownerErr 保存测试账号归属，供同步接口执行所有权校验。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// result、syncErr 保存全量同步结果及错误。
	result, syncErr := repository.SyncAll(ctx, itemapp.SyncQuery{UserID: owner.ID, CookieID: "cid", PageSize: 20, MaxPages: 1})
	if syncErr != nil {
		t.Fatalf("小账号空列表同步不应失败: %v", syncErr)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("小账号空列表应继续清理本地商品 deleted=%d", result.DeletedCount)
	}
	// active、countErr 保存同步后本地在售商品数量及查询错误。
	active, countErr := store.Items.CountActive(ctx, "cid")
	if countErr != nil || active != 0 {
		t.Fatalf("本地已下架商品应被清理 active=%d err=%v", active, countErr)
	}
}

// TestItemSyncSkipsDeleteWhenRemoteCountCollapses 验证远端数量暴跌时不会删除本地多余商品。
func TestItemSyncSkipsDeleteWhenRemoteCountCollapses(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 贯穿本地写入与同步调用。
	ctx := context.Background()
	// index 是本地在售商品序号，构造一个成规模的账号。
	for index := 0; index < 12; index++ {
		// itemID 是当前本地商品标识。
		itemID := fmt.Sprintf("local-%02d", index)
		// upsertErr 保存当前本地商品的写入错误。
		if upsertErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: itemID, ItemTitle: "在售商品"}); upsertErr != nil {
			t.Fatal(upsertErr)
		}
	}
	// logs 收集商品同步日志，用于验证跳过删除时有明确告警。
	var logs bytes.Buffer
	// client 只返回两条远端商品，远低于本地在售数量，模拟分页中断。
	client := &itemSyncListClient{allResult: &mtop.ItemListResult{Items: []mtop.ItemListItem{
		{ID: "local-00"}, {ID: "local-01"},
	}, PageNumber: 1, PageSize: 20, TotalPages: 1}}
	// repository 使用列表接口替身执行同步。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, slog.New(slog.NewTextHandler(&logs, nil)), nil, nil)
	// owner、ownerErr 保存测试账号归属，供同步接口执行所有权校验。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// result、syncErr 保存全量同步结果及错误。
	result, syncErr := repository.SyncAll(ctx, itemapp.SyncQuery{UserID: owner.ID, CookieID: "cid", PageSize: 20, MaxPages: 1})
	if syncErr != nil {
		t.Fatalf("数量暴跌同步不应失败: %v", syncErr)
	}
	if result.DeletedCount != 0 {
		t.Fatalf("远端数量暴跌时不得删除本地商品 deleted=%d", result.DeletedCount)
	}
	// active、countErr 保存同步后本地在售商品数量及查询错误。
	active, countErr := store.Items.CountActive(ctx, "cid")
	if countErr != nil || active != 12 {
		t.Fatalf("本地在售商品应全部保留 active=%d err=%v", active, countErr)
	}
	if !strings.Contains(logs.String(), "跳过本地删除") {
		t.Fatalf("跳过删除必须留下告警日志：%s", logs.String())
	}
}
