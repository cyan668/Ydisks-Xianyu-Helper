package engine

import (
	"context"
	"errors"
	"time"
)

// ErrReplyTakeover 表示发送前发生人工接管，调用方跳过自动回复而非报告发送成功或不确定。
var ErrReplyTakeover = errors.New("人工接管已阻止本次自动回复")

// replyPaused 在解析与发送前读取会话接管；本地读取失败阻止自动客服，不改变订单事件分流。
func (r *ReplyService) replyPaused(ctx context.Context, chatID string, observedAt int64) (bool, error) {
	if r.store == nil || r.store.Chats == nil || chatID == "" {
		return false, nil
	}
	// until、err 为当前会话最新暂停截止和读取故障。
	until, err := r.store.Chats.ReplyPausedUntil(ctx, r.cookieID, chatID)
	if err != nil || until > time.Now().UnixMilli() {
		return until > time.Now().UnixMilli(), err
	}
	// lastManual、err 是人工水位，明确恢复以后也保留，防止旧生成内容补发。
	lastManual, err := r.store.Chats.LastManualReplyAt(ctx, r.cookieID, chatID)
	return observedAt > 0 && lastManual >= observedAt, err
}
