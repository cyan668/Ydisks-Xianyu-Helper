package automation

import (
	"context"
	"encoding/json"
	"testing"

	"xianyu-go/internal/db"
)

// TestDeliveryQuantityModesWithInventory 验证真实临时库存按所选策略扣减；直接卡密和模板消息复用同一计数，不调用外部平台。
func TestDeliveryQuantityModesWithInventory(t *testing.T) {
	for _, mode := range []string{"per_item", "per_order"} { // mode 是本次取货使用的明确策略。
		for _, template := range []bool{false, true} { // template 决定直接卡密或双消息模板的路径。
			t.Run(mode+map[bool]string{false: "/card", true: "/template"}[template], func(t *testing.T) { // t 拥有独立库存和发送器替身。
				// store、cleanup 管理与生产完全隔离的 SQLite 夹具。
				store, cleanup := newAutomationTestStore(t)
				defer cleanup()
				// ctx 是夹具数据库与动作执行共用的上下文。
				ctx := context.Background()
				// admin、err 获取夹具用户，不使用任何真实账号。
				admin, err := store.Users.GetByUsername(ctx, "admin")
				if err != nil {
					t.Fatal(err)
				}
				// cardID、createErr 保存六份虚构库存的标识及创建结果。
				cardID, createErr := store.Cards.Create(ctx, &db.CardFull{Name: "quantity-fixture", Type: "data", DataContent: "A\nB\nC\nD\nE\nF", Enabled: true, UserID: admin.ID})
				if createErr != nil {
					t.Fatal(createErr)
				}
				// sender 只记录本地消息，不连接 WebSocket。
				sender := &testSender{}
				// executor 复用真实取库存路径与本地消息替身。
				executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}
				// action 的配置随运行冻结；模板的动作份数故意设大，不能参与变量二次乘法。
				action := db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 2, ConfigJSON: `{"quantity_mode":"` + mode + `"}`}
				if template {
					action.ActionType, action.DeliveryCount = ActionSendTemplate, 99
					action.TemplateBindings = []db.DeliveryTemplateBinding{{VariableKey: "main", CardID: cardID, DeliveryCount: 2}}
					action.TemplateMessages = []string{"{{cards.main}}", "再次说明：{{cards.main}}"}
				}
				// task 固定购买三件，按件应取六份，按单应取两份。
				task := Task{AccountID: "fixture", OrderID: "fixture-order", ChatID: "chat", BuyerID: "buyer", Quantity: "3"}
				// result、sendErr 走完整动作分发，验证模板只取一次变量且凭证单位仍按消息计。
				result, sendErr := executor.executeActionWithProof(ctx, task, action, shipmentDeliveryProof{})
				if sendErr != nil {
					t.Fatal(sendErr)
				}
				// remaining、readErr 回读实际库存，确保消息结果没有掩盖错误扣减。
				remaining, readErr := store.Cards.GetForDelivery(ctx, cardID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				// wantRemaining、wantMessages 区分库存份数与模板消息数。
				wantRemaining, wantMessages := "", 6
				if mode == "per_order" {
					wantRemaining, wantMessages = "C\nD\nE\nF", 2
				}
				if template {
					wantMessages = 2
				}
				if remaining.DataContent != wantRemaining || len(sender.texts) != wantMessages || result.proof.expectedUnits != wantMessages {
					t.Fatalf("inventory count or proof mismatch: remaining=%q messages=%d expected=%d", remaining.DataContent, len(sender.texts), result.proof.expectedUnits)
				}
			})
		}
	}
}

// TestDeliveryQuantityRejectsInvalidBeforeDependencies 验证未知模式先报错，不能访问未装配库存或发送器。
func TestDeliveryQuantityRejectsInvalidBeforeDependencies(t *testing.T) {
	// executor 故意不装配任何 I/O；错误策略若未提前阻断就会暴露错误调用。
	executor := automationActionExecutor{}
	// task 是购买三件的最小订单事实。
	task := Task{Quantity: "3"}
	// action 模拟损坏或旧外部写入的策略。
	action := db.AutomationAction{ActionType: ActionSendCard, CardID: 1, ConfigJSON: `{"quantity_mode":"invalid"}`}
	if _, err := executor.sendCardWithProof(context.Background(), task, action); err == nil { // err 必须在读库存前返回。
		t.Fatal("invalid card quantity mode accepted")
	}
	action.ActionType, action.TemplateMessages = ActionSendTemplate, []string{"fixture"}
	if _, err := executor.sendTemplate(context.Background(), task, action); err == nil { // err 必须在发送无变量模板前返回。
		t.Fatal("invalid template quantity mode accepted")
	}
}

// TestDeliveryReplayKeepsQuantityMode 证明补发只使用原动作策略，不把按单快照重算成购买数量。
func TestDeliveryReplayKeepsQuantityMode(t *testing.T) {
	// action 是每单固定两份且原始购买数量为九件的动作快照。
	action := db.AutomationAction{ActionType: ActionSendCard, CardID: 7, DeliveryCount: 2, Enabled: true, ConfigJSON: `{"quantity_mode":"per_order"}`}
	// original 固定历史订单身份和策略，实际规则后续改变不参与此路径。
	original := Task{AccountID: "account", OrderID: "order", Quantity: "9", ActionPlan: []db.AutomationAction{action}}
	// raw、err 序列化真实运行使用的原始任务结构。
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	// run 记录两份义务已经准备一份，应只允许补另一份。
	run := &db.AutomationRun{CookieID: "account", OrderID: "order", RawEventJSON: string(raw), DeliveryProof: db.AutomationDeliveryProof{ExpectedUnits: 2, PreparedUnits: 1}}
	// restored、planErr 必须恢复同一配置，而非缺省按件模式。
	restored, planErr := deliveryReplayPlan(run)
	if planErr != nil || restored.ConfigJSON != action.ConfigJSON {
		t.Fatalf("replay lost quantity mode: %v", planErr)
	}
	original.ActionPlan[0].ConfigJSON = `{"quantity_mode":"invalid"}`
	raw, err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	run.RawEventJSON = string(raw)
	if _, planErr = deliveryReplayPlan(run); planErr == nil {
		t.Fatal("invalid snapshot accepted")
	}
}

// TestDeliveryQuantityManualRefillKeepsOriginalPlan 验证人工补发按固定两份的原快照只补一份，不按现规则或购买九件重新扣库存。
func TestDeliveryQuantityManualRefillKeepsOriginalPlan(t *testing.T) {
	// ctx、store、center、sender、platform、order、cleanup 提供真实 SQLite 与本地平台替身。
	ctx, store, center, sender, platform, order, cleanup := newManualDeliveryFixture(t, "quantity-refill")
	defer cleanup()
	// err 将夹具改为可直接观察扣减的两份数据卡，禁止真实外部请求。
	if _, err := store.DB.ExecContext(ctx, `UPDATE cards SET type='data', data_content=?, delay_seconds=0`, "REFILL-ONE\nREFILL-TWO"); err != nil {
		t.Fatal(err)
	}
	// runID 创建已经有一份内容的两份义务快照。
	runID := seedPartialReplay(t, ctx, store, order)
	// run、err 读取原始任务后仅把历史计量设为按单。
	run, err := store.Automation.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	// original 保留夹具订单归属、动作顺序和发货门禁。
	var original Task
	if err := json.Unmarshal([]byte(run.RawEventJSON), &original); err != nil { // err 表示夹具任务快照解码失败。
		t.Fatal(err)
	}
	original.Quantity = "9"
	original.ActionPlan[0].ConfigJSON = `{"quantity_mode":"per_order"}`
	// raw、err 序列化随运行保存的原始计划，不替换为当前规则。
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET raw_event_json=? WHERE id=?`, string(raw), runID); err != nil { // err 检查原快照写入。
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_rule_actions SET delivery_count=4, config_json='{"quantity_mode":"per_item"}' WHERE action_type='send_card'`); err != nil { // err 模拟运行后用户编辑现行规则。
		t.Fatal(err)
	}
	// sent、replayErr 走完整人工补发入口，应发送原内容和一份新内容，再确认发货。
	sent, replayErr := center.ManualFullDelivery(ctx, order)
	if replayErr != nil || sent != 2 || len(sender.texts) != 2 || platform.consignCalls != 1 {
		t.Fatalf("manual refill: sent=%d confirms=%d err=%v", sent, platform.consignCalls, replayErr)
	}
	// remaining 核对真实库存只扣一份，没有乘九件或现行四份。
	var remaining string
	if err := store.DB.QueryRowContext(ctx, `SELECT data_content FROM cards LIMIT 1`).Scan(&remaining); err != nil { // err 检查库存查询结果。
		t.Fatal(err)
	}
	if remaining != "REFILL-TWO" {
		t.Fatal("manual refill consumed the wrong quantity")
	}
}
