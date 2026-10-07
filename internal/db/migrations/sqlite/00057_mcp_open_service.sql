-- +goose Up
-- MCP 开放服务的持久化令牌表：只保存 SHA-256 哈希，明文令牌仅在生成/轮换响应中出现一次。
-- kind=current 是当前令牌（至多一行），kind=previous 是轮换后 24 小时宽限期内的旧令牌（至多一行）。
-- expires_at 为 Unix 秒，0 表示不过期；宽限期旧令牌到期后由校验路径惰性清理。
CREATE TABLE mcp_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'current',
    created_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0
);
-- 令牌哈希全局唯一，常量时间比对前先按哈希取行。
CREATE UNIQUE INDEX idx_mcp_tokens_hash ON mcp_tokens(token_hash);

-- MCP 调用审计表：记录 tools/call、resources/read、prompts/get 的非敏感追溯信息。
-- arguments 只允许写入键级脱敏后的参数摘要，禁止包含任何秘密值或明文卡密。
CREATE TABLE mcp_call_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at INTEGER NOT NULL,
    user_id INTEGER NOT NULL DEFAULT 0,
    token_source TEXT NOT NULL,
    category TEXT NOT NULL,
    name TEXT NOT NULL,
    cookie_id TEXT NOT NULL DEFAULT '',
    arguments TEXT NOT NULL DEFAULT '',
    success INTEGER NOT NULL,
    error_class TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0
);
-- 审计按时间倒序分页查询，并支持按工具名与账号归属过滤。
CREATE INDEX idx_mcp_audit_created ON mcp_call_audit(created_at);
CREATE INDEX idx_mcp_audit_name ON mcp_call_audit(name);
CREATE INDEX idx_mcp_audit_cookie ON mcp_call_audit(cookie_id);

-- +goose Down
DROP TABLE IF EXISTS mcp_call_audit;
DROP TABLE IF EXISTS mcp_tokens;
