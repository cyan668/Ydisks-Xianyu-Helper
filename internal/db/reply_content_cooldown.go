package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrReplyContentClaimLost 表示旧回复已经失去领取权，绝不能继续发送或覆盖新的结果。
var ErrReplyContentClaimLost = errors.New("同内容回复领取权已变化")

// ReplyContentClaim 是单个入站内容处理周期的持久化领取证明，不包含消息正文或凭据。
type ReplyContentClaim struct {
	// ID、Version 共同防止重领、删除重建后旧处理器写回。
	ID, Version int64
}

// ReplyContentReview 只描述需人工核对的会话、平台消息标识与接纳时间，不返回正文摘要键。
type ReplyContentReview struct {
	// ID、Version 是明确解除保护时的比较条件。
	ID, Version int64
	// ChatID、MessageKey 帮助用户在自己的聊天记录中核对原消息。
	ChatID, MessageKey string
	// ClaimedAt 是本地接纳的 Unix 毫秒。
	ClaimedAt int64
}

// SameContentCooldownSeconds 读取账号独立的秒级重复内容抑制；旧账号默认关闭。
func (s *ChatStore) SameContentCooldownSeconds(ctx context.Context, accountID string) (int, error) {
	// seconds 是与人工接管分钟数独立的配置。
	var seconds int
	// err 读取非敏感账号配置，不能把查询故障当作默认关闭。
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(p.same_content_cooldown_seconds,0) FROM cookies c LEFT JOIN chat_reply_protection p ON p.cookie_id=c.id WHERE c.id=?`, accountID).Scan(&seconds)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return seconds, err
}

// SetSameContentCooldownSeconds 只更新重复内容秒数，关闭也不解除在途或结果未知的发送保护。
func (s *ChatStore) SetSameContentCooldownSeconds(ctx context.Context, accountID string, seconds int) error {
	// tx、err 只保证配置壳建立与更新原子化，不改变人工暂停或账号启用。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, dialectInsertIgnorePrefix(s.Dialect)+` INTO chat_reply_protection(cookie_id,manual_pause_minutes) VALUES(?,0)`+dialectInsertIgnore(s.Dialect, []string{"cookie_id"}), accountID); err != nil { // err 为独立配置壳的建立结果。
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE chat_reply_protection SET same_content_cooldown_seconds=? WHERE cookie_id=?`, seconds, accountID); err != nil { // err 不得导致覆盖另一项人工接管配置。
		return err
	}
	return tx.Commit()
}

// ClaimReplyContent 串行领取账号/会话/入站内容摘要，重复内容在生成前被拒绝；不持事务访问外部能力。
func (s *ChatStore) ClaimReplyContent(ctx context.Context, accountID, chatID, contentKey, messageKey string, now int64) (ReplyContentClaim, bool, error) {
	// tx、err 使同一账号的配置与领取在短事务内串行，SQLite 先取得写锁避免读升级竞争。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ReplyContentClaim{}, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE chat_reply_protection SET same_content_cooldown_seconds=same_content_cooldown_seconds WHERE cookie_id=?`, accountID); err != nil { // err 为本地配置行写锁的取得失败。
		return ReplyContentClaim{}, false, err
	}
	// seconds 为当前配置；没有保存过配置的旧账号按关闭处理。
	var seconds int64
	// err 查询当前秒数，数据库错误仍阻止处理。
	err = tx.QueryRowContext(ctx, `SELECT same_content_cooldown_seconds FROM chat_reply_protection WHERE cookie_id=?`, accountID).Scan(&seconds)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReplyContentClaim{}, false, err
	}
	// claim、status、leaseUntil、lastSent、started、uncertain 是当前内容的持久化状态。
	var claim ReplyContentClaim
	// status 是 running、cooldown 或 uncertain。
	var status string
	// leaseUntil、lastSent 为毫秒租约和最近成功发送时间。
	var leaseUntil, lastSent int64
	// started、uncertain 为发送前占用和未知结果事实。
	var started, uncertain int
	err = tx.QueryRowContext(ctx, `SELECT id,version,status,lease_until,last_sent_at,action_started,uncertain FROM reply_content_cooldowns WHERE cookie_id=? AND chat_id=? AND content_key=?`, accountID, chatID, contentKey).Scan(&claim.ID, &claim.Version, &status, &leaseUntil, &lastSent, &started, &uncertain)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReplyContentClaim{}, false, err
	}
	if err == nil {
		if status == "uncertain" || (status == "running" && leaseUntil > now) {
			return ReplyContentClaim{}, false, nil
		}
		if status == "running" && (started != 0 || uncertain != 0) {
			if _, err := tx.ExecContext(ctx, `UPDATE reply_content_cooldowns SET status='uncertain' WHERE id=? AND version=?`, claim.ID, claim.Version); err != nil { // err 将过期在途发送隔离，不自动重放。
				return ReplyContentClaim{}, false, err
			}
			return ReplyContentClaim{}, false, tx.Commit()
		}
		if seconds > 0 && lastSent > 0 && now-lastSent < seconds*1000 {
			return ReplyContentClaim{}, false, nil
		}
	}
	if seconds <= 0 {
		if claim.ID != 0 && status == "running" {
			if _, err := tx.ExecContext(ctx, `UPDATE reply_content_cooldowns SET status='cooldown',version=version+1 WHERE id=? AND version=?`, claim.ID, claim.Version); err != nil { // err 在关闭配置后使已过期的旧生成处理器失效。
				return ReplyContentClaim{}, false, err
			}
			return ReplyContentClaim{}, true, tx.Commit()
		}
		return ReplyContentClaim{}, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO reply_content_cooldowns(cookie_id,chat_id,content_key,message_key,version,status,claimed_at,lease_until) VALUES(?,?,?,?,1,'running',?,?)`, accountID, chatID, contentKey, messageKey, now, now+300000); err != nil { // err 建立首次内容领取；默认五分钟只用于生成期恢复。
			return ReplyContentClaim{}, false, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id,version FROM reply_content_cooldowns WHERE cookie_id=? AND chat_id=? AND content_key=?`, accountID, chatID, contentKey).Scan(&claim.ID, &claim.Version); err != nil { // err 读取三方言一致的领取身份。
			return ReplyContentClaim{}, false, err
		}
	} else {
		claim.Version++
		if _, err := tx.ExecContext(ctx, `UPDATE reply_content_cooldowns SET version=?,status='running',claimed_at=?,lease_until=?,last_sent_at=0,action_started=0,uncertain=0,message_key=? WHERE id=?`, claim.Version, now, now+300000, messageKey, claim.ID); err != nil { // err 以新代次重领，不改写旧处理器的证明。
			return ReplyContentClaim{}, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reply_content_cooldowns WHERE cookie_id=? AND status='cooldown' AND last_sent_at<?`, accountID, now-86400000); err != nil { // err 只惰性清理超过最大配置窗口的已确认记录。
		return ReplyContentClaim{}, false, err
	}
	return claim, true, tx.Commit()
}

// BeginReplyContentDelivery 在真正发送前持久化占用；崩溃后的占用只能进入人工核对，不能超时重发。
func (s *ChatStore) BeginReplyContentDelivery(ctx context.Context, claim ReplyContentClaim, now int64) error {
	// result、err 只允许当前代次且没有未知分段的处理器发出下一段。
	result, err := s.DB.ExecContext(ctx, `UPDATE reply_content_cooldowns SET action_started=1 WHERE id=? AND version=? AND status='running' AND action_started=0 AND uncertain=0 AND lease_until>?`, claim.ID, claim.Version, now)
	if err != nil {
		return err
	}
	// count、err 用来拒绝已经失效的处理器。
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrReplyContentClaimLost
	}
	return nil
}

// CheckReplyContentClaim 在首次回复之后、普通回复生成之前核对领取，未知首次投递不能继续生成另一条回复。
func (s *ChatStore) CheckReplyContentClaim(ctx context.Context, claim ReplyContentClaim, now int64) error {
	// valid 是当前代次仍可安全进入下一段生成或发送的本地事实。
	var valid int
	// err 不允许把读取失败当成可继续回复。
	err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM reply_content_cooldowns WHERE id=? AND version=? AND status='running' AND action_started=0 AND uncertain=0 AND lease_until>?`, claim.ID, claim.Version, now).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReplyContentClaimLost
	}
	return err
}

// ObserveReplyContentDelivery 保存实际分段结果；写入失败时保留 action_started，不能把已发送误释放。
func (s *ChatStore) ObserveReplyContentDelivery(ctx context.Context, claim ReplyContentClaim, sent, uncertain bool, now int64) error {
	// result、err 保留已发送时间和未知标记，后续确定失败不能抹掉之前的成功分段。
	result, err := s.DB.ExecContext(ctx, `UPDATE reply_content_cooldowns SET last_sent_at=CASE WHEN ?=1 THEN ? ELSE last_sent_at END,uncertain=CASE WHEN ?=1 THEN 1 ELSE uncertain END,action_started=0 WHERE id=? AND version=? AND status='running'`, boolToInt(sent), now, boolToInt(uncertain), claim.ID, claim.Version)
	if err != nil {
		return err
	}
	// count、err 防止旧代次覆盖新的领取或人工隔离。
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrReplyContentClaimLost
	}
	return nil
}

// FinishReplyContent 将整次入站处理收口；没发送的失败/无匹配释放，未知保护永不因时钟到期解除。
func (s *ChatStore) FinishReplyContent(ctx context.Context, claim ReplyContentClaim) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE reply_content_cooldowns SET status=CASE WHEN action_started=1 OR uncertain=1 THEN 'uncertain' WHEN last_sent_at>0 THEN 'cooldown' ELSE 'running' END WHERE id=? AND version=? AND status='running'`, claim.ID, claim.Version); err != nil { // err 为整次处理状态收口失败。
		return err
	}
	// err 只删除当前代次从未发出任何内容且没有未知结果的领取。
	_, err := s.DB.ExecContext(ctx, `DELETE FROM reply_content_cooldowns WHERE id=? AND version=? AND status='running' AND action_started=0 AND uncertain=0 AND last_sent_at=0`, claim.ID, claim.Version)
	return err
}

// ListReplyContentReviews 分页列出结果未知或发送占用已过期的记录；不读取正文、摘要键或账号凭据。
func (s *ChatStore) ListReplyContentReviews(ctx context.Context, accountID string, limit, offset int, now int64) ([]ReplyContentReview, int, error) {
	// predicate 同时覆盖正常未知结果和进程在发送后退出而未收口的占用。
	const predicate = `cookie_id=? AND (status='uncertain' OR (status='running' AND lease_until<=? AND (action_started=1 OR uncertain=1)))`
	// total 是账号下待核对记录总数，分页不隐藏剩余保护。
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM reply_content_cooldowns WHERE `+predicate, accountID, now).Scan(&total); err != nil { // err 为纯本地计数失败。
		return nil, 0, err
	}
	// rows、err 使用稳定主键排序；保护在途记录不能通过分页自动解除。
	rows, err := s.DB.QueryContext(ctx, `SELECT id,version,chat_id,message_key,claimed_at FROM reply_content_cooldowns WHERE `+predicate+` ORDER BY id LIMIT ? OFFSET ?`, accountID, now, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	// items 总是返回非空切片形态，HTTP 不需要把 null 解释为列表。
	items := make([]ReplyContentReview, 0)
	for rows.Next() {
		// item 只保存用户定位原消息所需的标识。
		var item ReplyContentReview
		if err := rows.Scan(&item.ID, &item.Version, &item.ChatID, &item.MessageKey, &item.ClaimedAt); err != nil { // err 为当前待核对行解码失败。
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// ResolveReplyContentReview 仅在用户核对平台后按账号、主键、版本解除未知保护，不补发旧消息。
func (s *ChatStore) ResolveReplyContentReview(ctx context.Context, accountID string, claim ReplyContentClaim, now int64) error {
	// result、err 不能删除别的账号、并发新代次或仍有效的发送占用。
	result, err := s.DB.ExecContext(ctx, `DELETE FROM reply_content_cooldowns WHERE cookie_id=? AND id=? AND version=? AND (status='uncertain' OR (status='running' AND lease_until<=? AND (action_started=1 OR uncertain=1)))`, accountID, claim.ID, claim.Version, now)
	if err != nil {
		return err
	}
	// count、err 区分确认解除和旧页面/重复提交冲突。
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrReplyContentClaimLost
	}
	return nil
}
