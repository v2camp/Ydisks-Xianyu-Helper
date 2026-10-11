-- +goose Up
-- AI 生成归因日志：回答「这条回复到底是谁产生的、依据是什么」。
--
-- 两个正交维度，都是业务方反复追问的问题：
--   answer_source   答案来源：策略话术还是模型生成（价格守卫会整段替换模型原稿）。
--   grounding_source 事实来源：发货渠道来自卡密事实、发货配置模板，还是商品描述关键词。
--                    只有 card-channel 是事实；后两者都仍是从文本里猜的，
--                    因此该字段的分布就是「发货能力是否真正替换掉文本规则」的证据。
--
-- 与 00064 的 ai_decision_log 分工：那张表回答「为什么接管或不接管」，
-- 本表回答「接管之后答了什么、依据是什么」。prompt_hash 而非提示全文：
-- 提示中含商品原始 JSON，落全文会显著放大体积并复制敏感内容。
CREATE TABLE IF NOT EXISTS ai_generation_log (
    id BIGSERIAL PRIMARY KEY,
    cookie_id TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    message_hash TEXT NOT NULL DEFAULT '',
    message_preview TEXT NOT NULL DEFAULT '',
    answer_source TEXT NOT NULL DEFAULT '',
    guarded INTEGER NOT NULL DEFAULT 0,
    guard_reason TEXT NOT NULL DEFAULT '',
    grounding_source TEXT NOT NULL DEFAULT '',
    grounding_detail TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_hash TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    reply_preview TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_ai_generation_log_chat ON ai_generation_log(cookie_id, chat_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_ai_generation_log_answer ON ai_generation_log(answer_source);
CREATE INDEX IF NOT EXISTS idx_ai_generation_log_grounding ON ai_generation_log(grounding_source);
CREATE INDEX IF NOT EXISTS idx_ai_generation_log_created ON ai_generation_log(created_at);

-- +goose Down
DROP TABLE IF EXISTS ai_generation_log;