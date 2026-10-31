package db

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/buyerblock"
)

// TestMultiDB_BuyerBlocks 覆盖三方言作用域优先级、用户隔离、去重、启停、删除和关单占用生命周期。
func TestMultiDB_BuyerBlocks(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是本轮真实可用的隔离数据库。
		t.Run(target.name, func(t *testing.T) { // t 拥有当前数据库的断言与资源。
			defer target.cleanup()
			// store、ctx 是此方言的持久化入口及请求上下文。
			store, ctx := target.store, context.Background()
			// userID、accountID 为虚构账号的非敏感归属。
			userID, accountID := seedAccount(t, store)
			// global 是用户级启用记录，所有账号共享。
			global := buyerblock.Entry{UserID: userID, BuyerID: "123", Enabled: true}
			// id、err 保存创建结果。
			id, err := store.BuyerBlocks.Save(ctx, global)
			if err != nil {
				t.Fatal(err)
			}
			global.ID = id
			// err 验证数据库唯一约束。
			if _, err := store.BuyerBlocks.Save(ctx, buyerblock.Entry{UserID: userID, BuyerID: "123"}); !errors.Is(err, buyerblock.ErrConflict) {
				t.Fatalf("duplicate: %v", err)
			}
			// scoped 是更具体的商品级记录。
			scoped := buyerblock.Entry{UserID: userID, CookieID: accountID, ItemID: "item", BuyerID: "123", Enabled: true, AutoClose: true}
			scoped.ID, err = store.BuyerBlocks.Save(ctx, scoped)
			if err != nil {
				t.Fatal(err)
			}
			// hit、matchErr 证明后缀归一和具体作用域优先。
			hit, matchErr := store.BuyerBlocks.MatchOrder(ctx, accountID, "", "123@goofish", "item")
			if matchErr != nil || hit == nil || hit.ID != scoped.ID {
				t.Fatalf("specific match: %v", matchErr)
			}
			if _, matchErr = store.BuyerBlocks.MatchOrder(ctx, accountID, "", "", ""); !errors.Is(matchErr, buyerblock.ErrIdentityMissing) {
				t.Fatalf("missing facts: %v", matchErr)
			}
			// matchErr 验证评审给出的“未知订单且身份全空”反例必须保守拒绝，而非静默放行。
			if _, matchErr = store.BuyerBlocks.MatchOrder(ctx, accountID, "missing-order", "", ""); !errors.Is(matchErr, buyerblock.ErrIdentityMissing) {
				t.Fatalf("missing order facts: %v", matchErr)
			}
			// foreign 是不存在的其他用户，不能更新或删除当前记录。
			foreign := global
			foreign.UserID = userID + 10000
			if _, err = store.BuyerBlocks.Save(ctx, foreign); !errors.Is(err, buyerblock.ErrNotFound) {
				t.Fatalf("foreign update: %v", err)
			}
			if err = store.BuyerBlocks.Delete(ctx, foreign.UserID, id); !errors.Is(err, buyerblock.ErrNotFound) {
				t.Fatalf("foreign delete: %v", err)
			}
			scoped.Enabled = false
			if _, err = store.BuyerBlocks.Save(ctx, scoped); err != nil {
				t.Fatal(err)
			}
			hit, matchErr = store.BuyerBlocks.MatchOrder(ctx, accountID, "", "123", "item")
			if matchErr != nil || hit == nil || hit.ID != global.ID {
				t.Fatal("disabled scoped rule hid global block")
			}
			// orderErr 准备真实调用所需的本地未发货订单事实。
			if orderErr := store.Orders.Upsert(ctx, "close-order", OrderUpsertOpts{CookieID: accountID, BuyerID: "123", ItemID: "item", OrderStatus: "pending_ship"}); orderErr != nil {
				t.Fatal(orderErr)
			}
			// claimed、claimErr 核验占用在重复事件间唯一。
			claimed, claimErr := store.BuyerBlocks.ClaimClose(ctx, accountID, "close-order", global.ID)
			if claimErr != nil || !claimed {
				t.Fatalf("claim: %v", claimErr)
			}
			claimed, claimErr = store.BuyerBlocks.ClaimClose(ctx, accountID, "close-order", global.ID)
			if claimErr != nil || claimed {
				t.Fatal("duplicate close claim")
			}
			if err = store.BuyerBlocks.Delete(ctx, userID, global.ID); err != nil {
				t.Fatal(err)
			}
			if _, matchErr = store.BuyerBlocks.MatchOrder(ctx, accountID, "close-order", "123", "item"); !errors.Is(matchErr, buyerblock.ErrCloseUnknown) {
				t.Fatal("deleted block removed unknown-close protection")
			}
			if err = store.BuyerBlocks.FinishClose(ctx, accountID, "close-order", "succeeded"); err != nil {
				t.Fatal(err)
			}
			if _, matchErr = store.BuyerBlocks.MatchOrder(ctx, accountID, "close-order", "123", "item"); !errors.Is(matchErr, buyerblock.ErrClosed) {
				t.Fatal("confirmed close allowed delivery")
			}
			// claimed、claimErr 为平台成功后本地订单并发删除的独立占用。
			// orderErr 建立领取时存在的待发货事实。
			if orderErr := store.Orders.Upsert(ctx, "missing-close", OrderUpsertOpts{CookieID: accountID, BuyerID: "123", ItemID: "item", OrderStatus: "pending_ship"}); orderErr != nil {
				t.Fatal(orderErr)
			}
			claimed, claimErr = store.BuyerBlocks.ClaimClose(ctx, accountID, "missing-close", scoped.ID)
			if claimErr != nil || !claimed {
				t.Fatal(claimErr)
			}
			// deleteErr 模拟平台返回前本地订单被并发清理。
			if _, deleteErr := store.DB.ExecContext(ctx, `DELETE FROM orders WHERE order_id='missing-close'`); deleteErr != nil {
				t.Fatal(deleteErr)
			}
			if err = store.BuyerBlocks.FinishClose(ctx, accountID, "missing-close", "succeeded"); !errors.Is(err, buyerblock.ErrCloseStateConflict) {
				t.Fatalf("missing local order: %v", err)
			}
			if _, matchErr = store.BuyerBlocks.MatchOrder(ctx, accountID, "missing-close", "123", "item"); !errors.Is(matchErr, buyerblock.ErrClosed) {
				t.Fatal("local conflict lost confirmed close protection")
			}
			if err = store.Cookies.Delete(ctx, accountID); err != nil {
				t.Fatal(err)
			}
			// rows、listErr 核验账号删除后不会继承旧账号规则。
			rows, listErr := store.BuyerBlocks.List(ctx, userID, "")
			if listErr != nil || len(rows) != 0 {
				t.Fatalf("account cleanup: %v", listErr)
			}
		})
	}
}
