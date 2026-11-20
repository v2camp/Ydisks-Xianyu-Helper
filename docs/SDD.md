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
| A1 | 评测口径修正 | `已生效` | 提交 `c9200229`；2026-11-07 在容器网络内完成真实模型跑分（`deepseek-flash`，70 条，采样 1 次）：通过率 72.9%(51/70)、内容层 70.3%(45/64)、决策层 100%(6/6)、判定失败 0，门禁通过。分层与按标签报告均正常产出 → 口径已可用 | `docs/eval-harness-validity-diagnosis-20261101.md` §7 |
| A2 | 发货渠道事实化覆盖率 | `在树` | `ai_live_context.go:47` 的 `deliverySourceCard`、`cards.delivery_channels`、`internal/netpan` 均已建。**差**覆盖率：27 个在售商品仅 21 个映射到渠道；A4 只在 `card-channel` 下断言，覆盖不到的商品仍靠提示词。**线上证据**(11-07/11-14 两次只读)：`ai_generation_log.grounding_source=card-channel` 维持 3/4(75%)，机制对已映射商品已生效；覆盖率缺口(27 中仅 21 映射)仍开；gen_log 自 11-04 起无新 AI 流量 | 同上文档 · `docs/customer-service-roi-optimization-20261018.md` |
| A3 | FAQ 通用话术降权 | `已发布` | 配置 `system_settings.ai_knowledge_config` 线上已确认含发货 FAQ 降权条目（match：百度\|度盘\|...\|夸克\|迅雷，answer 降级为「以商品实际为准」，11-14 只读核对）。无评测侧独立信号故不标已生效 | `docs/customer-service-agent-upgrade-plan.md` §12 |
| A4 | 生成后确定性校验兜底 | `已发布` | 提交 `2cc93e09` 已随镜像 `672a9310` 发布（`build_time 2026-11-07T16:00:03Z`，`/health` 已核）。守卫四层齐备：价格守卫 / 回避共现判定 `03079506` / 合规守卫 `0d7b055a` / **话题门控**（本轮）。**跑分(11-07 门控后)**：通过率 77.1%(54/70)，守卫仅触发 4 条且全通过，**守卫导致的回归 0 条**（门控前为 5 条）；`delivery` 7/7、`compliance` 5/7。**差**线上真实信号：`ai_generation_log` 迄 11-04 仍只有 `llm`/`policy-min-price`，`policy-guard`/`policy-compliance` 需等新流量。**残留**：#42 裸「不是」、#39 发票、#41 时效属事实缺口，不在守卫能力内。**11-14 只读**：`ai_generation_log` 迄 11-04 仍无 `policy-guard`/`policy-compliance` 行 → **仍非已生效**，待真实流量出现对应 `answer_source` | 同上 §12 |
| A5 | 多意图优先级（refund vs order） | `在树` | `IntentRefund` 已 allowAI（10-17 修问题 F）。**差** `ai_scope.go:268` 的 `decide` 是纯 OR，同时命中时两份 FAQ 一起进提示 | 同上 §12 |
| A6 | AI 启用态与凭据启动自检 | `已发布` | 提交 `76542eed` 已随 2026-11-01 镜像发布(`build_time 2026-11-01T15:37:20Z`)；`ai_master_enabled=true`、两账号 `ai_enabled=1`，`ai_decision_log` 录得 `taken`(迄 11-07) → 自检链路**已生效**。G1(商品归属)依赖具体消息，不在自检范围 | 同上 §12 |

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

> 部署记录（2026-11-01）：镜像重建 `7eb796e3` → 运行镜像 `d870a64f`（`v1.0.40-local-20261023-30-gd870a64f`，`build_time 2026-11-01T15:37:20Z`），app-1 `Up 5d healthy`。
> 部署记录（2026-11-08 00:08 +0800）：镜像重建 `672a9310` → 运行镜像 `v1.0.40-local-20261023-44-g672a9310`（`build_time 2026-11-07T16:00:03Z`），app-1 `Recreated` 后 `healthy`，迁移版本 67（无新迁移），启动无 error/warn。
> 线上核验（2026-11-07 只读）：`ai_master_enabled=true`；两账号 `ai_reply_settings.ai_enabled=1`；`ai_decision_log` 录得 `taken`=4(接管,迄 11-07)、`no-intent`=14、`item-sync-requested/failed`=6；`ai_generation_log` 4 行，`grounding_source=card-channel` 占 3(75%)。→ **AI 默认开启已生效**。
> 线上核验（2026-11-08 00:10 只读，重启后）：`ai_master_enabled=true`、`ai_api_key` 在位（长度 91，不回传明文）、两账号 `ai_enabled` 仍为 1；`ai_generation_log` 仍 4 行（`llm`=3、`policy-min-price`=1），最新 11-04 → 新镜像尚无 AI 流量，守卫的线上信号待观察。
> `ai_reply_review_mode` 线上无行(从未启用,删除零影响),写入现返回 400。

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 |
|---|---|---|---|
| E1 | 语义反转：无行 = 跟随总闸（默认开），有 0 = 显式关 | `已发布` | `02132065`：`Get` 无行返回默认结构（`ai_reply.go`），`ai.go` 闸门改为总闸 + 账号显式值；`ListEnabledCookieIDs` 改 `COALESCE(ai_enabled,1)=1`，`evalharness` 裸 SQL 同步 |
| E2 | 全局总闸 `ai_master_enabled`（缺省为开，走普通读取不写审计） | `已发布` | `02132065`：`ai_master_enabled` 走 `Settings.Get` 普通读，未进敏感白名单（已断言不产生审计行）；缺省为开 |
| E3 | 契约与 UI：`effective_enabled` + `source`，设置页顶部总闸开关 | `已发布` | 后端 `02132065` 已返回 `effective_enabled` / `source`；前端总闸开关待前端提交 |
| E4 | 意图白名单放开：order → inquiry → consult（spec / usage 待 A2） | `已发布` | `b0160659`：`order`/`inquiry`/`consult` 三处 `allowAI=true`，`spec`/`usage` 仍 false；6 处「只标注」断言同批改正 |
| E5 | 自检与人工确认口径同步 | `已发布` | `02132065`：`Ready()` 改为「总闸开 && 无凭据问题」，「无启用行」不再是故障；`ai_reply_review_mode` 随 E6 整键删除，不再需要联动提示 |
| E6 | 移除人工确认模式（UI + 逻辑 + 契约） | `已发布` | `adf80b0d`：`reply_review.go` 与 213 行测试已删，`NewReplyService` 收敛为 8 参，设置校验改为拒写该键。**线上原值仍未核对（O9）** |
| E7 | AI 密钥 provider（TTL + single-flight + 写时失效） | `已发布` | 提交 `2cd7750e`(`ai_key_provider.go`,256 行)已随 2026-11-01 镜像发布；模型密钥改走带 TTL 与 single-flight 的缓存，消除每回复一行审计(日 300 条 ≈ 9000 行/月)，fail-closed 语义保留 |
| E8 | 准入层异步补齐缺失商品（方案 C） | `已发布` | `f762c353`：新增 `item_backfill.go`（去重 + 24h 负缓存 + 按账号限流 + single-flight），单商品详情走新窄接口不改 `mtop.Client`，`platform_unresolved` 永久短路已修；原因码 `item-sync-requested` / `item-sync-failed` |
| E9 | Agent 开关归并到统一开关（保留 preset 配置） | `已发布` | `c0008544`：`KeySupportEnabled`、`agent_enabled` 读取、4 个接口 enabled 字段已删；`ResolveSession` 恒 true；并修掉写档位顺带写 `ai_enabled=0` 的隐患 |
| E10 | `source` 只认「显式保存过开关」 | `已发布` | `808322f9`（合并 `d870a64f`）：定性为缺陷——契约规定 `source=account` 表示「账号显式设置」，实现却用「行存在」冒充。未提交开关时改落 NULL，两条写入通路统一；`AIEnabledExplicit` 改判列是否有值。**顺带修掉只在 PostgreSQL 暴露的 `ON CONFLICT DO UPDATE` 列名歧义（SQLSTATE 42702）**，SQLite 测不出。生产实测：只改护栏 PUT 返回 200 且 `ai_enabled` 原值保留 |

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
| O9 | 删人工确认前的线上原值确认（是否真有人用） | 只能查生产库，需用户授权执行；**E6 已按产品决定先删**，本项转为事后核对：若线上原值为开，则删后开始外发 | 同上 §5.2 |
| O10 | 存量 `ai_enabled=0` 的行有多少是旧版写档位误伤 | 需在生产库跑只读诊断（`docs/sql/ai-enabled-zero-audit.sql`）。**本机 `~/Library/Application Support/YdisksXianyuHelper` 只有 `data-key`、没有库文件**，桌面包从未在此初始化本地库，故不能靠本机副本代替；且 `ai_decision_log` 仅留 30 天，「查不到 account-off」不等于「从未关闭」 | 同上 §2 |

### 1.7 F 组：Agent 控制台拆分（工作区实现中）

| 编号 | 事项 | 状态 | 判定依据 / 还差什么 |
|---|---|---|---|
| F1 | AI 设置 + Agent 中心重拆为系统设置 / 客服 Agent / 运营 Agent，并补运营 QQ 会话与客服接管历史 | `已发布` | 合并提交 `b17ca6e2`；运行镜像 `ydisks-xianyu-helper:local`（`1e7da7c9f4db`，`v1.0.42-local-20261120`，`build_time 2026-11-19T16:40:06Z`）已部署，`/health` 返回 `ok`，`/api/v1/agent/ops/conversations` 与 `/api/v1/agent/support/conversations` 均 200；发布前账号已停用、核对后已恢复 | `docs/architecture/agent-console-split-plan.md` |

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
| `docs/architecture/agent-console-split-plan.md` | Agent 控制台拆分与运营 Agent 对话模块设计 | 已落地（v1.0.42-local-20261120） |
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

## 5. 周度记录

### 2026-11-07

**数据可得性**：本次成功只读连接生产库（`docker exec` 进 `ydisks-xianyu-helper-postgres-1`，纯 SELECT，未触发迁移）。样本库 `data/evalexport/samples-all.jsonl` 仍为 10-11 导出，本周无新导出 → 规则层用旧快照，归因分布用线上实时（降级说明）。

**离线评测（dry-run）**：70 条消息；9 条可机检期望全部未通过（均为发货渠道回避话术「以商品详情/拍下后发货消息为准」，期望 百度网盘/夸克网盘）；6 条 decision 层通过（escalation）；其余 46 条为 dry-run 不可判定（无期望或无历史 AI 回复）。9/9 失败与 SDD 判定一致（A2/A4 仍在树未生效），无新增失败、无从失败变通过。

**「在树≠已发布」复核（单独成行）**：
- A1/A4 代码已随 2026-11-01 镜像发布（`d870a64f`,build 15:37Z），但缺真实跑分 / `policy-guard` 信号，仍判 `在树`。
- A6 镜像已发布且线上 `ai_decision_log` 录得 `taken`(迄 11-07) → 升 `已发布`(且已生效)。
- E7 实为提交 `2cd7750e` 已发布，原标 `未开始` 为误判 → 修正为 `已发布`。

**状态变化**：E7 未开始→已发布；A6 在树→已发布(且 AI 默认开启整体已生效,见 §1.5)；A2 补线上证据(card-channel 75%)；A4 补证据(无 policy-guard 行)。

**归因分布（线上实时,30 天窗口）**：

| 维度 | 分布 |
|---|---|
| grounding_source | card-channel 3/4(75%)、none 1 |
| answer_source | llm 3、policy-min-price 1 |
| 决策接管 | taken 4、no-intent 14、item-sync 6；接管率≈22%(小样本) |

**真实跑分（11-07 18:52，容器网络内，`deepseek-flash`，70 条 × 1 次）**：通过率 72.9%(51/70)，内容层 70.3%(45/64)，决策层 100%(6/6)，判定失败 0，门禁通过（基线 75%、容差 5pp、下限 70%）。按标签：`delivery` 7/7、`escalation` 6/6、`bargain` 5/5、`baseline` 2/2 全通过；`compliance` 2/7、`order` 1/3、`fulfillment` 2/4、`human-correction` 12/20 为失败集中区。19 条失败构成：回避/让看详情约 10 条、合规编造 3 条（包邮 / 正品 / 退款话术）、未达「额外判定要求」4 条。→ **发货渠道回避已由模型+提示词解决**，下一准确性焦点转为合规编造。

**合规守卫落地（11-07）**：提交 `0d7b055a` 新增 `guardCompliance`，按「枚举具体项 + 明确禁令 + 唯一默认话术」改写退款禁语（`不支持退款` 等）、无依据包邮承诺、显式正版否定（`不是正版/正品`）；新增 `answer_source=policy-compliance` 保留归因粒度，engine 包全绿、vet/commentlint 通过。残留：#42 违规是裸「不是」无法安全改写、#39 发票与 #41 时效属事实缺口，均不在确定性守卫能力内。

**优先级**：A1 已转入 `已生效`；未落地项仍按 准确性 > 通用性 > 热点 > 性能。准确性内部焦点由「发货回避」转向「合规编造（`compliance` 2/7）」，已由 A4 合规守卫部分承接；B/C/D 全组仍 `未开始`（本周无对应提交）。

#### 2026-11-07 续：守卫话题门控、评测接入守卫、发布

**测量缺口先补**：`internal/evalharness` 此前从不调用 `guardDeflection`/`applyAnswerGuards`，跑分测的是模型裸输出，`policy-guard` 结构上不可能出现——A4 的收益无法被测量。提交 `80be4ee7` 新增 `engine.ApplyReplyGuardsForEval` 与 `reconstructLiveFacts`（从快照系统提示重建最小事实集），评测链路自此复用与生产同源的守卫；`report.go` 增「确定性守卫改写：N 条」表头与逐条「兜底」行。

**跑分暴露守卫净收益为负**：接入后 17 条触发，逐条对比守卫前后同一样本，改善 3 条、回归 5 条；表观 +2 条提升全部来自采样噪声（6 条无人为改写的翻转）。回归形态一致——买家问发票/赠品/格式/设备兼容/发货进度，守卫把回复追加或替换成渠道结论。根因是作用域：`deliveryAnswerConstraint` 只在「买家问到网盘或发货方式」时承诺直答，守卫却无此门控。

**修复（提交 `2cc93e09`）**：`guardDeflection` 增加买家问句话题门控，判定词与提示词作用域对齐（单独「发货」不算渠道问句，避免误吞时效/履约）；规格事实改用属性组匹配（问「多少集」可用「1-4季」，问「PDF还是视频」不得用集数事实顶替），无同属性事实时放手；合规守卫的包邮分支改为分句级（整句替换曾把「在吗」的问候整段换成运费限定）。新增 17 条边界用例 + 3 条真实回归话术用例。

**门控后跑分（11-07 23:15 起，容器网络内，`deepseek-flash`，70 条 × 1 次）**：通过率 **77.1%(54/70)**，内容层 75.0%(48/64)，决策层 100%(6/6)，判定失败 0，门禁通过。守卫仅触发 4 条（全部 `policy-compliance`，全部通过），**守卫导致的回归 0 条**。按标签：`delivery` 7/7、`fulfillment` 4/4、`escalation` 6/6、`bargain` 5/5、`link-code` 8/9、`compliance` 5/7（较守卫前 2/7 提升）；`human-correction` 11/20 仍为最大失败池，且失败集中在发票/退款/时效/地址/物流类事实缺口，非守卫能力范围。

**发布（11-08 00:08 +0800）**：镜像重建 `672a9310`，`/health` 返回 `v1.0.40-local-20261023-44-g672a9310`、`build_time 2026-11-07T16:00:03Z`（晚于提交），app-1 重建后 healthy，迁移版本 67 无新迁移，启动日志无 error/warn。A4 由 `在树` 升 `已发布`。

**仍未生效的部分（单独成行）**：`ai_generation_log` 迄 11-04 仍只有 `llm`=3、`policy-min-price`=1，`policy-guard`/`policy-compliance` 尚无线上行 → A4 **不是** `已生效`，需真实流量出现对应 `answer_source` 才能转态。

**测量口径警示**：单样本 + 非零温度下，逐条通过与失败会随机翻转（本轮 6 条无人为改写翻转），因此**不接受单次跑分的逐条差分作为守卫归因**；归因只看「该条是否被守卫改写」（`GuardSource` 非空）。

### 2026-11-14

**数据可得性**：只读连接生产库成功（`docker exec` 进 `ydisks-xianyu-helper-postgres-1`，纯 SELECT，未触发迁移）。运行镜像仍为 `672a9310`（`build_time 2026-11-07T16:00:03Z`，`/health` 已核），自 11-07 起无新部署；HEAD 的 4 个提交（含 `e7750a55 docs(sdd)`）均不在运行镜像内，但无客服 AI 代码改动。`samples-all.jsonl` 仍为 10-11 导出，本周无新导出 → 规则层用旧快照（降级说明）。

**离线评测（dry-run）**：70 条；9 条可机检期望全部未通过（均为 10-11 样本历史回避话术「以商品详情/拍下后的发货消息为准」，期望 百度/夸克网盘）；6 条 decision 通过（escalation）；其余 55 条判定失败（dry-run 无历史 AI 回复可判）。9/9 失败与 11-07 一致，**无新增失败、无从失败变通过**（样本库未更新、规则层无代码改动）。

**「在树≠已发布」复核（单独成行）**：运行镜像仍 `672a9310`（11-07 构建）。A4（提交 `2cc93e09`）随该镜像已发布，但线上迄 11-04 仍无 `policy-guard`/`policy-compliance` 行 → **仍非已生效**。A2/A5 代码随镜像在树，但覆盖率缺口与 `decide` 纯 OR 未解 → 仍 `在树`。HEAD 4 提交（连接器/前端/文档）不在运行镜像，且不影响客服 AI 项。

**状态变化**：A3 `在树`→`已发布`（11-14 只读确认 `system_settings.ai_knowledge_config` 线上含发货 FAQ 降权条目，此前因本机无库未确认）；其余项状态不变。

**归因分布（线上实时,30 天窗口）**：

| 维度 | 分布 |
|---|---|
| grounding_source | card-channel 3/4(75%)、none 1（与 11-07 同） |
| answer_source | llm 3、policy-min-price 1（仍无 policy-guard/compliance） |
| 决策接管 | taken 4、no-intent 18、item-sync 6；接管率≈14%(4/28)，近期流量全部 no-intent（gen_log 最新 11-04） |

**优先级**：未落地项仍按 准确性>通用性>热点>性能。准确性焦点：A2（9/9 失败直接根因之一，覆盖率缺口）居首，A5（多意图 OR）次之；B/C/D 全组仍 `未开始`，本周无对应提交。
