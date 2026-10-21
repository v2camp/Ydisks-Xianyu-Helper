# AI 与 Agent 配置域重整设计（P1）

> 状态：**设计已确认，未实现**。
> 本文定义「AI 设置 / Agent 中心」两页的职责重构与共享 UI 原语；不定义重构阶段，不替代 `docs/architecture/refactoring-master-plan.md`。
> 范围：子项目 P1。P2（连接器页分区）与 P1 未覆盖的一致性项另有文档，本文不重复。

## 1. 背景与问题

### 1.1 现状事实（已核实的代码事实）

- `AISettings.tsx`（`/app/ai-settings`）承载：AI 模型与凭据、AI 客服接管范围（策略/语料/边界三层 JSON）、AI 回复人工确认、MCP 服务器列表（`mcp.servers`）。
- `AgentCenter.tsx`（`/app/agent-center`）承载：三类 Agent 的入口，其中客服端 Agent 直接配置开关与能力档位，管理端与运营端仅作指引。
- `MCPServiceCard`（对外 `/mcp` 端点、令牌生命周期、调用审计）位于**系统设置页**，而 Agent 中心已把它归入「管理端 Agent」叙事。
- `mcp.servers` 由 `internal/engine/plugin_reply.go` 消费：本服务 AI 引擎作为 **MCP 客户端**调用外部工具（find_stuff 找书/找短剧），属**出站**。
- `MCPServiceCard` 对外提供 MCP 端点供外部 Harness 调用，属**入站**。二者同名不同向。

三个真实问题：

1. **AI 客服链路跨两页**：接管范围在 AI 设置，开关与档位在 Agent 中心；两页都自称在管 AI 客服。
2. **MCP 同名不同向且归属不明**：出站客户端在 AI 设置，入站服务端在系统设置，都不是 Agent 中心宣称的归属。
3. **交互与视觉范式不一致**：保存模式四种、页面外壳两种、加载/错误态三种（含静默失败与状态闪烁）。

### 1.2 目标

1. 两页职责改为「AI 能力供给侧 / Agent 消费侧」，消除概念重叠。
2. 让跨页依赖可见：Agent 中心显示各类 Agent 的前置依赖状态与跳转目标。
3. MCP 服务卡归入其宣称的归属（管理端 Agent）。
4. 在本次落地中抽取共享 UI 原语，供 P1 与后续 P2 复用，避免新代码再次分叉。

### 1.3 非目标

- **不改页面文件与路由**：`AISettings.tsx`、`AgentCenter.tsx` 不改名，路由 `/app/ai-settings`、`/app/agent-center` 不变。
- 不合并两页为单页（见 §2 决策表）。
- 不改任何设置键名、不改后端保存语义与 API 契约。
- 不改外部 MCP 工具（出站）与对外 MCP 服务（入站）的运行语义。
- 本次不涉及连接器页（P2）与 AI 引擎行为。

## 2. 关键决策

| 决策 | 选择 | 依据 |
|---|---|---|
| 页面形态 | 分层保留双页（供给侧 / 消费侧） | AI 引擎能力同时服务客服 Agent 与既有规则通道，分层是真实领域关系而非妥协；Agent 中心文案本身承认这一点 |
| 替代方案 | 否：合并单页 | 单页会把「即时生效的令牌/审计」与「草稿+保存」混同屏，放大保存语义不一致；页面过长、bundle 变大、门禁全量重算 |
| 替代方案 | 否：按 Agent 类型重组 | 共享基础设施归属仍需解释，页面/文件/路由重排幅度最大 |
| MCP 服务卡 | 从系统设置迁入 Agent 中心「管理端 Agent」区块 | 对齐 Agent 中心自己宣称的归属；settings feature 内部移动，不触碰 feature 门禁 |
| 依赖可见 | 依赖行只读 + 跳转，不就地编辑 | 避免两页编辑同一份草稿产生竞态 |
| 跨 feature 数据 | 连接器就绪键声明下沉 `shared/api-contract/settings.ts` | 前端门禁禁止 feature 互引内部文件；该文件已有 `SENSITIVE_SYSTEM_SETTING_KEYS` 先例 |
| 保存提示 | 保存结果改由 `SaveBar` 内联，停用 `appDialog.alert` | 频繁保存不应被模态打断；失败态常驻可重试优于一次性弹窗 |

## 3. 信息架构

### 3.1 AI 设置（供给侧 `/app/ai-settings`）

- 页头副标题：AI 引擎本身的能力；被客服 Agent 与既有规则通道共用。
- 顶部指引条：谁在使用 → Agent 中心。
- 区块顺序：① 模型与凭据（API 地址 / API Key / 模型 + 读取模型、测试连接）② AI 客服接管范围（策略/语料/边界 + 组合词速查）③ AI 回复人工确认 ④ 外部 MCP 工具（出站 `mcp.servers`）。
- 保存：单一页级保存，沿用 `useSettings('ai')` 的 scope 白名单，提交动作迁移到 `SaveBar`。

### 3.2 Agent 中心（消费侧 `/app/agent-center`）

- 页头副标题：三类 Agent 的入口与前置依赖；能力本体在 AI 设置。
- ① 管理端 Agent：**就地承接 `MCPServiceCard`**（含接入地址、Harness 配置、令牌生成/轮换/吊销、最近调用审计），保存语义保持即时保存。
- ② 客服端 Agent：启用开关 + 能力档位 + 依赖行（AI 模型 / 接管范围 / 外部 MCP 工具），保存走**区块级 `SaveBar`**（标签写明「保存客服 Agent 配置」）。
- **Agent 中心不使用页级保存条**：三区块中只有客服端有草稿，页级条会被误读为「保存整页」；保存条按作用域分为页级（AI 设置）与区块级（Agent 中心客服端）。
- ③ 运营 Agent：依赖行（QQ 机器人连接器）+ 说明本页不提供配置，跳转连接器页。

### 3.3 跨 feature 约束与就绪声明

- Agent 中心显示「QQ 连接器就绪」需 `qqbot.*` 就绪判定，但该判定现属 `features/connectors/registry.ts`。
- 前端门禁禁止 feature 互相引用内部文件 → 就绪键声明下沉到 `shared/api-contract/settings.ts`；`connectors/registry.ts` 仍是连接器卡片 UI 的权威，两处消费同一份键声明。
- AI 模型 / 接管范围 / 外部 MCP 工具状态来自 `ai_api_key_configured`、`ai_model`、`ai_scope_config`、`mcp.servers`；Agent 中心新增一次 `getSystemSettings` 读取（settings feature 内已有该适配器）。

### 3.4 门禁影响

- `canonicalPageEntrypoints`、`routes.ts`、`Sidebar.tsx`、`routing.test.ts`：**不变**（页面文件不改名）。
- `bundleBoundary.test.ts`：Settings 预算下调（MCP 卡移出）、AgentCenter 预算上调（MCP 卡迁入，现 7834 字节）；两者均按实测字节回填。
- 补入 `AISettings` 预算：现状 `PAGE_CHUNK_BUDGETS` 与分片名清单均不含 AISettings，是预算治理盲区。
- `featureArchitecture.test.ts`：新增 settings 与 connectors 的边界断言。

## 4. 组件与共享原语

### 4.1 落点

```
frontend/shared/ui/
  PageShell.tsx        页面外壳：容器宽度、区块间距、底部保存条占位
  PageHeader.tsx       图标 + 标题 + 副标题 + 右侧动作槽
  SectionHeader.tsx    区块标题 + 语义色图标槽
  SaveBar.tsx          保存条：页级粘性（AI 设置）/ 区块级（Agent 中心客服端），四态
  AsyncSection.tsx     加载中 / 错误+重试 / 内容 三态容器
  StatusBadge.tsx      状态徽章（已配置 / 未配置 / 读取中）
  DependencyRow.tsx    依赖名 + 状态徽章 + 跳转目标
  __tests__/           对应聚焦测试
frontend/shared/api-contract/settings.ts    追加连接器就绪键声明（跨 feature 共用）
```

方向合规：features 引用 shared 是允许方向（`app → features → shared`），与既有 `Sidebar`、`appDialog` 同惯例。

### 4.2 原语契约（要点）

- `PageShell`：统一 `max-w-6xl` 容器与区块间距；承载保存条的避让空间，替掉现有 `pb-24` 硬编码。
- `SaveBar`：支持两种作用域——页级（AI 设置：单一草稿，粘性在页内底部）与区块级（Agent 中心：仅客服端草稿，位于该区块底部并带作用范围标签）；四态为 `saving` / `error`+重试回调 / 成功提示 / 空闲；停用 `appDialog.alert`；不遮挡内容。
- `AsyncSection`：`loading` / `error` + `onRetry` / 内容；错误态必须用户可见。
- `DependencyRow`：只读、弱化呈现（细边框白底），不与主配置区争夺视觉权重。
- `StatusBadge`：三态徽章，替掉 `ConnectorCard` 与 `AgentCenter` 各自的实现。

### 4.3 语义色板

| 色 | 语义 | 典型区块 |
|---|---|---|
| brand 蓝 | AI 能力 | 模型与凭据 |
| violet | 知识与接管范围 | 策略/语料/边界 |
| emerald | 外部工具接入 | 外部 MCP 工具 |
| indigo | 对外服务 / 平台连接器 | MCP 服务卡、平台连接器 |
| slate | 系统基础设施 | 系统级配置 |
| amber | 风险与权限 | 安全提示 |

## 5. 状态与保存语义

- **两种保存语义并存**：草稿型（AI 设置、Agent 中心客服端）→ `SaveBar` 显式提交；即时型（MCP 服务卡开关/令牌/审计）→ 操作即提交、卡内提示。
- **状态三分归属**：服务端数据（系统设置 / 租户配置）走草稿+保存态；表单态（三层 JSON 的 form/json 切换、模型下拉）留在组件本地；瞬态（toast、复制反馈、明文切换）留在组件本地。
- **依赖状态只读**：只展示与跳转，不就地编辑。
- **并发与取消**：依赖状态读取使用独立于租户配置的代次与 `AbortController`；保存与依赖刷新互不取消；依赖状态保存后不回写。

## 6. 加载、错误与依赖态

- 首次加载：Agent 中心三区块各自用 `AsyncSection` 包裹（三态：加载中 / 错误+重试 / 内容）；AI 设置**维持现有整页加载与错误态**，不再用 `AsyncSection` 重复包裹（该页只有一个草稿，整页态更清晰）。
- **任何 `getSystemSettings` 失败都必须有用户可见表现**（错误条 + 重试），不再仅 `console.error`。
- **数据未到达时状态徽章显示「读取中」，不显示「未配置」**——该规则同时消除 P2 连接器卡的「未配置」闪烁。
- 依赖三态：`读取中` / `已配置` / `未配置·回落内置`。「留空回落内置」必须与「未配置」区分，前者是合法默认，用琥珀弱化呈现而非告警色。
- 依赖跳转只跳路径，不做锚点滚动（页面内容有限，锚点收益不足以支撑路由 hash 处理）。

## 7. 视觉令牌

- 区块图标色按 §4.3 语义色板取色，不再随机。
- `SaveBar` 四态：空闲（深色条 + 主按钮）/ 保存中（spinner + 禁用）/ 失败（红条 + 重试常驻）/ 成功（绿勾，2 秒回落）。
- 保存条作用域区分：页级用于 AI 设置（单一草稿）；区块级用于 Agent 中心客服端，标签写明「保存客服 Agent 配置」，避免误读为保存整页。
- `DependencyRow`：名称 + 徽章 + 跳转，整体弱化。
- 区块标题沿用现有惯例（`text-lg font-extrabold` + 图标块）。

## 8. 测试

1. `shared/ui` 原语各态：SaveBar 四态、StatusBadge 三态、DependencyRow 三态与跳转、AsyncSection 三态。
2. Agent 中心依赖状态：读取成功 / 失败（含重试）/ 未配置·回落内置。
3. AI 设置保存失败 → `SaveBar` 显示重试（替换原 `appDialog.alert` 断言）。
4. MCP 服务卡迁移后行为不变：现有 `mcpHooks` 与卡片用例整体保留，仅改挂载断言。
5. 架构门禁：settings feature 不引用 connectors feature 内部文件。
6. `bundleBoundary.test.ts`：按实测重算 Settings / AgentCenter 预算，补入 AISettings。

按 AGENTS.md 1.4：新用户流程需覆盖成功、失败、取消、切换与过期响应。

## 9. 门禁与工作流

```bash
# 本机门禁（串行）
npm --prefix frontend run typecheck
npm --prefix frontend run comments:check
npm --prefix frontend test
npm --prefix frontend run build
```

- 无条件：新增组件需中文语义注释（AGENTS.md 1.2），生成代码与静态产物除外。
- 构建后按实测字节回填 `PAGE_CHUNK_BUDGETS`。
- 任务在 `.worktree/` 新建 worktree 内开发，禁止主工作区直接开发（AGENTS.md 0.6）。
- 前端产物单独一条 `build(webui)` 提交（项目惯例）。

## 10. 改动清单

| 文件 | 性质 | 说明 |
|---|---|---|
| `frontend/shared/ui/PageShell.tsx` 等 7 个原语 | 新增 | 共享外壳、页头、区块头、保存条、异步容器、徽章、依赖行 |
| `frontend/shared/ui/__tests__/` | 新增 | 上述原语的聚焦测试 |
| `frontend/shared/api-contract/settings.ts` | 修改 | 追加连接器就绪键声明（跨 feature 共用） |
| `frontend/app/features/settings/pages/AISettings.tsx` | 修改 | 改用 PageShell/PageHeader/SectionHeader/SaveBar；移除悬浮按钮与 `pb-24` |
| `frontend/app/features/settings/pages/AgentCenter.tsx` | 修改 | 三类 Agent 区块 + 依赖行；承接 `MCPServiceCard` |
| `frontend/app/features/settings/agentCenterHooks.ts` | 修改 | 新增依赖状态读取（独立代次与取消）与依赖状态字段 |
| `frontend/app/features/settings/pages/Settings.tsx` | 修改 | 移除 MCP 服务区块 |
| `frontend/app/features/settings/components/AIConfigEditor.tsx` | 修改 | 区块头改 `SectionHeader` |
| `frontend/app/features/settings/components/MCPServerList.tsx` | 修改 | 区块头改 `SectionHeader` |
| `frontend/bundleBoundary.test.ts` | 修改 | 预算重算、补入 AISettings |
| `frontend/featureArchitecture.test.ts` | 修改 | 新增 settings/connectors 边界断言 |
| 本文件 `docs/architecture/ai-agent-config-domain-plan.md` | 新增 | 设计文档 |

## 11. 遗留与后续

- **P2 连接器页**：复用本批原语；按「系统级配置（连接器 + SMTP）vs 用户级渠道 CRUD」分区；同时修掉 §6 的「未配置」闪烁、刷新只刷渠道、QQ 渠道表单与连接器卡凭据重叠三项。
- **运营 Agent 与管理端 Agent 的依赖行**目前在 Agent 中心只读展示；若后续要就地配置，需重新评估本文「消费侧不编辑供给」的边界。
- 颜色令牌目前以类名字面量分散在各组件；若要统一治理，属独立的设计令牌子项目，本次不做。
- 连接器就绪键下沉到契约层后，连接器卡的 UI 权威仍在 `connectors/registry.ts`；新增连接器时两处需同步，与现有「敏感键需在契约层同步」惯例一致。
