package server

import (
	"context"
	blockapp "xianyu-go/internal/application/buyerblock"
	"xianyu-go/internal/buyerblock"
)

// BuyerBlocksPort 是用户隔离的交易黑名单管理用例，运行时发货门禁不经由 HTTP。
type BuyerBlocksPort interface {
	// List 按当前用户与可选账号查询。
	List(context.Context, int64, string) ([]buyerblock.Entry, error)
	// Save 按认证用户创建或更新 entry。
	Save(context.Context, int64, buyerblock.Entry) (buyerblock.Entry, error)
	// Delete 只删除当前用户的记录。
	Delete(context.Context, int64, int64) error
	// Page 返回用户隔离的筛选分页。
	Page(context.Context, int64, blockapp.Query) (blockapp.Page, error)
	// Import 原子导入完整记录，重复作用域不覆盖。
	Import(context.Context, int64, []buyerblock.Entry) (blockapp.BatchResult, error)
	// Batch 启用、停用或删除完整的所选主键集合。
	Batch(context.Context, int64, []int64, string) (int, error)
	// FromOrder 从归属已校验的订单提取身份，不能信任浏览器传来的买家 ID。
	FromOrder(context.Context, int64, string, string, string) (blockapp.BatchResult, error)
	// ExportCSV 导出当前用户的本地配置，不涉及平台同步。
	ExportCSV(context.Context, int64, string) ([]byte, error)
	// Official 每次只操作一个账号和会话的平台黑名单。
	Official(context.Context, int64, string, string, string) (bool, error)
}
