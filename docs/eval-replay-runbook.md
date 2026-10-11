# 发布前评测运行手册（cmd/evalreplay）

> 日期：2026-10-11
> 关联：`docs/traffic-replay-evaluation-plan.md`（可行性评估与分层设计）、`docs/agent-evaluation-plan.md`（评测机制 v1.0）

## 一、这套东西解决什么

把生产事故转成**可回放的样本库**，每次开发后用真实模型重跑一遍，产出报告与指标，
并给出门禁结论：**通过率不得跌破「基线 − 容差」**。

三个环节各自独立、可单独重跑：

| 环节 | 命令 | 作用 |
| --- | --- | --- |
| 采集 | `cmd/evalexport` | 从生产库只读导出事故样本（对话全记录 + 重建的系统提示） |
| 回放 | `cmd/evalreplay` | 回放样本库，真实模型评测，出报告与门禁 |
| 基线 | `data/evalexport/baseline.json` | 记录上次通过率与逐条状态，用于跨版本对照 |

## 二、模型配置在哪里配

**结论：生产运行配置在数据库（Web UI → 系统设置），评测配置在项目 `.env`。**

| 用途 | 位置 | 键 |
| --- | --- | --- |
| 生产（客服 Agent 运行） | 数据库 `system_settings` | `ai_api_key` / `ai_api_url` / `ai_model` |
| 评测（发布前评测） | 项目根 `.env` | 见下 |

评测有三种配置方式，按需选择：

### 方式一：零配置（推荐先用这个）

`evalreplay` 只读项目 `.env` 里已有的 `DATABASE_URL` 与 `XIANYU_DATA_KEY`，
从数据库取**当前生效**的模型与密钥（只读解密，不打印、不落盘、不触发迁移）。

评测与生产用同一套模型配置，测的就是线上真实行为。**不需要额外填写任何东西。**

### 方式二：评测走独立模型端点

适合想用更强模型做对照、或不想动生产配置的场景。在 `.env` 中取消注释并填写：

```
EVAL_BASE_URL=https://api.deepseek.com/v1
EVAL_API_KEY=
EVAL_MODEL=
```

`EVAL_BASE_URL` 与 `EVAL_MODEL` 必须同时配置，否则会明确报错而不是静默回落到数据库。

### 方式三：判定模型与评测模型分离

判定端固定 `temperature=0` 以保证判定可复现。如需独立判定模型：

```
EVAL_JUDGE_BASE_URL=
EVAL_JUDGE_API_KEY=
EVAL_JUDGE_MODEL=
```

留空时判定端复用被测端点。

### 采样与门禁阈值

```
EVAL_SAMPLES_PER_CASE=1
EVAL_TOLERANCE=0.05
```

- `EVAL_SAMPLES_PER_CASE`：每个样本采样几次。**被测模型保持生产温度（0.7）**，
  真实模型有非确定性；对抖动大的样本可调到 3，按多数表决抑制噪声。
- `EVAL_TOLERANCE`：允许的通过率劣化幅度，默认 0.05（5 个百分点）。

命令行参数优先于 `.env`，`.env` 优先于默认值。

## 三、怎么跑

### 先自检（不花钱、不调模型）

```
go run ./cmd/evalreplay -root <项目根> -dry-run
```

自检用样本中记录的**历史真实回复**跑判定链路，验证「样本库 + 判定规则」是否可用。
它验证的是链路，不是当前系统质量：历史回避回复被判失败是正确结果。

### 再真实评测

`DATABASE_URL` 里的主机名是 compose 服务名，宿主机解析不了，因此真实评测要在容器网络内执行：

```
docker run --rm --network ydisks-xianyu-helper_default \
  -v /tmp/evalreplay-linux:/evalreplay:ro \
  -v <项目根>/.env:/eval/.env:ro \
  -v <项目根>/data/evalexport:/data \
  alpine:latest /evalreplay -root /eval -samples /data/samples.jsonl \
  -out /data/replay -baseline /data/baseline.json
```

交叉编译 Linux 版：

```
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/evalreplay-linux ./cmd/evalreplay
```

常用参数：

| 参数 | 说明 |
| --- | --- |
| `-limit N` | 只跑前 N 条，用于冒烟 |
| `-cases N` | 覆盖采样次数 |
| `-tolerance F` | 覆盖容差 |
| `-update-baseline` | 评测后把本次结果写为新基线 |

## 四、判定分层与门禁规则

判定优先规则、无法规则化时才调用判定模型：

| 层 | 依据 | 成本 | 说明 |
| --- | --- | --- | --- |
| `structure` | 系统提示是否包含必需片段 | 零 | 提示词组装本身错了，模型再强也不判通过 |
| `rule` | 回复是否满足可机检期望 | 零 | 期望来自样本标注或自动推导 |
| `judge` | 判定模型评估回复是否恰当 | 有 | 温度 0，输出 JSON 结论 |

**门禁规则（刻意区分总分与单条）：**

- **总分做门禁**：通过率跌破「基线 − 容差」即退出码 1，构建失败。
- **单条只做告警**：单条样本由通过转失败会列入报告的「较基线退化」清单，
  但**不直接拦截**。原因是真实模型本身有噪声，逐条严格判定会产生大量误报。
- 判定失败的样本**不计入分母**，单独统计并在门禁说明中告警。

退出码：`0` 门禁通过；`1` 门禁未通过；`2` 参数或运行错误。

## 五、样本库与期望

样本库由 `cmd/evalexport` 产出，落在 `data/evalexport/samples.jsonl`。
`/data/` 已被 `.gitignore` 忽略——**样本含生产对话原文，禁止提交或外发**。

每条样本包含：完整聊天记录、AI 对话记录、重建的系统提示、事故标签。

### 期望从哪来

`expectation` 可人工标注，也可自动推导。当前自动推导只做一条高置信规则：
**系统提示已声明「确定发货方式」时，要求回复至少给出其中一个渠道，且不得出现回避话术。**

其余场景返回空期望，交给判定模型。

人工标注示例（写进样本的 `expectation` 字段）：

```json
{"must_contain_any": ["百度网盘"], "must_not_contain": ["以商品详情为准"], "note": "人工标注"}
```

### ⚠️ 改代码后必须重新导出样本库

样本里的系统提示是**导出时点的代码重建**出来的。改了提示词相关代码后，
必须重新跑 `cmd/evalexport`，否则回放用的还是旧提示，测不出修复效果。

## 六、已知限制

1. **样本量小**：当前 12 条，指标波动大，单次通过率的绝对数值意义有限，看趋势与回归更有价值。
2. **重新导出会覆盖人工期望**：`evalexport` 目前不保留已有 `expectation`，重导前需备份。
3. **被测模型非确定性**：`EVAL_SAMPLES_PER_CASE=1` 时单条结果可能有抖动，重要结论建议采样 3 次。
4. **样本库只增不减会腐化**：需按 `docs/traffic-replay-evaluation-plan.md` §5.1 的三层治理（核心回归集/滚动热点集/归档）定期清理。
5. **判定模型自身有偏差**：只做趋势参考，关键结论仍应人工复核报告中的回复原文。
