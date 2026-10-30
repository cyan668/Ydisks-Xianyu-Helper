// Package buyerblock 编排交易黑名单输入与账号商品归属校验，不依赖 HTTP 或数据库。
package buyerblock

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"xianyu-go/internal/buyerblock"
)

// Repository 是黑名单 CRUD 的最小持久化与非敏感归属能力。
type Repository interface {
	// List 按本地用户与可选账号读取记录。
	List(ctx context.Context, userID int64, accountID string) ([]buyerblock.Entry, error)
	// Save 按记录 UserID 原子创建或更新。
	Save(ctx context.Context, entry buyerblock.Entry) (int64, error)
	// Delete 仅删除指定用户的主键。
	Delete(ctx context.Context, userID, id int64) error
	// OwnsScope 核验账号及可选商品属于 userID，不读取凭据。
	OwnsScope(ctx context.Context, userID int64, accountID, itemID string) (bool, error)
}

// Service 在构造时固定 repository，无自有并发状态。
type Service struct {
	// repository 由组合根提供并管理数据库生命周期。
	repository Repository
}

// NewService 创建使用 repository 的黑名单应用服务。
func NewService(repository Repository) *Service { return &Service{repository: repository} }

// List 校验 userID 与可选 accountID 后返回可见记录。
func (s *Service) List(ctx context.Context, userID int64, accountID string) ([]buyerblock.Entry, error) {
	if userID <= 0 || s == nil || s.repository == nil {
		return nil, buyerblock.ErrInvalid
	}
	if accountID != "" {
		// owned、err 表示当前用户是否拥有筛选账号。
		owned, err := s.repository.OwnsScope(ctx, userID, accountID, "")
		if err != nil {
			return nil, err
		}
		if !owned {
			return nil, buyerblock.ErrNotFound
		}
	}
	return s.repository.List(ctx, userID, accountID)
}

// Save 规范化 entry 并验证其新作用域；userID 来自认证上下文，不能由请求指定。
func (s *Service) Save(ctx context.Context, userID int64, entry buyerblock.Entry) (buyerblock.Entry, error) {
	if userID <= 0 || s == nil || s.repository == nil || entry.ID < 0 {
		return buyerblock.Entry{}, buyerblock.ErrInvalid
	}
	entry.UserID = userID
	entry.CookieID, entry.ItemID, entry.Reason = strings.TrimSpace(entry.CookieID), strings.TrimSpace(entry.ItemID), strings.TrimSpace(entry.Reason)
	// normalized、err 核验稳定买家身份，不接受可变昵称作为替代。
	normalized, err := buyerblock.NormalizeBuyerID(entry.BuyerID)
	if err != nil || len(entry.CookieID) > 191 || len(entry.ItemID) > 191 || utf8.RuneCountInString(entry.Reason) > 500 || (entry.ItemID != "" && entry.CookieID == "") {
		return buyerblock.Entry{}, buyerblock.ErrInvalid
	}
	entry.BuyerID = normalized
	if entry.CookieID != "" {
		// owned、scopeErr 核验账号与商品属于当前用户。
		owned, scopeErr := s.repository.OwnsScope(ctx, userID, entry.CookieID, entry.ItemID)
		if scopeErr != nil {
			return buyerblock.Entry{}, scopeErr
		}
		if !owned {
			return buyerblock.Entry{}, buyerblock.ErrNotFound
		}
	}
	entry.ID, err = s.repository.Save(ctx, entry)
	return entry, err
}

// Delete 按 userID 删除 id；不存在或越权统一返回记录不存在。
func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	if userID <= 0 || id <= 0 || s == nil || s.repository == nil {
		return fmt.Errorf("%w: 记录标识无效", buyerblock.ErrInvalid)
	}
	return s.repository.Delete(ctx, userID, id)
}
