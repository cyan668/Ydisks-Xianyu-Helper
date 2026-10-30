package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/adapter"
	blockapp "xianyu-go/internal/application/buyerblock"
)

// officialBlockFake 隔离契约测试，只记录当前会话操作，不调用平台。
type officialBlockFake struct { /* calls 是单次操作计数。 */
	calls int
}

// Official 返回本地确定性状态，ctx/userID/accountID/chatID/action 只作为本次明确会话调用的输入。
func (f *officialBlockFake) Official(_ context.Context, _ int64, _, _, action string) (bool, error) {
	f.calls++
	return action != "remove", nil
}

// TestBuyerBlockManagementContract 覆盖原系统表格迁移、重复去重、分页、批量、订单入口、二进制导出及会话平台接口契约。
func TestBuyerBlockManagementContract(t *testing.T) {
	// srv、store、cleanup 为隔离服务和数据库，不启用任何真实账号。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// platform 仅作为官方会话请求的内存替身。
	platform := &officialBlockFake{}
	srv.applicationServiceSet().buyerBlocks = blockapp.NewServiceWithPlatform(adapter.NewBuyerBlockRepository(store), platform)
	// handler、cookie 使用真实 Router 和认证会话。
	handler := srv.Router()
	// cookie 仅用于隔离测试。
	cookie := loginHelper(t, handler)
	// upload 构造真实 multipart 请求，每次断言都校验成功响应的 OpenAPI。
	upload := func(content string) *httptest.ResponseRecorder { // content 为只有虚构买家的 CSV。
		// body、writer 是本次上传独占的字节缓冲和 multipart 编码器。
		var body bytes.Buffer
		// writer 保留真实边界，不能用 JSON 包装文件。
		writer := multipart.NewWriter(&body)
		// part、err 构造原系统表头的文件部件。
		part, err := writer.CreateFormFile("file", "fixture.csv")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(content))
		_ = writer.Close()
		// request 为拥有者上传文件请求。
		request := httptest.NewRequest(http.MethodPost, "/api/v1/buyer-blocks/import", &body)
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		// response 记录真实响应。
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code == 200 {
			assertOpenAPIRecordedSuccessResponse(t, request, response)
		}
		return response
	}
	// csv 含两条不同启停状态，昵称与长整数作为文本，额外关单列可完整往返。
	csv := "账号ID,买家ID,买家昵称,商品ID,拉黑原因,是否启用,命中后关单\n,12345678901234567,测试买家,,fixture,是,否\n,222,第二位,,fixture,否,是\n"
	for attempt := 0; attempt < 2; attempt++ { // attempt 验证第二次导入不覆盖原配置或重复创建。
		// response 是同一文件的当前上传结果。
		response := upload(csv)
		if response.Code != 200 {
			t.Fatalf("import=%d: %s", response.Code, response.Body.String())
		}
		// result 是当前原子导入计数。
		var result buyerBlockBatchResponse
		// err 表示结果无法解码。
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if (attempt == 0 && result.Created != 2) || (attempt == 1 && result.Skipped != 2) {
			t.Fatal("import did not preserve duplicate semantics")
		}
	}
	// page 检验昵称搜索与精确总数。
	page := greetingRequest(t, handler, cookie, "GET", "/api/v1/buyer-blocks/page?search=fixture&page_size=1", "")
	// result 是实际分页 DTO。
	var result buyerBlockPageResponse
	// err 为分页响应解码失败，不能把错误响应当成空页。
	if err := json.Unmarshal(page.Body.Bytes(), &result); err != nil || result.Total != 2 || len(result.Data) != 1 {
		t.Fatalf("page=%d %v", page.Code, err)
	} // err 是分页解码失败。
	// id 定位第一页明确选中的记录。
	id := result.Data[0].ID
	// response 断言当前明确记录集合的批量状态响应契约。
	if response := greetingRequest(t, handler, cookie, "POST", "/api/v1/buyer-blocks/batch", `{"ids":[`+itoa(id)+`],"action":"disable"}`); response.Code != 200 {
		t.Fatal(response.Code)
	} // response 验证原子批量状态。
	// 本地订单买家由服务端派生，浏览器不能提供另一个买家 ID。
	if _, err := store.DB.Exec(`INSERT INTO orders(order_id,cookie_id,item_id,buyer_id,order_status) VALUES('block-from-order','acc1','item-fixture','333','pending_ship')`); err != nil {
		t.Fatal(err)
	} // err 是虚构订单准备错误。
	if response := greetingRequest(t, handler, cookie, "POST", "/api/v1/orders/block-from-order/buyer-block", `{"level":"user"}`); response.Code != 200 {
		t.Fatal(response.Code)
	} // response 验证订单派生入口。
	if response := greetingRequest(t, handler, cookie, "POST", "/api/v1/chat/acc1/chat-fixture/official-blacklist", `{"action":"query"}`); response.Code != 200 || platform.calls != 1 {
		t.Fatal("official contract or one-request bound failed")
	} // response 仅使用平台替身。
	// exportRequest、exportResponse 校验二进制成功契约及个人数据 no-store。
	exportRequest := httptest.NewRequest("GET", "/api/v1/buyer-blocks/export", nil)
	exportRequest.AddCookie(cookie)
	// exportResponse 为完整导出响应。
	exportResponse := httptest.NewRecorder()
	handler.ServeHTTP(exportResponse, exportRequest)
	assertOpenAPIRecordedSuccessResponse(t, exportRequest, exportResponse)
	if exportResponse.Header().Get("Cache-Control") != "no-store" || !strings.Contains(exportResponse.Body.String(), "12345678901234567") || !strings.Contains(exportResponse.Body.String(), "测试买家") {
		t.Fatal("export lost text identity or nickname")
	}
	// response 断言缺少必要表头时整份文件不写入。
	if response := upload("买家ID,是否启用\n333,是\n444,是\n"); response.Code != 200 {
		t.Fatal(response.Code)
	} // response 验证只带必需买家列也可导入。
	if response := upload("wrong\nfixture\n"); response.Code != 400 {
		t.Fatal("missing required header accepted")
	} // response 验证错误文件不会写入。
}

// TestBuyerBlockImportFlags 核验原系统及 CSV 的显式状态不能被静默改为停用，未知文本必须拒绝。
func TestBuyerBlockImportFlags(t *testing.T) {
	for _, value := range []string{"是", "true", "TRUE", "1", "yes"} { // value 为明确启用格式。
		// got、err 为解析结果。
		got, err := parseBuyerBlockFlag(value)
		if err != nil || !got {
			t.Fatalf("flag=%q err=%v", value, err)
		}
	}
	for _, value := range []string{"", "否", "false", "FALSE", "0", "no"} { // value 为关闭或缺省格式。
		// got、err 为解析结果。
		got, err := parseBuyerBlockFlag(value)
		if err != nil || got {
			t.Fatalf("flag=%q err=%v", value, err)
		}
	}
	// err 对未知显式状态不得猜测。
	if _, err := parseBuyerBlockFlag("maybe"); err == nil {
		t.Fatal("unknown flag accepted")
	}
}
