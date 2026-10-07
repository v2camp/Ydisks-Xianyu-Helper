package engine

import (
	"testing"
	"time"
)

// TestAccountReconnectStaggerIsStableAndBounded 验证账号重连错峰量的稳定性和取值范围。
//
// 回归背景：2026-10-06 22:02 两个账号在缺少错峰时同一时刻重连并同时请求 token，
// 直接放大了平台风控命中概率。错峰量必须以账号标识的稳定哈希为基，保证同一账号
// 每次退避可预期，同时不同账号之间自然分散到 [1s, reconnectStaggerSpan]。
func TestAccountReconnectStaggerIsStableAndBounded(t *testing.T) {
	// ids 保存用于覆盖多种账号标识形态的样本。
	ids := []string{"", "a", "cid", "unb=123", "47983389009", "account-with-long-name-0123456789"}
	// seen 记录不同账号出现的错峰量，用于确认不同账号确实被分散。
	seen := map[time.Duration]struct{}{}
	// id 表示当前遍历到的账号标识样本。
	for _, id := range ids {
		// first 是首次计算的错峰量。
		first := accountReconnectStagger(id)
		// second 是同一账号再次计算的错峰量，必须与 first 完全一致。
		second := accountReconnectStagger(id)
		if first != second {
			t.Fatalf("账号 %q 的错峰量不稳定: first=%v second=%v", id, first, second)
		}
		if first < time.Second || first > reconnectStaggerSpan {
			t.Fatalf("账号 %q 错峰量越界: %v", id, first)
		}
		seen[first] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("不同账号未产生分散的错峰量: %v", seen)
	}
}

// TestNetworkRetryDelayIncludesAccountStagger 验证重连退避在指数退避之上叠加了账号错峰。
//
// 该用例锁死「退避 = 指数退避 + 账号错峰」这一组合关系，避免后续改动把错峰量丢掉，
// 使多账号在同一时刻集中重连。
func TestNetworkRetryDelayIncludesAccountStagger(t *testing.T) {
	// account 是用于验证退避组合关系的本地账号。
	account := New(Config{CookieID: "stagger-account", CookieStr: "unb=1"})
	// stagger 是该账号应叠加的稳定错峰量。
	stagger := accountReconnectStagger(account.CookieID)
	account.recordNetworkFailure()
	// got 是首次失败后的退避时长。
	got := account.networkRetryDelay()
	// base 是叠加错峰前的指数退避基准（1 次失败为 4s）。
	base := 4 * time.Second
	// total 是叠加错峰后、抖动前应达到的退避基准；抖动按该值再上浮至多 30%。
	total := base + stagger
	if got < total || got >= total+total*3/10 {
		t.Fatalf("退避未按预期叠加账号错峰: got=%v base=%v stagger=%v", got, base, stagger)
	}
}
