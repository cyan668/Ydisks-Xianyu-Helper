package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/ws"
)

// replyContentClaimKey 携带单次处理的不可变领取证明；所有进展由数据库保存，不共享可变 Hook 状态。
type replyContentClaimKey struct{}

// claimReplyContent 在生成和商品首次回复前领取会话/入站正文，正文只形成隐私摘要，不写入日志或账本。
func (r *ReplyService) claimReplyContent(ctx context.Context, message ChatMessage) (context.Context, bool, error) {
	if r.store == nil || r.store.Chats == nil || message.ChatID == "" || strings.TrimSpace(message.Text) == "" {
		return ctx, true, nil
	}
	// digest 为原始入站正文的幂等匹配摘要，不进行大小写或空白归一，避免改变“相同消息”的含义。
	digest := sha256.Sum256([]byte(message.Text))
	// claim、allowed、err 是当前账号/会话/内容的原子领取结果。
	claim, allowed, err := r.store.Chats.ClaimReplyContent(ctx, r.cookieID, message.ChatID, hex.EncodeToString(digest[:]), message.MessageID, time.Now().UnixMilli())
	if err != nil || !allowed || claim.ID == 0 {
		return ctx, allowed, err
	}
	return context.WithValue(ctx, replyContentClaimKey{}, claim), true, nil
}

// replyContentClaim 从当前调用读取不可变证明，旧的离线投递和关闭配置不要求新端口。
func replyContentClaim(ctx context.Context) db.ReplyContentClaim {
	// claim 只含持久化主键和代次，不含正文、接收人或凭据。
	claim, _ := ctx.Value(replyContentClaimKey{}).(db.ReplyContentClaim)
	return claim
}

// beginReplyContentDelivery 在外部投递前落盘占用，旧租约即使晚生成了回复也不能开始发送。
func (r *ReplyService) beginReplyContentDelivery(ctx context.Context) error {
	// claim 为本次入站处理的领取身份。
	claim := replyContentClaim(ctx)
	if claim.ID == 0 {
		return nil
	}
	return r.store.Chats.BeginReplyContentDelivery(ctx, claim, time.Now().UnixMilli())
}

// checkReplyContentGeneration 在首次回复之后核验占用，防止未知首次投递仍调用后续 API 或 AI。
func (r *ReplyService) checkReplyContentGeneration(ctx context.Context) error {
	// claim 为本次入站正文的不可变领取身份；关闭配置沿用旧调用合同。
	claim := replyContentClaim(ctx)
	if claim.ID == 0 {
		return nil
	}
	return r.store.Chats.CheckReplyContentClaim(ctx, claim, time.Now().UnixMilli())
}

// observeReplyContentDelivery 按真实分段确认保存发送时间；未知结果始终隔离，确定未发送不消耗冷却。
func (r *ReplyService) observeReplyContentDelivery(ctx context.Context, sent ReplySendResult, sendErr error) error {
	// claim 为本次内容领取身份。
	claim := replyContentClaim(ctx)
	if claim.ID == 0 {
		return nil
	}
	// transportErr 兼容传输层显式未知错误，不能把它当作可释放的失败。
	var transportErr *ws.SendError
	// uncertain 为调用方确认事实与底层传输分类的并集。
	uncertain := sent.Uncertain || errors.As(sendErr, &transportErr) && ws.SendResultKind(sendErr) == ws.SendUncertain
	// persistCtx、cancel 只给本地发送结果收口独立五秒窗口，不新增后台任务。
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replyRecordPersistTimeout)
	defer cancel()
	return r.store.Chats.ObserveReplyContentDelivery(persistCtx, claim, sent.TextSent || sent.ImageSent, uncertain, time.Now().UnixMilli())
}

// finishReplyContent 收口整次入站处理，无匹配/未发送释放，崩溃或未知发送不因冷却到期自动解除。
func (r *ReplyService) finishReplyContent(ctx context.Context) error {
	// claim 为当前调用持有的证明，旧调用无法覆盖后来重领。
	claim := replyContentClaim(ctx)
	if claim.ID == 0 {
		return nil
	}
	// persistCtx、cancel 使账号停止或请求取消后仍能完成有界本地收口。
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replyRecordPersistTimeout)
	defer cancel()
	return r.store.Chats.FinishReplyContent(persistCtx, claim)
}
