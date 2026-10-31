import type { BuyerBlock } from '../buyerBlockModels';

/** BuyerBlockTableProps 只接收当前页和明确选中集合，不把全选扩展到其他页。 */
interface BuyerBlockTableProps {
  /** rows 是服务器返回的本页配置。 */
  rows: BuyerBlock[];
  /** selected 是本页勾选的主键。 */
  selected: number[];
  /** busy 阻止写入期间切换选择。 */
  busy: boolean;
  /** select 替换本页的明确主键集合。 */
  select: (ids: number[]) => void;
  /** edit 复制一条记录到临时表单。 */
  edit: (row: BuyerBlock) => void;
  /** remove 请求删除用户确认的记录。 */
  remove: (id: number) => void;
  /** accountLabel 将账号标识转换为用户可识别的名称。 */
  accountLabel: (id: string) => string;
}

/** BuyerBlockTable 展示昵称、范围和交易状态，勾选操作仅作用于当前页。 */
export default function BuyerBlockTable({ rows, selected, busy, select, edit, remove, accountLabel }: BuyerBlockTableProps) {
  return <div className="overflow-x-auto"><table className="w-full text-left text-sm">
    <thead className="text-gray-500"><tr>
      <th className="pr-4"><input aria-label="全选本页" type="checkbox" disabled={busy || !rows.length} checked={rows.length > 0 && rows.every(/* row 核验当前页是否全部勾选。 */ row => selected.includes(row.id))} onChange={/* event 只全选或清空当前页面。 */ event => select(event.target.checked ? rows.map(/* row 是本页主键来源。 */ row => row.id) : [])} /></th>
      <th className="py-3">买家 ID / 昵称</th><th>适用范围</th><th>状态</th><th>命中后关单</th><th className="text-right">操作</th>
    </tr></thead>
    <tbody className="divide-y divide-gray-100">{rows.map(/* row 是当前用户可见的本页记录。 */ row => <tr key={row.id}>
      <td className="pr-4"><input aria-label={`选择买家 ${row.buyer_id}`} type="checkbox" disabled={busy} checked={selected.includes(row.id)} onChange={/* event 切换当前主键，不改变其他行。 */ event => select(event.target.checked ? [...selected, row.id] : selected.filter(/* id 排除当前被取消的主键。 */ id => id !== row.id))} /></td>
      <td className="py-4 pr-3"><div className="font-bold">{row.buyer_id}</div>{row.buyer_nick && <div>{row.buyer_nick}</div>}{row.reason && <div className="mt-1 max-w-xs break-words text-gray-500">{row.reason}</div>}</td>
      <td className="pr-3">{row.cookie_id ? accountLabel(row.cookie_id) : '我的全部账号'}<div className="mt-1 text-gray-500">{row.item_id ? `商品 ${row.item_id}` : '全部商品'}</div></td>
      <td className="whitespace-nowrap pr-3">{row.enabled ? '已启用' : '已停用'}</td><td className="whitespace-nowrap pr-3">{row.auto_close ? '尝试一次' : '仅拦截'}</td>
      <td className="text-right whitespace-nowrap"><button disabled={busy} className="px-3 py-2" onClick={/* 用户编辑这条本地记录。 */ () => edit({ ...row })}>编辑</button><button disabled={busy} className="px-3 py-2 text-red-600" onClick={/* 删除只在用户确认后发送，历史关单保护保留。 */ () => { if (window.confirm(`删除买家 ${row.buyer_id} 的黑名单记录？`)) remove(row.id); }}>删除</button></td>
    </tr>)}</tbody>
  </table>{rows.length === 0 && <p className="py-10 text-center text-sm text-gray-500">当前筛选没有黑名单记录</p>}</div>;
}
