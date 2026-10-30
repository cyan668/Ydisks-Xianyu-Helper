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

// OfficialBlacklistContext 对指定 chatID 的 query/add/remove 最多发起一个 MTOP 请求，不遍历会话或自动重试。
// cookies 是仅用于当前签名/条件写回的明文，不记录到日志；返回明确状态、响应 Cookie 及安全错误。
func (c *ClientImpl) OfficialBlacklistContext(ctx context.Context, cookies, chatID, action string) (bool, string, error) {
	if chatID == "" || (action != "query" && action != "add" && action != "remove") {
		return false, cookies, errors.New("会话黑名单参数无效")
	}
	// version、api 指向已核实的三种官方会话能力，不是交易黑名单表。
	version := "1.0"
	if action == "add" {
		version = "2.0"
	}
	// api 只从固定 action 枚举拼接，不接受任意接口名。
	api := "mtop.taobao.idlemessage.pc.blacklist." + action
	// base 允许隔离测试注入回环服务器，生产固定 HTTPS 端点。
	base := c.OfficialBlacklistBaseURL
	if base == "" {
		base = "https://h5api.m.goofish.com/h5"
	}
	// endpoint 是单个会话操作的完整端点。
	endpoint := strings.TrimRight(base, "/") + "/" + api + "/" + version + "/"
	// signing、requestCookies 分别用于签名及按域发送，禁止传播到普通响应。
	signing, requestCookies := mtopRequestCookies(ctx, cookies, "https://www.goofish.com/", endpoint)
	// body 为唯一协议参数 sessionId，通过 JSON 转义避免注入。
	body, _ := json.Marshal(struct { /* SessionID 为当前会话平台标识。 */
		SessionID string `json:"sessionId"`
	}{chatID})
	// timestamp、query 是当前一次请求的时间和签名参数。
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// query 沿用统一应用键，版本由 action 固定决定。
	query := url.Values{"api": {api}, "v": {version}, "appKey": {protocol.SignAppKey}, "jsv": {"2.7.2"}, "t": {timestamp}, "sign": {protocol.GenerateSign(timestamp, protocol.SignToken(signing), string(body))}, "type": {"originaljson"}, "dataType": {"json"}, "accountSite": {"xianyu"}, "sessionOption": {"AutoLoginOnly"}}
	// request、err 为唯一平台请求，默认传输仍经过账号级节流。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+query.Encode(), strings.NewReader(url.Values{"data": {string(body)}}.Encode()))
	if err != nil {
		return false, cookies, err
	}
	setCommonHeaders(request, requestCookies)
	// response、err 为本次请求结果，不因超时、风控或过期增加请求。
	response, err := c.httpClient().Do(request)
	if err != nil {
		return false, cookies, c.mtopResponseFailureWithCause("会话黑名单接口", 0, nil, "请求失败，未自动重试", err)
	}
	defer response.Body.Close()
	// updated 保留失败响应下发的 Cookie。
	updated := absorbMTopResponseCookies(ctx, cookies, response)
	// raw、readErr 为有界响应内容和读取错误。
	raw, readErr := readMTopBody(response)
	if readErr != nil {
		return false, updated, c.mtopResponseFailureWithCause("会话黑名单接口", response.StatusCode, nil, "读取响应失败", readErr)
	}
	// result 只保留统一 ret 和查询所需的明确布尔值。
	var result struct {
		// Ret 是平台业务状态，不包含账号凭据。
		Ret []string `json:"ret"`
		// Data 是 query 返回的会话是否拉黑状态。
		Data struct { /* IsInBlack 使用指针区分 false 和字段缺失。 */
			IsInBlack *bool `json:"isInBlack"`
		} `json:"data"`
	}
	// parseErr 表示响应无法确认，不能猜测未拉黑。
	if parseErr := json.Unmarshal(raw, &result); parseErr != nil {
		return false, updated, c.mtopResponseFailureWithCause("会话黑名单接口", response.StatusCode, nil, "解析响应失败", parseErr)
	}
	// success 要求 HTTP 成功且 ret 明确成功。
	success := false
	// ret 是当前单次响应的业务状态码，只有明确 SUCCESS 才确认。
	for _, ret := range result.Ret {
		if strings.HasPrefix(ret, "SUCCESS::") {
			success = true
		}
	} // ret 是当前业务返回码。
	if response.StatusCode < 200 || response.StatusCode >= 300 || !success {
		return false, updated, c.mtopResponseFailure("会话黑名单接口", response.StatusCode, result.Ret, "平台未确认成功")
	}
	if action != "query" {
		return action == "add", updated, nil
	}
	if result.Data.IsInBlack == nil {
		return false, updated, errors.New("平台未返回明确的会话黑名单状态")
	}
	return *result.Data.IsInBlack, updated, nil
}
