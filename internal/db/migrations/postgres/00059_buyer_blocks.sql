-- +goose Up
-- 交易黑名单只存稳定买家身份与作用域；关单占用不保存凭据、卡密或消息正文。
CREATE TABLE buyer_blocks (
 id BIGSERIAL PRIMARY KEY,
 user_id BIGINT NOT NULL,
 cookie_id TEXT NOT NULL DEFAULT '',
 item_id TEXT NOT NULL DEFAULT '',
 buyer_id TEXT NOT NULL,
 reason TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 0,
 auto_close INTEGER NOT NULL DEFAULT 0,
 UNIQUE(user_id,cookie_id,item_id,buyer_id),
 FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_buyer_blocks_owner ON buyer_blocks(user_id,buyer_id);
CREATE TABLE buyer_block_closes (
 cookie_id TEXT NOT NULL,
 order_id TEXT NOT NULL,
 block_id BIGINT NOT NULL,
 status TEXT NOT NULL,
 PRIMARY KEY(cookie_id,order_id),
 FOREIGN KEY(cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
-- 账号删除后避免 ID 复用继承旧账号级规则或关单占用；用户级规则不绑定账号。
-- +goose Down
DROP TABLE buyer_block_closes;
DROP TABLE buyer_blocks;
