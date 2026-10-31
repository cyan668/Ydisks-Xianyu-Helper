package engine

import (
	"context"
	"crypto/sha256"
	"strconv"
	"strings"
	"sync"
	"time"
)

// outgoingEchoSentAt 读取紧凑消息信封中的实际创建时间，兼容秒/毫秒；缺失时不把历史回放当作新人工消息。
func outgoingEchoSentAt(envelope map[string]any) int64 {
	// value、err 只接受平台时间整数，不用本机接收时间猜测实际发送时间。
	value, err := strconv.ParseInt(toString(envelope["5"]), 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	if value < 10000000000 {
		value *= 1000
	}
	return value
}

// OutgoingOrigin 区分可信本地人工、自动消息和官方客户端消息；unknown 不能猜测为人工。
type OutgoingOrigin string

const (
	// OutgoingManual 来自本程序明确的人工发送入口。
	OutgoingManual OutgoingOrigin = "manual"
	// OutgoingAutomatic 来自自动回复、自动发货或赠品，不触发接管。
	OutgoingAutomatic OutgoingOrigin = "automatic"
	// OutgoingExternal 是未匹配任何本地发送的官方客户端实时消息。
	OutgoingExternal OutgoingOrigin = "external"
)

// outgoingOriginContextKey 是内部发送来源标记，不接受 HTTP 参数覆盖。
type outgoingOriginContextKey struct{}

// WithOutgoingOrigin 保留复用发送管线时的可信来源，不改变原投递确认规则。
func WithOutgoingOrigin(ctx context.Context, origin OutgoingOrigin) context.Context {
	return context.WithValue(ctx, outgoingOriginContextKey{}, origin)
}

// outgoingOrigin 返回显式来源；自动化确认标记本身也是可信的自动消息证据。
func outgoingOrigin(ctx context.Context) OutgoingOrigin {
	if wantsOutgoingEchoConfirmation(ctx) {
		return OutgoingAutomatic
	}
	// origin 是适配边界固定的来源，空值表示历史调用尚未声明。
	origin, _ := ctx.Value(outgoingOriginContextKey{}).(OutgoingOrigin)
	return origin
}

// outgoingMessageKey 返回本地发送管线固定的消息关联键，空值保持历史观察行为。
func outgoingMessageKey(ctx context.Context) string {
	// key 不包含正文，仅用于人工消息与回显的幂等关联。
	key, _ := ctx.Value(outgoingMessageKeyContextKey{}).(string)
	return strings.TrimSpace(key)
}

// originFingerprint 按会话、类型和正文摘要关联同步冲突，不长期保存卡密或人工正文。
type originFingerprint struct {
	// chatID、messageType 是归一化后的会话和媒体类型。
	chatID, messageType string
	// digest 是正文的单向匹配摘要，仅用于区分自身回显，不作安全认证或写入日志。
	digest [32]byte
}

// outgoingOriginRecord 保存一次本地出站的关联身份，不含正文或凭据。
type outgoingOriginRecord struct {
	// fingerprint 供发送响应早于或晚于推送时判定归属。
	fingerprint originFingerprint
	// origin 是构造发送时的可信来源。
	origin OutgoingOrigin
	// localKey 保留人工消息去重键，平台回显不得延长同一次接管。
	localKey string
	// platformKey 是已被严格响应验证的 PNM；空值表示响应尚不足以证明归属。
	platformKey string
	// at 是本机发送接纳时间，单位为 Unix 毫秒。
	at int64
}

// outgoingOriginTracker 隶属单账号生命周期，mu 只保护有界内存表，不跨网络、日志或数据库调用。
type outgoingOriginTracker struct {
	// mu 保护 records 及响应与同步推送的并发关联。
	mu sync.Mutex
	// records 按发送请求 mid 保存关联状态，最多 4096 条并在一小时后惰性清理。
	records map[string]outgoingOriginRecord
	// startedAt 固定当前实例开始接纳事件的时间，重启后的旧同步消息不能被猜测为新跨端人工。
	startedAt int64
	// externalBlockedUntil 在容量淘汰之后暂时保留来源不确定性，不能把被淘汰的本地自动消息猜成人工。
	externalBlockedUntil int64
}

// newOutgoingOriginTracker 创建独立的来源记录器，不改变已有自动化回显确认器。
func newOutgoingOriginTracker() *outgoingOriginTracker {
	return &outgoingOriginTracker{records: make(map[string]outgoingOriginRecord), startedAt: time.Now().UnixMilli()}
}

// fingerprintOutgoing 仅为回显来源匹配建立正文摘要；message 不被持久保存。
func fingerprintOutgoing(message OutgoingChatMessage) originFingerprint {
	return originFingerprint{chatID: normalizeOutgoingIdentity(message.ChatID), messageType: normalizeOutgoingEchoType(message.MessageType), digest: sha256.Sum256([]byte(outgoingEchoContent(message)))}
}

// register 在 WebSocket 写入前登记来源，避免同步推送先到时将自动消息误判为跨端人工。
func (t *outgoingOriginTracker) register(requestID string, message OutgoingChatMessage) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// now 为本次内存记录清理与发送接纳的基准。
	now := time.Now().UnixMilli()
	for key, record := range t.records { // key、record 为当前检查过期的请求及其非敏感来源。
		if now-record.at > 3600000 {
			delete(t.records, key)
		}
	}
	if len(t.records) >= 4096 {
		t.externalBlockedUntil = now + 60000
		// oldestKey、oldest 为容量到达上限时最早的记录，保持内存有界。
		oldestKey, oldest := "", now
		for key, record := range t.records { // key、record 为候选淘汰请求。
			if record.at <= oldest {
				oldestKey, oldest = key, record.at
			}
		}
		delete(t.records, oldestKey)
	}
	t.records[requestID] = outgoingOriginRecord{fingerprint: fingerprintOutgoing(message), origin: message.Origin, localKey: message.MessageKey, at: now}
}

// confirm 只接收既有 WS 严格响应校验之后的 mid/PNM 绑定，不以相同正文猜测平台消息身份。
func (t *outgoingOriginTracker) confirm(message OutgoingChatMessage) {
	if t == nil || message.RequestID == "" || message.MessageKey == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// record、exists 表示响应是否属于仍由本账号拥有的本地发送。
	record, exists := t.records[message.RequestID]
	if exists && record.fingerprint == fingerprintOutgoing(message) {
		record.platformKey = message.MessageKey
		t.records[message.RequestID] = record
	}
}

// forget 仅删除明确未发送的请求；未知结果保留来源保护，迟到自动回显不能触发人工接管。
func (t *outgoingOriginTracker) forget(requestID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.records, requestID)
	t.mu.Unlock()
}

// classify 为同步推送附加来源。正文冲突但缺乏 mid/PNM 证明时保持 unknown，不伪造人工事件。
func (t *outgoingOriginTracker) classify(message OutgoingChatMessage) OutgoingChatMessage {
	if t == nil || strings.TrimSpace(message.MessageKey) == "" {
		return message
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// fingerprint 为当前同步消息的会话、类型和正文匹配键。
	fingerprint := fingerprintOutgoing(message)
	// ambiguous 表示相同正文仍有未绑定 PNM 的本地请求，不能区分跨端同文与本地回显。
	ambiguous := false
	for key, record := range t.records { // key、record 为本账号保留的单次发送来源。
		if time.Now().UnixMilli()-record.at > 3600000 {
			delete(t.records, key)
			continue
		}
		if record.platformKey == message.MessageKey && record.fingerprint == fingerprint {
			message.Origin = record.origin
			if record.localKey != "" {
				message.TakeoverKey = record.localKey
			}
			return message
		}
		if record.fingerprint == fingerprint && record.platformKey == "" {
			ambiguous = true
		}
	}
	// 只把当前实例的一分钟内实时消息视为跨端候选；旧同步回放无法证明新的人工接管。
	if !ambiguous && time.Now().UnixMilli() >= t.externalBlockedUntil && message.SentAt >= t.startedAt && time.Now().UnixMilli()-message.SentAt <= 60000 {
		message.Origin = OutgoingExternal
	}
	return message
}
