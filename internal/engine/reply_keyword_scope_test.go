// reply_keyword_scope_test.go 覆盖关键词规则的商品范围匹配：多商品规则必须命中任一关联商品。
// 该行为在合入上游多商品关键词能力时被遗漏过，此文件作为回归用例长期保留。
package engine

import (
	"context"
	"path/filepath"
	"testing"

	"xianyu-go/internal/db"
)

// TestContainsItemID 验证商品范围字段的覆盖判定：单值、逗号多值、带空格、未命中与空值。
func TestContainsItemID(t *testing.T) {
	// cases 保存每个用例的商品范围字段、目标商品与期望结果。
	cases := []struct {
		// name 是用例名称，描述被验证的匹配形态。
		name string
		// raw 是持久化的商品范围字段原文。
		raw string
		// target 是本次消息所属的商品标识。
		target string
		// want 是期望的覆盖判定结果。
		want bool
	}{
		{name: "单值命中", raw: "item-a", target: "item-a", want: true},
		{name: "单值未命中", raw: "item-a", target: "item-b", want: false},
		{name: "多值命中首项", raw: "item-a,item-b", target: "item-a", want: true},
		{name: "多值命中末项", raw: "item-a,item-b", target: "item-b", want: true},
		{name: "多值未命中", raw: "item-a,item-b", target: "item-c", want: false},
		{name: "多值带空格命中", raw: "item-a, item-b", target: "item-b", want: true},
		{name: "空范围不覆盖", raw: "", target: "item-a", want: false},
		{name: "空目标不命中", raw: "item-a", target: "", want: false},
		{name: "前缀不算命中", raw: "item-ab", target: "item-a", want: false},
	}
	// testCase 是当前被验证的商品覆盖用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 是本次商品覆盖判定的实际结果。
			if got := containsItemID(testCase.raw, testCase.target); got != testCase.want {
				t.Fatalf("containsItemID(%q, %q) = %v，期望 %v", testCase.raw, testCase.target, got, testCase.want)
			}
		})
	}
}

// TestKeywordReplyMatchesMultiItemRule 回归验证：关联两个商品的规则对其中任一商品都必须命中，
// 账号级通用关键词与未关联商品的消息不受影响。
func TestKeywordReplyMatchesMultiItemRule(t *testing.T) {
	// store、cleanup 保存本用例专用的 SQLite 存储及其释放函数。
	store, cleanup := newKeywordScopeStore(t)
	defer cleanup()
	// ctx 是写入夹具与驱动回复共用的上下文。
	ctx := context.Background()
	// insertErr 保存多商品关键词规则的写入错误。
	if _, insertErr := store.DB.ExecContext(ctx,
		`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type) VALUES (?, ?, ?, ?, 'text')`,
		"cid", "还有吗", "有货，直接拍下即可", "item-a,item-b"); insertErr != nil {
		t.Fatalf("写入多商品关键词失败: %v", insertErr)
	}
	// service 是不含 API/AI/插件层的纯关键词回复服务。
	service := NewReplyService("cid", store, nil, nil, nil, nil, nil, nil)

	// cases 保存被验证的消息与期望命中的回复文本。
	cases := []struct {
		// name 是用例名称，说明消息所属商品。
		name string
		// itemID 是消息携带的商品标识。
		itemID string
		// wantText 是期望的回复文本；空串表示不应命中该规则。
		wantText string
	}{
		{name: "命中首项商品", itemID: "item-a", wantText: "有货，直接拍下即可"},
		{name: "命中末项商品", itemID: "item-b", wantText: "有货，直接拍下即可"},
		{name: "未关联商品不命中", itemID: "item-c", wantText: ""},
	}
	// testCase 是当前被验证的消息用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// result 是当前消息的关键词层匹配结果，nil 表示未命中。
			result := service.keywordReply(ctx, ChatMessage{ItemID: testCase.itemID, Text: "这个还有吗"})
			if testCase.wantText == "" {
				if result != nil {
					t.Fatalf("商品 %s 不应命中多商品规则，实际得到 %+v", testCase.itemID, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("商品 %s 应命中多商品规则，实际未命中", testCase.itemID)
			}
			if result.Text != testCase.wantText || result.Source != "关键词" {
				t.Fatalf("商品 %s 命中结果错误: text=%q source=%q", testCase.itemID, result.Text, result.Source)
			}
		})
	}
}

// newKeywordScopeStore 构造带 admin 用户与 cookie_id="cid" 账号的最小存储，满足关键词表外键约束。
func newKeywordScopeStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// database、openErr 保存临时 SQLite 数据库及其打开错误。
	database, _, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "keyword-scope.db"))
	if openErr != nil {
		t.Fatalf("打开测试数据库失败: %v", openErr)
	}
	// store 是测试用的仓储集合。
	store := db.NewStore(database, db.DialectSQLite)
	// ctx 用于创建夹具数据。
	ctx := context.Background()
	// createErr 保存测试用户创建结果错误。
	if _, createErr := store.Users.Create(ctx, "keyword-scope-user", "scope@example.com", "pw"); createErr != nil {
		t.Fatalf("创建测试用户失败: %v", createErr)
	}
	// user、userErr 保存测试用户身份及读取错误。
	user, userErr := store.Users.GetByUsername(ctx, "keyword-scope-user")
	if userErr != nil {
		t.Fatalf("读取测试用户失败: %v", userErr)
	}
	// saveErr 保存账号凭证夹具的写入错误。
	if saveErr := store.Cookies.Save(ctx, "cid", "unb=123; _m_h5_tk=tk_1;", user.ID); saveErr != nil {
		t.Fatalf("写入测试账号失败: %v", saveErr)
	}
	return store, func() { _ = database.Close() }
}
