package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/buyerblock"
	"xianyu-go/internal/db"
)

// TestBuyerBlockDeliveryGates 核验所有消息、库存、模板、确认与人工补发入口在黑名单命中后没有副作用。
func TestBuyerBlockDeliveryGates(t *testing.T) {
	// ctx、store、center、sender、platform、order、cleanup 提供真实 SQLite 和平台替身。
	ctx, store, center, sender, platform, order, cleanup := newManualDeliveryFixture(t, "blocked-order")
	defer cleanup()
	// user、err 是记录所属的虚构本地用户。
	user, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// err 为黑名单夹具保存结果。
	if _, err := store.BuyerBlocks.Save(ctx, buyerblock.Entry{UserID: user.ID, BuyerID: order.BuyerID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// cardID、err 创建可观察库存是否改变的数据卡。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{UserID: user.ID, Name: "fixture", Type: "data", DataContent: "A\nB", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// task 提供与已保存记录匹配的订单身份。
	task := Task{AccountID: order.CookieID, OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID, ChatID: order.ChatID, ForceConfirmShipment: true}
	for _, kind := range []string{ActionSendCard, ActionSendTemplate, ActionSendText, ActionConfirmShipment} { // kind 覆盖统一动作分派的全部交付类型。
		// action 同时提供卡密和模板夹具，但命中后任何路径都不能访问库存或网络。
		action := db.AutomationAction{ActionType: kind, CardID: cardID, MessageTemplate: "fixture", TemplateMessages: []string{"{{cards.main}}"}, TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 1}}}
		// err 必须明确为黑名单阻断。
		if _, err := center.actions.executeActionWithProof(ctx, task, action, shipmentDeliveryProof{}); !errors.Is(err, buyerblock.ErrBlocked) {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	// err 验证快照直接文字补发也被拦截。
	if err := center.actions.sendText(ctx, task, "snapshot"); !errors.Is(err, buyerblock.ErrBlocked) {
		t.Fatal(err)
	}
	// err 验证图片快照直接补发仍受黑名单保护。
	if err := center.actions.sendImage(ctx, task, "http://fixture/image", cardID); !errors.Is(err, buyerblock.ErrBlocked) {
		t.Fatal(err)
	}
	// err 验证人工完整发货不能绕过。
	if _, err := center.ManualFullDelivery(ctx, order); !errors.Is(err, buyerblock.ErrBlocked) {
		t.Fatal(err)
	}
	// card、err 回读实际库存，所有受阻入口都不得消费一行。
	card, err := store.Cards.GetForDelivery(ctx, cardID)
	if err != nil || card.DataContent != "A\nB" || len(sender.texts) != 0 || platform.consignCalls != 0 {
		t.Fatal("blocked order caused external effects")
	}
}

// blockCloseClient 在现有 MTOP 替身之上补充关单，不扩大旧 Client 接口。
type blockCloseClient struct {
	fakeMTop // fakeMTop 保留旧平台能力替身。
	// calls 是唯一关单请求的计数。
	calls int
	// fail 控制是否返回未知传输结果。
	fail bool
}

// CloseOrderBySellerContext 不访问网络，记录当前 Cookie/订单调用并返回确定或未知结果。
func (c *blockCloseClient) CloseOrderBySellerContext(_ context.Context, cookies, orderID string) (bool, []string, string, error) {
	c.calls++
	if c.fail {
		return false, nil, cookies, errors.New("fixture transport failure")
	}
	return true, []string{"SUCCESS::fixture"}, cookies, nil
}

// TestBuyerBlockCloseOnceAndUnknown 验证明确成功与未知结果均不会因重复事件或删除黑名单而重复关单、继续发卡。
func TestBuyerBlockCloseOnceAndUnknown(t *testing.T) {
	for _, fail := range []bool{false, true} { // fail 区分已成功和需人工核对的关单结果。
		t.Run(map[bool]string{false: "success", true: "unknown"}[fail], func(t *testing.T) { // t 管理独立关单占用。
			// ctx、store、original、sender、order、cleanup 为隔离订单夹具。
			ctx, store, _, sender, _, order, cleanup := newManualDeliveryFixture(t, "close-fixture")
			defer cleanup()
			// user、err 只读取虚构用户归属。
			user, err := store.Users.GetByUsername(ctx, "admin")
			if err != nil {
				t.Fatal(err)
			}
			// blockID、err 保存显式启用关单的用户级记录。
			blockID, err := store.BuyerBlocks.Save(ctx, buyerblock.Entry{UserID: user.ID, BuyerID: order.BuyerID, Enabled: true, AutoClose: true})
			if err != nil {
				t.Fatal(err)
			}
			// err 为本地未发货事实准备错误。
			if err := store.Orders.Upsert(ctx, order.OrderID, db.OrderUpsertOpts{CookieID: order.CookieID, ItemID: order.ItemID, BuyerID: order.BuyerID, OrderStatus: "pending_ship"}); err != nil {
				t.Fatal(err)
			}
			// client、center 将唯一关单能力注入生产自动化中心。
			client := &blockCloseClient{fail: fail}
			// center 不使用真实平台客户端。
			center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: client})
			// task 具备卖家 WS 事件已经核验的订单身份。
			task := Task{AccountID: order.CookieID, OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID, ChatID: order.ChatID, Source: "ws", TriggerType: TriggerOrderPaid}
			// backgroundTask 模拟调度恢复，命中后只拦截，不得关单。
			backgroundTask := task
			backgroundTask.Source = "scheduler"
			// guarded、guardErr 核验后台事件没有发起外部动作。
			guarded, guardErr := center.handleBlockedBuyer(ctx, backgroundTask)
			if !guarded || guardErr != nil || client.calls != 0 {
				t.Fatal("background event attempted a close")
			}
			// blocked、closeErr 是首次关单结果；失败仍必须拦截。
			blocked, closeErr := center.handleBlockedBuyer(ctx, task)
			if !blocked || (closeErr != nil) != fail || client.calls != 1 {
				t.Fatalf("first close: blocked=%v calls=%d err=%v", blocked, client.calls, closeErr)
			}
			_, _ = center.handleBlockedBuyer(ctx, task)
			// err 为删除配置结果，不应解除关单结果保护。
			if err := store.BuyerBlocks.Delete(ctx, user.ID, blockID); err != nil {
				t.Fatal(err)
			}
			// err 仍必须阻止继续交付。
			if err := center.actions.ensureBuyerAllowed(ctx, task); err == nil || client.calls != 1 || len(sender.texts) != 0 {
				t.Fatal("close protection lost after repeated event or config deletion")
			}
		})
	}
}
