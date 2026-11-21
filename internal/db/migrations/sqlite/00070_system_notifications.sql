-- +goose Up
-- 通知渠道增加作用域：tenant 为租户级渠道，system 为管理员系统告警渠道。
-- 系统渠道复用同一张表以保留 outbox 外键与发送器，但所有租户查询必须强制 scope='tenant'。
ALTER TABLE notification_channels ADD COLUMN scope TEXT NOT NULL DEFAULT 'tenant';
CREATE INDEX IF NOT EXISTS idx_notification_channels_scope_user
    ON notification_channels(scope, user_id, enabled);

-- 站内通知事件：租户事件按账号所有者隔离，系统事件只对管理员可见。
CREATE TABLE IF NOT EXISTS notification_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL DEFAULT 'tenant',
    owner_user_id INTEGER NOT NULL DEFAULT 0,
    account_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL DEFAULT '',
    level TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    dedupe_key TEXT,
    read_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_events_dedupe ON notification_events(dedupe_key);
CREATE INDEX IF NOT EXISTS idx_notification_events_owner ON notification_events(scope, owner_user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_notification_events_unread ON notification_events(owner_user_id, read_at, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_notification_events_unread;
DROP INDEX IF EXISTS idx_notification_events_owner;
DROP INDEX IF EXISTS idx_notification_events_dedupe;
DROP TABLE IF EXISTS notification_events;

-- 先删除系统渠道，避免删列后它们退化为租户渠道并向租户账号投递。
DELETE FROM notification_channels WHERE scope='system';
DROP INDEX IF EXISTS idx_notification_channels_scope_user;
ALTER TABLE notification_channels DROP COLUMN scope;
