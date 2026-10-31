package adapter

import (
	"context"
	"strings"

	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// HandleOutgoingChatMessage 将成功的人工、自动化或跨端出站消息作为旁路观察落库；本方法不参与平台投递。
func (a *Adapter) HandleOutgoingChatMessage(ctx context.Context, message engine.OutgoingChatMessage) error {
	if a.chat == nil {
		return nil
	}
	// messageType 和 content 将新的富消息观察与历史文本字段兼容转换。
	messageType, content := strings.TrimSpace(message.MessageType), strings.TrimSpace(message.Content)
	if messageType == "" {
		messageType = "text"
	}
	if content == "" {
		content = message.Text
	}
	// err 是出站消息状态收口或幂等新增错误。
	stored, err := a.chat.RecordOutgoingMessageSent(ctx, db.ChatSession{CookieID: message.AccountID, ChatID: message.ChatID,
		BuyerID: message.BuyerID}, message.MessageKey, messageType, content, message.ObservedAt)
	if err != nil || a.takeover == nil || stored == nil || (message.Origin != engine.OutgoingManual && message.Origin != engine.OutgoingExternal) || message.SentAt == 0 {
		return err
	}
	// key 复用本地人工消息键或可信平台键，重复回显不延长接管。
	key := message.TakeoverKey
	if key == "" {
		key = message.MessageKey
	}
	if err := a.takeover.ObserveManualReply(ctx, message.AccountID, message.ChatID, key, message.SentAt); err != nil { // err 为人工接管状态的本地持久化故障。
		return err
	}
	a.chat.PublishContext(ctx, message.AccountID, chat.Event{Type: "message.updated", Message: stored})
	return err
}
