package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
)

// TestContentCooldownRepositoryBoundary 核对数据库到应用的模型转换、账号隔离与旧版本冲突。
func TestContentCooldownRepositoryBoundary(t *testing.T) {
	// store、cleanup 为隔离 SQLite，不启用平台账号。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 为本地管理用例上下文。
	ctx := context.Background()
	// repository 是生产相同的窄聊天适配器。
	repository := chatRepository{store: store}
	if err := repository.SetSameContentCooldownSeconds(ctx, "cid", 3600); err != nil { // err 核对配置写入转发。
		t.Fatal(err)
	}
	if seconds, err := repository.SameContentCooldownSeconds(ctx, "cid"); err != nil || seconds != 3600 { // seconds、err 是非敏感配置回读。
		t.Fatal("cooldown setting lost in adapter")
	}
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "cid", ChatID: "chat"}); err != nil { // err 为虚构会话准备。
		t.Fatal(err)
	}
	// now 为触发消息的接纳毫秒。
	now := time.Now().UnixMilli()
	// claim、allowed、err 是发送结果未知的真实数据库领取。
	claim, allowed, err := store.Chats.ClaimReplyContent(ctx, "cid", "chat", "private-digest", "buyer-message", now)
	if err != nil || !allowed {
		t.Fatal("failed to seed unknown result")
	}
	if err := store.Chats.BeginReplyContentDelivery(ctx, claim, now+1); err != nil { // err 为持久化发送检查点。
		t.Fatal(err)
	}
	if err := store.Chats.ObserveReplyContentDelivery(ctx, claim, false, true, now+2); err != nil { // err 保存未知投递事实。
		t.Fatal(err)
	}
	if err := store.Chats.FinishReplyContent(ctx, claim); err != nil { // err 将领取收口为可管理的保护。
		t.Fatal(err)
	}
	// items、total、err 核对应用模型只保留定位信息与版本。
	items, total, err := repository.ListContentCooldownReviews(ctx, "cid", 20, 0, now+3)
	if err != nil || total != 1 || len(items) != 1 || items[0].ChatID != "chat" || items[0].MessageKey != "buyer-message" || items[0].ClaimedAt != now {
		t.Fatal("review metadata conversion failed")
	}
	if err := repository.ResolveContentCooldownReview(ctx, "cid", claim.ID, claim.Version+1, now+3); !errors.Is(err, chatapp.ErrContentReviewChanged) { // err 必须转换为应用冲突，不能泄露数据库类别。
		t.Fatal("stale version did not become application conflict")
	}
	if err := repository.ResolveContentCooldownReview(ctx, "cid", claim.ID, claim.Version, now+3); err != nil { // err 为精确保护版本的恢复转发。
		t.Fatal(err)
	}
	if err := store.DB.Close(); err != nil { // err 关闭隔离库以核对列表故障原样返回。
		t.Fatal(err)
	}
	if _, _, err := repository.ListContentCooldownReviews(ctx, "cid", 20, 0, now+3); err == nil { // err 不能被伪装成空列表。
		t.Fatal("adapter hid database failure")
	}
}
