package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/ws"
)

// cooldownTestReplier 拥有原子调用计数，可在生成期间暂停以验证并发去重；无平台 I/O。
type cooldownTestReplier struct {
	// calls 可由并发断言读取，只有真实进入生成的调用才递增。
	calls atomic.Int64
	// generate 控制本地生成结果或交错，所有调用由测试等待完成。
	generate func(context.Context, ChatMessage) (*ReplyResult, error)
}

// Reply 记录真实进入生成的次数，再执行当前测试的可控生成函数。
func (r *cooldownTestReplier) Reply(ctx context.Context, message ChatMessage) (*ReplyResult, error) {
	r.calls.Add(1)
	return r.generate(ctx, message)
}

// newCooldownReplyStore 建立两个隔离会话和明确一小时设置，所有账号保持原停用状态。
func newCooldownReplyStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// store、cleanup 是原回复夹具及释放责任。
	store, cleanup := newReplyStore(t)
	for _, chat := range []string{"chat-a", "chat-b"} { // chat 为同账号下的独立会话。
		if err := store.Chats.UpsertSession(context.Background(), db.ChatSession{CookieID: "cid", ChatID: chat}); err != nil {
			cleanup()
			t.Fatal(err)
		}
	}
	if err := store.Chats.SetSameContentCooldownSeconds(context.Background(), "cid", 3600); err != nil { // err 为夹具明确开启冷却。
		cleanup()
		t.Fatal(err)
	}
	return store, cleanup
}

// TestReplyContentCooldownOutcomes 核对成功、无回复、确定失败、部分发送和未知结果，不只检查账本实现。
func TestReplyContentCooldownOutcomes(t *testing.T) {
	// scenario 对比相同买家原文在不同真实投递事实下的后续行为。
	for _, scenario := range []struct {
		// name 是独立的投递行为。
		name string
		// reply 是可空或主动跳过的生成结果。
		reply *ReplyResult
		// sent 保存聊天应用实际确认的分段。
		sent ReplySendResult
		// sendErr 为明确失败或传输未知。
		sendErr error
		// wantCalls、wantSends 分别核对生成和外部投递次数。
		wantCalls, wantSends int
	}{
		{"success", &ReplyResult{Text: "reply"}, ReplySendResult{TextSent: true}, nil, 1, 1},
		{"no-reply", nil, ReplySendResult{}, nil, 2, 0},
		{"skip", &ReplyResult{Skip: true}, ReplySendResult{}, nil, 2, 0},
		{"definite-failure", &ReplyResult{Text: "reply"}, ReplySendResult{}, errors.New("local failure"), 2, 2},
		{"partial", &ReplyResult{Text: "reply", ImageURL: "fixture"}, ReplySendResult{ImageSent: true}, errors.New("text not sent"), 1, 1},
		{"unknown", &ReplyResult{Text: "reply"}, ReplySendResult{Uncertain: true}, errors.New("unknown delivery"), 1, 1},
		{"typed-unknown", &ReplyResult{Text: "reply"}, ReplySendResult{}, &ws.SendError{Kind: ws.SendUncertain, Err: errors.New("fixture")}, 1, 1},
	} { // scenario 是一次完整回复结果和重复消息的期望行为。
		t.Run(scenario.name, func(t *testing.T) { // t 管理当前独立本地夹具。
			// store、cleanup 为一小时冷却的隔离数据库。
			store, cleanup := newCooldownReplyStore(t)
			defer cleanup()
			// api、delivery 记录真实生成与投递；所有内容均为虚构文本。
			api := &fakeAPIReplier{result: scenario.reply}
			// delivery 返回当前分段事实，完全不连接平台。
			delivery := &recordingReplyDelivery{result: scenario.sent, err: scenario.sendErr}
			// service 使用生产相同的生成、领取和投递路径。
			service := NewReplyService("cid", store, delivery, api, nil, nil)
			for range 2 {
				// err 只在实际执行失败投递的那次返回；受保护的重复输入无外部调用。
				err := service.Handle(context.Background(), chatMsg("same buyer text", "", "chat-a"))
				if err != nil && scenario.sendErr == nil {
					t.Fatal(err)
				}
			}
			if api.called != scenario.wantCalls || len(delivery.messages) != scenario.wantSends {
				t.Fatalf("generated=%d sent=%d want=%d/%d", api.called, len(delivery.messages), scenario.wantCalls, scenario.wantSends)
			}
		})
	}
}

// TestReplyContentCooldownExactTextAndConcurrency 验证相同内容在生成期并发受保护，不同空格、大小写和会话保持独立。
func TestReplyContentCooldownExactTextAndConcurrency(t *testing.T) {
	// store、cleanup 是本地领取仓储和关闭责任。
	store, cleanup := newCooldownReplyStore(t)
	defer cleanup()
	// started、release 由首次生成通知测试并由测试释放，不创建后台循环。
	started, release := make(chan struct{}), make(chan struct{})
	// api 是可暂停的实际生成端口，只允许首次请求进入。
	api := &cooldownTestReplier{generate: func(context.Context, ChatMessage) (*ReplyResult, error) { // 首次生成在测试显式释放前保持在途。
		close(started)
		<-release
		return &ReplyResult{Text: "generated"}, nil
	}}
	// delivery 只在生成结束后由单个处理器写入。
	delivery := &recordingReplyDelivery{result: ReplySendResult{TextSent: true}}
	// service 可接受并发输入，唯一领取由数据库拥有。
	service := NewReplyService("cid", store, delivery, api, nil, nil)
	// done 由首个处理器回传最终结果，主测试必须等待。
	done := make(chan error, 1)
	go func() { // 首个入站处理器完成后退出，不遗留 goroutine。
		done <- service.Handle(context.Background(), chatMsg("Hello", "", "chat-a"))
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("generation did not start")
	}
	if err := service.Handle(context.Background(), chatMsg("Hello", "", "chat-a")); err != nil { // err 是并发重复输入被正常跳过的结果。
		close(release)
		<-done
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil { // err 是首个生成处理器最终收口结果。
		t.Fatal(err)
	}
	if api.calls.Load() != 1 || len(delivery.messages) != 1 {
		t.Fatal("duplicate content reached generator during in-flight request")
	}
	// nextAPI 使用同一回复正文，冷却仍按入站而不是出站文本区分。
	nextAPI := &fakeAPIReplier{result: &ReplyResult{Text: "generated"}}
	service.api = nextAPI
	for _, message := range []ChatMessage{chatMsg("hello", "", "chat-a"), chatMsg("Hello ", "", "chat-a"), chatMsg("Hello", "", "chat-b")} { // message 验证三个不能合并的输入。
		if err := service.Handle(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	if nextAPI.called != 3 || len(delivery.messages) != 4 {
		t.Fatal("cooldown normalized text or crossed conversation")
	}
}

// TestReplyContentCooldownGreetingAndCancellation 验证首次回复和普通链路共用一次领取，取消后仍完成本地未知隔离。
func TestReplyContentCooldownGreetingAndCancellation(t *testing.T) {
	// store、cleanup 为首次回复与内容冷却共享的本地仓储。
	store, cleanup := newCooldownReplyStore(t)
	defer cleanup()
	seedGreeting(t, store, true, "greeting")
	// delivery 返回未知首次回复，后续生成必须停止。
	delivery := &recordingReplyDelivery{result: ReplySendResult{Uncertain: true}, err: errors.New("unknown greeting")}
	// api 不应在未知首次投递之后生成第二条回复。
	api := &fakeAPIReplier{result: &ReplyResult{Text: "regular"}}
	// service 使用首次回复与普通链路的共同入口。
	service := NewReplyService("cid", store, delivery, api, nil, nil)
	if err := service.Handle(context.Background(), chatMsg("hi", "item1", "chat-a")); err != nil { // err 未知首次投递受保护跳过，正常处理器仍安全退出。
		t.Fatal(err)
	}
	if api.called != 0 || len(delivery.messages) != 1 {
		t.Fatal("unknown greeting allowed regular reply generation")
	}
	// reviews、total、err 验证未知投递没有隐形永久阻断而缺少管理入口。
	reviews, total, err := store.Chats.ListReplyContentReviews(context.Background(), "cid", 20, 0, time.Now().UnixMilli())
	if err != nil || total != 1 || len(reviews) != 1 {
		t.Fatal("greeting result not quarantined")
	}
	if err := store.Chats.SetSameContentCooldownSeconds(context.Background(), "cid", 0); err != nil { // err 为关闭新冷却的配置动作。
		t.Fatal(err)
	}
	if err := service.Handle(context.Background(), chatMsg("hi", "item1", "chat-a")); err != nil || len(delivery.messages) != 1 { // err 验证未知保护不会被零配置穿透。
		t.Fatal("disabling cooldown released unknown greeting")
	}
	if err := store.Chats.SetSameContentCooldownSeconds(context.Background(), "cid", 3600); err != nil { // err 为另一次受保护投递准备配置。
		t.Fatal(err)
	}
	// ctx、cancel 模拟发送方退出，独立持久化上下文仍须收口。
	ctx, cancel := context.WithCancel(context.Background())
	// claimCtx、allowed、err 为另一会话的新领取。
	claimCtx, allowed, err := service.claimReplyContent(ctx, chatMsg("cancel", "", "chat-b"))
	if err != nil || !allowed {
		t.Fatal(err)
	}
	if err := service.beginReplyContentDelivery(claimCtx); err != nil { // err 表示投递取消前已经进入发送占用。
		t.Fatal(err)
	}
	cancel()
	if err := service.observeReplyContentDelivery(claimCtx, ReplySendResult{Uncertain: true}, context.Canceled); err != nil { // err 必须通过独立上下文写入未知事实。
		t.Fatal(err)
	}
	if err := service.finishReplyContent(claimCtx); err != nil { // err 为取消后未知状态收口。
		t.Fatal(err)
	}
	// canceledReviews、canceledTotal、err 必须包含取消后仍保留的保护。
	canceledReviews, canceledTotal, err := store.Chats.ListReplyContentReviews(context.Background(), "cid", 20, 0, time.Now().UnixMilli())
	if err != nil || canceledTotal != 2 || len(canceledReviews) != 2 {
		t.Fatal("cancellation lost unknown result protection")
	}
	// 旧构造与空正文不需要新的占用能力。
	if _, allowed, err := service.claimReplyContent(context.Background(), chatMsg(" ", "", "chat-b")); err != nil || !allowed { // allowed、err 表示空白正文按原合同放行。
		t.Fatal(err)
	}
}

// TestReplyContentCooldownStaleGenerationAndReadFailure 验证旧生成换代后零投递，数据库读取故障也不会调用外部生成。
func TestReplyContentCooldownStaleGenerationAndReadFailure(t *testing.T) {
	// store、cleanup 为真实本地领取仓储。
	store, cleanup := newCooldownReplyStore(t)
	defer cleanup()
	// digest 是入站消息的匹配键，只用于模拟新代次，未保存正文。
	digest := sha256.Sum256([]byte("same"))
	// api 在旧请求生成中模拟租约已过期且新消息完成重领。
	api := &cooldownTestReplier{generate: func(ctx context.Context, _ ChatMessage) (*ReplyResult, error) { // ctx 仅传给本地代次更新。
		// allowed、err 是模拟五分钟后新入站消息的领取。
		_, allowed, err := store.Chats.ClaimReplyContent(ctx, "cid", "chat-a", hex.EncodeToString(digest[:]), "new-message", time.Now().UnixMilli()+300001)
		if err != nil || !allowed {
			return nil, errors.New("new generation not claimed")
		}
		return &ReplyResult{Text: "old generated reply"}, nil
	}}
	// delivery 必须始终为空，证明旧结果没有进入平台发送入口。
	delivery := &recordingReplyDelivery{result: ReplySendResult{TextSent: true}}
	// service 使用实际 Handle 证明而非仅测试 DB 比较条件。
	service := NewReplyService("cid", store, delivery, api, nil, nil)
	if err := service.Handle(context.Background(), chatMsg("same", "", "chat-a")); err != nil || len(delivery.messages) != 0 { // err 旧代次作为受保护跳过返回。
		t.Fatal("stale generated result was sent")
	}
	if _, err := store.DB.Exec(`DROP TABLE reply_content_cooldowns`); err != nil { // err 在隔离库模拟读取故障，人工暂停表仍可用。
		t.Fatal(err)
	}
	if err := service.Handle(context.Background(), chatMsg("different", "", "chat-a")); err == nil || api.calls.Load() != 1 { // err 必须阻止 API/AI 生成而非默认为关闭。
		t.Fatal("cooldown read failure allowed generation")
	}
}
