package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	blockapp "xianyu-go/internal/application/buyerblock"
	"xianyu-go/internal/buyerblock"
)

// buyerBlockRequest 是交易黑名单写入 DTO；用户身份只来自登录会话。
type buyerBlockRequest struct {
	// CookieID 为空表示当前用户全部账号。
	CookieID string `json:"cookie_id"`
	// ItemID 为空表示所选范围的全部商品。
	ItemID string `json:"item_id"`
	// BuyerID 是稳定平台买家 ID，而非昵称。
	BuyerID string `json:"buyer_id"`
	// BuyerNick 仅供展示和表格往返，不参与匹配。
	BuyerNick string `json:"buyer_nick"`
	// Reason 是仅供管理者查看的备注。
	Reason string `json:"reason"`
	// Enabled 显式决定是否参与交易拦截。
	Enabled bool `json:"enabled"`
	// AutoClose 显式授权系统事件命中时尝试一次关单。
	AutoClose bool `json:"auto_close"`
}

// buyerBlockResponse 返回带主键的非敏感黑名单配置，不暴露本地用户身份或平台凭据。
type buyerBlockResponse struct {
	// ID 是记录主键。
	ID int64 `json:"id"`
	// buyerBlockRequest 提供与写入相同的作用域和状态字段。
	buyerBlockRequest
}

// buyerBlockJSON 把 entry 转换为 HTTP 配置摘要。
func buyerBlockJSON(entry buyerblock.Entry) buyerBlockResponse {
	return buyerBlockResponse{ID: entry.ID, buyerBlockRequest: buyerBlockRequest{CookieID: entry.CookieID, ItemID: entry.ItemID, BuyerID: entry.BuyerID, BuyerNick: entry.BuyerNick, Reason: entry.Reason, Enabled: entry.Enabled, AutoClose: entry.AutoClose}}
}

// listBuyerBlocks 按登录用户和可选 cookie_id 返回交易黑名单。
func (s *Server) listBuyerBlocks(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是已认证的用户身份及获取结果。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// entries、err 是应用服务查询结果，不能返回其他用户的记录。
	entries, err := s.applicationServiceSet().buyerBlocks.List(r.Context(), userID, r.URL.Query().Get("cookie_id"))
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	// out 为空时仍输出数组。
	out := make([]buyerBlockResponse, 0, len(entries))
	for _, entry := range entries { // entry 是当前用户可见的一条记录。
		out = append(out, buyerBlockJSON(entry))
	}
	writeJSON(w, http.StatusOK, out)
}

// saveBuyerBlock 创建或更新交易黑名单；路径有 id 时必须为正整数。
func (s *Server) saveBuyerBlock(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证身份及验证结果。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id 为空时为新建记录，更新时由路径解析。
	var id int64
	if r.Method == http.MethodPut {
		// parsed、err 保存主键解析结果。
		parsed, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || parsed <= 0 {
			writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
			return
		}
		id = parsed
	}
	// request 只承载非敏感配置，未知用户标识不会被应用。
	var request buyerBlockRequest
	if err := decodeJSON(r, &request); err != nil { // err 表示 JSON 请求体无法解析。
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	}
	// entry、err 是经过归属和输入校验后的持久化结果。
	entry, err := s.applicationServiceSet().buyerBlocks.Save(r.Context(), userID, buyerblock.Entry{ID: id, CookieID: request.CookieID, ItemID: request.ItemID, BuyerID: request.BuyerID, BuyerNick: request.BuyerNick, Reason: request.Reason, Enabled: request.Enabled, AutoClose: request.AutoClose})
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	// status 区分创建与覆盖，响应使用同一具名 DTO。
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, buyerBlockJSON(entry))
}

// deleteBuyerBlock 只删除认证用户拥有的路径主键。
func (s *Server) deleteBuyerBlock(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证身份及其存在性。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id、err 核验正整数主键。
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	}
	if err := s.applicationServiceSet().buyerBlocks.Delete(r.Context(), userID, id); err != nil { // err 表示记录缺失、越权或持久化失败。
		writeBuyerBlockError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// writeBuyerBlockError 把应用错误映射为稳定错误信封，不泄漏数据库语句或平台凭据。
func writeBuyerBlockError(w http.ResponseWriter, r *http.Request, err error) {
	// status、message 只对已知输入错误公开具体原因。
	status, message := http.StatusInternalServerError, "交易黑名单操作失败"
	switch {
	case errors.Is(err, buyerblock.ErrInvalid):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, buyerblock.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, buyerblock.ErrConflict):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, blockapp.ErrPlatformUnavailable):
		status, message = http.StatusServiceUnavailable, err.Error()
	}
	writeErrRequest(w, r, status, message)
}
