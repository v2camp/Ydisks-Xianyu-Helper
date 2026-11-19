-- +goose Up
-- 运营 Agent（QQ 通道）会话与消息留痕。
--
-- 入站消息由 qqbot 传输层经注入的 Recorder 端口写入，命令回复与 Web 人工发送写入出站消息。
-- 平台 msg_id 只在非空时参与去重：命令回复与 Web 发送没有平台回执，重复写入不能依赖它。
-- 时间列统一使用 Unix 毫秒，与保留策略的 cutoffMillis 对齐。
CREATE TABLE IF NOT EXISTS ops_conversations (
    id BIGSERIAL PRIMARY KEY,
    channel TEXT NOT NULL DEFAULT 'qq',
    target_openid TEXT NOT NULL DEFAULT '',
    is_group INTEGER NOT NULL DEFAULT 0,
    last_message_at BIGINT NOT NULL DEFAULT 0,
    last_message_preview TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL DEFAULT 0,
    UNIQUE(channel, target_openid)
);
CREATE INDEX IF NOT EXISTS idx_ops_conversations_recent ON ops_conversations(last_message_at DESC);

CREATE TABLE IF NOT EXISTS ops_messages (
    id BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL,
    platform_msg_id TEXT NOT NULL DEFAULT '',
    direction TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL DEFAULT 0,
    FOREIGN KEY(conversation_id) REFERENCES ops_conversations(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ops_messages_conversation ON ops_messages(conversation_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ops_messages_platform ON ops_messages(conversation_id, platform_msg_id)
    WHERE platform_msg_id <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_ops_messages_platform;
DROP INDEX IF EXISTS idx_ops_messages_conversation;
DROP TABLE IF EXISTS ops_messages;
DROP INDEX IF EXISTS idx_ops_conversations_recent;
DROP TABLE IF EXISTS ops_conversations;
