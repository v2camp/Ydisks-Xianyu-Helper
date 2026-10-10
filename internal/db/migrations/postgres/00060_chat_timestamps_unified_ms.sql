-- +goose Up
-- 聊天域时间字段统一为 Unix 毫秒：历史数据按秒落库（time.Now().UTC().Unix()），
-- 而 sent_at / last_message_at 自平台与出站链路落库的一直是毫秒，同一张表内两种单位并存，
-- 导致按 created_at 排序、跨字段比较与前端解析都会错乱。
--
-- 转换只作用于「明显是秒级」的存量值：真实毫秒时间戳量级为 1e12，秒级为 1e9，
-- 以 1e11 为界可安全区分；已转换或为零值不受影响，重复执行幂等。
UPDATE chat_messages SET created_at = created_at * 1000 WHERE created_at > 0 AND created_at < 100000000000;
UPDATE chat_sessions SET created_at = created_at * 1000 WHERE created_at > 0 AND created_at < 100000000000;
UPDATE chat_sessions SET updated_at = updated_at * 1000 WHERE updated_at > 0 AND updated_at < 100000000000;
UPDATE chat_quick_replies SET created_at = created_at * 1000 WHERE created_at > 0 AND created_at < 100000000000;
UPDATE chat_buyer_notes SET updated_at = updated_at * 1000 WHERE updated_at > 0 AND updated_at < 100000000000;

-- +goose Down
-- 回滚把毫秒值还原为秒级；同样以 1e11 为界，避免误伤本就是秒的历史值。
UPDATE chat_messages SET created_at = created_at / 1000 WHERE created_at > 100000000000;
UPDATE chat_sessions SET created_at = created_at / 1000 WHERE created_at > 100000000000;
UPDATE chat_sessions SET updated_at = updated_at / 1000 WHERE updated_at > 100000000000;
UPDATE chat_quick_replies SET created_at = created_at / 1000 WHERE created_at > 100000000000;
UPDATE chat_buyer_notes SET updated_at = updated_at / 1000 WHERE updated_at > 100000000000;
