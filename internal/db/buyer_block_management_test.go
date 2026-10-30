package db

import (
	"context"
	"errors"
	"testing"
	"xianyu-go/internal/buyerblock"
)

// TestMultiDB_BuyerBlockManagement 在真实可用三方言目标上验证导入去重、昵称搜索、完整集合授权和批量事务。
func TestMultiDB_BuyerBlockManagement(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是隔离的 SQLite/MySQL/PostgreSQL 目标。
		t.Run(target.name, func(t *testing.T) { // t 管理当前目标的资源与断言。
			defer target.cleanup()
			// store、ctx 是本目标仓储与本地请求上下文。
			store, ctx := target.store, context.Background()
			// userID、accountID 是虚构所属用户和账号。
			userID, accountID := seedAccount(t, store)
			// entries 覆盖启用、停用及包含长文本身份的记录。
			entries := []buyerblock.Entry{{BuyerID: "12345678901234567", BuyerNick: "测试昵称", Enabled: true}, {BuyerID: "222", Enabled: false}}
			// created、skipped、err 为原子导入结果。
			created, skipped, err := store.BuyerBlocks.Import(ctx, userID, entries)
			if err != nil || created != 2 || skipped != 0 {
				t.Fatalf("import: %d/%d %v", created, skipped, err)
			}
			entries[0].Enabled = false
			created, skipped, err = store.BuyerBlocks.Import(ctx, userID, entries)
			if err != nil || created != 0 || skipped != 2 {
				t.Fatal("duplicate import did not skip")
			}
			// page、total、err 验证数据库昵称搜索及完整计数。
			page, total, err := store.BuyerBlocks.ListPage(ctx, userID, accountID, "测试昵称", 1, 20)
			if err != nil || total != 1 || len(page) != 1 || !page[0].Enabled {
				t.Fatal("search or duplicate preservation failed")
			}
			// id 是当前明确选择的记录。
			id := page[0].ID
			// err 核验集合混入缺失/越权主键时整批拒绝，不能先删除合法记录。
			if _, err := store.BuyerBlocks.Batch(ctx, userID, []int64{id, id + 1000}, "delete"); !errors.Is(err, buyerblock.ErrNotFound) {
				t.Fatal("mixed-owner/missing batch was not rejected")
			} // err 为集合授权拒绝结果。
			if _, err := store.BuyerBlocks.Batch(ctx, userID, []int64{id}, "disable"); err != nil {
				t.Fatal(err)
			} // err 为本地停用结果。
			if _, err := store.BuyerBlocks.Batch(ctx, userID, []int64{id}, "enable"); err != nil {
				t.Fatal(err)
			} // err 为本地启用结果。
			if _, err := store.BuyerBlocks.Batch(ctx, userID, []int64{id}, "delete"); err != nil {
				t.Fatal(err)
			} // err 为本地删除结果。
			page, total, err = store.BuyerBlocks.ListPage(ctx, userID, "", "", 1, 1)
			if err != nil || total != 1 || len(page) != 1 {
				t.Fatal("batch deletion damaged remaining records")
			}
		})
	}
}
