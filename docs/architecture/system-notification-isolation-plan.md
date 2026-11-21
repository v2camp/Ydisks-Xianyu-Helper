# 系统通知隔离与站内通知中心方案

## 1. 背景

通知链路此前把「租户业务事件」与「实例级告警」混在同一模型：

- `business_silence` 是进程级事件，却由 `internal/adapter/business_silence_notifier.go` 取「有启用渠道的最小 ID 账号」当投递宿主，实例告警实际落到某个租户。
- `system_error` 是账号级未知告警的兜底分类，却作为「系统错误」订阅项展示给租户。
- 租户渠道 `event_types` 为空表示接收全部事件，`business_silence` 不在租户事件清单里，租户会被动收到系统级告警。
- `message_notifications` 删除后没有站内通知历史，通知只存在于外部渠道与 uncertain 列表。

本方案把系统通知从租户通知中隔离，并新增租户与系统两个站内通知视图。运营 Agent 保持独立会话页，只统一底层事件模型，不合并页面。

## 2. 目标模型

| 层 | 租户级 | 系统级 |
|---|---|---|
| 事件 | `account_*`、`security_*`、`automation_*`、`manual_*` | `business_silence`（本期唯一实例级事件，管线可扩展） |
| 归属 | `owner_user_id` = 账号所有者 | `owner_user_id=0`，scope=`system` |
| 外部接收人 | 账号所有者全部启用渠道 | 管理员独立系统告警渠道 |
| 站内视图 | 当前用户的通知中心 | 管理员的系统告警页 |
| 配置入口 | 连接器页 | 系统告警页（管理员） |

站内事件总是记录；账号静音与渠道订阅只影响外部投递。

## 3. 数据与存储

迁移 `00070_system_notifications.sql`（sqlite/postgres 同编号）：

- `notification_channels` 增加 `scope TEXT NOT NULL DEFAULT 'tenant'` 与 `(scope,user_id,enabled)` 索引。
- 系统渠道 `scope='system'`，`user_id` 记录创建管理员（审计 + 外键），配置加密归属固定为 `"system"`，避免多管理员先后编辑时解密失败。
- 新建 `notification_events`：`scope`、`owner_user_id`、`account_id`、`event_type`、`level`、`title`、`body`、`dedupe_key`（可空唯一索引）、`read_at`（unix 秒）、`created_at`（unix 秒）。
- Down 顺序：先删 `scope='system'` 渠道，再删事件表，最后删 `scope` 列；站内历史不可恢复，回滚依赖数据库备份。

存储实现：

- `internal/db/notification_events.go`：事件写入、所有者解析、用户/系统列表、未读统计、单条与全部已读。
- `internal/db/notification_channels_scope.go`：系统渠道查询与 CRUD、`notificationConfigOwner` 加密归属。
- 租户查询统一加 `scope='tenant'`；`GetChannel` 不按 scope 过滤，供 outbox worker 按显式渠道 ID 投递。

## 4. 通知分发

`internal/notify`：

- `NotificationEvent` 增加 `Scope`（空值按 tenant 兼容）。
- 新增 `NotifySystemEvent`；系统事件不携带账号。
- `notifyEvent` 先解析所有者并写站内事件，再按 scope 取渠道、过滤订阅、入队。
- `business_silence` 标签为「业务静默」；`system_error` 展示文案改为「账号异常（未分类）」。
- `businessSilenceAlerter` 改为直接调用 `NotifySystemEvent`，删除宿主账号选择。

## 5. 接口

普通登录用户：

- `GET /api/v1/notifications/events`
- `POST /api/v1/notifications/events/{event_id}/read`
- `POST /api/v1/notifications/events/read-all`

管理员：

- `GET /api/v1/admin/notifications/system-events`
- `POST /api/v1/admin/notifications/system-events/{event_id}/read`
- `POST /api/v1/admin/notifications/system-events/read-all`
- `GET|POST /api/v1/admin/system-alert-channels`
- `GET|PUT|DELETE /api/v1/admin/system-alert-channels/{channel_id}`
- `POST /api/v1/admin/system-alert-channels/{channel_id}/test`

`api/openapi.yaml` 已同步，前端类型由 `npm --prefix frontend run api:generate` 生成。

## 6. 前端

`frontend/app/features/connectors` 扩展：

- `pages/NotificationCenter.tsx`：租户事件列表、未读、单条/全部已读。
- `pages/SystemAlerts.tsx`：系统事件列表 + 系统告警渠道 CRUD + 测试发送。
- `useNotificationEvents.ts`：按 tenant/system 作用域加载与已读，带取消与代次保护。
- 渠道 Hook 参数化为 `useChannelAdmin`，租户与系统各绑一套 API 适配器。
- `NotificationChannelModal` 增加 `events` 可选 prop；系统渠道只展示「业务静默」。
- 路由 `/app/notifications`（所有用户）与 `/app/system-alerts`（管理员）。

## 7. 验证

```bash
bash scripts/guard-local-dev-env.sh
go vet ./...
go test ./... -count=1
go run ./tools/architecturecheck
go run ./tools/commentlint -mode check -root .
npm --prefix frontend run comments:check
npm --prefix frontend run typecheck
npm --prefix frontend run test:coverage
npm --prefix frontend run api:generate
npm --prefix frontend run api:check
```

核心链路覆盖率：

```bash
go test -count=1 -covermode=set -coverprofile=/tmp/cover-core.out \
  ./internal/automation ./internal/engine ./internal/adapter ./internal/db ./internal/xianyu/ws \
  && go tool cover -func=/tmp/cover-core.out | tail -1
```

Web UI e2e：

```bash
sh scripts/compose-functional.sh build seed-sqlite
sh scripts/compose-functional.sh build webui-e2e-test
sh scripts/compose-functional.sh run --rm webui-e2e-test
```

## 8. 假设与限制

- 系统告警未读状态为管理员共享（单 `read_at`）；按管理员独立未读留待后续。
- 本期实例级事件只接入 `business_silence`；数据库不可用等场景仍以进程日志兜底。
- 站内事件暂不做保留期清理；列表限量并有索引，后续按事件量补清理任务。
- 系统渠道与租户渠道共用 `notification_channels` 表、以 `scope` 隔离；租户所有查询与 API 均强制 `scope='tenant'`。
- 账号静音只抑制外部投递，站内历史仍记录。
