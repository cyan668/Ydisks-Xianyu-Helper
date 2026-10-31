package adapter

import (
	"context"
	"errors"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
)

// SameContentCooldownSeconds 读取独立重复消息配置，不借用人工暂停分钟字段。
func (r chatRepository) SameContentCooldownSeconds(ctx context.Context, accountID string) (int, error) {
	return r.store.Chats.SameContentCooldownSeconds(ctx, accountID)
}

// SetSameContentCooldownSeconds 只写入通过应用校验的秒数，不影响人工接管或启停。
func (r chatRepository) SetSameContentCooldownSeconds(ctx context.Context, accountID string, seconds int) error {
	return r.store.Chats.SetSameContentCooldownSeconds(ctx, accountID, seconds)
}

// ListContentCooldownReviews 将数据库分页转换为应用模型，正文摘要从不进入应用或 HTTP。
func (r chatRepository) ListContentCooldownReviews(ctx context.Context, accountID string, limit, offset int, now int64) ([]chatapp.ContentCooldownReview, int, error) {
	// rows、total、err 是归属已核验账号的本地未知保护。
	rows, total, err := r.store.Chats.ListReplyContentReviews(ctx, accountID, limit, offset, now)
	if err != nil {
		return nil, 0, err
	}
	// items 为明确的应用模型，不能直接序列化数据库行。
	items := make([]chatapp.ContentCooldownReview, 0, len(rows))
	for _, row := range rows { // row 只含会话、消息键、毫秒和 CAS 版本。
		items = append(items, chatapp.ContentCooldownReview{ID: row.ID, Version: row.Version, ChatID: row.ChatID, MessageKey: row.MessageKey, ClaimedAt: row.ClaimedAt})
	}
	return items, total, nil
}

// ResolveContentCooldownReview 只解除用户明确核对的账号/版本，冲突转换为稳定应用错误。
func (r chatRepository) ResolveContentCooldownReview(ctx context.Context, accountID string, id, version, now int64) error {
	// err 为本地比较删除结果，不执行旧消息重发。
	err := r.store.Chats.ResolveReplyContentReview(ctx, accountID, db.ReplyContentClaim{ID: id, Version: version}, now)
	if errors.Is(err, db.ErrReplyContentClaimLost) {
		return chatapp.ErrContentReviewChanged
	}
	return err
}
