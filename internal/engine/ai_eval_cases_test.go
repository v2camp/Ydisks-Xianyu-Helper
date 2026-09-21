// ai_eval_cases_test.go 决策层评测集 v1：每条消息标注「期望意图 + 是否接管 + 期望负向拦截」，
// 用于零账号离线评测「该不该 AI 接管、进哪个意图、是否被负向词拦截」。
// 数据全部脱敏：文本按升级计划第五节语义类别重新拟写的短句，不含真实聊天全文与凭证。
package engine

// 特殊语义保持检查的标注名，供 runner 按标注执行对应语义回归。
const (
	// aiEvalSpecialLabelOnly 是「ai=false 意图只标注不接管」特殊语义标注。
	aiEvalSpecialLabelOnly = "label-only"
	// aiEvalSpecialMergeAmbiguous 是「多意图归并 ambiguous」特殊语义标注。
	aiEvalSpecialMergeAmbiguous = "merge-ambiguous"
	// aiEvalSpecialExtraKeywords 是「负向扩展词表 ai_complaint_keywords 行为不变」特殊语义标注。
	aiEvalSpecialExtraKeywords = "extra-keywords"
	// aiEvalSpecialRealPhrase 是「真实口语负向话术必须被拦截」特殊语义标注，
	// 用于锁定升级计划第五节梳理出的真实买家话术（如「那我退了吧」「扣了20」）不回归漏拦。
	aiEvalSpecialRealPhrase = "real-phrase"
)

// aiEvalCase 是一条决策层评测用例。
type aiEvalCase struct {
	// Name 是用例名，用于失败定位与日志输出。
	Name string
	// Text 是脱敏后的买家消息文本。
	Text string
	// WantIntent 是期望意图标签（bargain/order/inquiry/consult/stock/ambiguous/complaint/chitchat）。
	WantIntent string
	// WantTakeover 是期望是否允许 AI 接管。
	WantTakeover bool
	// WantNegative 是期望是否被负向拦截（内置/配置/扩展词表任一命中）。
	WantNegative bool
	// Special 是特殊语义保持检查标注；空串表示不参与。
	Special string
}

// aiEvalGroup 是一组共享同一配置场景的评测用例。
type aiEvalGroup struct {
	// Name 是配置场景名，如默认内置/生产种子配置。
	Name string
	// ScopeConfig 是写入 ai_scope_config 的 JSON；空串表示未配置回落内置。
	ScopeConfig string
	// ComplaintKeywords 是写入 ai_complaint_keywords 的扩展负向词表；空串表示不扩展。
	ComplaintKeywords string
	// Cases 是该场景下的评测用例列表。
	Cases []aiEvalCase
}

// aiEvalSeedScopeJSON 是生产种子边界配置：砍价/商品/发货/库存四类意图接管，
// 负向词表为内置词表追加违规/扣分/封号/申诉，与升级计划第六节一致。
// 评测驱动优化：负向词表追加口语表达「退了|扣了」，覆盖升级计划第五节真实话术
// 「那我退了吧」「扣了20」的漏拦（详见 customer-service-agent-upgrade-plan.md 九、遗留）。
// 该配置是评测集标注的期望基线；生产库 ai_scope_config 的 negative 需同步更新后才生效。
const aiEvalSeedScopeJSON = `{"intents":[
	{"id":"bargain","enabled":true,"ai":true,"match":"便宜|优惠|少点|最低|砍价|降价|打折"},
	{"id":"product","enabled":true,"ai":true,"match":"正版|音频|文字版"},
	{"id":"delivery","enabled":true,"ai":true,"match":"发货|网盘|夸克|资源码|提取码|下载"},
	{"id":"stock","enabled":true,"ai":true,"match":"\"有.{0,4}季\"|全集|单买|在售"}
],"negative":"退款|退货|投诉|差评|举报|骗子|骗人|假货|被骗|维权|违规|扣分|封号|申诉|退了|扣了"}`

// aiEvalBadScopeJSON 是语法非法的边界配置，用于验证非法配置回落内置行为。
const aiEvalBadScopeJSON = `{"intents": [{"id":"bargain","match":"便宜"}], "negative": }`

// aiEvalEmptyScopeJSON 是全部意图禁用、无可用意图的边界配置，同样应回落内置。
const aiEvalEmptyScopeJSON = `{"intents":[{"id":"x","enabled":false,"match":"a"}]}`

// aiEvalSetV1 是决策层评测集 v1，按配置场景分组，覆盖升级计划第五节的语义类别。
// 分组设计：默认内置边界、生产种子配置、多意图归并、扩展负向词表、非法/空配置回落。
var aiEvalSetV1 = []aiEvalGroup{
	{
		Name:              "默认内置边界",
		ScopeConfig:       "",
		ComplaintKeywords: "",
		Cases: []aiEvalCase{
			{Name: "砍价接管", Text: "能便宜点吗", WantIntent: IntentBargain, WantTakeover: true},
			{Name: "砍价优惠接管", Text: "可以优惠点吗", WantIntent: IntentBargain, WantTakeover: true},
			{Name: "砍价最低接管", Text: "最低多少能出", WantIntent: IntentBargain, WantTakeover: true},
			{Name: "发货咨询只标注", Text: "什么时候发货", WantIntent: IntentOrder, WantTakeover: false, Special: aiEvalSpecialLabelOnly},
			{Name: "提取码咨询只标注", Text: "提取码是多少", WantIntent: IntentOrder, WantTakeover: false, Special: aiEvalSpecialLabelOnly},
			{Name: "询价只标注", Text: "多少钱", WantIntent: IntentInquiry, WantTakeover: false, Special: aiEvalSpecialLabelOnly},
			{Name: "包邮咨询只标注", Text: "包邮吗", WantIntent: IntentInquiry, WantTakeover: false, Special: aiEvalSpecialLabelOnly},
			{Name: "适合咨询只标注", Text: "这个适合吗", WantIntent: IntentConsult, WantTakeover: false, Special: aiEvalSpecialLabelOnly},
			{Name: "内容咨询只标注", Text: "内容是什么", WantIntent: IntentConsult, WantTakeover: false, Special: aiEvalSpecialLabelOnly},
			{Name: "退款负向拦截", Text: "我要退款", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "退货负向拦截", Text: "退货怎么弄", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "骗子负向拦截", Text: "你是骗子", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "退款叠加砍价否决", Text: "便宜点，不行我退款", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "投诉负向拦截", Text: "我要投诉你", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "差评负向拦截", Text: "差评警告", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "举报负向拦截", Text: "被举报了", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "假货负向拦截", Text: "卖的是假货吧", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "被骗负向拦截", Text: "被骗了", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "维权负向拦截", Text: "我要维权", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "闲聊在吗不接管", Text: "在吗", WantIntent: IntentChitchat, WantTakeover: false},
			{Name: "闲聊谢谢不接管", Text: "谢谢", WantIntent: IntentChitchat, WantTakeover: false},
			{Name: "闲聊你好不接管", Text: "你好", WantIntent: IntentChitchat, WantTakeover: false},
		},
	},
	{
		Name:              "生产种子配置",
		ScopeConfig:       aiEvalSeedScopeJSON,
		ComplaintKeywords: "",
		Cases: []aiEvalCase{
			{Name: "砍价接管", Text: "还能便宜点吗", WantIntent: "bargain", WantTakeover: true},
			{Name: "砍价最低接管", Text: "最低多少能出", WantIntent: "bargain", WantTakeover: true},
			{Name: "商品正版接管", Text: "是正版的吗", WantIntent: "product", WantTakeover: true},
			{Name: "商品音频接管", Text: "有音频嘛", WantIntent: "product", WantTakeover: true},
			{Name: "商品文字版接管", Text: "文字版在哪里", WantIntent: "product", WantTakeover: true},
			{Name: "发货网盘接管", Text: "百度网盘发货吗", WantIntent: "delivery", WantTakeover: true},
			{Name: "发货夸克接管", Text: "有夸克的吗", WantIntent: "delivery", WantTakeover: true},
			{Name: "发货资源码接管", Text: "资源码怎么用", WantIntent: "delivery", WantTakeover: true},
			{Name: "库存季数接管", Text: "有第三季了吗", WantIntent: "stock", WantTakeover: true},
			{Name: "库存单买接管", Text: "可以单买第二季吗", WantIntent: "stock", WantTakeover: true},
			{Name: "库存全集接管", Text: "是全集吗", WantIntent: "stock", WantTakeover: true},
			{Name: "退款负向拦截", Text: "那我退款吧", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "举报负向拦截", Text: "被举报了", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "扣分负向拦截", Text: "会不会扣分", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "申诉负向拦截", Text: "申诉一下", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "投诉叠加砍价否决", Text: "便宜点，不然我投诉", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
			{Name: "真实口语退款拦截", Text: "那我退了吧", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true, Special: aiEvalSpecialRealPhrase},
			{Name: "真实口语扣分拦截", Text: "扣了20", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true, Special: aiEvalSpecialRealPhrase},
			{Name: "闲聊在吗不接管", Text: "在吗", WantIntent: IntentChitchat, WantTakeover: false},
			{Name: "闲聊谢谢不接管", Text: "谢谢", WantIntent: IntentChitchat, WantTakeover: false},
		},
	},
	{
		Name:              "多意图归并",
		ScopeConfig:       "",
		ComplaintKeywords: "",
		Cases: []aiEvalCase{
			{Name: "砍价叠加询价归并", Text: "最低多少钱能卖", WantIntent: IntentAmbiguous, WantTakeover: true, Special: aiEvalSpecialMergeAmbiguous},
			{Name: "砍价叠加发货归并", Text: "能便宜点吗，是百度网盘发货吗", WantIntent: IntentAmbiguous, WantTakeover: true, Special: aiEvalSpecialMergeAmbiguous},
			{Name: "询价叠加发货归并不接管", Text: "多少钱，什么时候发货", WantIntent: IntentAmbiguous, WantTakeover: false, Special: aiEvalSpecialMergeAmbiguous},
		},
	},
	{
		Name:              "扩展负向词表",
		ScopeConfig:       "",
		ComplaintKeywords: "封号,违规,申诉,扣分",
		Cases: []aiEvalCase{
			{Name: "封号扩展拦截", Text: "会封号吗", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true, Special: aiEvalSpecialExtraKeywords},
			{Name: "违规扩展拦截", Text: "违规了怎么办", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true, Special: aiEvalSpecialExtraKeywords},
			{Name: "扣分扩展拦截", Text: "会不会扣分", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true, Special: aiEvalSpecialExtraKeywords},
			{Name: "扩展叠加砍价否决", Text: "便宜点，不然我申诉", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true, Special: aiEvalSpecialExtraKeywords},
			{Name: "扩展不误伤砍价", Text: "能便宜点吗", WantIntent: IntentBargain, WantTakeover: true, WantNegative: false, Special: aiEvalSpecialExtraKeywords},
		},
	},
	{
		Name:              "非法JSON回落内置",
		ScopeConfig:       aiEvalBadScopeJSON,
		ComplaintKeywords: "",
		Cases: []aiEvalCase{
			{Name: "回落内置仍接管砍价", Text: "能便宜点吗", WantIntent: IntentBargain, WantTakeover: true},
			{Name: "回落内置询价只标注", Text: "多少钱", WantIntent: IntentInquiry, WantTakeover: false},
			{Name: "回落内置退款仍拦截", Text: "我要退款", WantIntent: IntentComplaint, WantTakeover: false, WantNegative: true},
		},
	},
	{
		Name:              "空意图回落内置",
		ScopeConfig:       aiEvalEmptyScopeJSON,
		ComplaintKeywords: "",
		Cases: []aiEvalCase{
			{Name: "回落内置仍接管砍价", Text: "能便宜点吗", WantIntent: IntentBargain, WantTakeover: true},
		},
	},
}
