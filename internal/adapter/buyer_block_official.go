package adapter

import (
	"context"
	"errors"
	blockapp "xianyu-go/internal/application/buyerblock"
	"xianyu-go/internal/buyerblock"
	"xianyu-go/internal/db"
)

// officialBlacklistClient 是逐会话单次平台操作，旧 MTOP Client 接口保持不变。
type officialBlacklistClient interface {
	// OfficialBlacklistContext 返回平台状态和响应 Cookie，不自动重试。
	OfficialBlacklistContext(context.Context, string, string, string) (bool, string, error)
}

// BuyerBlockPlatform 复用订单运行时已有凭证快照及条件写回，外部 I/O 不持有账号锁。
type BuyerBlockPlatform struct {
	// runtime 固定平台客户端和账号凭据仓储，不拥有后台任务。
	runtime *OrderRuntime
}

// Official 对当前用户拥有的实际会话执行一次操作，不从全量本地列表扇出平台请求。
func (p *BuyerBlockPlatform) Official(ctx context.Context, userID int64, accountID, chatID, action string) (bool, error) {
	// session、sessionErr 仅核验当前用户拥有的本地会话，不读取凭据。
	session, sessionErr := p.runtime.store.Chats.FindSession(ctx, userID, accountID, chatID)
	if errors.Is(sessionErr, db.ErrNotFound) {
		return false, buyerblock.ErrNotFound
	}
	if sessionErr != nil {
		return false, sessionErr
	}
	if session == nil {
		return false, buyerblock.ErrNotFound
	}
	// client、ok 为可选单次平台能力，缺少时不伪造成功。
	client, ok := p.runtime.mtopClient().(officialBlacklistClient)
	if !ok {
		return false, blockapp.ErrPlatformUnavailable
	}
	// unlock 只保护最小平台凭证视图的读取，不跨网络等待。
	unlock := p.runtime.store.LockAccountCredentials(accountID)
	// detail、readErr 是当前账号凭证快照，明文禁止记录或返回给 HTTP。
	detail, readErr := p.runtime.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	unlock()
	if readErr != nil {
		return false, readErr
	}
	if detail.UserID != userID {
		return false, buyerblock.ErrNotFound
	}
	if !hasStoredOrderCredential(detail) {
		return false, errors.New("账号 Cookie 为空，请先登录账号")
	}
	// requestCtx、cookieSession 沿用项目标准的完整 Cookie Jar 规则。
	requestCtx, cookieSession := withOrderCookieSnapshot(ctx, detail)
	// blocked、updated、callErr 是唯一平台结果；未知操作状态不触发重试。
	blocked, updated, callErr := client.OfficialBlacklistContext(requestCtx, detail.Value, session.ChatID, action)
	// value、changed、handled、saveErr 条件写回响应，避免覆盖并发凭据。
	value, changed, _, saveErr := p.runtime.persistCurrentOrderCookieSession(ctx, detail, cookieSession, updated, userID)
	if changed && saveErr == nil {
		p.runtime.UpdateRunningCookie(ctx, accountID, value)
	}
	if callErr != nil || saveErr != nil {
		return false, errors.New("闲鱼会话黑名单结果未确认，请手动查询状态；未自动重试")
	}
	return blocked, nil
}
