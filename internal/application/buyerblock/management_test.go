package buyerblock

import (
	"context"
	"errors"
	"strings"
	"testing"
	"xianyu-go/internal/buyerblock"
)

// TestManagementRejectsMissingDependenciesAndInvalidOfficial 保持旧用例的 nil 合同，非法平台请求必须先返回参数错误。
func TestManagementRejectsMissingDependenciesAndInvalidOfficial(t *testing.T) {
	// service 是没有被组合根构造的接收者，不允许 panic。
	var service *Service
	// ctx 是不含凭据的本地测试上下文。
	ctx := context.Background()
	// err 验证 nil 分页拒绝。
	if _, err := service.Page(ctx, 1, Query{}); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 验证 nil 导入拒绝。
	if _, err := service.Import(ctx, 1, nil); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 验证 nil 批量拒绝。
	if _, err := service.Batch(ctx, 1, nil, "delete"); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 验证 nil 订单派生拒绝。
	if _, err := service.FromOrder(ctx, 1, "order", "user", ""); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	service = NewService(&blockRepositoryFake{owned: true})
	// err 非法 action 不应被“平台未装配”掩盖。
	if _, err := service.Official(ctx, 1, "a", "chat", "invalid"); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 合法请求缺平台能力时才返回不可用。
	if _, err := service.Official(ctx, 1, "a", "chat", "query"); !errors.Is(err, ErrPlatformUnavailable) {
		t.Fatal(err)
	}
}

// managementRepositoryFake 捕获应用层校验之后的请求，所有故障均在本地注入。
type managementRepositoryFake struct {
	blockRepositoryFake // blockRepositoryFake 保持原 CRUD 与作用域校验合同。
	// query 记录最终分页与搜索参数。
	query Query
	// imported 记录一次原子写入的全部条目；失败输入不应调用此端口。
	imported []buyerblock.Entry
	// failure 模拟管理仓储故障，必须原样传播。
	failure error
}

// Page 捕获已归一的 query，返回单页结果或注入故障。
func (r *managementRepositoryFake) Page(_ context.Context, _ int64, query Query) (Page, error) {
	r.query = query
	return Page{Rows: []buyerblock.Entry{}, Number: query.Page, Size: query.PageSize}, r.failure
}

// Import 捕获已验证的 entries，不在替身内重复业务校验。
func (r *managementRepositoryFake) Import(_ context.Context, _ int64, entries []buyerblock.Entry) (int, int, error) {
	r.imported = entries
	return len(entries), 0, r.failure
}

// Batch 返回明确集合的处理数或故障，不调用平台。
func (r *managementRepositoryFake) Batch(_ context.Context, _ int64, ids []int64, _ string) (int, error) {
	return len(ids), r.failure
}

// OrderBuyer 返回仓储已核验的订单身份，调用者不能提供另一个买家。
func (r *managementRepositoryFake) OrderBuyer(context.Context, int64, string) (buyerblock.Entry, error) {
	return r.entry, r.failure
}

// TestManagementPaginationAndAuthorization 验证默认页码、上限、搜索归一与归属故障。
func TestManagementPaginationAndAuthorization(t *testing.T) {
	// repository、service 为本地管理端口与应用服务。
	repository := &managementRepositoryFake{blockRepositoryFake: blockRepositoryFake{owned: true}}
	// service 不拥有数据库或后台任务。
	service := NewService(repository)
	// ctx 为本轮用例上下文。
	ctx := context.Background()
	// err 验证默认分页和去除搜索词外侧空白。
	if _, err := service.Page(ctx, 1, Query{Search: " 测试 "}); err != nil || repository.query.Page != 1 || repository.query.PageSize != 20 || repository.query.Search != "测试" {
		t.Fatal("pagination defaults or search normalization failed")
	}
	// err 验证指定账号及页大小硬上限。
	if _, err := service.Page(ctx, 1, Query{AccountID: "account", Page: 2, PageSize: 999}); err != nil || repository.query.PageSize != 100 || repository.query.Page != 2 {
		t.Fatal("pagination limit failed")
	}
	repository.owned = false
	// err 越权筛选必须拒绝，不能变成全量结果。
	if _, err := service.Page(ctx, 1, Query{AccountID: "foreign"}); !errors.Is(err, buyerblock.ErrNotFound) {
		t.Fatal(err)
	}
	repository.scopeErr = errors.New("scope fixture failure")
	// err 归属查询故障不能伪装成空列表。
	if _, err := service.Page(ctx, 1, Query{AccountID: "account"}); !errors.Is(err, repository.scopeErr) {
		t.Fatal(err)
	}
	// err 缺少管理扩展时明确拒绝，不调用旧无界列表。
	if _, err := NewService(&blockRepositoryFake{}).Page(ctx, 1, Query{}); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// err 缺少认证身份时拒绝。
	if _, err := service.Page(ctx, 0, Query{}); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
}

// TestManagementImportAndBatch 验证整份文件预校验、身份归一、原子提交和明确集合约束。
func TestManagementImportAndBatch(t *testing.T) {
	// repository、service 使用本地事务替身。
	repository := &managementRepositoryFake{blockRepositoryFake: blockRepositoryFake{owned: true}}
	// service 为当前用户管理用例。
	service := NewService(repository)
	// ctx 不承载真实平台状态。
	ctx := context.Background()
	for _, entry := range []buyerblock.Entry{{BuyerID: "bad nickname"}, {BuyerID: "123", ItemID: "orphan"}, {BuyerID: "123", BuyerNick: strings.Repeat("长", 101)}, {BuyerID: "123", Reason: strings.Repeat("长", 501)}, {BuyerID: "123", CookieID: strings.Repeat("a", 192)}, {BuyerID: "123", CookieID: "account", ItemID: strings.Repeat("a", 192)}} { // entry 为应在任何写入前拒绝的文件行。
		// err 验证合法首行也不能在后行无效时提前写入。
		if _, err := service.Import(ctx, 1, []buyerblock.Entry{{BuyerID: "111"}, entry}); !errors.Is(err, buyerblock.ErrInvalid) || len(repository.imported) != 0 {
			t.Fatal("invalid import caused partial write")
		}
	}
	// result、err 验证输入用户不能覆盖认证用户，长 ID 不经过浮点转换。
	result, err := service.Import(ctx, 1, []buyerblock.Entry{{ID: 99, UserID: 99, BuyerID: " 12345678901234567@goofish ", CookieID: " account ", BuyerNick: " 测试 ", Reason: " 备注 "}})
	if err != nil || result.Created != 1 || repository.imported[0].ID != 0 || repository.imported[0].UserID != 1 || repository.imported[0].BuyerID != "12345678901234567" || repository.imported[0].BuyerNick != "测试" || repository.imported[0].Reason != "备注" {
		t.Fatal("import normalization failed")
	}
	repository.owned = false
	// err 验证外部账号拒绝。
	if _, err := service.Import(ctx, 1, []buyerblock.Entry{{BuyerID: "123", CookieID: "foreign"}}); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	repository.scopeErr = errors.New("scope fixture failure")
	// err 保留所有权查询错误。
	if _, err := service.Import(ctx, 1, []buyerblock.Entry{{BuyerID: "123", CookieID: "account"}}); !errors.Is(err, repository.scopeErr) {
		t.Fatal(err)
	}
	for _, entries := range [][]buyerblock.Entry{nil, make([]buyerblock.Entry, MaxBatchRows+1)} { // entries 为长度不合法的文件。
		// err 验证空导入和超限均不进入事务。
		if _, err := service.Import(ctx, 1, entries); !errors.Is(err, buyerblock.ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, ids := range [][]int64{nil, {0}, {-1}, {1, 1}, make([]int64, MaxBatchRows+1)} { // ids 为非法或重复的主键集合。
		// err 验证集合不被静默缩小。
		if _, err := service.Batch(ctx, 1, ids, "disable"); !errors.Is(err, buyerblock.ErrInvalid) {
			t.Fatal(err)
		}
	}
	// err 验证任意动作文本不能进入数据库。
	if _, err := service.Batch(ctx, 1, []int64{1}, "invalid"); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// affected、err 验证合法动作只处理明确集合。
	if affected, err := service.Batch(ctx, 1, []int64{1, 2}, "disable"); err != nil || affected != 2 {
		t.Fatal("explicit batch failed")
	}
	repository.failure = errors.New("transaction fixture failure")
	// err 验证事务故障不会返回成功。
	if _, err := service.Batch(ctx, 1, []int64{1}, "delete"); !errors.Is(err, repository.failure) {
		t.Fatal(err)
	}
}

// TestManagementOrderScopesAndExport 验证订单三个作用域、默认备注、缺失身份、导出防公式和读取故障。
func TestManagementOrderScopesAndExport(t *testing.T) {
	// repository 的订单身份只包含虚构稳定标识。
	repository := &managementRepositoryFake{blockRepositoryFake: blockRepositoryFake{owned: true, entry: buyerblock.Entry{CookieID: "account", ItemID: "item", BuyerID: "123", BuyerNick: "=fixture", Reason: " @fixture"}}}
	// service、ctx 是纯应用测试对象和本地上下文。
	service := NewService(repository)
	// ctx 不允许测试连接外部平台。
	ctx := context.Background()
	for _, level := range []string{"user", "account", "item"} { // level 为用户必须明确选择的生效范围。
		// result、err 验证每次派生都会走相同的导入校验。
		result, err := service.FromOrder(ctx, 1, "order", level, "")
		if err != nil || result.Created != 1 {
			t.Fatal(err)
		}
		// entry 为经过范围裁剪后实际提交的本地规则。
		entry := repository.imported[0]
		if !entry.Enabled || entry.AutoClose || !strings.Contains(entry.Reason, "订单页一键拉黑") || (level == "user" && entry.CookieID != "") || (level != "item" && entry.ItemID != "") || (level == "item" && entry.ItemID != "item") {
			t.Fatal("order scope or close policy changed")
		}
	}
	// err 验证明确备注去除空白后被保留。
	if _, err := service.FromOrder(ctx, 1, "order", "user", " 人工备注 "); err != nil || repository.imported[0].Reason != "人工备注" {
		t.Fatal("explicit reason lost")
	}
	// err 验证范围未选择时不会创建用户级名单。
	if _, err := service.FromOrder(ctx, 1, "order", "", ""); !errors.Is(err, buyerblock.ErrInvalid) {
		t.Fatal(err)
	}
	// content、err 验证 CSV BOM 和公式前缀防护，不泄露测试内容到失败日志。
	content, err := service.ExportCSV(ctx, 1, "")
	if err != nil || !strings.HasPrefix(string(content), "\xef\xbb\xbf") || !strings.Contains(string(content), "'=fixture") || !strings.Contains(string(content), "' @fixture") {
		t.Fatal("CSV safety or encoding failed")
	}
	repository.failure = buyerblock.ErrNotFound
	// err 仓储拒绝拥有关系时不能创建任何范围名单。
	if _, err := service.FromOrder(ctx, 1, "missing", "user", ""); !errors.Is(err, buyerblock.ErrNotFound) {
		t.Fatal(err)
	}
	repository.scopeErr = errors.New("export scope fixture failure")
	// err 导出不能绕过列表的所有权核验。
	if _, err := service.ExportCSV(ctx, 1, "foreign"); !errors.Is(err, repository.scopeErr) {
		t.Fatal(err)
	}
}
