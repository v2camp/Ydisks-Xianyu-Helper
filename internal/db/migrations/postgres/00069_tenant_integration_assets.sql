-- +goose Up
-- 通知渠道归属本地用户：删除按闲鱼账号的绑定表，改为账号所有者全部启用渠道生效。
-- 从未被任何账号绑定的渠道视为“从未投递”，迁移时直接停用，避免改为用户级后突然群发。
ALTER TABLE cookies ADD COLUMN IF NOT EXISTS notifications_disabled BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE notification_channels
   SET enabled = FALSE
 WHERE id NOT IN (SELECT DISTINCT channel_id FROM message_notifications);

DROP TABLE IF EXISTS message_notifications;

-- 运营 Agent 会话按本地用户隔离；旧会话只可能来自管理员，统一归给当前管理员。
ALTER TABLE ops_conversations ADD COLUMN IF NOT EXISTS user_id BIGINT NOT NULL DEFAULT 0;
UPDATE ops_conversations
   SET user_id = COALESCE((SELECT id FROM users WHERE is_admin = TRUE ORDER BY id LIMIT 1), 0);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_conversations_user_target
    ON ops_conversations(user_id, channel, target_openid);
CREATE INDEX IF NOT EXISTS idx_ops_conversations_user_recent
    ON ops_conversations(user_id, last_message_at DESC);

-- 应用层一次性数据迁移标记：全局凭据与渠道级凭据折叠到用户级需要解密/重加密。
CREATE TABLE IF NOT EXISTS data_migrations (
    name TEXT PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP INDEX IF EXISTS idx_ops_conversations_user_recent;
DROP INDEX IF EXISTS idx_ops_conversations_user_target;
ALTER TABLE ops_conversations DROP COLUMN IF EXISTS user_id;
DROP TABLE IF EXISTS data_migrations;
ALTER TABLE cookies DROP COLUMN IF EXISTS notifications_disabled;

-- 仅恢复空表结构；绑定数据不可恢复，生产回滚依赖数据库备份。
CREATE TABLE IF NOT EXISTS message_notifications (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    enabled INTEGER DEFAULT 1,
    event_types TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cookie_id, channel_id)
);
