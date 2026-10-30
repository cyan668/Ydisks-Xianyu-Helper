-- +goose Up
-- 昵称仅供展示和原系统表格往返，不参与匹配。
ALTER TABLE buyer_blocks ADD COLUMN buyer_nick TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE buyer_blocks DROP COLUMN buyer_nick;
