package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMultiDB_ChatReplyTakeover 在三方言复用夹具上验证重放、会话隔离、关闭设置及恢复比较更新。
func TestMultiDB_ChatReplyTakeover(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 为可用 SQLite/MySQL/PostgreSQL 隔离实例。
		t.Run(target.name, func(t *testing.T) { // t 管理当前方言生命周期与断言。
			defer target.cleanup()
			// store、ctx 为当前方言仓储与本地请求上下文。
			store, ctx := target.store, context.Background()
			// owner、account 为非敏感夹具标识。
			_, account := seedAccount(t, store)
			for _, id := range []string{"chat-a", "chat-b"} { // id 建立可独立接管的两个会话。
				if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: account, ChatID: id, BuyerID: "fixture"}); err != nil { // err 是夹具准备失败。
					t.Fatal(err)
				}
			}
			// minutes、err 检查旧账号不会静默启用新能力。
			minutes, err := store.Chats.ManualReplyPauseMinutes(ctx, account)
			if err != nil || minutes != 0 {
				t.Fatal("legacy account must keep takeover disabled")
			}
			// now 是可检查的毫秒时间，无需等待实际十分钟。
			now := time.Now().UnixMilli()
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "disabled-key", now, now); err != nil { // err 为默认关闭时的无副作用结果。
				t.Fatal(err)
			}
			if err := store.Chats.SetManualReplyPauseMinutes(ctx, account, 10); err != nil { // err 为明确启用十分钟的配置结果。
				t.Fatal(err)
			}
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "manual-key", now, now); err != nil { // err 为首次人工事件持久化结果。
				t.Fatal(err)
			}
			// until、err 为精确截止时间，必须从实际人工发送时间计算。
			until, err := store.Chats.ReplyPausedUntil(ctx, account, "chat-a")
			if err != nil || until != now+600000 {
				t.Fatal("manual reply deadline is not exactly ten minutes")
			}
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "manual-key", now+60000, now+60000); err != nil { // err 为同一消息回显的去重结果。
				t.Fatal(err)
			}
			// repeated、err 验证同一消息晚到一分钟也不能延长。
			repeated, err := store.Chats.ReplyPausedUntil(ctx, account, "chat-a")
			if err != nil || repeated != until {
				t.Fatal("duplicate echo extended takeover")
			}
			// other、err 验证另一个会话不受影响。
			other, err := store.Chats.ReplyPausedUntil(ctx, account, "chat-b")
			if err != nil || other != 0 {
				t.Fatal("takeover crossed conversation boundary")
			}
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "new-manual", now, now+1000); err != nil { // err 模拟同一平台时间但不同本地接纳的另一条人工消息。
				t.Fatal(err)
			}
			if err := store.Chats.ResumeReply(ctx, account, "chat-a", 1); !errors.Is(err, ErrReplyPauseChanged) { // err 必须保留新窗口，即使截止时间与旧消息完全相同。
				t.Fatal("stale resume cleared a newer manual pause")
			}
			if err := store.Chats.ResumeReply(ctx, account, "chat-a", 2); err != nil { // err 为匹配新版本的明确恢复。
				t.Fatal(err)
			}
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "new-manual", now, now+1000); err != nil { // err 验证恢复后旧回显仍无法重新暂停。
				t.Fatal(err)
			}
			// resumed、err 必须保持明确恢复状态。
			resumed, err := store.Chats.ReplyPausedUntil(ctx, account, "chat-a")
			if err != nil || resumed != 0 {
				t.Fatal("old echo reactivated manually resumed conversation")
			}
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "old-history", now-600001, now); err != nil { // err 为已过期历史，不应重置计时。
				t.Fatal(err)
			}
			if err := store.Chats.RecordManualReply(ctx, account, "chat-a", "third-manual", now+2000, now+2000); err != nil { // err 为另一条新的人工回复。
				t.Fatal(err)
			}
			if err := store.Chats.SetManualReplyPauseMinutes(ctx, account, 0); err != nil { // err 为关闭配置并清除现有接管的原子结果。
				t.Fatal(err)
			}
			// closed、err 验证设置关闭后现有窗口已结束。
			closed, err := store.Chats.ReplyPausedUntil(ctx, account, "chat-a")
			if err != nil || closed != 0 {
				t.Fatal("disabling protection retained active pause")
			}
			// paused、_,err 验证整个流程没有借用账号订单暂停。
			paused, _, err := store.Cookies.IsPaused(ctx, account)
			if err != nil || paused {
				t.Fatal("conversation takeover changed account task pause")
			}
		})
	}
}
