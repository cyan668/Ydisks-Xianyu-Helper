package chat

import (
	"context"
	"errors"
	"testing"
)

// contentCooldownTestRepository 提供独立冷却管理扩展，保留旧聊天查询构造合同。
type contentCooldownTestRepository struct {
	*fakeRepository // fakeRepository 仅提供非敏感归属与原聊天能力。
	// seconds 是独立配置，不含人工暂停。
	seconds int
	// fault 控制配置与保护管理的本地故障。
	fault error
	// calls 记录真正进入管理仓储的调用，证明越权/非法输入先被拒绝。
	calls int
}

// SameContentCooldownSeconds 返回测试秒数或可控读取故障。
func (r *contentCooldownTestRepository) SameContentCooldownSeconds(context.Context, string) (int, error) {
	r.calls++
	return r.seconds, r.fault
}

// SetSameContentCooldownSeconds 模拟窄字段保存并返回可控故障。
func (r *contentCooldownTestRepository) SetSameContentCooldownSeconds(_ context.Context, _ string, seconds int) error {
	r.calls++
	r.seconds = seconds
	return r.fault
}

// ListContentCooldownReviews 返回一条无正文的分页定位信息。
func (r *contentCooldownTestRepository) ListContentCooldownReviews(context.Context, string, int, int, int64) ([]ContentCooldownReview, int, error) {
	r.calls++
	return []ContentCooldownReview{{ID: 1, Version: 2, ChatID: "chat", MessageKey: "message", ClaimedAt: 1000}}, 1, r.fault
}

// ResolveContentCooldownReview 只记录明确恢复调用，故障不会被归一为成功。
func (r *contentCooldownTestRepository) ResolveContentCooldownReview(context.Context, string, int64, int64, int64) error {
	r.calls++
	return r.fault
}

// TestContentCooldownApplication 验证秒数上限、明确确认、分页、归属与故障传播。
func TestContentCooldownApplication(t *testing.T) {
	// repository、service、ctx 是完全本地的应用夹具。
	repository := &contentCooldownTestRepository{fakeRepository: &fakeRepository{owned: true}}
	// service 不要求新的构造参数，旧端口保持兼容。
	service := New(repository)
	// ctx 只控制本地管理调用。
	ctx := context.Background()
	if value, err := service.ContentCooldownSeconds(ctx, 1, " account "); err != nil || value != 0 { // value、err 为默认关闭设置和读取结果。
		t.Fatal("default cooldown changed")
	}
	if err := service.SaveContentCooldownSeconds(ctx, 1, "account", 3600); err != nil || repository.seconds != 3600 { // err 核对用户一小时配置的保存结果。
		t.Fatal("one-hour cooldown not persisted")
	}
	// page、err 是当前账号的本地保护定位分页。
	page, err := service.ContentCooldownReviews(ctx, 1, "account", 20, 0)
	if err != nil || page.Total != 1 || page.Items[0].Version != 2 || page.Limit != 20 {
		t.Fatal("review page lost metadata")
	}
	if err := service.ResolveContentCooldown(ctx, 1, "account", 1, 2, true); err != nil { // err 是用户明确核对后的恢复结果。
		t.Fatal(err)
	}
	// before 记录非法输入之前的仓储调用数。
	before := repository.calls
	for _, seconds := range []int{-1, 86401} { // seconds 为不能静默修正的输入。
		if err := service.SaveContentCooldownSeconds(ctx, 1, "account", seconds); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid seconds accepted")
		}
	}
	for _, query := range [][2]int{{0, 0}, {101, 0}, {20, -1}} { // query 为超限页长或负偏移。
		if _, err := service.ContentCooldownReviews(ctx, 1, "account", query[0], query[1]); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid page accepted")
		}
	}
	if err := service.ResolveContentCooldown(ctx, 1, "account", 1, 2, false); !errors.Is(err, ErrInvalidInput) { // err 必须拒绝缺少确认的恢复。
		t.Fatal("review without confirmation accepted")
	}
	if err := service.ResolveContentCooldown(ctx, 1, "account", 0, 0, true); !errors.Is(err, ErrInvalidInput) { // err 必须拒绝无效主键和版本。
		t.Fatal("invalid review identity accepted")
	}
	if _, err := service.ContentCooldownSeconds(ctx, 0, "account"); !errors.Is(err, ErrInvalidInput) { // err 不允许匿名用户查询配置。
		t.Fatal("missing user accepted")
	}
	if _, err := service.ContentCooldownSeconds(ctx, 1, " "); !errors.Is(err, ErrInvalidInput) { // err 不允许空账号形成批量查询。
		t.Fatal("missing account accepted")
	}
	if repository.calls != before {
		t.Fatal("invalid input reached repository")
	}
	repository.owned = false
	if err := service.SaveContentCooldownSeconds(ctx, 1, "account", 0); !errors.Is(err, ErrSessionForbidden) { // err 必须在写仓储之前拒绝越权。
		t.Fatal("foreign account accepted")
	}
	if _, err := service.ContentCooldownReviews(ctx, 1, "account", 20, 0); !errors.Is(err, ErrSessionForbidden) { // err 不能列出别的用户账号的保护。
		t.Fatal("foreign reviews were listed")
	}
	if err := service.ResolveContentCooldown(ctx, 1, "account", 1, 2, true); !errors.Is(err, ErrSessionForbidden) { // err 明确确认也不能绕过账号归属。
		t.Fatal("foreign review was resolved")
	}
	repository.ownedErr = errors.New("ownership failed")
	if _, err := service.ContentCooldownSeconds(ctx, 1, "account"); !errors.Is(err, repository.ownedErr) { // err 保留真实归属查询故障。
		t.Fatal("ownership failure was ignored")
	}
	repository.ownedErr = nil
	repository.owned = true
	repository.fault = errors.New("storage failed")
	if _, err := service.ContentCooldownSeconds(ctx, 1, "account"); !errors.Is(err, repository.fault) { // err 不得把存储读取故障当成配置零。
		t.Fatal("read failure became default off")
	}
	if err := service.SaveContentCooldownSeconds(ctx, 1, "account", 86400); !errors.Is(err, repository.fault) { // err 保留合法上限输入的真实写入故障。
		t.Fatal("save failure became success")
	}
	if _, err := service.ContentCooldownReviews(ctx, 1, "account", 20, 0); !errors.Is(err, repository.fault) { // err 不能被转换为空待核对列表。
		t.Fatal("list failure became empty page")
	}
	if err := service.ResolveContentCooldown(ctx, 1, "account", 1, 2, true); !errors.Is(err, repository.fault) { // err 解除故障不能显示为成功。
		t.Fatal("resolve failure became success")
	}
	if _, err := New(&fakeRepository{}).ContentCooldownSeconds(ctx, 1, "account"); !errors.Is(err, ErrSessionUnavailable) { // err 是旧构造不提供新扩展时的稳定结果。
		t.Fatal("old constructor no longer provides clear unavailable result")
	}
	// missing 模拟未构造的应用，用例返回稳定不可用。
	var missing *Service
	if _, err := missing.ContentCooldownSeconds(ctx, 1, "account"); !errors.Is(err, ErrSessionUnavailable) { // err 不能因 nil 应用引起 panic。
		t.Fatal("nil service did not fail closed")
	}
}
