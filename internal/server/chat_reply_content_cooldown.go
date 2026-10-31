package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// chatContentCooldownPort 保持原 ChatPort 构造可用，独立管理本地重复内容保护。
type chatContentCooldownPort interface {
	// ContentCooldownSeconds 读取当前用户账号的冷却秒数。
	ContentCooldownSeconds(context.Context, int64, string) (int, error)
	// SaveContentCooldownSeconds 保存当前用户明确设置的秒数。
	SaveContentCooldownSeconds(context.Context, int64, string, int) error
	// ContentCooldownReviews 分页读取本人账号的未知投递保护。
	ContentCooldownReviews(context.Context, int64, string, int, int) (chatapp.ContentCooldownPage, error)
	// ResolveContentCooldown 核对后按旧版本解除保护，不重发消息。
	ResolveContentCooldown(context.Context, int64, string, int64, int64, bool) error
}

// contentCooldownDTO 是独立秒级配置，不能覆盖人工接管分钟数。
type contentCooldownDTO struct {
	// Seconds 为 0 到 86400 的整数，零关闭新冷却。
	Seconds int `json:"seconds"`
}

// contentCooldownWriteDTO 是明确的账号配置输入。
type contentCooldownWriteDTO struct {
	// AccountID 必须属于当前认证用户。
	AccountID string `json:"account_id"`
	// Seconds 用指针区分缺省输入与明确关闭。
	Seconds *int `json:"seconds"`
}

// contentCooldownReviewDTO 只含定位原消息及状态比较所需字段，不暴露正文或摘要键。
type contentCooldownReviewDTO struct {
	// ID 是此项保护的稳定主键。
	ID int64 `json:"id"`
	// Version 是用户看到的状态代次。
	Version int64 `json:"version"`
	// ChatID 是当前账号下的会话标识。
	ChatID string `json:"chat_id"`
	// MessageKey 是触发原处理的平台消息标识。
	MessageKey string `json:"message_key"`
	// ClaimedAt 是本地接纳消息的 Unix 毫秒。
	ClaimedAt int64 `json:"claimed_at"`
}

// contentCooldownPageDTO 描述稳定排序的本地保护分页。
type contentCooldownPageDTO struct {
	// Items 是当前页保护，空集合序列化为空数组。
	Items []contentCooldownReviewDTO `json:"items"`
	// Total 是本人账号待核对总数。
	Total int `json:"total"`
	// Limit 是经过上限校验的页长。
	Limit int `json:"limit"`
	// Offset 是当前页跳过的记录数。
	Offset int `json:"offset"`
}

// contentCooldownResolveDTO 必须含用户明确核对确认及旧状态版本。
type contentCooldownResolveDTO struct {
	// AccountID 只定位当前认证用户的账号。
	AccountID string `json:"account_id"`
	// ExpectedVersion 缺省不能被解释为有效的解除请求。
	ExpectedVersion *int64 `json:"expected_version"`
	// Confirmed 表示用户已在平台核对过可能送达的回复。
	Confirmed bool `json:"confirmed"`
}

// contentCooldownResolvedDTO 只确认本地保护已解除，不表示已发送或重放消息。
type contentCooldownResolvedDTO struct {
	// Resolved 成功时固定为 true。
	Resolved bool `json:"resolved"`
}

// contentCooldownApplication 在新入口消费扩展，不扩大旧 ChatPort 必需合同。
func (s *Server) contentCooldownApplication(w http.ResponseWriter) chatContentCooldownPort {
	w.Header().Set("Cache-Control", "no-store")
	// port、ok 为已装配聊天应用的独立保护管理能力。
	port, ok := s.chatApplication().(chatContentCooldownPort)
	if !ok {
		writeContentCooldownError(w, chatapp.ErrSessionUnavailable)
		return nil
	}
	return port
}

// getContentCooldown 读取本人账号的本地配置，不启用账号或访问平台。
func (s *Server) getContentCooldown(w http.ResponseWriter, r *http.Request) {
	// port 为只提供本地管理用例的应用服务。
	port := s.contentCooldownApplication(w)
	if port == nil {
		return
	}
	// seconds、err 保留归属错误与读取故障，不能把故障当作零。
	seconds, err := port.ContentCooldownSeconds(r.Context(), auth.SessionFromContext(r.Context()).UserID, r.URL.Query().Get("account_id"))
	if err != nil {
		writeContentCooldownError(w, err)
		return
	}
	writeJSON(w, 200, contentCooldownDTO{Seconds: seconds})
}

// saveContentCooldown 只保存秒数，默认关闭也不能删除未知投递保护。
func (s *Server) saveContentCooldown(w http.ResponseWriter, r *http.Request) {
	// port 是可选的新用例端口。
	port := s.contentCooldownApplication(w)
	if port == nil {
		return
	}
	// request 是当前明确账号的输入，空秒数不能静默写零。
	var request contentCooldownWriteDTO
	if err := decodeJSON(r, &request); err != nil || request.Seconds == nil { // err 为 JSON 格式或类型错误。
		writeContentCooldownError(w, chatapp.ErrInvalidInput)
		return
	}
	if err := port.SaveContentCooldownSeconds(r.Context(), auth.SessionFromContext(r.Context()).UserID, request.AccountID, *request.Seconds); err != nil { // err 是范围、归属或存储错误。
		writeContentCooldownError(w, err)
		return
	}
	writeJSON(w, 200, contentCooldownDTO{Seconds: *request.Seconds})
}

// listContentCooldownReviews 返回账号下有界的未知保护分页，完全不查询平台。
func (s *Server) listContentCooldownReviews(w http.ResponseWriter, r *http.Request) {
	// port 为新管理应用。
	port := s.contentCooldownApplication(w)
	if port == nil {
		return
	}
	// limit、offset 使用显式默认值，非法文本直接拒绝。
	limit, offset := 20, 0
	for name, value := range map[string]*int{"limit": &limit, "offset": &offset} { // name、value 分别是查询参数与目标整数字段。
		if raw := r.URL.Query().Get(name); raw != "" { // raw 是用户显式输入的分页文本。
			// parsed、err 为不允许静默回退的十进制输入。
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				writeContentCooldownError(w, chatapp.ErrInvalidInput)
				return
			}
			*value = parsed
		}
	}
	// page、err 是已核验账号归属的分页结果。
	page, err := port.ContentCooldownReviews(r.Context(), auth.SessionFromContext(r.Context()).UserID, r.URL.Query().Get("account_id"), limit, offset)
	if err != nil {
		writeContentCooldownError(w, err)
		return
	}
	// response 明确转换应用模型，不直接序列化数据库行。
	response := contentCooldownPageDTO{Items: make([]contentCooldownReviewDTO, 0, len(page.Items)), Total: page.Total, Limit: page.Limit, Offset: page.Offset}
	for _, item := range page.Items { // item 为本人账号当前页的保护定位信息。
		response.Items = append(response.Items, contentCooldownReviewDTO{ID: item.ID, Version: item.Version, ChatID: item.ChatID, MessageKey: item.MessageKey, ClaimedAt: item.ClaimedAt})
	}
	writeJSON(w, 200, response)
}

// resolveContentCooldownReview 只解除明确核对的旧版本，不调用发送或账号启停入口。
func (s *Server) resolveContentCooldownReview(w http.ResponseWriter, r *http.Request) {
	// port 是本地恢复用例。
	port := s.contentCooldownApplication(w)
	if port == nil {
		return
	}
	// id、err 为保护记录主键，不接受任意会话正文。
	id, err := strconv.ParseInt(chi.URLParam(r, "claim_id"), 10, 64)
	// request 需要账号、用户看到的版本和明确确认。
	var request contentCooldownResolveDTO
	if err != nil || decodeJSON(r, &request) != nil || request.ExpectedVersion == nil {
		writeContentCooldownError(w, chatapp.ErrInvalidInput)
		return
	}
	if err := port.ResolveContentCooldown(r.Context(), auth.SessionFromContext(r.Context()).UserID, request.AccountID, id, *request.ExpectedVersion, request.Confirmed); err != nil { // err 保留并发版本冲突，不误报解除成功。
		writeContentCooldownError(w, err)
		return
	}
	writeJSON(w, 200, contentCooldownResolvedDTO{Resolved: true})
}

// writeContentCooldownError 将用例失败转换为稳定统一错误，不传播 SQL、正文或凭据。
func writeContentCooldownError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chatapp.ErrInvalidInput):
		writeErrCode(w, 400, "content_cooldown_invalid", "账号、冷却秒数、分页或核对确认无效", "")
	case errors.Is(err, chatapp.ErrSessionForbidden):
		writeErrCode(w, 403, "content_cooldown_forbidden", "无权操作该账号", "")
	case errors.Is(err, chatapp.ErrContentReviewChanged):
		writeErrCode(w, 409, "content_cooldown_changed", "保护状态已变化，请刷新待核对列表", "")
	case errors.Is(err, chatapp.ErrSessionUnavailable):
		writeErrCode(w, 503, "content_cooldown_unavailable", "相同消息冷却尚未启用", "")
	default:
		writeErrCode(w, 500, "content_cooldown_failed", "读取或保存相同消息冷却失败", "")
	}
}
