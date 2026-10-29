package automation

import (
	"context"
	"encoding/json"
	"testing"
)

// TestRuleQuantityModeValidation 验证规则应用入口拒绝未知策略，SKU 规范化不会丢掉固定份数策略。
func TestRuleQuantityModeValidation(t *testing.T) {
	// service 注入本地归属替身，不读凭据或连接平台。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{cardEnabled: true})
	for _, mode := range []string{"per_item", "per_order", "invalid"} { // mode 遍历合法枚举和明确非法值。
		// draft 仅以 JSON 扩展字段提供策略，旧接口结构不变。
		draft := RuleDraft{CookieID: "fixture", TriggerType: TriggerOrderPaid, Actions: []ActionDraft{{ActionType: ActionSendCard, CardID: 1, ConfigJSON: `{"quantity_mode":"` + mode + `","delay_override":true}`}}}
		// input、err 保存规范化结果与验证错误。
		input, err := service.Normalize(context.Background(), 1, draft)
		if mode == "invalid" {
			if err == nil {
				t.Fatal("invalid quantity mode accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		// config 回读 SKU 规范化后的配置，验证策略与延迟字段仍保留。
		var config map[string]any
		// err 检查归一化是否仍输出合法 JSON。
		if err := json.Unmarshal([]byte(input.Actions[0].ConfigJSON), &config); err != nil {
			t.Fatal(err)
		}
		if config["quantity_mode"] != mode || config["delay_override"] != true {
			t.Fatal("normalization lost action configuration")
		}
	}
}

// TestRuleQuantityTemplateModeValidation 验证模板变量份数与扩展字段保留，非法模式也不能通过模板保存入口。
func TestRuleQuantityTemplateModeValidation(t *testing.T) {
	// service 仅使用已启用模板和卡密摘要替身。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleTemplateOwnershipFake{ruleOwnershipFake: &ruleOwnershipFake{cardEnabled: true}, template: TemplateInfo{Enabled: true, Keys: []string{"main"}}})
	for _, mode := range []string{"per_item", "per_order", "invalid"} { // mode 验证模板与直接卡密使用同一枚举。
		// draft 外层份数保持历史字段，不能替代绑定的两份。
		draft := RuleDraft{CookieID: "fixture", TriggerType: TriggerOrderPaid, Actions: []ActionDraft{{ActionType: ActionSendTemplate, DeliveryTemplateID: 1, DeliveryCount: 99, ConfigJSON: `{"quantity_mode":"` + mode + `","future_field":true}`, TemplateBindings: []TemplateBinding{{VariableKey: "main", CardID: 1, DeliveryCount: 2}}}}}
		// input、err 分别为规范化动作及其校验结果。
		input, err := service.Normalize(context.Background(), 1, draft)
		if mode == "invalid" {
			if err == nil {
				t.Fatal("invalid template quantity mode accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		// config 验证模板自定义变量归一后仍保留策略和额外字段。
		var config map[string]any
		// err 表示模板动作配置无法解码。
		if err := json.Unmarshal([]byte(input.Actions[0].ConfigJSON), &config); err != nil {
			t.Fatal(err)
		}
		if config["quantity_mode"] != mode || config["future_field"] != true || input.Actions[0].TemplateBindings[0].DeliveryCount != 2 {
			t.Fatal("template normalization lost mode or binding units")
		}
	}
}
