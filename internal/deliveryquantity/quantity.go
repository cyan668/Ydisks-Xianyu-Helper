// Package deliveryquantity 统一规则保存、发货及历史补发使用的数量策略，不访问库存或平台。
package deliveryquantity

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Mode 表示配置份数的计量单位，随动作配置一起冻结到运行快照。
type Mode string

const (
	// PerItem 保持历史行为：配置份数乘以订单购买件数。
	PerItem Mode = "per_item"
	// PerOrder 表示整笔订单固定配置份数，不随购买件数增加。
	PerOrder Mode = "per_order"
)

// Parse 从 config 的 JSON 对象读取策略；旧配置未提供字段时按件，非法显式值返回错误。
// 错误不包含配置正文，避免把同一对象中的私有自定义变量写入日志。
func Parse(config string) (Mode, error) {
	if strings.TrimSpace(config) == "" {
		return PerItem, nil
	}
	// object 仅解码策略字段，保留其它动作配置由其原有职责处理。
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &object); err != nil || object == nil { // err 表示非 JSON 对象或损坏配置。
		return "", errors.New("发货数量配置必须是 JSON 对象")
	}
	// raw、exists 区分历史字段缺省与错误的 null、空字符串等显式值。
	raw, exists := object["quantity_mode"]
	if !exists {
		return PerItem, nil
	}
	// mode 是经过枚举校验后才允许参与取卡计数的值。
	var mode Mode
	if err := json.Unmarshal(raw, &mode); err != nil || (mode != PerItem && mode != PerOrder) { // err 表示策略不是字符串。
		return "", errors.New("发货数量策略只支持 per_item 或 per_order")
	}
	return mode, nil
}

// Count 按 config 策略、订单原始数量 quantity 和配置份数 units 计算取卡总数。
// 缺省/非正份数与历史非法订单数量仍按一份兼容；上层付款事实门禁不受此函数影响。
func Count(config, quantity string, units int) (int, error) {
	// mode、err 保存经过校验的策略及拒绝原因，错误时不得取卡或发消息。
	mode, err := Parse(config)
	if err != nil {
		return 0, err
	}
	if units <= 0 {
		units = 1
	}
	if mode == PerOrder {
		return units, nil
	}
	// multiplier、parseErr 兼容历史任务数量缺省；真实订单有效性由原有准备阶段核验。
	multiplier, parseErr := strconv.Atoi(strings.TrimSpace(quantity))
	if parseErr != nil || multiplier <= 0 {
		multiplier = 1
	}
	if units > int(^uint(0)>>1)/multiplier {
		return 0, errors.New("发货份数乘购买数量超出可表示范围")
	}
	return units * multiplier, nil
}
