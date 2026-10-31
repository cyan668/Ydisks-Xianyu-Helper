package automation

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/buyerblock"
	"xianyu-go/internal/db"
)

// checkDeliveryExecution 同时复核原执行租约和实时黑名单；ctx/task 不改变订单原始事实。
func (e *automationActionExecutor) checkDeliveryExecution(ctx context.Context, task Task) error {
	// err 表示运行执行权已失效，不能继续外部动作。
	if err := checkRunExecution(ctx); err != nil {
		return err
	}
	return e.ensureBuyerAllowed(ctx, task)
}

// ensureBuyerAllowed 在取卡、发消息和确认发货前读取实时黑名单；旧无 Store 的独立发送器合同保持可用。
func (e *automationActionExecutor) ensureBuyerAllowed(ctx context.Context, task Task) error {
	if e.store == nil || e.store.BuyerBlocks == nil {
		return nil
	}
	// entry、err 保存本地匹配；查询失败和缺身份均禁止猜测放行。
	entry, err := e.store.BuyerBlocks.MatchOrder(ctx, task.AccountID, task.OrderID, task.BuyerID, task.ItemID)
	if err == nil && entry != nil {
		err = buyerblock.ErrBlocked
	}
	if err != nil {
		return noRetryAction(errors.Join(ErrMessageNotSent, err))
	}
	return nil
}

// sellerOrderCloser 是新增的窄 MTOP 能力，旧客户端接口不增加新前置条件。
type sellerOrderCloser interface {
	// CloseOrderBySellerContext 只发送一次 Cookie 签名关单请求，返回明确成功、ret、响应 Cookie 与请求错误。
	CloseOrderBySellerContext(context.Context, string, string) (bool, []string, string, error)
}

// handleBlockedBuyer 在已核验卖家和账号启用后阻止交易；只有订单系统事件可触发显式配置的单次关单。
func (c *Center) handleBlockedBuyer(ctx context.Context, task Task) (bool, error) {
	// entry、err 保存实时匹配结果，黑名单不改变已记录的订单事实。
	entry, err := c.store.BuyerBlocks.MatchOrder(ctx, task.AccountID, task.OrderID, task.BuyerID, task.ItemID)
	if err != nil {
		c.audit.skipped(ctx, task, "交易黑名单核验未通过，已停止自动化")
		if errors.Is(err, buyerblock.ErrClosed) {
			c.audit.skipped(ctx, task, err.Error())
			return true, nil
		}
		if errors.Is(err, buyerblock.ErrCloseUnknown) || errors.Is(err, buyerblock.ErrIdentityMissing) {
			// notifyCtx、cancel 有界保存同订单去重的人工核对通知。
			notifyCtx, cancel := newAutomationRunCompensationContext(ctx)
			defer cancel()
			c.notifications.notifyManualIntervention(notifyCtx, task, "黑名单关单", err.Error(), fmt.Sprintf("buyer-block-close:%s:%s", task.AccountID, task.OrderID))
		}
		return true, err
	}
	if entry == nil {
		return false, nil
	}
	c.audit.skipped(ctx, task, buyerblock.ErrBlocked.Error())
	if !entry.AutoClose || task.Source != "ws" || (task.TriggerType != TriggerOrderCreated && task.TriggerType != TriggerOrderPaid) {
		return true, nil
	}
	// closeErr 是关单或其持久化失败；失败仍保持拦截并发送独立人工处理通知。
	closeErr := c.closeBlockedOrder(ctx, task, entry.ID)
	if closeErr != nil {
		// notifyCtx、cancel 保证请求结束后仍能有界持久化人工处理通知。
		notifyCtx, cancel := newAutomationRunCompensationContext(ctx)
		defer cancel()
		c.notifications.notifyManualIntervention(notifyCtx, task, "黑名单关单", closeErr.Error(), fmt.Sprintf("buyer-block-close:%s:%s", task.AccountID, task.OrderID))
	}
	return true, closeErr
}

// closeBlockedOrder 对 task 的本地同账号未发货订单最多请求一次关单；未知结果保留占用，不自动恢复请求。
func (c *Center) closeBlockedOrder(ctx context.Context, task Task, blockID int64) error {
	// order、err 核验本地订单归属和当前阶段，不能只凭消息标题关单。
	order, err := c.store.Orders.Get(ctx, task.OrderID)
	if err != nil {
		return fmt.Errorf("关单前读取本地订单失败: %w", err)
	}
	// state 复用数据库权威状态归一，不能对已发货、退款或完成订单关单。
	state := db.NormalizeOrderStatus(order.OrderStatus)
	if order.CookieID != task.AccountID || order.ItemID != task.ItemID || order.BuyerID != task.BuyerID || (state != "processing" && state != "pending_ship") {
		return errors.New("关单前订单身份或未发货状态不符，请人工核对")
	}
	if c.actions.mtop == nil {
		return errors.New("卖家关单能力未装配，请人工处理")
	}
	// client、supported 保留旧客户端兼容，但启用关单后缺能力必须明确报错。
	client, supported := c.actions.mtop().(sellerOrderCloser)
	if !supported {
		return errors.New("卖家关单能力未装配，请人工处理")
	}
	// session、err 固定凭证快照，外部请求期间不持有账号凭证锁。
	session, err := c.actions.openShipmentConsignSession(ctx, task.AccountID)
	if err != nil {
		return err
	}
	// claimed、err 在请求前写入永久占用，防止重入或崩溃后重复关闭。
	claimed, err := c.store.BuyerBlocks.ClaimClose(ctx, task.AccountID, task.OrderID, blockID)
	if err != nil {
		return err
	}
	if !claimed {
		return errors.New("订单状态、已有发货运行或关单占用不允许再次提交；请人工核对")
	}
	// success、returns、updated、callErr 是唯一一次 MTOP 结果，原始 ret 不进入通知。
	success, returns, updated, callErr := client.CloseOrderBySellerContext(session.requestContext, session.cookieStr, task.OrderID)
	// savedCookies 复用响应 Cookie 条件写回，防止覆盖并发更新的凭据。
	savedCookies := c.actions.persistShipmentConsignCookies(ctx, task.AccountID, session, shipmentConsignResult{succeeded: success, returns: returns, updatedCookie: updated, callErr: callErr})
	// status 根据明确成功或未知结果保存，不把失败当作可再次执行。
	status := "unknown"
	if success {
		status = "succeeded"
	}
	// saveCtx、cancel 在请求取消后仍有界保存结果，失败则 started 占用继续保护。
	saveCtx, cancel := newAutomationRunCompensationContext(ctx)
	defer cancel()
	// saveErr 是关单状态收口错误；未知状态必须转人工而非再次请求。
	saveErr := c.store.BuyerBlocks.FinishClose(saveCtx, task.AccountID, task.OrderID, status)
	if callErr != nil || !success || saveErr != nil || len(savedCookies.errors) > 0 {
		return errors.New("黑名单已阻止发卡；关单结果或本地保存尚需人工核对，不会自动重试")
	}
	c.audit.skipped(saveCtx, task, "黑名单已拦截，平台确认关单成功")
	return nil
}
