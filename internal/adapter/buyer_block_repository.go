package adapter

import (
	"context"
	"errors"

	"xianyu-go/internal/buyerblock"
	"xianyu-go/internal/db"
)

// BuyerBlockRepository 将数据库黑名单适配为应用层 Port，所有权只读取非敏感标识。
type BuyerBlockRepository struct {
	// store 是组合根注入的数据库聚合对象。
	store *db.Store
}

// NewBuyerBlockRepository 为 store 创建固定依赖的黑名单仓储适配器。
func NewBuyerBlockRepository(store *db.Store) *BuyerBlockRepository {
	return &BuyerBlockRepository{store: store}
}

// List 返回 userID 下按 accountID 过滤的规则。
func (r *BuyerBlockRepository) List(ctx context.Context, userID int64, accountID string) ([]buyerblock.Entry, error) {
	return r.store.BuyerBlocks.List(ctx, userID, accountID)
}

// Save 按 entry.UserID 隔离写入规则。
func (r *BuyerBlockRepository) Save(ctx context.Context, entry buyerblock.Entry) (int64, error) {
	return r.store.BuyerBlocks.Save(ctx, entry)
}

// Delete 只删除 userID 拥有的 id。
func (r *BuyerBlockRepository) Delete(ctx context.Context, userID, id int64) error {
	return r.store.BuyerBlocks.Delete(ctx, userID, id)
}

// OwnsScope 验证 userID 拥有 accountID 及可选 itemID，商品缺失或跨用户均返回 false。
func (r *BuyerBlockRepository) OwnsScope(ctx context.Context, userID int64, accountID, itemID string) (bool, error) {
	// owned、err 只读取账号所有权，禁止解密 Cookie。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, accountID)
	if err != nil || !owned || itemID == "" {
		return owned, err
	}
	// _,itemErr 核验商品属于所选账号。
	_, itemErr := r.store.Items.GetByCookieItem(ctx, accountID, itemID)
	if errors.Is(itemErr, db.ErrNotFound) {
		return false, nil
	}
	return itemErr == nil, itemErr
}
