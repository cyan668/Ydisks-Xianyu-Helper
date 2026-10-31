package engine

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// pauseDuringResolve 在生成内容期间模拟人工接管，不能让旧 API/AI 结果穿透发送前门禁。
type pauseDuringResolve struct {
	// store 为隔离 SQLite，只有本地事件写入。
	store *db.Store
}

// Reply 在返回回复前先建立人工接管，重现生成期间的交错。
func (f pauseDuringResolve) Reply(ctx context.Context, _ ChatMessage) (*ReplyResult, error) {
	// now 是人工事件的实际毫秒时间。
	now := time.Now().UnixMilli()
	if err := f.store.Chats.RecordManualReply(ctx, "cid", "chat", "manual-during-ai", now, now); err != nil { // err 为本地持久化结果。
		return nil, err
	}
	return &ReplyResult{Text: "must not send"}, nil
}

// TestReplyPauseBeforeResolutionAndDelivery 验证已暂停不调 AI/API，生成期间的人工接管也阻止投递。
func TestReplyPauseBeforeResolutionAndDelivery(t *testing.T) {
	// store、cleanup 为本地账号及回收责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 为本地回复调用上下文。
	ctx := context.Background()
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "cid", ChatID: "chat"}); err != nil { // err 为虚构会话准备失败。
		t.Fatal(err)
	}
	if err := store.Chats.SetManualReplyPauseMinutes(ctx, "cid", 10); err != nil { // err 为明确启用接管配置。
		t.Fatal(err)
	}
	// delivery 只记录实际投递，不连接平台。
	delivery := &recordingReplyDelivery{}
	// service 首先由 API 生成期间建立暂停。
	service := NewReplyService("cid", store, delivery, pauseDuringResolve{store: store}, nil, nil)
	if err := service.Handle(ctx, chatMsg("hello", "", "chat")); err != nil || len(delivery.messages) != 0 { // err 验证发送前复核阻止旧生成结果。
		t.Fatal("manual takeover during generation allowed delivery")
	}
	// api 在已有接管状态下不应被调用。
	api := &fakeAPIReplier{result: &ReplyResult{Text: "must not resolve"}}
	service.api = api
	if err := service.Handle(ctx, chatMsg("hello", "", "chat")); err != nil || api.called != 0 { // err 验证暂停首先阻止生成，避免无意义请求。
		t.Fatal("paused conversation invoked API replier")
	}
}
