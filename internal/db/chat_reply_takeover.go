package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrReplyPauseChanged 表示恢复期间出现了新的人工消息，旧页面不能清除新的暂停。
var ErrReplyPauseChanged = errors.New("会话暂停状态已变化，请刷新后重试")

// ManualReplyPauseMinutes 只读取账号的人工接管配置；旧账号默认关闭，不复用订单任务的暂停时长。
func (s *ChatStore) ManualReplyPauseMinutes(ctx context.Context, accountID string) (int, error) {
	// minutes 是独立于 cookies.pause_duration 的分钟数，不读取账号凭据。
	var minutes int
	// err 保留账号不存在和数据库故障，不能伪装成关闭配置。
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(p.manual_pause_minutes,0) FROM cookies c LEFT JOIN chat_reply_protection p ON p.cookie_id=c.id WHERE c.id=?`, accountID).Scan(&minutes)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return minutes, err
}

// SetManualReplyPauseMinutes 原子保存账号配置；关闭时同时结束该账号的会话接管，不影响订单自动任务。
func (s *ChatStore) SetManualReplyPauseMinutes(ctx context.Context, accountID string, minutes int) error {
	// tx、err 保证关闭配置与清除接管状态一起提交。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// err 只插入配置壳，随后更新在三方言中保持相同语义。
	if _, err := tx.ExecContext(ctx, dialectInsertIgnorePrefix(s.Dialect)+` INTO chat_reply_protection(cookie_id,manual_pause_minutes) VALUES(?,0)`+dialectInsertIgnore(s.Dialect, []string{"cookie_id"}), accountID); err != nil {
		return err
	}
	// err 保存独立分钟配置，不改变账号启用或账户级暂停。
	if _, err := tx.ExecContext(ctx, `UPDATE chat_reply_protection SET manual_pause_minutes=? WHERE cookie_id=?`, minutes, accountID); err != nil {
		return err
	}
	if minutes == 0 {
		// err 结束现有会话接管；事件去重账本保留，迟到回显不能重新暂停。
		if _, err := tx.ExecContext(ctx, `UPDATE chat_sessions SET reply_paused_until=0,reply_pause_version=reply_pause_version+1 WHERE cookie_id=?`, accountID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplyPausedUntil 返回会话暂停截止的 Unix 毫秒；尚未建立会话时自动回复沿用未暂停语义。
func (s *ChatStore) ReplyPausedUntil(ctx context.Context, accountID, chatID string) (int64, error) {
	// until、_,err 取同一快照中的截止时间，不向运行时暴露 HTTP 版本字段。
	until, _, err := s.ConversationReplyState(ctx, accountID, chatID)
	return until, err
}

// ConversationReplyState 在同一次读取中返回截止时间及版本，避免旧截止时间配上新版本而错误恢复。
func (s *ChatStore) ConversationReplyState(ctx context.Context, accountID, chatID string) (int64, int64, error) {
	// until 是会话暂停时间；version 为不依赖时钟的状态序号，二者从同一快照读取。
	var until, version int64
	// err 是纯本地读取结果；只有缺失会话可归一为零。
	err := s.DB.QueryRowContext(ctx, `SELECT reply_paused_until,reply_pause_version FROM chat_sessions WHERE cookie_id=? AND chat_id=?`, accountID, chatID).Scan(&until, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return until, version, err
}

// LastManualReplyAt 返回最近一次人工消息的本地接纳毫秒时间；恢复不删除水位，平台秒级时间不会让旧 AI 结果穿透。
func (s *ChatStore) LastManualReplyAt(ctx context.Context, accountID, chatID string) (int64, error) {
	// at 是人工事件水位，空集合返回零。
	var at int64
	// err 为纯本地查询故障，自动回复不能因故障而放行。
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(manual_at),0) FROM chat_manual_reply_events WHERE cookie_id=? AND chat_id=?`, accountID, chatID).Scan(&at)
	return at, err
}

// RecordManualReply 按稳定消息键去重，并基于实际发送时间延长指定会话；历史回灌不会重新开始计时。
func (s *ChatStore) RecordManualReply(ctx context.Context, accountID, chatID, messageKey string, sentAt, now int64) error {
	// tx、err 保护配置读取、消息去重和截止时间更新，不执行平台或文件 I/O。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// err 在读取设置前取得短写锁，与配置关闭串行；SQLite 避免读事务升级竞争，不执行外部 I/O。
	if _, err := tx.ExecContext(ctx, `UPDATE chat_reply_protection SET manual_pause_minutes=manual_pause_minutes WHERE cookie_id=?`, accountID); err != nil {
		return err
	}
	// minutes 为本事务观察到的接管配置；未配置保持旧行为。
	var minutes int64
	// err 读取配置，不从当前账号全局暂停字段推断。
	err = tx.QueryRowContext(ctx, `SELECT manual_pause_minutes FROM chat_reply_protection WHERE cookie_id=?`, accountID).Scan(&minutes)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && minutes == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	// until 从人工消息实际发生时间计算，旧消息即使重复同步也不会延长窗口。
	until := sentAt + minutes*60000
	if until <= now {
		return nil
	}
	// result、err 只领取尚未处理的稳定消息；manual_at 保存本地接纳水位，截止时间仍使用实际发送时间。
	result, err := tx.ExecContext(ctx, dialectInsertIgnorePrefix(s.Dialect)+` INTO chat_manual_reply_events(cookie_id,chat_id,message_key,manual_at) VALUES(?,?,?,?)`+dialectInsertIgnore(s.Dialect, []string{"cookie_id", "message_key"}), accountID, chatID, messageKey, now)
	if err != nil {
		return err
	}
	// count、err 区分首次人工消息和已处理的重放。
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	// err 仅延长此会话，不缩短另一个较新的人工接管窗口。
	if _, err := tx.ExecContext(ctx, `UPDATE chat_sessions SET reply_paused_until=CASE WHEN reply_paused_until<? THEN ? ELSE reply_paused_until END,reply_pause_version=reply_pause_version+1 WHERE cookie_id=? AND chat_id=?`, until, until, accountID, chatID); err != nil {
		return err
	}
	// err 惰性清理一天前的去重账本；更旧事件因实际时间已过期也无法重新触发。
	if _, err := tx.ExecContext(ctx, `DELETE FROM chat_manual_reply_events WHERE cookie_id=? AND manual_at<?`, accountID, now-86400000); err != nil {
		return err
	}
	return tx.Commit()
}

// ResumeReply 只恢复用户确认的状态版本；即使两条人工消息的截止时间相同也不会清除更新的接管。
func (s *ChatStore) ResumeReply(ctx context.Context, accountID, chatID string, expected int64) error {
	// result、err 是截止时间比较并交换的结果。
	result, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET reply_paused_until=0,reply_pause_version=reply_pause_version+1 WHERE cookie_id=? AND chat_id=? AND reply_pause_version=?`, accountID, chatID, expected)
	if err != nil {
		return err
	}
	// count、err 判断旧页面是否已被新消息淘汰。
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrReplyPauseChanged
	}
	return nil
}
