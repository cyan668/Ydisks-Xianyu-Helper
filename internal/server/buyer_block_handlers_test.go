package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestBuyerBlockHandlersContract 使用真实 Router 和 SQLite 验证四个新操作的成功契约及输入/越权保护。
func TestBuyerBlockHandlersContract(t *testing.T) {
	// srv、store、cleanup 提供隔离 HTTP 服务和数据库。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler、cookie 是真实路由与夹具登录会话。
	handler := srv.Router()
	// cookie 只用于本地 HTTP 夹具，不连接平台。
	cookie := loginHelper(t, handler)
	// created 验证用户级记录不依赖账号凭据且初始停用。
	created := greetingRequest(t, handler, cookie, http.MethodPost, "/api/v1/buyer-blocks", `{"buyer_id":"123@goofish","reason":"fixture","enabled":false,"auto_close":false}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	// row 保存服务端确认的规范化配置。
	var row buyerBlockResponse
	// err 是响应解码错误。
	if err := json.Unmarshal(created.Body.Bytes(), &row); err != nil || row.ID == 0 || row.BuyerID != "123" {
		t.Fatalf("decode: %v", err)
	}
	// path 定位刚创建的记录。
	path := "/api/v1/buyer-blocks/" + itoa(row.ID)
	// got 验证列表数组契约。
	if got := greetingRequest(t, handler, cookie, http.MethodGet, "/api/v1/buyer-blocks", ""); got.Code != 200 {
		t.Fatal(got.Code)
	}
	// got 验证覆盖成功契约。
	if got := greetingRequest(t, handler, cookie, http.MethodPut, path, `{"buyer_id":"123","enabled":true,"auto_close":true}`); got.Code != 200 {
		t.Fatalf("update=%d", got.Code)
	}
	// got 验证同作用域重复记录拒绝。
	if got := greetingRequest(t, handler, cookie, http.MethodPost, "/api/v1/buyer-blocks", `{"buyer_id":"123","enabled":true,"auto_close":false}`); got.Code != 409 {
		t.Fatalf("conflict=%d", got.Code)
	}
	// got 验证账号归属校验。
	if got := greetingRequest(t, handler, cookie, http.MethodPut, path, `{"buyer_id":"123","cookie_id":"foreign","enabled":true,"auto_close":true}`); got.Code != 404 {
		t.Fatalf("ownership=%d", got.Code)
	}
	// got 验证歧义昵称式标识拒绝。
	if got := greetingRequest(t, handler, cookie, http.MethodPost, "/api/v1/buyer-blocks", `{"buyer_id":"x y","enabled":true,"auto_close":false}`); got.Code != 400 {
		t.Fatalf("invalid=%d", got.Code)
	}
	// got 验证删除成功契约。
	if got := greetingRequest(t, handler, cookie, http.MethodDelete, path, ""); got.Code != 200 {
		t.Fatalf("delete=%d", got.Code)
	}
	// got 验证重复删除不是假成功。
	if got := greetingRequest(t, handler, cookie, http.MethodDelete, path, ""); got.Code != 404 {
		t.Fatalf("missing=%d", got.Code)
	}
}
