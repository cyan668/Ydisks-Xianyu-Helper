package deliveryquantity

import "testing"

// TestCountModesAndValidation 覆盖缺省兼容、按单固定、非法配置与溢出；t 不使用平台或库存。
func TestCountModesAndValidation(t *testing.T) {
	// cases 为数量策略的确定性边界，bad 表示必须拒绝而非返回默认份数。
	cases := []struct {
		// name、config、quantity 分别是场景名、动作配置及订单购买数量。
		name, config, quantity string
		// units、want 分别是配置份数与期望实际份数。
		units, want int
		// bad 标记无效策略或溢出，不能产生发货义务。
		bad bool
	}{
		{"legacy", `{}`, "3", 2, 6, false},
		{"missing-config", "", "2", 0, 2, false},
		{"per-item", `{"quantity_mode":"per_item"}`, " 3 ", 2, 6, false},
		{"per-order", `{"quantity_mode":"per_order"}`, "3", 2, 2, false},
		{"per-order-default-units", `{"quantity_mode":"per_order"}`, "9", 0, 1, false},
		{"legacy-invalid-order", `{}`, "bad", 0, 1, false},
		{"legacy-negative-order", `{}`, "-1", -2, 1, false},
		{"unknown-mode", `{"quantity_mode":"other"}`, "1", 1, 0, true},
		{"null-mode", `{"quantity_mode":null}`, "1", 1, 0, true},
		{"number-mode", `{"quantity_mode":2}`, "1", 1, 0, true},
		{"empty-mode", `{"quantity_mode":""}`, "1", 1, 0, true},
		{"null-config", `null`, "1", 1, 0, true},
		{"array-config", `[]`, "1", 1, 0, true},
		{"broken-config", `{`, "1", 1, 0, true},
		{"overflow", `{}`, "2", int(^uint(0) >> 1), 0, true},
	}
	for _, scenario := range cases { // scenario 是一次独立数量计算，不与其它用例共享状态。
		t.Run(scenario.name, func(t *testing.T) { // t 管理当前计数与错误断言。
			// got、err 保存实际份数和校验结果。
			got, err := Count(scenario.config, scenario.quantity, scenario.units)
			if (err != nil) != scenario.bad || got != scenario.want {
				t.Fatalf("count=%d want=%d error=%v", got, scenario.want, err)
			}
		})
	}
}
