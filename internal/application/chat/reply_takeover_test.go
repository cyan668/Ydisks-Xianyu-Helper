package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// takeoverTestRepository 将本地历史查询替身扩展为独立接管端口，不访问平台。
type takeoverTestRepository struct {
	*fakeRepository // fakeRepository 提供原聊天查询与归属合同。
	// minutes、until 是独立分钟配置和会话截止。
	minutes int
	// until 是 Unix 毫秒截止。
	until int64
	// events 只记录人工消息去重键，不保存正文。
	events map[string]bool
	// fault 为配置/状态读取或写入的可控失败。
	fault error
	// manualAt 为明确恢复之后仍须保留的最近人工水位。
	manualAt int64
	// version 是接管/恢复的严格递增序号，不依赖截止时间精度。
	version int64
}

// ManualReplyPauseMinutes 返回独立配置或预置故障。
func (r *takeoverTestRepository) ManualReplyPauseMinutes(context.Context, string) (int, error) {
	return r.minutes, r.fault
}

// SetManualReplyPauseMinutes 模拟关闭配置结束现有会话接管。
func (r *takeoverTestRepository) SetManualReplyPauseMinutes(_ context.Context, _ string, minutes int) error {
	r.minutes = minutes
	if minutes == 0 {
		r.until = 0
		r.version++
	}
	return r.fault
}

// ReplyPausedUntil 返回当前会话的预置本地状态。
func (r *takeoverTestRepository) ReplyPausedUntil(context.Context, string, string) (int64, error) {
	return r.until, r.fault
}

// RecordManualReply 模拟稳定消息键去重，只有配置开启才建立接管。
func (r *takeoverTestRepository) RecordManualReply(_ context.Context, _, _, key string, sentAt, now int64) error {
	if r.minutes > 0 && !r.events[key] {
		r.events[key] = true
		r.until = sentAt + int64(r.minutes)*60000
		r.manualAt = now
		r.version++
	}
	return r.fault
}

// LastManualReplyAt 在恢复后仍返回原水位，不让旧自动请求补发。
func (r *takeoverTestRepository) LastManualReplyAt(context.Context, string, string) (int64, error) {
	return r.manualAt, r.fault
}

// ConversationReplyState 模拟同次读取的版本和截止快照。
func (r *takeoverTestRepository) ConversationReplyState(context.Context, string, string) (int64, int64, error) {
	return r.until, r.version, r.fault
}

// ResumeReply 保留截止时间比较更新的应用错误。
func (r *takeoverTestRepository) ResumeReply(_ context.Context, _, _ string, expected int64) error {
	if expected != r.version {
		return ErrReplyPauseChanged
	}
	r.until = 0
	r.version++
	return r.fault
}

// TestManualAndAutomaticChatSendTakeover 验证相同发送管线下只有人工成功/不确定触发，明确失败与自动回复都不触发。
func TestManualAndAutomaticChatSendTakeover(t *testing.T) {
	// repository、outgoing、sender 分别控制接管、本地消息、平台投递。
	repository := &takeoverTestRepository{fakeRepository: &fakeRepository{owned: true}, minutes: 10, events: map[string]bool{}}
	// outgoing 为明确的虚构待发送消息，不承载真实账号。
	outgoing := &sendRepository{}
	// sender 为可控发送器，不能连接平台。
	sender := &sendSender{}
	// service 使用同一条生产发送编排，不增加状态 setter。
	service := NewWithSending(repository, outgoing, sendProvider{sender: sender}, sendUploader{result: ImageUpload{URL: "https://fixture.invalid/a.png"}})
	// input 为虚构会话和文本。
	input := OutgoingInput{Session: Session{AccountID: "a", ChatID: "c", PeerUserID: "b"}, Text: "fixture"}
	if _, err := service.SendText(WithAutomaticReply(context.Background()), input); err != nil || len(repository.events) != 0 { // err 验证自动回复不触发接管。
		t.Fatal("automatic reply created manual takeover")
	}
	sender.sendErr = errors.New("definitely not sent")
	if _, err := service.SendText(context.Background(), input); !errors.Is(err, ErrSend) || len(repository.events) != 0 { // err 验证明确信息失败不会建立接管。
		t.Fatal("definite failed manual send created takeover")
	}
	sender.sendErr = ErrSendUncertain
	if _, err := service.SendText(context.Background(), input); !errors.Is(err, ErrSendUncertain) || len(repository.events) != 1 || repository.until <= time.Now().UnixMilli() { // err 验证可能已发送的人工消息仍保护会话。
		t.Fatal("uncertain manual send did not protect conversation")
	}
	// before 为接管之前已经出现的模拟发送次数。
	before := len(sender.calls)
	sender.sendErr = nil
	if _, err := service.SendText(WithAutomaticReply(context.Background()), input); !errors.Is(err, ErrReplyPaused) || len(sender.calls) != before { // err 保证暂停期间没有任何文本投递。
		t.Fatal("paused auto reply reached platform")
	}
	// oldReply 保留人工接管以前的原买家消息水位。
	oldReply := WithAutomaticReplyAt(context.Background(), repository.manualAt-1)
	repository.until = 0
	if _, err := service.SendText(oldReply, input); !errors.Is(err, ErrReplyPaused) || len(sender.calls) != before { // err 验证显式恢复也不允许旧生成结果补发。
		t.Fatal("manual resume replayed an older generated reply")
	}
	repository.fault = errors.New("local read failed")
	if _, err := service.SendText(WithAutomaticReply(context.Background()), input); !errors.Is(err, repository.fault) || len(sender.calls) != before { // err 保证读故障不会放行自动发送。
		t.Fatal("pause read failure allowed automatic send")
	}
	repository.fault = nil
	repository.until = 0
	repository.events = map[string]bool{}
	if _, err := service.SendText(context.Background(), input); err != nil || len(repository.events) != 1 { // err 验证人工成功发送建立接管。
		t.Fatal("successful manual send did not pause")
	}
	repository.until = 0
	repository.events = map[string]bool{}
	if _, err := service.SendImage(context.Background(), ImageInput{Session: input.Session, Data: []byte("image"), ContentType: "image/png"}); err != nil || len(repository.events) != 1 { // err 验证人工图片沿相同接管合同。
		t.Fatal("successful manual image did not pause")
	}
}

// TestReplyProtectionValidationAndFailures 验证新接口校验与旧查询/发送构造仍可用。
func TestReplyProtectionValidationAndFailures(t *testing.T) {
	// repository 与 service 不装配平台能力。
	repository := &takeoverTestRepository{fakeRepository: &fakeRepository{owned: true}, events: map[string]bool{}}
	// service 为本地管理用例。
	service := New(repository)
	for _, minutes := range []int{-1, 1441} { // minutes 为配置边界反例。
		if err := service.SaveReplyProtectionMinutes(context.Background(), 1, "a", minutes); !errors.Is(err, ErrInvalidInput) { // err 验证校验先于写入。
			t.Fatal(err)
		}
	}
	if err := service.SaveReplyProtectionMinutes(context.Background(), 1, "a", 10); err != nil || repository.minutes != 10 { // err 验证正数只设置以后接管，不立即暂停。
		t.Fatal(err)
	}
	repository.owned = false
	if _, err := service.ReplyProtectionMinutes(context.Background(), 1, "a"); !errors.Is(err, ErrSessionForbidden) { // err 为非敏感归属拒绝。
		t.Fatal(err)
	}
	if _, err := New(&fakeRepository{}).ReplyProtectionMinutes(context.Background(), 1, "a"); !errors.Is(err, ErrSessionUnavailable) { // err 保持旧构造可用但新能力明确不可用。
		t.Fatal(err)
	}
	if err := service.ObserveManualReply(context.Background(), "a", "c", "", time.Now().UnixMilli()); !errors.Is(err, ErrInvalidInput) { // err 为缺少稳定消息键。
		t.Fatal(err)
	}
	if err := service.ObserveManualReply(context.Background(), "a", "c", "key", time.Now().Add(time.Hour).UnixMilli()); !errors.Is(err, ErrInvalidInput) { // err 拒绝异常未来时间。
		t.Fatal(err)
	}
}
