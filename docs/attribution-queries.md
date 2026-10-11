# 归因查询手册

> 日期：2026-10-11
> 依赖：迁移 `00064`（`ai_decision_log`，决策归因）、`00065`（`ai_generation_log`，生成归因）与 `00066`（`automation_run_steps`，发货步骤留痕）
> 前提：**部署并跑一段时间后才有数据**；三张表默认保留 30 天，随 `log_retention_days` 清理。

三张表回答三类问题，分工明确：

| 表 | 回答的问题 |
|---|---|
| `ai_decision_log` | 这条消息**为什么被接管 / 为什么没被接管** |
| `ai_generation_log` | 接管之后**是谁答的、依据什么答的** |
| `automation_run_steps` | 发货链路**卡在哪一步** |

---

## 一、这条回复是策略给的，还是模型自己写的？

价格守卫命中越界时会**整段替换**模型原稿，替换后的文本看起来同样流畅，
不记录来源就会把策略产出误当成模型能力。

```sql
SELECT answer_source AS 来源,
       count(*) AS 次数,
       round(100.0 * count(*) / sum(count(*)) OVER (), 1) AS 占比
FROM ai_generation_log
WHERE created_at > now() - interval '30 days'
GROUP BY answer_source
ORDER BY 次数 DESC;
```

取值含义：

| 值 | 含义 |
|---|---|
| `llm` | 模型自己写的 |
| `policy-no-discount` | 报价越界且不可再降，**策略拒绝话术**替换 |
| `policy-min-price` | 报价越界，**策略最低价模板**替换 |
| `empty` | 模型没有产出任何内容（空回复） |

**空回复要单独看**——这是硬故障，评测已实测出现过：

```sql
SELECT created_at, cookie_id, chat_id, message_preview
FROM ai_generation_log
WHERE answer_source = 'empty'
  AND created_at > now() - interval '7 days'
ORDER BY created_at DESC;
```

---

## 二、发货答案是靠卡密事实，还是靠文本规则猜的？

这是「发货渠道能力建设好并启用了吗」的直接证据。

```sql
SELECT grounding_source AS 依据来源,
       count(*) AS 次数,
       round(100.0 * count(*) / sum(count(*)) OVER (), 1) AS 占比,
       string_agg(DISTINCT grounding_detail, ' | ') AS 涉及渠道
FROM ai_generation_log
WHERE created_at > now() - interval '30 days'
GROUP BY grounding_source
ORDER BY 次数 DESC;
```

取值含义：

| 值 | 含义 | 是否事实 |
|---|---|---|
| `card-channel` | 渠道来自**卡密正文识别**（发货规则 → 卡密 → 渠道提取链路） | ✅ 是事实 |
| `delivery-config` | 来自自动化发货配置的**模板消息文本** | ❌ 文本规则 |
| `item-text` | 来自**商品描述关键词探测**（最弱） | ❌ 文本规则 |
| `none` | 无任何渠道线索 | — |

**判读方式**：

- `card-channel` 占比高 → 卡密渠道能力已启用并生效，发货结论**基于事实**。
- 大量落在 `item-text` → 能力建好了但**没启用或没生效**，仍在靠描述关键词猜。
  常见原因：卡密未填写含链接的正文、发货规则未绑定卡密、渠道提取失败。
- `delivery-config` 有量 → 配置模板消息里写了渠道，属于文本规则，不是事实。

逐条查还在用文本规则回答发货的会话：

```sql
SELECT created_at, cookie_id, chat_id, grounding_source,
       message_preview AS 买家问, reply_preview AS 回复
FROM ai_generation_log
WHERE grounding_source IN ('item-text', 'delivery-config')
  AND created_at > now() - interval '7 days'
ORDER BY created_at DESC
LIMIT 50;
```

---

## 三、最不可信的组合

**用文本规则当依据、又被策略话术覆盖**的回复，说明模型在这条上没有产出可用信息：

```sql
SELECT count(*) AS 次数
FROM ai_generation_log
WHERE answer_source <> 'llm'
  AND grounding_source IN ('item-text', 'delivery-config')
  AND created_at > now() - interval '30 days';
```

---

## 四、跨表联查：为什么这条没被接管？

决策表与生成表按 `cookie_id + chat_id + item_id` 关联，可还原一条消息的完整链路：

```sql
SELECT g.created_at,
       g.message_preview AS 买家问,
       d.takeover          AS 是否接管,
       d.reason            AS 不接管原因,
       g.answer_source     AS 答案来源,
       g.grounding_source  AS 依据来源,
       g.reply_preview     AS 回复
FROM ai_generation_log g
LEFT JOIN (
    SELECT cookie_id, chat_id, item_id, takeover, reason
    FROM ai_decision_log
) d ON d.cookie_id = g.cookie_id AND d.chat_id = g.chat_id AND d.item_id = g.item_id
WHERE g.created_at > now() - interval '7 days'
ORDER BY g.created_at DESC
LIMIT 100;
```

---

## 五、区分「模型波动」与「配置变更」

`ai_decision_log.config_fingerprint` 记录判定所用边界配置的指纹。
指纹变化而指标同步变化，说明是配置改动导致；指纹未变而指标波动，是模型随机性：

```sql
SELECT config_fingerprint AS 配置指纹,
       count(*) AS 判定次数,
       sum(CASE WHEN takeover = 1 THEN 1 ELSE 0 END) AS 接管次数,
       sum(CASE WHEN reason = 'negative' THEN 1 ELSE 0 END) AS 负向词拦截,
       sum(CASE WHEN reason = 'no-intent' THEN 1 ELSE 0 END) AS 无意图未接管
FROM ai_decision_log
WHERE created_at > now() - interval '30 days'
GROUP BY config_fingerprint
ORDER BY 判定次数 DESC;
```

---

## 七、发货链路卡在哪一步？

`automation_runs` 只有终态 `status` 与 `error_message`，**无法回答"卡在哪一步"**。
`automation_run_steps`（迁移 `00066`）按动作游标逐步留痕。

### 某次运行的全过程

```sql
SELECT id, attempt_count AS 重试代次, action_cursor AS 游标,
       step AS 阶段, action_type AS 动作, status AS 结果,
       latency_ms AS 耗时毫秒, error AS 原因, created_at
FROM automation_run_steps
WHERE run_id = <运行ID>
ORDER BY id;
```

### 哪个动作类型最常失败

```sql
SELECT action_type AS 动作, status AS 结果, count(*) AS 次数,
       round(avg(latency_ms)) AS 平均耗时毫秒
FROM automation_run_steps
WHERE created_at > now() - interval '30 days'
GROUP BY action_type, status
ORDER BY 动作, 次数 DESC;
```

### 被门禁拦截的内容分布

内容门禁与链接健康检查会拦截发货正文——这是**主动保护**，不是故障，
但集中出现说明卡密内容本身有问题（链接失效、含违规词）：

```sql
SELECT action_type, count(*) AS 次数,
       left(error, 80) AS 原因摘录
FROM automation_run_steps
WHERE step = 'quarantine'
  AND created_at > now() - interval '30 days'
GROUP BY action_type, left(error, 80)
ORDER BY 次数 DESC
LIMIT 30;
```

### 反复重试仍失败的运行

同一 `(run_id, action_cursor)` 在不同 `attempt_count` 下多次失败，
说明重试无效，更可能是内容问题而非网络抖动：

```sql
SELECT run_id, action_cursor, count(DISTINCT attempt_count) AS 尝试次数,
       count(*) AS 步骤记录数, max(status) AS 最终状态
FROM automation_run_steps
WHERE created_at > now() - interval '30 days'
GROUP BY run_id, action_cursor
HAVING count(DISTINCT attempt_count) >= 3
ORDER BY 尝试次数 DESC
LIMIT 30;
```

---

## 八、⚠️ 仍然无法回答的问题：买家到底收到没有

这是当前可观测性的**最后一块空白**，也是风险最高的一项。

即使动作执行全部成功（`automation_run_steps.status = success`），
也只能证明**平台接口返回成功**，不能证明买家实际收到。以下情况系统无感知：

- 平台静默拦截（限流、风控）——接口返回成功，消息未送达；
- 买家未看到消息（折叠、被吞）；
- 网盘链接本身失效——内容发到了，但打不开。

要闭环需要**买家侧确认信号**，建议方向（尚未实施）：

1. 发出后 N 分钟内，检测买家回复中是否含「收到 / 谢谢 / 好了」等确认词；
2. 未确认且已超过阈值时，把该运行标记为「疑似未送达」并进人工核对；
3. 识别需谨慎：买家也可能回复「没收到」，两种语义必须区分。

**当前可行的替代做法**：遇到纠纷时，先查 `automation_run_steps` 判断
「动作是否真的发出去」。如果步骤全是 success，说明问题在平台侧或买家侧，
不在我们的链路——这本身就是有价值的归因结论。

---

## 九、指标口径说明

- `answer_source = 'empty'` 的记录**不计入**评测通过率的分母（判定失败单独统计），
  但在归因时要单独看：空回复是硬故障，不是「没答对」而是「没答」。
- 单条样本存在模型随机性。做趋势判断时建议观察 30 天窗口而不是单日。
- 三张归因表都是**当前写入、按时间清理**，不保留修改历史；需要长期趋势请定期导出。