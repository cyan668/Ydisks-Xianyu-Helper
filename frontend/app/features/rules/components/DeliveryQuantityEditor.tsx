import type { ShippingVariant } from '../models';

/** DeliveryQuantityEditorProps 只更新当前动作的数量策略与份数，不改变订单原始数量。 */
interface DeliveryQuantityEditorProps {
  /** index 是当前发货内容在草稿中的位置。 */
  index: number;
  /** variant 包含当前策略与卡券/模板发货方式。 */
  variant: ShippingVariant;
  /** updateVariant 把用户选择写回外层草稿，取消弹窗时由原有状态管理丢弃。 */
  updateVariant: (index: number, patch: Partial<ShippingVariant>) => void;
}

/** DeliveryQuantityEditor 让普通卡券和模板绑定共享明确的按件/按单计量，旧草稿默认按件。 */
export default function DeliveryQuantityEditor({ index, variant, updateVariant }: DeliveryQuantityEditorProps) {
  // mode 在历史配置损坏时显示待确认项，只有真正缺省才沿用按件。
  const mode = variant.quantity_mode_error ? '' : variant.quantity_mode ?? 'per_item';
  return <>
    <label className="block min-w-0 text-xs font-bold text-gray-600">
      发货数量策略
      <select value={mode} className="mt-2 w-full ios-input px-3 py-2.5 rounded-lg"
        onChange={/* event 只接受选择器提供的两种合法策略，显式选择后清除数量策略告警。 */ event => updateVariant(index, { quantity_mode: event.target.value === 'per_order' ? 'per_order' : 'per_item', quantity_mode_error: undefined })}>
        {variant.quantity_mode_error && <option value="" disabled>配置无效，请选择</option>}
        <option value="per_item">按购买数量</option>
        <option value="per_order">每单固定份数</option>
      </select>
    </label>
    {variant.delivery_mode !== 'template' && <label className="block text-xs font-bold text-gray-600">
      {mode === 'per_order' ? '每单份数' : '每件份数'}
      <input type="number" min="1" max="100" value={variant.delivery_count}
        onChange={/* event 更新本动作的配置份数，最少为一份。 */ event => updateVariant(index, { delivery_count: Math.max(1, Number(event.target.value) || 1) })}
        className="mt-2 w-full ios-input px-3 py-2.5 rounded-lg" />
    </label>}
    {variant.quantity_mode_error ? <p role="alert" className="md:col-span-full text-sm leading-6 text-red-600">{variant.quantity_mode_error}</p> : <p className="md:col-span-full text-sm leading-6 text-gray-500">
      {mode === 'per_order' ? '同一笔订单固定发送配置份数，不乘购买数量。' : '实际发货份数 = 购买数量 × 配置份数。'}
      {variant.delivery_mode === 'template' ? ' 此策略应用于每个模板变量的卡密份数。' : ''}
    </p>}
  </>;
}
