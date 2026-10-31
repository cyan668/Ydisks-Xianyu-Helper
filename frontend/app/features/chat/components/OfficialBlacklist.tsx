import { useEffect, useRef, useState } from 'react';
import { officialChatBlacklist } from '../api';

/** OfficialBlacklist 逐会话查询/拉黑/解除，不在挂载时请求，也不提供伪造的全量同步。 */
export function OfficialBlacklist({ accountID, chatID }: { /** accountID 是当前账号。 */ accountID: string; /** chatID 是其可见会话。 */ chatID: string }) {
  // open、setOpen 控制用户主动打开的操作界面。
  const [open, setOpen] = useState(false);
  // blocked、setBlocked 是平台明确状态，null 表示尚未查询或结果未知。
  const [blocked, setBlocked] = useState<boolean | null>(null);
  // message、setMessage 是安全反馈，不包含原始平台响应。
  const [message, setMessage] = useState('');
  // busy、setBusy 防止重复平台请求。
  const [busy, setBusy] = useState(false);
  // controller 由当前会话组件拥有，卸载取消并丢弃旧响应。
  const controller = useRef<AbortController | null>(null);
  useEffect(/* 不主动请求平台，仅在卸载时取消旧会话操作。 */ () => /* 已提交的平台变更无法通过本地取消撤回。 */ () => controller.current?.abort(), []);
  /** execute 每次明确点击只提交一次平台操作，变更前确认目标会话。 */
  const execute = async (action: 'query' | 'add' | 'remove') => {
    if (controller.current) return;
    if (action !== 'query' && !window.confirm(action === 'add' ? '将当前会话对方加入闲鱼官方黑名单？这会改变平台账号状态。' : '解除当前会话对方的闲鱼官方拉黑？这不会移除个人交易黑名单。')) return;
    // request 是本次操作独占的取消器，不做失败自动重试。
    const request = new AbortController(); controller.current = request; setBusy(true); setMessage('');
    try {
      // result 是此会话经平台确认的状态。
      const result = await officialChatBlacklist(accountID, chatID, action, { signal: request.signal });
      if (!request.signal.aborted) { setBlocked(result.blocked); setMessage(action === 'query' ? '已查询当前会话的平台状态。' : '平台已确认本次操作。'); }
    } catch (/* error 表示结果未确认，清除旧值后只允许用户手动查询。 */ error) { if (!request.signal.aborted) { setBlocked(null); setMessage(error instanceof Error ? error.message : '结果未确认，请手动查询状态'); } }
    finally { if (controller.current === request) { controller.current = null; setBusy(false); } }
  };
  return <><button type="button" className="ml-3 rounded-lg border border-gray-200 px-2 py-1 text-xs text-gray-600" onClick={/* 用户打开界面，不自动查询。 */ () => setOpen(true)}>闲鱼拉黑管理</button>
    {open && <div role="dialog" aria-label="闲鱼官方黑名单" aria-modal="true" className="fixed inset-0 z-[80] flex items-center justify-center bg-black/40 p-4"><section className="w-full max-w-md space-y-4 rounded-2xl bg-white p-6 shadow-xl">
      <h3 className="font-bold text-lg">当前会话的闲鱼官方黑名单</h3><p className="text-sm leading-6 text-gray-600">与个人交易名单独立。每次只操作当前会话，不遍历全部用户或自动同步。</p>
      <p className="text-sm font-bold">{blocked === null ? '尚未查询 / 结果待核对' : blocked ? '当前对方已被闲鱼拉黑' : '当前对方未被闲鱼拉黑'}</p>{message && <p role="status" className="text-sm">{message}</p>}
      <div className="flex flex-wrap gap-3"><button disabled={busy} onClick={/* 用户明确查询此会话。 */ () => void execute('query')} className="rounded-lg bg-gray-100 px-3 py-2">查询平台状态</button>{blocked !== null && <button disabled={busy} onClick={/* 依据明确状态提交操作，仍需要再次确认。 */ () => void execute(blocked ? 'remove' : 'add')} className="rounded-lg bg-red-50 px-3 py-2 text-red-700">{blocked ? '解除闲鱼拉黑' : '加入闲鱼黑名单'}</button>}<button className="ml-auto rounded-lg px-3 py-2" onClick={/* 关闭会丢弃旧响应，下次必须重新查询。 */ () => { controller.current?.abort(); controller.current = null; setOpen(false); setBusy(false); setBlocked(null); }}>关闭</button></div>
    </section></div>}
  </>;
}
