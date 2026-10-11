-- +goose Up
-- 订单商品标题快照：让订单自带商品名，不再依赖本地商品表。
--
-- 现状缺口：订单落库只有 item_id，展示时靠 LEFT JOIN item_info 补商品名。
-- 商品表只由商品同步填充，凡是商品同步没抓到的商品（下架、删除、分页未覆盖），
-- 订单就永远没有名字。实测 325 条订单中有 62 条（19.1%）因此缺名，
-- 且商品软删除并不会丢名，说明缺口来自「商品从未入库」而不是「入库后被删」。
--
-- 平台已售订单列表响应本身携带 itemVO.title，因此订单落库时直接冗余该快照，
-- 与闲鱼官方「删除商品后订单信息仍然完整」的行为一致。
ALTER TABLE orders ADD COLUMN IF NOT EXISTS item_title TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE orders DROP COLUMN IF EXISTS item_title;
