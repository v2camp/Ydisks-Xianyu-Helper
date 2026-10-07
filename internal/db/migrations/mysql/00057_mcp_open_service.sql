-- +goose Up
-- MCP 开放服务的持久化令牌表：只保存 SHA-256 哈希，明文令牌仅在生成/轮换响应中出现一次。
-- kind=current 是当前令牌（至多一行），kind=previous 是轮换后 24 小时宽限期内的旧令牌（至多一行）。
-- expires_at 为 Unix 秒，0 表示不过期；宽限期旧令牌到期后由校验路径惰性清理。
CREATE TABLE mcp_tokens (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'current',
    created_at BIGINT NOT NULL,
    last_used_at BIGINT NOT NULL DEFAULT 0,
    expires_at BIGINT NOT NULL DEFAULT 0,
    UNIQUE KEY idx_mcp_tokens_hash (token_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- MCP 调用审计表：记录 tools/call、resources/read、prompts/get 的非敏感追溯信息。
-- arguments 只允许写入键级脱敏后的参数摘要，禁止包含任何秘密值或明文卡密。
CREATE TABLE mcp_call_audit (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    created_at BIGINT NOT NULL,
    user_id BIGINT NOT NULL DEFAULT 0,
    token_source VARCHAR(16) NOT NULL,
    category VARCHAR(16) NOT NULL,
    name VARCHAR(128) NOT NULL,
    cookie_id VARCHAR(255) NOT NULL DEFAULT '',
    arguments TEXT NOT NULL,
    success TINYINT NOT NULL,
    error_class VARCHAR(32) NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    KEY idx_mcp_audit_created (created_at),
    KEY idx_mcp_audit_name (name),
    KEY idx_mcp_audit_cookie (cookie_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS mcp_call_audit;
DROP TABLE IF EXISTS mcp_tokens;
