# QQ 机器人通知渠道 需求与技术设计（Spec）

> 状态：**设计草案，待评审**；实现已交付**草稿基线**（本地门禁全绿，尚未经真实平台联调）。
> 本文定义 QQ 通知渠道的需求与技术设计，不定义重构阶段，不替代 `docs/architecture/refactoring-master-plan.md`。
> 实现进度以实际代码、测试与本文"实现记录"章节为准。

## 1. 背景与目标

### 1.1 背景

现状（已核实的代码事实）：

- 通知渠道类型在数据库层已放行 `qq`：SQLite/Postgres 的 `CHECK (type IN (... ,'qq', ...))` 显式包含，MySQL 无 CHECK 约束。
- 但发送层是显式占位：`internal/notify/notifier_channels.go` 的 `send` 对 `case "qq"` 直接返回 `qq 渠道暂不支持`。
- 仓库无任何 QQ 机器人 SDK 依赖，无 QQ 发送实现。
- 存在一个与通知渠道无关、且**无消费实现**的系统设置 `qq_reply_secret_key`（历史遗留，不在本需求范围）。

### 1.2 目标

1. 让应用能把既有通知事件（掉线、安全验证、需要人工处理、业务静默等）推送到 QQ 机器人。
2. 发送目标为 **QQ 单聊（user_openid）优先**，同时兼容 **群聊（group_openid）**。
3. 采用 **QQ 开放平台官方机器人（bot.qq.com）+ 官方 Go SDK `botgo`**。
4. 出站走 **QQ 机器人 api-v2（HTTPS OpenAPI）**，与事件推送链路解耦，不依赖长连接。
5. 为未来的"QQ 消息驱动应用内 MCP tools 执行货架操作"预留清晰的接入边界（本文只定边界，不含实现）。

### 1.3 非目标

- 本次**不实现入站指令处理**（不接收 QQ 消息、不接 MCP、不做指令解析）。
- 本次**不实现 webhook 回调服务端**（入站链路的方向已确定，另立任务）。
- 不修改既有其它渠道行为，不重构 outbox 重试/幂等机制。
- 不引入跨服务架构，仍在单进程内闭环。
- 不清理历史遗留的 `qq_reply_secret_key`（除非评审要求）。

## 2. 关键决策与依据

| 决策 | 选择 | 依据（官方文档事实） |
|---|---|---|
| 目标场景 | 群聊 / 单聊 | QQ 机器人可加入群聊或与用户单聊；非旧版"频道/子频道"模型 |
| 接入方式 | api-v2 HTTPS OpenAPI | 统一域名 `https://api.bot.qq.com`，鉴权 `Authorization: QQBot {ACCESS_TOKEN}` |
| 鉴权 | AppID + AppSecret 换 Access Token | 官方明确 **Token 已弃用**，改用 Access Token；需自动刷新 |
| SDK | `github.com/tencent-connect/botgo` v0.2.1 | 官方 Go SDK；`PostC2CMessage` / `PostGroupMessage` 支持 api-v2 群/单聊 |
| 目标标识 | `user_openid` / `group_openid` | openid 与 AppID 绑定，不同机器人拿到的 openid 不同 |
| 事件链路 | **本次不启用** | 官方 README 声明 WS 事件链路"逐步下线、不再维护"，新方向是 webhook 回调 |

### 2.1 已知平台约束（设计必须承接）

- **主动消息限额**：不带 `msg_id` 的主动推送有严格额度限制——**同一用户或同一群每月仅 4 条主动消息**；且主动消息需审核。这意味着 QQ 渠道**不适合承载高频告警**，只适合少量关键通知。
- **被动回复窗口**：被动消息（带 `msg_id`/`event_id` 的回复）不受上述月额度限制，但窗口有限——**单聊 60 分钟、群聊 5 分钟**；本次只做出站主动推送，不消费该窗口。
- **频率**：同一目标每秒发送条数受限。
- **OpenAPI 失败语义**：错误以 `err_code` 判断，禁止依赖 `message` 文案。
- **目标 openid 不可跨 AppID 复用**：渠道配置必须与具体机器人 AppID 成对。
- **官方通道 vs 协议端**：用户印象中「扫码配置」的实现（NapCat / Lagrange / NoneBot / AstrBot 等）走的是 **OneBot v11 协议端**，本质是拿真实 QQ 账号扫码登录做反向协议，属非官方通道、有封号风险；**OpenClaw 与本项目一致，走官方 botgo 通道**（AppID + AppSecret），配置动作是「去 QQ 开放平台创建机器人」而非「扫码登录账号」。

## 3. 技术设计

### 3.1 架构落点

发送逻辑落在既有通知服务的路由点，不新增层级：

```
internal/notify/notifier_channels.go   send() switch 增加 case "qq"
internal/notify/notifier_qq.go         新增：QQ 发送实现（本次新增文件）
internal/notify/notifier.go            Notifier 增加 QQ 客户端缓存字段
```

依据 AGENTS.md：`internal/notify` 是通知应用服务，渠道实现属其内聚职责；不触碰 `internal/server`、`internal/db` 结构。

### 3.2 配置字段（channel.config，落库加密）

| 字段 | 必填 | 说明 |
|---|---|---|
| `app_id` | 是 | QQ 机器人 AppID |
| `app_secret` | 是 | QQ 机器人 AppSecret（换取 Access Token） |
| `user_openid` | 条件 | 单聊目标；与 `group_openid` 至少填一个 |
| `group_openid` | 条件 | 群聊目标 |

目标选择规则：**`user_openid` 非空则优先单聊**，否则发群聊；两者皆空返回配置错误。

**凭据两级来源**（后补）：`app_id` / `app_secret` 优先取渠道配置，任一缺失时按渠道所有者回退读取**系统级 QQ 连接器设置** `qqbot.app_id` / `qqbot.app_secret`（见 §3.7）。多渠道共用同一机器人时，只需在系统设置里配一次。

> 注：不使用已弃用的 `token` 字段；不引入 `sandbox` 开关（如后续需要可加 `use_sandbox`，本期不做）。

### 3.3 发送流程

```
send(channel) → case "qq" → sendQQ(ctx, cfg, message, channelUserID)
  ├─ 解析凭据：优先取渠道配置 app_id/app_secret，缺失时回退系统连接器设置
  │  （qqbot.app_id / qqbot.app_secret，按 channelUserID 读取）
  ├─ 校验 app_id/app_secret 非空，否则配置错误（提示到渠道或系统设置补充）
  ├─ 校验 user_openid 或 group_openid 至少一个非空
  ├─ 取/建按 app_id 缓存的 botgo 客户端（含 Access Token 自动刷新）
  ├─ Context 有界超时（15s）包裹单次发送
  ├─ user_openid 非空 → PostC2CMessage(ctx, user_openid, MessageToCreate{Content, MsgType: TextMsg})
  └─ 否则            → PostGroupMessage(ctx, group_openid, 同消息体)
```

- 消息体：`dto.MessageToCreate{Content: message, MsgType: dto.TextMsg}`（官方 `msg_type=0` 纯文本）。
- 失败分类：配置错误（缺字段）→ 立即失败；平台/网络错误 → 返回 error 交 outbox 统一重试。

### 3.4 客户端缓存与并发

- `Notifier` 新增 `qqMu sync.Mutex` 与 `qqClients map[string]qqBotClient`，**按 AppID 缓存**，避免每次通知重建 Access Token 刷新链路。
- 并发约束：锁只保护 map 读写；**构造客户端在锁外完成**，禁止持锁执行网络 I/O（对齐 AGENTS.md 1.6）。
- 并发首次构造只保留最先登记的客户端，不覆盖已存在的可用客户端。
- Access Token 刷新协程归属：**继承 Notifier 进程生命周期上下文**（`lifecycleCtx`，由 `Start(ctx)` 注入），随进程关闭取消；未调用 `Start` 的场景回退到按实例惰性缓存的有限预算上下文。详见 7.7 实现调整说明。
- 失败降级：构造失败不缓存，下次发送重试构造。

### 3.5 可测试性设计

- 定义窄接口 `qqBotClient`（`SendC2CMessage` / `SendGroupMessage`），生产实现为 `botgoQQClient`。
- 工厂 `newQQBotClient` 为包级变量，可在测试中替换为替身，**测试不触网**。
- 聚焦测试覆盖：目标路由（单聊优先/群聊/皆空报错）、凭据缺失、缓存命中与并发构造、发送错误透传、`send()` 路由分发。

### 3.6 契约与前端

- **OpenAPI**：`NotificationChannelResponse/Create/Patch` 的 `type` 为自由 string，**无需改 schema**。
- **应用层校验**：`internal/application/notifications/channels.go` 当前只校验 name/type 非空；QQ 字段完整性校验放在**发送时**（渠道配置可能后填），可选在创建时对 qq 型做软校验。
- **前端**：`frontend/app/features/notifications/models.ts` 的 `NotificationChannelType` 联合类型加 `'qq'`；`state.ts` 的 `notificationChannelTypes` 增加 qq 元数据（字段/图标/引导）。无硬编码枚举限制。
- **MCP 工具描述**：`internal/mcp/tools_notifications.go` 已把 `qq` 作为示例，无需改。

### 3.7 系统级 QQ 连接器与设置页模块（后补）

凭据两级来源要求在系统设置页提供一处连接器配置，避免每条渠道重复填 AppID/AppSecret：

| 设置键 | 敏感 | 说明 |
|---|---|---|
| `qqbot.app_id` | 否 | 机器人 AppID，明文返回，可在页面直接回显与编辑 |
| `qqbot.app_secret` | **是** | 机器人 AppSecret，服务端只回显 `qqbot.app_secret_configured` 标记，写入必须走 secrets 三态命令 |
| `qqbot.app_secret_configured` | 否（派生） | 由 `internal/db.SensitiveSettingKeys()` 驱动派生，前端列入 `SETTINGS_SAVE_OMIT_KEYS`，不参与提交 |

落点：

- **后端**：`internal/db/secrets.go` 把 `qqbot.app_secret` 加入敏感键白名单（`isSensitiveSettingKey` + `SensitiveSettingKeys()`）；设置应用服务与 HTTP 层是泛型实现，无需额外改动即可获得「脱敏读取 + 三态写入 + 访问审计」。
- **前端**：`frontend/shared/api-contract/settings.ts` 的 `SENSITIVE_SYSTEM_SETTING_KEYS` 同步新增该键（前端镜像）；`frontend/app/features/settings/constants.ts` 的 `SYSTEM_SETTING_KEYS` 新增两个可保存键、`SETTINGS_SAVE_OMIT_KEYS` 新增派生标记；`QQConnectorCard.tsx` 提供引导：前往 QQ 开放平台 `q.qq.com` 扫码登录 → 创建机器人 → 复制 AppID/AppSecret，并在卡片内明示主动推送额度与 openid 绑定约束。
- **读取路径**：`Notifier.settingValue` 经 `settingsRepository` 按渠道所有者读取，未装配仓储或键缺失时回退空串，最终由 `sendQQ` 的校验统一报错。

## 4. 未来扩展边界（本次不实现，仅定方向）

用户最终目标：QQ 机器人承接远程指令，经应用内 Agent 调用 MCP tools 执行货架操作。相关事实：

- 应用**已内建完整 MCP 工具层**（`internal/mcp`，`/mcp` 端点），是唯一现成的"外部指令驱动应用能力"入口。
- 不存在 agent 包、命令总线、通用外部指令入口。
- 入站方向定为 **webhook 回调**（官方推荐、长期方向），需公网可达入口或内网穿透。

**预留边界**：

1. QQ 渠道实现只做**出站**，不引入 botgo 的 WS 事件订阅，避免绑定将下线的链路。
2. 入站将另立任务，复用 `/mcp` 作为执行入口，鉴权与安全单独设计（MCP 固定管理员身份，需额外的指令来源鉴权与白名单）。

## 5. 验收标准

- `send()` 路由 `qq` 到 `sendQQ`，移除占位报错。
- 单聊/群聊目标选择正确；缺字段返回明确配置错误（不含明文凭据）。
- 同一 AppID 复用客户端；并发安全，无数据竞争（`-race` 通过）。
- 新增聚焦测试覆盖上述分支，且不触网。
- `go build ./...`、`go vet`、`commentlint`、`architecturecheck` 无新增违规；受影响包测试通过。
- 前端 qq 渠道可创建、可编辑、可测试发送。
- 系统设置页「连接器」区块可配置 QQ 机器人 AppID/AppSecret；AppSecret 只写不读，页面仅展示「已配置」标记。
- 渠道未填凭据时能回退使用系统连接器凭据；渠道已填时以渠道配置为准。

## 6. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| 主动消息每日限额 | 高频通知被平台拒绝 | 依赖 outbox 重试 + 频控；必要时后续加目标级限流 |
| 目标 openid 与 AppID 绑定 | 换机器人后配置失效 | 配置说明中明示，错误回显引导重配 |
| botgo 依赖较重（resty/websocket/oauth2） | 构建变慢、依赖面扩大 | 只用于出站；如评审认为过重，可改为直接调 api-v2 HTTPS |
| Access Token 刷新协程生命周期 | 进程退出回收 | 归属进程级，随进程回收；不随渠道删除回收（可接受） |
| 入站链路（未来）需公网入口 | 私域部署不可达 | 另立任务评估内网穿透/反向代理 |

## 7. 实现记录

> 本节记录 QQ 通知渠道的落地情况：改动文件清单、测试覆盖、门禁证据、覆盖率与例外、示例配置与真实平台验证结论。
> 当前为**草稿基线**：代码、测试与前端接线已完成并通过本地门禁，但**未经真实 QQ 机器人凭据与平台联调验证**。

### 7.1 改动文件清单

| 文件 | 性质 | 说明 |
|---|---|---|
| `internal/notify/notifier_qq.go` | 新增 | QQ 发送实现：`qqBotClient` 窄接口、工厂 `newQQBotClient`、单聊/群聊发送、按 AppID 缓存、15s 单次发送超时、令牌刷新根 Context 解析 |
| `internal/notify/notifier_channels.go` | 修改 | `send()` 的 `case "qq"` 由占位报错改为带界超时的 `n.sendQQ(ctx, cfg, message, ch.UserID)` |
| `internal/notify/notifier.go` | 修改 | `Notifier` 新增 `lifecycleCtx`、`qqMu`、`qqClients`、`qqRefreshCtx`、`qqRefreshCancel`；`Start()` 绑定进程生命周期上下文 |
| `internal/notify/notifier_test.go` | 修改 | 路由测试断言由「qq 暂不支持」更新为「qq 缺配置应报错」 |
| `internal/notify/notifier_qq_test.go` | 新增 | QQ 发送聚焦测试，共 10 个用例（见 7.2） |
| `internal/db/secrets.go` | 修改 | `qqbot.app_secret` 加入敏感设置键白名单（`isSensitiveSettingKey` / `SensitiveSettingKeys()`） |
| `internal/mcp/tools_settings.go` | 修改 | 文件头红线与 `settings_get_system` 工具描述同步新增 `qqbot.app_secret` |
| `frontend/app/features/notifications/models.ts` | 修改 | `NotificationChannelType` 联合类型新增 `'qq'` |
| `frontend/app/features/notifications/state.ts` | 修改 | `notificationChannelTypes` 新增 qq 渠道元数据（字段/图标/配置指南） |
| `frontend/shared/api-contract/settings.ts` | 修改 | `SENSITIVE_SYSTEM_SETTING_KEYS` 新增 `qqbot.app_secret`（后端白名单的前端镜像） |
| `frontend/app/features/settings/constants.ts` | 修改 | `SYSTEM_SETTING_KEYS` 新增 `qqbot.app_id`/`qqbot.app_secret`；`SETTINGS_SAVE_OMIT_KEYS` 新增 `qqbot.app_secret_configured` |
| `frontend/app/features/settings/models.ts` | 修改 | `SystemSettings` 新增三个 QQ 连接器字段 |
| `frontend/app/features/settings/components/QQConnectorCard.tsx` | 新增 | 设置页 QQ 连接器配置卡：开放平台引导、AppID/AppSecret 输入、额度与 openid 约束提示 |
| `frontend/app/features/settings/components/QQConnectorCard.test.tsx` | 新增 | 连接器卡组件测试，5 个用例 |
| `frontend/app/features/settings/pages/Settings.tsx` | 修改 | 左栏新增「连接器」区块并挂载 `QQConnectorCard` |
| `frontend/app/features/settings/state.test.ts` | 修改 | 新增 QQ 连接器凭据保存与派生标记剔除用例 |
| `frontend/app/features/apiAdapters.test.ts` | 修改 | 敏感分流用例覆盖 `qqbot.app_secret`，并新增 QQ 连接器明文/密文分流用例 |
| `frontend/routing.test.ts` | 修改 | 新增「设置页暴露 QQ 连接器引导」源码契约用例 |
| `go.mod`、`go.sum` | 修改 | 引入 `github.com/tencent-connect/botgo v0.2.1` |

### 7.2 测试覆盖

聚焦测试 `internal/notify/notifier_qq_test.go` 覆盖 §3.5 要求的所有分支，全部注入替身、**不触网**：

| 用例 | 覆盖分支 |
|---|---|
| `TestSendQQ_SingleChatPriority` | 单聊优先：`user_openid` 与 `group_openid` 并存时只走单聊 |
| `TestSendQQ_GroupFallback` | 仅配置群聊目标时降级为群聊发送 |
| `TestSendQQ_BothOpenIDEmpty` | 两个目标皆空返回配置错误，且**不构造客户端** |
| `TestSendQQ_MissingCredentials` | 缺 `app_id`/`app_secret` 返回配置错误，且**不构造客户端** |
| `TestSendQQ_SendErrorWrapped` | 发送错误经中文包装后透交 outbox 重试分类 |
| `TestSendQQ_ClientCacheByAppID` | 同一 AppID 复用客户端，构造仅发生一次 |
| `TestSendQQ_ConcurrentConstruct` | 20 并发构造：缓存不损坏、`-race` 无竞争、发送次数完整 |
| `TestRouteByChannelType_QQ` | `send()` 把 `qq` 类型路由到 `sendQQ` 并成功发送 |
| `TestSendQQ_FallsBackToSystemConnector` | 渠道配置缺凭据时回退读取系统连接器设置 `qqbot.app_id`/`qqbot.app_secret` |
| `TestSendQQ_ChannelConfigOverridesSystemConnector` | 渠道配置与系统连接器并存时**渠道配置优先**（`capturedID == "ch_id"`） |

前端新增测试：`QQConnectorCard.test.tsx` 5 个用例（未配置引导与开放平台链接、AppID/AppSecret 写回 `qqbot.` 前缀键、已配置时只回显 AppID 且不回显密文、AppSecret 默认掩码可切换明文、仅有 AppID 时仍未就绪并展示额度说明），全部 jsdom 渲染、不触网。

### 7.3 门禁证据

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过（EXIT=0，botgo v0.2.1 依赖已解析） |
| `go vet ./internal/...` | 通过，无告警 |
| `go test -race -count=1 ./internal/notify/...` | 通过（约 73s） |
| `go test ./internal/db/... ./internal/mcp/... ./internal/application/settings/... ./internal/adapter/... ./internal/server/...` | 通过 |
| `go run ./tools/commentlint -mode check -root .` | 通过（无缺少或模板化中文注释） |
| `go run ./tools/architecturecheck` | 通过 |
| `npm --prefix frontend run typecheck` | 通过 |
| `npm --prefix frontend run comments:check` | 通过 |
| `npm --prefix frontend test` | 通过（103 文件 / 646 用例） |

### 7.4 覆盖率与未覆盖例外

- `internal/notify` 包语句覆盖率 **91.7%**（`go test -coverprofile`）。
- QQ 相关函数：`sendQQ` 92.3%、`qqClient` 95.0%、`refreshContext` 87.5%。
- **例外**：`newBotgoQQClient` 为 **0%**。该函数会真实调用 `botgo.NewOpenAPI` 与 `StartRefreshAccessToken` 建立 Access Token 刷新链路，属「仅外部环境/真实平台」类不可覆盖分支（AGENTS.md 2.1 允许的三类例外之一）；测试已通过工厂替身隔离，避免触网。

### 7.5 示例配置（channel.config，落库加密）

```json
{
  "app_id": "你的机器人 AppID",
  "app_secret": "你的机器人 AppSecret",
  "user_openid": "单聊目标 openid（可选，优先）",
  "group_openid": "群聊目标 openid（可选）"
}
```

前端「QQ 机器人」渠道表单即按上述四个字段渲染，`app_secret` 以 `password` 类型掩码展示，编辑态不回显明文。

### 7.6 真实平台验证

**未完成**。当前仅有本地替身测试与门禁证据，尚未用真实机器人凭据向 QQ 单聊/群聊实际投递消息。

上线前需补充：① 真实 AppID/AppSecret 与 openid 的一次端到端发送；② 主动消息每日限额与频率限制的实际表现（见 §6 风险表）；③ 关停进程时 Access Token 刷新协程随 `lifecycleCtx` 回收的确认。

### 7.7 相对设计草案的实现调整

- **令牌刷新根 Context 改为继承进程生命周期**：原草案写为以裸 `context.Background()` 启动 `StartRefreshAccessToken`，违反架构门禁规则 `checkUnboundedRootContexts`（禁止后台组件把 `Background`/`TODO` 直接作为异步链根 Context）。实现改为在 `Notifier.Start(ctx)` 记录 `lifecycleCtx`，构造 QQ 客户端时优先继承它；未调用 `Start` 的场景回退到按实例惰性缓存的有限预算上下文（`qqRefreshBudget = 24h`，根 Context 必须是 `Background` 且被 `WithTimeout` 包裹），并保证取消句柄归属 Notifier 实例、**不被丢弃**（同时满足 `go vet` lostcancel 检查）。
- **`qqMu` 保护范围扩展**：除 `qqClients` 外，也保护惰性创建的兜底刷新上下文；已在 `notifier.go` 字段注释中写明锁归属与「持锁期间禁止网络/平台 I/O」约束（对齐 AGENTS.md 1.6）。
- **`sendQQ` 签名扩展为带所有者与有界上下文**：为支持系统连接器回退，签名由 `sendQQ(cfg, message)` 改为 `sendQQ(ctx, cfg, message, channelUserID)`；`notifier_channels.go` 的 `case "qq"` 与文件内其余渠道一致，用 `legacyNotifierOperationTimeout` 包裹出有界上下文再下发，避免发送链无超时。
- **凭据解析抽成 `qqCredential`**：`app_id`/`app_secret` 的「渠道配置优先 + 系统连接器回退」逻辑集中在单一辅助函数，便于测试替身注入设置值，也避免 `sendQQ` 内重复分支。
- **敏感键白名单需前后端同步**：`qqbot.app_secret` 必须同时进入后端 `internal/db/secrets.go` 与前端 `shared/api-contract/settings.ts` 的 `SENSITIVE_SYSTEM_SETTING_KEYS`，否则前端会把密文塞进 `values` 被后端以「敏感设置必须通过显式变更命令提交」拒绝。已新增 `internal/db/pure_business_coverage_test.go` 断言（键数量 5 且 `qqbot.app_secret` 为敏感键）防止后端白名单被静默回退。
