package adapter

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/buyerblock"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestBuyerBlockOnlyConfirmGuard 验证仅确认发货先拦截，并且不向越权用户泄漏交易保护状态。
func TestBuyerBlockOnlyConfirmGuard(t *testing.T) {
	// store、cleanup 提供独立数据库，不读取真实账号。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是虚构夹具的本地上下文。
	ctx := context.Background()
	// owner、err 只读取账号非敏感所有者。
	owner, err := store.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	// entry 是用户级启用记录。
	entry := buyerblock.Entry{UserID: owner, BuyerID: "123", Enabled: true}
	// err 核验夹具配置写入。
	if _, err := store.BuyerBlocks.Save(ctx, entry); err != nil {
		t.Fatal(err)
	}
	// err 核验夹具未发货订单写入。
	if err := store.Orders.Upsert(ctx, "block-order", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item", BuyerID: "123", OrderStatus: "pending_ship"}); err != nil {
		t.Fatal(err)
	}
	// client 只统计本地替身调用，禁止真实网络。
	client := &orderRuntimeMTopFake{}
	// runtime 固定注入平台替身。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }}, nil, nil)
	// forbidden 必须先返回所有权错误，而不是透露黑名单状态。
	forbidden := runtime.ConfirmShipment(ctx, "cid", "block-order", owner+100)
	if !errors.Is(forbidden.Err, orderapp.ErrForbidden) {
		t.Fatalf("ownership: %v", forbidden.Err)
	}
	// blocked 是拥有者操作同订单时的明确拒绝。
	blocked := runtime.ConfirmShipment(ctx, "cid", "block-order", owner)
	if blocked.Err == nil || client.consignCalls != 0 {
		t.Fatal("blocklist did not stop only-confirm entry")
	}
}
