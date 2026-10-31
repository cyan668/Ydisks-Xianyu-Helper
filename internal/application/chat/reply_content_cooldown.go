package chat

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrContentReviewChanged 表示待核对保护已变化，旧页面不能解除新的领取。
var ErrContentReviewChanged = errors.New("重复消息保护已变化，请刷新后重试")

// ContentCooldownReview 描述本人账号的一条未知回复保护，不携带消息正文或内容摘要。
type ContentCooldownReview struct {
	// ID、Version 共同定位用户明确确认的旧状态。
	ID, Version int64
	// ChatID、MessageKey 帮助从本人聊天记录核对发送结果。
	ChatID, MessageKey string
	// ClaimedAt 为本地接纳消息的 Unix 毫秒。
	ClaimedAt int64
}

// ContentCooldownPage 是本地待核对列表的有界分页，不执行平台查询。
type ContentCooldownPage struct {
	// Items 只保存当前页的非敏感定位信息。
	Items []ContentCooldownReview
	// Total、Limit、Offset 分别为总记录数、页长和跳过数量。
	Total, Limit, Offset int
}

// ContentCooldownRepository 是独立于人工接管和旧聊天构造的窄能力，不读取平台凭据。
type ContentCooldownRepository interface {
	// SameContentCooldownSeconds 返回独立秒级设置，默认零。
	SameContentCooldownSeconds(context.Context, string) (int, error)
	// SetSameContentCooldownSeconds 仅保存冷却秒数，不解除未知保护。
	SetSameContentCooldownSeconds(context.Context, string, int) error
	// ListContentCooldownReviews 返回当前账号需用户核对的分页快照。
	ListContentCooldownReviews(context.Context, string, int, int, int64) ([]ContentCooldownReview, int, error)
	// ResolveContentCooldownReview 比较账号和状态版本后解除，绝不重放原消息。
	ResolveContentCooldownReview(context.Context, string, int64, int64, int64) error
}

// contentCooldownRepository 保留旧的轻量聊天端口合同，生产适配器固定提供此能力。
func (s *Service) contentCooldownRepository() ContentCooldownRepository {
	if s == nil {
		return nil
	}
	// repository 是仅供本地保护管理使用的扩展。
	repository, _ := s.repository.(ContentCooldownRepository)
	return repository
}

// validateContentCooldownOwner 用账号存在性校验隔离当前用户，不能为归属检查解密凭据。
func (s *Service) validateContentCooldownOwner(ctx context.Context, userID int64, accountID string) error {
	if userID <= 0 || strings.TrimSpace(accountID) == "" {
		return ErrInvalidInput
	}
	if s.contentCooldownRepository() == nil {
		return ErrSessionUnavailable
	}
	// owned、err 为不含秘密的账号归属事实和查询故障。
	owned, err := s.OwnsAccount(ctx, userID, strings.TrimSpace(accountID))
	if err != nil {
		return err
	}
	if !owned {
		return ErrSessionForbidden
	}
	return nil
}

// ContentCooldownSeconds 在账号归属核验后读取独立秒数，停用账号也可配置。
func (s *Service) ContentCooldownSeconds(ctx context.Context, userID int64, accountID string) (int, error) {
	if err := s.validateContentCooldownOwner(ctx, userID, accountID); err != nil { // err 是归属或扩展不可用故障。
		return 0, err
	}
	return s.contentCooldownRepository().SameContentCooldownSeconds(ctx, strings.TrimSpace(accountID))
}

// SaveContentCooldownSeconds 保存 0 到 86400 秒，零关闭新冷却但保留未知投递保护。
func (s *Service) SaveContentCooldownSeconds(ctx context.Context, userID int64, accountID string, seconds int) error {
	if seconds < 0 || seconds > 86400 {
		return ErrInvalidInput
	}
	if err := s.validateContentCooldownOwner(ctx, userID, accountID); err != nil { // err 是当前账号归属故障。
		return err
	}
	return s.contentCooldownRepository().SetSameContentCooldownSeconds(ctx, strings.TrimSpace(accountID), seconds)
}

// ContentCooldownReviews 只分页读取当前用户明确选中账号的保护，无后台扫描或平台请求。
func (s *Service) ContentCooldownReviews(ctx context.Context, userID int64, accountID string, limit, offset int) (ContentCooldownPage, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return ContentCooldownPage{}, ErrInvalidInput
	}
	if err := s.validateContentCooldownOwner(ctx, userID, accountID); err != nil { // err 保留账号隔离和数据库故障。
		return ContentCooldownPage{}, err
	}
	// items、total、err 是同一次列表操作的本地分页，不包含正文或内容键。
	items, total, err := s.contentCooldownRepository().ListContentCooldownReviews(ctx, strings.TrimSpace(accountID), limit, offset, time.Now().UnixMilli())
	return ContentCooldownPage{Items: items, Total: total, Limit: limit, Offset: offset}, err
}

// ResolveContentCooldown 只在用户明确完成平台核对后解除旧状态，后续新买家消息才重新参与回复。
func (s *Service) ResolveContentCooldown(ctx context.Context, userID int64, accountID string, id, version int64, confirmed bool) error {
	if id <= 0 || version <= 0 || !confirmed {
		return ErrInvalidInput
	}
	if err := s.validateContentCooldownOwner(ctx, userID, accountID); err != nil { // err 是解除前的归属检查。
		return err
	}
	return s.contentCooldownRepository().ResolveContentCooldownReview(ctx, strings.TrimSpace(accountID), id, version, time.Now().UnixMilli())
}
