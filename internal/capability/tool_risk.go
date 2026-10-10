// tool_risk.go 登记平台对外暴露的全部工具及其危险等级。
//
// 它与 catalog_platform.go 的分工：
//
//   - PlatformCatalog 是**决策目录**，只登记已经接入消费方的能力。未登记的能力
//     求值恒为拒绝，因此「尚未接入」在结构上等价于「不可调用」，这是客服端最保守
//     的那道门。
//   - 本文件的表是**标注表**，覆盖传输层实际暴露的全部工具。它回答「这个动作危险
//     到什么程度」，不回答「现在谁可以调用它」。
//
// 两者评价同一个人工判断，但变更节奏不同：新增一个 MCP 工具必须同时登记危险等级
// （未登记等级的工具不注册，见 internal/mcp/registry.go），而是否对客服端开放要等
// 该工具真正接入 Agent 时才在决策目录里登记。把两件事合成一张表，会让「加一个工具」
// 意外变成「开放一个能力」。
//
// 表本身不参与求值：它只被传输层投影成工具声明。危险等级的权威语义与求值规则
// 仍只由 Risk 与 policy.go 定义。

package capability

import (
	"fmt"
	"sort"
)

// ToolRiskSpec 是一条工具危险等级声明。
type ToolRiskSpec struct {
	// Name 是工具名，与各传输层对外暴露的名称一致。
	Name string
	// Risk 是该工具的危险等级。
	Risk Risk
}

// ToolRiskTable 是工具危险等级的只读注册表。
//
// 装配期一次性构建后不再变更，因此可被多个消费方并发读取而无需加锁。
type ToolRiskTable struct {
	// risks 是按工具名索引的等级表，构建完成后只读。
	risks map[string]Risk
}

// NewToolRiskTable 用给定声明构建标注表。
//
// 任一声明非法或名称重复时返回错误，不静默跳过：漏标的工具会让传输层拒绝注册该
// 工具，这类问题必须在装配期就暴露，而不是等到 Harness 报告「工具不存在」。
func NewToolRiskTable(specs ...ToolRiskSpec) (*ToolRiskTable, error) {
	// table 是构建中的名称索引，构建完成后不再修改。
	table := make(map[string]Risk, len(specs))
	// spec 是当前待登记的工具声明。
	for _, spec := range specs {
		if spec.Name == "" || !spec.Risk.Valid() {
			return nil, fmt.Errorf("工具危险等级声明非法，无法登记：%q", spec.Name)
		}
		// exists 表示同名工具是否已被占用。
		if _, exists := table[spec.Name]; exists {
			return nil, fmt.Errorf("工具名重复，无法登记：%q", spec.Name)
		}
		table[spec.Name] = spec.Risk
	}
	return &ToolRiskTable{risks: table}, nil
}

// Lookup 按工具名查找危险等级；未登记时 ok 为 false。
//
// 接收者为 nil 时同样返回 false，使未装配标注表的部署表现为「工具不注册」而非放行。
func (t *ToolRiskTable) Lookup(name string) (Risk, bool) {
	if t == nil {
		return "", false
	}
	// risk 和 ok 分别是命中的等级与命中标志。
	risk, ok := t.risks[name]
	return risk, ok
}

// Names 返回全部已登记工具名，按字典序排列。
//
// 排序保证同一张表每次输出一致，便于与传输层的工具清单做双向对照。
func (t *ToolRiskTable) Names() []string {
	if t == nil {
		return nil
	}
	// names 是待排序的工具名集合。
	names := make([]string, 0, len(t.risks))
	// name 是当前遍历到的工具名。
	for name := range t.risks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len 返回已登记的工具数量。
func (t *ToolRiskTable) Len() int {
	if t == nil {
		return 0
	}
	return len(t.risks)
}

// PlatformToolRisks 返回平台暴露面的危险等级表。
//
// 定级规则（按影响面递增的五个等级逐条落位）：
//
//   - read：进程内只读，无对外副作用、无额度消耗、重复调用结果一致。
//   - quote：生成对外承诺但不改写业务数据。本期没有工具落在此级，保留等级本身。
//   - write：其余非高危动作。包含两类容易被忽略的成员——本地可逆写入（改设置、
//     建卡券组）与平台触达（刷新、同步、发布、发送、测试连通性）。它们共同点是
//     有对外副作用或消耗额度，但能靠后续操作补偿，因此不属于 destructive。
//   - account：改动账号登录态、运行实例、平台资料或账号自身生命周期。
//   - destructive：删除、清空、整表覆盖或中止进行中的动作，无法靠后续操作恢复。
//
// 定级只描述语义，不改变任何既有行为：MCP 工具是否需要同步 confirm 仍由
// ToolDef.Destructive 决定，本表加入前后那 39 个工具与名称保持不变。
func PlatformToolRisks() (*ToolRiskTable, error) {
	return NewToolRiskTable(
		// 服务自检。
		ToolRiskSpec{Name: "system_ping", Risk: RiskRead},
		ToolRiskSpec{Name: "system_version", Risk: RiskRead},
		ToolRiskSpec{Name: "auth_whoami", Risk: RiskRead},

		// 账号域：摘要与查询。
		ToolRiskSpec{Name: "account_list_all", Risk: RiskRead},
		ToolRiskSpec{Name: "account_list", Risk: RiskRead},
		ToolRiskSpec{Name: "account_get", Risk: RiskRead},
		ToolRiskSpec{Name: "account_get_pause", Risk: RiskRead},
		ToolRiskSpec{Name: "account_get_long_login", Risk: RiskRead},
		ToolRiskSpec{Name: "account_runtime_status", Risk: RiskRead},
		ToolRiskSpec{Name: "account_task_get_settings", Risk: RiskRead},
		ToolRiskSpec{Name: "account_task_list_runs", Risk: RiskRead},
		// 账号域：本地可逆配置写入。
		ToolRiskSpec{Name: "account_set_remark", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_set_status", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_set_pause", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_set_auto_confirm", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_set_auto_consign", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_set_auto_bargain", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_update_settings", Risk: RiskWrite},
		ToolRiskSpec{Name: "account_task_update_settings", Risk: RiskWrite},
		// 账号域：登录态、运行实例、平台资料与账号生命周期。
		ToolRiskSpec{Name: "account_set_long_login", Risk: RiskAccount},
		ToolRiskSpec{Name: "account_restart", Risk: RiskAccount},
		ToolRiskSpec{Name: "account_refresh_profile", Risk: RiskAccount},
		ToolRiskSpec{Name: "account_delete", Risk: RiskAccount},
		ToolRiskSpec{Name: "account_task_run", Risk: RiskAccount},

		// 订单与履约：查询与分析。
		ToolRiskSpec{Name: "order_list", Risk: RiskRead},
		ToolRiskSpec{Name: "order_get", Risk: RiskRead},
		ToolRiskSpec{Name: "order_refresh_job_get", Risk: RiskRead},
		ToolRiskSpec{Name: "analytics_dashboard", Risk: RiskRead},
		ToolRiskSpec{Name: "analytics_orders", Risk: RiskRead},
		ToolRiskSpec{Name: "analytics_valid_orders", Risk: RiskRead},
		ToolRiskSpec{Name: "issue_list", Risk: RiskRead},
		// 订单与履约：平台刷新、手动发货与异常处理。
		ToolRiskSpec{Name: "order_refresh_single", Risk: RiskWrite},
		ToolRiskSpec{Name: "order_refresh", Risk: RiskWrite},
		ToolRiskSpec{Name: "order_manual_ship", Risk: RiskWrite},
		ToolRiskSpec{Name: "order_refresh_job_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "order_refresh_job_cancel", Risk: RiskWrite},
		ToolRiskSpec{Name: "issue_resolve_run", Risk: RiskWrite},
		ToolRiskSpec{Name: "issue_resolve_deferred", Risk: RiskWrite},

		// 商品：货架查询与预检。
		ToolRiskSpec{Name: "item_list", Risk: RiskRead},
		ToolRiskSpec{Name: "item_get", Risk: RiskRead},
		ToolRiskSpec{Name: "item_batch_preview", Risk: RiskRead},
		ToolRiskSpec{Name: "item_category_recommend", Risk: RiskRead},
		ToolRiskSpec{Name: "item_batch_list", Risk: RiskRead},
		ToolRiskSpec{Name: "item_batch_get", Risk: RiskRead},
		ToolRiskSpec{Name: "item_batch_result_csv", Risk: RiskRead},
		// 商品：本地写入、平台同步与发布；批次中止与重试可再补偿，不是不可逆动作。
		ToolRiskSpec{Name: "item_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_update", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_set_multi_spec", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_set_multi_quantity", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_sync_all", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_sync_page", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_publish_single", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_batch_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_batch_start", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_batch_cancel", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_batch_retry_failed", Risk: RiskWrite},
		// 商品：删除货架条目与整批记录。
		ToolRiskSpec{Name: "item_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "item_batch_delete", Risk: RiskDestructive},

		// 卡券库存：查询。
		ToolRiskSpec{Name: "card_list", Risk: RiskRead},
		ToolRiskSpec{Name: "card_get", Risk: RiskRead},
		// 卡券库存：建组、改组与追加卡密；连通性测试会带已存配置发起真实外呼。
		ToolRiskSpec{Name: "card_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "card_update", Risk: RiskWrite},
		ToolRiskSpec{Name: "card_append_data", Risk: RiskWrite},
		ToolRiskSpec{Name: "card_test_api", Risk: RiskWrite},
		// 卡券库存：删除整组会连带丢失库存正文。
		ToolRiskSpec{Name: "card_delete", Risk: RiskDestructive},

		// 自动化配置：规则、模板、默认回复、关键词与指定商品回复的查询。
		ToolRiskSpec{Name: "rule_list", Risk: RiskRead},
		ToolRiskSpec{Name: "rule_list_all", Risk: RiskRead},
		ToolRiskSpec{Name: "rule_trigger_counts", Risk: RiskRead},
		ToolRiskSpec{Name: "rule_preview", Risk: RiskRead},
		ToolRiskSpec{Name: "template_list", Risk: RiskRead},
		ToolRiskSpec{Name: "template_get", Risk: RiskRead},
		ToolRiskSpec{Name: "default_reply_list", Risk: RiskRead},
		ToolRiskSpec{Name: "default_reply_get", Risk: RiskRead},
		ToolRiskSpec{Name: "keyword_list", Risk: RiskRead},
		ToolRiskSpec{Name: "item_reply_list", Risk: RiskRead},
		ToolRiskSpec{Name: "item_reply_get", Risk: RiskRead},
		// 自动化配置：新增与单条修改，均可再改回。
		ToolRiskSpec{Name: "rule_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "rule_update", Risk: RiskWrite},
		ToolRiskSpec{Name: "template_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "template_update", Risk: RiskWrite},
		ToolRiskSpec{Name: "default_reply_set", Risk: RiskWrite},
		ToolRiskSpec{Name: "keyword_add", Risk: RiskWrite},
		ToolRiskSpec{Name: "keyword_update", Risk: RiskWrite},
		ToolRiskSpec{Name: "item_reply_set", Risk: RiskWrite},
		// 自动化配置：删除与清空。
		ToolRiskSpec{Name: "rule_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "template_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "default_reply_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "default_reply_clear_records", Risk: RiskDestructive},
		ToolRiskSpec{Name: "keyword_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "keyword_delete_by_index", Risk: RiskDestructive},
		ToolRiskSpec{Name: "item_reply_delete", Risk: RiskDestructive},
		// 整表替换会丢掉被覆盖的旧条目，旧值不可再取回，因此比逐条删除更高一档。
		ToolRiskSpec{Name: "keyword_replace", Risk: RiskDestructive},

		// 聊天：本地已落库内容的查询。
		ToolRiskSpec{Name: "chat_session_list", Risk: RiskRead},
		ToolRiskSpec{Name: "chat_message_list", Risk: RiskRead},
		ToolRiskSpec{Name: "chat_quick_reply_list", Risk: RiskRead},
		ToolRiskSpec{Name: "chat_buyer_note_get", Risk: RiskRead},
		ToolRiskSpec{Name: "chat_item_list", Risk: RiskRead},
		// 聊天：平台拉取、发送与已读上报。已读上报是尽力而为的幂等写，不改变业务数据。
		ToolRiskSpec{Name: "chat_session_refresh_contacts", Risk: RiskWrite},
		ToolRiskSpec{Name: "chat_history_refresh", Risk: RiskWrite},
		ToolRiskSpec{Name: "chat_send_text", Risk: RiskWrite},
		ToolRiskSpec{Name: "chat_send_image", Risk: RiskWrite},
		ToolRiskSpec{Name: "chat_mark_read", Risk: RiskWrite},
		ToolRiskSpec{Name: "chat_quick_reply_add", Risk: RiskWrite},
		ToolRiskSpec{Name: "chat_buyer_note_set", Risk: RiskWrite},
		// 聊天：隐藏会话会连带清空其展示消息。
		ToolRiskSpec{Name: "chat_session_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "chat_quick_reply_delete", Risk: RiskDestructive},

		// 通知：渠道、绑定与不确定通知的查询。
		ToolRiskSpec{Name: "notification_channel_list", Risk: RiskRead},
		ToolRiskSpec{Name: "notification_channel_get", Risk: RiskRead},
		ToolRiskSpec{Name: "notification_binding_list", Risk: RiskRead},
		ToolRiskSpec{Name: "notification_binding_get", Risk: RiskRead},
		ToolRiskSpec{Name: "notification_uncertain_list", Risk: RiskRead},
		ToolRiskSpec{Name: "notification_uncertain_list_all", Risk: RiskRead},
		// 通知：渠道与绑定的写入；测试发送会向真实渠道投递一条消息。
		ToolRiskSpec{Name: "notification_channel_create", Risk: RiskWrite},
		ToolRiskSpec{Name: "notification_channel_update", Risk: RiskWrite},
		ToolRiskSpec{Name: "notification_channel_test", Risk: RiskWrite},
		ToolRiskSpec{Name: "notification_binding_set", Risk: RiskWrite},
		ToolRiskSpec{Name: "notification_binding_toggle", Risk: RiskWrite},
		// 通知：删除渠道与绑定；清空账号绑定一次移除全部关联。
		ToolRiskSpec{Name: "notification_channel_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "notification_binding_delete", Risk: RiskDestructive},
		ToolRiskSpec{Name: "notification_binding_clear_account", Risk: RiskDestructive},

		// 系统设置与 AI：查询。
		ToolRiskSpec{Name: "settings_get_system", Risk: RiskRead},
		ToolRiskSpec{Name: "settings_user_list", Risk: RiskRead},
		ToolRiskSpec{Name: "settings_user_get", Risk: RiskRead},
		ToolRiskSpec{Name: "ai_reply_list", Risk: RiskRead},
		ToolRiskSpec{Name: "ai_reply_get", Risk: RiskRead},
		// 模型目录只读远端清单，幂等且不消耗额度。
		ToolRiskSpec{Name: "ai_models_list", Risk: RiskRead},
		// 设置写入；敏感键仍由 settings 应用服务走三态命令与审计，等级不因此升高。
		ToolRiskSpec{Name: "settings_update_system", Risk: RiskWrite},
		ToolRiskSpec{Name: "settings_set_system", Risk: RiskWrite},
		ToolRiskSpec{Name: "settings_user_set", Risk: RiskWrite},
		ToolRiskSpec{Name: "ai_reply_set", Risk: RiskWrite},
		// 连通性测试会带已存密钥发起一次真实对话，消耗额度。
		ToolRiskSpec{Name: "ai_test_connection", Risk: RiskWrite},

		// 管理员域：全局只读视图。
		ToolRiskSpec{Name: "admin_stats", Risk: RiskRead},
		ToolRiskSpec{Name: "admin_user_list", Risk: RiskRead},
		ToolRiskSpec{Name: "admin_background_tasks", Risk: RiskRead},
		ToolRiskSpec{Name: "mcp_audit_list", Risk: RiskRead},
		// 管理员域：删除用户。
		ToolRiskSpec{Name: "admin_user_delete", Risk: RiskDestructive},
	)
}
