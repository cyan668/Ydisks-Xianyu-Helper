// Package buyerblock 定义买家交易黑名单；它不代表平台会话拉黑，不读取账号凭据。
package buyerblock

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	// ErrBlocked 表示已命中交易黑名单，禁止取卡和发货。
	ErrBlocked = errors.New("买家在交易黑名单中，已阻止发货")
	// ErrClosed 表示该订单已由黑名单流程明确关单，停用规则也不能继续发货。
	ErrClosed = errors.New("订单已经由黑名单流程关闭，禁止发货")
	// ErrCloseUnknown 表示已有未确认的关单请求，必须人工核对而非继续发货。
	ErrCloseUnknown = errors.New("订单存在未确认的黑名单关单请求，请人工核对")
	// ErrCloseStateConflict 表示平台关单成功但本地订单已被并发删除或推进到其他终态，不能覆盖。
	ErrCloseStateConflict = errors.New("平台已确认关单，本地订单状态冲突，请人工核对")
	// ErrIdentityMissing 表示有相关黑名单但缺少订单身份，不能猜测放行。
	ErrIdentityMissing = errors.New("订单买家或商品身份不完整，无法核验交易黑名单")
	// ErrInvalid 表示输入不满足作用域或稳定身份约束。
	ErrInvalid = errors.New("交易黑名单参数无效")
	// ErrNotFound 表示记录不存在或不属于调用用户。
	ErrNotFound = errors.New("交易黑名单记录不存在")
	// ErrConflict 表示同用户、买家和作用域已有记录。
	ErrConflict = errors.New("该买家在此作用域已有黑名单记录")
)

// Entry 是黑名单记录；空 CookieID 表示用户级，空 ItemID 表示账号全部商品。
type Entry struct {
	// ID 是记录主键。
	ID int64
	// UserID 是所属本地用户，运行时按账号所有者隔离。
	UserID int64
	// CookieID 是限定账号；用户级记录为空。
	CookieID string
	// ItemID 是限定商品，只允许在指定账号后设置。
	ItemID string
	// BuyerID 是平台买家 ID，不使用可变昵称匹配。
	BuyerID string
	// Reason 是管理备注，不发送给买家或平台。
	Reason string
	// Enabled 控制是否参与匹配。
	Enabled bool
	// AutoClose 允许卖家订单系统事件命中时尝试一次关单。
	AutoClose bool
}

// NormalizeBuyerID 去掉 buyerID 空白和明确的 @goofish 后缀，拒绝其他域后缀和昵称式空白。
func NormalizeBuyerID(buyerID string) (string, error) {
	buyerID = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(buyerID), "@goofish"))
	if buyerID == "" || utf8.RuneCountInString(buyerID) > 64 || strings.ContainsAny(buyerID, "@ \t\r\n") {
		return "", ErrInvalid
	}
	return buyerID, nil
}
