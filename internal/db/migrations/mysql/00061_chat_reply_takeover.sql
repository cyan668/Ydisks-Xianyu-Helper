-- +goose Up
CREATE TABLE chat_reply_protection (
    cookie_id VARCHAR(191) PRIMARY KEY,
    manual_pause_minutes INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
ALTER TABLE chat_sessions ADD COLUMN reply_paused_until BIGINT NOT NULL DEFAULT 0;
ALTER TABLE chat_sessions ADD COLUMN reply_pause_version BIGINT NOT NULL DEFAULT 0;
CREATE TABLE chat_manual_reply_events (
    cookie_id VARCHAR(191) NOT NULL,
    chat_id VARCHAR(191) NOT NULL,
    message_key VARCHAR(255) NOT NULL,
    manual_at BIGINT NOT NULL,
    PRIMARY KEY (cookie_id, message_key),
    FOREIGN KEY (cookie_id, chat_id) REFERENCES chat_sessions(cookie_id, chat_id) ON DELETE CASCADE
);
CREATE INDEX idx_chat_manual_reply_time ON chat_manual_reply_events(cookie_id,chat_id,manual_at);
-- +goose Down
DROP TABLE chat_manual_reply_events;
ALTER TABLE chat_sessions DROP COLUMN reply_paused_until;
ALTER TABLE chat_sessions DROP COLUMN reply_pause_version;
DROP TABLE chat_reply_protection;
