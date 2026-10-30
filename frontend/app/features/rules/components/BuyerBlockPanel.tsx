import { emptyBuyerBlock, useBuyerBlocks } from '../buyerBlocks';

/** BuyerBlockPanelProps 提供规则页当前筛选和账号展示名。 */
interface BuyerBlockPanelProps {
  /** accountID 是当前筛选；空串表示全部账号。 */
  accountID: string;
  /** accounts 只含非敏感账号 ID 和展示名，不接受凭据模型。 */
  accounts: Array<{ /** id 为账号标识。 */ id: string; /** label 为账号展示名。 */ label: string }>;
}

/** BuyerBlockPanel 使用既有规则页令牌，集中呈现交易拦截范围、启用状态和关单授权。 */
export default function BuyerBlockPanel({ accountID, accounts }: BuyerBlockPanelProps) {
  // blocks 管理请求取消、最新视图、编辑草稿及唯一写入状态。
  const blocks = useBuyerBlocks(accountID);
  // draft 是尚未保存的配置；切换账号会由 Hook 丢弃。
  const draft = blocks.draft;
  return <section className="rounded-xl border border-gray-100 bg-white p-6 space-y-5">
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div><h2 className="font-bold text-lg text-gray-900">买家交易黑名单</h2><p className="mt-1 text-sm leading-6 text-gray-600">命中后不取卡、不发货；关单失败也不会继续发卡。这里不改变平台会话拉黑或普通客服回复。</p></div>
      <div className="flex gap-3"><button className="rounded-xl px-4 py-2.5 text-sm text-gray-600 bg-gray-100" disabled={blocks.busy} onClick={blocks.refresh}>刷新黑名单</button><button className="ios-btn-primary rounded-xl px-4 py-2.5 text-sm font-bold" disabled={blocks.busy} onClick={/* 用户打开停用的新草稿，关单需要单独勾选。 */ () => blocks.setDraft(emptyBuyerBlock(accountID))}>新增黑名单</button></div>
    </header>
    {blocks.message && <p role="alert" className="text-sm text-red-600">{blocks.message}</p>}
    {draft && <form className="grid gap-4 rounded-xl bg-gray-50 p-5 sm:grid-cols-2" onSubmit={/* event 阻止浏览器导航，交给 Hook 保存。 */ event => { event.preventDefault(); void blocks.save(); }}>
      <label className="text-sm font-bold">买家 ID<input required value={draft.buyer_id} onChange={/* event 更新稳定买家标识。 */ event => blocks.setDraft({ ...draft, buyer_id: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3" placeholder="买家 ID，不是昵称" /></label>
      <label className="text-sm font-bold">适用账号<select value={draft.cookie_id} onChange={/* event 切换账号时清空商品，避免跨账号沿用旧商品。 */ event => blocks.setDraft({ ...draft, cookie_id: event.target.value, item_id: '' })} className="ios-input mt-2 w-full rounded-lg p-3"><option value="">我的全部账号</option>{accounts.map(/* account 是一个可见账号摘要。 */ account => <option key={account.id} value={account.id}>{account.label}</option>)}</select></label>
      <label className="text-sm font-bold">商品 ID（留空为全部）<input disabled={!draft.cookie_id} value={draft.item_id} onChange={/* event 更新所选账号下的商品范围。 */ event => blocks.setDraft({ ...draft, item_id: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3 disabled:opacity-50" /></label>
      <label className="text-sm font-bold">管理备注<input maxLength={500} value={draft.reason} onChange={/* event 更新仅供管理者查看的备注。 */ event => blocks.setDraft({ ...draft, reason: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3" /></label>
      <label className="flex gap-2 items-center text-sm"><input type="checkbox" checked={draft.enabled} onChange={/* event 明确选择是否启用交易拦截。 */ event => blocks.setDraft({ ...draft, enabled: event.target.checked })} />启用交易拦截</label>
      <label className="flex gap-2 items-center text-sm"><input type="checkbox" checked={draft.auto_close} onChange={/* event 显式授权系统事件命中后的一次关单。 */ event => blocks.setDraft({ ...draft, auto_close: event.target.checked })} />命中时尝试关闭未发货订单</label>
      {draft.auto_close && <p className="sm:col-span-2 text-sm leading-6 text-amber-800">关单会改变真实交易。每单最多请求一次；已有发送内容或结果不确定时停止并通知人工核对，不自动重复关单。</p>}
      <div className="sm:col-span-2 flex justify-end gap-3"><button type="button" disabled={blocks.busy} className="rounded-xl px-4 py-2 text-sm bg-white" onClick={/* 用户放弃未保存草稿，不影响已保存记录。 */ () => blocks.setDraft(null)}>取消</button><button disabled={blocks.busy} className="ios-btn-primary rounded-xl px-5 py-2 text-sm font-bold">{blocks.busy ? '保存中…' : '保存黑名单'}</button></div>
    </form>}
    {blocks.loading ? <p className="text-sm text-gray-500">正在读取黑名单…</p> : <div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead className="text-gray-500"><tr><th className="py-3">买家 ID</th><th>适用范围</th><th>状态</th><th>命中后关单</th><th className="text-right">操作</th></tr></thead><tbody className="divide-y divide-gray-100">{blocks.rows.map(/* row 是当前用户可见的一条规则。 */ row => <tr key={row.id}><td className="py-4"><div className="font-bold">{row.buyer_id}</div>{row.reason && <div className="mt-1 text-gray-500">{row.reason}</div>}</td><td>{row.cookie_id ? accounts.find(/* account 匹配可见账号展示名。 */ account => account.id === row.cookie_id)?.label || row.cookie_id : '我的全部账号'}<div className="mt-1 text-gray-500">{row.item_id ? `商品 ${row.item_id}` : '全部商品'}</div></td><td>{row.enabled ? '已启用' : '已停用'}</td><td>{row.auto_close ? '尝试一次' : '仅拦截'}</td><td className="text-right whitespace-nowrap"><button disabled={blocks.busy} className="px-3 py-2" onClick={/* 用户复制当前记录为临时编辑草稿。 */ () => blocks.setDraft({ ...row })}>编辑</button><button disabled={blocks.busy} className="px-3 py-2 text-red-600" onClick={/* 用户确认后删除规则，历史关单保护仍保留。 */ () => { if (window.confirm(`删除买家 ${row.buyer_id} 的黑名单记录？`)) void blocks.remove(row.id); }}>删除</button></td></tr>)}</tbody></table>{blocks.rows.length === 0 && <p className="py-10 text-center text-sm text-gray-500">暂无黑名单。新增记录后，明确选择是否启用拦截与关单。</p>}</div>}
  </section>;
}
