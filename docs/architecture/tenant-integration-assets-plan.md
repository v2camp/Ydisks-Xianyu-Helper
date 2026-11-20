# 租户级集成资产 · 设计与迁移

> 状态：**实施中**。本文定义通知渠道、AI 模型与密钥、QQ 机器人凭据、系统 SMTP 统一归属本地用户的方案与迁移语义；不定义重构阶段，不替代 `docs/architecture/refactoring-master-plan.md`。

## 1. 目标

四类集成资产归本地用户（租户），与闲鱼账号保持一对多：

| 资产 | 目标归属 | 解析规则 |
|---|---|---|
| 通知渠道 | 本地用户 | 该用户全部启用渠道对其全部闲鱼账号生效；账号可静音 |
| AI 模型与密钥 | 本地用户 | 账号使用其所有者的配置；缺失即未配置 |
| QQ 机器人凭据 | 本地用户 | 出站按渠道所有者读取；入站按用户启动独立网关 |
| 系统 SMTP | 本地用户 | 邮件渠道使用渠道所有者的 SMTP |

实例级配置保持不变：注册、安全、验证码远程服务、MCP 令牌、日志保留、出站网络策略、AI 总闸 `ai_master_enabled`。

非目标：

- 不保留系统默认兜底；现有全局值一次性迁移给管理员用户。
- 不保留渠道级 QQ/SMTP 凭据覆盖；迁移时折叠到用户级。
- 不保留按闲鱼账号的通知绑定；迁移时删除。

## 2. 数据模型

### 2.1 迁移 `00069_tenant_integration_assets.sql`

- `cookies.notifications_disabled`：账号级静音开关，默认关闭。
- 停用从未被任何账号绑定的通知渠道，避免改为用户级后突然群发。
- 删除 `message_notifications`。
- `ops_conversations.user_id`：运营会话按用户隔离；旧数据归管理员。
- `data_migrations`：记录需要应用层解密/重加密的一次性迁移。

### 2.2 用户级设置

沿用 `user_settings(user_id, key, value)`：

- 普通键明文保存。
- 敏感键（`ai_api_key`、`smtp_password`、`qqbot.app_secret`）使用 AES-256-GCM，scope `user-setting`，owner 为用户 ID。
- 新增 `RedactedForUser`、`ApplyChangesForUser`、`ReadSensitiveUserSetting`。
- 系统设置接口拒绝写入用户集成键。

### 2.3 一次性数据迁移

`Store.MigrateIntegrationSettingsV1` 在 `EncryptLegacySecrets` 之后、服务装配之前执行，失败即启动失败：

1. 解析管理员用户。
2. 全局 AI/SMTP/QQ 键解密后写入管理员 `user_settings`，再删除全局键。
3. 折叠渠道级 QQ/SMTP 凭据：用户级为空则复制；冲突保留用户级并记录键名告警；随后从渠道配置删除凭据字段。
4. 写入 `data_migrations` 标记，保证幂等。

迁移日志只输出计数与用户 ID，不输出秘密。

## 3. 运行时落点

- `internal/db`：用户设置加密、通知投递、运营会话用户过滤、一次性迁移。
- `internal/notify`：QQ/SMTP 只读用户级配置。
- `internal/engine`：AI 配置按账号所有者读取用户设置。
- `internal/application/settings`：用户集成配置用例与写入监听。
- `internal/application/notifications`：删除绑定用例。
- `internal/application/opschat`：会话与消息按用户隔离。
- `internal/qqbot`：按用户网关管理器；命令身份、白名单、会话均按用户。
- `internal/server`：新增用户集成接口；删除绑定接口；运营接口改为登录用户。
- `internal/mcp`：删除通知绑定工具；新增用户集成只读工具。
- 前端：连接器页承载四类资产；账号编辑只保留静音；运营 Agent 对所有用户开放。

## 4. 风险

- 迁移删除 `message_notifications` 与全局键，属单向变更；发布前必须备份数据库与旧镜像。
- 非管理员用户失去全局凭据，需要各自补配；启动日志输出未配置计数。
- QQ 按用户需要多个机器人，平台联调受真实凭据限制。
- `internal/db` 属核心链路，新增文件与分支必须同批补测。

## 5. 验证

按仓库门禁执行：Go build/vet/test、核心链路覆盖率、架构与注释检查、前端 typecheck/test/build、Web UI 冒烟。SQLite 迁移必跑；有 `TEST_POSTGRES_URL` 时补 Postgres。
