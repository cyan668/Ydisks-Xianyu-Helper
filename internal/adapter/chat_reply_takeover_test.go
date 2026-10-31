package adapter

import (
	"context"
	"testing"
	"time"

	chatapp "xianyu-go/internal/application/chat"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/engine"
)

// TestOutgoingTakeoverUsesTrustedOriginAndStableKey 验证引擎来源在真实本地消息落库边界上决定接管，自动/未知来源不触发。
func TestOutgoingTakeoverUsesTrustedOriginAndStableKey(t *testing.T) {
	// store、cleanup 是隔离 SQLite 及资源所有权。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 为本地事件处理上下文。
	ctx := context.Background()
	// adapter 的发送来源来自引擎窄模型，不能通过 HTTP 传入。
	adapter := New(store, nil, nil)
	adapter.chat = domainchat.New(store)
	adapter.takeover = chatapp.New(NewChatRepository(store))
	if err := store.Chats.SetManualReplyPauseMinutes(ctx, "cid", 10); err != nil { // err 为本地明确配置十分钟的结果。
		t.Fatal(err)
	}
	// now 固定人工/自动消息的实际时间以核对精确截止。
	now := time.Now().UnixMilli()
	// automatic 是本地自动投递的明确来源。
	automatic := engine.OutgoingChatMessage{AccountID: "cid", ChatID: "chat", MessageKey: "auto-pnm", Text: "fixture auto", Origin: engine.OutgoingAutomatic, SentAt: now}
	if err := adapter.HandleOutgoingChatMessage(ctx, automatic); err != nil { // err 为自动消息的旁路落库结果。
		t.Fatal(err)
	}
	// until、err 验证自动消息没有暂停该会话。
	until, err := store.Chats.ReplyPausedUntil(ctx, "cid", "chat")
	if err != nil || until != 0 {
		t.Fatal("automatic outgoing message triggered takeover")
	}
	automatic.Origin, automatic.MessageKey = "", "unknown-pnm"
	if err := adapter.HandleOutgoingChatMessage(ctx, automatic); err != nil { // err 为未知来源的保守落库结果。
		t.Fatal(err)
	}
	// manual 是已经由来源跟踪器判定的跨端实时人工消息。
	manual := engine.OutgoingChatMessage{AccountID: "cid", ChatID: "chat", MessageKey: "manual-pnm", Text: "fixture manual", Origin: engine.OutgoingExternal, SentAt: now}
	if err := adapter.HandleOutgoingChatMessage(ctx, manual); err != nil { // err 为跨端人工消息的接管持久化结果。
		t.Fatal(err)
	}
	until, err = store.Chats.ReplyPausedUntil(ctx, "cid", "chat")
	if err != nil || until != now+600000 {
		t.Fatal("trusted external manual reply did not establish exact pause")
	}
	manual.MessageKey, manual.TakeoverKey, manual.Origin, manual.SentAt = "second-observation-pnm", "manual-pnm", engine.OutgoingManual, now+1000
	if err := adapter.HandleOutgoingChatMessage(ctx, manual); err != nil { // err 为本地键与平台键关联后的重复观察结果。
		t.Fatal(err)
	}
	// repeated、err 必须保留首次截止，不能因另一观察路径延长。
	repeated, err := store.Chats.ReplyPausedUntil(ctx, "cid", "chat")
	if err != nil || repeated != until {
		t.Fatal("correlated local/platform observations extended same takeover")
	}
}
