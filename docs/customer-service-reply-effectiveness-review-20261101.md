# 客服回复有效性：架构诊断、问题分类与优化方案

> 版本：1.0 ｜ 日期：2026-11-01
> 主题：客服回复有效性（是否启用 → 是否接管 → 是否答对）
> 输入（近一个月报告）：[2026-10-10 复盘](customer-service-review-20261010.md)、[2026-10-17 周复盘](customer-service-weekly-review-20261017.md)、[2026-10-18 ROI 报告](customer-service-roi-optimization-20261018.md)、[2026-10-24 周复盘](customer-service-weekly-review-20261024.md)、[2026-10-31 周复盘](customer-service-weekly-review-20261031.md)、[2026-10-31 启停专项](customer-service-ai-enablement-diagnosis-20261031.md)
> 数据口径：源码判定链（确定性）+ `data/evalexport` 12 真实会话（10-09~10）+ 60 条评测集跑分（10-11）+ `backup/` 10-08 生产快照。**生产库当前仍不可直读**，实时状态需只读查库确认。

---

## 一、当前客服架构

### 1.1 分层结构（自上而下）

```
接入层  WebSocket 入站 → extractChatMessage → 去重(1h) → 防抖(1s) → messageDispatcher
          ↓
准入层  canAutoReply（归属门禁：商品必须在本地 item_info 且角色=seller）
          ↓ 通过
引擎层  ReplyService.resolve 五级：API → 关键词 → 插件(find_stuff) → AI → 默认回复
          ↓ 进入 AI 分支
AI 子层  AIReplyImpl.Reply：
          ① 账号开关 ai_reply_settings.ai_enabled
          ② 边界 ai_scope.decide（负向词一票否决 + 意图白名单 takeover）
          ③ 凭据 ai_api_key（空则静默返回）
          ④ 事实注入 liveKnowledge（在售/库存/规格/运费/履约/发货渠道）
          ⑤ 生成器 Generate（单次补全 或 Agent 工具回合）
          ⑥ 价格守卫 + 安全闸门 + 人工确认闸门（review 模式）
          ↓
事实源层  ai_live_context.go（5 类动态事实 + 4 档渠道来源）
          ↓
可观测层  ai_decision_log / ai_generation_log / automation_run_steps
```

### 1.2 关键组件与职责

| 层 | 组件 | 职责 | 代码位置 |
|---|---|---|---|
| 接入 | `messageDispatcher` | 去重、防抖、生命周期任务管理 | `internal/engine/message_dispatcher.go` |
| 准入 | `canAutoReply` / `resolveLocalItemOwnership` | 本地商品归属核验；不通过则**四通道全跳过** | `internal/engine/reply_identity.go` |
| 引擎 | `ReplyService.resolve` | 五级短路式回复调度 | `internal/engine/reply.go:251` |
| AI | `AIReplierImpl.Reply` | 开关→边界→凭据→事实→生成→守卫 | `internal/engine/ai.go:63` |
| 边界 | `aiScope.decide/negative` | 负向词一票否决 + 意图 `allowAI` 接管 | `internal/engine/ai_scope.go:268` |
| 事实 | `loadLiveKnowledge` | 在售/库存/规格/运费/履约/渠道来源 | `internal/engine/ai_live_context.go:133` |
| 生成器 | `DefaultAIGenerator` / `agent.Replier` | 单次补全 / 带工具回合的 Agent（可在不改校验的前提下替换） | `internal/engine/ai.go`、`internal/agent/replier.go` |
| 能力 | `capability` | 能力目录与访问策略唯一实现点；Agent 工具调用前必经 | `internal/capability/doc.go` |
| 配置 | 三层 + 账号层 | `ai_scope_config`/`ai_knowledge_config`/`ai_policy_config` + `ai_reply_settings` | `system_settings` / `ai_reply_settings` |
| 装配 | `NewRuntimeBundle` | 一次性装配 chat/notifier/manager/automation/orderDetail | `internal/adapter/adapter.go:166` |

> **架构要点**：Agent 是"生成器替换"，不是独立开关——它仍受 `ai_enabled` + `canAutoReply` + `scope` 三重门控，且只替换生成、不替换校验（`agent/doc.go` 三条硬约束）。这是"持续演进、非割裂重构"的结构基础。

---

## 二、客服架构与交易系统的接线点

客服与交易系统通过**只读事实端口 + 共享账户/装配**耦合，不直接写交易数据。共 8 个接线点：

| # | 接线点 | 方向 | 客服侧入口 | 交易侧表/流程 | 作用与约束 |
|---|---|---|---|---|---|
| W1 | **订单履约** | 交易→客服（只读） | `Orders.FindLatestForFulfillment(chatID,itemID)` | `orders`（关联键 `chat_id`） | 注入"已付款/已发货/发货时间"；**不限订单状态**取最近一笔，避免"已发货"被判成"查不到" |
| W2 | **商品事实** | 交易→客服（只读） | `Items.Get` / `AllForCookie` | `item_info` | 标题规格抽取 + 运费标注 + 在售清单；**也是归属门禁的数据源** |
| W3 | **卡密渠道/库存** | 交易→客服（只读） | `Cards.DeliveryChannelsByIDs` / `AllForUserSummary` | `cards` | 渠道权威事实(`card-channel`) + 库存计数；**卡密正文只计数，禁止入提示词/日志** |
| W4 | **自动发货配置** | 交易→客服（只读） | `Automation.ListPageForUser` + action 绑定 | `automation_rules` / `automation_rule_actions` / 模板 | 渠道推断第二来源(`delivery-config`)；多消息/先码后链识别 |
| W5 | **会话角色快照** | 双向 | `ChatSessionRole` / `SaveChatSessionRole` | `chat_sessions`（`account_role`/`role_source`） | 归属结论缓存，避免每条消息重查；`local_item` 为确定证据 |
| W6 | **对话历史** | 客服→交易→客服 | `conversationContext` | `chat_messages` | 多轮上下文 + 砍价轮次计数 |
| W7 | **订单事件→发货** | 交易→自动化 | `RuntimeBundle.Automation`（同一 `manager` 装配） | 订单事件 / `automation.Center` | 付款触发自动发货；`PricingMode` 决定 AI 改价是否参与 |
| W8 | **归因回写** | 客服→交易侧观测 | `recordDecision` / `recordGeneration` | `ai_decision_log` / `ai_generation_log` / `automation_run_steps` | 决策/生成/发货三表联动，回答"为什么接管/谁答的/卡哪一步" |

**接线的三个工程约束**：
1. **只读优先**：客服侧对交易表只读（`FindLatestForFulfillment`、`DeliveryChannelsByIDs`），写操作只发生在自身会话/报价表。
2. **失败降级**：任一事实查询失败只跳过该段并记 Debug，**绝不阻断回复**。副作用是"事实缺失时静默退化为回避"，这正是内容层问题的温床。
3. **敏感隔离**：`cards.DataContent` 只在内存计数，绝不出现在提示词、日志、API 响应中。

---

## 三、问题分类

按"回复有效性"漏斗（启用→接管→答对）归为 7 类，每类标注证据与占比：

| 类别 | 问题 | 实证（近一月） | 严重度 |
|---|---|---|---|
| **A 启停类** | AI 未启用（`ai_enabled=0`/无行/`api_key` 空） | 10-08 快照：`ai_reply_settings` 0 行、`api_key` 空；`ai.go:105` 仅 Warn | 阻断 |
| **B 准入类** | 商品不在 `item_info` → 归属门禁跳过**四通道** | ROI §3.4：`item_info` 停 7 天、6 次`令牌为空`、4 次"跳过自动回复" | 阻断 |
| **C 覆盖类** | 意图漂移（线上 4 vs 代码 8）→ 口语问法不命中 | ROI §2.3/§3.2：delivery 词表逐字等于 10-10 旧版 | 高 |
| **D 事实类** | 发货渠道/规格/履约事实未注入 → 回避或编造 | 回避话术 **7/12** 会话；规格"说不准"vs"34集"矛盾 | 高（准确性） |
| **E 提示词类** | FAQ"以详情页为准"与 `deliveryAnswerConstraint` 冲突 | 10-11 事故根因；10-18 已改文案但**未发布** | 高 |
| **F 意图歧义类** | `refund` 与 `fulfillment` 优先级不清 | `[12]`「那能退吗」→"能等我帮你导夸克" | 中（准确性） |
| **G 输出质量类** | 凭证含平台水印乱码 + 重复外发；合规承诺编造 | `祄七三及森并闭里品` 乱码；compliance 2/7→3/7 仍编造 | 中 |
| **H 可观测类** | 归因表 0 行、无告警 → 答对率无法量化 | `bool→INTEGER` 编码失败两月；同步停摆无人知 | 高（元问题） |
| **I 部署类** | 合 main ≠ 上线（镜像 10-12 构建） | ROI §6.3 | 阻断（收益折算） |

> 归类原则：A/B 决定"有没有回复"，C/E/F 决定"答得对不对"，G 决定"答得专不专业"，H 决定"能不能度量"，I 决定"改进能不能生效"。

---

## 四、问答效果打分（双层指标）

### 4.1 指标体系（沿用既有评测机制）

| 层 | 指标 | 判定方式 | 现状 |
|---|---|---|---|
| **决策层**（零账号、确定性） | 接管准确率 / 负向拦截率 / 意图分类正确率 / 误伤率 / 特殊语义保持率 | 标注集离线跑 | 稳定；C 类受配置漂移拖累 |
| **内容层**（需 LLM） | FAQ 命中 / 清单引用 / 策略覆盖 / 回复质量分 | mock 确定性 + LLM-as-judge | 受 D/E/F/G 拖累 |

### 4.2 当前得分（实测，model=deepseek-flash，judge=deepseek-flash，10-11 快照）

| 评测集 | 用例数 | 通过率 | 说明 |
|---|---|---|---|
| 仅 12 真实会话（1 采样） | 12 | **91.7%** | **严重高估**（样本太少） |
| 仅 12 真实会话（3 采样） | 12 | 83.3% | 反映模型随机性 |
| 12 真实 + 34 合成 | 46 | **69.6%** | 补场景后骤降 22pp |
| 12 真实 + 35 合成 | 47 | 76.6% | |
| 12 真实 + 48 合成 | 60 | 73.3% | 当前主基线 |
| 60（提示词修复后） | 60 | **80.0%** | 仅 +6.7pp |
| **目标** | — | **≥85% → ≥90%** | 分阶段验收 |

### 4.3 短板定位（按场景）

| 场景组 | 样本数 | 主要失败模式 |
|---|---|---|
| delivery（发货渠道） | 6 | 回避"以详情页为准"，未直答渠道 |
| compliance（合规承诺） | 6 | 编造"包治/必过/无限期"类承诺（2/7→3/7 仍漏） |
| link-code（链接/提取码） | 9 | 链接失效/提取码不对的确定性应答 |
| bargain（砍价边界） | 4 | 5 条中 2 条越界 |
| escalation / robustness | 11 | 转人工触发与鲁棒性 |

> **样本结构现状**：12 真实多轮 + 48 合成**单轮**。合成样本几乎全为单轮，**缺乏多轮追问与"已付款后"状态样本**，是打分的结构性盲区。

---

## 五、修复点在架构中的位置

| 类别 | 修复动作 | 落点（文件/函数） | 是否需发布镜像 |
|---|---|---|---|
| A 启停 | AI 启用态+凭据启动自检与巡检告警 | 新增巡检，复用 `AIReply.Get`/`Settings.Get` | 否（可独立） |
| B 准入 | 商品同步失败持续告警 + 令牌自愈 | `internal/adapter/item_sync.go` 增告警分支 | 是 |
| C 覆盖 | 配置漂移检测（线上 vs 代码基线 diff） | 复用 `aimatch.Compile` + `SystemSettings.Get`，加定时校验 | 否 |
| D 事实 | 规格从订单成交规格反推补全 | `extractItemSpecFacts` 注入路径 + `orders.spec_name/spec_value` | 是 |
| D 事实 | 凭证正文清洗 + 同会话去重 | `internal/adapter/chat_reply_delivery.go` 增剥离/去重步骤 | 是 |
| E 提示词 | FAQ 文案去冲突 + 约束优先级 | `system_settings.ai_knowledge_config`（已改，待发布） | 配置即时；代码需发布 |
| F 意图 | `refund` 与 `fulfillment` 互斥优先级 | `ai_scope.go` 意图判定 + `ai.go` 内容层优先级 | 是 |
| G 输出 | 编造拦截的确定性后置校验 | 生成后校验层（未建）——当前仅提示词约束 | 是（新增） |
| H 可观测 | 归因布尔写入修复（已合）+ 发布 | `internal/db/ai_reply.go` `boolToInt`（已合，待发布） | 是 |
| I 部署 | "合 main → 发布镜像"门禁 | `.github/workflows/docker-publish.yml` 触发链 | CI |

> **落点原则**：全部落在既有层内（准入/引擎/AI 子层/事实源/配置/可观测/CI），不新增并行链路。Agent 层不承担校验修复（`agent/doc.go` 约束 1）。

---

## 六、预期效果（分数）

> 置信度说明：以下为**基于既有实测增量的区间估计**，非承诺值。历史实测显示"提示词单改"仅 +6.7pp，故不夸大。

| 修复批次 | 动作 | 预期指标变化 | 依据 |
|---|---|---|---|
| 第 1 批（启用+发布） | A 自检 + I 发布镜像 + H 归因上线 | 内容层 73.3%→**80%**（兑现已合代码）；归因从 0 行→可出分布 | 提示词修复实测 +6.7pp |
| 第 2 批（事实源） | D 规格反推 + 渠道 card-channel 绑定 + 履约注入 | 内容层 80%→**85%±2**；delivery 场景由"回避"转"直答" | 12 真实中 7/12 为发货回避，事实源直击 |
| 第 3 批（意图+输出） | F 退款解耦 + G 凭证清洗/去重 + 编造后置校验 | 内容层 85%→**88%±2**；compliance 编造再降 | `[12]` 单点、compliance 3/7 有编造 |
| 第 4 批（评测扩容） | 补多轮/已付款/compliance 样本 | 分数**可能回落 3~5pp**（更真实），但可信度上升 | 12→60 条曾从 91.7% 降到 73.3% |

**关键判断**：真正的瓶颈不是模型，而是 **I 部署缺口 + A/B 静默失败 + D 事实缺失**。在镜像发布前，任何"分数提升"都停留在离线评测，不反映线上。

---

## 七、评测样本需增加哪些

当前 60 条 = 12 真实多轮 + 48 合成单轮。缺口与补充清单：

| 缺口 | 需补充的样本类型 | 数量建议 | 对应问题 |
|---|---|---|---|
| **多轮状态** | "已付款后追问渠道/链接/退款"多轮会话 | +10 | D/F/link-code |
| **退款歧义** | "没夸克→那能退吗"型：refund 应在 fulfillment 语境中正确触发 | +6 | F |
| **凭证质量** | 断言外发正文**不含**水印乱码、**不重复**发送 | +6 | G |
| **合规编造** | 枚举"治愈/必过/无限期/永久"类诱导，断言唯一兜底话术 | +8 | G |
| **归属门禁反向** | 商品不在 `item_info` → 期望**不回复**（四通道全跳过） | +5 | B |
| **启用态决策** | `ai_enabled=0` / `api_key` 空 → 期望不接管 | +4 | A |
| **规格事实** | 同商品"多少集/几季/几册"跨会话一致性断言 | +6 | D |
| **链接失效** | "链接打不开/提取码不对/重发"确定性应答 | +6 | link-code |
| **真实增量** | 每周从生产只读导出新增真实会话 | 滚动 +N | 全部（防过拟合） |

**原则**：合成样本必须覆盖"多轮 + 状态前置（已付款/已发货）+ 反向断言（不该回复）"，否则会像 12→60 那样把真实通过率从 91.7% 打回 73.3%。目标评测集 **≥100 条**，真实:合成 ≈ 1:3。

---

## 八、跑分情况（时间线）

| 时间 | 评测集 | 通过率 | 变化 | 备注 |
|---|---|---|---|---|
| 10-10 20:15 | 12 | 75.0% | 基线 | 初始 |
| 10-11 02:04 | 12（3 采样） | 91.7% | +16.7pp | 采样增多，噪声显现 |
| 10-11 02:07 | 46 | 69.6% | — | **加入 34 条合成后骤降** |
| 10-11 02:34 | 47 | 76.6% | — | |
| 10-11 02:46 | 60 | 73.3% | — | 主基线 |
| 10-11 06:59 | 60 | **80.0%** | +6.7pp | 提示词/事实修复后 |
| 目标 | ≥100 | ≥85%→90% | — | 待发布镜像后重跑 |

**跑分可信度提示**：
1. 12 条样本下 91.7% 是**假绿灯**，不可作为质量结论。
2. 当前 80% 的样本 `Model`/`Judge` 均为 `deepseek-flash`，与生产模型未必一致；judge 换更强模型可能下调 3~5pp。
3. 归因表 0 行期间，分数与线上真实接管率**无映射关系**——离线评测的是"配置+提示词"，线上跑的是"旧二进制"。

---

## 九、结论与下一步

**结论**：客服回复有效性的瓶颈按影响排序为 **部署缺口(I) > 启停静默失败(A/B) > 事实缺失(D) > 提示词冲突(E) > 意图歧义(F) > 输出质量(G) > 度量缺失(H)**。模型能力不是主要矛盾。

**下一步（按依赖顺序）**：
1. 发布镜像（兑现已合代码：归因/负向词/事实/FAQ）→ 立刻跑归因分布，把"答对率"落到数字。
2. 加 A 自检 + B 告警（两个静默失败点）。
3. D 事实源补全（规格反推 + 渠道绑定确认）。
4. F/G 意图解耦与凭证清洗，随下一镜像发布。
5. 评测集扩到 ≥100 条（多轮+状态前置+反向断言），再重跑定基线。
