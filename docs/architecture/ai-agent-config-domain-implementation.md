# AI 与 Agent 配置域重整（P1）实施计划

> 依据：`docs/architecture/ai-agent-config-domain-plan.md`（设计已确认）
> 分支：`feature/ai-agent-config-domain`，worktree：`.worktree/ai-agent-config-domain`
> 本文只定义实施步骤与每一步的验收，不重述设计理由。

## 0. 步骤顺序与原则

顺序：**地基 → 页面改造 → 预算与产物**。理由：共享原语与页面清单归一必须先落地，否则新页面代码会各自再长一份同样 UI 的私有实现，且预算盲区会一直拖到收尾才被发现。

| 步骤 | 内容 | 可独立验证 | 备注 |
|---|---|---|---|
| 1 | 共享 UI 原语 + 聚焦测试 | 是 | 先建、先测，暂不改任何页面 |
| 2 | 页面清单归一 + AISettings 预算补入 | 是 | 修掉治理盲区（见 §7.1） |
| 3 | AI 设置页改骨架与页级保存条 | 是 | |
| 4 | MCP 服务卡迁入 Agent 中心 + 命名消歧 | 是 | 见 §7.2 |
| 5 | Agent 中心依赖状态 + 区块级保存条 | 是 | 依赖步骤 1、4 |
| 6 | 预算实测回填 + `build(webui)` 产物提交 | 是 | 项目惯例单列一条提交 |

每步跑本机门禁（AGENTS.md 2.2，串行）：

```bash
npm --prefix frontend run typecheck
npm --prefix frontend run comments:check
npm --prefix frontend test
```

收尾补 `npm --prefix frontend run build`（步骤 6 必须）。

## 1 · 共享 UI 原语

**落点**：`frontend/shared/ui/`，均为纯展示组件，不引入请求逻辑（shared 引用方向允许，与既有 `Sidebar`、`dialog` 同惯例）。

| 文件 | 职责 |
|---|---|
| `PageShell.tsx` | `max-w-6xl` 容器 + 区块间距 + 保存条避让空间（替掉 `pb-24` 硬编码） |
| `PageHeader.tsx` | 图标 + 标题 + 副标题 + 右侧动作槽 |
| `SectionHeader.tsx` | 区块标题 + 语义色图标槽（色板见设计 §4.3） |
| `SaveBar.tsx` | 四态 + 两种作用域（页级 / 区块级，区块级带作用范围标签） |
| `AsyncSection.tsx` | 加载中 / 错误 + 重试 / 内容 |
| `StatusBadge.tsx` | 已配置 / 未配置 / 读取中 三态 |
| `DependencyRow.tsx` | 依赖名 + 徽章 + 跳转目标，只读弱化呈现 |

**验收**：每个原语一个聚焦测试文件，覆盖全部状态分支与 `onRetry` / 跳转回调被调用；`comments:check` 通过（新增组件需中文语义注释）。

## 2 · 页面清单归一与 AISettings 预算

**事实**：页面清单目前有三份且互相矛盾——`featureArchitecture.test.ts` 的 `canonicalPageEntrypoints`（13 项，含 SessionGate）、`bundleBoundary.test.ts` 的 `PAGE_CHUNK_BUDGETS` 与分片名正则（11 项，不含 AISettings）、`routing.test.ts` 的懒加载正则清单（11 项，含 AISettings 但不含 DeliveryTemplates）。AISettings 因此逃出预算治理。

**做法**：
1. 新增 `frontend/app/router/pageEntries.ts` 作为唯一来源，导出 `pageEntryFiles`（页面文件路径）与 `lazyPageNames`（会产生独立分片的页面名，不含 SessionGate）；
2. 三处测试各自改为引用该常量，`featureArchitecture` 校验入口、`bundleBoundary` 校验预算覆盖、`routing` 校验懒加载；
3. `bundleBoundary` 新增断言：**`lazyPageNames` 中每个名字都必须在 `PAGE_CHUNK_BUDGETS` 有条目**——漏登记即失败；
4. `AISettings` 补入预算：实测分片 25099 字节，初始取 `28 * 1024`。

**为什么不能靠“大写字母前缀自动识别”**：磁盘上的大写前缀分片还有 `PublishImagesEditor`、`PublishSpecsEditor`（懒加载的重组件而非页面），自动识别会误报。

**验收**：故意删掉一个预算键，测试必须失败；跑全量 `npm test` 绿。

## 3 · AI 设置页改造

**改动**：装配 `PageShell` / `PageHeader` / `SectionHeader`；右下悬浮保存按钮改为页级 `SaveBar` 并移除 `pb-24`；保存成功从 `appDialog.alert` 改为 `SaveBar` 内联成功态；三个区块的 `h3` 改用 `SectionHeader`（语义色：模型=brand、接管范围=violet、外部 MCP 工具=emerald、人工确认=brand）；顶部加「谁在使用 → Agent 中心」指引条。

**不动**：`useSettings('ai')` 的 scope 白名单、AI 三层 JSON 表单内容与「读取模型 / 测试连接」交互、整页加载错误态。

**验收**：更新现有 AI 设置用例的保存断言（`appDialog.alert` → `SaveBar` 成功/失败态）；补失败态重试的用例。

## 4 · MCP 服务卡迁移与命名消歧

**迁移**：从 `Settings.tsx` 移除 `<MCPServiceCard />` 及其区块标题，挂进 `AgentCenter.tsx`「管理端 Agent」区块。组件与 `useMCPService` 内部代码不动，即时保存语义不变。

**命名消歧**（对应设计评审的第 2 号问题，详见 §7.2）：
- AI 设置区块名：「外部 MCP 工具（出站）」，副文案说明本服务作为 MCP 客户端调用外部工具；
- Agent 中心区块名：「对外 MCP 服务（入站）」，沿用卡片既有说明；
- `docs/wiki/MCP开放服务.zh-CN.md` 补一句：出站客户端配置在 AI 设置页，二者同名不同向，勿混；
- **不改 `mcp.servers` 键名**：两侧存储本就不冲突（入站走 `internal/db/mcp_auth.go` 与 `/api/v1/mcp/*`，出站才是 `mcp.servers`），改键名需要迁移并动 `internal/engine/plugin_reply.go`，收益低于风险。

**验收**：`mcpHooks` 与卡片既有用例保留并只改挂载断言；`Settings` 相关用例移除 MCP 断言、迁移至 Agent 中心用例；补 settings 与 connectors 的 feature 边界断言（不允许跨 feature 引用内部文件）。

## 5 · Agent 中心依赖状态

**数据**：新增一次 `getSystemSettings` 读取（settings feature 内已有适配器），取 `ai_api_key_configured` / `ai_model` / `ai_scope_config` / `mcp.servers` / 连接器就绪键。

**跨 feature 约束**：QQ 连接器就绪键声明下沉 `shared/api-contract/settings.ts`（该文件已有 `SENSITIVE_SYSTEM_SETTING_KEYS` 先例），`connectors/registry.ts` 仍是连接器卡片 UI 的权威。

**并发**：依赖读取复用既有 `commitIfLatest`（`shared/async/latestRequest.ts`）做最新代次判断，配合独立 `AbortController`；与租户配置保存互不取消；不另造一套代次逻辑。

**呈现**：三区块各自 `AsyncSection` 包裹；依赖行三态（读取中 / 已配置 / 未配置·回落内置）；**数据未到达显示「读取中」，绝不显示「未配置」**；客服端用区块级 `SaveBar`（标签「保存客服 Agent 配置」）。

**验收**：依赖读取成功 / 失败重试 / 未配置回落内置 三条用例；服务 Cap 读取失败 → 分区块错误态可见。

## 6 · 预算回填与构建产物

- 跑 `npm --prefix frontend run build`，按实测字节回填 `PAGE_CHUNK_BUDGETS`：`Settings` 下调（MCP 卡移出，现 28154）、`AgentCenter` 上调（MCP 卡迁入，现 7831）、`AISettings` 按改后实测；
- 每条预算注释写明「实测 N 字节」，与现有注释风格一致；
- **产物单独一条 `build(webui)` 提交**（项目惯例）。

## 7 · 两个遗留问题的处置结论

### 7.1 AISettings 没有预算条目

- **性质**：不是缺陷而是治理漏项——页面存在分片却未被任何清单登记，且三份页面清单互相矛盾（见步骤 2 事实）才是根因。
- **处置**：立即补预算（28 KiB），并在本次把三份清单归一下沉唯恐后患；否则下一次新增页面仍会静默逃出治理。
- **代价**：步骤 2 涉及三个测试文件的结构调整，需跑全量前端测试确认无副作用。

### 7.2 MCP 同名不同向

- **性质**：纯命名/归属问题，**不是数据重复**——入站配置在 `internal/db/mcp_auth.go` 与 `/api/v1/mcp/*`，出站配置是 system_settings 的 `mcp.servers`，两者键空间不重叠。
- **处置**：改 UI 文案标方向 + 文档补一句交叉说明 + 卡片迁入其宣称归属；**不改任何键名**（改动需迁移且波及引擎消费方，与连接器页「不改 `qqbot.*` 键名」同一判断）。
- **未做**：多 MCP 客户端注册表模式（目前只有 find_stuff 一个消费点，YAGNI）。

## 8 · 部署与回滚

- 纯前端改动，无数据迁移；受影响页面均为管理员配置页。
- 合并后按 AGENTS.md 4.1 部署：停账号 → 备份 `rollback-<日期>` → 带 `APP_VERSION` 构建 → 重启 app → 核对 `/health` 与版本 → 恢复账号。
- 回滚：`docker compose -p ydisks-xianyu-helper -f compose.yml -f compose.override.yml up -d app` 前把 app 镜像指回 rollback tag；代码侧 revert 即可。
- 命中「跨多个包的结构性改动」，合并部署后按 AGENTS.md 3.3 打 tag（`<版本>-local-<日期>`）。

## 9 · 与 P2 的接口

P1 交付的七个原语由 P2（连接器页分区）直接复用，届时同时处理：系统级与用户级分区、连接器卡「未配置」闪烁（依赖步骤 5 的通用规则）、「刷新」只刷渠道、QQ 渠道表单与连接器卡凭据重叠四项。
