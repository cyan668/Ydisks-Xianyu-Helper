/** 动作配置中与规则自定义变量相关的最小读取结构。 */
interface CustomVariableConfig {
  /** 传给发货模板的自定义字符串键值表。 */
  custom_variables?: unknown;
}

// parseCustomVariables 从动作配置中恢复规则页保存的自定义字符串键值表。
export const parseCustomVariables = (raw?: string): Record<string, string> => {
  // config 保存动作配置 JSON 对象。
  let config: unknown;
  try {
    config = JSON.parse(raw || '{}');
  } catch {
    return {};
  }
  if (!config || typeof config !== 'object' || Array.isArray(config)) return {};
  // values 保存配置中的自定义变量候选值。
  const values = (config as CustomVariableConfig).custom_variables;
  if (values && typeof values === 'object' && !Array.isArray(values)) {
    // result 保存经过字符串过滤的自定义变量键值表。
    const result: Record<string, string> = {};
    Object.entries(values).forEach(/* entry 保存一个自定义变量键值对，用于拆分和过滤响应字段。 */ entry => {
      // key 保存当前自定义变量键。
      const key = entry[0];
      // value 保存当前自定义变量的字符串候选值。
      const value = entry[1];
      if (typeof value === 'string') result[key] = value;
    });
    return result;
  }
  if (Array.isArray(values)) {
    // legacyResult 保存历史数组格式转换后的兼容键值表。
    return Object.fromEntries(values.filter(/* value 是历史数组中的字符串候选值。 */ (value: unknown): value is string => typeof value === 'string').map(/* valueIndex 保存历史数组值及其兼容键名。 */ (value, valueIndex) => [String(valueIndex), value]));
  }
  return {};
};

