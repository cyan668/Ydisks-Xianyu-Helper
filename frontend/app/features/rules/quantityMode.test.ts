import { describe, expect, test, vi } from 'vitest';
import { actionConfig, quantityModeFromConfig } from './quantityMode';

// clientMock 返回内存 DTO 并记录写入请求，不发送真实网络请求。
const clientMock = vi.hoisted(/* 初始化隔离的 GET/PUT 请求替身。 */ () => ({ GET: vi.fn(), PUT: vi.fn() }));
vi.mock('../../../shared/api-contract/client', /* 用契约层替身验证真实规则 adapter 的往返映射。 */ () => ({
  contractClient: clientMock,
  runContractRequest: /* execute 是 adapter 构造的可取消请求；测试只返回内存 data。 */ async (execute: (signal: AbortSignal) => Promise<{ /** data 保存替身成功响应。 */ data: unknown }>) => (await execute(new AbortController().signal)).data,
}));
import { getShippingRules, updateShippingRule } from './api';

describe('发货数量策略', /* 对默认、非法值和编辑往返执行行为断言。 */ () => {
  test('缺省保持按件，只接受明确枚举', /* 无效服务端配置不能静默显示成按件。 */ () => {
    expect(quantityModeFromConfig()).toBe('per_item');
    expect(quantityModeFromConfig('{}')).toBe('per_item');
    expect(quantityModeFromConfig('{"quantity_mode":"per_item"}')).toBe('per_item');
    expect(quantityModeFromConfig('{"quantity_mode":"per_order"}')).toBe('per_order');
    for (const /* raw 是不应被隐藏的损坏配置。 */ raw of ['null', '[]', '{', '{"quantity_mode":null}', '{"quantity_mode":"other"}']) {
      expect(/* 解析失败应阻止错误策略进入草稿。 */ () => quantityModeFromConfig(raw)).toThrow();
    }
    expect(actionConfig('{"custom_variables":{"note":"fixture"}}')).toEqual({ custom_variables: { note: 'fixture' } });
  });

  test('卡券和模板编辑往返保留数量策略、规格与自定义字段', /* 覆盖载入、变更、保存及旧配置缺省，不连接真实服务。 */ async () => {
    for (const /* actionType 遍历两种共用数量策略的动作。 */ actionType of ['send_card', 'send_template']) {
      clientMock.GET.mockResolvedValue({ data: [{ id: 1, cookie_id: 'fixture', item_id: 'item', enabled: false, actions: [{ id: 2, action_type: actionType, card_id: 7, delivery_count: 2, enabled: true, config_json: '{"quantity_mode":"per_order","spec_name":"套餐","spec_value":"A","future_field":true,"custom_variables":{"note":"fixture"}}', template_bindings: [{ variable_key: 'main', card_id: 7, delivery_count: 2 }] }] }] });
      clientMock.PUT.mockResolvedValue({ data: { success: true } });
      // rules 是实际 adapter 解码的页面草稿，而非直接拼造写请求。
      const rules = await getShippingRules();
      expect(rules[0].variants?.[0].quantity_mode).toBe('per_order');
      await updateShippingRule(rules[0]);
      // payload 是真实 adapter 构造的最末次规则保存对象。
      const payload = clientMock.PUT.mock.calls.at(-1)?.[1].body;
      expect(JSON.parse(payload.actions[0].config_json)).toMatchObject({ quantity_mode: 'per_order', spec_name: '套餐', spec_value: 'A', future_field: true, custom_variables: { note: 'fixture' } });
      expect(payload.enabled).toBe(false);
      expect(payload.actions[0].delivery_count).toBe(2);
      // variant 指向草稿中的同一动作；切回按件必须显式保存新值。
      const variant = rules[0].variants![0];
      variant.quantity_mode = 'per_item';
      await updateShippingRule(rules[0]);
      expect(JSON.parse(clientMock.PUT.mock.calls.at(-1)?.[1].body.actions[0].config_json).quantity_mode).toBe('per_item');
    }
    clientMock.GET.mockResolvedValue({ data: [{ id: 1, actions: [{ action_type: 'send_card', config_json: '{}' }] }] });
    expect((await getShippingRules())[0].variants?.[0].quantity_mode).toBe('per_item');
  });

  test('单条坏配置不阻断其它规则，未确认的非法策略不能默存为按件', /* 列表隔离损坏 JSON 和未知枚举，并在实际 adapter 保存时拒绝未修复配置。 */ async () => {
    for (const /* raw 遍历会使旧数据解析失败的配置形状。 */ raw of ['{', 'null', '[]', '{"quantity_mode":"future","keep":true}']) {
      clientMock.GET.mockResolvedValue({ data: [
        { id: 1, actions: [{ action_type: 'send_card', config_json: raw }] },
        { id: 2, actions: [{ action_type: 'send_card', config_json: '{}' }] },
      ] });
      clientMock.PUT.mockClear();
      // rules 包含异常规则和仍可正常编辑的其它规则。
      const rules = await getShippingRules();
      expect(rules).toHaveLength(2);
      expect(rules[0].variants?.[0]).toMatchObject({ config_json: raw, quantity_mode_error: expect.any(String) });
      expect(rules[0].variants?.[0].quantity_mode).toBeUndefined();
      await expect(updateShippingRule(rules[0])).rejects.toThrow();
      expect(clientMock.PUT).not.toHaveBeenCalled();
      await updateShippingRule(rules[1]);
      expect(clientMock.PUT).toHaveBeenCalledOnce();
      if (raw.includes('future')) {
        rules[0].variants![0].quantity_mode = 'per_order';
        await updateShippingRule(rules[0]);
        expect(JSON.parse(clientMock.PUT.mock.calls.at(-1)?.[1].body.actions[0].config_json)).toMatchObject({ quantity_mode: 'per_order', keep: true });
      }
    }
  });
});
