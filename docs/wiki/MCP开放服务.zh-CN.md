# MCP 开放服务 使用手册（中文）

中文

本页说明如何把本服务的运营能力通过 MCP（Model Context Protocol）开放给本机的 AI Harness，以及相应的安全边界。

## 功能边界

- 本服务把管理员的**账号、订单、商品、卡密、自动化配置、聊天、通知、AI 与系统设置、管理员全局**能力以 MCP Streamable HTTP 协议开放。
- 接入方始终以**固定管理员身份**执行，等价于管理员在 Web 端操作；不区分调用方用户。
- 端点固定为 `http://<host>:59188/mcp`，与 Web 界面共用同一端口，**不新开监听端口**。
- 未启用或无有效令牌时，`/mcp` 对外返回 404，不暴露端点存在性。
- 不可变业务规则（砍价免拼与自动发货顺序、结果不确定转 `needs_review` 且不自动重放）对 MCP 调用同样生效。

以下能力在 MCP 中**永不提供**：Cookie/Token/密码/加密 metadata 的读写、二维码与密码登录、会话登录登出、管理员初始化与改密、明文卡密与完整卡密数据读取、滑块验证码与浏览器参数设置、商品/订单导入的停用兼容入口。

## 接入步骤

1. 进入「系统与AI → MCP 服务」，打开「启用 MCP 服务」。
2. 点击「生成令牌」，**立即复制**弹出的一次性明文；刷新或关闭页面后无法再次查看。
3. 复制页面上的 Harness 配置示例（见下）。
4. 在 Harness 中粘贴配置并重启，随后即可用 `tools/list` 看到全部工具。

推荐的 Harness 配置示例（`Authorization` 使用上一步复制的令牌）：

```json
{
  "mcpServers": {
    "ydisks-xianyu-helper": {
      "url": "http://127.0.0.1:59188/mcp",
      "headers": { "Authorization": "Bearer <生成的令牌>" }
    }
  }
}
```

若 Harness 运行在其它机器上，需要先打开「允许非本机访问」并把地址换成宿主机的可达地址。

## 工具清单（共 136 个）

说明列使用三种标注：

- **只读**：纯查询，不修改任何状态。
- **只读（本地写入）**：只写本地配置或本地数据，不触达闲鱼平台，因此不需要 `confirm`。
- **破坏性**：会触达平台或产生不可逆结果，必须显式传入 `confirm=true`；未传时在调用任何应用用例之前即被拒绝。

### 服务自检（3）

| 工具 | 说明 |
| --- | --- |
| `system_ping` | 只读；检查协议链路与鉴权是否正常 |
| `system_version` | 只读；返回应用与 MCP 服务版本 |
| `auth_whoami` | 只读；返回本次调用的管理员身份与令牌来源 |

### 账号域（21）

| 工具 | 说明 |
| --- | --- |
| `account_list_all` | 只读；管理员全局账号摘要（含归属用户） |
| `account_list` | 只读；当前管理员归属账号列表 |
| `account_get` | 只读；单个归属账号详情 |
| `account_set_remark` | 只读（本地写入）；更新账号备注 |
| `account_set_status` | 只读（本地写入）；启用或暂停账号运行开关 |
| `account_set_pause` | 只读（本地写入）；设置暂停时长 |
| `account_get_pause` | 只读；读取暂停状态 |
| `account_set_auto_confirm` | 只读（本地写入）；自动确认发货开关 |
| `account_set_auto_consign` | 只读（本地写入）；自动转已发货开关 |
| `account_set_auto_bargain` | 只读（本地写入）；砍价自动免拼开关 |
| `account_update_settings` | 只读（本地写入）；综合设置 |
| `account_get_long_login` | 只读；平台长登录状态 |
| `account_set_long_login` | 破坏性；向平台开启/关闭长登录 |
| `account_runtime_status` | 只读；全部账号运行时状态 |
| `account_restart` | 破坏性；重启账号运行实例 |
| `account_refresh_profile` | 破坏性；向平台刷新昵称与头像 |
| `account_delete` | 破坏性；删除账号 |
| `account_task_get_settings` | 只读；自动评价、擦亮与每日定时下架配置 |
| `account_task_update_settings` | 只读（本地写入）；更新任务配置，可设置下架开关、下架时间与下架商品白名单 |
| `account_task_list_runs` | 只读；任务运行记录 |
| `account_task_run` | 破坏性；立即执行自动评价/擦亮/定时下架（`task_type=delist` 会真实下架白名单商品，且不可自动恢复） |

### 订单与履约（14）

| 工具 | 说明 |
| --- | --- |
| `order_list` | 只读；分页查询订单 |
| `order_get` | 只读；订单详情 |
| `order_refresh_single` | 破坏性；向平台刷新单个订单 |
| `order_refresh` | 破坏性；批量刷新订单 |
| `order_refresh_job_create` | 只读（本地写入）；创建后台刷新任务 |
| `order_refresh_job_get` | 只读；查询刷新任务状态 |
| `order_refresh_job_cancel` | 只读（本地写入）；取消刷新任务 |
| `order_manual_ship` | 破坏性；手动完整发货 |
| `analytics_dashboard` | 只读；仪表盘统计 |
| `analytics_orders` | 只读；订单分析 |
| `analytics_valid_orders` | 只读；有效订单分析 |
| `issue_list` | 只读；自动化异常与延期异常列表 |
| `issue_resolve_run` | 破坏性；处理自动化运行异常 |
| `issue_resolve_deferred` | 破坏性；处理死信延期任务 |

### 商品货架与批量发布（20）

| 工具 | 说明 |
| --- | --- |
| `item_list` | 只读；本地商品列表 |
| `item_get` | 只读；商品详情 |
| `item_create` | 只读（本地写入）；新建本地商品 |
| `item_update` | 只读（本地写入）；编辑本地商品 |
| `item_delete` | 破坏性；删除本地商品 |
| `item_set_multi_spec` | 只读（本地写入）；多规格发货标记 |
| `item_set_multi_quantity` | 只读（本地写入）；多数量发货标记 |
| `item_sync_all` | 破坏性；全量平台同步 |
| `item_sync_page` | 破坏性；分页平台同步 |
| `item_category_recommend` | 只读；发布类目推荐 |
| `item_publish_single` | 破坏性；单商品发布 |
| `item_batch_preview` | 只读；批量发布预检 |
| `item_batch_create` | 只读（本地写入）；创建发布批次 |
| `item_batch_start` | 破坏性；启动批量发布 |
| `item_batch_list` | 只读；批次列表 |
| `item_batch_get` | 只读；批次详情 |
| `item_batch_cancel` | 破坏性；取消批次 |
| `item_batch_retry_failed` | 破坏性；重试失败行 |
| `item_batch_delete` | 破坏性；删除批次 |
| `item_batch_result_csv` | 只读；批次结果 CSV |

### 卡密库存（7）

| 工具 | 说明 |
| --- | --- |
| `card_list` | 只读；卡券组列表（仅库存计数） |
| `card_get` | 只读；卡券组详情（不含明文卡密） |
| `card_create` | 只读（本地写入）；新建卡券组 |
| `card_update` | 只读（本地写入）；更新卡券组 |
| `card_delete` | 破坏性；删除卡券组 |
| `card_append_data` | 只读（本地写入，只追加不回读）；追加卡密数据 |
| `card_test_api` | 破坏性；API 发卡配置连通性测试 |

### 自动化配置（27）

| 工具 | 说明 |
| --- | --- |
| `rule_list` | 只读；规则列表 |
| `rule_list_all` | 只读；全量规则列表 |
| `rule_trigger_counts` | 只读；触发类型计数 |
| `rule_preview` | 只读；规则 dry-run 预览 |
| `rule_create` | 只读（本地写入）；新建规则 |
| `rule_update` | 只读（本地写入）；编辑规则 |
| `rule_delete` | 破坏性；删除规则 |
| `template_list` | 只读；发货模板列表 |
| `template_get` | 只读；发货模板详情 |
| `template_create` | 只读（本地写入）；新建模板 |
| `template_update` | 只读（本地写入）；更新模板 |
| `template_delete` | 破坏性；删除模板 |
| `default_reply_list` | 只读；默认回复列表 |
| `default_reply_get` | 只读；默认回复详情 |
| `default_reply_set` | 只读（本地写入）；写入默认回复 |
| `default_reply_delete` | 破坏性；删除默认回复 |
| `default_reply_clear_records` | 破坏性；清除发送记录 |
| `keyword_list` | 只读；关键词列表 |
| `keyword_add` | 只读（本地写入）；新增关键词 |
| `keyword_replace` | 破坏性；批量替换关键词 |
| `keyword_update` | 只读（本地写入）；更新关键词 |
| `keyword_delete` | 破坏性；删除关键词 |
| `keyword_delete_by_index` | 破坏性；按序号删除关键词 |
| `item_reply_list` | 只读；指定商品回复列表 |
| `item_reply_get` | 只读；指定商品回复详情 |
| `item_reply_set` | 只读（本地写入）；写入指定商品回复 |
| `item_reply_delete` | 破坏性；删除指定商品回复 |

### 聊天域（14）

| 工具 | 说明 |
| --- | --- |
| `chat_session_list` | 只读；会话分页 |
| `chat_session_refresh_contacts` | 破坏性；向平台刷新联系人 |
| `chat_history_refresh` | 破坏性；向平台拉取历史消息 |
| `chat_message_list` | 只读；库内消息查询 |
| `chat_send_text` | 破坏性；发送文本消息 |
| `chat_send_image` | 破坏性；发送图片（URL 或 base64） |
| `chat_mark_read` | 只读（本地写入）；标记已读 |
| `chat_session_delete` | 破坏性；删除会话 |
| `chat_quick_reply_list` | 只读；快捷回复列表 |
| `chat_quick_reply_add` | 只读（本地写入）；新增快捷回复 |
| `chat_quick_reply_delete` | 破坏性；删除快捷回复 |
| `chat_buyer_note_get` | 只读；买家备注 |
| `chat_buyer_note_set` | 只读（本地写入）；写入买家备注 |
| `chat_item_list` | 只读；商品卡查询 |

### 通知域（14）

| 工具 | 说明 |
| --- | --- |
| `notification_channel_list` | 只读；渠道列表（脱敏） |
| `notification_channel_get` | 只读；渠道编辑态（脱敏） |
| `notification_channel_create` | 只读（本地写入）；新建渠道 |
| `notification_channel_update` | 只读（本地写入）；更新渠道 |
| `notification_channel_delete` | 破坏性；删除渠道 |
| `notification_channel_test` | 破坏性；测试发送 |
| `notification_binding_list` | 只读；账号渠道绑定 |
| `notification_binding_get` | 只读；单账号绑定 |
| `notification_binding_set` | 只读（本地写入）；整组设置绑定 |
| `notification_binding_toggle` | 只读（本地写入）；单条切换绑定 |
| `notification_binding_delete` | 破坏性；删除绑定 |
| `notification_binding_clear_account` | 破坏性；清空账号绑定 |
| `notification_uncertain_list` | 只读；不确定通知（用户视图） |
| `notification_uncertain_list_all` | 只读；不确定通知（管理员视图） |

### AI 与系统设置（11）

| 工具 | 说明 |
| --- | --- |
| `settings_get_system` | 只读；系统设置（敏感值只返回是否已配置） |
| `settings_update_system` | 只读（本地写入）；批量更新系统设置 |
| `settings_set_system` | 只读（本地写入）；写入单个系统设置键 |
| `settings_user_list` | 只读；用户设置列表 |
| `settings_user_get` | 只读；单个用户设置 |
| `settings_user_set` | 只读（本地写入）；写入用户设置 |
| `ai_reply_list` | 只读；账号 AI 回复设置列表 |
| `ai_reply_get` | 只读；单个账号 AI 回复设置 |
| `ai_reply_set` | 只读（本地写入）；写入账号 AI 回复设置 |
| `ai_models_list` | 只读；可用模型列表 |
| `ai_test_connection` | 只读；AI 连通性测试 |

### 管理员域（5）

| 工具 | 说明 |
| --- | --- |
| `admin_stats` | 只读；全局统计 |
| `admin_user_list` | 只读；用户列表（脱敏） |
| `admin_user_delete` | 破坏性；删除用户 |
| `admin_background_tasks` | 只读；后台任务总览 |
| `mcp_audit_list` | 只读；MCP 调用审计分页查询 |

## 资源与提示

只读资源（`resources/list` 与 `resources/read`）：

| URI | 内容 |
| --- | --- |
| `ydisks://health` | 服务与 MCP 启用状态 |
| `ydisks://dashboard` | 仪表盘统计 |
| `ydisks://accounts` | 账号摘要列表 |
| `ydisks://accounts/{account_id}` | 单个账号摘要 |
| `ydisks://orders{?status,account_id,search,page,page_size}` | 订单分页（支持过滤） |
| `ydisks://automation/issues` | 自动化异常列表 |
| `ydisks://cards/low-stock{?threshold}` | 低库存卡券组 |

内置提示（`prompts/list` 与 `prompts/get`）：`daily_inspection`（每日巡检）、`resolve_needs_review_orders`（处理不确定订单）、`onboard_account_automation`（新账号自动化开通向导）、`restock_low_stock_cards`（卡密补货）、`ai_config_health_check`（AI 配置体检）。提示只产出中文剧本与建议调用顺序，不代替管理员确认执行破坏性动作。

## 安全模型

- **本机优先**：默认只允许回环地址访问；「允许非本机访问」打开后，局域网内任何持有令牌的调用方都能执行全部管理能力，请只在可信网络开启。
- **令牌**：使用 crypto/rand 生成 256 bit 熵，持久化只保存 SHA-256 哈希；轮换后旧令牌保留 24 小时宽限；吊销立即生效。令牌明文只在生成/轮换响应出现一次，状态与审计接口永不回显。
- **鉴权与限流**：`/mcp` 使用 Bearer 令牌常量时间比对；同一来源鉴权失败会被计数并短暂封禁。
- **审计**：每次工具/资源/提示调用写入 `mcp_call_audit`，参数按白名单键级脱敏并截断，可用于追溯“谁在什么时间对哪个账号执行了什么动作”。
- **禁能力面**：不提供凭证读写、登录流程、改密、明文卡密、验证码与浏览器参数等能力；密码与密钥类设置只能写入、不能读回。

## Docker / 非本机访问

- 容器默认通过 `127.0.0.1:59188` 暴露；跨机访问时需要把端口映射到宿主机可达地址，并打开「允许非本机访问」。
- 建议只在内网或经反向代理并附加额外鉴权后暴露；不要直接映射到公网。
- 无人工初始化（无头部署）时，可用环境变量 `XIANYU_MCP_TOKEN` 提供引导令牌，跳过页面生成步骤。

## 环境变量与数据表

| 名称 | 说明 |
| --- | --- |
| `XIANYU_MCP_TOKEN` | 可选的 MCP 引导令牌，仅从进程环境读取，不落库、不写日志；删除后需改用页面生成的持久化令牌 |
| `mcp.server.enabled` | 系统设置键：MCP 服务启用状态 |
| `mcp.server.allow_non_loopback` | 系统设置键：是否放行非本机来源 |
| `mcp_tokens` | 00057 迁移建立；只存令牌 SHA-256 哈希与创建/使用/失效时间 |
| `mcp_call_audit` | 00057 迁移建立；调用审计记录，参数已键级脱敏 |

启用状态与网络策略变更在下一次请求立即生效，无需重启服务。
