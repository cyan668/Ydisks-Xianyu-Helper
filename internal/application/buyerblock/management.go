package buyerblock

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"xianyu-go/internal/buyerblock"
)

// MaxBatchRows 限制一次导入或批量操作，全部仅访问本地数据库。
const MaxBatchRows = 500

// Query 是用户隔离的列表筛选，分页以记录为单位。
type Query struct {
	// AccountID 为空表示用户名下全部账号，非空时仍包含用户级规则。
	AccountID string
	// Search 按买家 ID、昵称或备注搜索。
	Search string
	// Page、PageSize 为一基页码与每页条数。
	Page, PageSize int
}

// Page 是规范化后的分页结果。
type Page struct {
	// Rows 为当前页的规则。
	Rows []buyerblock.Entry
	// Total、Number、Size 分别为记录总数、一基页码及页大小。
	Total, Number, Size int
}

// ManagementRepository 为批量写入和分页提供单次事务能力，旧 CRUD Port 仍可独立使用。
type ManagementRepository interface {
	// Page 读取用户隔离的数据库分页结果。
	Page(context.Context, int64, Query) (Page, error)
	// Import 以一个事务插入已校验记录，重复作用域保持原记录不变。
	Import(context.Context, int64, []buyerblock.Entry) (int, int, error)
	// Batch 以一个事务启用、停用或删除当前用户所选主键。
	Batch(context.Context, int64, []int64, string) (int, error)
	// OrderBuyer 返回当前用户拥有订单的稳定买家和所属账号/商品。
	OrderBuyer(context.Context, int64, string) (buyerblock.Entry, error)
}

// BatchResult 返回本地批量处理的数量，不代表平台操作。
type BatchResult struct {
	// Created、Skipped 分别为新增记录和重复跳过数。
	Created, Skipped int
}

// Page 校验用户和账号归属，再通过数据库查询筛选结果。
func (s *Service) Page(ctx context.Context, userID int64, query Query) (Page, error) {
	if s == nil || s.repository == nil {
		return Page{}, buyerblock.ErrInvalid
	}
	// repository、ok 为管理能力扩展，缺少时明确拒绝而不退化为无界读取。
	repository, ok := s.repository.(ManagementRepository)
	if !ok || userID <= 0 {
		return Page{}, buyerblock.ErrInvalid
	}
	if query.AccountID != "" {
		// owned、err 核验账号筛选不属于其他用户。
		owned, err := s.repository.OwnsScope(ctx, userID, query.AccountID, "")
		if err != nil {
			return Page{}, err
		}
		if !owned {
			return Page{}, buyerblock.ErrNotFound
		}
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	query.Search = strings.TrimSpace(query.Search)
	return repository.Page(ctx, userID, query)
}

// Import 校验完整文件所有记录的身份和作用域后一次提交，不允许失败行留下未报告的部分写入。
func (s *Service) Import(ctx context.Context, userID int64, entries []buyerblock.Entry) (BatchResult, error) {
	if s == nil || s.repository == nil {
		return BatchResult{}, buyerblock.ErrInvalid
	}
	// repository、ok 表示批量事务能力是否装配。
	repository, ok := s.repository.(ManagementRepository)
	if !ok || userID <= 0 || len(entries) == 0 || len(entries) > MaxBatchRows {
		return BatchResult{}, buyerblock.ErrInvalid
	}
	// normalized 是通过校验的完整文件，重复记录由数据库原子跳过。
	normalized := make([]buyerblock.Entry, 0, len(entries))
	for index, entry := range entries { // index、entry 为文件内的记录位置和未可信输入。
		entry.ID = 0
		entry.UserID = userID
		entry.CookieID, entry.ItemID, entry.Reason, entry.BuyerNick = strings.TrimSpace(entry.CookieID), strings.TrimSpace(entry.ItemID), strings.TrimSpace(entry.Reason), strings.TrimSpace(entry.BuyerNick)
		// identity、err 校验稳定标识，不接受浮点舍入后的数字猜测。
		identity, err := buyerblock.NormalizeBuyerID(entry.BuyerID)
		if err != nil || len(entry.CookieID) > 191 || len(entry.ItemID) > 191 || utf8.RuneCountInString(entry.Reason) > 500 || utf8.RuneCountInString(entry.BuyerNick) > 100 || (entry.ItemID != "" && entry.CookieID == "") {
			return BatchResult{}, fmt.Errorf("%w: 第 %d 条记录无效", buyerblock.ErrInvalid, index+1)
		}
		entry.BuyerID = identity
		if entry.CookieID != "" {
			// owned、scopeErr 防止导入文件指定其他用户的账号或商品。
			owned, scopeErr := s.repository.OwnsScope(ctx, userID, entry.CookieID, entry.ItemID)
			if scopeErr != nil {
				return BatchResult{}, scopeErr
			}
			if !owned {
				return BatchResult{}, fmt.Errorf("%w: 第 %d 条账号或商品不属于当前用户", buyerblock.ErrInvalid, index+1)
			}
		}
		normalized = append(normalized, entry)
	}
	// created、skipped、err 是一次本地事务的结果。
	created, skipped, err := repository.Import(ctx, userID, normalized)
	return BatchResult{Created: created, Skipped: skipped}, err
}

// Batch 对所选 ids 执行 action；不允许空集合、重复主键或超限，删除不解除关单保护。
func (s *Service) Batch(ctx context.Context, userID int64, ids []int64, action string) (int, error) {
	if s == nil || s.repository == nil {
		return 0, buyerblock.ErrInvalid
	}
	// repository、ok 为单次事务能力。
	repository, ok := s.repository.(ManagementRepository)
	if !ok || userID <= 0 || len(ids) == 0 || len(ids) > MaxBatchRows || (action != "enable" && action != "disable" && action != "delete") {
		return 0, buyerblock.ErrInvalid
	}
	// seen 防止重复 ID 干扰受影响行数核验。
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids { // id 为当前待处理主键。
		if id <= 0 || seen[id] {
			return 0, buyerblock.ErrInvalid
		}
		seen[id] = true
	}
	return repository.Batch(ctx, userID, ids, action)
}

// FromOrder 从当前用户拥有的订单提取买家，根据 level 建立启用的个人黑名单，不操作平台会话拉黑或关单。
func (s *Service) FromOrder(ctx context.Context, userID int64, orderID, level, reason string) (BatchResult, error) {
	if s == nil || s.repository == nil {
		return BatchResult{}, buyerblock.ErrInvalid
	}
	// repository、ok 是订单身份的非敏感读取能力。
	repository, ok := s.repository.(ManagementRepository)
	if !ok || userID <= 0 || (level != "user" && level != "account" && level != "item") {
		return BatchResult{}, buyerblock.ErrInvalid
	}
	// entry、err 由本地订单核验归属，不能信任浏览器提交买家 ID。
	entry, err := repository.OrderBuyer(ctx, userID, orderID)
	if err != nil {
		return BatchResult{}, err
	}
	if level == "user" {
		entry.CookieID = ""
		entry.ItemID = ""
	} else if level == "account" {
		entry.ItemID = ""
	}
	entry.Enabled = true
	entry.AutoClose = false
	entry.Reason = strings.TrimSpace(reason)
	if entry.Reason == "" {
		entry.Reason = "订单页一键拉黑（" + map[string]string{"user": "用户级", "account": "账号级", "item": "商品级"}[level] + "）"
	}
	return s.Import(ctx, userID, []buyerblock.Entry{entry})
}

// ExportCSV 导出当前用户账号筛选下的全部本地记录，表头兼容原系统，额外保留关单开关。
func (s *Service) ExportCSV(ctx context.Context, userID int64, accountID string) ([]byte, error) {
	// rows、err 经过与列表相同的用户归属校验。
	rows, err := s.List(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	// buffer 保存带 UTF-8 BOM 的 Excel 可读 CSV。
	buffer := bytes.NewBufferString("\xef\xbb\xbf")
	// writer 使用标准 CSV 转义，不拼接未可信单元格。
	writer := csv.NewWriter(buffer)
	_ = writer.Write([]string{"账号ID", "买家ID", "买家昵称", "商品ID", "拉黑原因", "是否启用", "命中后关单"})
	for _, row := range rows { // row 为当前用户拥有的一条记录。
		// cells 只包含用户可见配置，身份列不转换为数值。
		cells := []string{row.CookieID, row.BuyerID, row.BuyerNick, row.ItemID, row.Reason, map[bool]string{true: "是", false: "否"}[row.Enabled], map[bool]string{true: "是", false: "否"}[row.AutoClose]}
		for index, cell := range cells { // index、cell 定位需要防止表格公式注入的单元格。
			// trimmed 识别忽略前导空白后的表格公式前缀，避免 Excel 执行上传内容。
			trimmed := strings.TrimLeft(cell, " \t\r\n")
			if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				cells[index] = "'" + cell
			}
		}
		_ = writer.Write(cells)
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

// ErrPlatformUnavailable 表示逐会话平台能力尚未装配，不提供伪造的同步成功状态。
var ErrPlatformUnavailable = errors.New("闲鱼会话黑名单能力不可用")

// OfficialPlatform 每次调用最多对应一个平台请求，禁止遍历会话或后台自动刷新。
type OfficialPlatform interface {
	// Official 按账号、会话查询或改变平台拉黑，action 只能为 query/add/remove。
	Official(context.Context, int64, string, string, string) (bool, error)
}

// NewServiceWithPlatform 固定注入逐会话平台 Port，不影响旧的本地 CRUD 构造合同。
func NewServiceWithPlatform(repository Repository, platform OfficialPlatform) *Service {
	return &Service{repository: repository, platform: platform}
}

// Official 对 action 和用户身份校验后委托逐会话端口，不进行自动重试。
func (s *Service) Official(ctx context.Context, userID int64, accountID, chatID, action string) (bool, error) {
	if s == nil || userID <= 0 || accountID == "" || chatID == "" || (action != "query" && action != "add" && action != "remove") {
		return false, buyerblock.ErrInvalid
	}
	if s.platform == nil {
		return false, ErrPlatformUnavailable
	}
	return s.platform.Official(ctx, userID, accountID, chatID, action)
}
