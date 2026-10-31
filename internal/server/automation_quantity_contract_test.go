package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/deliveryquantity"
)

// TestAutomationQuantityModeContract 用真实路由和 SQLite 验证扩展策略可保存、回读，非法更新不会覆盖原规则。
func TestAutomationQuantityModeContract(t *testing.T) {
	// srv、store、cleanup 创建完全隔离的服务与测试数据库，不启用账号或调用平台。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 仅用于测试数据准备。
	ctx := context.Background()
	// cardID、err 创建只含虚构内容的卡券组。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "quantity-contract", Type: "text", TextContent: "fixture", Enabled: true, UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// handler 同时校验真实版本化响应的 OpenAPI 契约。
	handler := contractRecordingHandler(t, srv.Router())
	// cookie 是本地夹具会话，只用于此 HTTP 测试。
	cookie := loginHelper(t, handler)
	// draft 使用具名 transport DTO，保持请求字段与现有协议一致。
	draft := automationRuleRequest{CookieID: "acc1", Name: "固定份数", TriggerType: "order_paid", Enabled: false, ConfigJSON: "{}", Actions: []automationActionRequest{{ActionType: "send_card", CardID: cardID, DeliveryCount: 2, ConfigJSON: `{"quantity_mode":"per_order"}`}}}
	// raw、marshalErr 编码当前合法草稿。
	raw, marshalErr := json.Marshal(draft)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	// request 是登录用户提交的本地新建请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/automation-rules", strings.NewReader(string(raw)))
	request.AddCookie(cookie)
	// response 捕获实际服务状态码和响应，不访问网络。
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create status=%d: %s", response.Code, response.Body.String())
	}
	// created 解码新建结果，用于验证失败更新不会覆盖规则。
	var created mutationIDResponse
	// err 检查创建结果是否仍符合响应 DTO。
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	draft.Actions[0].ConfigJSON = `{"quantity_mode":"invalid"}`
	raw, marshalErr = json.Marshal(draft)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/automation-rules/"+itoa(created.ID), strings.NewReader(string(raw)))
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode status=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/automation-rules", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d", response.Code)
	}
	// rows 是保存后重新查询的非敏感规则 DTO。
	var rows []automationRuleResponse
	// err 检查列表响应是否可以按具名 DTO 解码。
	if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Actions) != 1 || rows[0].Enabled {
		t.Fatal("规则或启用状态不符")
	}
	// mode、modeErr 验证持久化后的 JSON 仍是原先固定策略。
	mode, modeErr := deliveryquantity.Parse(rows[0].Actions[0].ConfigJSON)
	if modeErr != nil || mode != deliveryquantity.PerOrder || rows[0].Actions[0].DeliveryCount != 2 {
		t.Fatal("持久化丢失数量策略")
	}
}
