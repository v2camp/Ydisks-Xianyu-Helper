# Agent 能力内核与配置归拢方案

本文记录三条 Agent 消费方（外部 Harness、运营 Agent、客服 Agent）的架构决策、配置分层与实施路径。
适用范围：新增 Agent 能力、改动 `internal/mcp` / `internal/qqbot` 包边界、新增 Agent 相关设置界面。

---

## 1. 背景与决策摘要

平台存在**三个** Agent 消费方，共用同一套平台能力：

| 消费方 | 本平台扮演 | 驱动来源 | 通道 | 出错代价 |
|---|---|---|---|---|
| 外部 Harness | 能力提供方（MCP Server） | 外部 LLM，人在回路 | HTTPS `/mcp` | 操作者本人可见，可随时中断 |
| 运营 Agent | 能力提供方 + 决策方 | 平台自身（QQ 指令扩充而来） | QQ WebSocket | 管理员授权，但**无同步确认通道** |
| 客服 Agent | 决策方 + 对外 MCP 消费方 | 平台自身 | 进程内 | 资损、客诉、平台封号 |

**决策一（分层）**：共享 L0 能力内核，L1 执行/传输编排与 L2 运行时策略各自独立。
判断标准——**共享「能力是什么」，不共享「谁决定调用它」**。

**决策二（策略入口）**：能力内核是**唯一策略求值入口**，MCP 只是它的一个传输出口，不是内部 Agent 的调用方式。
内核只回答「是否允许、需要何种确认」，**不执行能力**；执行由消费方在取得决策后调用应用层用例完成。

**决策三（配置）**：配置按「谁的答案唯一」分三层，不按 Agent 类型分。

**决策四（暴露粒度）**：客服端对 UI 只暴露**档位**，不暴露工具清单。

---

## 2. 现状盘点（实测）

| 项 | 实测结果 |
|---|---|
| `internal/mcp/` | 26 个 `tools_*.go`，非测试约 15k 行 |
| 已有能力资产 | `registry.go`（ArgSpec 声明式 schema + 破坏性自动补 `confirm`）、`confirm.go`、`gate.go`（loopback/Bearer/限流/启用门）、`ports.go`、`domain_ports.go`（15 个领域契约）、`errors.go`（7 个稳定 `ErrorClass`，含 `needs_review`） |
| `internal/qqbot/` | 1764 行（含测试），7 文件；窄端口只读（销量/健康/会话），**已上生产但未接真实机器人** |
| QQ 入站既有规划 | `docs/architecture/qq-inbound-command-plan.md` §4 已明确远期方向：「经应用内 Agent 调用 MCP tools 执行货架操作」 |
| 客服端现状 | `internal/engine/ai.go` 473 行，单次 chat completions；无 loop、无工具、无权限模型；直连 `*db.Store`，与 AGENTS.md §1.1 存在张力 |
| 全局设置载体 | `user_settings(user_id, key, value)` 字符串 KV |
| 账号级 AI 载体 | `ai_reply_settings`（主键 `cookie_id`），含 `ai_enabled`、`model_name`、`api_key`、`base_url`、业务护栏字段 |
| MCP 持久化 | 迁移 `00057`：`mcp_tokens`（仅存 SHA-256 哈希）、`mcp_call_audit` |
| 前端入口（现状分散） | `settings/pages/AISettings.tsx`、`accounts/components/AccountAISettingsModal.tsx`、`settings/components/MCPServiceCard.tsx`、`settings/components/QQConnectorCard.tsx` |

现状问题：入口已分散四处，若不归拢，新增 Agent 配置后将进一步碎片化。

---

## 3. 调用路径：为什么 MCP 不是内部 Agent 的调用方式

### 3.1 三个候选路径

| 路径 | 做法 | 评价 |
|---|---|---|
| A. 内部 Agent 走 MCP 协议 | 用 `mcp-go` v1.1.1 的 `client.NewInProcessClient(server)` 进程内直连 | 可行，但代价大（见 3.2） |
| B. 共享能力内核，MCP 仅为传输出口 | 三方都先向 `capability` 求值；MCP 端点负责把内核投影成 MCP tools，执行由消费方完成 | **采纳** |
| C. 各自独立实现 | 三套工具目录 | 已否决：能力漂移 + 15k 行资产复制 |

### 3.2 为什么否决路径 A

`mcp-go` 确实提供 `client.NewInProcessClient(*server.MCPServer)`（走 channel transport，无网络开销），技术上可用。但存在三个实质摩擦：

1. **Guard 不生效**。`gate.go` 的 Bearer / loopback / 限流是 `http.Handler` 中间件，进程内 client 绕过 HTTP 层，**鉴权链路为空**。
2. **身份注入缺失**。`CallIdentity` 由 Guard 写入 context；绕过 Guard 后 `IdentityFromContext` 返回 nil，工具实现会失败或行为异常。
3. **要消除上述两点，必须把身份与策略从 HTTP 层下沉到能力层** —— 而这正是路径 B。绕一圈仍要回到 B，且额外背上 JSON-RPC 编解码与 `map[string]any` 松散参数。

**结论**：路径 A 想达到的效果，路径 B 直接达成；路径 A 的额外成本（序列化、类型丢失、Guard 不适用）无对应收益。

### 3.3 采纳路径 B 的形态

```text
capability.Evaluate(catalog, Request{Principal, Capability, Confirmed}) → Decision
消费方取得 Decision 后，再调用应用层用例端口执行

  外部 Harness ──→ /mcp (Guard 鉴权) ──┐
                                       ├──→ capability.Evaluate ──→ 消费方执行用例端口
  运营 Agent (QQ)  ────────────────────┤
                                       │
  客服 Agent       ────────────────────┘
```

- 策略求值**只有一份实现**（在 `capability`）；MCP 端点退化为「鉴权 + 内核投影」，不再持有业务语义；
- 审计写入与错误归一沿用既有 `internal/mcp/registry.go` / `errors.go`，三方共享同一套 `ErrorClass` 语义（含 `needs_review`）；
- 三方能力面严格同构——**外部能做而内部不能做（或反之）即为安全缺陷**，路径 B 从结构上排除这种可能。

**为跨进程保留缝**：`Request` / `Decision` 的形状刻意与 MCP `tools/call` 同构（能力名 + 参数映射）。
若将来某个内部 Agent 需要拆成独立进程，改用 `client.NewInProcessClient` 或真实 MCP 客户端的切换成本极低。

---

## 4. 分层设计

### L0 能力内核（唯一，共享）

| 组成 | 说明 |
|---|---|
| 能力目录 | `Spec{Name, Risk, Scope, MinPreset}`，**危险等级是一等属性** |
| 策略求值 | `Evaluate(catalog, Request{Principal, Capability, Confirmed})` —— 唯一求值入口 |
| 作用域断言 | `ResolveAccountScope(Principal, Spec, requestedCookieID)` —— 跨账号与否由 Principal 决定 |
| 用例契约 | 现有 `domain_ports.go` 的 15 个领域契约上卷后复用（由消费方注入并调用） |
| 统一审计 | 三方写入同一张审计表，以 `principal_type` 区分 |
| 错误归一 | 复用既有 7 个 `ErrorClass`（含 `needs_review`） |

**边界**：L0 不执行能力、不持有传输层对象、不触达业务数据。「是否允许」与「如何执行」分离，
消费方在拿到 `allow` / `require_confirm` / `require_async_confirm` 之后自行调用用例；
拿到 `deny` 时不得降级为直接调用用例。

### L1 编排（各自独立）

| 组件 | 职责 |
|---|---|
| `internal/mcp` | 瘦身为传输 adapter：Guard + 内核投影为 MCP tools/resources/prompts |
| `internal/qqbot` | 升级为运营 Agent：命令解析 → capability 调用 → **异步确认状态机** |
| `internal/agent`（新增） | 客服 Agent Loop：回合预算、步骤上限、转人工逃生阀 |
| `internal/agent/mcpclient` | 对外 MCP Client（找书 MCP 等第三方），独立故障域与超时 |

### L2 运行时策略（同一引擎，三个 Principal）

| Principal | 权限范围 | 确认机制 | 作用域 | 通道 |
|---|---|---|---|---|
| `remote_harness` | 全量 | 同步 `confirm=true` 参数 | 跨账号 | HTTPS |
| `ops_agent` | 全量（admin 级） | **异步二次确认** | 跨账号 | QQ WS |
| `support_agent` | 档位子集 | 档位内置 + 业务护栏 | **强制锁定当前 cookie** | 进程内 |

**运营 Agent 为什么不能等同于 `remote_harness`**：它的权限确实等同管理员（既有规划已定性「openid 白名单等价于固定管理员身份，与 MCP 的 `AdminUserID` 同层」），但它在**室外、异步、无法同步确认**。管理员在电脑前可以即时点确认，在 QQ 里不行。因此它需要独立的确认机制。

---

## 5. 运营 Agent 的离线确认机制

对「无人在场的写操作」按影响面分档：

| 操作特征 | 处理 |
|---|---|
| 只读、幂等 | 直接执行 |
| 写、可逆、低影响 | 执行 + 事后 QQ 通知 |
| 写、不可逆或高影响 | **异步确认**：回复「将要执行 X，回「确认」以继续」，超时作废 |
| 需要即时判断的 | 拒绝，提示回网页操作 |

实现要点：

- 需要一张**待确认操作表**，沿用既有 `ai_bargain_quotes` 的 `status / expires_at` 模式（该项目已有成熟先例）。
- 待确认记录中的操作入参**必须按白名单脱敏**，与 `mcp_call_audit.arguments` 同策略（AGENTS.md §1.7）。
- 超时未确认 → 状态置为作废并通知；**禁止自动执行**。
- 结果不确定的操作走既有 `ClassUncertain = needs_review` 语义，**禁止自动重试**。

---

## 6. 配置分层（按「谁的答案唯一」）

| 层 | 回答的问题 | 载体 | 可配置性 |
|---|---|---|---|
| 平台级 | 有哪些能力？危险等级？档位如何定义？ | 代码常量 | **不可配置** |
| 租户级 | 客服端默认跑到哪一档？预算多少？ | `user_settings` KV | 管理员可配 |
| 账号级 | 这个账号开不开？是否覆盖默认档位？ | `ai_reply_settings` 扩展列 | 逐账号可配 |
| 通道级 | 运营 Agent 开关与白名单？ | `user_settings`（`qqbot.*` 已有） | 管理员可配 |

### 6.1 档位定义（平台级，代码常量）

| 档位 | 能力范围 | 定位 |
|---|---|---|
| `readonly` | 只读查询类 capability | 最保守，只答复不动作 |
| `standard` | `readonly` + 议价报价、库存与价格确认、发货状态同步 | **默认档** |
| `advanced` | `standard` + 改价、发券等写操作（仍受 `confirm` 与业务护栏约束） | 需显式开启 |

### 6.2 危险等级与放行矩阵

| 等级 | 语义 | `remote_harness` | `ops_agent` | `support_agent` |
|---|---|---|---|---|
| `read` | 只读 | 允许 | 允许 | `readonly` 及以上 |
| `quote` | 生成报价/承诺 | 允许 | 允许 | `standard` 及以上 |
| `write` | 变更业务数据 | 允许（confirm） | **异步确认** | `advanced` |
| `account` | 账号、凭证、令牌 | 允许（confirm） | **异步确认** | **一律 Deny** |
| `destructive` | 删除/不可逆 | 允许（confirm） | **异步确认** | **一律 Deny** |

### 6.3 求值顺序（客服端）

```text
1. account.agent_enabled = false  → 不运行 Agent，回落确定性回复链路
2. preset = COALESCE(account.agent_preset,
                      tenant['agent.support.preset'],
                      platform.default_preset = 'readonly')
3. scope  : 强制 cookie 作用域 = 当前账号，无法跨账号
4. risk   : account / destructive → 任何档位一律 Deny
5. budget : 会话或日预算耗尽 → 转人工，不静默失败
```

### 6.4 为什么客服端只暴露档位，不暴露工具清单

| 维度 | 工具清单勾选 | 档位（本方案） |
|---|---|---|
| 认知负荷 | 需判断 60+ 工具的危险性，信息不足 | 只需理解 3 个有序档位 |
| 可审计 | 「勾了什么」无语义 | 「跑 standard 档」可审计可比较 |
| 演进 | 新工具上线后旧清单永久过时 | 按危险等级自动归入各档 |
| 安全 | 把安全决策交给无判断力的使用者 | 危险等级由平台代码判定 |

**管理端与运营端不设能力配置**：二者定位即「最大权限、最多工具」，加白名单将重蹈「权限向最严者收敛」。运营端的约束只体现在**确认机制**（异步），不体现在**能力裁剪**。

---

## 7. 前端入口归拢

原则：**就近归属**——配置项放在它生效前提所在的位置。

| 入口 | 承载 | 变更 |
|---|---|---|
| 设置 → Agent 中心（新增） | 管理端 Agent（外部 Harness）、客服端 Agent | 新建 |
| 设置 → QQ 连接器卡片 | 运营 Agent：入站命令开关、openid 白名单、**离线写操作策略（新增）** | 扩展现有卡片 |
| 账号编辑弹窗 → AI 助手区 | 启用开关、档位（默认继承租户） | 精简 |
| 设置 → AI 设置 | 模型凭证与业务护栏 | 原样保留 |

**运营 Agent 配置留在 QQ 卡片、不迁入 Agent 中心**，理由：

1. 它的启用前提是 QQ 通道已配置（`app_id` / `app_secret`），分开会产生「在 Agent 中心开了但没生效」的困惑；
2. `qqbot.commands_enabled` / `qqbot.command_openids` 已在其中，零迁移成本；
3. Agent 中心内放一行**只读指引**（「运营 Agent 在 QQ 连接器卡片配置」）消除「找不到」的问题。

**AI 设置页保留业务护栏**（最大折扣比例、最大议价轮次）：这些是业务规则而非 Agent 能力配置，混入档位是认知负荷的主要来源。

**实施补充（阶段 3c 落地时确定）**：

1. **档位名单随响应下发，前端不建第二份。** `presets` 字段是档位选项的唯一来源，
   租户级与账号级共用同一份。契约里 `preset` 的取值因此声明为普通 `string` 而不是
   `enum`——把取值固化成枚举会让「内核新增一档」直接打穿前端类型，与「名单动态下发」
   自相矛盾；非法档位由服务端拒绝（400），契约不比实现更严格。
2. **账号弹窗的 Agent 授权并入既有「保存」按钮，不即时生效。** 一次编辑一次提交，
   避免同一弹窗内出现两种生效语义。提交顺序为先授权后 AI 策略：授权失败即中止整个
   保存，不留下「AI 策略已改、能力授权没改」的半途状态；未改动授权时不产生写入。
3. **Agent 中心不迁入 MCP 服务卡片。** 管理端 Agent 一栏只读展示 Harness 的启用状态
   与接入地址，并指向「系统设置」的 MCP 卡片；搬迁会牵动 Settings 页既有契约，而收益
   只是视觉上的集中。
4. **前端传输边界照旧。** 两个 Agent 适配层都落在各自 feature 的 `api.ts`
   （依赖规则 §6「每个 feature 只有一个 api.ts 承担传输契约读取」），不新建
   `agentSupportApi.ts` 之类的平行适配文件——门禁实测会判违规。
5. **QQ 卡片的「离线写操作策略」本期不加。** 它是异步确认的配置面，而异步确认状态机
   本期未实现；先暴露一个不生效的开关，正是 §8 拒绝 `daily_budget` 时的同一条理由。
   运营 Agent 本期只做只读迁移：三个 `ops_*` 命令在执行前经能力内核求值，判定恒为
   放行，对管理员行为零变化。QQ 卡片因此保持原样，只由 Agent 中心放一行只读指引。
6. **危险等级标注与既有 `Destructive` 是两个维度，不合并。** §6.2 的放行矩阵要求平台
   知道每个动作危险到什么程度，而 `Destructive` 回答的是另一个问题——这个 MCP 工具
   要不要 Harness 先传 `confirm=true`。两者判据不同：本地可逆写入（建卡券组、改设置）
   是 `write` 却不需要确认；平台触达类动作（刷新、同步、发布、发送）也是 `write`
   却需要确认。因此本期保留 `Destructive` 原样（39 个工具、名称不变），另建平台危险
   等级标注表，两者之间只成立两条单向蕴含：只读不要求确认；高危必须要求确认。
7. **标注表与决策目录分开维护。** 危险等级表覆盖传输层实际暴露的全部工具，新增工具
   必须同时登记等级，否则该工具不注册（等级未知的动作宁可不存在）；而「对客服端开不
   开放」仍只在 `PlatformCatalog` 里登记，等工具真正接入 Agent 时才加。合成一张表会让
   「加一个工具」意外变成「开放一个能力」。
8. **启用与档位在每次补全前解析，不冻结在账号启动时。** 账号弹窗的 Agent 授权走
   `/api/v1/agent/support/accounts/{cookie_id}`，只写库、不重启账号；若生成器在账号启动
   那一刻固定档位，网页上的开关就会**在无声中失效**——这正是 §8 拒绝 `daily_budget` 的
   同一条理由。因此 `internal/agent` 定义 `SessionResolver` 端口，由组合层在每次补全前
   解析，开关与档位无需重启账号即可生效。代价是每条 AI 触发消息多几次数库读，相对同一条
   路径上已有的十余次读与一次模型调用可以忽略。
9. **回落是三级而非一级。** 未启用、配置不可解析、循环失败都回落到单次问答，且回落路径
   根本不持有工具。配置不可解析（归属失败、读库失败）按「未启用」处理而不是按默认档位
   继续跑：宁可不带工具答复，也不能凭未知档位运行工具回合。

---

## 8. 存储设计

| 层 | 载体 | 键/列 |
|---|---|---|
| 租户级 | `user_settings` KV | `agent.support.enabled`、`agent.support.preset`（`agent.support.daily_budget` 推迟到预算执行语义落地后再加，见下） |
| 账号级 | 扩展 `ai_reply_settings` | `agent_enabled`（可空，NULL = 继承）、`agent_preset`（可空，NULL = 继承） |
| 运营待确认 | 新建 `agent_pending_operations` | 沿用 `status` / `expires_at` 模式，入参白名单脱敏 |

**为什么 `daily_budget` 暂不加**：配置项的前提是它真的生效。日预算需要「按账号按日计数与
扣减」的持久化与执行路径，该路径尚未实现；先暴露一个不生效的开关，正是 §7 反对的
「在 Agent 中心开了但没生效」。

**为何扩展 `ai_reply_settings` 而非新建表**：该表已承载账号级 AI 凭证（`api_key` / `base_url` / `model_name`），与 Agent 配置**归属相同、敏感度相同**，符合 AGENTS.md §1.5「敏感度或归属不同的类型尤其禁止合并」的反向判定。

**必须遵守**：

- `AIReplySettings` 含 `api_key`，按 §1.7 禁止序列化为 HTTP 响应、禁止进入前端状态。新增 Agent 配置必须**新建领域模型**（如 `AgentAccountConfig`），不得复用含凭证的持久化模型作 DTO。
- 新增迁移 `00059_agent_account_config.sql` 与 `00060_agent_pending_operations.sql`。**`00058` 已被占用**（`00058_account_auto_delist.sql`，账号每日定时下架，随 v1.0.32 上线）。
- 迁移只需 `sqlite` / `postgres` **两份**，编号与最终 schema 一致。MySQL 已于 v1.0.32 移除，AGENTS.md §1.5 现为「两种方言」。
- 数据库行为变更需 SQLite 聚焦测试；有环境时用**本机已运行实例**加 `TEST_POSTGRES_URL` 补 PostgreSQL 回归，**禁止为门禁启动数据库容器**（§0.1）。

---

## 9. 包结构变更

```text
internal/capability/              新增（L0，纯策略）
  doc.go                          包边界说明：只回答是否允许、需要何种确认
  risk.go                         五级危险等级（read/quote/write/account/destructive）
  preset.go                       三档能力档位与比较
  principal.go                    三个调用方类型与身份合法性
  spec.go                         能力声明与自洽性校验
  catalog.go                      只读注册表（重复名与非法声明拒绝登记）
  catalog_platform.go             平台能力声明源 PlatformCatalog()
  policy.go                       唯一求值入口 Evaluate()
  scope.go                        账号作用域断言

internal/mcp/                     瘦身为传输 adapter（Guard + 内核投影）
internal/qqbot/                   升级为运营 Agent（命令解析 + 异步确认状态机）
internal/agent/                   新增（客服端 Loop）
  runtime.go                      回合预算、步骤上限、转人工逃生阀
  mcpclient/                      对外 MCP Client
internal/application/agentadmin/  新增（配置用例）
```

### 规范变更提示（必须同步修订）

AGENTS.md §1.1 现规定「`internal/mcp` 的用例接口由包内定义并由组合层投影实现」，且 `internal/mcp` 禁止依赖 db / server / 平台 / 浏览器 / 自动化 / 引擎。将 `domain_ports.go` 上卷至 `internal/capability` 与该条冲突，**必须同步修订 AGENTS.md §1.1 与 `docs/architecture/dependency-rules.md`**，否则架构门禁会持续报错。

---

## 10. API 与契约

新增接口遵循 AGENTS.md §1.3：`/api/v1` 前缀、具名 DTO、`api/openapi.yaml` 唯一契约源、先改规范再重生成类型再补契约测试、统一错误信封。

审计查询接口新增 `principal_type` 过滤维度，沿用既有分页形态。运营 Agent 的异步确认需要「确认/取消」接口，**必须走显式确认语义**（对应 `RequireAsyncConfirm`），禁止做成隐式副作用。

---

## 11. 实施步骤

属 AGENTS.md §0.6 改进性任务（六阶段重构已全部完成，不适用阶段制度）：

```bash
git worktree add ".worktree/agent-capability-kernel" -b "feature/agent-capability-kernel"
cd ".worktree/agent-capability-kernel"
```

顺序：

1. 修订 AGENTS.md §1.1 与 `dependency-rules.md`；
2. 新建 `internal/capability`（risk / preset / principal / spec / catalog / policy / scope）+ 聚焦测试；
3. `internal/mcp` 瘦身为传输 adapter，现有 tools 测试**不减项**；
4. 迁移 `00059` / `00060`（SQLite + PostgreSQL 两份）+ `agentadmin` 应用服务 + 仓储端口；
5. `/api/v1` 接口登记 + 生成类型 + 契约测试；
6. 客服 Agent Loop（`internal/agent`）+ 对外 MCP Client；
7. 运营 Agent：`internal/qqbot` 接入 capability + 异步确认状态机；
8. 前端 Agent 中心页、账号弹窗精简、QQ 卡片扩展。

提交按 AGENTS.md §3.1 拆分：一条提交只做一件事，且该提交点自身可编译。
本任务属改进性任务，不适用阶段制度的「一阶段一提交」约束；但**禁止把互不相关的改动合并进同一条提交**。

---

## 12. 验收

```bash
bash scripts/guard-local-dev-env.sh
go vet ./...
go test ./... -count=1
go run ./tools/architecturecheck
go run ./tools/commentlint -mode check -root .
npm --prefix frontend run comments:check
make api-check
```

- 合并到 main **以本机门禁为准，不再要求容器完整门禁**（AGENTS.md §0.1）。
- 确需 Chromium 或镜像验证时走 `sh scripts/compose-functional.sh`，且**必须在 worktree 目录执行**；禁止在部署工作区根目录执行 functional 栈（§2.2 / §2.6）。
- 运营 Agent 的 QQ 联调受限于真实凭据（`qq-inbound-command-plan.md` §7.5 已记录该阻塞）。

---

## 13. 风险与未决

| 项 | 说明 |
|---|---|
| 门禁与基线重刷 | 上卷后 `architecturecheck` 规则与 commentlint 基线需同步更新 |
| 规范联动 | 不同步修订 §1.1 与 `dependency-rules.md` 将导致门禁持续失败 |
| 运营 Agent 异步确认复杂度 | 引入跨会话状态，是本方案新增的主要复杂度；建议先只开放 `write` 中的低风险子集 |
| 未决：人工接管通道 | 客服端是否需要「每会话转人工」按钮，待产品确认 |
| 未决：第三方 MCP 连接配置层级 | 找书 MCP 等的连接参数建议租户级，待确认 |
| 未决：自定义档位 | 暂定不可自定义 |
| 方案失效条件 | 若客服端最终被限定为纯闲聊、完全不触碰订单/账号/卡密，能力内核的共享收益下降，应改用保守版（仅复用领域契约，不做包结构上卷） |
