# 上游同步评估报告（2026-10-04）

- 上游：`origin` = https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
- 本地基线：`29026cb`（tag `v1.0.18-local-20260930`）
- **执行状态：四个上游提交已全部处置完毕并合入 `main`（2026-10-05）。** 第 1 批见第七节、第 2 批见第八节、第 3 批见第九节。
- 对应本地 tag：`v1.0.19-local-20261005`（两个缺陷修复）、`v1.0.20-local-20261005`（订单同步三项收紧）、`v1.0.21-local-20261005`（多商品关键词）。
- 位置：`main...origin/main` **ahead 152 / behind 4**
- 合并基：`643c702` v1.0.13 Release Note（2026-09-17）

## 一、结论摘要

| 结论 | 内容 |
|---|---|
| 是否要合 | **要，但不能整体 merge。** 4 个提交里有 2 个是本地同源存在的正确性缺陷，必须补；另 2 个需拆分/决策 |
| 推荐做法 | **逐个 cherry-pick，不要 `git merge origin/main`** |
| 立即做 | `34eda75` + `c2c41f2`（已验证零冲突，纯缺陷修复） |
| 需决策 | `42a476f`（含删除 `item_detail.go`，与本地 20+ 引用点冲突） |
| 可暂缓 | `1672791`（PR54 多商品关键词，feature，5 个文件真冲突） |

整体 merge 会产生 120 个冲突文件，其中 47 个是 webui 构建产物噪声（两边各自重新构建导致 hash 文件名不同，产生 rename/rename 与 modify/delete）。这类噪声会让真正需要人工判断的冲突被淹没，是本次不建议整体 merge 的主要原因之一。

## 二、落后提交清单

| # | 提交 | 日期 | 主题 | 性质 |
|---|---|---|---|---|
| 1 | `1672791` | 09-19 | 合并 PR54：支持多商品关键词并修复关联前端门禁 | Feature |
| 2 | `34eda75` | 09-21 | 防止买家订单误触发自动发货 | **缺陷修复** |
| 3 | `c2c41f2` | 09-21 | 修复同步帧遗漏后续付款消息 | **缺陷修复** |
| 4 | `42a476f` | 09-21 | 收紧订单同步、自动回复与规则匹配边界 | 混合（含破坏性删除） |

## 三、逐项必要性评估

### 3.1 `34eda75` 防止买家订单误触发自动发货 —— 🔴 必须合并

- 上游新增 `internal/automation/order_role_gate.go`（231 行）+ 测试（328 行），并在 `events.go` 增加 `orderRoleConflict`（角色字段互相矛盾时拒绝进入任何自动化分支）。
- **本地状态**：`order_role_gate.go` 不存在。`internal/automation/events.go:88` 仅有基础的 `if f.orderRole == "buyer"` 单行拒绝（继承自 v1.0.13），**没有**协议缺角色时用本地订单/商品事实补证的完整门禁，也没有矛盾字段检测。
- 本地 `events.go` 的自有改动集中在 `updateKey` 布局与免拼触发（`6c5a4a3`、`fa2d745`、`87930ea`），与本次修复**不重叠**。
- 风险：协议未携带角色时把买家订单当作卖家订单自动发货，属资损级缺陷。
- **摘取代价：零冲突**（`merge-tree` 模拟 exit=0）。

### 3.2 `c2c41f2` 修复同步帧遗漏后续付款消息 —— 🔴 必须合并

- 上游把 `extractSyncPayload`（只取 `body.syncPushPackage.data[0].data`）重写为 `extractSyncPayloads`，遍历 `data[]` 全条目并保留无效条目的可观测告警。
- **本地状态**：`internal/xianyu/ws/sync.go:110` 仍是 `extractSyncPayload` 单条实现，第 123-127 行仍是 `arr[0]` 取值 —— **同源缺陷确认存在**。
- 后果：同一同步帧中第 2 条及以后的付款卡片被丢弃，导致漏发货 / 漏自动回复，且现象隐蔽（只在多消息同帧时出现）。
- **摘取代价：零冲突**（`merge-tree` 模拟 exit=0）。

### 3.3 `42a476f` 三项边界收紧 —— 🟡 需要决策，不可直接整体合

该提交含三件事，风险差异很大：

| 子项 | 内容 | 本地影响 |
|---|---|---|
| a | 批量订单同步仅使用已售列表，避免详情接口风控 | `refresh_service.go` 改 120 行，可 auto-merge，**建议采纳** |
| b | 自动回复以本地商品归属为唯一门禁；账号级付款规则仅在明确授权时匹配带规格订单 | `reply_identity.go` 重构、`event_pipeline.go` 等，可 auto-merge，**建议采纳** |
| c | **删除** `internal/xianyu/mtop/item_detail.go`（241 行）及其测试 | ⚠️ **与本地冲突，需决策** |

**关于 c 的冲突**：本地 `main` 的 `item_detail.go` 相对合并基有实质投入 —— `a32709e fix(mtop): 缺少签名令牌改为刷新补齐后重试`，即把「缺少签名令牌」与「令牌过期」同列为可刷新恢复的前置条件（`IsMissingSignTokenErr`），并保留原始错误原因。上游则在同提交中连同调用方一起删除该文件。

本地对该文件的引用点共 20+ 个文件，包括：

```
internal/adapter/analytics_repository.go      internal/adapter/api_delivery_client.go
internal/adapter/item_catalog.go              internal/adapter/item_publish.go
internal/adapter/item_sync.go                 internal/adapter/order_repository.go
internal/application/analytics/models.go      internal/application/analytics/service.go
internal/application/items/batch_local_publish.go
internal/application/items/catalog.go         internal/application/items/catalog_mutation.go
internal/application/items/single_publish.go  ...
```

**两种处置路线**：

- **路线 A（保留本地文件，推荐，已采用）**：只采纳 a + b，冲突处置为「保留本地 `item_detail.go`」。文件继续被 analytics / catalog / publish 等调用；订单同步路径因 a 已改为走已售列表，不再调用详情接口，同样达到规避风控的目的。代价：保留了上游已删除的代码路径。
  - ⚠️ **必要补充（执行后修正）**：仅保留文件**不足以编译**。上游同提交还删除了 `mtop.ItemDetailAPI` 常量与 `ClientImpl.ItemDetailURL` 字段，而 `item_detail.go:105/107` 正依赖这两个符号。路线 A 必须连带恢复它们，否则 `go build ./...` 直接失败。详见第八节。
- **路线 B（跟随上游彻底删除）**：收益是彻底消除详情接口风控面；代价是要同步清理本地 20+ 个引用点，且需先确认 analytics / 商品发布链路是否有本地替代口径。**工作量显著，不建议与本次缺陷修复混做。**

> ~~注：`item-detail-degrade` 与上游 a 项是同一风控问题的两种解法，须避免双重降级。~~
> **执行后更正**：该判断不成立。本地 `item-detail-degrade`（`e389c1c`）改的是**商品同步** `internal/adapter/item_sync.go`（多规格探测复用），上游 a 项改的是**订单同步** `internal/application/orders/refresh_service.go`（仅用已售列表）。两者作用于不同链路，不存在叠加或双重降级，可共存。

### 3.4 `1672791` PR54 多商品关键词 —— 🟢 可暂缓

- Feature 性质（规则支持多商品关键词 + 前端 `ItemMultiSelect` 组件 + openapi 契约变更）。
- 摘取代价：**5 个 Go 文件真冲突** —— `internal/account/manager.go`、`internal/adapter/adapter.go`、`internal/engine/account.go`、`internal/engine/reply.go`、`internal/engine/reply_extra_test.go`。
- 本地规则系统已自行演进 152 个提交，这些文件均有本地改造。
- 建议：仅在确实需要「一条规则匹配多个商品」时再单独拉，且与本次缺陷修复分开做。

## 四、冲突面全景（整体 merge 时）

| 类别 | 数量 | 说明 |
|---|---|---|
| webui 构建产物 | 47 | `internal/webui/static/assets/*.js` 的 rename/rename、modify/delete 噪声，可直接用本地产物覆盖后重新构建 |
| Go 源码内容冲突 | 5 | `account/manager.go`、`adapter/adapter.go`、`engine/account.go`、`engine/reply.go`、`engine/reply_extra_test.go` —— 均由 `1672791` 引发 |
| modify/delete | 2 | `item_detail.go`、`item_detail_test.go` —— 由 `42a476f` 引发 |
| index.html | 1 | 构建产物，同上 |

`34eda75`、`c2c41f2` 相关的 `events.go`、`center.go`、`scheduler.go`、`action_executor.go`、`run_coordinator.go`、`task_preparation.go` 在试算中**全部自动合并成功**。

## 五、建议执行顺序

```bash
# 前置：建 worktree（禁止在主工作区直接开发）
git worktree add .worktree/upstream-sync -b upstream/sync-20261004

# 第 1 批：两个零冲突缺陷修复
git cherry-pick 34eda75 c2c41f2

# 第 2 批：42a476f 采用路线 A，冲突处置为保留本地 item_detail.go
git cherry-pick 42a476f
#  -> 对 item_detail.go / item_detail_test.go 选「保留本地版本 (ours)」
#  -> 复核 refresh_service.go 与已合入的 item-detail-degrade 是否出现双重降级

# 第 3 批（按需）：PR54
git cherry-pick 1672791   # 需人工解 5 个文件冲突
```

每批之后按 `AGENTS.md` 要求：`tools/commentlint` 注释门禁 + 受影响包单测 + 核心链路覆盖率回归。冻结项（滑块验证码）不受本次任何提交影响，无需触碰。

## 六、未验证清单（需在执行阶段确认）

第 1 批执行后，原清单第 1、2、5 项已核实并关闭，剩余如下：

1. ~~`order_role_gate.go` 与本地免拼 / 待发货续跑逻辑是否语义叠加~~ —— **已核实关闭**：`buildTriggerKey` 优先复用 `roleVerificationTaskKeyField` 标记，两处延期共用同一 `TaskKey` 空间，不会产生重复延期记录；砍价待刀成的实际交互见第七节。
2. ~~`c2c41f2` 与本地 `cbabb16` 出站确认改动是否同区域~~ —— **已核实关闭**：合并后 `ws/sync.go` 同时保留本地 `WithOutgoingRequestID` / `outgoingEchoFromResponse` 与上游 `extractSyncPayloads`，两套逻辑并存未相互覆盖。
3. `42a476f` 采纳 a 项后，本地 `internal/adapter/analytics_*` 与 `application/analytics` 的数据口径是否受影响（仍走 item_detail），未验证。
4. 上游提交自 2026-09-21 起未经本地真实账号回归；`34eda75`、`c2c41f2` 均属 WS 报文解析路径，建议用本地 Playwright 夹具而非真实平台验证。
5. ~~冲突结论基于内存试算~~ —— **已核实关闭**：实际 `cherry-pick` 落盘结果与 `merge-tree` 试算一致（零冲突）。
6. **新增待验证**：WS 砍价待刀成卡片在真实平台上是否携带买卖方向字段。若普遍缺失，实时免拼信号会退化到订单同步轮询（延迟数分钟）才生效；功能不丢，但实时性下降。需线上观察或抓样确认。

## 七、第 1 批执行记录（2026-10-04）

### 7.1 执行结果

| 项 | 结果 |
|---|---|
| 分支 | `.worktree/upstream-sync` → `upstream/sync-20261004` |
| `git cherry-pick 34eda75 c2c41f2` | 零冲突，与 `merge-tree` 试算一致 |
| 提交 | `38700fa`（上游原提交保真）、`9f1c5c7`（上游原提交保真）、`0ba0d69`（本地适配） |
| 合入 `main` | `94ffebf`，`--no-ff` 保留任务边界 |
| 改动规模 | 22 文件，+982 / -95；新增 `order_role_gate.go`(231) 与 `order_role_gate_test.go`(328) |

### 7.2 验证证据（均在 `golang:1.26` 容器内执行）

```bash
go build -o /tmp/xianyu-server ./cmd/server          # 通过
go vet ./internal/automation/... ./internal/xianyu/ws/...   # 通过
go run ./tools/commentlint -mode check -root .       # 通过
go test -count=1 ./internal/automation/... ./internal/xianyu/ws/...
```

| 包 | 语句覆盖率 | 结果 |
|---|---|---|
| `internal/automation` | 87.9% | ok（117.9s） |
| `internal/xianyu/ws` | 85.2% | ok（2.4s） |

未启用浏览器集成开关，无真实平台账号依赖。

### 7.3 执行中发现的问题与处置

**现象**：合并后 `TestBargainPendingSootheGuardNeedsReview` 失败（基线 `main` 上通过，属本次引入）。

**根因**：`34eda75` 的 `isWebSocketShippingTask` 把 `TriggerBargainPending` 与 `TriggerOrderPaid` 并列纳入发货阶段门禁。该用例构造的 WS 任务未带 `OrderRole`，于是被「未知角色延期待核验」分支先行拦截（`deferUnknownRoleTask` → `return true, nil`），不再触达它本要验证的安抚话术内容门禁，断言拿不到上抛。

**判定**：属上游的合理设计（砍价免拼确实导向发货），不是需要回退的缺陷。本地生产路径也未受损——订单同步来源的待刀成任务 `Source` 为 `ordersync`，不进入该门禁，WS 缺角色事件延期后仍由同步轮询兜底重投。

**处置**：
- `TestBargainPendingSootheGuardNeedsReview` 补 `OrderRole: OrderRoleSeller`，使其回归验证内容门禁的本意；**全部断言原样保留，未放宽任何判据**。
- 新增 `TestBargainPendingWithoutRoleIsDeferred`，固化合并后的新行为：缺角色待刀成事件写入一条延期任务、不执行免拼、不发送消息。上游 `order_role_gate_test.go` 只覆盖 `TriggerOrderPaid` 与 `TriggerOrderCreated`，**砍价场景此前无覆盖**，此用例补上了这个缺口。

### 7.4 附带的架构门禁修复

全量验证时发现 `34eda75` 使 `internal/automation/center.go` 由 787 行增至 **824 行**，越过阶段二架构门禁的 800 行上限，属本次合并**引入的新违规**（基线另有 4 项历史违规）。

处置：把门禁接线按职责抽到 `order_role_gate.go` —— 新增 `authorizeWebSocketSellerRoleGate`（收口核验、延期写入与拒绝日志三段分支）与 `roleVerificationMarker`（供 `buildTriggerKey` 沿用延期防重键）。行为等价，`center.go` 降至 **799 行**。修复后门禁违规项回落到基线的 4 项，**本次零新增**。

## 八、第 2 批执行记录（2026-10-05）

### 8.1 结果

| 项 | 结果 |
|---|---|
| 分支 | `.worktree/upstream-p2` → `upstream/sync-p2` |
| `git cherry-pick 42a476f` | 冲突仅 `item_detail.go` / `item_detail_test.go` 两处 modify/delete，与试算一致 |
| 冲突处置 | 路线 A：`--ours` 保留本地文件 |
| 提交 | `03b2192`（原作者 Christ 保留，提交信息追加本地处置说明） |
| 合入 `main` | `f34017a`，`--no-ff`；tag `v1.0.20-local-20261005` |
| 改动规模 | 30 文件，+456 / -558 |

### 8.2 执行中发现的问题与处置

**现象**：按路线 A 保留 `item_detail.go` 后，`go build ./...` 失败——

```
internal/xianyu/mtop/item_detail.go:105:16: c.ItemDetailURL undefined
internal/xianyu/mtop/item_detail.go:107:14: undefined: ItemDetailAPI
```

**根因**：`42a476f` 不只删文件，还删除了 `mtop.ItemDetailAPI` 常量与 `ClientImpl.ItemDetailURL` 字段，而保留下来的 `item_detail.go` 正依赖它们。**评估阶段曾误判为「全仓无引用、路线 A 可行」，该结论由 `grep` 命令被 `&&` 短路而未真正执行验证所致**——编译才是唯一可靠的判据。

**处置**：在 `client.go` 按 base 原貌恢复这两个符号，并补充中文注释说明本地为何保留。同时更正评估中「双重降级」的判断（见 3.3 更正注）。

### 8.3 验证证据

```bash
go build ./...     # 通过
go vet ./...       # 通过
go run ./tools/commentlint -mode check -root .   # 通过
go test -count=1 ./...                            # 54 个包全部 ok
```

| 检查 | 结果 |
|---|---|
| 全仓 Go 单测 | 54 个包全部通过 |
| 阶段二架构门禁 | 仍为基线 4 项历史违规，**无新增**；`engine/account.go` 由 812 降至 809 行 |

### 8.4 教训

评估阶段的静态判断（尤其依赖 `grep` 的结论）必须由编译或测试实证，不能以「看起来无引用」作为可合并的依据。

## 九、第 3 批执行记录（2026-10-05）

### 9.1 关键发现：PR54 夹带了一次不相关的架构重构

`1672791` 名为「支持多商品关键词」，实际混合了两类改动：

| 类别 | 文件 | 处置 |
|---|---|---|
| 多商品关键词（feature） | `keywords/service.go`、`server/keyword_handlers.go`、`api/openapi.yaml`、前端规则页与 `ItemMultiSelect` 组件 | **采纳** |
| 回复发送架构重构 | 把回复服务发送端口由 `engine.MessageSender` 换成 `engine.ReplyDelivery`，新增 `chat_sending.go` 聊天应用投递，删除 `reply_image_dimensions.go`，波及 `account/manager.go`、`adapter/adapter.go`、`engine/account.go`、`engine/reply.go` 等 | **跳过** |

**证据**：cherry-pick 产生的 13 处冲突全部集中在第二类文件；第一类文件全部自动合并成功。这说明两类改动彼此独立，可以分离。

**跳过重构的理由**：本地回复链路有插件层 find_stuff 回复、AI 回复人工确认闸门、图片尺寸处理、分段重试等自有投入（`PluginReplier`、`ReplyReviewNotifier`、`resolveReplyImageDimensions`），与上游 `MessageSender → ReplyDelivery` 的替换正面冲突。收益（发送能力换一种实现）不足以抵偿风险（重写本地已验证的回复链路）。

**做法**：`git cherry-pick -n` 应用全部改动后，把第二类文件 `git checkout HEAD --` 回退，只保留第一类。

### 9.2 结果

| 项 | 结果 |
|---|---|
| 分支 | `.worktree/upstream-p3` → `upstream/sync-p3` |
| 提交 | `229310d` |
| 合入 `main` | `920df39`，`--no-ff`；tag `v1.0.21-local-20261005` |
| 改动规模 | 53 文件，+1150 / -221（含前端产物重建） |

### 9.3 验证证据

| 检查 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | 通过 |
| Go commentlint / 前端 commentlint | 通过 |
| `tsc --noEmit` | 通过 |
| 前端契约检查 `api:check` | 通过 |
| 规则相关前端用例 | 8 文件 46 测试全通过（含新增 `ItemMultiSelect.test.tsx`） |
| 前端架构门禁 `featureArchitecture` | 11/11 通过，本次新增组件未违反依赖边界 |
| Go 全仓单测 | 54 个包全部通过 |
| 阶段二架构门禁 | 维持基线 4 项历史违规，**无新增** |

前端产物已按新源码重新构建（旧 hash 文件删除、新 hash 生成），否则嵌入的 Web UI 不会包含多选组件。

### 9.4 关于前端全量 vitest 的说明

本机跑全量前端测试存在 vitest worker 启动超时：基线（未含本次改动）为 `featureArchitecture` 8 项失败，本次为 10 项失败。分目录重跑确认 `featureArchitecture` 实际 **11/11 通过**、规则相关全绿，故判定为资源竞争导致的超时，**非本次改动引入**。

### 9.5 最终同步状态

四个上游提交全部处置完毕。注意：因采用 cherry-pick 而非 merge，`main` 与 `origin/main` 的提交对象不同，`git status` 仍会显示「behind 4」——这是 cherry-pick 工作流的正常表现，**内容已全部引入**，勿据此重复合并。
