package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"xianyu-go/internal/adapter"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
)

// TestChatReplyTakeoverContract 验证真实 Router 的设置、状态、恢复、归属和并发冲突，完全不启用账号或访问平台。
func TestChatReplyTakeoverContract(t *testing.T) {
	// srv、store、cleanup 为隔离的 HTTP 与数据库夹具。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.applicationServiceSet().chat = chatapp.New(adapter.NewChatRepository(store))
	// ctx 是本地设置/会话的生命周期。
	ctx := context.Background()
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "acc1", ChatID: "takeover-chat", BuyerID: "fixture", LastMessageAt: 1}); err != nil { // err 是虚构会话准备结果。
		t.Fatal(err)
	}
	// handler、cookie 是实际路由和认证会话。
	handler := srv.Router()
	// cookie 为本地认证，不含真实平台凭据。
	cookie := loginHelper(t, handler)
	if response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/reply-protection?account_id=acc1", ""); response.Code != 200 { // response 核对旧账号默认读取成功契约。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "PUT", "/api/v1/chat/reply-protection", `{"account_id":"acc1","manual_pause_minutes":10}`); response.Code != 200 { // response 是独立配置保存契约。
		t.Fatal(response.Code)
	}
	// now 是人工事件的实际毫秒时间，不等待真实十分钟。
	now := time.Now().UnixMilli()
	if err := store.Chats.RecordManualReply(ctx, "acc1", "takeover-chat", "manual-key", now, now); err != nil { // err 为本地接管事件结果。
		t.Fatal(err)
	}
	// response 是实际会话状态，必须包含毫秒级精确截止。
	response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/reply-pause?account_id=acc1&chat_id=takeover-chat", "")
	// state 是从真实 transport DTO 解码的暂停状态。
	var state replyPauseDTO
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil || response.Code != 200 || !state.Paused || state.PausedUntil != now+600000 { // err 为本地契约解码故障。
		t.Fatal("invalid pause deadline response")
	}
	if response := greetingRequest(t, handler, cookie, "POST", "/api/v1/chat/reply-pause/resume", `{"account_id":"acc1","chat_id":"takeover-chat","expected_version":0}`); response.Code != 409 { // response 为新接管不能被旧页面清除的冲突契约。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "POST", "/api/v1/chat/reply-pause/resume", `{"account_id":"acc1","chat_id":"takeover-chat","expected_version":`+itoa(state.Version)+`}`); response.Code != 200 { // response 为精确恢复成功契约。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "PUT", "/api/v1/chat/reply-protection", `{"account_id":"acc1","manual_pause_minutes":1441}`); response.Code != 400 { // response 拒绝超限分钟数。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "PUT", "/api/v1/chat/reply-protection", `{"account_id":"acc1"}`); response.Code != 400 { // response 不能把缺省分钟数误当成明确关闭。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "POST", "/api/v1/chat/reply-pause/resume", `{"account_id":"acc1","chat_id":"takeover-chat"}`); response.Code != 400 { // response 必须携带所见版本。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/reply-protection?account_id=foreign", ""); response.Code != 403 { // response 证明归属核验先于配置读取。
		t.Fatal(response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/reply-pause?account_id=acc1&chat_id=missing", ""); response.Code != 404 { // response 不允许恢复不存在会话。
		t.Fatal(response.Code)
	}
}
