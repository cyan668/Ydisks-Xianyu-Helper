package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/xianyu/protocol"
)

// TestSellerCloseContractAndNoRetry 用本地 HTTP 证明签名、正文、Cookie 合并和所有失败场景最多一个请求。
func TestSellerCloseContractAndNoRetry(t *testing.T) {
	for _, body := range []string{`{"ret":["SUCCESS::调用成功"]}`, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::fixture"]}`, `{"ret":["FAIL_BIZ_STATUS::fixture"]}`, `{"ret":[]}`, `broken`} { // body 是不含真实订单或凭据的响应替身。
		t.Run(body, func(t *testing.T) { // t 管理本场景的本地服务器。
			// calls 统计真实 HTTP 请求次数，不允许内部重试。
			calls := 0
			// server 只监听回环地址，不访问平台。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // w/r 分别接收替身响应和本地请求。
				calls++
				// err 核验表单编码。
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				// data 为签名与表单使用的同一份 JSON。
				data := r.PostForm.Get("data")
				// payload 核验订单字段及固定关单原因，不包含管理备注。
				var payload map[string]string
				// err 为正文解码错误。
				if err := json.Unmarshal([]byte(data), &payload); err != nil {
					t.Error(err)
				}
				if payload["tid"] != "fixture-order" || payload["bizOrderId"] != "fixture-order" || payload["closeReason"] != "其他原因" {
					t.Error("invalid close payload")
				}
				if r.URL.Query().Get("v") != "2.0" || r.URL.Query().Get("api") != "mtop.taobao.idle.trade.merchant.close.by.seller" || r.URL.Query().Get("sign") != protocol.GenerateSign(r.URL.Query().Get("t"), "token", data) {
					t.Error("invalid close signature or version")
				}
				http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "updated", Path: "/"})
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			// client 通过本地端点注入，仍使用生产的节流传输包装。
			client := &ClientImpl{HTTPClient: server.Client(), CloseOrderURL: server.URL}
			// success、ret、updated、err 为单次关单的公开协议结果。
			success, _, updated, err := client.CloseOrderBySellerContext(context.Background(), "unb=123; _m_h5_tk=token_1;", "fixture-order")
			if calls != 1 || success != (body == `{"ret":["SUCCESS::调用成功"]}`) || (err == nil) != success || updated == "" {
				t.Fatalf("close calls=%d success=%v err=%v", calls, success, err)
			}
		})
	}
}
