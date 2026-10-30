package adapter

import (
	"context"
	blockapp "xianyu-go/internal/application/buyerblock"
	"xianyu-go/internal/buyerblock"
)

// Page 把应用分页输入映射为本地查询，不向上层暴露数据库句柄。
func (r *BuyerBlockRepository) Page(ctx context.Context, userID int64, query blockapp.Query) (blockapp.Page, error) {
	// rows、total、err 分别是当前页、完整计数和查询错误。
	rows, total, err := r.store.BuyerBlocks.ListPage(ctx, userID, query.AccountID, query.Search, query.Page, query.PageSize)
	return blockapp.Page{Rows: rows, Total: total, Number: query.Page, Size: query.PageSize}, err
}

// Import 将已完成归属校验的 entries 交给单次数据库事务，重复作用域跳过。
func (r *BuyerBlockRepository) Import(ctx context.Context, userID int64, entries []buyerblock.Entry) (int, int, error) {
	return r.store.BuyerBlocks.Import(ctx, userID, entries)
}

// Batch 执行当前用户拥有的完整主键集合，部分越权会整体拒绝。
func (r *BuyerBlockRepository) Batch(ctx context.Context, userID int64, ids []int64, action string) (int, error) {
	return r.store.BuyerBlocks.Batch(ctx, userID, ids, action)
}

// OrderBuyer 只返回当前用户拥有订单的买家、账号及商品标识。
func (r *BuyerBlockRepository) OrderBuyer(ctx context.Context, userID int64, orderID string) (buyerblock.Entry, error) {
	return r.store.BuyerBlocks.OrderBuyer(ctx, userID, orderID)
}
