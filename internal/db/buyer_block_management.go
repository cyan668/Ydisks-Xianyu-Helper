package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"xianyu-go/internal/buyerblock"
)

// ListPage 为 userID 按账号和 search 查询数据库分页，不读取账号凭据。
func (b *BuyerBlocks) ListPage(ctx context.Context, userID int64, accountID, search string, page, size int) ([]buyerblock.Entry, int, error) {
	// condition、args 是固定 SQL 与绑定参数，搜索词不能变成 SQL 片段。
	condition := ` FROM buyer_blocks WHERE user_id=? AND (?='' OR cookie_id='' OR cookie_id=?) AND (?='' OR buyer_id LIKE ? OR buyer_nick LIKE ? OR reason LIKE ?)`
	// args 为账号作用域和文本匹配条件。
	args := []any{userID, accountID, accountID, search, "%" + search + "%", "%" + search + "%", "%" + search + "%"}
	// total 是当前筛选的完整记录数。
	var total int
	// err 为当前用户筛选计数查询失败，不能返回不完整总数。
	if err := b.DB.QueryRowContext(ctx, `SELECT COUNT(*)`+condition, args...).Scan(&total); err != nil {
		return nil, 0, err
	} // err 为计数查询失败。
	// rows、err 为单页结果，页码已由应用服务限制。
	rows, err := b.DB.QueryContext(ctx, `SELECT id,user_id,cookie_id,item_id,buyer_id,buyer_nick,reason,enabled,auto_close`+condition+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	// entries 即使页为空也返回数组。
	entries := make([]buyerblock.Entry, 0)
	for rows.Next() { // 当前行仅包含用户可见配置。
		// entry 是本页当前黑名单记录。
		var entry buyerblock.Entry
		// err 为本页配置行解码失败，不能忽略损坏数据。
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.CookieID, &entry.ItemID, &entry.BuyerID, &entry.BuyerNick, &entry.Reason, &entry.Enabled, &entry.AutoClose); err != nil {
			return nil, 0, err
		} // err 为行解码失败。
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}

// Import 对同一用户已校验的 entries 在单个事务内插入，重复作用域不覆盖现有配置。
func (b *BuyerBlocks) Import(ctx context.Context, userID int64, entries []buyerblock.Entry) (int, int, error) {
	// tx、err 使导入要么完整提交，要么不留下部分记录。
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	// created、skipped 分别统计本次新增和唯一键重复数。
	created, skipped := 0, 0
	for _, entry := range entries { // entry 已在应用层完成用户及作用域校验。
		// result、writeErr 只忽略四元组唯一键重复，不更新任何旧记录。
		result, writeErr := tx.ExecContext(ctx, dialectInsertIgnorePrefix(b.Dialect)+` INTO buyer_blocks(user_id,cookie_id,item_id,buyer_id,buyer_nick,reason,enabled,auto_close) VALUES(?,?,?,?,?,?,?,?)`+dialectInsertIgnore(b.Dialect, []string{"user_id", "cookie_id", "item_id", "buyer_id"}), userID, entry.CookieID, entry.ItemID, entry.BuyerID, entry.BuyerNick, entry.Reason, boolToInt(entry.Enabled), boolToInt(entry.AutoClose))
		if writeErr != nil {
			return 0, 0, writeErr
		}
		// affected、countErr 区分新增和保留原配置。
		affected, countErr := result.RowsAffected()
		if countErr != nil {
			return 0, 0, countErr
		}
		if affected == 1 {
			created++
		} else {
			skipped++
		}
	}
	return created, skipped, tx.Commit()
}

// Batch 在单个事务内对 userID 的完整 ids 集合操作；混入越权主键会整体拒绝。
func (b *BuyerBlocks) Batch(ctx context.Context, userID int64, ids []int64, action string) (int, error) {
	if len(ids) == 0 {
		return 0, buyerblock.ErrInvalid
	}
	// tx、err 持有本地短事务，不进行平台或文件 I/O。
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// placeholders、args 只由经过校验的整数主键组成，值仍通过参数绑定。
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	// args 先放用户身份，再放主键。
	args := []any{userID}
	// id 是当前明确选择的记录主键，只作参数绑定。
	for _, id := range ids {
		args = append(args, id)
	} // id 是当前已校验主键。
	// count 用于防止部分越权时假报成功。
	var count int
	// err 为完整主键集合的归属查询失败，事务不得继续。
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM buyer_blocks WHERE user_id=? AND id IN (`+placeholders+`)`, args...).Scan(&count); err != nil {
		return 0, err
	} // err 是集合归属查询错误。
	if count != len(ids) {
		return 0, buyerblock.ErrNotFound
	}
	// statement 只从受控操作枚举选择 SQL，不接受浏览器 SQL 片段。
	statement := `DELETE FROM buyer_blocks`
	if action == "enable" {
		statement = `UPDATE buyer_blocks SET enabled=1`
	} else if action == "disable" {
		statement = `UPDATE buyer_blocks SET enabled=0`
	} else if action != "delete" {
		return 0, buyerblock.ErrInvalid
	}
	// err 为批量状态或删除写入失败，整体回滚。
	if _, err := tx.ExecContext(ctx, statement+` WHERE user_id=? AND id IN (`+placeholders+`)`, args...); err != nil {
		return 0, err
	} // err 为本地批量写入错误。
	return count, tx.Commit()
}

// OrderBuyer 返回当前用户订单对应的稳定买家及账号/商品；不存在与越权统一拒绝。
func (b *BuyerBlocks) OrderBuyer(ctx context.Context, userID int64, orderID string) (buyerblock.Entry, error) {
	// entry 只包含非敏感订单身份，不读取或序列化 Cookie。
	entry := buyerblock.Entry{UserID: userID}
	// err 为带账号归属的本地查询结果。
	err := b.DB.QueryRowContext(ctx, `SELECT o.cookie_id,COALESCE(o.item_id,''),COALESCE(o.buyer_id,'') FROM orders o JOIN cookies c ON c.id=o.cookie_id WHERE o.order_id=? AND c.user_id=?`, orderID, userID).Scan(&entry.CookieID, &entry.ItemID, &entry.BuyerID)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, buyerblock.ErrNotFound
	}
	return entry, err
}
