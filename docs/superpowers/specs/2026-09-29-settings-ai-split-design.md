# 系统设置与 AI 设置拆分 + MCP 配置 · 设计文档

- 日期：2026-09-29
- 状态：已与需求方确认（方案 A + JSON 低阻力增补）
- 范围：前端设置页拆分、MCP 服务器列表配置、后端设置键校验、引擎 find_stuff MCP 动态读取

## 1. 背景与目标

当前只有一个管理员设置入口「系统与AI」（`/app/settings`），页面内混杂系统配置与 AI 配置。AI 能力正在扩展（找书/找短剧服务将以 MCP 形式接入），需要：

1. 系统设置与 AI 设置成为两个独立设置页，AI 功能后续只动 AI 页。
2. AI 页支持配置 MCP 服务器列表（通用形态），引擎消费其中 `find_stuff` 条目。
3. JSON 类配置（AI 客服三层配置）表单化，降低使用成本。

非目标：真正的 AI 插件管理框架、多 MCP 协议类型（仅 streamable HTTP URL）、导入导出、拖拽排序、正则试匹配。

## 2. 页面与导航

- `frontend/app/router/routes.ts`：新增 `/app/ai-settings` ↔ `ai-settings`。
- `frontend/shared/ui/Sidebar.tsx`：管理员菜单由一项变两项，替换原「系统与AI」：
  - 「系统设置」（`settings`，齿轮图标）
  - 「AI 设置」（`ai-settings`，Sparkles 图标）
- 权限守卫：`frontend/app/router/AppRouter.tsx` 的地址栏回退与 `navigate` 拦截把 `ai-settings` 与 `settings` 同等对待（非管理员一律回仪表盘）。
- `frontend/app/shell/AuthenticatedShell.tsx`：新增 `case 'ai-settings'`，懒加载 `AISettings`，`isAdmin` 才渲染，否则回仪表盘。
- 两页均 `lazy()` 懒加载，与现有页面一致。

## 3. 内容归属与保存语义

| 页面 | 文件 | 板块 |
|---|---|---|
| 系统设置 | `features/settings/pages/Settings.tsx`（瘦身后） | 基础设置、安全阀门（全局日发送额度、静默告警阈值）、远程过滑块配置、登录凭据 |
| AI 设置 | `features/settings/pages/AISettings.tsx`（新建） | AI 智能回复配置、AI 客服接管 JSON、AI 回复人工确认模式、MCP 服务器列表 |

「AI 回复人工确认模式」从安全阀门迁入 AI 页（AI 行为开关归 AI）。

### 保存白名单（核心语义）

`useSettings` 增加参数 `scope: 'system' | 'ai'`：

- 加载仍是同一全量 GET `/api/v1/settings/system`（脱敏返回）。
- 保存时按 scope 字段白名单裁剪草稿后再 PUT，任一页保存绝不覆盖另一页字段：
  - **system 白名单**：`log_level`、`log_format`、`renewal_log_retention_days`、`outbound_http_public_only`、`global_send_daily_limit`、`silence_alert_minutes`、`captcha.remote_service_url`、`captcha.remote_secret_key`、`captcha.remote_pass_cookies`。
  - **ai 白名单**：`ai_api_url`、`ai_api_key`、`ai_model`、`ai_reply_review_mode`、`ai_scope_config`、`ai_knowledge_config`、`ai_policy_config`、`mcp.servers`。
- 模型发现（`fetchAIModels`）与连接测试仅 `scope='ai'` 时请求。
- 登录凭据走独立端点 `/api/v1/session/credentials`，仅系统页渲染，行为不变。
- 敏感键（`ai_api_key`、`captcha.remote_secret_key`）仍经 `normalizeSystemSettingsUpdate` 走 secrets 通道，白名单包含它们但不改变脱敏/审计语义。

## 4. MCP 配置

### 4.1 前端

- 新组件 `features/settings/components/MCPServerList.tsx`：可增删行列表，每行「名称 + 服务地址（URL）」。
- 设置键 `mcp.servers` 存 JSON 数组字符串 `[{"name":"...","url":"..."}]`；编辑草稿里 parse / stringify。
- 行内校验：名称必填且页内不重复、URL 须为 http(s)；终校在服务端。
- 空状态文案示范：名称填 `find_stuff` + 找书找短剧服务地址。
- 提示语：引擎目前消费 `find_stuff` 条目；未配置时回落环境变量 `FIND_STUFF_MCP_URL`。

### 4.2 后端校验

- `internal/application/settings/service.go` 的 `validateSystemValue` 新增 `case "mcp.servers"`：
  - 值必须是合法 JSON 数组；
  - 元素为对象，含非空 `name`（≤64 字符）与非空 `url`（≤512 字符）；
  - 非法值拒绝落库。
- OpenAPI 不改：`SystemSettingsUpdateRequest` / `SystemSettingsResponse` 本就是自由 map。

### 4.3 引擎接线（`internal/engine`）

- `plugin_reply.go`：`FindStuffReplier` 的固定 `mcpURL` 改为注入 URL 解析器 `func(ctx context.Context) string`。
- `account.go`：用 `cfg.Store.Settings` 构造解析器（该处已有 `cfg.Store != nil` 保障）。
- 解析优先级（每次命中找书/找短剧意图、建连前解析）：
  1. `mcp.servers` 已配置且合法 → 取 `name=find_stuff` 条目；条目缺失或 url 为空 = 关闭插件层。
  2. 键不存在或空串 → 现有环境变量逻辑不变（`FIND_STUFF_MCP_URL`，未设置回落 compose 默认 `http://find-stuff:59190/mcp`，显式空串 = 关闭）。
  3. 键存在但非法 JSON（历史脏数据）→ 记 Warn 并回落环境变量。
- 生效时机：会话若已建立且解析出的 URL 与建连时不同，则关旧连新——保存后下一条消息生效，无需重启，与 `ai_reply_review_mode` 即时语义一致。
- 找书/找短剧意图未命中时不读设置、不连 MCP（保持现状零开销）。

## 5. JSON 配置低阻力设计

原则：用户默认不面对 JSON，表单即配置；JSON 仅作高级模式。空状态给「一键填入推荐配置」，不给空表单。视觉沿用现有 ios-card / ios-input 语言；列表行交互与 MCP 列表同构。实现上做一个「通用可增删行容器」，FAQ / 在售清单 / 意图 / MCP 四处复用。

| 设置键 | 默认视图 | 交互 |
|---|---|---|
| `ai_policy_config`（策略） | 两张文案卡：「最低价回复」「不议价回复」 | 多行输入，placeholder 演示 `{amount}`；两字段即完整配置；文案为空时提供「填入推荐话术」 |
| `ai_knowledge_config`（语料） | 两个可增删列表：FAQ 行（分类 / 命中词 / 回答）、在售清单行（标题 / 详情） | 行内增删；顶部「填入推荐语料」（填入 KNOWLEDGE_DEFAULT 示例，catalog 示例可直接改成自家在售） |
| `ai_scope_config`（边界） | 意图列表：ID / 启用开关 / AI 接管开关 / 匹配规则 + 负向词单行 | 意图 ID 可编辑（模板带出 bargain 等默认值）；「常用意图」模板芯片（砍价、商品问答、发货、库存）一键插入；负向词 `\|` 分隔，placeholder 演示 |

通用机制：

- **表单 ↔ JSON 切换**：每块右上角「JSON」小按钮；JSON 视图沿用现有 textarea + 本地校验；JSON 非法时禁止切回表单并提示错误。两视图共用同一草稿。
- **保留未知键**：表单保存时只改已知字段，解析出对象中的其他键原样保留，来回切换不丢扩展字段。
- **行内校验**：非法行红框 + 一行错误提示，不用全局 alert。
- **未保存提示**：保存按钮上方「有未保存更改」轻提示（两页拆分后更重要）。
- 不做：拖拽排序、正则试匹配、导入导出。

## 6. 测试与门禁

前端（Vitest）：

- `hooks.test.tsx`：scope 白名单保存（system 保存不含 ai_* 与 mcp.servers，反之亦然）、系统页不发模型发现请求、既有用例适配。
- `routing.test.ts`：`/app/ai-settings` 路由映射、非管理员回退。
- `AuthenticatedShell.test.tsx`：`ai-settings` 分支渲染与权限回退。
- `featureArchitecture.test.ts` / `bundleBoundary.test.ts`：按现状校验，必要时适配。
- 新增：MCP 列表（增删/校验/JSON 序列化）、通用可增删行容器、三块 JSON 表单化组件（含表单↔JSON 切换、未知键保留、推荐配置填充）的聚焦测试。

Go：

- `internal/application/settings/service_test.go`：`mcp.servers` 合法 / 非法 JSON / 非数组 / 缺 name / 超长 URL 等用例。
- `internal/engine/plugin_reply_test.go`：解析优先级三条分支、URL 变更重连、非法 JSON 回落、空 url 关闭插件层。
- **注意**：`internal/engine` 属核心链路包，语句覆盖率必须保持 100%，新分支全部配测试，与改动同批提交。

门禁命令（验证时按 AGENTS.md 执行）：

- Go/前端注释门禁（commentlint、comments:check）——新增函数/状态/回调全部中文语义注释。
- 受影响包 go test + 覆盖率声明。
- 本次改了前端路由，必须跑 `webui-e2e-test` 门禁。

## 7. 实施与提交

- 按 AGENTS.md 0.6 在 `.worktree/` 新建 worktree 与分支开发，主工作区不动。
- 提交拆分（每条自身可编译、中文提交信息）：
  1. 前端：路由/侧边栏/壳层拆分 + `AISettings` 页骨架 + hook scope 白名单保存。
  2. 前端：JSON 表单化（通用行容器 + 三块表单 + JSON 切换）+ MCP 列表组件。
  3. 后端：`mcp.servers` 校验 + 聚焦测试。
  4. 引擎：URL 解析器注入 + 动态解析/重连 + 核心链路覆盖测试。
- 验证证据记录到重构进度文档（如适用）；完成后按 commit / merge --no-ff / push 流程。

## 8. 风险与冻结项

- 滑块验证码生产冻结：远程过滑块仅 UI 归属移到系统页，不改行为、选择器、参数、测试；不动冻结规范列出的七个文件。
- `captcha.remote_secret_key` 等敏感键走 secrets 通道的语义不变。
- `mcp.servers` 为普通设置键（URL 非密钥）；若后续 MCP 地址需携带凭证，再评估纳入敏感白名单。
- 表单化编辑器与旧 JSON 数据双向兼容：旧值可解析即进表单；解析失败进 JSON 模式并提示。
