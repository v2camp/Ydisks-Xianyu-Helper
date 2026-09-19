-- +goose Up
-- 砍价“待刀成”阶段免拼前发送给买家的账号级安抚模板（留空则不发送）。
ALTER TABLE cookies ADD COLUMN bargain_soothe_template TEXT NOT NULL DEFAULT '' AFTER auto_bargain;
-- 免拼改为账号全量控制：已发布的 skip_pin_items 商品名单在 00053 建立，此处物理移除。
DROP TABLE IF EXISTS skip_pin_items;

-- +goose Down
ALTER TABLE cookies DROP COLUMN bargain_soothe_template;
-- 重建曾由 00053 建立的名单表；Down 必须精确还原三方言 schema 以保持 down-up 链条一致。
CREATE TABLE skip_pin_items (
    cookie_id VARCHAR(255) NOT NULL,
    item_id VARCHAR(64) NOT NULL,
    enabled TINYINT NOT NULL DEFAULT 1,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, item_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;