-- +goose Up
ALTER TABLE chat_reply_protection ADD COLUMN same_content_cooldown_seconds INTEGER NOT NULL DEFAULT 0;
CREATE TABLE reply_content_cooldowns (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL,
    content_key VARCHAR(64) NOT NULL,
    message_key VARCHAR(255) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    status VARCHAR(16) NOT NULL,
    claimed_at BIGINT NOT NULL,
    lease_until BIGINT NOT NULL,
    last_sent_at BIGINT NOT NULL DEFAULT 0,
    action_started INTEGER NOT NULL DEFAULT 0,
    uncertain INTEGER NOT NULL DEFAULT 0,
    UNIQUE(cookie_id,chat_id,content_key),
    FOREIGN KEY(cookie_id,chat_id) REFERENCES chat_sessions(cookie_id,chat_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_reply_content_cooldown_owner ON reply_content_cooldowns(cookie_id,status,last_sent_at);
-- +goose Down
DROP TABLE reply_content_cooldowns;
ALTER TABLE chat_reply_protection DROP COLUMN same_content_cooldown_seconds;
