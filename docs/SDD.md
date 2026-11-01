# SDD · 唯一待办与状态真相文档

> **SDD = Single Source of Truth Document（单一真相文档）。**
> 本文件是本仓库**唯一**的未完成待办清单与状态真相源。
> 任何「还有什么事没做完」的问题，答案只在本文件；其它文档只描述方案、设计与证据，不再各自维护待办列表。

## 0. 使用纪律

### 0.1 唯一性

| 允许 | 禁止 |
|---|---|
| 本文件登记未完成待办与状态 | 在其它文档新增「遗留」「待办」「后续」清单 |
| 其它文档写方案、设计、证据、复盘 | 在周报里给出新的优先级排序（只允许在本文件重排） |
| 引用本文件编号（如 `A4`） | 用不同编号体系重复描述同一件事 |

**唯一例外**：`docs/architecture/refactoring-master-plan.md` 仍是「六阶段重构」的阶段状态来源。
该重构六个阶段**已全部完成**，不再新增阶段；本文件只引用、不复制其内容。

### 0.2 四态状态机

| 状态 | 判定依据（必须可机检） |
|---|---|
| `未开始` | 代码树无对应实现，或配置键未写入 |
| `在树` | 代码已合入 main，`git log -S` 能定位提交 |
| `已发布` | 运行镜像构建时间晚于该提交（`/health` 的 version 与构建日期） |
| `已生效` | 线上归因表或评测报告出现预期信号 |

**`在树` ≠ `已修好`**：2026-10 三周的失效全部源于把「已合 main」当成「已上线」。
状态为 `在树` 时必须在「判定依据」列写清**还差什么**。

### 0.3 更新规则

1. 状态变更与判定依据必须在同一次改动内更新本文件。
2. 新增计划文档必须在 §3 索引登记，否则视为孤儿文档。
3. 声称某项完成时，本文件对应行必须已是 `已发布` 或 `已生效`。
4. 排序尺度固定为 **准确性 > 通用性 > 热点 > 性能**，同档内按「是否卡住其它项」排。

## 1. 待办总表

### 1.1 P0 准确性

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 | 方案出处 |
|---|---|---|---|---|
| A1 | 评测口径修正 | `在树` | 提交 `c9200229`；`commentlint`/门禁全绿。**差**一次真实模型跑分（需容器网络内或 `EVAL_*`） | `docs/eval-harness-validity-diagnosis-20261101.md` §7 |
| A2 | 发货渠道事实化覆盖率 | `在树` | `ai_live_context.go:47` 的 `deliverySourceCard`、`cards.delivery_channels`、`internal/netpan` 均已建。**差**覆盖率：27 个在售商品仅 21 个映射到渠道；A4 只在 `card-channel` 下断言，覆盖不到的商品仍靠提示词 | 同上文档 · `docs/customer-service-roi-optimization-20261018.md` |
| A3 | FAQ 通用话术降权 | `在树` | 配置 `system_settings.ai_knowledge_config` 已改。**差**线上版本号确认（本机无库） | `docs/customer-service-agent-upgrade-plan.md` §12 |
| A4 | 生成后确定性校验兜底 | `在树` | 提交 `76542eed`：`reply_guard.go` + `applyAnswerGuards` 已合入；4 条真实回避话术全部被改写。**差**一次真实模型跑分与线上 `answer_source=policy-guard` 出现 | 同上 §12 |
| A5 | 多意图优先级（refund vs order） | `在树` | `IntentRefund` 已 allowAI（10-17 修问题 F）。**差** `ai_scope.go:268` 的 `decide` 是纯 OR，同时命中时两份 FAQ 一起进提示 | 同上 §12 |
| A6 | AI 启用态与凭据启动自检 | `在树` | 提交 `76542eed`：`engine.AIReadinessProbe` 已登记为生命周期组件，启动即检 + 周期复检。**差**镜像发布；G1（商品归属）依赖具体消息，不在自检范围 | 同上 §12 |

**A2 + A4 是唯一能消掉评测里 9/9 失败组合的解**（A2 让事实进得去，A4 让回避出不来），其余各项不影响这 9 条。

### 1.2 P1 通用性

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 | 方案出处 |
|---|---|---|---|---|
| B1 | 配置漂移检测（线上 vs 代码基线） | `未开始` | 无实现 | 升级计划 §12 |
| B2 | 归因表可用性断言（每周必须有新行） | `未开始` | 无实现 | 同上 |
| B3 | 商品同步失败持续告警 | `未开始` | `adapter/item_sync.go` 只有单次 WARN，无持续告警 | 同上 |
| B4 | 周度样本库自动导出 | `未开始` | `cmd/evalexport` 仍是手动执行 | 同上 |

### 1.3 P2 热点

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 | 方案出处 |
|---|---|---|---|---|
| C1 | 砍价边界话术 | `未开始` | 5 条砍价样本中 2 条失败 | 升级计划 §12 |
| C2 | 目录/库存前后矛盾 | `未开始` | 同会话内数量口径不一致 | 同上 |
| C3 | FAQ 覆盖度补齐 | `未开始` | 高频诉求无对应条目 | 同上 |

### 1.4 P3 性能

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 | 方案出处 |
|---|---|---|---|---|
| D1 | 提示词 token 瘦身 | `未开始` | 商品「描述」注入平台原始 JSON，噪音且占 token | 升级计划 §12 |
| D2 | 多轮上下文裁剪 | `未开始` | 依赖 D1，暂不评估 | 同上 |

### 1.5 E 组：AI 默认开启与一键关闭（P0，方案见 `docs/customer-service-ai-default-on-plan.md`）

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 |
|---|---|---|---|
| E1 | 语义反转：无行 = 跟随总闸（默认开），有 0 = 显式关 | `未开始` | `ai_reply.go:107` 仍返回 `ErrNotFound`；`ai.go:68` 仍按 nil 跳过 |
| E2 | 全局总闸 `ai_master_enabled`（缺省为开，走普通读取不写审计） | `未开始` | `system_settings` 无该键，`ai.go` 无总闸判定 |
| E3 | 契约与 UI：`effective_enabled` + `source`，设置页顶部总闸开关 | `未开始` | `ai_enabled` 仍是唯一字段；前端无全局开关 |
| E4 | 意图白名单放开：order → inquiry → consult（spec / usage 待 A2） | `未开始` | `ai_scope.go:161-175` 四类仍 `allowAI=false`；负向词表保持不动 |
| E5 | 自检与人工确认口径同步 | `未开始` | `ai_readiness.go:47` 仍把「无启用行」当故障；`ai_reply_review_mode` 与总闸无联动提示 |
| E6 | 移除人工确认模式（UI + 逻辑 + 契约） | `未开始` | 触点 6 处：`reply_review.go`(79 行)+测试(213 行)、`settings/service.go:535` 键校验、前端 3 处、前端测试。**前置：先查线上 `ai_reply_review_mode` 实际值** |
| E7 | AI 密钥 provider（TTL + single-flight + 写时失效） | `未开始` | `globalAIConfig` 每条回复都走 `ReadSensitiveSettingForAccount` → 每条回复一行审计；日 300 条 ≈ 9000 行/月 |
| E8 | 准入层异步补齐缺失商品（方案 C） | `未开始` | `canAutoReply` 纯本地查询，`mtop.Client` 无单品详情方法；需负缓存 24h + 按账号限流 + single-flight，新 reason `item-sync-requested` |
| E9 | Agent 开关归并到统一开关（保留 preset 配置） | `未开始` | 待删：`KeySupportEnabled`、`agent_enabled` 列读取、4 个 `/api/v1/agent/support/*` 的 enabled 字段、前端两处开关；默认档保持 `readonly` |

### 1.6 待决策（非工程待办，需产品/用户拍板）

这些不是「没写代码」，而是「等一个决定」，因此不占四态：

| 编号 | 待决策事项 | 卡点 | 出处 |
|---|---|---|---|
| O1 | 客服端是否需要「每会话转人工」按钮 | 产品决策 | `docs/architecture/agent-capability-kernel-plan.md` §13 |
| O2 | 第三方 MCP 连接参数放在租户级还是会话级 | 产品决策 | 同上 §13 |
| O3 | 连接器页「刷新」是否连带重载连接器草稿 | 体验决策 | `docs/architecture/connector-page-plan.md` §7 |
| O4 | QQ 渠道表单是否隐藏渠道级凭据 | 产品决策 | 同上 §7 |
| O5 | 颜色令牌是否做统一治理子项目 | 设计决策 | `docs/architecture/ai-agent-config-domain-plan.md` §11 |
| O6 | Agent 中心依赖行是否支持就地配置 | 架构边界决策（涉及「消费侧不编辑供给」） | 同上 §11 |
| O7 | 买家侧「是否收到」确认信号如何采集 | 产品 + 技术，闭环最高遗留风险 | `docs/attribution-queries.md` |
| O8 | 历史 `ai_enabled=0` 行如何解释（显式关 vs 继承默认开） | 决定 E1 的迁移语义：若一律继承，原先手工关掉的账号会被打开 | `docs/customer-service-ai-default-on-plan.md` §2 |
| O9 | 删人工确认前的线上原值确认（是否真有人用） | 只能查生产库，需用户授权执行 | 同上 §5.2 |

## 2. 索引：方案与设计文档

| 文档 | 作用 | 状态 |
|---|---|---|
| `docs/architecture/refactoring-master-plan.md` | 六阶段重构的唯一阶段状态源 | 六阶段全部完成，不再新增 |
| `docs/architecture/refactoring-progress.md` | 重构各阶段最终提交与验证证据 | 已完成，只读 |
| `docs/architecture/dependency-rules.md` | 包依赖门禁的权威规则 | 生效中 |
| `docs/architecture/comment-standard.md` | 中文注释标准 | 生效中 |
| `docs/customer-service-agent-upgrade-plan.md` | 客服 Agent 总体方案 + §12 看板 | 持续演进 |
| `docs/eval-harness-validity-diagnosis-20261101.md` | 评测口径 7 处缺陷与修复记录 | A1 依据 |
| `docs/customer-service-ai-default-on-plan.md` | AI 默认开启与一键关闭的阻碍清单与整改方案 | E 组依据 |
| `docs/eval-replay-runbook.md` | 评测运行手册 | 生效中 |
| `docs/attribution-queries.md` | 三张归因表 SQL 手册 | 生效中 |
| `docs/architecture/agent-capability-kernel-plan.md` | 能力内核方案（含未决项） | 方案态 |
| `docs/architecture/connector-page-plan.md` | 连接器页设计 | 已落地，留 2 项待决策 |
| `docs/architecture/ai-agent-config-domain-plan.md` | AI 配置域设计 | 已落地，留 2 项待决策 |
| `docs/slider-captcha-frozen-spec.md` | 滑块验证码冻结规范 | 长期冻结，禁止改动 |

## 3. 索引：证据与复盘（只读，不再产出同类新文档）

| 文档 | 时间窗 | 用途 |
|---|---|---|
| `docs/customer-service-weekly-review-20261017.md` | 2026-10-10~17 | 首轮周度复盘 |
| `docs/customer-service-weekly-review-20261024.md` | 2026-10-17~24 | 第二轮 |
| `docs/customer-service-weekly-review-20261031.md` | 2026-10-24~31 | 第三轮 |
| `docs/customer-service-roi-optimization-20261018.md` | 2026-10-11~18 | 117 条真实消息回测，接管率 23.9%→41.0% |
| `docs/customer-service-reply-effectiveness-review-20261101.md` | 累计 | 架构、接线点、问题分类 A–I、双层打分 |
| `docs/customer-service-ai-enablement-diagnosis-20261031.md` | 累计 | 启停四闸门 + 10-08 生产快照硬证据 |
| `docs/eval-harness-validity-diagnosis-20261101.md` | 累计 | 评测失真根因 |

**周度任务此后不再新建周报文档**，只在本文件更新状态列并追加当周记录（见 §0.1）。

## 4. 归档规则

1. 一项进入 `已生效` 后，在本文件保留一行摘要与日期，详细论证留在其方案文档。
2. 方案文档被完全取代时，在 §2 标注「已归档：由 X 取代」，不删除。
3. 本文件超过 300 行时，把 `已生效` 项移入 `docs/archive/sdd-done.md`，本文件只留未完成项。
