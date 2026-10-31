package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestOutgoingOriginsConcurrentResponses 验证并发同文发送仍由各自 mid/PNM 绑定来源，回显不能串到另一消息。
func TestOutgoingOriginsConcurrentResponses(t *testing.T) {
	// tracker 是所有发送回调共享的单账号状态。
	tracker := newOutgoingOriginTracker()
	// group 等待本测试启动的全部响应处理完成，不留下后台 goroutine。
	var group sync.WaitGroup
	for index := 0; index < 32; index++ { // index 是并发请求的非敏感序号。
		group.Add(1)
		go func(index int) { // index 为本 goroutine 独占的请求身份。
			defer group.Done()
			// requestID 为当前本地请求唯一 mid。
			requestID := fmt.Sprintf("mid-%d", index)
			tracker.register(requestID, OutgoingChatMessage{Origin: OutgoingAutomatic, ChatID: "chat", Text: "same fixture"})
			// response 是经严格响应验证后到达的该请求平台消息。
			response := OutgoingChatMessage{ChatID: "chat", Text: "same fixture", RequestID: requestID, MessageKey: fmt.Sprintf("pnm-%d", index), SentAt: tracker.startedAt + 1}
			tracker.confirm(response)
			if got := tracker.classify(response); got.Origin != OutgoingAutomatic { // got 必须属于本次精确响应，不能受其他并发请求影响。
				t.Errorf("concurrent response %d lost automatic origin", index)
			}
		}(index)
	}
	group.Wait()
}

// TestOutgoingOriginCapacityKeepsAmbiguousMessagesUnknown 验证来源缓存有界，容量淘汰不会把自动回显猜测为新人工消息。
func TestOutgoingOriginCapacityKeepsAmbiguousMessagesUnknown(t *testing.T) {
	// tracker 为单个账号的独立来源缓存。
	tracker := newOutgoingOriginTracker()
	for index := 0; index < 4097; index++ { // index 为虚构连续出站请求序号。
		tracker.register(fmt.Sprint(index), OutgoingChatMessage{Origin: OutgoingAutomatic, ChatID: "chat", Text: "fixture"})
	}
	if len(tracker.records) != 4096 {
		t.Fatal("origin cache did not remain bounded")
	}
	// message 为淘汰窗口内无法证明来源的另一条实时消息。
	message := OutgoingChatMessage{ChatID: "other-chat", Text: "different fixture", MessageKey: "external-pnm", SentAt: tracker.startedAt + 1}
	if got := tracker.classify(message); got.Origin != "" { // got 必须保持不确定，不能伪造人工暂停。
		t.Fatal("eviction turned ambiguous outgoing echo into manual takeover")
	}
}

// TestOutgoingOriginsSeparateAutomaticManualAndExternal 验证来源分类不改变自动化确认器，也不把同文推送猜成人工。
func TestOutgoingOriginsSeparateAutomaticManualAndExternal(t *testing.T) {
	// tracker 为当前账号独享的来源缓存。
	tracker := newOutgoingOriginTracker()
	// message 是实时文本推送的虚构协议摘要。
	message := OutgoingChatMessage{ChatID: "chat", MessageType: "text", Text: "fixture", SentAt: tracker.startedAt + 1}
	tracker.register("auto-mid", OutgoingChatMessage{Origin: OutgoingAutomatic, ChatID: "chat", Text: "fixture"})
	message.MessageKey = "auto-pnm"
	if got := tracker.classify(message); got.Origin != "" { // got 在严格 PNM 绑定前只能保留未知。
		t.Fatal("unconfirmed same-content echo was guessed to be manual")
	}
	message.RequestID = "auto-mid"
	tracker.confirm(message)
	if got := tracker.classify(message); got.Origin != OutgoingAutomatic { // got 通过请求响应的严格绑定识别自动消息。
		t.Fatal("confirmed automatic echo lost its origin")
	}
	message.MessageKey = "external-pnm"
	message.RequestID = ""
	if got := tracker.classify(message); got.Origin != OutgoingExternal { // got 为另一条 PNM 的跨端人工同文，不应被已绑定的自动消息永久掩盖。
		t.Fatal("distinct external message incorrectly inherited automatic origin")
	}
	tracker.register("manual-mid", OutgoingChatMessage{Origin: OutgoingManual, ChatID: "chat", Text: "manual fixture", MessageKey: "local-manual"})
	message.Text, message.RequestID, message.MessageKey = "manual fixture", "manual-mid", "manual-pnm"
	tracker.confirm(message)
	if got := tracker.classify(message); got.Origin != OutgoingManual || got.TakeoverKey != "local-manual" { // got 必须复用本地去重键，不能因另一 PNM 延长暂停。
		t.Fatal("manual echo did not preserve local takeover identity")
	}
	message.Text, message.MessageKey, message.SentAt = "old fixture", "old-pnm", tracker.startedAt-1
	if got := tracker.classify(message); got.Origin != "" { // got 是实例开始前的回放，不能创建新接管。
		t.Fatal("pre-start replay was classified as new external manual reply")
	}
	tracker.forget("manual-mid")
	if _, exists := tracker.records["manual-mid"]; exists { // exists 检查确定未发送时不会留下来源占用。
		t.Fatal("definite failure retained pending origin")
	}
}

// TestOutgoingOriginExpiryAndTimestamp 验证缓存过期与平台秒/毫秒转换，缺失实际时间不创建新接管。
func TestOutgoingOriginExpiryAndTimestamp(t *testing.T) {
	// tracker 只用于本地来源分类。
	tracker := newOutgoingOriginTracker()
	tracker.register("stale", OutgoingChatMessage{ChatID: "chat", Text: "same"})
	// record 模拟一小时前没有严格响应的请求。
	record := tracker.records["stale"]
	record.at = time.Now().Add(-2 * time.Hour).UnixMilli()
	tracker.records["stale"] = record
	if got := tracker.classify(OutgoingChatMessage{ChatID: "chat", Text: "same", MessageKey: "new", SentAt: tracker.startedAt + 1}); got.Origin != OutgoingExternal { // got 不被已过期的来源记录阻断。
		t.Fatal("expired origin hid a new manual reply")
	}
	if outgoingEchoSentAt(map[string]any{"5": "1793376000"}) != 1793376000000 || outgoingEchoSentAt(map[string]any{"5": "1793376000123"}) != 1793376000123 || outgoingEchoSentAt(map[string]any{"5": "invalid"}) != 0 {
		t.Fatal("platform creation time normalization failed")
	}
}
