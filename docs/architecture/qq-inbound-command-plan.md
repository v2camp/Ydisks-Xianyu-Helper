# QQ 机器人入站命令 需求与技术设计（Spec）

> 状态：设计草案，待评审；实现进行中。
> 上游文档：`docs/architecture/qq-notification-channel-plan.md`（出站通知渠道，已交付草稿基线）。

## 1. 背景与目标

### 1.1 背景

QQ 通知渠道（出站）已落地：管理员可在系统设置配置 QQ 机器人连接器，通知渠道选「QQ 机器人」即可推送告警。

但该通道是**单向**的——管理员只能被动接收推送，不能主动问系统要信息。实际运维里最常见的三个诉求需要「人在外面、随手问一句」：

- **销量**：今天卖了多少、按账号拆开看。
- **健康**：系统是否正常（网络、账号、交易）。
- **会话**：机器人最近跟买家聊了什么，回复是否合理。

这三个诉求的共同点：**低频、只读、结果短、需要即时反馈**。它们不适合做成仪表盘（要打开网页），也不适合做成推送（要预先定义触发条件）。

### 1.2 目标

允许管理员（及被授权者）在 QQ 里给机器人发一条文本命令，机器人以文本回复结果。本期只做三个只读命令 + 帮助：

| 命令 | 别名 | 输出 |
|---|---|---|
| `销量` | `今日销量` | 当日按账号分组的成交单数与金额，附合计 |
| `健康` | `健康度` | 数据库、账号在线、交易异常、业务静默四个维度的健康快照 |
| `会话` | `会话审查` | 按账号分组的近期会话片段（含买卖双方发言），用于审查机器人回复 |
| `帮助` | `help`、`？`、`?` | 命令清单 |

### 1.3 非目标

- **不执行任何写操作**：不发货、不改价、不启停账号、不改设置。本期命令**全部只读**。
- **不做自然语言理解**：只做关键词精确匹配（含别名），不匹配时回帮助。不接 LLM。
- **不做入站指令驱动 MCP**：这是 §4 的远期方向，本期不引入命令总线或 Agent。
- **不做 Webhook 入站**：本期只交付 WebSocket 网关，Webhook 作为可切换抽象预留（见 §2）。

## 2. 关键决策与依据

### 2.1 入站链路：选 WebSocket，不因公网门槛牺牲可用性

| 决策 | 选择 | 依据（官方文档事实） |
|---|---|---|
| 入站链路 | **WebSocket 网关** | 官方《QQ Bot 介绍与接入指南》原文：「WebSocket：不依赖公网服务器部署，**适合个人单机使用**……如果你是想用于连接 OpenClaw 等类似 AI Agent 服务……选用 WebSocket 即可」 |
| Webhook | 本期不做，预留接口 | 官方要求「开发者需要提供一个 **HTTPS 回调地址**」，且「回调地址允许配置的端口号为：80、443、8080、8443」；自部署私域场景普遍无公网地址与备案域名 → **物理不可用，非工程取舍** |
| 回复通道 | **被动回复**（带 `msg_id`） | `dto.MessageToCreate.MsgID` 非空即被动消息；主动消息每月每用户/每群仅 4 条，命令场景必然撞配额 |
| SDK | 沿用 `botgo` v0.2.1 | 与出站同源，`session_manager` + `event.RegisterHandlers` 完整可用 |

**为什么 Webhook 不是「更稳」而是「不可选」**：Webhook 要求平台服务器能主动 POST 到一个公网 HTTPS 地址。本项目是跑在用户自己机器上的闲鱼助手，典型环境是家用宽带 / 内网，既无固定公网 IP 也无备案域名。官方自己也把 WS 定位为「不依赖公网服务器部署，适合个人单机使用」。

> 风险对冲：WS 链路官方已声明「逐步下线、不再维护」（botgo README 原文）。因此入站层做成**窄接口**，WS 与 Webhook 是两个可替换实现；将来用户具备公网条件时，只换传输实现，业务命令层一行不改。

### 2.2 一条被推翻的旧结论（重要）

上一轮调研曾记入风险：「官方 wiki 要求发送消息时机器人需连接 WebSocket 保持在线」。

**核实后该结论不成立于本场景**。该句出自《消息收发概述》的 **「主动消息 → 文字子频道」** 小节，以及《发送子频道消息》——**仅限频道子频道场景**。本项目使用的两个 v2 接口页：

- 《发送单聊消息》`POST /v2/users/{user_openid}/messages`：注意事项只有「被动消息有效时间 60 分钟，每个消息最多回复 4 次」+ 主动频控。
- 《发送群聊消息》`POST /v2/groups/{group_openid}/messages`：注意事项只有「被动消息有效时间 5 分钟，每个消息最多回复 5 次」+ 主动频控。

两页**均无「必须 WS 在线」要求**。

**影响**：① 现有出站通知链路在纯 Webhook 模式下也能工作，无需维持 WS；② WS 只服务入站命令，故障域隔离——WS 挂了只影响「问不了」，不影响「收得到告警」。

### 2.3 botgo v0.2.1 的一个反序列化坑（必须绕开）

`dto.Message` / `dto.User` **没有** `group_openid`、`user_openid`、`member_openid` 字段，但官方 v2 事件体实际返回：

- 群事件 `GROUP_AT_MESSAGE_CREATE`：`d.group_openid`、`d.author.member_openid`
- 单聊事件 `C2C_MESSAGE_CREATE`：`d.author.user_openid`

即 **SDK 会把 openid 静默丢弃**，导致「不知道往哪儿回」。单聊场景下 `author.id` 恰好等于 `user_openid`（巧合，不可依赖）。

**对策**：入站层不直接用 `dto.Message` 的字段，而是从 `payload.RawMessage` 自行反序列化一个最小事件结构，显式取 `group_openid` / `author.user_openid` / `author.member_openid` / `id`（msg_id）。

### 2.4 授权模型：默认关闭 + 显式 openid 白名单

机器人一旦上线，任何能找到它的人都能给它发消息。命令返回的是**经营数据**（销量）和**会话内容**（买家聊天），属敏感信息，不能无鉴权暴露。

| 设置键 | 默认 | 说明 |
|---|---|---|
| `qqbot.commands_enabled` | `false` | 入站命令总开关；关闭时**不启动 WS 网关**，零外部连接 |
| `qqbot.command_openids` | 空 | 允许发命令的 openid 白名单，逗号或换行分隔；**空表示拒绝所有** |

规则：

1. 开关关闭 → 不启动网关，服务静默（不产生任何对外连接，也不报错）。
2. 白名单为空 → 同样拒绝所有发送者，避免「开了开关就裸奔」。
3. 发送者不在白名单 → 被动回复一条固定文案，**并回显其 openid**，方便管理员把该标识加入白名单。openid 与 AppID 绑定且无法用于登录，回显不构成额外风险。

> 远期：若需要更细粒度（按账号隔离、只读子集），应复用既有 userID 数据隔离，而不是在 openid 层发明新权限模型。本期的 openid 白名单等价于「固定管理员身份」，与 MCP 的 `AdminUserID` 同层。

## 3. 技术设计

### 3.1 架构落点

```
internal/qqbot/                    新增：QQ 机器人应用服务（与 internal/notify 平级）
  ├── ports.go                     窄端口：销量/健康/会话/身份/回复
  ├── commands.go                  命令解析 + 三个中文格式化（纯函数）
  ├── service.go                   编排：授权校验 → 命令执行 → 文本产出
  ├── inbound.go                   WS 网关（botgo）+ 事件解析（绕开 2.3 的坑）
  └── *_test.go                    聚焦测试，不触网

internal/composition/runtime/
  └── qqbot_adapter.go             把 analytics/chat/account 服务适配成 qqbot 窄端口
  └── runtime.go                   lifecycle 新增 qqbot 组件
```

分层约束（对齐 AGENTS.md）：

- `internal/qqbot` **不直接依赖** `internal/application/*` 或 `internal/db`，只依赖自己声明的窄端口；适配器在 composition 层完成，与既有 `mcp_*_adapter.go` 风格一致。
- 命令格式化是**纯函数**（入参决定出参），可单测、可快照，不碰时间与随机源（时间由调用方注入）。
- WS 网关是后台组件，根 Context **必须继承** lifecycle 组件 `Start(ctx)` 传入的 ctx（架构门禁 `checkUnboundedRootContexts`）。

### 3.2 窄端口

```go
// SalesSnapshot 是「今日销量」命令所需的账号级销量快照。
type SalesSnapshot struct {
    Accounts []AccountSales  // 按金额降序
    TotalOrders int
    TotalAmount string       // 已格式化的人民币金额
}

// HealthSnapshot 是「健康」命令所需的四维健康快照。
type HealthSnapshot struct {
    DatabaseOK bool
    Accounts   []AccountHealth  // State / Connected / Failures
    PendingIssues int
    SilenceMinutes int64        // -1 表示无业务活动记录
}

// ChatDigest 是「会话」命令所需的分组会话摘要。
type ChatDigest struct {
    Accounts []AccountChats      // 每账号若干近期会话
}

// 端口（全部只读）
type SalesReader interface { TodaySales(ctx, adminUserID int64) (SalesSnapshot, error) }
type HealthReader interface { Snapshot(ctx, adminUserID int64) (HealthSnapshot, error) }
type ChatReader  interface { RecentChats(ctx, adminUserID int64, perAccount int) (ChatDigest, error) }
type AdminResolver interface { AdminUserID(ctx) (int64, error) }
```

数据隔离一律以 `adminUserID` 透传，复用既有 `EXISTS (SELECT 1 FROM cookies WHERE ... user_id = ?)` 约定；**不接受 userID=0**。

### 3.3 数据来源映射

| 命令 | 复用入口 | 备注 |
|---|---|---|
| 销量 | `analytics.Service.ValidOrders` / `OrderAnalytics`，按返回的 `CookieID` 内存分组 | 日期边界用 `analytics.DateBoundary` 转 UTC，时区取服务器本地 |
| 健康·数据库 | `db.Store.HealthProbe().Ping(ctx)` | 既有 `/health` 同源 |
| 健康·账号 | `account.RuntimeService.RuntimeStatuses(ctx)` | State 取值 `online/error/auth_expired/...` |
| 健康·交易 | `automation.IssueRepository.ListIssues(ctx, adminUserID)` | RunIssue + DeferredIssue 计数 |
| 健康·业务静默 | `db.AnalyticsQueries.LatestBusinessActivityAt(ctx)` | 与看门狗同源；阈值复用 `silence_alert_minutes` |
| 会话 | `chat.Service.ListSessions` + `ListStoredMessages` | 按账号分组，每账号取 N 个会话、每会话取 M 条消息 |

### 3.4 命令流程

```
WS 收到事件
  → 解析 RawMessage 取 msg_id / 发送者 openid / 群 openid
  → Service.Handle(ctx, openid, text)
      ├─ 开关关闭或 openid 不在白名单 → 授权拒绝文案（含 openid 提示）
      ├─ AdminResolver 取 adminUserID；失败 → 文案「身份不可用」
      ├─ 解析命令（trim + 精确匹配 + 别名）→ 未知则回帮助
      └─ 执行 → 格式化 → 按 QQ 单条文本上限裁剪
  → 被动回复（MessageToCreate{MsgID: msg_id}）
```

- 裁剪策略：单条上限取 **1500 个 rune**（留足余量），超出时按行截断并追加「…（已截断，共 N 项）」。
- 命令执行有界超时（15s），与出站发送一致。

### 3.5 可测试性设计

- 命令解析、三个格式化函数、裁剪函数全部是**纯函数**，直接单测。
- `Service.Handle` 只依赖窄端口，测试注入内存替身，**不触网、不连库**。
- 事件解析（`RawMessage` → 内部结构）是纯函数，用官方事件体样本做往返测试，覆盖群与单聊两种。
- WS 网关本身不加单测（外部环境类不可覆盖分支，与出站 `newBotgoQQClient` 同类例外）。

## 4. 未来扩展边界（本期不实现，仅定方向）

远期目标：QQ 机器人承接远程指令，经应用内 Agent 调用 MCP tools 执行货架操作。

- 应用**已内建完整 MCP 工具层**（`internal/mcp`，`/mcp` 端点），是唯一现成的「外部指令驱动应用能力」入口。
- 本期只做只读查询，不引入命令总线、不做写操作、不做自然语言解析。
- 若将来要支持写操作，需另立任务解决：指令来源鉴权（openid 层之上要有操作级授权）、写操作确认机制、审计留痕。
- Webhook 入站作为可切换实现预留；业务命令层与传输层解耦后替换成本极低。

## 5. 验收标准

- 三个命令 + 帮助可用，输出为中文结构化文本，超长自动裁剪。
- 开关关闭时不启动 WS 网关、不产生对外连接；白名单为空时拒绝所有发送者。
- 未授权发送者收到明确拒绝文案，且不影响其他人。
- 命令执行只读，不产生任何写库或对外副作用。
- 事件解析覆盖群与单聊两种官方事件体，openid 不丢失。
- `go build ./...`、`go vet`、`commentlint`、`architecturecheck` 无新增违规；受影响包测试通过；前端门禁通过。
- 真实平台联调：以沙箱环境跑通一次「发命令 → 收到回复」（待补）。

## 6. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| WS 链路官方不再维护，可能随时失效 | 入站命令整体不可用 | 传输层做成可替换窄接口；出站通知不依赖 WS（已核实），故障域隔离 |
| botgo 反序列化丢 openid | 不知道往哪儿回 | 从 `RawMessage` 自行解析，加往返测试锁定 |
| 主动消息配额（每月 4 条） | 命令回复发不出去 | 一律走被动回复（带 `msg_id`）；被动窗口群 5 分钟 / 单聊 60 分钟足够 |
| 机器人可被陌生人找到 | 经营数据泄露 | 默认关闭 + openid 白名单；白名单空则全拒 |
| 会话内容含买家个人信息 | 隐私外泄 | 会话命令输出截断买家 ID 与商品标题；仅返回给白名单成员 |
| 命令执行慢拖垮 WS 心跳 | 网关掉线 | 命令执行独立于心跳，且有界超时 15s |

## 7. 实现记录

> 本节记录 QQ 入站命令的落地情况：改动文件清单、测试覆盖、门禁证据与真实平台验证结论。
> 当前为**已上生产、未接通真实机器人**状态：命令服务、入站网关、装配、前端开关与生产部署编排均已完成，
> 并在生产环境用**伪造凭据**验证了「配置 → 加密落库 → 网关接线 → 主动连接 QQ」全链路；
> 因尚无真实 AppID/AppSecret，**尚未完成「发命令 → 收到回复」的真实联调**。

### 7.1 改动文件清单

| 文件 | 性质 | 说明 |
|---|---|---|
| `internal/qqbot/ports.go` | 新增 | 窄端口与只读数据模型：销量/健康/会话快照、身份解析、授权判断 |
| `internal/qqbot/commands.go` | 新增 | 命令解析（`NormalizeInboundText` 剥离 @ 提及）、四个中文渲染函数、金额格式化、长度裁剪 |
| `internal/qqbot/service.go` | 新增 | 命令编排：授权校验 → 解析 → 有界超时读取 → 渲染 → 裁剪 |
| `internal/qqbot/inbound.go` | 新增 | WS 网关、`ParseInboundMessage`（绕开 SDK 丢 openid）、被动回复（带 `msg_id`） |
| `internal/qqbot/commands_test.go` | 新增 | 纯函数测试：别名解析、@ 剥离、三个渲染、金额、裁剪 |
| `internal/qqbot/service_test.go` | 新增 | 服务测试：开关关闭、未授权不下读、三命令透传身份、端口缺失、文案不泄露细节 |
| `internal/qqbot/inbound_test.go` | 新增 | 入站测试：官方事件体往返（群/单聊）、被动回复回传 msg_id、静默/未授权/失败分支 |
| `internal/composition/runtime/qqbot_adapter.go` | 新增 | 适配器：把 analytics/chat/account/automation 适配成窄端口；`addQQBotGatewayComponent` 登记生命周期 |
| `internal/composition/runtime/runtime.go` | 修改 | `BuildRuntime` 增加一行 QQ 网关装配（未启用为空操作） |
| `frontend/app/features/settings/components/QQConnectorCard.tsx` | 修改 | 卡片增加「启用入站命令」开关与 openid 白名单输入 |
| `frontend/app/features/settings/components/QQConnectorCard.test.tsx` | 修改 | 新增 2 个用例：默认关闭、开关往返与条件渲染 |
| `frontend/app/features/settings/constants.ts` | 修改 | `SYSTEM_SETTING_KEYS` 新增 `qqbot.commands_enabled`、`qqbot.command_openids` |
| `frontend/app/features/settings/models.ts` | 修改 | `SystemSettings` 新增两个入站命令字段 |
| `frontend/app/features/settings/state.test.ts` | 修改 | 新增入站命令设置的保存裁剪用例 |
| `frontend/routing.test.ts` | 修改 | 新增「入站命令默认关闭且需白名单授权」源码契约用例 |

**生产验证阶段补充（7.7 节两个生产缺陷的修复）：**

| 文件 | 性质 | 说明 |
|---|---|---|
| `internal/logsafe/logsafe.go` | 修改 | 新增 `quotedSecretPairPattern`，收敛 JSON 风格 `"键":"值"` 凭证对 |
| `internal/logsafe/logsafe_test.go` | 修改 | 新增 `TestTextRedactsQuotedCredentialPairs`：请求体、响应体、`%+v` 结构体三种形态 |
| `internal/logging/botgo.go` | 新增 | botgo Logger 适配器与 `InstallBotgoLogger`：先脱敏再转发，Info 降级为 Debug |
| `internal/logging/botgo_test.go` | 新增 | 4 个用例：AppSecret 请求体、Access Token 响应体、等级路由、空参保护 |
| `internal/composition/runtime/restart_loop.go` | 新增 | `serveWithRestart` 通用退避守护：5s 起、2 倍增长、上限 5min、稳定 60s 后重置 |
| `internal/composition/runtime/restart_loop_test.go` | 新增 | 5 个用例：退避阶梯、稳定后重置、已取消不再启动、缺依赖静默、上限收敛 |
| `internal/composition/runtime/qqbot_adapter.go` | 修改 | 网关由「单次 `Run`」改为「`serveWithRestart` 守护」，退出后自动重连 |
| `cmd/server/main.go` | 修改 | 两处日志器创建点均调用 `InstallBotgoLogger`，覆盖入站与出站两条 QQ 链路 |
| `frontend/app/features/settings/components/QQConnectorCard.tsx` | 修改 | 补充「重启生效 + 自动重连」提示文案 |
| `frontend/app/features/settings/components/QQConnectorCard.test.tsx` | 修改 | 新增 1 个用例断言该提示存在（共 8 个） |
| `frontend/bundleBoundary.test.ts` | 修改 | Settings 分片预算 30KiB → 35KiB（实测 33438 字节，含连接器卡片） |

### 7.2 测试覆盖

命令层与入站层共 **29 个用例**，全部注入替身、**不触网**：

| 用例组 | 覆盖点 |
|---|---|
| `TestParseCommandRecognizesAliases` / `TestParseCommandUnknownInputs` | 四个命令共 17 个别名可识别；空白与无关输入回退未知 |
| `TestNormalizeInboundTextStripsMentions` | 群聊 `<@!bot>` 与 `@bot` 提及前缀被剥离 |
| `TestFormatSales*` | 按账号分行 + 合计；无成交回退空态 |
| `TestFormatHealth*` | 四维渲染；数据库异常与无业务活动两个降级分支 |
| `TestFormatChats*` | 按账号分组；单条发言裁剪；无会话空态 |
| `TestFormatFenRendersYuan` | 分转元，含补位与负号 |
| `TestTruncateReply*` | 超长按行裁剪并追加提示；短文本不变 |
| `TestHandle*` | 开关关闭不下读、未授权不下读、三命令透传管理员身份、未知命令回帮助、身份与读取失败透错、端口缺失不 panic |
| `TestParseInboundMessage*` | 官方单聊/群事件体往返，**锁定 `group_openid` 不丢失**；openid 缺失回退通用标识；三类坏报文报错 |
| `TestRun*` | 单聊回复发送者并回传 msg_id；群场景回复群且剥离 @；开关关闭静默；未授权回显 openid；执行失败回兜底文案 |

**生产缺陷修复新增 11 个用例**（全部注入替身、**不触网**）：

| 用例 | 覆盖点 |
|---|---|
| `TestTextRedactsQuotedCredentialPairs` | JSON 请求体 / 响应体、`%+v` 结构体的令牌字段被脱敏；`clientSecret` 键名保留；`appId`、`expires_in` 等非敏感字段**不被改写**；普通 JSON 原样返回 |
| `TestInstallBotgoLoggerRedactsTokenRequest` | 复刻生产泄漏行，断言 AppSecret 不出现在最终日志，且存在 `<redacted>` 占位 |
| `TestInstallBotgoLoggerRedactsTokenResponse` | `access_token` / `RefreshToken` 两种形态均不泄漏 |
| `TestInstallBotgoLoggerRoutesLevels` | SDK Info 降级为 Debug（默认 info 等级下不输出），Warn 仍输出 |
| `TestInstallBotgoLoggerIgnoresNilLogger` | 传入空日志器时不替换 SDK 全局 logger |
| `TestServeWithRestartRetriesWithBackoff` | 失败 2 次后按 `5s → 10s` 逐次翻倍；ctx 取消即停止；启动次数与退避序列精确断言 |
| `TestServeWithRestartResetsBackoffAfterHealthyRun` | 单次运行超过 60s 稳定阈值后，退避回到初始值而非继续翻倍 |
| `TestServeWithRestartStopsWhenContextAlreadyCancelled` | 进程已在关闭阶段时不再启动组件（启动次数保持 0） |
| `TestServeWithRestartIgnoresMissingDependencies` | 缺 ctx 或运行体时静默返回，不 panic |
| `TestNextRestartDelayCapsAtMaximum` | 翻倍、上限截断、非法零值回落到上限 |
| `QQConnectorCard` 新增用例 | 前端显式告知「重启生效 + 自动重连」，避免误判「保存即已连上」 |

### 7.3 门禁证据

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./...` | 通过，无告警 |
| `go test ./...` | 通过 |
| `go test -race -count=1 ./internal/qqbot/` | 通过 |
| `go run ./tools/architecturecheck` | 通过 |
| `go run ./tools/commentlint -mode check -root .` | 通过 |
| `golangci-lint run`（改动包） | 通过，0 issue |
| `npm --prefix frontend run typecheck` | 通过 |
| `npm --prefix frontend run comments:check` | 通过 |
| `npm --prefix frontend run api:check` + `go run ./tools/apicheck` | 通过 |
| `npm --prefix frontend test` | 通过（103 文件 / **651** 用例） |

### 7.4 覆盖率与未覆盖例外

- `internal/qqbot` 包语句覆盖率 **81.3%**。
- `internal/logsafe` 覆盖率 **100%**，`internal/logging` 覆盖率 **92.2%**。
- 新增的 `restart_loop.go`：`serveWithRestart` **94.7%**、`logRestartAttempt` **100%**、`nextRestartDelay` **100%**（`internal/composition/runtime` 包整体 4.9%，该包以接线胶水代码为主，不单独立项）。
- **例外一**：`newBotgoGatewayClient` 为 **0%**。该函数真实调用 `botgo.NewOpenAPI` 与 `StartRefreshAccessToken` 建立刷新链路，属「仅外部环境/真实平台」类不可覆盖分支；测试已通过 `newGatewayClient` 工厂替身隔离。
- **例外二**：`startInboundSession` 的生产实现（含 `event.RegisterHandlers` 与 `SessionManager.Start`）为 **0%**，同样属外部环境分支；测试通过替换该变量注入事件，完整覆盖了事件处理与回复链路。

### 7.5 真实平台验证

**部分完成**：未接通真实 QQ 机器人，但已在**全新生产环境**（`docker compose` + PostgreSQL 17，commit `266b475`）完成可自动化部分的端到端验证。

已验证项：

| 验证项 | 结果 |
|---|---|
| 服务启动与数据库迁移 | `/health` = `{"status":"ok","database":"ok","commit":"266b475"}`；goose 迁移至版本 57 |
| 前端产物 | 首页 HTTP 200，加载重建后的 `index-Ud3-XcAc.js` |
| 设置写入 | `PUT /api/v1/settings/system` 写入 `qqbot.app_id` / `qqbot.commands_enabled` / `qqbot.command_openids` + `secrets.qqbot.app_secret(action=replace)` → `{"success":true}` |
| 敏感值不外泄 | `GET` 只返回 `qqbot.app_secret_configured=true`，响应体中**不含明文** |
| 落库加密 | 直查数据库得 `qqbot.app_secret \| 67 \| enc:v1:MTJL2GZM5UgO7e5Zl`，确认为 AES-256-GCM 密文 |
| 启用门禁 | 开关打开且凭据齐备后重启，网关**确实被接线**并主动连接 QQ；开关关闭时不产生任何对外连接 |

未完成项（阻塞于缺少真实凭据）：

- ① 未跑通「在 QQ 里发命令 → 收到回复」，被动回复不撞主动配额的结论仍为**设计推断**；
- ② 未确认 WS 网关在真实账号下可连接（官方已声明不再维护）；
- ③ 未确认群 @ 场景下 `group_openid` 的实际字段与本文假设一致（仅由官方事件体样本锁定）。

**生产环境另外暴露了两个缺陷，已在 7.7 节修复。** 验证期间写入的伪造凭据（`qqbot.app_id=102012345`、`qqbot.app_secret=test-secret-value`）属测试数据，上线真实凭据前必须清除。

### 7.6 相对设计草案的实现调整

- **授权端口从「服务内字段」改为独立 `Authorizer` 端口**：原草案把开关与白名单作为 Service 的配置项，实现改为端口注入，使授权判断可随系统设置动态变化（每次事件都重新读库），也让「未授权不下读数据」可被单测直接断言。
- **`Service.Handle` 的未授权分支改为返回错误而非文案**：让传输层决定「回不回复」，从而支持「开关关闭时完全静默」这一安全语义；文案由 `FormatUnauthorized` 单独渲染。
- **装配期禁止裸 `Background`**：`NewQQBotGateway` 需要读系统设置，架构门禁 `checkUnboundedRootContexts` 不允许后台组件以裸 `Background` 为根，故把进程生命周期上下文作为首个参数显式传入。
- **`BuildRuntime` 的 QQ 装配抽成 `addQQBotGatewayComponent`**：直接在 `BuildRuntime` 内联会让该函数触发架构门禁的「函数过大或分支过多」上限（187 行 / 复杂度 30），抽函数后 `BuildRuntime` 只增加一行调用。
- **敏感凭据走 `Store.ReadSensitiveSetting` 而非 `settings.GetSystem`**：后者是脱敏视图，敏感键只返回 `_configured` 标记拿不到明文；入站网关复用出站连接器凭据时必须走带审计的敏感读取路径。

### 7.7 生产环境暴露并修复的两个缺陷

#### 缺陷一：botgo SDK 明文打印 AppSecret

**现象**（生产容器 stdout 实测）：

```
[Debug] 2026-10-08 01:43:53 token_source.go:123:getNewToken
retrieve access token URL:https://bots.qq.com/app/getAppAccessToken
req:{"appId":"102012345","clientSecret":"test-secret-value"}
```

**根因**：`botgo` v0.2.1 在 `token/token_source.go:123` 用 `log.Debugf("retrieve access token URL:%v req:%v", url, string(data))` 原样打印换取令牌的请求体，`data` 是 `{AppID, ClientSecret}` 的 JSON。同类风险点还有 `:147` 的令牌响应体与 `:180` 的 `%+v` 结构体打印。业务侧无法关闭该调用点。

**为什么原有的脱敏链路没挡住**：`logsafe.sensitiveValuePattern` 要求键名后**紧跟**冒号，而 JSON 形态是 `"clientSecret":"…"`，键名与冒号之间隔着引号，正则无法命中。这是本次新增 `quotedSecretPairPattern` 的直接原因。

**修复**（两层，互为兜底）：

1. `internal/logsafe` 新增 `quotedSecretPairPattern`：匹配 `"[…token|secret|password|credential…]"\s*:\s*"…"`，在 `Text` 与 `ExternalError` 中统一收敛。键名保留、只替换值，便于定位来源；非敏感字段（如 `appId`、`expires_in`）不受影响。
2. `internal/logging` 新增 `InstallBotgoLogger`：把 SDK 的全局 `log.DefaultLogger` 换成先整体脱敏、再转发到项目 slog 的适配器。同时把 SDK 的 **Info 级降级为 Debug**——SDK 在 Info 级会打印**完整收发报文**（含用户消息正文与令牌字段），默认等级下不应进入生产日志。

**接线位置**：`cmd/server/main.go` 两处创建日志器的地方（启动期 + 数据库日志格式覆盖后）。进程级只装一次，入站网关与出站通知共用同一 SDK，因此两条链路同时受保护。

#### 缺陷二：网关退出后不再重连

**现象**：伪造凭据导致 `QQ 入站网关退出 err="启动 QQ 入站 Access Token 刷新失败: strconv.ParseInt: parsing \"\": invalid syntax"`。退出后**再没有任何重启动作**，入站命令在进程重启前永久失效。

**根因**：`addQQBotGatewayComponent` 的 `StartFunc` 只起了一个 goroutine 调一次 `gateway.Run(ctx)`，`Run` 返回即结束，没有任何守护。

**修复**：新增 `internal/composition/runtime/restart_loop.go` 的 `serveWithRestart`，把「启动一次」改为「持续守护」：

| 参数 | 取值 | 理由 |
|---|---|---|
| 首次重连间隔 | 5s | 短于常见网络抖动恢复时间，避免用户感知到断档 |
| 退避策略 | 2 倍增长 | 同一组件无并发，纯指数退避足够 |
| 间隔上限 | 5min | 长期失败时每分钟一次的无效请求也不可接受，但要比「永不重试」好 |
| 稳定阈值 | 60s | 已稳定运行过的组件再次断开，多半是新一轮网络事件，退避应从头发起 |

守护循环在 `ctx` 取消时立即退出，不引入新的关停路径；`restartSleep` / `restartNow` 抽象为包级变量以便测试注入可控时间。

**已知边界**：网关凭据在**装配期读取一次**，运行中在设置页修改凭据仍需重启进程才生效。这是「默认零外部连接」（未启用时不登记任何组件、不与 QQ 建立连接）这一设计取舍的代价，取舍结果已通过前端提示文案显式告知用户。
