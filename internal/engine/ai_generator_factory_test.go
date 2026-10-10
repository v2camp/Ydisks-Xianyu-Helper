// ai_generator_factory_test.go 覆盖账号级模型生成接缝的注入链路：
// Config 上的工厂被调用一次、返回的生成器真正装进 AI 回复实现、返回 nil 时回落默认实现。

package engine

import (
	"testing"
)

// TestAccountConfigInjectsAIGeneratorFactory 验证工厂按账号标识构造并被装入 AI 回复实现。
func TestAccountConfigInjectsAIGeneratorFactory(t *testing.T) {
	// store、cleanup 是带 admin 与 cookie 的测试仓储及清理函数。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// stub 是注入用的生成器替身，用于断言它确实被装进实现。
	stub := &recordingAIGenerator{content: "注入生成器"}
	// factoryCookieID 记录工厂收到的账号标识。
	factoryCookieID := ""
	// account 是注入工厂后的账号运行时。
	account := New(Config{
		CookieID: "cid",
		Store:    store,
		// AIGeneratorFactory 是组合层注入客服 Agent 的入口。
		AIGeneratorFactory: func(cookieID string) AIGenerator {
			factoryCookieID = cookieID
			return stub
		},
	})
	if factoryCookieID != "cid" {
		t.Fatalf("工厂必须收到账号标识: got=%q", factoryCookieID)
	}
	if account.reply == nil {
		t.Fatal("Store 可用时回复服务必须被装配")
	}
	// impl、ok 分别是回复服务持有的 AI 实现及其类型断言结果。
	impl, ok := account.reply.ai.(*AIReplierImpl)
	if !ok {
		t.Fatalf("回复服务应持有 AIReplierImpl: %T", account.reply.ai)
	}
	if impl.gen != AIGenerator(stub) {
		t.Fatalf("注入的生成器必须生效: %T", impl.gen)
	}
}

// TestAccountConfigFallsBackWhenFactoryReturnsNil 验证工厂返回 nil 时回落默认单次问答实现。
func TestAccountConfigFallsBackWhenFactoryReturnsNil(t *testing.T) {
	// store、cleanup 是带 admin 与 cookie 的测试仓储及清理函数。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// factoryCalls 记录工厂被调用的次数。
	factoryCalls := 0
	// account 是工厂返回 nil 的账号运行时。
	account := New(Config{
		CookieID: "cid",
		Store:    store,
		AIGeneratorFactory: func(string) AIGenerator {
			factoryCalls++
			return nil
		},
	})
	if factoryCalls != 1 {
		t.Fatalf("工厂应被调用一次: %d", factoryCalls)
	}
	// impl、ok 分别是回复服务持有的 AI 实现及其类型断言结果。
	impl, ok := account.reply.ai.(*AIReplierImpl)
	if !ok {
		t.Fatalf("回复服务应持有 AIReplierImpl: %T", account.reply.ai)
	}
	if // genIsDefault 表示工厂返回 nil 后是否回落到默认单次问答实现。
	_, genIsDefault := impl.gen.(DefaultAIGenerator); !genIsDefault {
		t.Fatalf("工厂返回 nil 时应回落默认实现: %T", impl.gen)
	}
}

// TestAccountConfigWithoutFactoryUsesDefaultGenerator 验证未装配工厂时保持既有默认行为。
func TestAccountConfigWithoutFactoryUsesDefaultGenerator(t *testing.T) {
	// store、cleanup 是带 admin 与 cookie 的测试仓储及清理函数。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// account 是未装配生成器工厂的账号运行时。
	account := New(Config{CookieID: "cid", Store: store})
	// impl、ok 分别是回复服务持有的 AI 实现及其类型断言结果。
	impl, ok := account.reply.ai.(*AIReplierImpl)
	if !ok {
		t.Fatalf("回复服务应持有 AIReplierImpl: %T", account.reply.ai)
	}
	if // genIsDefault 表示未装配工厂时是否使用默认单次问答实现。
	_, genIsDefault := impl.gen.(DefaultAIGenerator); !genIsDefault {
		t.Fatalf("未装配工厂时应使用默认实现: %T", impl.gen)
	}
}
