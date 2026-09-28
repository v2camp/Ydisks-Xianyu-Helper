# 系统设置与 AI 设置拆分 + MCP 配置 · 实施计划

- 日期：2026-09-29
- 依据：`docs/superpowers/specs/2026-09-29-settings-ai-split-design.md`
- 分支：`feat/settings-ai-split`（worktree：`.worktree/settings-ai-split`）
- 执行方式：多波多 agent 并行，文件所有权严格不重叠

## 0. 验证命令（全波共用）

```bash
# 前端（在 worktree 根目录）
npm --prefix frontend run typecheck
npm --prefix frontend run test
npm --prefix frontend run comments:check

# Go 注释门禁
docker run --rm -v "$PWD":/src -w /src -v ydisks-gomod:/go/pkg/mod \
  golang:1.26 go run ./tools/commentlint -mode check -root .

# Go 单测（受影响包；核心链路覆盖率）
docker run --rm -v "$PWD":/src -w /src -v ydisks-gomod:/go/pkg/mod \
  golang:1.26 sh -c 'go test ./internal/application/settings ./internal/engine'
docker run --rm -v "$PWD":/src -w /src -v ydisks-gomod:/go/pkg/mod \
  golang:1.26 sh -c 'go test -coverprofile=cover-core.out \
    ./internal/automation ./internal/engine ./internal/adapter ./internal/db ./internal/xianyu/ws \
    && go tool cover -func=cover-core.out | tail -1'

# UI 动线门禁（改了前端路由，必须跑）
docker compose -f docker-compose.functional.yml build webui-e2e-test
docker compose -f docker-compose.functional.yml run --rm webui-e2e-test
```

网络受限时 Go 构建设置 `GOPROXY=https://goproxy.cn,direct`。覆盖率必须 ≥ 改动前且核心链路保持 100%。

## 1. 文件所有权（并行防冲突的关键）

| 包 | 所有权文件 | 所属波次 |
|---|---|---|
| A 前端导航拆页 | `frontend/app/router/routes.ts`、`AppRouter.tsx`、`frontend/app/shell/AuthenticatedShell.tsx`、`AuthenticatedShell.test.tsx`、`frontend/shared/ui/Sidebar.tsx`、`frontend/app/features/settings/pages/Settings.tsx`、`pages/AISettings.tsx`（新建）、`hooks.ts`、`hooks.test.tsx`、`constants.ts`、`state.ts`、`state.test.ts`、`types.ts`、`models.ts`、`frontend/routing.test.ts` | W1 |
| B 后端校验 | `internal/application/settings/service.go`、`service_test.go` | W1 |
| C 引擎接线 | `internal/engine/plugin_reply.go`、`plugin_reply_test.go`、`account.go` | W1 |
| D UX 组件 | `frontend/app/features/settings/components/**`（含新建）、`pages/AISettings.tsx` 的组件装配小改、组件测试 | W2（依赖 A 完成） |
| E 门禁集成 | 上述之外的收尾修复、门禁跑通 | W3 |

同名文件跨包禁止并行编辑；`AISettings.tsx` 仅 A 创建、D 在 A 完成后接管装配。

## 2. Wave 1（3 agent 并行）

### A. 前端：路由拆页 + hook scope 白名单（产出提交 1）

1. `routes.ts`：`AppRoute` 增 `'ai-settings'`，`/app/ai-settings` 映射。
2. `AppRouter.tsx`：非管理员回退与 `navigate` 拦截覆盖 `ai-settings`。
3. `AuthenticatedShell.tsx`：`lazy` 注册 `AISettings`，`case 'ai-settings'`；非管理员回仪表盘。
4. `Sidebar.tsx`：管理员菜单改两项——「系统设置」（`settings`）+「AI 设置」（`ai-settings`），替换「系统与AI」。
5. `constants.ts`：新增 `SYSTEM_SETTING_KEYS` / `AI_SETTING_KEYS` 白名单集合（见 spec §3）。
6. `hooks.ts` + `state.ts`：`useSettings(scope)`；`buildPersistableSettings(settings, scope)` 按白名单裁剪；`scope='system'` 不发起模型发现；连接测试仅 AI 页用。
7. `Settings.tsx` 瘦身：移除 AI 三块（AI 配置、AIConfigEditor、人工确认开关），`useSettings('system')`；「安全阀门」保留两项。
8. `AISettings.tsx` 新建：`useSettings('ai')`；装配 AI 智能回复配置 + `AIConfigEditor` + 人工确认开关 + MCP 列表占位（W2 换成真组件，接口先按 spec §4/§5 预留 import）。
9. 测试：`routing.test.ts`（新路由 + 回退）、`AuthenticatedShell.test.tsx`（ai-settings 分支）、`hooks.test.tsx`（白名单保存、system 不发模型请求）、既有断言适配。`typecheck` + `test` + `comments:check` 全绿。
10. 注释按 AGENTS.md 1.2：新增/修改的函数、状态、回调全部中文语义注释。

### B. 后端：`mcp.servers` 校验（产出提交 3）

1. `validateSystemValue` 新增 `case "mcp.servers"`：JSON 数组；元素含非空 `name`≤64、非空 `url`≤512；其余形状拒绝。
2. `service_test.go`：合法、非法 JSON、非数组、缺 name/url、超长、空串（空串视为清空合法？——空串=未配置，放行，与「键不存在」同语义）等聚焦用例。
3. 不改 OpenAPI；不改敏感键白名单。
4. `go test ./internal/application/settings` + commentlint 全绿。

### C. 引擎：MCP URL 动态解析（产出提交 4）

1. `plugin_reply.go`：`FindStuffReplier` 持有 `urlResolver func(ctx) string`（替代固定 `mcpURL`）；新增解析函数：`mcp.servers` 合法 → 取 `name=find_stuff` 条目（缺失/url 空=关闭）；键缺/空 → 既有 `resolveFindStuffMCPURL` 环境变量逻辑；非法 JSON → Warn + 回落环境变量。
2. 会话带 `sessionURL`；`ensureSession` 解析值变化时关旧连新。意图未命中不读设置。
3. `account.go`：用 `cfg.Store.Settings` 构造解析器；`NewFindStuffReplierFromEnv` 保留为环境变量回落路径（或内联进解析器，二选一，测试语义不变）。
4. `plugin_reply_test.go`：三分支优先级、URL 变更重连、非法 JSON 回落、空 url 关闭、既有用例适配。**核心链路 100% 语句覆盖必须保持**。
5. `go test ./internal/engine` + 核心链路覆盖率 + commentlint 全绿。

## 3. Wave 2（A 完成后启动，1 agent）

### D. JSON 表单化 + MCP 列表（产出提交 2）

1. `components/EditableList.tsx`：通用可增删行容器（增行、删行、行内校验错误展示），FAQ / 在售清单 / 意图 / MCP 复用。
2. `components/MCPServerList.tsx`：name+URL 行编辑，序列化 `mcp.servers` JSON；行内校验（名称必填不重复、http(s) URL）；空状态示范 `find_stuff`。
3. `components/AIConfigForms.tsx`：
   - 策略两卡（最低价回复/不议价回复）+「填入推荐话术」；
   - 语料 FAQ/在售清单两个 EditableList +「填入推荐语料」；
   - 边界意图行（ID 可编辑/启用/AI 接管/匹配规则）+ 常用意图芯片 + 负向词单行。
   - 每块右上角「JSON」切换：与表单共用草稿；JSON 非法禁切回并提示；表单保存保留未知键。
4. `AIConfigEditor.tsx` 改为组装上述表单（或删除并由 `AISettings.tsx` 直接装配，保持单一入口）。
5. 组件测试：增删行、校验、序列化往返、未知键保留、推荐填充、表单↔JSON 切换。
6. `typecheck` + `test` + `comments:check` 全绿。

## 4. Wave 3（集成与门禁，主协调 + E）

1. 合并各波产出为四条提交（见下），确保每个提交点可编译。
2. 跑 §0 全部门禁：commentlint（Go+前端）、前端 typecheck/test、Go 受影响包测试、核心链路覆盖率、`webui-e2e-test`。
3. 失败即定位修复（优先最小修复，禁止弱化测试），重跑至绿。
4. 输出验证证据（命令、环境、覆盖率数字、e2e 结果）写入进度说明。

## 5. 提交拆分（AGENTS.md 3.1）

1. `feat(settings): 拆分系统与AI设置页并按范围保存配置` —— A
2. `feat(settings): AI配置表单化与MCP服务器列表` —— D
3. `feat(settings): 校验 mcp.servers 设置键` —— B
4. `feat(engine): find_stuff MCP 地址改由设置动态解析` —— C

每条：中文首行 + 正文（改动点/为什么/怎么验证）；测试与改动同条；不夹带无关重构。

## 6. 风险与约束

- 滑块验证码冻结：只动 Settings 页 UI 归属，不碰冻结七文件与行为。
- 并行纪律：只改所有权表内文件；发现必须改表外文件时先上报协调者，禁止跨界。
- 禁止跳过/弱化测试；核心链路覆盖率回落即门禁失败。
- 合并与推送：完成验证后 `merge --no-ff` 回 main；push 前核实 remote 并经用户确认。
