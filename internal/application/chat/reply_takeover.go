package chat

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrReplyPaused 表示当前会话由人工接管，只跳过自动客服回复，不影响交易自动化或人工发送。
var ErrReplyPaused = errors.New("当前会话由人工接管，自动回复已暂停")

// ErrReplyPauseChanged 表示恢复请求基于旧的暂停状态，必须重新读取，不能清除新的人工接管。
var ErrReplyPauseChanged = errors.New("会话暂停状态已变化，请刷新后重试")

// ReplyPauseState 是本地会话人工接管状态，时间戳为 Unix 毫秒。
type ReplyPauseState struct {
	// Version 在每条新人工消息、明确恢复或关闭配置时递增，不依赖时钟精度。
	Version int64
	// PausedUntil 是最后一次人工消息对应的截止时间；零表示已明确恢复。
	PausedUntil int64
	// Paused 表示读取时截止时间仍未到期。
	Paused bool
}

// ReplyTakeoverRepository 仅提供非敏感设置、接管事件去重与状态比较更新，不读取凭据或访问平台。
type ReplyTakeoverRepository interface {
	// ManualReplyPauseMinutes 读取账号独立的人工接管时长，默认关闭。
	ManualReplyPauseMinutes(context.Context, string) (int, error)
	// SetManualReplyPauseMinutes 保存配置，关闭时清除该账号已有接管。
	SetManualReplyPauseMinutes(context.Context, string, int) error
	// ReplyPausedUntil 读取指定会话的 Unix 毫秒截止时间。
	ReplyPausedUntil(context.Context, string, string) (int64, error)
	// ConversationReplyState 同一次读取截止时间与版本，防止恢复时使用混合快照。
	ConversationReplyState(context.Context, string, string) (int64, int64, error)
	// LastManualReplyAt 保留手动恢复之前的人工水位，拒绝接管前已经生成中的旧回复。
	LastManualReplyAt(context.Context, string, string) (int64, error)
	// RecordManualReply 根据可信发送时间和稳定键原子去重并更新截止时间。
	RecordManualReply(context.Context, string, string, string, int64, int64) error
	// ResumeReply 比较旧状态版本后恢复，不能覆盖并发新人工消息。
	ResumeReply(context.Context, string, string, int64) error
}

// automaticReplyContextKey 只在内部完整回复路径设置，HTTP 请求不能声明自己是自动消息。
type automaticReplyContextKey struct{}

// automaticReplyStartedKey 保存触发自动回复的买家消息接纳时间，不接受 HTTP 传入。
type automaticReplyStartedKey struct{}

// WithAutomaticReply 标记内部自动客服回复，复用人工发送管线时仍保留来源和会话暂停门禁。
func WithAutomaticReply(ctx context.Context) context.Context {
	return WithAutomaticReplyAt(ctx, time.Now().UnixMilli())
}

// WithAutomaticReplyAt 在内部发送管线保留原买家消息的接纳时间，避免手动恢复后发送旧生成结果。
func WithAutomaticReplyAt(ctx context.Context, observedAt int64) context.Context {
	if observedAt <= 0 {
		observedAt = time.Now().UnixMilli()
	}
	return context.WithValue(context.WithValue(ctx, automaticReplyContextKey{}, true), automaticReplyStartedKey{}, observedAt)
}

// IsAutomaticReply 返回调用链是否来自内部自动客服；缺省保持旧人工发送合同。
func IsAutomaticReply(ctx context.Context) bool {
	// automatic 为内部布尔标记，不携带消息正文或凭据。
	automatic, _ := ctx.Value(automaticReplyContextKey{}).(bool)
	return automatic
}

// takeoverRepository 在旧的轻量构造下保持发送可用；生产数据库适配器固定提供扩展。
func (s *Service) takeoverRepository() ReplyTakeoverRepository {
	if s == nil {
		return nil
	}
	// repository 只在已装配的聊天仓储上读取窄能力。
	repository, _ := s.repository.(ReplyTakeoverRepository)
	return repository
}

// ReplyProtectionMinutes 核验账号归属后读取人工接管设置，不改变启用或订单暂停状态。
func (s *Service) ReplyProtectionMinutes(ctx context.Context, userID int64, accountID string) (int, error) {
	if err := s.validateReplyProtectionOwner(ctx, userID, accountID); err != nil { // err 是非敏感账号归属查询结果。
		return 0, err
	}
	return s.takeoverRepository().ManualReplyPauseMinutes(ctx, strings.TrimSpace(accountID))
}

// SaveReplyProtectionMinutes 只配置以后人工发送的接管时长，正数设置本身不会开始暂停。
func (s *Service) SaveReplyProtectionMinutes(ctx context.Context, userID int64, accountID string, minutes int) error {
	if minutes < 0 || minutes > 1440 {
		return ErrInvalidInput
	}
	if err := s.validateReplyProtectionOwner(ctx, userID, accountID); err != nil { // err 表示账号归属或仓储不可用。
		return err
	}
	return s.takeoverRepository().SetManualReplyPauseMinutes(ctx, strings.TrimSpace(accountID), minutes)
}

// validateReplyProtectionOwner 在设置读写前核验非敏感账号归属。
func (s *Service) validateReplyProtectionOwner(ctx context.Context, userID int64, accountID string) error {
	if userID <= 0 || strings.TrimSpace(accountID) == "" {
		return ErrInvalidInput
	}
	if s.takeoverRepository() == nil {
		return ErrSessionUnavailable
	}
	// owned、err 只返回账号存在性，不能读取 Cookie。
	owned, err := s.OwnsAccount(ctx, userID, strings.TrimSpace(accountID))
	if err != nil {
		return err
	}
	if !owned {
		return ErrSessionForbidden
	}
	return nil
}

// ConversationReplyPause 核验可见会话归属后读取当前接管状态。
func (s *Service) ConversationReplyPause(ctx context.Context, userID int64, accountID, chatID string) (ReplyPauseState, error) {
	if strings.TrimSpace(chatID) == "" {
		return ReplyPauseState{}, ErrInvalidInput
	}
	if err := s.validateReplyProtectionOwner(ctx, userID, accountID); err != nil { // err 为账号归属或依赖错误。
		return ReplyPauseState{}, err
	}
	if _, err := s.lookupOwnedSession(ctx, userID, strings.TrimSpace(accountID), strings.TrimSpace(chatID)); err != nil { // err 保留会话不存在与越权语义。
		return ReplyPauseState{}, err
	}
	// until、version、err 是同一快照的截止、版本及故障，不能拼接不同时间的状态。
	until, version, err := s.takeoverRepository().ConversationReplyState(ctx, strings.TrimSpace(accountID), strings.TrimSpace(chatID))
	return ReplyPauseState{Version: version, PausedUntil: until, Paused: until > time.Now().UnixMilli()}, err
}

// ResumeConversationReply 以用户看到的状态版本作比较，同一时钟刻度的新消息也能防止旧恢复覆盖。
func (s *Service) ResumeConversationReply(ctx context.Context, userID int64, accountID, chatID string, expected int64) error {
	if expected < 0 {
		return ErrInvalidInput
	}
	if _, err := s.ConversationReplyPause(ctx, userID, accountID, chatID); err != nil { // err 在写入前核验完整归属。
		return err
	}
	return s.takeoverRepository().ResumeReply(ctx, strings.TrimSpace(accountID), strings.TrimSpace(chatID), expected)
}

// ObserveManualReply 仅供可信本地发送/跨端分类调用；未知来源、旧历史和缺少稳定消息键不触发接管。
func (s *Service) ObserveManualReply(ctx context.Context, accountID, chatID, key string, sentAt int64) error {
	if s.takeoverRepository() == nil {
		return nil
	}
	// now 是当前毫秒时间；未来时间异常和缺少实际发送时间都不能开始新的暂停。
	now := time.Now().UnixMilli()
	accountID, chatID, key = strings.TrimSpace(accountID), strings.TrimSpace(chatID), strings.TrimSpace(key)
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(chatID) == "" || strings.TrimSpace(key) == "" || len(key) > 255 || sentAt <= 0 || sentAt > now+60000 {
		return ErrInvalidInput
	}
	return s.takeoverRepository().RecordManualReply(ctx, accountID, chatID, key, sentAt, now)
}

// checkAutomaticReply 在每个发送分段前核验暂停，阻止 AI 生成期间到达的人工接管被旧结果穿透。
func (s *Service) checkAutomaticReply(ctx context.Context, session Session) error {
	if !IsAutomaticReply(ctx) || s.takeoverRepository() == nil {
		return nil
	}
	// until、err 是发送前最新状态；读取故障直接阻止自动发送。
	until, err := s.takeoverRepository().ReplyPausedUntil(ctx, session.AccountID, session.ChatID)
	if err != nil {
		return err
	}
	if until > time.Now().UnixMilli() {
		return ErrReplyPaused
	}
	// started 是原买家消息的接纳水位；lastManual、err 为其之后是否发生人工接管的本地证据。
	started, _ := ctx.Value(automaticReplyStartedKey{}).(int64)
	// lastManual、err 保留显式恢复前的事件水位，不能把恢复视为补发旧回复的授权。
	lastManual, err := s.takeoverRepository().LastManualReplyAt(ctx, session.AccountID, session.ChatID)
	if err != nil {
		return err
	}
	if started > 0 && lastManual >= started {
		return ErrReplyPaused
	}
	return nil
}

// recordManualSend 在人工消息已发送或结果不确定时持久化接管；明确未发送的失败由调用方排除。
func (s *Service) recordManualSend(ctx context.Context, session Session, message Message) error {
	if IsAutomaticReply(ctx) || s.takeoverRepository() == nil {
		return nil
	}
	// persistCtx、cancel 保留发送后的收口预算，不受客户端断开取消。
	persistCtx, cancel := outgoingStatusContext(ctx)
	defer cancel()
	return s.ObserveManualReply(persistCtx, session.AccountID, session.ChatID, message.MessageKey, time.Now().UnixMilli())
}
