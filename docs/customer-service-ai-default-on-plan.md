# 客服 AI「默认开启 + 一键关闭」整改方案

> 目标：AI 客服在 API Key 配好后**默认对全部账号生效**，同时提供**一个全局开关可以立即全关**。
> 本文只做两件事：列出当前实现里阻碍这个设想的点（附代码位置），给出整改方案与取舍。
> 待办登记在 `docs/SDD.md` 的 E 组，不在此处维护状态。

## 一、阻碍清单

### 阻碍 1：账号表「无行即关闭」是硬语义

| 位置 | 行为 |
|---|---|
| `internal/db/ai_reply.go:107` | `Get` 查不到行直接返回 `ErrNotFound` |
| `internal/engine/ai.go:68` | `cfg == nil` 即 `return nil, nil`，AI 通道被跳过 |
| `internal/db/ai_reply.go:59` | `IsEnabled` 无行返回 `false` |
| `internal/db/ai_reply.go:95` | `PricingMode` 无行返回 `false, false` |
| `internal/server/settings_handlers.go:372` | 无行时接口回 `ai_enabled: false`，前端因此显示「关」 |

后果：**新建账号、恢复备份、换机部署后，AI 一律是关的**，必须逐个账号手动打开。这正是「默认开启」要改的第一处，也是最容易改的一处。

### 阻碍 2：开关只有账号维度，没有全局闸

`ai_reply_settings.ai_enabled` 是唯一开关，写在每账号一行。要「一键全关」只能对 N 个账号逐个 `PUT`，且漏一个就还在回。
`system_settings` 里已有大量全局键（`ai_api_key` / `ai_model` / `ai_scope_config` / `ai_reply_review_mode`），但**没有任何一个表示「AI 总开关」**。

### 阻碍 3：意图白名单让「开了也不回」（第二大阻碍）

`internal/engine/ai_scope.go:161-175` 的内置边界：

| 意图 | allowAI | 覆盖的真实问法 |
|---|---|---|
| bargain | ✅ | 便宜点、最低多少 |
| refund | ✅ | 能退吗、退款政策 |
| stock | ✅ | 全集吗、多少集 |
| shipping | ✅ | 运费、包邮、能寄吗 |
| **order** | ❌ | 什么时候发货、发什么网盘、怎么下载 |
| **inquiry** | ❌ | 多少钱、怎么卖、怎么下单 |
| **consult** | ❌ | 内容是什么、适合谁、怎么用 |
| **spec** | ❌ | 几本、第几册、什么版本 |
| **usage** | ❌ | 有效期、支持吗 |

也就是说：**即使把 `ai_enabled` 全部打开，买家问「多少钱」「什么时候发货」仍然是 `no-intent` 不回复**，直接落到默认回复。
更关键的是：`ai_scope_config` 一旦在库里存在，**内置默认完全不参与**（`loadScope` 走配置分支），线上配置里列了几条就只认几条。

### 阻碍 4：人工确认模式会「开了但不发」

`internal/engine/reply_review.go` 的 `ai_reply_review_mode` 开启时，AI 草稿不外发，转成卖家通知等人工确认。
它与「默认开启」是两套语义：一个决定生成，一个决定发送。若线上该键为开，默认开启后买家仍然收不到回复。

### 阻碍 5：准入层在前，商品不在库时四条通道一起跳过（非设置项）

`internal/engine/reply_identity.go:canAutoReply`：商品不在本地 `item_info` 时**关键词/插件/AI/默认回复全部跳过**，且 `ItemID` 为空那条**没有日志**。
它不是配置问题，但会表现成「明明开了却不回」，整改时必须一并纳入验收口径。

### 阻碍 6：API Key 是全局单键，但每次读取都写一条审计

`internal/db/store.go:157` 的 `ReadSensitiveSettingForAccount` 最终读的是全局 `system_settings.ai_api_key`，`cookieID` 只用于解析归属写审计。
而 `internal/engine/ai.go:403` 的 `globalAIConfig` **每条回复都调一次** → **每条 AI 回复产生一条 `security_audit_log`**。
这让「默认开启」在校验成本上不中性：开启后审计表会随回复量线性膨胀。设计总闸时应把「总闸读取」放在不触发审计的普通 `Settings.Get` 路径上。

### 阻碍 7：Agent 档位是另一套开关，容易与 AI 开关混淆

`agent.support.enabled`（租户级，`internal/application/agentadmin`）+ `ai_reply_settings.agent_enabled`（账号级）决定是否启用**带工具的客服 Agent**，与 `ai_enabled` 完全独立。
现状下「AI 开着但 Agent 没开」是合法组合，用户很容易以为自己已经全开。

## 二、整改方案

### 2.1 方案 B（推荐）：总闸 + 保持二态账号开关

核心语义改写只有一句：

```
生效 = 总闸开 && (账号无行 ? 开 : 账号行里的值)
```

| 改动点 | 内容 |
|---|---|
| 新键 | `system_settings.ai_master_enabled`，**缺省视为 true**（未配置即开启） |
| `ai_reply.go:Get` | 查不到行时返回默认值结构（`AIEnabled` 由调用链按「总闸」决定）而不是 `ErrNotFound` |
| `ai.go:Reply` | 先读总闸（普通 `Settings.Get`，不审计）；总闸关 → 直接 `return nil, nil`；再按「无行=开」判定账号级 |
| `IsEnabled` / `PricingMode` | 改为返回「账号是否显式关闭」，语义变更需同步调用方 |
| `ListEnabledCookieIDs` | 改为「未显式关闭的账号」= 全部账号减去显式 0 的行 |
| 一键关闭 | `PUT /api/v1/settings` 写 `ai_master_enabled=false`，逐回复读取即时生效，无需重启 |
| 契约 | `ai_enabled` 响应加 `effective_enabled` 与 `source`（`global`/`account`/`none`），前端才能显示真实状态 |
| 前端 | AI 设置页顶部一个总闸开关；账号级开关改成「跟随全局 / 强制开 / 强制关」三态文案 |

**优点**：不动表结构，不迁移数据，语义可解释（无行=跟随，0=显式关）；发布后历史 `ai_enabled=0` 的行仍然是关闭，符合直觉。
**缺点**：账号一旦保存过 AI 设置就会写行，此时「跟随」语义丢失；需要在保存逻辑里区分「只改砍价护栏」与「改开关」。

### 2.2 方案 A（更彻底）：`ai_enabled` 改三态

把 `ai_enabled` 迁移成可空列：`NULL=继承总闸 / 1=强制开 / 0=强制关`，判定为三元表达式。
**优点**：语义显式，保存护栏不再意外改变继承关系。
**缺点**：需要迁移（SQLite + PostgreSQL 双份），且要决定历史 0 值的解释——若把历史 0 当作「继承」，发布即等于全量开启；若保留为「强制关」，用户的诉求在存量账号上仍不成立，仍要手工点。

**建议**：先做方案 B 拿到「默认开启 + 一键全关」，把方案 A 作为后续把「跟随」表达清楚的小重构。

### 2.3 必须同步做的三件事（否则「默认开启」名不副实）

1. **放开意图白名单**：先放 `order`（渠道事实已有 `card-channel`）与 `consult`（FAQ 有据可答），`spec`/`usage` 等 A2 覆盖率提升后再放。否则默认开启只是「多接管了砍价与运费」。
2. **人工确认模式与总闸联动**：总闸开启时若 `ai_reply_review_mode` 也为开，UI 必须显式提示「AI 已开启，但回复需人工确认后发出」。
3. **自检口径同步**：A6 的探针不能再把「`ai_enabled=1` 的行数为 0」当故障（`ai_readiness.go:47`），要改为判断「总闸是否关闭」与「凭据是否缺失」。

## 三、风险与取舍

| 风险 | 说明 | 缓解 |
|---|---|---|
| 默认开启放大编造 | A4 只挡「回避话术」，挡不住「承诺包邮/承诺售后」这类编造 | 总闸上线首周与 `ai_reply_review_mode` 同时开，观察后再关人工确认 |
| 意图放开过猛 | `spec` 无事实源时会重演「答非所问」 | 按意图逐条放开，每条都配一条可机检期望进评测 |
| 审计表膨胀 | 每条回复一条敏感设置审计（阻碍 6） | 总闸走普通读取；把 key 读取改为进程内缓存 + 定期失效 |
| 一键关闭被误用 | 关掉后买家消息只走默认回复 | 关闭动作记审计 + 页面保留「已关闭 N 分钟」提示 |

## 四、验收信号（可机检）

1. 库里无任何 `ai_reply_settings` 行时，`GET /api/v1/ai/settings` 返回 `effective_enabled=true`（前提：`ai_api_key` 非空）。
2. 写 `ai_master_enabled=false` 后，下一条消息的 `ai_decision_log.reason` 为新增的 `master-off`，且不发 AI 回复。
3. 总闸开 + 某账号 `ai_enabled=0` 时，该账号 `reason=account-off`，其它账号正常接管。
4. 一条真实「多少钱」消息在总闸开启后 `reason=taken`（放开 inquiry 后）。

## 五、五项整改的逐项评估（2026-11-01）

### 5.1 意图白名单实际拦住了什么（实测，不是推测）

方法：用内置边界 `defaultScope` 对 `data/evalexport/samples.jsonl` 里 12 个真实会话的 **22 条买家文本消息**逐条分类（临时探针跑完即删，未入库）。

| 结果 | 条数 |
|---|---|
| 负向词拦截（转人工） | 0 |
| 放行（bargain/stock/refund/shipping） | 5 |
| **被拦** | **17**，其中 **11 条命中 `order`**、6 条零命中 |

被 `order` 拦下的原话（全部是渠道问法，正是事实最全的一类）：

- 「有度盘的吗」「夸克？」「夸克网盘吗」「有夸克网盘的吗」「夸克网盘有吗」
- 「支持复制粘贴到百度网盘吗？」「我要夸克的」「你好想问下有夸克的嘛」「发网盘吗」「有夸克嘛」

**结论：当前白名单拦下的主力不是合规/安全，而是「发货渠道」——最高频、且 `card-channel` 事实最完整的一类问句。** 这正是「AI 开着却总不回」的主因。

按「只拦合规、安全」的目标重排：

| 意图 | 建议 | 理由与前置 |
|---|---|---|
| `order` | **放开** | 渠道/发货/下载，事实源已具备；须补一条「不得承诺发货时效」约束 |
| `inquiry` | **放开** | 价格问题已有价格守卫（最低价 / 拒绝兜底）兜底 |
| `consult` | **放开** | FAQ 与商品描述有据；需 acceptance：无 FAQ 命中时不得编造内容 |
| `spec` | 暂留拦 | 依赖标题规格抽取覆盖率（A2，27 个商品仅 21 个有渠道事实） |
| `usage` | 暂留拦 | 有效期/会员属承诺类，无事实源，放开会直接放大编造 |
| 负向词表 | **保持不动** | 投诉、站外引流、退款动作、转人工——这就是「合规/安全」的全部 |

预期效果：接管率 5/22（23%）→ 16/22（73%）；剩余 6 条是零命中闲聊（「？」「已经买了」「[捂脸哭]」），不属白名单问题。

### 5.2 移除人工确认模式：可以删，但要按顺序

现有触点 6 处：`engine/reply_review.go`（79 行）+ 测试（213 行）、`ReplyService.reviewNotifier` 字段与构造参数、`application/settings/service.go` 的键校验分支、前端 `AISettings.tsx` 开关 + `models.ts`/`constants.ts`/`api.ts`、前端测试。

**风险**：它是当前唯一的「发送前人工缓冲」。删掉之后，AI 的每条产出都会直接外发，唯一的确定性保护只剩 A4 的回避话术兜底——而 A4 挡不住「承诺包邮 / 承诺售后 / 承诺时效」这类编造。

**建议顺序**：先在线上查一次 `ai_reply_review_mode` 的实际值（若为开，说明至少有人在用，删前需确认）→ 与 E1/E2 同批上线总闸 → 观察一周编造类投诉 → 再删。删除本身是一次性动作，晚删一周的成本远低于删早了出事。

### 5.3 准入层自动同步：设想可行，但不要同步等待

现状证据：`canAutoReply` 全程是**本地查询**（`ItemBelongsToAccount`）；mtop `Client` 接口**没有单品详情方法**（`fetchItemDetail` 是私有的，`DetectItemMultiSpec`/`FetchItemPublisher` 只读不落库），`adapter` 侧只有 `SyncAll`/`SyncPage` 列表同步；`items.UpsertItemBasic` 存在可复用。

| 方案 | 做法 | 问题 |
|---|---|---|
| A 阻塞式全量同步 | 发现缺失 → 调 `SyncAll` → 重查 | 列表分页多次请求，1~3 秒起；dispatcher 串行处理，会拖垮整账号吞吐 |
| B 单品详情直查 | 新增 `FetchItemDetail`（公开）+ 映射落库 → 重查 | 单次请求可控，但要新增字段映射，且凭证锁 + 网络 I/O 仍在回复链上 |
| **C 异步补齐（推荐）** | 缺失时投递一次去重的补齐任务，本条按现有降级 | 本条仍不回，下一条即可命中；无阻塞、无风暴 |

**推荐 C 先上、B 作为第二阶段**。无论哪个方案都必须配套：同一商品负缓存（24h 内不重试）、按账号限流（例：5 分钟 3 次）、single-flight（并发消息只触发一次）、以及明确的失败语义（同步失败不得改变「不回」的默认）。
验收：`ai_decision_log` 出现新 reason `item-sync-requested`；补齐成功后该会话下一条消息 `reason=taken`。

### 5.4 密钥审计膨胀：可优化，推荐内存缓存 + 写时失效

量级：`globalAIConfig` 每条 AI 回复调一次 `ReadSensitiveSettingForAccount` → **每条回复一行 `security_audit_logs`**；该表已有 30 天保留清理（`retention.go:108`），所以是「有界但没必要的高频」——日 300 条回复即约 9000 行/月。

| 方案 | 说明 | 取舍 |
|---|---|---|
| 读缓存（TTL 10 分钟） | 命中缓存不写审计，只有真实解密读取才写 | 审计语义从「每次访问」变「每次真实读取」，需在文档写明 |
| 写入去重 | 同 (user,key,action) 窗口内合并一行 | 每次仍打一次 DB |
| **专用 provider（推荐）** | `aiKeyProvider`：TTL + single-flight + 设置写入时 bump 版本失效 | 一次改动同时解决审计膨胀与每次解密的 CPU 开销 |

注意：`Settings.Get` 是普通读、不审计；总闸与 `ai_scope_config` 走普通读路径，不受此事影响。

### 5.5 Agent 开关统一：留配置、去开关

现状：`ai_enabled`（AI）之外还叠了两层——租户 `agent.support.enabled` 与账号 `agent_enabled`，`ResolveSession` 返回 `effective.Enabled`。三者独立，用户极易误判「全开了」。

建议：**`ResolveSession` 的 enabled 直接取统一生效值**（E1/E2 的产物），`preset` 仍按「租户默认 → 账号覆盖」解析并保留。
要删：`KeySupportEnabled`、`agent_enabled` 列的读取、4 个 `/api/v1/agent/support/*` 接口里的 enabled 字段、前端 AgentCenter 与账号页的 Agent 开关。
要留：preset 的读写接口与 UI。

**风险**：Agent 带工具，成本与延迟都高于单次问答；「AI 开 = Agent 开」会把工具调用量一起拉起来，与 E4 意图放开叠加时尤其明显。默认档位保持 `readonly`（`capability.DefaultPreset`），需要时再逐账号升档。

### 5.6 五项合起来的实施顺序

1. **E1 + E2**（语义反转 + 总闸）——一切的前提；
2. **E6 删人工确认**——必须在 E1/E2 之后，且线上原值先确认；
3. **E4 意图放开**——order → inquiry → consult，逐条配可机检期望；
4. **E7 密钥 provider**——与总闸同批，避免开启后审计表暴涨；
5. **E8 准入层异步补齐**——独立，可并行；
6. **E9 Agent 开关统一**——最后做，避免与前三项同时在线上放大变量。
