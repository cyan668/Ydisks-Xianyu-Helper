package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOfficialBlacklistSingleRequest 验证三种官方会话能力的真实协议正文、版本、布尔状态和禁止自动重试。
func TestOfficialBlacklistSingleRequest(t *testing.T) {
	for _, action := range []string{"query", "add", "remove"} { // action 是允许的单次会话平台动作。
		for _, body := range []string{`{"ret":["SUCCESS::ok"],"data":{"isInBlack":false}}`, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::fixture"]}`, `{"ret":["SUCCESS::ok"],"data":{}}`, `broken`} { // body 覆盖明确状态和未知结果。
			// calls 证明每次操作最多一个出站请求。
			calls := 0
			// server 是回环替身，不访问平台。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // w/r 提供本地协议观察和固定响应。
				calls++
				_ = r.ParseForm()
				if r.PostForm.Get("data") != `{"sessionId":"fixture-chat"}` || !strings.HasSuffix(r.URL.Path, "/1.0/") && action != "add" || action == "add" && !strings.HasSuffix(r.URL.Path, "/2.0/") {
					t.Error("wrong session payload or API version")
				}
				fmt.Fprint(w, body)
			}))
			// client 使用生产协议和节流包装，仅端点为本地注入。
			client := &ClientImpl{HTTPClient: server.Client(), OfficialBlacklistBaseURL: server.URL}
			// blocked、err 为本次明确结果。
			blocked, _, err := client.OfficialBlacklistContext(context.Background(), consignCookies, "fixture-chat", action)
			server.Close()
			// wantErr 查询时缺少布尔值也不能猜作 false；写入必须有 SUCCESS。
			wantErr := body == `broken` || strings.Contains(body, "FAIL_SYS") || action == "query" && !strings.Contains(body, "isInBlack")
			if calls != 1 || (err != nil) != wantErr || err == nil && blocked != (action == "add") {
				t.Fatalf("%s: calls=%d blocked=%v err=%v", action, calls, blocked, err)
			}
		}
	}
}
