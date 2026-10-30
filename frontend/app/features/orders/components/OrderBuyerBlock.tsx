import { useEffect, useRef, useState } from 'react';
import { addOrderBuyerBlock } from '../api';

/** OrderBuyerBlock 从已授权订单派生买家；用户必须明确选择范围，不立即关单或操作平台拉黑。 */
export function OrderBuyerBlock({ orderID, buyerID }: { /** orderID 是已授权订单。 */ orderID: string; /** buyerID 仅供确认提示，不提交给服务端。 */ buyerID: string }) {
  // open、setOpen 控制本订单的确认界面。
  const [open, setOpen] = useState(false);
  // level、setLevel 不预设作用域，防止一键操作扩大到所有账号。
  const [level, setLevel] = useState<'' | 'user' | 'account' | 'item'>('');
  // reason、setReason 是未保存的管理备注。
  const [reason, setReason] = useState('');
  // message、setMessage 显示实际新增或重复跳过结果。
  const [message, setMessage] = useState('');
  // busy、setBusy 防止重复提交。
  const [busy, setBusy] = useState(false);
  // done、setDone 在保存完成后只允许关闭。
  const [done, setDone] = useState(false);
  // controller 随组件按订单主键挂载，取消和卸载丢弃旧响应，不撤销已完成的本地写入。
  const controller = useRef<AbortController | null>(null);
  useEffect(/* 不在挂载时写入；卸载负责取消旧订单请求。 */ () => /* 清理在途响应。 */ () => controller.current?.abort(), []);
  /** close 放弃确认并取消在途反馈。 */
  const close = () => { controller.current?.abort(); controller.current = null; setOpen(false); setBusy(false); };
  /** submit 在用户明确选择范围后创建启用的个人名单，关单开关仍关闭。 */
  const submit = async () => {
    if (!level || controller.current || done) return;
    // request 标识本次写入，旧请求不能污染再次打开的确认界面。
    const request = new AbortController(); controller.current = request; setBusy(true); setMessage('');
    try {
      // result 为服务端从订单身份派生后的原子新增/去重结果。
      const result = await addOrderBuyerBlock(orderID, level, reason, { signal: request.signal });
      if (!request.signal.aborted) { setDone(true); setMessage(result.created ? '已加入个人黑名单，后续交易将被拦截。未启用自动关单。' : '此作用域已有记录，原配置保持不变。'); }
    } catch (/* error 为当前订单写入失败原因，不自动重复提交。 */ error) { if (!request.signal.aborted) setMessage(error instanceof Error ? error.message : '加入黑名单失败'); }
    finally { if (controller.current === request) { controller.current = null; setBusy(false); } }
  };
  return <><button type="button" disabled={!buyerID} className="rounded-xl bg-red-50 px-4 py-3 text-sm font-bold text-red-700 disabled:opacity-40" onClick={/* 打开此订单买家的明确范围确认。 */ () => { setOpen(true); setDone(false); setMessage(''); setLevel(''); }}>加入个人黑名单</button>
    {open && <div role="dialog" aria-modal="true" aria-label="加入个人黑名单" className="fixed inset-0 z-[80] flex items-center justify-center bg-black/40 p-4"><div className="w-full max-w-lg space-y-4 rounded-2xl bg-white p-6 text-left shadow-xl">
      <h3 className="text-lg font-bold">将买家 {buyerID} 加入个人黑名单</h3><p className="text-sm leading-6 text-gray-600">只影响后续交易发货，不会立即关单或操作闲鱼官方会话拉黑。</p>
      <label className="block text-sm">生效范围<select aria-label="黑名单生效范围" disabled={busy || done} value={level} onChange={/* event 只提供三个合法作用域。 */ event => setLevel(event.target.value as typeof level)} className="ios-input mt-2 w-full rounded-lg p-3"><option value="" disabled>请选择范围</option><option value="item">仅此账号的此商品</option><option value="account">此账号的全部商品</option><option value="user">我的全部账号</option></select></label>
      <label className="block text-sm">备注（可选）<input maxLength={500} disabled={busy || done} value={reason} onChange={/* event 更新待保存备注。 */ event => setReason(event.target.value)} className="ios-input mt-2 w-full rounded-lg p-3" /></label>
      {message && <p role="status" className="text-sm leading-6">{message}</p>}<div className="flex justify-end gap-3"><button onClick={close} className="rounded-lg bg-gray-100 px-4 py-2">{done ? '关闭' : '取消'}</button>{!done && <button onClick={/* 用户明确确认本地个人拉黑。 */ () => void submit()} disabled={!level || busy} className="rounded-lg bg-red-600 px-4 py-2 text-white disabled:opacity-40">{busy ? '正在保存…' : '确认加入个人黑名单'}</button>}</div>
    </div></div>}
  </>;
}
