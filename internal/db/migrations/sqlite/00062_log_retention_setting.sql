-- +goose Up
-- 消息与日志的统一保留天数：默认 30 天，可在系统设置中调整。
-- 同一保留期覆盖聊天消息、WS 诊断帧与各审计日志表，避免本地库随时间无界增长。
INSERT OR IGNORE INTO system_settings (key, value, description) VALUES
    ('log_retention_days', '30', '消息与日志保留天数：至少 10 天，默认 30 天');

-- +goose Down
DELETE FROM system_settings WHERE key = 'log_retention_days';
