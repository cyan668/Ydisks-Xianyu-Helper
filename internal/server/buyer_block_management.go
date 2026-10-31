package server

import (
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	blockapp "xianyu-go/internal/application/buyerblock"
	"xianyu-go/internal/buyerblock"
)

// buyerBlockPageResponse 是分页本地黑名单的 HTTP 契约。
type buyerBlockPageResponse struct {
	// Data 为当前页配置，空页仍为数组。
	Data []buyerBlockResponse `json:"data"`
	// Total、Page、PageSize 是筛选总数、一基页码与页大小。
	Total int `json:"total"`
	// Page 是一基页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
}

// buyerBlockBatchResponse 提供原子导入和订单拉黑结果。
type buyerBlockBatchResponse struct {
	// Created 是新增数量。
	Created int `json:"created"`
	// Skipped 是重复或空记录跳过数。
	Skipped int `json:"skipped"`
	// Affected 是批量状态变更/删除所处理的记录数。
	Affected int `json:"affected"`
}

// buyerBlockBatchRequest 是所选记录的明确批量动作，不把当前页选择扩大为全部筛选。
type buyerBlockBatchRequest struct {
	// IDs 是用户明确勾选的主键。
	IDs []int64 `json:"ids"`
	// Action 为 enable/disable/delete。
	Action string `json:"action"`
}

// buyerBlockOrderRequest 必须明确选择商品、账号或用户级，不能通过浏览器指定买家身份。
type buyerBlockOrderRequest struct {
	// Level 是 item/account/user。
	Level string `json:"level"`
	// Reason 是用户提供的可选管理备注。
	Reason string `json:"reason"`
}

// buyerBlockOfficialRequest 显式选择 query/add/remove，每个操作最多一次平台请求。
type buyerBlockOfficialRequest struct { /* Action 是用户明确选择的会话动作。 */
	Action string `json:"action"`
}

// buyerBlockOfficialResponse 返回当前会话的明确平台状态，不代表全量名单同步。
type buyerBlockOfficialResponse struct { /* Blocked 表示此会话的买家是否被平台拉黑。 */
	Blocked bool `json:"blocked"`
}

// pageBuyerBlocks 解析分页/搜索并交给应用服务核验用户归属。
func (s *Server) pageBuyerBlocks(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是认证用户身份。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// page、size 是用户输入，非法值不会变成无界列表。
	page, size := 1, 20
	for key, target := range map[string]*int{"page": &page, "page_size": &size} { // key、target 为允许的分页参数和接收位置。
		if raw := r.URL.Query().Get(key); raw != "" { // raw 为当前参数原始文本。
			// value、err 限制正整数输入。
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 {
				writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
				return
			}
			*target = value
		}
	}
	// result、err 是应用分页结果。
	result, err := s.applicationServiceSet().buyerBlocks.Page(r.Context(), userID, blockapp.Query{AccountID: r.URL.Query().Get("cookie_id"), Search: r.URL.Query().Get("search"), Page: page, PageSize: size})
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	// rows 是当前页独立 transport DTO。
	rows := make([]buyerBlockResponse, 0, len(result.Rows))
	// entry 是当前页的个人配置，转换为独立 HTTP DTO。
	for _, entry := range result.Rows {
		rows = append(rows, buyerBlockJSON(entry))
	} // entry 是本页可见配置。
	writeJSON(w, 200, buyerBlockPageResponse{Data: rows, Total: result.Total, Page: result.Number, PageSize: result.Size})
}

// batchBuyerBlocks 对用户明确选择的 ids 做一次本地原子变更。
func (s *Server) batchBuyerBlocks(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是认证用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// request 是用户确认后的批量动作。
	var request buyerBlockBatchRequest
	// err 表示批量请求 JSON 无法解码，未产生任何写入。
	if err := decodeJSON(r, &request); err != nil {
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	} // err 是 JSON 解析失败。
	// affected、err 是集合全部归属核验后的事务结果。
	affected, err := s.applicationServiceSet().buyerBlocks.Batch(r.Context(), userID, request.IDs, request.Action)
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	writeJSON(w, 200, buyerBlockBatchResponse{Affected: affected})
}

// buyerBlockFromOrder 从拥有的订单创建个人记录，重复调用只跳过，不关闭订单或操作平台。
func (s *Server) buyerBlockFromOrder(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是认证用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// request 必须明确选择作用域。
	var request buyerBlockOrderRequest
	// err 表示订单拉黑请求无法解码，不能猜测生效范围。
	if err := decodeJSON(r, &request); err != nil {
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	} // err 是 JSON 解析失败。
	// result、err 是安全派生买家身份后的本地导入结果。
	result, err := s.applicationServiceSet().buyerBlocks.FromOrder(r.Context(), userID, chi.URLParam(r, "order_id"), request.Level, request.Reason)
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	writeJSON(w, 200, buyerBlockBatchResponse{Created: result.Created, Skipped: result.Skipped})
}

// exportBuyerBlocks 导出本人本地配置，明确标记 no-store，避免响应缓存个人名单。
func (s *Server) exportBuyerBlocks(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是认证用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// content、err 是应用服务生成的 CSV 字节。
	content, err := s.applicationServiceSet().buyerBlocks.ExportCSV(r.Context(), userID, r.URL.Query().Get("cookie_id"))
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "个人黑名单.csv"}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
}

// importBuyerBlocks 接受 XLSX/CSV/TSV，复用现有有界表格读取，原始字节不进入日志或公共仓库。
func (s *Server) importBuyerBlocks(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是认证用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	if !parseMultipartRequest(w, r, 6<<20, 5<<20, "黑名单文件不能超过 5 MiB") {
		return
	}
	// file、header、err 是上传流及元数据。
	file, header, err := r.FormFile("file")
	if err != nil {
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	}
	defer file.Close()
	// raw、large、readErr 是有界文件内容和失败结果。
	raw, large, readErr := readLimitedBytes(file, 5<<20)
	if readErr != nil || large {
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	}
	// rows、parseErr 为有限条数的传输表格，不在 handler 内决定归属或执行数据库写入。
	rows, parseErr := parsePublishSheetBytesWithLimit(raw, safeBaseName(header.Filename), blockapp.MaxBatchRows)
	if parseErr != nil {
		writeErrRequest(w, r, 400, parseErr.Error())
		return
	}
	// entries、blank 归一为应用配置，空买家行跳过并明确计数。
	entries := make([]buyerblock.Entry, 0, len(rows))
	// blank 统计文件中的空买家记录，与重复跳过数一起返回。
	blank := 0
	for index, row := range rows { // index、row 是文件中当前记录及原始单元格。
		// value 按明确别名读取文本，保留账号/买家长 ID，不转浮点。
		value := func(keys ...string) string { // keys 为允许的原系统中文或版本化字段别名。
			for cellKey, cell := range row { // cellKey、cell 是保留原大小写的表头及文本单元格。
				for _, key := range keys { // key 是允许的表头别名，不接受任意字段写入。
					if strings.EqualFold(strings.ReplaceAll(cellKey, "_", ""), strings.ReplaceAll(key, "_", "")) {
						// text 恢复本系统 CSV 公式防护转义，不对身份做浮点转换。
						text := strings.TrimSpace(fmt.Sprint(cell))
						if len(text) > 1 && text[0] == '\'' && strings.ContainsRune("=+-@", rune(text[1])) {
							text = text[1:]
						}
						return text
					}
				}
			}
			return ""
		}
		if index == 0 {
			// found 验证必需表头存在，第一行为空买家也不误判表头缺失。
			found := false
			// key 遍历第一行实际表头，大小写差异不应误判必要列缺失。
			for key := range row {
				if strings.EqualFold(key, "买家ID") || strings.EqualFold(strings.ReplaceAll(key, "_", ""), "buyerid") {
					found = true
				}
			} // key 是当前原始列名。
			if !found {
				writeErrRequest(w, r, 400, "表格缺少买家ID列")
				return
			}
		}
		// buyerID 是未经过数值转换的稳定身份。
		buyerID := value("买家id", "buyerid", "buyer_id")
		if buyerID == "" {
			blank++
			continue
		}
		// enabled、enabledErr 兼容明确的表格布尔格式，不能把未知文本默认为停用。
		enabled, enabledErr := parseBuyerBlockFlag(value("是否启用", "enabled"))
		// autoClose、closeErr 是独立关单开关，字段缺省始终关闭。
		autoClose, closeErr := parseBuyerBlockFlag(value("命中后关单", "autoclose"))
		if enabledErr != nil || closeErr != nil {
			writeErrRequest(w, r, 400, fmt.Sprintf("第 %d 条启用/关单列必须填写是、否、true、false、1或0", index+1))
			return
		}
		entries = append(entries, buyerblock.Entry{CookieID: value("cookie_id", "账号id", "accountid"), BuyerID: buyerID, BuyerNick: value("买家昵称", "buyernick"), ItemID: value("商品id", "itemid"), Reason: value("拉黑原因", "reason"), Enabled: enabled, AutoClose: autoClose})
	}
	if len(entries) == 0 {
		writeErrRequest(w, r, 400, "文件没有可导入的买家记录")
		return
	}
	// result、writeErr 表示整份文件验证后原子提交的结果，重复记录不覆盖。
	result, writeErr := s.applicationServiceSet().buyerBlocks.Import(r.Context(), userID, entries)
	if writeErr != nil {
		writeBuyerBlockError(w, r, writeErr)
		return
	}
	writeJSON(w, 200, buyerBlockBatchResponse{Created: result.Created, Skipped: result.Skipped + blank})
}

// parseBuyerBlockFlag 接受原系统的“是/否”及常见 CSV 布尔文本；字段缺省关闭，其他显式值拒绝整份文件。
func parseBuyerBlockFlag(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "是", "true", "1", "yes", "启用":
		return true, nil
	case "", "否", "false", "0", "no", "停用":
		return false, nil
	default:
		return false, buyerblock.ErrInvalid
	}
}

// officialBuyerBlock 只在用户主动点击时操作一个会话，禁止全量扇出和自动轮询。
func (s *Server) officialBuyerBlock(w http.ResponseWriter, r *http.Request) {
	// userID、ok 是认证用户。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// request 是明确的平台动作。
	var request buyerBlockOfficialRequest
	// err 表示官方会话操作请求无法解码，不会发起平台请求。
	if err := decodeJSON(r, &request); err != nil {
		writeBuyerBlockError(w, r, buyerblock.ErrInvalid)
		return
	} // err 是 JSON 解析失败。
	// blocked、err 为单次平台返回的明确状态。
	blocked, err := s.applicationServiceSet().buyerBlocks.Official(r.Context(), userID, chi.URLParam(r, "cookie_id"), chi.URLParam(r, "chat_id"), request.Action)
	if err != nil {
		writeBuyerBlockError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, buyerBlockOfficialResponse{Blocked: blocked})
}
