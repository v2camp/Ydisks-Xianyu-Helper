-- +goose Up
-- 卡券发货渠道：在贴入或导入卡密时由应用层自动提取并落库，供 AI 客服回答
-- 「有夸克吗 / 什么网盘」时使用确定事实，不再用「以商品详情为准」打太极。
-- 存储值为逗号连接的渠道展示名（如「百度网盘,夸克网盘」），空串表示未识别。
ALTER TABLE cards ADD COLUMN delivery_channels TEXT NOT NULL DEFAULT '';

-- 存量回填：判定口径与 internal/netpan 一致，域名优先、关键词兜底。
-- 只回填尚未识别的行，重复执行幂等；用 ILIKE 兼容大小写。
UPDATE cards SET delivery_channels =
  TRIM(',' FROM
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%pan.baidu.com%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%百度网盘%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%百度云盘%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%度盘%'
         THEN '百度网盘,' ELSE '' END ||
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%pan.quark.cn%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%夸克%'
         THEN '夸克网盘,' ELSE '' END ||
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%pan.xunlei.com%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%迅雷%'
         THEN '迅雷网盘,' ELSE '' END ||
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%alipan.com%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%aliyundrive.com%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%阿里云盘%'
         THEN '阿里云盘,' ELSE '' END ||
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%cloud.189.cn%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%天翼云盘%'
         THEN '天翼云盘,' ELSE '' END ||
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%115.com%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%115cdn.com%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%115网盘%'
         THEN '115网盘,' ELSE '' END ||
    CASE WHEN COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%drive.uc.cn%'
              OR COALESCE(text_content, '') || ' ' || COALESCE(data_content, '') ILIKE '%UC网盘%'
         THEN 'UC网盘,' ELSE '' END
  )
WHERE COALESCE(delivery_channels, '') = '';

-- +goose Down
ALTER TABLE cards DROP COLUMN delivery_channels;
