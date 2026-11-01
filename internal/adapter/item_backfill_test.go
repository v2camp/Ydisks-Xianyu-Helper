package adapter

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// itemBackfillClientFixture 是单商品回补测试使用的 MTOP 客户端替身，
// 在默认客户端实现上补充可选详情能力，用于隔离平台网络。
type itemBackfillClientFixture struct {
	// orderRuntimeMTopFake 提供回补测试不涉及的其余 MTOP 接口默认实现。
	orderRuntimeMTopFake
	// snapshot 是详情查询成功时返回的强类型商品事实。
	snapshot mtop.ItemDetailSnapshot
	// detailErr 是详情查询按场景返回的错误。
	detailErr error
	// calls 记录详情查询次数，用于验证失败路径不会重复请求。
	calls int
}

// FetchItemDetailSnapshot 返回预置详情快照或错误，并记录调用次数。
func (fixture *itemBackfillClientFixture) FetchItemDetailSnapshot(context.Context, string, string) (mtop.ItemDetailSnapshot, error) {
	fixture.calls++
	return fixture.snapshot, fixture.detailErr
}

// itemBackfillListOnlyClient 是只实现基础 Client 的替身，用于验证缺少可选详情能力时 fail-closed。
type itemBackfillListOnlyClient struct {
	// orderRuntimeMTopFake 提供基础 Client 的全部方法。
	orderRuntimeMTopFake
}

// TestItemBackfillWritesOwnedItem 验证发布人与当前账号一致时把商品写入本地库，并让归属门禁放行。
func TestItemBackfillWritesOwnedItem(t *testing.T) {
	// store、cleanup 提供隔离数据库与关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 返回与测试账号（unb=1）一致的发布人。
	client := &itemBackfillClientFixture{snapshot: mtop.ItemDetailSnapshot{
		ItemID: "item-1", Title: "二手相机", Description: "成色良好", Price: "1999", PriceText: "￥1999",
		CategoryID: "5001", SellerID: "1", DetailJSON: `{"title":"二手相机"}`,
	}}
	// repository 是待测回补仓储，使用替身客户端隔离平台网络。
	repository := NewItemBackfillRepository(store, func() mtop.Client { return client }, slog.Default())
	if // err 是回补结果。
	err := repository.BackfillItem(context.Background(), "cid", "item-1"); err != nil {
		t.Fatalf("回补应成功: %v", err)
	}
	// item、getErr 保存回补后写入的本地商品。
	item, getErr := store.Items.Get(context.Background(), "cid", "item-1")
	if getErr != nil {
		t.Fatalf("回补后商品应存在于本地库: %v", getErr)
	}
	if item.ItemTitle != "二手相机" || item.ItemPrice != "1999" || item.ItemCategory != "5001" || item.ItemDescription != "成色良好" {
		t.Fatalf("回补写入字段异常: %+v", item)
	}
	// owned、ownedErr 是准入层使用的本地归属结论，回补后必须为真。
	owned, ownedErr := New(store, nil, slog.Default()).ItemBelongsToAccount(context.Background(), "cid", "item-1")
	if ownedErr != nil || !owned {
		t.Fatalf("回补后归属门禁应放行: owned=%v err=%v", owned, ownedErr)
	}
}

// TestItemBackfillRejectsForeignSeller 验证发布人不是当前账号时拒绝落库，避免把买家咨询的商品当成本账号库存。
func TestItemBackfillRejectsForeignSeller(t *testing.T) {
	// store、cleanup 提供隔离数据库与关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 返回他人商品详情。
	client := &itemBackfillClientFixture{snapshot: mtop.ItemDetailSnapshot{ItemID: "item-2", Title: "他人商品", SellerID: "999"}}
	// repository 是待测回补仓储，客户端返回他人商品详情。
	repository := NewItemBackfillRepository(store, func() mtop.Client { return client }, slog.Default())
	// err 是回补结果，必须因归属不一致而失败。
	err := repository.BackfillItem(context.Background(), "cid", "item-2")
	if err == nil || !strings.Contains(err.Error(), "发布人") {
		t.Fatalf("他人商品必须拒绝回补: %v", err)
	}
	if // owned 是归属门禁结论，必须仍为 false。
	owned, _ := New(store, nil, slog.Default()).ItemBelongsToAccount(context.Background(), "cid", "item-2"); owned {
		t.Fatal("他人商品不得进入本地商品库")
	}
}

// TestItemBackfillPropagatesDetailError 验证平台详情请求失败时回补返回错误，不写入任何本地商品。
func TestItemBackfillPropagatesDetailError(t *testing.T) {
	// store、cleanup 提供隔离数据库与关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 返回平台错误。
	client := &itemBackfillClientFixture{detailErr: errors.New("平台限流")}
	// repository 是待测回补仓储，客户端固定返回平台错误。
	repository := NewItemBackfillRepository(store, func() mtop.Client { return client }, slog.Default())
	if // err 是回补结果。
	err := repository.BackfillItem(context.Background(), "cid", "item-3"); err == nil {
		t.Fatal("详情失败必须返回错误")
	}
	if // owned 是归属门禁结论。
	owned, _ := New(store, nil, slog.Default()).ItemBelongsToAccount(context.Background(), "cid", "item-3"); owned {
		t.Fatal("详情失败不得写入本地商品")
	}
}

// TestItemBackfillRequiresDetailCapability 验证客户端未实现可选详情接口时 fail-closed。
func TestItemBackfillRequiresDetailCapability(t *testing.T) {
	// store、cleanup 提供隔离数据库与关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 注入只实现基础 Client 的替身。
	repository := NewItemBackfillRepository(store, func() mtop.Client { return &itemBackfillListOnlyClient{} }, slog.Default())
	if // err 是回补结果。
	err := repository.BackfillItem(context.Background(), "cid", "item-4"); err == nil {
		t.Fatal("缺少详情能力必须返回错误")
	}
}

// TestAdapterBackfillAndDecisionLog 验证 Adapter 同时满足引擎的可选回补端口与可选留痕端口。
func TestAdapterBackfillAndDecisionLog(t *testing.T) {
	// store、cleanup 提供隔离数据库与关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 返回与测试账号一致的发布人。
	client := &itemBackfillClientFixture{snapshot: mtop.ItemDetailSnapshot{ItemID: "item-5", Title: "联动商品", SellerID: "1"}}
	// adapter 是可注入替身客户端的适配器；browser 传 nil 只使用回补与留痕能力。
	adapter := New(store, nil, slog.Default())
	adapter.itemBackfill = NewItemBackfillRepository(store, func() mtop.Client { return client }, slog.Default())
	if // err 是经 Adapter 委托的回补结果。
	err := adapter.BackfillItem(context.Background(), "cid", "item-5"); err != nil {
		t.Fatalf("Adapter 回补应成功: %v", err)
	}
	if // err 是决策归因写入结果。
	err := adapter.AddDecisionLog(context.Background(), db.AIDecisionRecord{
		CookieID: "cid", ChatID: "chat", ItemID: "item-5", Reason: "item-sync-requested",
	}); err != nil {
		t.Fatalf("Adapter 决策留痕应成功: %v", err)
	}
	// count 是已写入的归因记录数量。
	var count int
	if // err 是归因数量查询错误。
	err := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM ai_decision_log WHERE cookie_id=? AND item_id=? AND reason=?`,
		"cid", "item-5", "item-sync-requested").Scan(&count); err != nil {
		t.Fatalf("查询归因记录失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("归因记录未落库: count=%d", count)
	}
}
