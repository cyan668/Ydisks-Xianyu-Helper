import type { DeliveryQuantityMode } from './models';

/** actionConfig 读取动作扩展对象；损坏配置不静默覆盖为缺省数量。 */
export const actionConfig = (raw?: string): Record<string, unknown> => {
  // config 保留规格、延迟、自定义变量及未来扩展字段。
  const config: unknown = JSON.parse(raw || '{}');
  if (!config || typeof config !== 'object' || Array.isArray(config)) throw new Error('动作配置必须是 JSON 对象');
  return config as Record<string, unknown>;
};

/** quantityModeFromConfig 仅字段缺省时沿用按件；未知策略阻止编辑器误显示为另一种策略。 */
export const quantityModeFromConfig = (raw?: string): DeliveryQuantityMode => {
  // mode 是服务端持久化的数量计量方式，必须与后台枚举一致。
  const mode = actionConfig(raw).quantity_mode;
  if (mode === undefined) return 'per_item';
  if (mode === 'per_item' || mode === 'per_order') return mode;
  throw new Error('不支持的发货数量策略，请核对规则配置');
};
