package adapter

import (
	"context"
	"errors"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// chatOutgoingContext 在适配边界显式区分人工和自动客服，不允许相同发送管线丢失来源。
func chatOutgoingContext(ctx context.Context, key string) context.Context {
	ctx = engine.WithOutgoingMessageKey(ctx, key)
	if chatapp.IsAutomaticReply(ctx) {
		return engine.WithOutgoingOrigin(ctx, engine.OutgoingAutomatic)
	}
	return engine.WithOutgoingOrigin(ctx, engine.OutgoingManual)
}

// ManualReplyPauseMinutes 适配独立接管配置，不复用账号订单暂停。
func (r chatRepository) ManualReplyPauseMinutes(ctx context.Context, accountID string) (int, error) {
	return r.store.Chats.ManualReplyPauseMinutes(ctx, accountID)
}

// SetManualReplyPauseMinutes 保存已完成归属和范围校验的分钟配置。
func (r chatRepository) SetManualReplyPauseMinutes(ctx context.Context, accountID string, minutes int) error {
	return r.store.Chats.SetManualReplyPauseMinutes(ctx, accountID, minutes)
}

// ReplyPausedUntil 返回当前会话毫秒截止时间，数据库错误原样交给应用层。
func (r chatRepository) ReplyPausedUntil(ctx context.Context, accountID, chatID string) (int64, error) {
	return r.store.Chats.ReplyPausedUntil(ctx, accountID, chatID)
}

// ConversationReplyState 只转换会话截止时间与递增版本，保持快照原子读取。
func (r chatRepository) ConversationReplyState(ctx context.Context, accountID, chatID string) (int64, int64, error) {
	return r.store.Chats.ConversationReplyState(ctx, accountID, chatID)
}

// LastManualReplyAt 返回独立人工事件水位，手动恢复不会重放旧 AI 回复。
func (r chatRepository) LastManualReplyAt(ctx context.Context, accountID, chatID string) (int64, error) {
	return r.store.Chats.LastManualReplyAt(ctx, accountID, chatID)
}

// RecordManualReply 仅持久化应用层已验证的人工事件。
func (r chatRepository) RecordManualReply(ctx context.Context, accountID, chatID, key string, sentAt, now int64) error {
	return r.store.Chats.RecordManualReply(ctx, accountID, chatID, key, sentAt, now)
}

// ResumeReply 将状态版本冲突转换为应用错误，不清除并发产生的新暂停。
func (r chatRepository) ResumeReply(ctx context.Context, accountID, chatID string, expected int64) error {
	// err 是当前用户会话的版本比较结果。
	err := r.store.Chats.ResumeReply(ctx, accountID, chatID, expected)
	if errors.Is(err, db.ErrReplyPauseChanged) {
		return chatapp.ErrReplyPauseChanged
	}
	return err
}
