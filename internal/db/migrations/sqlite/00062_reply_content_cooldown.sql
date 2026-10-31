-- +goose Up
ALTER TABLE chat_reply_protection ADD COLUMN same_content_cooldown_seconds INTEGER NOT NULL DEFAULT 0;
CREATE TABLE reply_content_cooldowns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    content_key TEXT NOT NULL,
    message_key TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL,
    claimed_at INTEGER NOT NULL,
    lease_until INTEGER NOT NULL,
    last_sent_at INTEGER NOT NULL DEFAULT 0,
    action_started INTEGER NOT NULL DEFAULT 0,
    uncertain INTEGER NOT NULL DEFAULT 0,
    UNIQUE(cookie_id,chat_id,content_key),
    FOREIGN KEY(cookie_id,chat_id) REFERENCES chat_sessions(cookie_id,chat_id) ON DELETE CASCADE
);
CREATE INDEX idx_reply_content_cooldown_owner ON reply_content_cooldowns(cookie_id,status,last_sent_at);
-- +goose Down
DROP TABLE reply_content_cooldowns;
ALTER TABLE chat_reply_protection DROP COLUMN same_content_cooldown_seconds;
