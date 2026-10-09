-- +goose Up
-- 每日定时下架任务：账号级开关、每天执行时间、参与下架的商品白名单与当日去重标记。
ALTER TABLE account_task_settings ADD COLUMN auto_delist_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE account_task_settings ADD COLUMN delist_time TEXT NOT NULL DEFAULT '09:00';
ALTER TABLE account_task_settings ADD COLUMN delist_item_ids TEXT NOT NULL DEFAULT '';
ALTER TABLE account_task_settings ADD COLUMN last_delist_date TEXT NOT NULL DEFAULT '';
ALTER TABLE account_task_settings ADD COLUMN last_delist_at BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE account_task_settings DROP COLUMN auto_delist_enabled;
ALTER TABLE account_task_settings DROP COLUMN delist_time;
ALTER TABLE account_task_settings DROP COLUMN delist_item_ids;
ALTER TABLE account_task_settings DROP COLUMN last_delist_date;
ALTER TABLE account_task_settings DROP COLUMN last_delist_at;
