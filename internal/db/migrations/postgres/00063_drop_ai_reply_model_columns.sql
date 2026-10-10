-- +goose Up
-- 移除账号级模型配置三列，坐实「模型配置单一来源」。
-- 权威来源已统一为 system_settings 的 ai_model / ai_api_url / ai_base_url（由 00003 建立）：
-- 写入侧 UpsertSettings 不再写这三列，读取侧 ListForUser 不读，前端零引用，
-- AIReply.Get 虽仍 SELECT 却在取得后从不消费（唯一消费者 engine/ai.go 只用 AIEnabled
-- 与砍价护栏字段），api_key 还被无意义地解密一次。
-- 删除 api_key 同时缩小敏感数据面：本表不再承载任何可读密钥。
ALTER TABLE ai_reply_settings DROP COLUMN model_name;
ALTER TABLE ai_reply_settings DROP COLUMN api_key;
ALTER TABLE ai_reply_settings DROP COLUMN base_url;

-- +goose Down
-- 按 00001 原始定义重建三列，保持 Up 之前的结构形状。
-- 列内历史值不可恢复，重建后为空；Down 后模型配置仍以 system_settings 为准。
ALTER TABLE ai_reply_settings ADD COLUMN model_name TEXT DEFAULT 'qwen-plus';
ALTER TABLE ai_reply_settings ADD COLUMN api_key TEXT;
ALTER TABLE ai_reply_settings ADD COLUMN base_url TEXT DEFAULT 'https://dashscope.aliyuncs.com/compatible-mode/v1';
