# 上游同步评估报告（2026-11-20）

- 上游：`origin` = https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
- 本地：`fork` = https://github.com/v2camp/Ydisks-Xianyu-Helper.git，基线 `245fc2a7`（tag `v1.0.43-local-20261120`）
- 合并基：`643c702` v1.0.13 Release Note（2026-09-17）——与上次评估一致，说明上游未改写历史
- 未合入上游提交：**152 个**（其中 4 个已于 2026-10-05 摘取，见 `upstream-sync-evaluation-20261004.md`；git 仍显示未合因为走的是 cherry-pick）
- 本次新评估：**148 个**
- 本地领先：447 个本地提交（含 MCP、QQ 渠道、Agent 内核、评测链路等自建能力）

## 一、结论摘要

| 档位 | 数量 | 含义 |
|---|---|---|
| **A 立即合入** | 13 | 本地同源缺陷确认存在，改动小、与本地结构不冲突 |
| **B 需决策** | 36 | 口径分歧、结构冲突或新功能前置，需先定方案 |
| **C 不合** | 60 | 已处置 / 已被本地等价实现覆盖 / 不适用（MySQL 已删、冻结项、产物与文案） |
| **D 新功能** | 43 | 本地无对应模块，整包含入不可行，按需另立项 |

**不要 `git merge origin/main`。** 上游这段时间大量重做了前端（运行日志页、销售统计、资源库、卡密台账、移动端），并新增商品级 AI 配置、外部 Agent 连接器、外部 API 管理；这些与本地自建的前端结构与能力正面冲突。仍按「逐个 cherry-pick」执行。

## 二、A 档：建议立即合入（13）

均已在本地代码中实测到同源缺陷（非据标题推断）。

| # | 上游提交 | 主题 | 本地证据 | 冲突 |
|---|---|---|---|---|
| 1 | `848a5267` | 凭证锁自死锁（#101） | `internal/application/orders/refresh_service.go` 327/339/346 持锁调 `UpdateRunningCookie`；落到 `internal/engine/credential_tokens.go` 647 再取同一把 `store.LockAccountCredentials`（`internal/db/store.go:192` 为不可重入 `sync.Mutex`） | 低 |
| 2 | `623e1a2f` | 自动评价累计 200 单后失效（#96） | `internal/db/account_tasks.go` 198-200 候选 SQL 无 `NOT EXISTS` 过滤，已办完订单永久占满 `LIMIT 200` 窗口 | 低 |
| 3 | `5a1d50a5` | 已评价应按成功收口（#81） | `internal/automation/account_tasks.go:438` 只有 `IsRateOrderExpiredErr`；`internal/xianyu/mtop/account_tasks.go` 无 `IsRateAlreadyRatedErr`/`NotRateable` | 低 |
| 4 | `4e435035` | 凭证请求超时误报「请重新登录」 | `internal/engine/connection_coordinator.go:322` 网络类失败走 `notifyOffline`（文案「请更新登录信息或重新登录」），但实际在退避重试 | 低 |
| 5 | `6ab1204b` | 免拼/改价成功后 Cookie 写回冲突误判结果未知（#106） | `internal/automation/action_executor.go:403`、`free_shipping_action.go:94` 仍 `uncertainAction("…但响应凭证保存失败")` | 低 |
| 6 | `39f4627c` | 仅支持 SMTP LOGIN 的服务器认证失败 | `internal/notify/notifier_channels.go:322` 只有 `smtp.PlainAuth` | 低 |
| 7 | `bc1617fb` | 缺幂等键不得重试 API 卡 | `internal/adapter/api_delivery_client.go:256-257` `RetryEnabled` 即 `attempts=apiDeliveryRetryLimit`，未校验出站载荷是否真的带幂等键 | 低 |
| 8 | `0d9bfad5` | 退款中/已取消状态迁移不一致 | `internal/db/orders.go:430` `shouldUpdateOrderStatus` 仅拦 `incoming=="shipped"` 对 `completed/cancelled`，`refunding` 仍可被改回 `shipped` | 低 |
| 9 | `8993a8c9` | 成交金额门禁，修 ¥0 服务预约单误发卡密（#104） | 本地无 `paidAmountBlockedReason`；`db.Order.Amount` 存在但付款链路不做正数校验 | 中 |
| 10 | `07453c1e` | 退款撤回后安全取消的运行不再永久阻断发货 | 同源判定存在 | 低 |
| 11 | `6295dab9` | 退款撤回续跑保留已发卡密的发货凭证 | 同上 | 低 |
| 12 | `0d750f8f` | 缺发货凭证续跑转人工核对，不提交空凭证 | 同上 | 低 |
| 13 | `3d9cbd04` | 扫码人脸/人工验证后长登录凭证收尾遗漏（#98） | 本地 `internal/renewal` 无对应收尾分支 | 中 |

第 10-12 项必须**整组摘取**（同一缺陷的三层递进：先解阻断、再保留凭证、最后缺凭证转人工），单独摘任一条都会留下不一致状态。

## 三、B 档：需决策（36）

### 3.1 付款发货门禁与兜底窗口（9）

`5d0e18ff`、`24819f83`(#91)、`7c32178d`、`d999ddd5`、`0e14127a`、`2dbdd690`、`cf2d2d42`、`d5687134`、`2d24a67a`

上游把「缺订单号不猜订单、金额未确认不发货、已有进度遇已发货即取消」收成一套门禁。本地在同一链路上已有**不同口径**的自建实现（`c4989a2a`、`1943df93`、`17311db5`、`f02a9a47`、`a0dc4da1`、`259d4bdf`）。整组摘取会推翻本地「未知角色事件用会话订单归属收口」的策略，需先定口径再动。

### 3.2 MTOP 节流与风控（3）

`39e76607`（账号级全局限流）、`f151d806`（身份查询/商品同步频率上限）、`c483bcce`（批处理遇风控即停、时隙计时）

方向与本地 `e389c1c9`/`64fd92f2`（详情与砍价探测降频）一致但粒度更系统。建议先评估本地已有降频是否已覆盖，再决定是替换还是补一层账号级令牌桶。

### 3.3 发货凭证原子化（3）

`394570d3`、`cee56e4a`、`e8dd1be8`：把「运行隔离 + 人工处理通知」收敛到同一事务。本地事务边界是自建的（`internal/db` 工作单元），需先对齐再摘。

### 3.4 卡密与库存（5）

`5b18d9d0`（幂等重试前置条件与真实载荷对齐）、`3900e40a`（编辑库存旧快照覆盖发货消费）、`4276cc9e`、`25c3b1a5`/`f0708500`（API 卡密模板三态编辑与完整密钥回显）

上游这组建立在 PR #84 的模板编辑之上，本地没有该 UI，需整包移植而非摘取；其中「向前端下发完整密钥」属安全例外，必须单独评审。

### 3.5 其它需决策

| 提交 | 议题 |
|---|---|
| `cec90f46` | 砍价阶段卡片绕过系统消息门禁——本地免拼触发链自建，`events.go:137` `isBargainPendingCard` 无发送方校验 |
| `1fbe8cff` | 无角色拍下且商品不在本地库时静默跳过；本地 `order_role_gate.go:229` 把 `missing_local_item` 列为可重试，是**有意**延期+通知，属口径分歧 |
| `3b0453a0` | 确认发货命中敏感词后卡在待发货（#113）——可部分摘取告警与放行分支 |
| `7936118e` | 订单同步改按页增量入库——收益真实但改动大，建议单独立项 |
| `122dec45` | WS 应用层心跳 + 不再把 101 判为慢请求——与本地 `process-heartbeat`/看门狗有重叠，需先对齐 |
| `08a7bb82` | 求评价规则的发货时间回溯窗口——纯业务口径，需产品确认 |
| `b6bdea42` | 无头 Chromium 特征补齐（`internal/browser/stealth.go`，非冻结文件但影响风控表现）——需授权后单独验证 |
| `32821a8a`+`58df832a`+`6c5692f9`+`4e2daabb` | 商品级 AI 客服设置——本地只有意图级 `ai_scope.go`，缺商品级维度，属新功能 |
| `fcac03f6` / `1afb20e8` | 关键词多表达式正则 / 关键词分割——本地 `aimatch` 组合词表达式只服务 AI 判定，规则侧无等价能力 |
| `92d17af1` | 四处查询未检查 `rows.Err()`——本地已删 MySQL，需逐点核对剩余位置 |
| `04025e9a` | 系统事件按会话识别已处理订单（#68），消除人工处理误报——可部分摘取 |
| `cddbbb68` | 外部通知首行改「【类型】成功/失败」——本地 `NotificationEvent` 无 `Outcome` 字段，体验改进，可选 |

## 四、C 档：不合（60）

- **已处置（4）**：`16727910`、`34eda75`、`c2c41f2b`、`42a476fa`（2026-10-05 已摘取，勿重复合）
- **已被本地等价实现覆盖**：
  - `0f4182cb` WS 拨号失败永久退出 —— 本地 `connection_coordinator.go:233` `handleConnectFailure` + `isTransientDialError` 已分流，`networkRetryDelay` 带抖动与账号错峰
  - `d88798e2` 确认收货四段键把阶段码当订单号 —— 本地 `events.go:607` 按「首段 15 位以上纯数字」判定，已覆盖该场景
  - `f49b256f` AI 承诺边界（提示词层）—— 本地已有确定性合规兜底（`engine/ai.go:314` 禁语与无依据承诺改写），强于提示词约束
- **不适用**：`d1afae2d`（MySQL 专用，本地已移除 MySQL）、`b2ad6d4b`（即 `4e435035` 的合并提交，冗余）
- **冻结项**：`a058a0e2` 滑块高层轨迹点数 60→90，触碰 `slider.go`/`slider_test.go`/`token_captcha_fallback_integration_test.go` 三个受保护文件，按 `docs/slider-captcha-frozen-spec.md` **禁止合入**（需本次任务显式授权）
- **前端产物 / 体积预算 / vite 配置（7）**：`60d23257`、`8a074578`、`297b75a0`、`3d91c648`、`411af997`、`95a0d1d9`、`4a8fc2c1`
- **测试 / lint / 迁移号补丁（8）**：`5f3c53c8`、`025c3b31`、`7e7878ee`、`d1204f53`、`8266f348`、`b860bc43`、`3af72630`、`2671f88a` —— 跟随上游结构，本地无法单独落
- **文案 / 赞助 / Release Note / 文档（13）**：`2aa608e4`、`561ba547`、`6443eea6`、`33ffd174`、`389066b0`、`6b971fb0`、`6916789a`、`070705d8`、`fbf9e80d`、`34d9650f`、`55501d2d`、`191f30ba`、`87b39651` 等
- **上游 PR 合并提交（14）**：`ea95affd`、`e737607b`、`94ef7877`、`f7a07304`、`3e7b4e15`、`14756daa`、`ade80c27`、`533c876d`、`3358e97d`、`fcaa16b5`、`2dc84bb1`、`7034beb4`、`2546b511`、`13274294` —— 内容由其子提交代表

## 五、D 档：新功能（43）

本地完全没有对应模块（已用代码检索确认无实现），整包含入不可行，按需另立项：

| 功能 | 主要提交 | 规模 |
|---|---|---|
| 本地资源库（图片库 + 商品模板 + Excel 导入 + 共享发货模板） | `97f16f66`、`c894c1fd`、`34c246f3`、`cf94cff0`、`4c239a18`、`005def1b`、`2c2ae4be`、`4c0106e7` | ~120+ 文件 |
| 外部 API 管理与商品平台下架 | `b96fc1b5`、`345f31c2`、`fcaa16b5` | ~108 文件 |
| 外部 Agent 回复连接器 | `6e34812c`、`e98b58db`、`c386baef`、`d3cddfe9` | ~84 文件（本地有自建 Agent 内核，需对比取舍） |
| 运行日志页 / 销售统计页 / 卡密台账 / 自动化规则日志 | `5c31085a`、`bfd5ef37`、`ecfa6dd6`、`dc92074b`、`2a4c3f3c`、`2c894655` | ~250 文件 |
| 移动端导航与订单页改版 | `bdef3889`、`c53c9b50`、`12cd2e8c`、`de0072e9` | ~150 文件 |
| 商品首次回复 / 人工回复后暂停自动回复 | `e73b3ace`、`c6814edd` | ~168 文件 |
| 订单阶段给 AI/外部 Agent（#115）、续期安全验证通道（#116）、MTOP 平台模拟器 | `73122ba4`、`ac072729`、`f69c9693`、`584521c2`、`7d13c459` | ~160 文件 |
| 日志可观测性、浏览器提示音、风控提示、PageHeader、类目推荐等 UI 改进 | `21b00249`、`d92d53a3`、`f8d5d430`、`273c79ec`、`6980a75a`、`704da0b3`、`a2e95f1e`、`081e7f56`、`46111e91` | ~180 文件 |

> `7d13c459`（MTOP 平台模拟器）内含一批「模拟器发现的风控重放缺陷」修复，若不要模拟器本体，可单独评估其修复部分。

## 六、评估中额外发现：本地自有回归（不依赖上游，建议优先修）

2026-10-05 摘取 PR54（多商品关键词）时，为避开上游顺带的回复发送架构重构，把 `internal/engine/reply.go` 整体回退了——**连带丢掉了同提交里的匹配函数**。

- 上游 `1672791` 把 `kw.ItemID == m.ItemID` 改成 `containsItemID(kw.ItemID, m.ItemID)`（按逗号分隔匹配任一商品）。
- 本地现状 `internal/engine/reply.go:300` 仍是 `kw.ItemID == m.ItemID`。
- 而本地存储层确实按多商品写入：`internal/application/keywords/service.go:87` `JoinItemIDs` 用逗号拼接，`internal/server/keyword_handlers.go` 也接受 `item_ids` 数组。

⇒ **凡是关联了两个及以上商品的关键词规则，在本地永远不会命中**（单商品仍正常，所以测试与人工验证都容易漏掉）。修复只需把 `containsItemID` 补回 `reply.go`，并补一条「逗号分隔命中任一」的用例。

## 七、建议执行顺序

1. **先修本地回归**：`reply.go` 补 `containsItemID`（半天，纯本地）
2. **A 档第 1-8 项**：单文件小改，逐条 cherry-pick，每条独立编译+跑受影响包测试
3. **A 档第 9-13 项**：`8993a8c9` 与退款续跑三项需先看 `order_role_gate.go` 现状再摘
4. **B 档**：按 3.1 → 3.2 → 3.3 顺序开专项，每项单独出设计方案，不与 A 档混提交
5. **D 档**：只按产品优先级立项，不跟随上游节奏

## 八、执行纪律（沿用 AGENTS.md）

- 每批独立分支 + worktree，禁止在主工作区改代码
- 每批一条中文提交，提交前跑受影响包测试 + Commentlint + 架构门禁
- 前端若改动，必须重建嵌入产物，否则 Web UI 与后端不一致
- 冻结项（`internal/browser/slider*.go`、`token_captcha*.go`）未经授权一律不动
