# 连接器页重构（通知设置 → 连接器）需求与技术设计

> 状态：**已实现，本地门禁全绿**；未做真实平台联调（无新增平台行为，无需联调）。
> 本文定义「把通知设置升级为连接器」的需求与技术设计，不定义重构阶段，不替代 `docs/architecture/refactoring-master-plan.md`。

## 1. 背景与目标

### 1.1 背景（已核实的代码事实）

- 「通知设置」页（`/app/notifications`，feature `notifications`）承载通知渠道 CRUD、事件订阅与系统 SMTP。
- 「连接器」是系统设置页的一个区块：`QQConnectorCard` 写 `qqbot.app_id` / `qqbot.app_secret` / `qqbot.commands_enabled` / `qqbot.command_openids`。
- 该凭据有**两个消费方**：
  - 出站：`internal/notify/notifier_qq.go`，QQ 通知渠道凭据「渠道配置优先 → 回退系统连接器」。
  - 入站：`internal/qqbot/*`，由 `qqbot.commands_enabled` + `qqbot.command_openids` 驱动 QQ 网关只读命令。

结论：**连接器不是通知的附属，而是通知渠道的上游共享凭据**，即「通知设置」的上位概念。把二者放在两个页面会造成「配凭据」与「用凭据」割裂。

### 1.2 目标

1. 把「通知设置」升级为「连接器」页：同一页闭环「平台凭据 → 通知渠道」。
2. 引入**通用连接器注册表**：连接器以数据声明（凭据字段、能力开关、引导、支撑的渠道类型），新增平台只需追加注册表条目。
3. 从系统设置页删除 QQ 机器人连接器相关逻辑。

### 1.3 非目标

- 不新增数据库迁移，不改 `qqbot.*` 设置键名（改键名需迁移且要动出站/入站消费方，风险高、收益低）。
- 不改出站通知与入站命令的运行语义、权限与文案语义。
- 不为只有一个连接器的场景引入通用 Connector 表或注册中心。

## 2. 关键决策

| 决策 | 选择 | 依据 |
|---|---|---|
| 页面形态 | 通知设置升级为「连接器」页 | 连接器是通知渠道的上游共享凭据，需同页闭环 |
| 抽象程度 | 前端注册表声明式连接器 | 只有一个连接器时不做后端表；注册表足以支撑多平台扩展 |
| 存储 | 沿用 `system_settings` 的 `qqbot.*` 键 | `system_settings` 已是通用 KV，无需迁移 |
| 路由 | `/app/notifications` → `/app/connectors`，导航标签「通知设置」→「连接器」 | URL 与语义一致 |
| feature 归属 | feature 由 `notifications` 更名 `connectors`，通知渠道 UI 并入 | 前端门禁禁止 feature 互相引用内部文件，同页内容必须同 feature |
| 加密与审计 | 复用敏感键白名单与三态写入 | `qqbot.app_secret` 已在 `SENSITIVE_SYSTEM_SETTING_KEYS`，无需新机制 |

## 3. 技术设计

### 3.1 落点

```
frontend/app/features/connectors/                     （由 notifications 更名）
  registry.ts            新增：连接器注册表（ConnectorMeta / 字段 / 能力 / 引导 / key 汇总）
  state.ts               新增：connectorReady / buildConnectorSettings（纯函数）
  hooks.ts               useNotifications → useConnectors；新增连接器草稿加载与保存
  types.ts               NotificationState → ConnectorsState；新增连接器状态字段
  components/ConnectorCard.tsx         新增：注册表驱动的连接器卡
  components/ConnectorCard.test.tsx    新增：连接器卡聚焦测试
  pages/Connectors.tsx   由 Notifications.tsx 更名；新增「平台连接器」区（管理员可见）
frontend/app/features/settings/
  components/QQConnectorCard.tsx(.test.tsx)  删除：迁入 connectors
  pages/Settings.tsx                         删除「连接器」区块
  constants.ts                               SYSTEM_SETTING_KEYS 移除四个 qqbot.*；省略键移除派生标记
  models.ts                                  移除 qqbot 字段（迁入 connectors/models.ts）
```

### 3.2 注册表契约（`registry.ts`）

- `ConnectorMeta`：`id` / `label` / `icon` / `summary` / `fields` / `capabilities` / `guide?` / `notices?` / `effectNote` / `powersChannelTypes`。
- `ConnectorField`：`key`（系统设置键）、`sensitive`、`configuredKey`（服务端派生标记）、`required`。
- `ConnectorCapability`：`toggleKey` + 可选 `allowlistKey`。
- `connectorSettingsKeys()` 汇总注册表占用的全部设置键，作为「保存草稿」与契约测试的唯一来源。

### 3.3 保存语义

- 连接器页有独立草稿与独立保存动作，与 SMTP 草稿、渠道编辑表单互不覆盖。
- `buildConnectorSettings` 只提交注册表登记的键；**敏感键仅当草稿显式存在该键时提交**，缺省时保持服务端原值（避免加载后误清空）。
- 传输层 `normalizeSystemSettingsUpdate` 把 `qqbot.app_secret` 转为 `secrets.{action}` 三态命令。
- 权限：连接器区与 SMTP 一样按 `isAdmin` 门控；路由本身不对普通用户隐藏（渠道列表仍需可见）。

### 3.4 并发与取消

- 连接器保存使用独立代次 `connectorGeneration` 与独立 `connectorAbort`，避免与 SMTP 保存互相取消。
- 数据加载复用同一次系统设置读取；管理员状态切换或卸载时统一失效并取消。

## 4. 改动清单

| 文件 | 性质 | 说明 |
|---|---|---|
| `frontend/app/features/notifications/**` → `connectors/**` | 更名 | feature 目录与页面文件整体迁移 |
| `frontend/app/features/connectors/registry.ts` | 新增 | 连接器注册表与 QQ 条目（含开放平台引导） |
| `frontend/app/features/connectors/components/ConnectorCard.tsx` | 新增 | 注册表驱动卡：凭据输入、平台引导、能力开关、平台限制 |
| `frontend/app/features/connectors/components/ConnectorCard.test.tsx` | 新增 | 8 个用例：引导、凭据往返、掩码、能力开关、半配置、生效提示 |
| `frontend/app/features/connectors/state.ts` | 修改 | 新增 `connectorReady` / `buildConnectorSettings` |
| `frontend/app/features/connectors/hooks.ts` | 修改 | `useConnectors` + 连接器草稿加载/保存 |
| `frontend/app/features/connectors/types.ts` | 修改 | `ConnectorsState` + 连接器状态字段 |
| `frontend/app/features/connectors/models.ts` | 修改 | 补 `qqbot.*` 字段声明 |
| `frontend/app/features/connectors/pages/Connectors.tsx` | 修改 | 标题「连接器」+ 「平台连接器」区 |
| `frontend/app/features/settings/components/QQConnectorCard.tsx` | 删除 | 迁入 connectors |
| `frontend/app/features/settings/pages/Settings.tsx` | 修改 | 删除连接器区块 |
| `frontend/app/features/settings/constants.ts` | 修改 | 移除 `qqbot.*` 保存键与派生标记 |
| `frontend/app/features/settings/models.ts` | 修改 | 移除 `qqbot.*` 字段 |
| `frontend/app/features/settings/pages/AgentCenter.tsx` | 修改 | 指引文案改指「连接器」页 |
| `frontend/app/router/routes.ts` | 修改 | `notifications` → `connectors`、`/app/connectors` |
| `frontend/app/shell/AuthenticatedShell.tsx` | 修改 | 懒加载与路由分支更名 |
| `frontend/shared/ui/Sidebar.tsx` | 修改 | 标签「连接器」 |
| `frontend/featureArchitecture.test.ts` | 修改 | 页面入口清单更名 |
| `frontend/routing.test.ts` | 修改 | 懒加载清单与两条连接器契约断言改指连接器页 + 注册表 |
| `frontend/bundleBoundary.test.ts` | 修改 | 分片名与预算（Settings 下调、Connectors 上调） |
| `frontend/app/features/settings/state.test.ts` | 修改 | 移除 qqbot 白名单用例（已迁至连接器 state 测试） |
| `frontend/app/features/connectors/state.test.ts` | 修改 | 新增就绪判定与保存载荷用例 |
| `frontend/app/features/apiAdapters.test.ts` | 修改 | 适配器导入路径更名 |
| `frontend/app/features/presentational-components.test.tsx` | 修改 | 导入路径更名 |
| `frontend/app/features/connectors/hooks.test.tsx`、`notificationActions.concurrent.test.tsx` | 修改 | Hook 更名 |
| `internal/notify/notifier_qq.go` | 修改 | 缺凭据错误文案改指「连接器」页 |
| `internal/qqbot/service.go` | 修改 | 未授权与策略拒绝文案改指「连接器」页 |
| `docs/architecture/qq-notification-channel-plan.md` | 修改 | §3.7 落点与位置调整说明 |
| `docs/architecture/qq-inbound-command-plan.md` | 修改 | 背景表述改指「连接器」页 |

## 5. 验收标准

- 侧边栏出现「连接器」，路由 `/app/connectors` 可直达；旧 `/app/notifications` 不再登记。
- 连接器页在管理员视角展示 QQ 机器人连接器卡（凭据 + 入站命令开关 + 白名单 + 平台限制），普通用户仅见渠道列表。
- 系统设置页不再出现任何连接器相关控件；`SYSTEM_SETTING_KEYS` 不含 `qqbot.*`。
- 连接器保存：AppID 走普通值、AppSecret 走敏感三态命令；未输入 AppSecret 时保持原值。
- 前端 `typecheck`、`comments:check`、全部测试与生产构建通过；分片预算用例通过。
- 受影响 Go 包测试、`go build ./...`、架构与注释门禁无新增违规。

## 6. 验证记录

| 命令 | 结果 |
|---|---|
| `npm --prefix frontend run typecheck` | 通过 |
| `npm --prefix frontend run comments:check` | 通过 |
| `npm --prefix frontend test` | 通过（107 文件 / 694 用例） |
| `npm --prefix frontend run build` | 通过（Connectors 47941 字节、Settings 28154 字节） |
| `go build ./...` | 通过 |
| `go test ./internal/notify/... ./internal/qqbot/... -count=1` | 通过 |

## 7. 遗留与后续

- 连接器页「刷新」按钮目前只刷新渠道列表，不重载连接器草稿；如后续需要，可把系统设置读取暴露到页面状态。
- QQ 渠道在渠道表单中仍保留 AppID/AppSecret 字段（凭据两级来源的渠道级覆盖）；如需在连接器就绪后隐藏渠道级凭据，属独立产品决策，本次不做。
- 新增第二个连接器时：在 `registry.ts` 追加条目，并在 `internal/db/secrets.go` 与 `frontend/shared/api-contract/settings.ts` 同步其敏感键。
