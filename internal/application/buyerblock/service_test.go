package buyerblock

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/buyerblock"
)

// blockRepositoryFake 隔离应用服务输入和归属测试，不连接数据库或平台。
type blockRepositoryFake struct {
	// owned 控制作用域归属校验。
	owned bool
	// entry 记录最后一次持久化输入。
	entry buyerblock.Entry
	// scopeErr 注入所有权查询故障，不能伪装成允许访问。
	scopeErr error
}

// List 返回夹具的唯一记录，userID/accountID 由应用服务预先核验。
func (r *blockRepositoryFake) List(context.Context, int64, string) ([]buyerblock.Entry, error) {
	return []buyerblock.Entry{r.entry}, nil
}

// Save 捕获 entry 并返回固定主键，验证 UserID 只能来自认证上下文。
func (r *blockRepositoryFake) Save(_ context.Context, entry buyerblock.Entry) (int64, error) {
	r.entry = entry
	return 7, nil
}

// Delete 接收已经校验过的用户和记录主键。
func (r *blockRepositoryFake) Delete(context.Context, int64, int64) error { return nil }

// OwnsScope 按夹具配置返回归属，不读取任何凭据。
func (r *blockRepositoryFake) OwnsScope(context.Context, int64, string, string) (bool, error) {
	return r.owned, r.scopeErr
}

// TestBuyerBlockService 验证归属、规范化、默认停用和输入拒绝，不把用户级记录误限为一个账号。
func TestBuyerBlockService(t *testing.T) {
	// repository、service 使用可控归属替身。
	repository := &blockRepositoryFake{owned: true}
	// service 为纯应用用例测试对象。
	service := NewService(repository)
	// ctx 不承载生产凭据。
	ctx := context.Background()
	// result、err 验证身份规范化与服务器用户覆盖。
	result, err := service.Save(ctx, 1, buyerblock.Entry{UserID: 999, BuyerID: " 123@goofish ", Reason: " fixture "})
	if err != nil || result.ID != 7 || repository.entry.UserID != 1 || repository.entry.BuyerID != "123" || repository.entry.Reason != "fixture" || result.Enabled || result.AutoClose {
		t.Fatalf("normalize: %v", err)
	}
	for _, entry := range []buyerblock.Entry{{BuyerID: ""}, {BuyerID: "123", ItemID: "orphan"}, {BuyerID: "nickname with spaces"}, {BuyerID: "123", Reason: strings.Repeat("长", 501)}, {ID: -1, BuyerID: "123"}} { // entry 遍历需在写入前拒绝的草稿。
		// err 为输入验证结果。
		if _, err := service.Save(ctx, 1, entry); !errors.Is(err, buyerblock.ErrInvalid) {
			t.Fatalf("invalid: %v", err)
		}
	}
	// err 为无身份请求的拒绝结果。
	if _, err := service.Save(ctx, 0, buyerblock.Entry{BuyerID: "123"}); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 为未装配仓储的拒绝结果。
	if _, err := NewService(nil).List(ctx, 1, ""); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 验证合法账号筛选。
	if _, err := service.List(ctx, 1, "account"); err != nil {
		t.Fatal(err)
	}
	// err 验证用户级筛选。
	if _, err := service.List(ctx, 1, ""); err != nil {
		t.Fatal(err)
	}
	repository.owned = false
	// err 验证跨用户作用域拒绝。
	if _, err := service.Save(ctx, 1, buyerblock.Entry{BuyerID: "123", CookieID: "foreign"}); !errors.Is(err, buyerblock.ErrNotFound) {
		t.Fatal(err)
	}
	// err 验证列表归属拒绝。
	if _, err := service.List(ctx, 1, "foreign"); !errors.Is(err, buyerblock.ErrNotFound) {
		t.Fatal(err)
	}
	// err 验证合法删除被委托。
	if err := service.Delete(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	// err 验证非法主键拒绝。
	if err := service.Delete(ctx, 1, 0); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// backendErr 验证所有权仓储故障原样传播，不扩大可写范围。
	backendErr := errors.New("fixture ownership failure")
	repository.scopeErr = backendErr
	// err 必须保留读取侧的故障来源。
	if _, err := service.List(ctx, 1, "account"); !errors.Is(err, backendErr) {
		t.Fatal(err)
	}
	// err 必须保留写入侧的故障来源。
	if _, err := service.Save(ctx, 1, buyerblock.Entry{BuyerID: "123", CookieID: "account"}); !errors.Is(err, backendErr) {
		t.Fatal(err)
	}
}
