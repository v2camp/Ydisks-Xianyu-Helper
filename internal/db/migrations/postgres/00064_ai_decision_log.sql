-- +goose Up
-- AI 接管决策的归因日志：记录每条买家消息「为什么接管 / 为什么没接管」。
--
-- 现状缺口：接管判定为 false 时 Reply 直接返回，既不落库也不记日志，
-- 导致评测发现回复不合格后无法回答「这条消息当时为什么被放行或拦下」。
-- 消息原文只保留 16 位哈希与 60 字预览，避免把完整对话复制一份到审计表；
-- config_fingerprint 用于判断行为变化是否由配置变更引起。
CREATE TABLE IF NOT EXISTS ai_decision_log (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    message_hash TEXT NOT NULL DEFAULT '',
    message_preview TEXT NOT NULL DEFAULT '',
    takeover INTEGER NOT NULL DEFAULT 0,
    hit_intents TEXT NOT NULL DEFAULT '',
    negative_hit INTEGER NOT NULL DEFAULT 0,
    extra_hit INTEGER NOT NULL DEFAULT 0,
    intent_label TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    config_fingerprint TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ai_decision_log_chat ON ai_decision_log(cookie_id, chat_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_ai_decision_log_created ON ai_decision_log(created_at);

-- +goose Down
DROP TABLE IF EXISTS ai_decision_log;
