package adapter

import (
	"context"
	"testing"
	"xianyu-go/internal/db"
)

// officialRuntimeFake 只提供内存平台响应，不能连接闲鱼。
type officialRuntimeFake struct {
	orderRuntimeMTopFake // orderRuntimeMTopFake 保持原客户端合同。
	// calls 验证只有当前用户当前会话被请求一次。
	calls int
}

// OfficialBlacklistContext 记录一次会话操作并返回新虚构 Cookie，验证条件写回路径。
func (f *officialRuntimeFake) OfficialBlacklistContext(_ context.Context, _, _, action string) (bool, string, error) {
	f.calls++
	return action != "remove", "sid=official-fixture", nil
}

// TestOfficialBuyerBlockOwnershipAndCookie 验证非敏感归属查询先于凭据与平台操作，响应 Cookie 经标准条件写回。
func TestOfficialBuyerBlockOwnershipAndCookie(t *testing.T) {
	// store、cleanup 是隔离数据库及清理责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 只属于本地测试。
	ctx := context.Background()
	// owner、err 获取非敏感账号所有者。
	owner, err := store.Cookies.GetOwnerID(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	// err 准备当前账号的真实形状会话，不激活任何账号运行时。
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "cid", ChatID: "official-chat", BuyerID: "123", AccountRole: "seller", BuyerUserID: "123"}); err != nil {
		t.Fatal(err)
	}
	// client 是只记录调用的 MTOP 替身。
	client := &officialRuntimeFake{}
	// dependencies、err 是工厂所需的固定本地依赖。
	dependencies, err := NewAutomationDependencies(store)
	if err != nil {
		t.Fatal(err)
	}
	// platform 注入回环替身，不使用生产 MTOP。
	platform := dependencies.NewBuyerBlockPlatform(client, nil)
	// err 必须在越权查询时返回且不请求平台。
	if _, err := platform.Official(ctx, owner+100, "cid", "official-chat", "query"); err == nil || client.calls != 0 {
		t.Fatal("foreign caller reached platform")
	}
	// blocked、err 是拥有者的唯一平台请求结果。
	blocked, err := platform.Official(ctx, owner, "cid", "official-chat", "query")
	if err != nil || !blocked || client.calls != 1 {
		t.Fatalf("official result: %v", err)
	}
	// saved、err 验证未丢失平台响应凭据，不输出内容到失败日志。
	saved, err := store.Cookies.GetValue(ctx, "cid")
	if err != nil || saved != "sid=official-fixture" {
		t.Fatal("official response cookie not persisted")
	}
	// err 清除隔离账号的凭证，验证新增平台能力不会发送无凭据请求。
	if err := store.Cookies.UpdateValueOwned(ctx, "cid", "", owner); err != nil {
		t.Fatal(err)
	}
	// err 必须明确拒绝空凭证；平台调用数保持此前的一次。
	if _, err := platform.Official(ctx, owner, "cid", "official-chat", "query"); err == nil || client.calls != 1 {
		t.Fatal("empty credentials reached platform")
	}
}
