// escalation_guard_test.go 诊断并锁定「必须转人工的表达」不被 AI 接管。
//
// 2026-10-11 benchmark 发现：退款、投诉、举报、转人工、加微信、差评共 6 类场景
// 决策层判定 0/6 被接管。本文件用最小用例定位失效环节并作为回归护栏。
package engine

import "testing"

// escalationPhrases 是必须交人工、不得由 AI 接管的买家表达。
var escalationPhrases = []string{
	"我要退款",
	"我要投诉",
	"我要举报这个店铺",
	"转人工",
	"加个微信聊吧",
	"东西不行我要给差评",
}

// TestBuiltinComplaintExprCompiles 内置负向词表必须能编译成判定函数。
// 编译失败时 compileSafe 会静默回落为恒不命中，这是决策层失效的隐藏根因。
func TestBuiltinComplaintExprCompiles(t *testing.T) {
	// negative 是内置负向词表的判定函数。
	negative := compileSafe(builtinComplaintExpr)
	// probe 是任一必须被拦截的探针词。
	probe := escalationPhrases[0]
	if !negative(probe) {
		t.Fatalf("内置负向词表未能拦截探针 %q：组合词编译很可能失败并回落为恒不命中", probe)
	}
}

// TestBuiltinComplaintCoversEscalationPhrases 逐条验证必须转人工的表达能被内置词表拦截。
func TestBuiltinComplaintCoversEscalationPhrases(t *testing.T) {
	// negative 是内置负向词表的判定函数。
	negative := compileSafe(builtinComplaintExpr)
	// phrase 是当前遍历到的买家表达。
	for _, phrase := range escalationPhrases {
		if !negative(phrase) {
			t.Errorf("内置负向词表未拦截: %q", phrase)
		}
	}
}

// TestBuiltinComplaintDoesNotBlockNormalInquiry 负向词表不得误伤正常咨询。
// 这是补词时的底线：宁可漏拦不可错拦，否则正常会话会被大面积夺走接管权。
func TestBuiltinComplaintDoesNotBlockNormalInquiry(t *testing.T) {
	// negative 是内置负向词表的判定函数。
	negative := compileSafe(builtinComplaintExpr)
	// normal 是必须保持接管的正常咨询表达。
	normal := []string{
		"有度盘的吗",
		"能不能便宜点",
		"这个一共多少集",
		"支持网盘发货吗",
		"怎么发货",
	}
	// phrase 是当前遍历到的正常咨询表达。
	for _, phrase := range normal {
		if negative(phrase) {
			t.Errorf("负向词表误伤正常咨询: %q", phrase)
		}
	}
}
