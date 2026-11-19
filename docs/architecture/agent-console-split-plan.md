# Agent 控制台拆分与运营 Agent 对话模块设计

> 状态：**已发布（v1.0.42-local-20261120）**。
> 本文定义「AI 设置 / Agent 中心」两页重拆为「系统设置 + 客服 Agent + 运营 Agent」的信息架构、接口与数据设计；不定义重构阶段，不替代 `docs/architecture/refactoring-master-plan.md`。

## 1. 背景

现状三处重叠：

- 「AI 设置」承载模型与凭据、AI 总闸、接管范围与出站 MCP 工具；「Agent 中心」承载对外 MCP 服务卡、客服档位与运营指引入口。
- 对外 MCP 服务被放进 Agent 叙事，但它不是 Agent；出站 `mcp.servers` 与入站 `/mcp` 同名不同向。
- 运营 Agent 只有 QQ 入站只读命令，没有会话落库，也没有本地 Web 对话入口。

## 2. 目标信息架构

| 模块 | 路由 | 内容 |
|---|---|---|
| 系统设置 | `/app/settings` | 原基础/安全/滑块/凭据 + 对外 MCP 服务（入站，即时保存） |
| 客服 Agent | `/app/customer-agent` | AI 模型与凭据、AI 总闸、接管范围、出站 MCP 工具、能力档位、AI 接管过的会话只读回放 |
| 运营 Agent | `/app/ops-agent` | QQ 入站开关与白名单 + QQ 会话列表/消息/Web 人工发送 |

删除 `/app/ai-settings`、`/app/agent-center`，不保留重定向（本地管理端）。

## 3. 关键决策

| 决策 | 选择 | 依据 |
|---|---|---|
| 共享 AI 引擎归属 | 并入客服 Agent 页 | 当前唯一实际消费方是客服 Agent；运营对话未开发，页面内标注「共用」 |
| 客服接管历史标记 | `ai_conversations` 的 assistant 轮次 | `ai_decision_log` 受 30 天保留清理，不能作为长期历史；`ai_conversations` 不被清理 |
| 客服历史形态 | 会话级只读回放 + 接管轮次计数 | `chat_messages` 无生成来源列，无法可靠逐条标注 AI/人工 |
| 运营 Agent 设置 | 只迁 `qqbot.commands_enabled` / `qqbot.command_openids` | AppID/AppSecret 与通知渠道共用，连接器页保持唯一编辑入口 |
| 运营会话数据 | 新表 `ops_conversations` / `ops_messages` | QQ 现状无任何会话落库；新表可按通道扩展但不做抽象 |
| 入站记录时机 | 网关解析后立即记录，命令回复成功后记录出站 | 命令失败也保留入站；入站表用于回放，不参与命令执行 |
| Web 发送顺序 | 先调 QQ 发送，成功后再落库 | 避免本地出现平台未送达的假消息 |
| 实时性 | 手动刷新 + 发送后增量更新 | 首版不引入轮询/WebSocket |
| 页面归属 | 继续放 `features/settings` | 跨 feature 引用被架构门禁禁止；搬迁整套设置传输层不划算 |

## 4. 数据模型

```sql
ops_conversations(
  id, channel, target_openid, is_group,
  last_message_at, last_message_preview, created_at, updated_at,
  UNIQUE(channel, target_openid)
)
ops_messages(
  id, conversation_id, platform_msg_id, direction, source, content, created_at,
  FOREIGN KEY(conversation_id) REFERENCES ops_conversations(id) ON DELETE CASCADE
)
-- messages(conversation_id, id DESC)
-- UNIQUE(conversation_id, platform_msg_id) WHERE platform_msg_id <> ''
```

- 时间统一 Unix 毫秒，与聊天域一致。
- `direction`：`inbound` / `outbound`；`source`：`inbound` / `command` / `operator`。
- `platform_msg_id` 用于 WS 重连重投去重；重复入站不重复记录出站。
- 纳入 `CleanupRetention`：先删超期消息，再删无消息且超期的会话。

## 5. 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/agent/support/conversations` | 客服接管会话分页（`last_message_at` + `chat_id` 游标） |
| GET | `/api/v1/agent/support/conversations/messages` | 指定账号会话的消息（复用聊天归属校验） |
| GET | `/api/v1/agent/ops/conversations` | 运营会话分页（`page/page_size`） |
| GET | `/api/v1/agent/ops/conversations/{id}/messages` | 运营消息分页（`before_id`） |
| POST | `/api/v1/agent/ops/conversations/{id}/messages` | Web 人工发送，成功 201 返回消息与会话摘要 |

- 客服历史接口归任意已登录会话，数据按账号归属校验。
- 运营接口归管理员（`RequireAdmin`），响应只含 openid 与正文，不含任何凭据。
- 运营发送错误映射：空白/超长 400、会话不存在 404、凭据未配置 409、平台发送失败 502。
- 文本上限 1500 字符，与 QQ 命令回复共用同一余量常量。

## 6. 运行链路

- 入站：`qqbot.Gateway.handleEvent` 解析消息 → 记录端口写 `ops_messages(inbound)` → 命令服务 → 被动回复成功 → 记录 `ops_messages(outbound, source=command)`。
- Web 发送：HTTP → `internal/application/opschat` → QQ 发送端口（botgo C2C/群）→ 成功后写 `ops_messages(outbound, source=operator)` 并更新会话摘要。
- 网关启动仍由 `qqbot.commands_enabled` 门控；关闭即不连接、不记录。

## 7. 前端落点

- `features/settings/pages/CustomerAgent.tsx`：AI 引擎 + 接管范围 + 出站 MCP 工具 + 档位 + `AgentTakeoverHistory`。
- `features/settings/pages/OpsAgent.tsx`：QQ 入站开关/白名单 + `OpsConversationPanel`。
- `features/settings/components/AgentTakeoverHistory.tsx`：只读会话列表与消息回放。
- `features/settings/components/OpsConversationPanel.tsx`：两栏会话 UI、Enter 发送、字数上限、凭据未就绪时禁用输入。
- `features/settings/opsHooks.ts`：列表/消息/发送，各自 `AbortController + 代次`，发送单飞。
- `Settings.tsx` 回收 `MCPServiceCard`；`connectors/registry.ts` 移除 QQ 入站能力声明。

## 8. 非目标

- 不做 LLM 自动运营回复。
- 不做多通道注册与抽象（仅保留 `channel` 列，首版只写 `qq`）。
- 不做未读计数、在线状态、正在输入、撤回、搜索、新建会话。
- 不做图片/文件/语音发送。
- 不新增聊天消息的 AI/人工来源列。
- 不做运营对话实时推送。

## 9. 风险

- `ai_conversations` 计数是「AI 生成过回复」的近似值，不能等同于决策层接管次数。
- QQ 主动消息有平台配额与窗口限制，Web 发送失败只能在界面如实报错。
- OpenAPI 重新生成会带来较大生成文件 diff。
- 分片预算需按生产构建实测回填。
