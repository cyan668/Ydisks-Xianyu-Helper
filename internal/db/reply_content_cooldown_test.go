package db

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// expectContentClaim 核对原子领取结果，key 为虚构内容摘要，now 为可控制的毫秒时间。
func expectContentClaim(t *testing.T, store *Store, account, chat, key string, now int64, want bool) ReplyContentClaim {
	t.Helper()
	// claim、allowed、err 是同一账号/会话/内容的真实仓储结果。
	claim, allowed, err := store.Chats.ClaimReplyContent(context.Background(), account, chat, key, "fixture-message", now)
	if err != nil || allowed != want {
		t.Fatalf("content claim allowed=%v want=%v err=%v", allowed, want, err)
	}
	return claim
}

// TestMultiDB_ReplyContentCooldown 验证三方言相同内容领取、成功计时、过期代次、未知隔离及明确恢复。
func TestMultiDB_ReplyContentCooldown(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是当前可用的隔离数据库实例。
		t.Run(target.name, func(t *testing.T) { // t 负责本方言的生命周期和断言。
			defer target.cleanup()
			// store、ctx 为当前数据库和本地事务上下文。
			store, ctx := target.store, context.Background()
			// owner、account 为不含凭据的虚构账号归属。
			owner, account := seedAccount(t, store)
			for _, chat := range []string{"chat-a", "chat-b"} { // chat 创建两个独立的买家会话。
				if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: account, ChatID: chat}); err != nil { // err 为夹具会话建立结果。
					t.Fatal(err)
				}
			}
			// now 是固定起点，测试不等待真实冷却时间。
			now := time.Now().UnixMilli()
			// seconds、err 证明旧账号保持关闭。
			seconds, err := store.Chats.SameContentCooldownSeconds(ctx, account)
			if err != nil || seconds != 0 {
				t.Fatal("legacy cooldown must be off")
			}
			if claim := expectContentClaim(t, store, account, "chat-a", "same", now, true); claim.ID != 0 { // claim 关闭时不引入新的发送前提。
				t.Fatal("disabled cooldown unexpectedly claimed")
			}
			if err := store.Chats.SetManualReplyPauseMinutes(ctx, account, 10); err != nil { // err 为独立人工分钟配置。
				t.Fatal(err)
			}
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 3600); err != nil { // err 为明确的一小时配置。
				t.Fatal(err)
			}
			// minutes、err 确认两项设置不会整行覆盖。
			minutes, err := store.Chats.ManualReplyPauseMinutes(ctx, account)
			if err != nil || minutes != 10 {
				t.Fatal("cooldown overwrote manual pause")
			}
			if err := store.Chats.SetManualReplyPauseMinutes(ctx, account, 0); err != nil { // err 为反向配置保留检查。
				t.Fatal(err)
			}
			seconds, err = store.Chats.SameContentCooldownSeconds(ctx, account)
			if err != nil || seconds != 3600 {
				t.Fatal("manual pause overwrote cooldown")
			}
			// first 是第一次收到相同原文的唯一领取。
			first := expectContentClaim(t, store, account, "chat-a", "same", now, true)
			expectContentClaim(t, store, account, "chat-a", "same", now+1, false)
			// other、changed 验证会话与原文隔离。
			other := expectContentClaim(t, store, account, "chat-b", "same", now, true)
			// changed 属于相同会话的另一原文。
			changed := expectContentClaim(t, store, account, "chat-a", "different", now, true)
			if err := store.Cookies.Save(ctx, "other-account", "fixture=offline", owner); err != nil { // err 为同用户另一虚构账号准备结果。
				t.Fatal(err)
			}
			if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: "other-account", ChatID: "chat-a"}); err != nil { // err 创建同名会话，验证账号也属于键。
				t.Fatal(err)
			}
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, "other-account", 3600); err != nil { // err 为另一账号独立配置。
				t.Fatal(err)
			}
			// otherAccount 的同文、同会话名领取不能被第一账号占用。
			otherAccount := expectContentClaim(t, store, "other-account", "chat-a", "same", now, true)
			if err := store.Chats.FinishReplyContent(ctx, otherAccount); err != nil { // err 释放独立账号的空回复领取。
				t.Fatal(err)
			}
			for _, claim := range []ReplyContentClaim{other, changed} { // claim 无回复时释放，不消耗冷却。
				if err := store.Chats.FinishReplyContent(ctx, claim); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Chats.CheckReplyContentClaim(ctx, first, now+10); err != nil { // err 为生成期间有效的证明。
				t.Fatal(err)
			}
			if err := store.Chats.BeginReplyContentDelivery(ctx, first, now+100); err != nil { // err 为外部发送前占用。
				t.Fatal(err)
			}
			if err := store.Chats.CheckReplyContentClaim(ctx, first, now+110); !errors.Is(err, ErrReplyContentClaimLost) { // err 在途发送不能被另一个生成阶段穿透。
				t.Fatal("active delivery remained reusable")
			}
			if err := store.Chats.ObserveReplyContentDelivery(ctx, first, true, false, now+200000); err != nil { // err 为实际成功时间保存。
				t.Fatal(err)
			}
			if err := store.Chats.FinishReplyContent(ctx, first); err != nil { // err 是成功回复整次处理的状态收口。
				t.Fatal(err)
			}
			expectContentClaim(t, store, account, "chat-a", "same", now+3600000, false)
			// next 在成功时间加一小时后才允许，版本必须递增。
			next := expectContentClaim(t, store, account, "chat-a", "same", now+3800000, true)
			if next.ID != first.ID || next.Version <= first.Version {
				t.Fatal("reclaim did not advance generation")
			}
			if err := store.Chats.BeginReplyContentDelivery(ctx, first, now+3800001); !errors.Is(err, ErrReplyContentClaimLost) { // err 必须拒绝过时代次的新发送。
				t.Fatal("stale generation started sending")
			}
			if err := store.Chats.ObserveReplyContentDelivery(ctx, first, true, false, now+3800001); !errors.Is(err, ErrReplyContentClaimLost) { // err 必须防止旧结果覆盖新领取。
				t.Fatal("stale generation overwrote result")
			}
			if err := store.Chats.FinishReplyContent(ctx, first); err != nil { // err 旧收口幂等无副作用，不能删除 next。
				t.Fatal(err)
			}
			expectContentClaim(t, store, account, "chat-a", "same", now+3800002, false)
			if err := store.Chats.FinishReplyContent(ctx, next); err != nil { // err 收口没有实际发送的新领取。
				t.Fatal(err)
			}
			// recreated 的新自增主键防止删除重建后的 ABA。
			recreated := expectContentClaim(t, store, account, "chat-a", "same", now+3800003, true)
			if recreated.ID == next.ID {
				t.Fatal("delete/recreate reused old proof")
			}
			if err := store.Chats.FinishReplyContent(ctx, recreated); err != nil { // err 释放新主键对应的无回复处理。
				t.Fatal(err)
			}
			// expired 只在生成期租约过期，允许安全换代。
			expired := expectContentClaim(t, store, account, "chat-a", "lease", now, true)
			// renewed 是五分钟后接纳的新买家消息。
			renewed := expectContentClaim(t, store, account, "chat-a", "lease", now+300000, true)
			if renewed.Version <= expired.Version {
				t.Fatal("expired generation was not invalidated")
			}
			if err := store.Chats.CheckReplyContentClaim(ctx, expired, now+300000); !errors.Is(err, ErrReplyContentClaimLost) { // err 必须阻止旧生成结果继续。
				t.Fatal("expired generation remained valid")
			}
			if err := store.Chats.BeginReplyContentDelivery(ctx, renewed, now+300001); err != nil { // err 模拟新代次进入平台发送前的占用。
				t.Fatal(err)
			}
			expectContentClaim(t, store, account, "chat-a", "lease", now+600001, false)
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 0); err != nil { // err 关闭配置也不能解除已开始发送但未收口的占用。
				t.Fatal(err)
			}
			expectContentClaim(t, store, account, "chat-a", "lease", now+90000000, false)
			// reviews、total、err 显示需核对保护，零配置不会把它隐藏。
			reviews, total, err := store.Chats.ListReplyContentReviews(ctx, account, 20, 0, now+90000000)
			if err != nil || total != 1 || len(reviews) != 1 || reviews[0].ID != renewed.ID || reviews[0].MessageKey != "fixture-message" {
				t.Fatal("unknown delivery is not reviewable")
			}
			if err := store.Chats.ResolveReplyContentReview(ctx, "foreign", renewed, now+90000000); !errors.Is(err, ErrReplyContentClaimLost) { // err 核对其他账号不能解除此保护。
				t.Fatal("foreign account released protection")
			}
			if err := store.Chats.ResolveReplyContentReview(ctx, account, expired, now+90000000); !errors.Is(err, ErrReplyContentClaimLost) { // err 核对过时版本不能解除新保护。
				t.Fatal("stale review released protection")
			}
			if err := store.Chats.ResolveReplyContentReview(ctx, account, renewed, now+90000000); err != nil { // err 为明确核对后允许未来新消息的恢复结果。
				t.Fatal(err)
			}
			expectContentClaim(t, store, account, "chat-a", "lease", now+90000001, true)
		})
	}
}

// TestReplyContentCooldownSurvivesReopen 关闭并重开实际 SQLite 连接，未知结果仍保护，账号配置不依赖进程内存。
func TestReplyContentCooldownSurvivesReopen(t *testing.T) {
	// ctx、path 是本地生命周期和隔离数据库文件。
	ctx := context.Background()
	// path 不含任何生产数据。
	path := filepath.Join(t.TempDir(), "cooldown-restart.db")
	// handle、dialect、err 是首次启动使用的真实连接。
	handle, dialect, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// store 为首次启动的仓储聚合。
	store := NewStore(handle, dialect)
	// account 为虚构身份。
	_, account := seedAccount(t, store)
	if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: account, ChatID: "chat"}); err != nil { // err 为重启前的会话准备。
		t.Fatal(err)
	}
	if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 3600); err != nil { // err 明确保存一小时设置。
		t.Fatal(err)
	}
	// now 是重启前实际接纳时间。
	now := time.Now().UnixMilli()
	// claim 是已经进入发送但进程退出而未确认的领取。
	claim := expectContentClaim(t, store, account, "chat", "same", now, true)
	if err := store.Chats.BeginReplyContentDelivery(ctx, claim, now+1); err != nil { // err 为崩溃前最后持久化检查点。
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil { // err 模拟进程数据库退出，不执行结果收口。
		t.Fatal(err)
	}
	handle, dialect, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	store = NewStore(handle, dialect)
	expectContentClaim(t, store, account, "chat", "same", now+90000000, false)
	// items、total、err 验证重启后的管理页面仍能定位未知保护。
	items, total, err := store.Chats.ListReplyContentReviews(ctx, account, 20, 0, now+90000000)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != claim.ID {
		t.Fatal("restart lost delivery protection")
	}
}

// TestMultiDB_ReplyContentOutcomes 验证确定失败、部分成功、未知结果和实时设置变化，复用三方言。
func TestMultiDB_ReplyContentOutcomes(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是独立数据库目标。
		t.Run(target.name, func(t *testing.T) { // t 为当前方言断言作用域。
			defer target.cleanup()
			// store、ctx 为当前目标仓储及上下文。
			store, ctx := target.store, context.Background()
			// account 为虚构归属账号。
			_, account := seedAccount(t, store)
			if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: account, ChatID: "chat"}); err != nil { // err 为各结果场景共用的虚构会话准备。
				t.Fatal(err)
			}
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 3600); err != nil { // err 为一小时配置保存结果。
				t.Fatal(err)
			}
			// now 是控制租约和成功计时的毫秒起点。
			now := time.Now().UnixMilli()
			// scenario 区分未发送、部分成功和不同未知结果，不以错误文本猜测事实。
			for _, scenario := range []struct {
				// name 为独立内容键。
				name string
				// sent、uncertain 为真实投递事实。
				sent, uncertain bool
				// allowed 表示收口后下一条相同原文是否可立即回复。
				allowed bool
			}{{"failure", false, false, true}, {"partial", true, false, false}, {"unknown", false, true, false}, {"partial-unknown", true, true, false}} { // scenario 描述一类结果。
				// claim 是该结果独立的领取。
				claim := expectContentClaim(t, store, account, "chat", scenario.name, now, true)
				if err := store.Chats.BeginReplyContentDelivery(ctx, claim, now+1); err != nil { // err 为当前场景发送前占用结果。
					t.Fatal(err)
				}
				if err := store.Chats.ObserveReplyContentDelivery(ctx, claim, scenario.sent, scenario.uncertain, now+2); err != nil { // err 保存真实分段事实，不把失败覆盖既有成功。
					t.Fatal(err)
				}
				if err := store.Chats.FinishReplyContent(ctx, claim); err != nil { // err 为当前投递结果的整次处理收口。
					t.Fatal(err)
				}
				// retried 为确定失败才获得的新领取。
				retried := expectContentClaim(t, store, account, "chat", scenario.name, now+3, scenario.allowed)
				if retried.ID != 0 {
					if err := store.Chats.FinishReplyContent(ctx, retried); err != nil { // err 清理确定失败之后的空回复重试。
						t.Fatal(err)
					}
				}
			}
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 1); err != nil { // err 为实时缩短冷却的保存结果。
				t.Fatal(err)
			}
			// shorter 证明新秒数对既有成功记录实时生效。
			shorter := expectContentClaim(t, store, account, "chat", "partial", now+1002, true)
			if err := store.Chats.FinishReplyContent(ctx, shorter); err != nil { // err 释放缩短窗口后产生的空回复领取。
				t.Fatal(err)
			}
			// pending 是关闭配置前尚在生成的任务。
			pending := expectContentClaim(t, store, account, "chat", "disable", now, true)
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 0); err != nil { // err 为关闭新冷却但保留在途事实的配置。
				t.Fatal(err)
			}
			expectContentClaim(t, store, account, "chat", "disable", now+1, false)
			expectContentClaim(t, store, account, "chat", "disable", now+300000, true)
			if err := store.Chats.BeginReplyContentDelivery(ctx, pending, now+300001); !errors.Is(err, ErrReplyContentClaimLost) { // err 验证关闭配置时废弃的旧代次不能发送。
				t.Fatal("disabled expired generation started sending")
			}
			expectContentClaim(t, store, account, "chat", "unknown", now+90000000, false)
			// page、total、err 核对分页计数与页长，未知保护不被一天清理删除。
			page, total, err := store.Chats.ListReplyContentReviews(ctx, account, 1, 1, now+90000000)
			if err != nil || total != 2 || len(page) != 1 {
				t.Fatal("review pagination lost uncertain results")
			}
		})
	}
}

// TestMultiDB_ReplyContentConcurrent 验证同文并发只允许一个处理器生成回复，数据库持有唯一领取权。
func TestMultiDB_ReplyContentConcurrent(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是当前可用方言。
		t.Run(target.name, func(t *testing.T) { // t 管理此目标的并发夹具。
			defer target.cleanup()
			// store、ctx 为并发调用共享的线程安全仓储与上下文。
			store, ctx := target.store, context.Background()
			// account 为虚构账号。
			_, account := seedAccount(t, store)
			if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: account, ChatID: "chat"}); err != nil { // err 为并发领取创建共同会话。
				t.Fatal(err)
			}
			if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 3600); err != nil { // err 为并发处理器共用的一小时配置。
				t.Fatal(err)
			}
			// results、failures 由各处理器写入，主测试在 Wait 后读取；没有外部 I/O。
			results := make(chan bool, 12)
			// failures 收集真实并发数据库错误，不能把锁失败算成正常去重。
			failures := make(chan error, 12)
			// group 由本测试启动并等待全部处理器。
			var group sync.WaitGroup
			for range 12 {
				group.Add(1)
				go func() { // 每个并发处理器仅尝试本地原子领取。
					defer group.Done()
					// allowed、err 为该处理器的独立结果。
					_, allowed, err := store.Chats.ClaimReplyContent(ctx, account, "chat", "same", "message", time.Now().UnixMilli())
					results <- allowed
					failures <- err
				}()
			}
			group.Wait()
			close(results)
			close(failures)
			// winners 只统计实际拿到生成权的处理器。
			winners := 0
			for allowed := range results { // allowed 是单个处理器真实领取结果。
				if allowed {
					winners++
				}
			}
			for err := range failures { // err 不得由并发压力导致业务路径漏报。
				if err != nil {
					t.Fatal(err)
				}
			}
			if winners != 1 {
				t.Fatalf("concurrent winners=%d want=1", winners)
			}
		})
	}
}
