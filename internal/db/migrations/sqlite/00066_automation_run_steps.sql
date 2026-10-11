-- +goose Up
-- 自动发货链路步骤留痕：回答「这次发货卡在哪一步」。
--
-- 现状缺口：automation_runs 只有最终 status 与 error_message，
-- 规则匹配、卡密选择、消息动作、凭证保存这几个阶段没有分别留痕，
-- 排查时只能看到终态。��为终态 success 只代表动作调用返回成功，
-- 不代表买家实际收到，平台静默拦截时系统记为成功而丢件无法被发现。
--
-- 延迟会让同一动作多次执行（deferred），因此以 (run_id, cursor, attempt) 定位一次尝试，
-- 而不是用自增 id 当唯一键去判断「是否已经记录过」。
CREATE TABLE IF NOT EXISTS automation_run_steps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL DEFAULT 0,
    rule_id INTEGER NOT NULL DEFAULT 0,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    action_cursor INTEGER NOT NULL DEFAULT -1,
    step TEXT NOT NULL DEFAULT '',
    action_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_automation_run_steps_run ON automation_run_steps(run_id, id);
CREATE INDEX IF NOT EXISTS idx_automation_run_steps_created ON automation_run_steps(created_at);

-- +goose Down
DROP TABLE IF EXISTS automation_run_steps;