package db

import (
	"context"
	"database/sql"
	"errors"

	"xianyu-go/internal/buyerblock"
)

// BuyerBlocks 保存交易黑名单和每单唯一关单占用；没有缓存或自有 goroutine。
type BuyerBlocks struct {
	// DB 由 Store 持有和关闭，全部查询只访问本地数据。
	DB *sql.DB
	// Dialect 决定自增主键及插入去重语法。
	Dialect Dialect
}

// List 返回 userID 的记录；accountID 非空时同时显示对此账号生效的用户级记录。
func (b *BuyerBlocks) List(ctx context.Context, userID int64, accountID string) ([]buyerblock.Entry, error) {
	// rows、err 保存用户隔离查询的结果和错误。
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,cookie_id,item_id,buyer_id,reason,enabled,auto_close FROM buyer_blocks WHERE user_id=? AND (?='' OR cookie_id='' OR cookie_id=?) ORDER BY id DESC`, userID, accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// entries 为空时仍返回非 nil 切片。
	entries := make([]buyerblock.Entry, 0)
	for rows.Next() {
		// entry 是当前非敏感规则记录。
		var entry buyerblock.Entry
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.CookieID, &entry.ItemID, &entry.BuyerID, &entry.Reason, &entry.Enabled, &entry.AutoClose); err != nil { // err 为扫描失败原因。
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Save 创建或更新 entry；更新必须匹配 UserID，禁止覆盖其他用户记录。
func (b *BuyerBlocks) Save(ctx context.Context, entry buyerblock.Entry) (int64, error) {
	// id、err 为最终主键和写入错误。
	id, err := entry.ID, error(nil)
	if entry.ID == 0 {
		id, err = insertReturningID(ctx, b.DB, b.Dialect, `INSERT INTO buyer_blocks(user_id,cookie_id,item_id,buyer_id,reason,enabled,auto_close) VALUES(?,?,?,?,?,?,?)`, entry.UserID, entry.CookieID, entry.ItemID, entry.BuyerID, entry.Reason, boolToInt(entry.Enabled), boolToInt(entry.AutoClose))
	} else {
		// result 是按用户限定的更新结果。
		var result sql.Result
		result, err = b.DB.ExecContext(ctx, `UPDATE buyer_blocks SET cookie_id=?,item_id=?,buyer_id=?,reason=?,enabled=?,auto_close=? WHERE id=? AND user_id=?`, entry.CookieID, entry.ItemID, entry.BuyerID, entry.Reason, boolToInt(entry.Enabled), boolToInt(entry.AutoClose), entry.ID, entry.UserID)
		if err == nil {
			// affected、countErr 区分写入失败和未修改记录。
			affected, countErr := result.RowsAffected()
			if countErr != nil {
				return 0, countErr
			}
			if affected == 0 {
				// exists 区分 MySQL 未改值和不存在的记录。
				var exists int
				// lookupErr 是当前用户记录存在性读取错误。
				if lookupErr := b.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM buyer_blocks WHERE id=? AND user_id=?`, entry.ID, entry.UserID).Scan(&exists); lookupErr != nil {
					return 0, lookupErr
				}
				if exists == 0 {
					return 0, buyerblock.ErrNotFound
				}
			}
		}
	}
	if err != nil && isUniqueViolation(err) {
		return 0, buyerblock.ErrConflict
	}
	return id, err
}

// Delete 只删除 userID 拥有的 id；独立关单占用不删除，规则重建不能重新提交同一订单。
func (b *BuyerBlocks) Delete(ctx context.Context, userID, id int64) error {
	// result、err 为隔离删除的结果。
	result, err := b.DB.ExecContext(ctx, `DELETE FROM buyer_blocks WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	// affected、err 判断是否找到当前用户的记录。
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return buyerblock.ErrNotFound
	}
	return err
}

// MatchOrder 用 accountID 的当前所有者和订单身份匹配；缺事实时只在存在相关规则时拒绝，未配置账号保持历史合同。
func (b *BuyerBlocks) MatchOrder(ctx context.Context, accountID, orderID, buyerID, itemID string) (*buyerblock.Entry, error) {
	if orderID != "" {
		// closeStatus 即使删除黑名单规则也保留订单关单结果，避免结果未知时改规则后继续发货。
		var closeStatus string
		// closeErr 为同账号订单关单记录查询结果。
		closeErr := b.DB.QueryRowContext(ctx, `SELECT status FROM buyer_block_closes WHERE cookie_id=? AND order_id=?`, accountID, orderID).Scan(&closeStatus)
		if closeErr != nil && !errors.Is(closeErr, sql.ErrNoRows) {
			return nil, closeErr
		}
		if closeStatus == "succeeded" {
			return nil, buyerblock.ErrClosed
		}
		if closeStatus == "started" || closeStatus == "unknown" {
			return nil, buyerblock.ErrCloseUnknown
		}
	}
	if orderID != "" && (buyerID == "" || itemID == "") {
		// configured 避免给未配置黑名单的旧调用新增订单事实前置条件。
		var configured int
		// configuredErr 只读取当前账号所有者的启用规则数量。
		configuredErr := b.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM buyer_blocks b JOIN cookies c ON c.user_id=b.user_id WHERE c.id=? AND b.enabled=1 AND (b.cookie_id='' OR b.cookie_id=c.id)`, accountID).Scan(&configured)
		if configuredErr != nil {
			return nil, configuredErr
		}
		if configured == 0 {
			return nil, nil
		}
		// storedBuyer、storedItem 是同账号订单已经保存的身份，不触发详情请求。
		var storedBuyer, storedItem string
		// lookupErr 表示本地订单查询结果，订单缺失仍继续保守核验。
		lookupErr := b.DB.QueryRowContext(ctx, `SELECT COALESCE(buyer_id,''),COALESCE(item_id,'') FROM orders WHERE cookie_id=? AND order_id=?`, accountID, orderID).Scan(&storedBuyer, &storedItem)
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return nil, lookupErr
		}
		if buyerID == "" {
			buyerID = storedBuyer
		}
		if itemID == "" {
			itemID = storedItem
		}
	}
	// normalized、idErr 核验稳定买家身份，不能按昵称猜测放行。
	normalized, idErr := buyerblock.NormalizeBuyerID(buyerID)
	if idErr != nil {
		normalized = ""
	}
	// entry 保存最具体的一条可能命中，查询只连接账号所有者字段。
	var entry buyerblock.Entry
	// err 为查询结果，缺身份时查询所有可能规则以防静默放行。
	err := b.DB.QueryRowContext(ctx, `SELECT b.id,b.user_id,b.cookie_id,b.item_id,b.buyer_id,b.reason,b.enabled,b.auto_close FROM buyer_blocks b JOIN cookies c ON c.user_id=b.user_id WHERE c.id=? AND b.enabled=1 AND (b.cookie_id='' OR b.cookie_id=c.id) AND (b.item_id='' OR b.item_id=? OR ?='') AND (b.buyer_id=? OR ?='') ORDER BY CASE WHEN b.item_id<>'' THEN 2 WHEN b.cookie_id<>'' THEN 1 ELSE 0 END DESC,b.id ASC LIMIT 1`, accountID, itemID, itemID, normalized, normalized).Scan(&entry.ID, &entry.UserID, &entry.CookieID, &entry.ItemID, &entry.BuyerID, &entry.Reason, &entry.Enabled, &entry.AutoClose)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if normalized == "" || (itemID == "" && entry.ItemID != "") {
		return nil, buyerblock.ErrIdentityMissing
	}
	return &entry, nil
}

// ClaimClose 在平台请求前取得 accountID/orderID 的永久唯一占用，重复事件及重启不能再次提交。
func (b *BuyerBlocks) ClaimClose(ctx context.Context, accountID, orderID string, blockID int64) (bool, error) {
	// result、err 在同一语句中复核未发货订单、无活动/已发送运行并领取永久占用；重复键不修改旧结果。
	result, err := b.DB.ExecContext(ctx, dialectInsertIgnorePrefix(b.Dialect)+` INTO buyer_block_closes(cookie_id,order_id,block_id,status)
	 SELECT ?,?,?,'started' WHERE EXISTS(SELECT 1 FROM orders WHERE cookie_id=? AND order_id=? AND order_status IN ('processing','1','pending_ship','paid','2','pending_delivery'))
	 AND NOT EXISTS(SELECT 1 FROM automation_runs WHERE cookie_id=? AND order_id=? AND (status IN ('running','needs_review') OR sent_count>0 OR action_started=1))`+dialectInsertIgnore(b.Dialect, []string{"cookie_id", "order_id"}), accountID, orderID, blockID, accountID, orderID, accountID, orderID)
	if err != nil {
		return false, err
	}
	// affected、err 判断本调用是否首次领取。
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// FinishClose 保存 status，只收口 started 占用，不提供自动重置或重试入口。
func (b *BuyerBlocks) FinishClose(ctx context.Context, accountID, orderID, status string) error {
	if status != "succeeded" && status != "failed" && status != "unknown" {
		return buyerblock.ErrInvalid
	}
	// tx、err 原子收口关单状态和本地未发货订单状态；提交失败保留原占用。
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// result、writeErr 保存唯一关单状态的条件写入；不存在占用时不得修改订单。
	result, writeErr := tx.ExecContext(ctx, `UPDATE buyer_block_closes SET status=? WHERE cookie_id=? AND order_id=? AND status='started'`, status, accountID, orderID)
	if writeErr != nil {
		return writeErr
	}
	// changed、countErr 核验本调用拥有 started 占用的收口权。
	changed, countErr := result.RowsAffected()
	if countErr != nil {
		return countErr
	}
	if changed != 1 {
		return buyerblock.ErrCloseUnknown
	}
	// stateConflict 标记平台已成功但本地订单无法安全收口，仍保留明确成功的占用并通知人工。
	stateConflict := false
	if status == "succeeded" {
		result, writeErr = tx.ExecContext(ctx, `UPDATE orders SET order_status='cancelled',version=version+1 WHERE cookie_id=? AND order_id=? AND order_status IN ('processing','1','pending_ship','paid','2','pending_delivery')`, accountID, orderID)
		if writeErr != nil {
			return writeErr
		}
		changed, countErr = result.RowsAffected()
		if countErr != nil {
			return countErr
		}
		if changed != 1 {
			// currentStatus 区分重复的已关闭事实和需要人工核对的并发终态。
			var currentStatus string
			// lookupErr 不泄漏底层 SQL，缺失订单同样保留人工核对状态。
			lookupErr := tx.QueryRowContext(ctx, `SELECT COALESCE(order_status,'') FROM orders WHERE cookie_id=? AND order_id=?`, accountID, orderID).Scan(&currentStatus)
			if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
				return lookupErr
			}
			stateConflict = NormalizeOrderStatus(currentStatus) != "cancelled"
		}
	}
	// err 表示结果尚未可靠写入，调用方必须人工核对。
	if err := tx.Commit(); err != nil {
		return err
	}
	if stateConflict {
		return buyerblock.ErrCloseStateConflict
	}
	return nil
}
