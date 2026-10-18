# 客服机器人优化：报告评估与 ROI 排序

> 日期：2026-10-18
> 输入：[2026-10-10 复盘](customer-service-review-20261010.md)、[2026-10-17 周复盘](customer-service-weekly-review-20261017.md)
> 取证：生产 PostgreSQL（只读）、生产容器日志、近 7 天 117 条真实买家消息回测
> 结论：**两报告的主根因判断成立，但都已部分过时；真正卡住收益的不是模型或提示词，而是「线上配置漂移」与「归因埋点静默失效」两项工程问题。**

---

## 一、结论先行

| # | 判断 | 证据 |
|---|---|---|
| 1 | **发货渠道事实源已经建成并生效**，不需要再新建 `item_delivery_channel` 表 | 20/20 卡券有 `delivery_channels`（15 百度、5 双盘）；21/27 商品可经自动化规则映射到卡券渠道；10-18 线上回复已正确答出「这款发的是百度网盘」 |
| 2 | **离线是「配置」不是「代码」**：代码内置词表已扩充，但线上 `ai_scope_config` 仍是 10-10 报告记录的那版旧词表 | 线上 delivery 词表与 10-10 报告 4.2 节原文逐字一致；无 refund/spec/usage 三个意图 |
| 3 | **归因表从部署起一行都没写进去**，导致两报告要求的「量化验收」根本无法执行 | `ai_decision_log` / `ai_generation_log` 均 0 行；日志中 20 条 `unable to encode false into binary format for int4` |
| 4 | 把配置补齐后，**接管率 23.9% → 41.0%（+17.1 个百分点）且零回落** | 对近 7 天 117 条真实买家消息做新旧配置对照判定 |
| 5 | 但**商品同步已停摆 7 天**，归属门禁把四条回复通道一起拦掉，配置收益会被它吃掉大半 | `item_info` 最新更新 10-11 15:23；近 48h 6 次 `令牌为空`；近 24h 4 次「商品不在本地清单，跳过自动回复」 |

---

## 二、对两份报告的评估

### 2.1 判断正确的部分

- **10-10 报告 4.1（事实源缺位 → AI 只能打太极）**：当日成立，是本轮全部问题的起点。
- **10-10 报告 4.3（负向词与售前退款咨询死锁）**：至今仍成立，代码侧刚修复。
- **10-17 报告 3.3（意图只认书面问法、漏「X 吗」句式）**：完全成立，回测复现。
- **两份报告的核心方法论正确**：先分离「决策层接管」与「内容层作答」，再用真实样本量化。这个分层让问题可定位，值得保留。

### 2.2 已被证据推翻或过时的部分

| 报告结论 | 现状 | 影响 |
|---|---|---|
| 10-10 P0-1「新建 `item_delivery_channel` 映射表，从历史链接反向抽取」 | **不需要**。迁移后渠道由卡密正文在写入时自动识别（`netpan.Detect`），落 `cards.delivery_channels`，已覆盖全部 20 张卡券 | 省掉一张表、一个抽取工具和持续维护成本 |
| 10-17 问题 A「`inferDeliveryHints()` 恒返回空」 | 快照（10-10~10-11）时点正确，现已闭环：卡密渠道走 `card-channel` 权威分支 | 该问题不必再投入 |
| 10-17 M3「是非题先给明确结论」 | **已落地**（`internal/engine/ai.go` 提示词第 481-482 行），且另有枚举式禁止编造清单 | 该问题不必再投入 |
| 10-17 M4「改 `ai_scope_config` 为渠道词 + 疑问句式」 | 代码内置词表已扩，但**线上配置未同步**，改动等于没发生 | 这是本轮最高 ROI 的突破口 |
| 6 个商品无发货规则 → 认为事实源仍有缺口 | 6 个均为实物/服务类（音响、内胆包、求职辅导、Agent 定制、两套实体绘本），本就不发网盘 | 不构成缺口，无需补规则 |

### 2.3 两份报告的共同盲区

1. **把「代码里的行为」当成「线上的行为」。** 两份报告都只读代码与样本快照，没有核对 `system_settings` 里的实际配置，也没有核对运行镜像的构建时间。代码改对了、配置没跟上，收益为零。
2. **没有验证观测数据本身可用。** 报告要求「以人工纠正命中率下降为验收指标」，但归因表写不进去，这个指标取不到数。任何基于该表的验收在修复前都是空转。
3. **把「合并进 main」当成「已上线」。** M3、M4 的代码侧改动在 10-11/10-12 就进了仓库，但线上镜像构建于 10-12 15:06，配置从未随之迁移。

---

## 三、生产事实（本次取证）

### 3.1 渠道事实源覆盖

```
cards：20 张，全部识别出渠道
  百度网盘          15
  百度网盘,夸克网盘   5
item_info：27 个在售商品，21 个经自动化规则绑定到带渠道的卡券（77.8%）
```

### 3.2 线上三层配置的落后点

| 配置 | 线上现状 | 代码内置基线 |
|---|---|---|
| `ai_scope_config` 意图数 | 4（bargain / product_qa / delivery / stock） | 7（另有 refund / spec / usage） |
| delivery 词表 | 只认「什么盘 / 网盘发货 / 度盘 / 夸克 / 发货方式」等书面问法 | 已含 百度 / 迅雷 / 网盘 / 下载链接 / 提取码 等口语词 |
| stock 词表 | 只有「第X季 / 单买 / 全集 / 完整版」 | 已含 全剧 / 所有的 / 多少集 / 几集 / 更新完 |
| `negative` | 未配置，走内置（含「退款」整词） | 已收窄为动作表达 |
| FAQ「发货」条目 | 「不同商品发货方式不同，请以商品详情页说明为准…」且注入语标为「必须以此为据」 | 与 `deliveryAnswerConstraint` 直接冲突 |

最后一行是隐形杀手：即使 `DeliveryHint` 置顶并声明优先，同一次注入里仍有一段被标为「必须以此为据」的相反指引，模型会在两者间摇摆。

### 3.4 比配置更前置的阻断点：商品同步已停摆 7 天

本节是本次核对中最重要的发现，它把「配置对齐」的收益上限压低了。

| 观测 | 值 |
|---|---|
| `item_info` 最后更新时间 | **2026-10-11 15:23**（至今 7 天无更新） |
| 商品同步失败 | 近 48h 共 6 次 `FAIL_SYS_TOKEN_EMPTY::令牌为空`（每日 03:00 定时任务） |
| 归属门禁拦截 | 近 24h 4 次 `商品不在当前账号本地商品清单，跳过自动回复` |

链路是：商品同步拿不到令牌 → `item_info` 停留在旧快照 → 新上架商品从未入库 →
`internal/engine/reply_identity.go` 的 `canAutoReply` 判定归属失败 →
**关键词、插件、AI、默认回复四条通道一起被跳过**（不只是 AI）。

也就是说：**即使把意图词表补全到 100%，这些会话仍然一条都进不了 AI。**
配置对齐只能覆盖「商品已在 `item_info` 里」的那部分会话。

因此真实收益排序要修正：商品同步是 P-1（阻断级），它的修复同时解锁
归属门禁、每日擦亮、AI 回复与关键词回复四件事，单位成本收益高于任何提示词或配置改动。


### 3.3 归因表为何是空的

```
level=DEBUG msg=写入 AI 决策归因失败
err="failed to encode args[5]: unable to encode false into binary format for int4 (OID 23)"
```

`ai_decision_log.takeover/negative_hit/extra_hit` 与 `ai_generation_log.guarded` 在两种方言里都是 `INTEGER`，代码却直接传 Go `bool`。SQLite 会自动转换，PostgreSQL 的 pgx 二进制编码拒绝。埋点按设计「失败只记 Debug」，于是**静默失效两个月级别的观测缺口**——不是没写代码，是写了但从没成功过。

---

## 四、ROI 排序与处置

排序口径：单位工程成本的「可验证收益」。已做的两项已在本分支完成并通过门禁。

| 优先级 | 措施 | 收益 | 成本 | 状态 |
|---|---|---|---|---|
| **P-1** | 恢复商品同步（mtop 令牌失效） | 解锁归属门禁、每日擦亮、AI 回复、关键词回复四条通道；否则下方 P0 全部打折 | 需重新登录账号刷新令牌，或修同步链路令牌刷新 | **阻断中** |
| **P0** | 修复归因表在 PostgreSQL 下的布尔写入 | 恢复全部可观测性；无它则后续优化无法验收 | 4 行 + 1 条跨方言回归 | **已实施** |
| **P0** | 线上三层配置对齐（意图补齐 + 词表扩充 + 负向收窄 + FAQ 文案） | **接管率 +17.1pp，零回落**，配置热生效无需重启 | 一次配置写入 | **待批准** |
| **P1** | 负向词只收退款动作表达，解开售前咨询死锁 | refund 意图从「永不可达」变为可用 | 常量改写 + 2 条护栏测试 | **已实施** |
| **P2** | 商品规格事实源（集数 / 册数 / 版本） | 消除「说不准」与「自信断言」自相矛盾 | 需规格抽取 + 注入，约 1~2 天 | 未做 |
| **P3** | 链接失效 / 提取码核对 / 重发的确定性应答 | `link-code` 为最高频售后诉求 | 需引入订单履约只读端口 | 未做 |
| **P4** | 履约状态查询（付款未收到 / 重发） | 闭环「付款—发货—查收」 | 能力扩展，1~2 天 | 未做 |
| **不做** | 新建 `item_delivery_channel` 表 + 历史链接反向抽取 | 已被卡券渠道覆盖 | —— | **建议放弃** |

### 4.1 为什么「配置对齐」能排到 P0

因为它同时满足三个条件：**收益可量化、成本接近零、失败可回滚**。

回测方法：取近 7 天 117 条真实买家消息（`chat_messages` 中 `direction='incoming'` 且为文本），分别用「线上配置」与「建议配置」复刻 `scope.decide()` 判定：

```
样本数: 117
线上配置接管: 28 (23.9%)
建议配置接管: 48 (41.0%)  提升 +20 条 / +17.1 个百分点
新接管(仅建议配置放行): 20
回落(仅线上放行): 0
```

新放行的 20 条全部是报告点名的漏配句式：

```
+ 百度吗                      + 是百度网盘吗（×3）
+ 是百度网盘嘛                 + 是网盘吗
+ 百度网盘吗                   + 百度网盘有4k吗
+ 百度云网盘的吗？我只想要剧本…  + 电视剧是在网盘里面吗
+ 我需要百度网盘               + 这个一共多少集
+ 这个是只到47集完结吗          + 百度没有VIP可以看吗
```

零回落说明扩充没有误伤既有判定——因为新词表是「渠道词 + 完整性词」的并集，不与砍价、投诉语义重叠。

**注意收益上限（两层折损）**：

1. 这 20 条里有渠道事实的是 21/27 商品范围内，能直接答出「发百度网盘」；无渠道事实的少数商品仍走兜底话术。所以 +17.1pp 是**接管率**提升，**答对率**提升会略低。
2. 更关键的是 §3.4 的归属门禁：回测用的是全部买家消息，而线上这些消息里有相当一部分**根本到不了 AI**。所以 +17.1pp 是「商品同步恢复后」的潜在收益，不是明天就能拿到的数字。两项修复必须配套，单独做配置对齐会被门禁吃掉大部分增益。

---

## 五、已实施改动（本分支 `feature/ai-roi-opt`）

### 5.1 归因表布尔写入修复

`internal/db/ai_reply.go`：`AddDecisionLog` 三个布尔列与 `AddGenerationLog` 的 `guarded` 改用既有的 `boolToInt`，并在注释里写清「为什么不能直接传 bool」，避免下次回归。

回归护栏：`internal/db/multidb_test.go` 新增 `TestMultiDB_AIAttributionBoolEncoding`，两种方言都跑，且**断言读回的整数值**（只断言「无错误」抓不到「写进去但存错」）。

有效性已验证——回退修复后该用例在 PostgreSQL 下按预期失败：

```
--- FAIL: TestMultiDB_AIAttributionBoolEncoding/postgres
    AddDecisionLog: failed to encode args[5]: unable to encode true into binary format for int4 (OID 23)
```

### 5.2 负向词死锁解除

`internal/engine/ai_scope.go`：`builtinComplaintExpr` 中的「退款」整词改为动作表达
（`要退款|我退款|给我退|申请退款|已退款|退款吧|退掉|退下款|退了吧`），「退货」保持整词。

护栏：`internal/engine/escalation_guard_test.go` 新增 `TestRefundConsultationReachesAI`，
同时锁两个方向——售前咨询必须由 `refund` 接管，售后动作必须拦截。

### 5.3 验证证据

| 项 | 命令 | 结果 |
|---|---|---|
| 静态检查 | `go vet ./internal/db/ ./internal/engine/` | 通过 |
| 引擎测试 | `go test ./internal/engine/... -count=1` | ok（52.6s） |
| 数据库测试 | `go test ./internal/db/ -count=1` | ok（44.3s） |
| 跨方言回归 | `REQUIRE_MULTIDB=1 go test ./internal/db/ -run TestMultiDB_AIAttribution` | sqlite + postgres 均 PASS |
| 注释门禁 | `go run ./tools/commentlint -mode check -root .` | 通过 |

---

## 六、待批准：线上配置对齐

属于生产数据变更，需要确认后执行。改动只落在 `system_settings` 两个键，不涉及表结构与代码，可秒级回滚。

### 6.1 目标 `ai_scope_config`

```json
{
  "intents": [
    {"id": "bargain", "enabled": true, "ai": true,
     "match": "(便宜|优惠|少点|最低|砍价|降价|打折)|(能不能&(元|块))|(\"\\d+(\\.\\d+)?\\s*(元|块)\"&(卖|行|可以))"},
    {"id": "product_qa", "enabled": true, "ai": true,
     "match": "正版|出版|彩图|音频|目录|pdf|扫描|文字版|内容一样|有什么区别|讲什么|讲的什么|适合|内容"},
    {"id": "delivery", "enabled": true, "ai": true,
     "match": "什么盘|哪种盘|什么网盘|网盘发货|发网盘|资源码|鸿蒙|百度|度盘|夸克|迅雷|阿里云盘|天翼|网盘|怎么发|什么时候发|多久发|发货方式|下载链接|怎么下载|提取码|拍下发|直接拍"},
    {"id": "stock", "enabled": true, "ai": true,
     "match": "全集|全剧|完整版|完结|所有的|全吗|多少集|几集|更新完|更新了吗|单买|\"第[一二三四五六七八九十\\d]季\""},
    {"id": "refund", "enabled": true, "ai": true,
     "match": "能退|可以退|能不能退|支持退|退吗|退款吗|退钱吗|包退|无理由|退款政策"},
    {"id": "spec", "enabled": true, "ai": true,
     "match": "几本|全册|第几册|上册|下册|套装|版本|新版|规格|大班|中班|小班|教材|尺寸|颜色"},
    {"id": "usage", "enabled": true, "ai": true,
     "match": "可以用吗|能用吗|支持吗|可以用|有效期|时间限制|到期|会员|试看|试一下|点读笔"}
  ],
  "negative": ""
}
```

`negative` 故意留空：让内置词表（已收窄）始终生效，未来内置更新不必再改配置。
`spec` / `usage` 放 `ai: true` 是有意的——回测里这两类各只有 1 条，接管后由禁止编造清单兜住，风险低于继续沉默不答。

### 6.2 目标 FAQ「发货」条目

把

> 不同商品发货方式不同，请以商品详情页说明为准；拍下付款后按该商品方式发货，可能是网盘链接、资源码，或先发资源码后发链接。

改为

> 具体网盘以该商品实际发货为准；拍下付款后按该商品方式发货，可能是网盘链接、资源码，或先发资源码后发链接。

删掉与商品级确定渠道冲突的那半句，其余保留。其余 4 条 FAQ（资源码 / 退款 / 更新 / 赠送）不动。

### 6.3 执行与回滚

```bash
# 1. 备份现值（务必先执行）
docker exec ydisks-xianyu-helper-postgres-1 psql -U xianyu -d xianyu -c \
  "CREATE TABLE IF NOT EXISTS system_settings_bak_20261018 AS
   SELECT * FROM system_settings WHERE key IN ('ai_scope_config','ai_knowledge_config');"

# 2. 写入新配置（用上文 JSON，注意 SQL 内单引号需双写）
#    建议通过 Web UI「AI 客服 → 边界/语料」粘贴保存，走既有校验与审计链路

# 3. 观察 10 分钟后核对归因分布（修复镜像上线后才有数）
#    reason: taken 占比应上升；hit_intents 中 delivery/stock 应显著变多
```

回滚：`UPDATE system_settings SET value=(SELECT value FROM system_settings_bak_20261018 WHERE key='ai_scope_config') WHERE key='ai_scope_config';`

### 6.4 前置条件

归因修复要**重新构建镜像并重启容器**才生效（当前镜像构建于 10-12 15:06，不含本次改动）。
建议顺序：先发镜像 → 再改配置 → 用 `ai_decision_log` 对比改前改后的 `reason` 分布，形成闭环证据。

---

## 七、不建议现在做的事

| 事项 | 理由 |
|---|---|
| 新建商品渠道映射表 | 卡券侧已覆盖，再建一张只增加双写不一致风险 |
| 给 6 个无规则商品补发货规则 | 它们是实物/服务类，本不发网盘 |
| 语义向量检索、多轮 SOP、三级护栏 | 属升级计划 §9 的独立里程碑，与当前事实源/意图修复混做会失去可验证性 |
| 用抽象提示词约束替代确定性校验 | 已被两次 benchmark 证伪：抽象表述对模型无效，只有枚举 + 唯一默认话术有效 |

---

## 八、下一步建议

1. **先恢复商品同步**（§3.4）。这是唯一能同时解锁归属门禁、擦亮与四条回复通道的动作，
   在它修复前，任何意图或提示词优化的实测收益都会被门禁稀释。
2. 批准配置对齐（§6），与镜像发布同批执行。
3. 发布后 24 小时，用 `docs/attribution-queries.md` 的 SQL 出第一份真实 `answer_source` / `grounding_source` 分布，作为「事实源是否真正替换文本规则」的判据。
4. 若 `grounding_source` 仍大量落在 `item-text`，说明卡券渠道未绑定到对应自动化规则，再针对性补规则——而不是继续调提示词。
5. 规格事实源（P2）在拿到归因数据后再排期，避免在无法测量的情况下改动。
6. 给商品同步失败加持续告警：当前只有 ERROR 日志，停摆 7 天无人发现，这是比任何单点 bug 都贵的过程缺口。

---

## 附录：本次取证命令（全部只读）

```bash
# 卡券渠道覆盖
docker exec ydisks-xianyu-helper-postgres-1 psql -U xianyu -d xianyu -P pager=off -c \
 "SELECT COALESCE(NULLIF(delivery_channels,''),'(空)'), count(*) FROM cards GROUP BY 1;"

# 商品→渠道覆盖（经自动化规则绑定卡券）
docker exec ydisks-xianyu-helper-postgres-1 psql -U xianyu -d xianyu -c \
 "SELECT count(*) FILTER (WHERE ch IS NOT NULL) AS 有渠道, count(*) AS 全部 FROM (
    SELECT i.item_id, MAX(NULLIF(c.delivery_channels,'')) AS ch
    FROM item_info i
    LEFT JOIN automation_rules r ON r.item_id=i.item_id AND r.deleted_at IS NULL
    LEFT JOIN automation_rule_actions a ON a.rule_id=r.id AND COALESCE(a.card_id,0)>0
    LEFT JOIN cards c ON c.id=a.card_id
    WHERE i.deleted_at IS NULL GROUP BY 1) t;"

# 归因埋点失败证据
docker logs ydisks-xianyu-helper-app-1 2>&1 | grep "归因失败" | tail -5
```
