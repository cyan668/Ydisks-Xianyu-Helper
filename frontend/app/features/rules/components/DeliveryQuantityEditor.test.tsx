// @vitest-environment jsdom
import { useState } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test } from 'vitest';
import DeliveryQuantityEditor from './DeliveryQuantityEditor';
import TemplateVariantEditor from './TemplateVariantEditor';
import type { ShippingVariant } from '../models';

/** Harness 复用真实草稿更新接口，让测试通过用户选择观察份数标签和模板绑定行为。 */
function Harness({ template = false, invalid = false }: { /** template 选择直接卡券或模板变量场景。 */ template?: boolean; /** invalid 模拟历史非法策略，需要用户明确确认。 */ invalid?: boolean }) {
  // variant、setVariant 保存用户正在编辑的临时草稿，未保存时不会触达后端。
  const [variant, setVariant] = useState<ShippingVariant>({ spec_name: '', spec_value: '', card_id: 1, enabled: true, delivery_count: 2, delivery_mode: template ? 'template' : 'card', delivery_template_id: 9, template_bindings: [{ variable_key: 'main', card_id: 1, delivery_count: 2 }], quantity_mode_error: invalid ? '请确认历史策略' : undefined });
  // updateVariant 只覆盖用户实际修改的字段，策略变化不能重置已有份数。
  const updateVariant = (_index: number, patch: Partial<ShippingVariant>) => setVariant(/* current 保留其它未修改配置。 */ current => ({ ...current, ...patch }));
  return <>
    <DeliveryQuantityEditor index={0} variant={variant} updateVariant={updateVariant} />
    {template && <TemplateVariantEditor index={0} variant={variant} updateVariant={updateVariant} cards={[{ id: 1, name: '测试库存', type: 'data', enabled: true }]} deliveryTemplates={[{ id: 9, name: '测试模板', enabled: true, keys: ['main'], custom_keys: [], messages: [], created_at: '', updated_at: '' }]} />}
  </>;
}

afterEach(/* 每个交互用例结束后清理 DOM，防止残留标签影响下一例。 */ () => cleanup());

test('旧规则默认按件，切换按单保留份数并显示说明', /* 按真实选择器操作策略，而非检查源码字符串。 */ () => {
  render(<Harness />);
  expect((screen.getByLabelText('发货数量策略') as HTMLSelectElement).value).toBe('per_item');
  expect((screen.getByLabelText('每件份数') as HTMLInputElement).value).toBe('2');
  fireEvent.change(screen.getByLabelText('发货数量策略'), { target: { value: 'per_order' } });
  expect((screen.getByLabelText('每单份数') as HTMLInputElement).value).toBe('2');
  expect(screen.getByText('同一笔订单固定发送配置份数，不乘购买数量。')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('每单份数'), { target: { value: '3' } });
  fireEvent.change(screen.getByLabelText('发货数量策略'), { target: { value: 'per_item' } });
  expect((screen.getByLabelText('每件份数') as HTMLInputElement).value).toBe('3');
});

test('模板只编辑绑定份数，不再显示无效的动作份数', /* 策略作用于模板的每个变量，避免二次乘法误导。 */ () => {
  render(<Harness template />);
  expect(screen.getAllByRole('spinbutton')).toHaveLength(1);
  fireEvent.change(screen.getByLabelText('发货数量策略'), { target: { value: 'per_order' } });
  expect((screen.getByLabelText('main 每单份数') as HTMLInputElement).value).toBe('2');
  fireEvent.change(screen.getByLabelText('main 每单份数'), { target: { value: '4' } });
  expect((screen.getByLabelText('main 每单份数') as HTMLInputElement).value).toBe('4');
});

test('非法历史策略显示待确认，显式选择后才能恢复有效说明', /* 防止列表隔离错误后把未知值伪装成按件默认。 */ () => {
  render(<Harness invalid />);
  expect((screen.getByLabelText('发货数量策略') as HTMLSelectElement).value).toBe('');
  expect(screen.getByRole('alert').textContent).toBe('请确认历史策略');
  fireEvent.change(screen.getByLabelText('发货数量策略'), { target: { value: 'per_order' } });
  expect(screen.queryByRole('alert')).toBeNull();
  expect((screen.getByLabelText('每单份数') as HTMLInputElement).value).toBe('2');
});
