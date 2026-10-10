-- +goose Up
-- 账号级客服 Agent 配置：开关与档位两列均可空。
-- NULL 表示继承租户默认，因此本次迁移不改变任何既有账号的现行行为。
-- 不设 CHECK 约束：档位合法性的判定单点在 internal/capability 的 Preset.Valid()，
-- 数据库不重复策略校验，两方言也就此保持一致。
ALTER TABLE ai_reply_settings ADD COLUMN agent_enabled INTEGER;
ALTER TABLE ai_reply_settings ADD COLUMN agent_preset TEXT;

-- +goose Down
ALTER TABLE ai_reply_settings DROP COLUMN agent_enabled;
ALTER TABLE ai_reply_settings DROP COLUMN agent_preset;
