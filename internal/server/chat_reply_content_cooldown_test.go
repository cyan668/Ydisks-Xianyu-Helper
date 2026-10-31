package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/adapter"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
)

// TestChatContentCooldownContract 经真实 Router、应用及 SQLite 验证所有新 operation 成功契约及失败边界。
func TestChatContentCooldownContract(t *testing.T) {
	// srv、store、cleanup 为隔离的本地 HTTP 夹具。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.applicationServiceSet().chat = chatapp.New(adapter.NewChatRepository(store))
	// ctx 只用于本地会话和占用准备。
	ctx := context.Background()
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "acc1", ChatID: "cooldown-chat"}); err != nil { // err 为本地虚构会话准备结果。
		t.Fatal(err)
	}
	// handler、cookie 为真实路由及本地认证。
	handler := srv.Router()
	// cookie 是测试管理员会话，不使用真实平台凭据。
	cookie := loginHelper(t, handler)
	if response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/content-cooldown?account_id=acc1", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"seconds":0`) || response.Header().Get("Cache-Control") != "no-store" { // response 证明旧账号默认关闭且不缓存。
		t.Fatal("invalid default settings response")
	}
	if response := greetingRequest(t, handler, cookie, "PUT", "/api/v1/chat/content-cooldown", `{"account_id":"acc1","seconds":3600}`); response.Code != 200 { // response 为独立秒数保存成功契约。
		t.Fatal(response.Code)
	}
	// now 为本地发送事实毫秒起点。
	now := time.Now().UnixMilli()
	// claim、allowed、err 为未知结果的真实持久化领取。
	claim, allowed, err := store.Chats.ClaimReplyContent(ctx, "acc1", "cooldown-chat", "private-content-digest", "buyer-message", now)
	if err != nil || !allowed {
		t.Fatal("failed to prepare content claim")
	}
	if err := store.Chats.BeginReplyContentDelivery(ctx, claim, now+1); err != nil { // err 为真实数据库发送占用结果。
		t.Fatal(err)
	}
	if err := store.Chats.ObserveReplyContentDelivery(ctx, claim, false, true, now+2); err != nil { // err 为虚构未知发送事实保存结果。
		t.Fatal(err)
	}
	if err := store.Chats.FinishReplyContent(ctx, claim); err != nil { // err 将此内容稳定隔离为待核对。
		t.Fatal(err)
	}
	// response 为真实分页成功契约，不能含内容键或正文。
	response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/content-cooldown/reviews?account_id=acc1&limit=20&offset=0", "")
	// page 为 transport DTO，只核对公开的定位信息。
	var page contentCooldownPageDTO
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || response.Code != 200 || page.Total != 1 || len(page.Items) != 1 || page.Items[0].MessageKey != "buyer-message" || strings.Contains(response.Body.String(), "private-content-digest") || strings.Contains(response.Body.String(), "content_key") { // err 为真实响应模型解码失败。
		t.Fatal("invalid or sensitive review response")
	}
	// path 为唯一已知保护的恢复入口。
	path := "/api/v1/chat/content-cooldown/reviews/" + itoa(claim.ID) + "/resolve"
	if response := greetingRequest(t, handler, cookie, "POST", path, `{"account_id":"acc1","expected_version":999,"confirmed":true}`); response.Code != 409 { // response 必须拒绝过时保护版本。
		t.Fatal("stale review was accepted")
	}
	if response := greetingRequest(t, handler, cookie, "POST", path, `{"account_id":"acc1","expected_version":1,"confirmed":false}`); response.Code != 400 { // response 必须要求明确平台核对确认。
		t.Fatal("unconfirmed resolve was accepted")
	}
	if response := greetingRequest(t, handler, cookie, "POST", path, `{"account_id":"acc1","expected_version":`+itoa(claim.Version)+`,"confirmed":true}`); response.Code != 200 { // response 是精确版本确认后的成功恢复契约。
		t.Fatal("explicit resolve failed", response.Code)
	}
	if response := greetingRequest(t, handler, cookie, "POST", path, `{"account_id":"acc1","expected_version":1,"confirmed":true}`); response.Code != 409 { // response 不能把重复恢复当作新状态解除。
		t.Fatal("repeated resolve should conflict")
	}
	// test 穷举缺省、超限、越权及非法分页的统一错误行为。
	for _, test := range []struct {
		// method、path、body 定位非法 HTTP 输入。
		method, path, body string
		// status 为统一错误状态。
		status int
	}{
		{"GET", "/api/v1/chat/content-cooldown?account_id=foreign", "", 403},
		{"GET", "/api/v1/chat/content-cooldown", "", 400},
		{"PUT", "/api/v1/chat/content-cooldown", `{"account_id":"acc1"}`, 400},
		{"PUT", "/api/v1/chat/content-cooldown", `{"account_id":"acc1","seconds":86401}`, 400},
		{"PUT", "/api/v1/chat/content-cooldown", `{"account_id":"acc1","seconds":-1}`, 400},
		{"PUT", "/api/v1/chat/content-cooldown", `{`, 400},
		{"GET", "/api/v1/chat/content-cooldown/reviews?account_id=acc1&limit=101", "", 400},
		{"GET", "/api/v1/chat/content-cooldown/reviews?account_id=acc1&limit=oops", "", 400},
		{"GET", "/api/v1/chat/content-cooldown/reviews?account_id=acc1&offset=-1", "", 400},
		{"GET", "/api/v1/chat/content-cooldown/reviews?account_id=foreign", "", 403},
		{"POST", path, `{"account_id":"foreign","expected_version":1,"confirmed":true}`, 403},
		{"POST", path, `{"account_id":"acc1","confirmed":true}`, 400},
		{"POST", "/api/v1/chat/content-cooldown/reviews/bad/resolve", `{}`, 400},
	} { // test 是需要拒绝而没有本地写入的输入。
		if response := greetingRequest(t, handler, cookie, test.method, test.path, test.body); response.Code != test.status {
			t.Fatalf("%s %s got=%d want=%d", test.method, test.path, response.Code, test.status)
		}
	}
	srv.applicationServiceSet().chat = contractChatPort{}
	for _, method := range []string{"GET", "PUT"} { // method 核对旧端口在新入口提供明确不可用结果。
		if response := greetingRequest(t, handler, cookie, method, "/api/v1/chat/content-cooldown?account_id=acc1", `{}`); response.Code != 503 { // response 不得把未提供能力当成默认零或成功保存。
			t.Fatal("old chat port did not return unavailable", response.Code)
		}
	}
	srv.applicationServiceSet().chat = chatapp.New(adapter.NewChatRepository(store))
	if err := store.DB.Close(); err != nil { // err 关闭隔离库，以实际存储故障验证错误脱敏。
		t.Fatal(err)
	}
	if response := greetingRequest(t, handler, cookie, "GET", "/api/v1/chat/content-cooldown?account_id=acc1", ""); response.Code != 500 || strings.Contains(response.Body.String(), "database") { // response 只能返回稳定应用错误，不能泄露底层 SQL。
		t.Fatal("database failure became successful or leaked details", response.Code)
	}
}
