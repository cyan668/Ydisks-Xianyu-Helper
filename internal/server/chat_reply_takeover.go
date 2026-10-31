package server

import (
	"context"
	"errors"
	"net/http"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// chatReplyTakeoverPort 保持原 ChatPort 构造合同，新入口独立消费接管设置与会话状态。
type chatReplyTakeoverPort interface {
	// ReplyProtectionMinutes 只读当前用户账号的分钟配置。
	ReplyProtectionMinutes(context.Context, int64, string) (int, error)
	// SaveReplyProtectionMinutes 保存当前用户账号的配置。
	SaveReplyProtectionMinutes(context.Context, int64, string, int) error
	// ConversationReplyPause 读取已核验归属会话的暂停状态。
	ConversationReplyPause(context.Context, int64, string, string) (chatapp.ReplyPauseState, error)
	// ResumeConversationReply 基于旧状态版本恢复，避免覆盖并发的新人工消息。
	ResumeConversationReply(context.Context, int64, string, string, int64) error
}

// replyProtectionDTO 是账号设置的具名传输模型，零表示关闭，正数单位为分钟。
type replyProtectionDTO struct {
	// ManualPauseMinutes 是人工发送后该会话自动客服暂停的分钟数。
	ManualPauseMinutes int `json:"manual_pause_minutes"`
}

// replyPauseDTO 是会话接管的具名传输模型，时间戳单位为毫秒。
type replyPauseDTO struct {
	// Version 是与截止时间同一快照的递增版本。
	Version int64 `json:"version"`
	// PausedUntil 是接管截止的 Unix 毫秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 是当前会话是否仍在接管窗口内。
	Paused bool `json:"paused"`
}

// resumeReplyRequest 必须携带用户看到的状态版本，防止旧页面清除新接管。
type resumeReplyRequest struct {
	// AccountID、ChatID 只定位当前登录用户拥有的会话。
	AccountID string `json:"account_id"`
	// ChatID 是明确的会话标识。
	ChatID string `json:"chat_id"`
	// ExpectedVersion 是比较并交换的旧状态版本。
	ExpectedVersion *int64 `json:"expected_version"`
}

// replyProtectionRequest 是保存账号接管配置的具名请求。
type replyProtectionRequest struct {
	// AccountID 必须属于当前认证用户。
	AccountID string `json:"account_id"`
	// ManualPauseMinutes 是 0 到 1440 的分钟数。
	ManualPauseMinutes *int `json:"manual_pause_minutes"`
}

// replyTakeoverApplication 在独立新入口检查可选扩展，旧离线 ChatPort 保持可构造。
func (s *Server) replyTakeoverApplication(w http.ResponseWriter, r *http.Request) chatReplyTakeoverPort {
	// port、ok 是生产聊天应用是否具备接管用例。
	port, ok := s.chatApplication().(chatReplyTakeoverPort)
	if !ok {
		writeErrCode(w, 503, "reply_protection_unavailable", "会话回复保护未启用", "")
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	return port
}

// getReplyProtection 返回账号独立的接管设置，不请求闲鱼。
func (s *Server) getReplyProtection(w http.ResponseWriter, r *http.Request) {
	// port 是已装配的聊天接管应用服务。
	port := s.replyTakeoverApplication(w, r)
	if port == nil {
		return
	}
	// minutes、err 是当前认证用户账号的本地配置。
	minutes, err := port.ReplyProtectionMinutes(r.Context(), auth.SessionFromContext(r.Context()).UserID, r.URL.Query().Get("account_id"))
	if err != nil {
		writeReplyTakeoverError(w, r, err)
		return
	}
	writeJSON(w, 200, replyProtectionDTO{ManualPauseMinutes: minutes})
}

// saveReplyProtection 保存用户明确填写的分钟数，本动作不会启用账号或立即开始接管。
func (s *Server) saveReplyProtection(w http.ResponseWriter, r *http.Request) {
	// port 为已固定装配的新用例端口。
	port := s.replyTakeoverApplication(w, r)
	if port == nil {
		return
	}
	// request 是本次账号配置输入，身份不从客户端接收。
	var request replyProtectionRequest
	if err := decodeJSON(r, &request); err != nil { // err 是 JSON 格式错误，尚无写入。
		writeReplyTakeoverError(w, r, chatapp.ErrInvalidInput)
		return
	}
	if request.ManualPauseMinutes == nil {
		writeReplyTakeoverError(w, r, chatapp.ErrInvalidInput)
		return
	}
	if err := port.SaveReplyProtectionMinutes(r.Context(), auth.SessionFromContext(r.Context()).UserID, request.AccountID, *request.ManualPauseMinutes); err != nil { // err 为范围、归属或持久化错误。
		writeReplyTakeoverError(w, r, err)
		return
	}
	writeJSON(w, 200, replyProtectionDTO{ManualPauseMinutes: *request.ManualPauseMinutes})
}

// getConversationReplyPause 返回当前用户可见会话的实时本地接管状态。
func (s *Server) getConversationReplyPause(w http.ResponseWriter, r *http.Request) {
	// port 为新会话管理用例，不依赖在线账号。
	port := s.replyTakeoverApplication(w, r)
	if port == nil {
		return
	}
	// state、err 是本地状态和读取错误；错误不能伪装为“已恢复”。
	state, err := port.ConversationReplyPause(r.Context(), auth.SessionFromContext(r.Context()).UserID, r.URL.Query().Get("account_id"), r.URL.Query().Get("chat_id"))
	if err != nil {
		writeReplyTakeoverError(w, r, err)
		return
	}
	writeJSON(w, 200, replyPauseDTO{Version: state.Version, PausedUntil: state.PausedUntil, Paused: state.Paused})
}

// resumeConversationReply 仅恢复明确状态版本，不调用平台删除、发送或账号启停。
func (s *Server) resumeConversationReply(w http.ResponseWriter, r *http.Request) {
	// port 是当前固定的新能力扩展。
	port := s.replyTakeoverApplication(w, r)
	if port == nil {
		return
	}
	// request 是用户看到状态后的明确恢复动作。
	var request resumeReplyRequest
	if err := decodeJSON(r, &request); err != nil { // err 为请求格式错误。
		writeReplyTakeoverError(w, r, chatapp.ErrInvalidInput)
		return
	}
	if request.ExpectedVersion == nil {
		writeReplyTakeoverError(w, r, chatapp.ErrInvalidInput)
		return
	}
	if err := port.ResumeConversationReply(r.Context(), auth.SessionFromContext(r.Context()).UserID, request.AccountID, request.ChatID, *request.ExpectedVersion); err != nil { // err 是归属或并发版本冲突。
		writeReplyTakeoverError(w, r, err)
		return
	}
	// state、err 为恢复后的最新快照，并发出现的新接管也会如实返回。
	state, err := port.ConversationReplyPause(r.Context(), auth.SessionFromContext(r.Context()).UserID, request.AccountID, request.ChatID)
	if err != nil {
		writeReplyTakeoverError(w, r, err)
		return
	}
	writeJSON(w, 200, replyPauseDTO{Version: state.Version, PausedUntil: state.PausedUntil, Paused: state.Paused})
}

// writeReplyTakeoverError 使用统一错误封装，仅反馈安全的用例类别，不传播 SQL 或平台正文。
func writeReplyTakeoverError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, chatapp.ErrInvalidInput):
		writeErrCode(w, 400, "reply_protection_invalid", "账号、会话或暂停分钟数无效", "")
	case errors.Is(err, chatapp.ErrSessionForbidden):
		writeErrCode(w, 403, "reply_protection_forbidden", "无权操作该账号", "")
	case errors.Is(err, chatapp.ErrChatSessionNotFound):
		writeErrCode(w, 404, "reply_pause_not_found", "聊天会话不存在", "")
	case errors.Is(err, chatapp.ErrReplyPauseChanged):
		writeErrCode(w, 409, "reply_pause_changed", "会话状态已变化，请刷新后再恢复", "")
	case errors.Is(err, chatapp.ErrSessionUnavailable):
		writeErrCode(w, 503, "reply_protection_unavailable", "会话回复保护未启用", "")
	default:
		writeErrCode(w, 500, "reply_protection_failed", "保存或读取会话回复保护失败", "")
	}
}
