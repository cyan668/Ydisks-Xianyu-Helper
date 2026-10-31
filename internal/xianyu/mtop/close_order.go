package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// sellerCloseAPI 是 Cookie 签名的卖家关单接口，不是 ISV accessToken 接口。
const sellerCloseAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.trade.merchant.close.by.seller/2.0/"

// CloseOrderBySellerContext 对 orderID 只发送一次关单请求；cookiesStr 明文仅用于本次签名与响应 Cookie 合并。
// 返回平台明确成功、ret、更新后的 Cookie 和请求错误；不刷新 Token、不自动重试不确定交易。
func (c *ClientImpl) CloseOrderBySellerContext(ctx context.Context, cookiesStr, orderID string) (bool, []string, string, error) {
	if strings.TrimSpace(orderID) == "" {
		return false, nil, cookiesStr, errors.New("卖家关单缺少订单号")
	}
	// endpoint 默认使用正式接口；本地测试可显式注入回环地址。
	endpoint := c.CloseOrderURL
	if endpoint == "" {
		endpoint = sellerCloseAPI
	}
	// signingCookies、requestCookies 由既有 Cookie Jar 策略决定，不能记录到日志。
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://seller.goofish.com/", endpoint)
	// payload 固定使用已核实的订单字段与非敏感原因，不传递用户备注。
	payload := struct {
		// TID 与 BizOrderID 均为同一平台订单号。
		TID string `json:"tid"`
		// BizOrderID 是卖家交易接口的兼容订单字段。
		BizOrderID string `json:"bizOrderId"`
		// Reason 是平台卖家关单原因。
		Reason string `json:"closeReason"`
	}{orderID, orderID, "其他原因"}
	// data、err 为签名和表单共享的 JSON 字节。
	data, err := json.Marshal(payload)
	if err != nil {
		return false, nil, cookiesStr, err
	}
	// timestamp 是本次请求毫秒时间戳。
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// query 使用与签名一致的应用键和接口版本。
	query := url.Values{"jsv": {"2.7.2"}, "appKey": {protocol.SignAppKey}, "t": {timestamp}, "sign": {protocol.GenerateSign(timestamp, protocol.SignToken(signingCookies), string(data))}, "api": {"mtop.taobao.idle.trade.merchant.close.by.seller"}, "v": {"2.0"}, "type": {"originaljson"}, "dataType": {"json"}, "accountSite": {"xianyu"}, "timeout": {"20000"}, "sessionOption": {"AutoLoginOnly"}, "spm_cnt": {"a21107h.44911108.0.0"}, "spm_pre": {"a21107h.44911108.0.0"}}
	// request、err 为唯一一次平台出站请求，始终经过账号级节流传输层。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader(url.Values{"data": {string(data)}}.Encode()))
	if err != nil {
		return false, nil, cookiesStr, err
	}
	setCommonHeaders(request, requestCookies)
	request.Header.Set("Origin", "https://seller.goofish.com")
	request.Header.Set("Referer", "https://seller.goofish.com/")
	// response、err 保存出站响应；传输错误不能推断平台未关单。
	response, err := c.httpClientWithTimeout(20 * time.Second).Do(request)
	if err != nil {
		return false, nil, cookiesStr, c.mtopResponseFailureWithCause("卖家关单接口", 0, nil, "请求失败", err)
	}
	defer response.Body.Close()
	// updated 合并响应 Cookie，包括平台拒绝和解码失败路径。
	updated := absorbMTopResponseCookies(ctx, cookiesStr, response)
	// raw、readErr 是有界响应读取结果。
	raw, readErr := readMTopBody(response)
	if readErr != nil {
		return false, nil, updated, c.mtopResponseFailureWithCause("卖家关单接口", response.StatusCode, nil, "读取响应失败", readErr)
	}
	// result 只读取 ret，不记录可能含私有信息的原响应。
	var result struct { /* Ret 是平台统一业务状态。 */
		Ret []string `json:"ret"`
	}
	// parseErr 表示响应结构无法确认，不能认定关单失败或安全重试。
	if parseErr := json.Unmarshal(raw, &result); parseErr != nil {
		return false, nil, updated, c.mtopResponseFailureWithCause("卖家关单接口", response.StatusCode, nil, "JSON 解析失败", parseErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, result.Ret, updated, c.mtopResponseFailure("卖家关单接口", response.StatusCode, result.Ret, "HTTP 状态异常")
	}
	for _, ret := range result.Ret { // ret 是本次响应的单个业务状态码。
		if strings.HasPrefix(ret, "SUCCESS::") {
			return true, result.Ret, updated, nil
		}
	}
	return false, result.Ret, updated, c.mtopResponseFailure("卖家关单接口", response.StatusCode, result.Ret, "平台未确认关单成功")
}
