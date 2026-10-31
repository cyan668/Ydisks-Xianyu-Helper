package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestReplyContentCooldownPersistenceFailures 用已有故障驱动逐点验证关键事务，故障必须传播且未知占用不能丢失。
func TestReplyContentCooldownPersistenceFailures(t *testing.T) {
	// template 是已迁移的空 SQLite 模板，只复制本地文件，不涉及真实数据。
	template := faultTemplate(t)
	for _, scenario := range []string{"settings", "new-claim", "reclaim", "expired-action", "disabled-expired", "begin", "check", "observe", "finish", "list", "resolve"} { // scenario 为持久化生命周期中可独立注入故障的入口。
		t.Run(scenario, func(t *testing.T) { // t 管理当前本地故障场景。
			for point := int64(1); point < 14; point++ { // point 逐一覆盖短事务内操作，超过实际操作数后立即结束。
				// store、state、cleanup 是每个注入点独立的数据库和故障状态。
				store, state, cleanup := openFaultStore(t, template)
				// ctx 为本地操作上下文。
				ctx := context.Background()
				// account 为虚构账号归属。
				_, account := seedAccount(t, store)
				if err := store.Chats.UpsertSession(ctx, ChatSession{CookieID: account, ChatID: "chat"}); err != nil { // err 为未布防阶段的会话准备结果。
					cleanup()
					t.Fatal(err)
				}
				if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 3600); err != nil { // err 为未布防的一小时配置准备结果。
					cleanup()
					t.Fatal(err)
				}
				// now 为固定租约与状态比较的毫秒时间。
				now := time.Now().UnixMilli()
				// claim 为后续入口共用的领取，new-claim 场景使用另一内容键。
				claim := expectContentClaim(t, store, account, "chat", "existing", now, true)
				if scenario == "reclaim" {
					if err := store.Chats.FinishReplyContent(ctx, claim); err != nil { // err 释放无回复领取后建立已过期的生成代次。
						cleanup()
						t.Fatal(err)
					}
					claim = expectContentClaim(t, store, account, "chat", "existing", now, true)
				}
				if scenario == "expired-action" || scenario == "observe" || scenario == "finish" || scenario == "list" || scenario == "resolve" {
					if err := store.Chats.BeginReplyContentDelivery(ctx, claim, now+1); err != nil { // err 为可恢复的发送前检查点。
						cleanup()
						t.Fatal(err)
					}
				}
				if scenario == "list" || scenario == "resolve" {
					if err := store.Chats.ObserveReplyContentDelivery(ctx, claim, false, true, now+2); err != nil { // err 准备真实未知结果。
						cleanup()
						t.Fatal(err)
					}
					if err := store.Chats.FinishReplyContent(ctx, claim); err != nil { // err 在布防前收口未知保护。
						cleanup()
						t.Fatal(err)
					}
				}
				if scenario == "disabled-expired" {
					if err := store.Chats.SetSameContentCooldownSeconds(ctx, account, 0); err != nil { // err 准备关闭配置但仍在生成的状态。
						cleanup()
						t.Fatal(err)
					}
				}
				// err 必须收到任何触发的底层故障，不能静默放行或报恢复成功。
				var err error
				state.remaining.Store(point)
				switch scenario {
				case "settings":
					err = store.Chats.SetSameContentCooldownSeconds(ctx, account, 10)
				case "new-claim":
					_, _, err = store.Chats.ClaimReplyContent(ctx, account, "chat", "new", "message", now)
				case "reclaim", "expired-action", "disabled-expired":
					_, _, err = store.Chats.ClaimReplyContent(ctx, account, "chat", "existing", "message", now+300000)
				case "begin":
					err = store.Chats.BeginReplyContentDelivery(ctx, claim, now+1)
				case "check":
					err = store.Chats.CheckReplyContentClaim(ctx, claim, now+1)
				case "observe":
					err = store.Chats.ObserveReplyContentDelivery(ctx, claim, true, false, now+2)
				case "finish":
					err = store.Chats.FinishReplyContent(ctx, claim)
				case "list":
					_, _, err = store.Chats.ListReplyContentReviews(ctx, account, 20, 0, now+3)
				case "resolve":
					err = store.Chats.ResolveReplyContentReview(ctx, account, claim, now+3)
				}
				state.remaining.Store(0)
				// fired 说明此次操作确实走到了故障注入点。
				fired := state.fired.Load()
				if fired && !errors.Is(err, errInjectedFault) {
					cleanup()
					t.Fatalf("failure lost at point %d: %v", point, err)
				}
				if fired && (scenario == "observe" || scenario == "finish" || scenario == "resolve" || scenario == "expired-action") {
					// reviews、total、reviewErr 核对保存故障仍保留发送前占用或稳定未知保护。
					reviews, total, reviewErr := store.Chats.ListReplyContentReviews(ctx, account, 20, 0, now+300001)
					if reviewErr != nil || total != 1 || len(reviews) != 1 {
						cleanup()
						t.Fatal("persistence failure erased uncertain protection")
					}
				}
				cleanup()
				if !fired {
					break
				}
			}
		})
	}
}
