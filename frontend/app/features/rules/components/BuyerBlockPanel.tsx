import { emptyBuyerBlock, useBuyerBlocks } from '../buyerBlocks';
import { useState } from 'react';
import BuyerBlockTable from './BuyerBlockTable';

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
  // searchInput、setSearchInput 只保存输入框草稿，按“搜索”才查询。
  const [searchInput, setSearchInput] = useState('');
  return <section className="rounded-xl border border-gray-100 bg-white p-6 space-y-5">
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div><h2 className="font-bold text-lg text-gray-900">个人交易黑名单</h2><p className="mt-1 text-sm leading-6 text-gray-600">命中后不取卡、不发货。闲鱼官方会话拉黑是另一项操作，可在在线聊天中手动查询、拉黑或解除。</p></div>
      <div className="flex gap-3"><button className="rounded-xl px-4 py-2.5 text-sm text-gray-600 bg-gray-100" disabled={blocks.busy} onClick={blocks.refresh}>刷新黑名单</button><button className="ios-btn-primary rounded-xl px-4 py-2.5 text-sm font-bold" disabled={blocks.busy} onClick={/* 用户打开停用的新草稿，关单需要单独勾选。 */ () => blocks.setDraft(emptyBuyerBlock(accountID))}>新增黑名单</button></div>
    </header>
    <div className="flex flex-wrap items-center gap-3">
      <form className="flex flex-1 gap-2" onSubmit={/* event 提交搜索词，不在每次按键时请求。 */ event => { event.preventDefault(); blocks.applySearch(searchInput); }}><input aria-label="搜索黑名单" className="ios-input min-w-0 flex-1 rounded-lg px-3 py-2 text-sm" placeholder="买家 ID、昵称或原因" value={searchInput} onChange={/* event 仅更新输入草稿。 */ event => setSearchInput(event.target.value)} /><button disabled={blocks.busy} className="rounded-lg bg-gray-100 px-4 text-sm">搜索</button></form>
      <label className="cursor-pointer rounded-lg bg-gray-100 px-4 py-2 text-sm">导入 Auto Replay 表格<input aria-label="导入黑名单表格" type="file" accept=".xlsx,.csv,.tsv" className="hidden" disabled={blocks.busy} onChange={/* event 提取用户明确选择的文件，重置控件以支持同文件重试。 */ event => { const file = event.target.files?.[0]; /* file 是本次独占的用户上传，不写浏览器持久存储。 */ event.target.value = ''; if (file) void blocks.importFile(file); }} /></label>
      <button disabled={blocks.busy} className="rounded-lg bg-gray-100 px-4 py-2 text-sm" onClick={/* 用户明确下载当前账号范围的个人配置。 */ () => void blocks.exportFile()}>导出 CSV</button>
    </div>
    <p className="text-sm leading-6 text-gray-500">支持原系统“账号ID、买家ID、买家昵称、商品ID、拉黑原因、是否启用”列。导入保留启停状态，重复记录跳过不覆盖；缺少启用或关单列时默认关闭。单次最多 500 条。</p>
    {blocks.selected.length > 0 && <div className="flex flex-wrap items-center gap-3 rounded-lg bg-blue-50 p-3 text-sm"><span>已选 {blocks.selected.length} 条（仅本页）</span><button disabled={blocks.busy} onClick={/* 用户明确启用所选个人名单，不启用账号或平台关单。 */ () => void blocks.batch('enable')}>批量启用</button><button disabled={blocks.busy} onClick={/* 用户停用所选个人名单。 */ () => void blocks.batch('disable')}>批量停用</button><button disabled={blocks.busy} className="text-red-600" onClick={/* 批量删除必须明确二次确认，关单占用不删除。 */ () => { if (window.confirm(`删除选中的 ${blocks.selected.length} 条个人名单？`)) void blocks.batch('delete'); }}>批量删除</button></div>}
    {blocks.message && <p role={blocks.hasError ? 'alert' : 'status'} className={`text-sm ${blocks.hasError ? 'text-red-600' : 'text-emerald-700'}`}>{blocks.message}</p>}
    {draft && <form className="grid gap-4 rounded-xl bg-gray-50 p-5 sm:grid-cols-2" onSubmit={/* event 阻止浏览器导航，交给 Hook 保存。 */ event => { event.preventDefault(); void blocks.save(); }}>
      <fieldset disabled={blocks.busy} className="contents">
      <label className="text-sm font-bold">买家 ID<input required value={draft.buyer_id} onChange={/* event 更新稳定买家标识。 */ event => blocks.setDraft({ ...draft, buyer_id: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3" placeholder="买家 ID，不是昵称" /></label>
      <label className="text-sm font-bold">买家昵称（仅展示）<input maxLength={100} value={draft.buyer_nick || ''} onChange={/* event 更新展示昵称，不影响匹配键。 */ event => blocks.setDraft({ ...draft, buyer_nick: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3" /></label>
      <label className="text-sm font-bold">适用账号<select value={draft.cookie_id} onChange={/* event 切换账号时清空商品，避免跨账号沿用旧商品。 */ event => blocks.setDraft({ ...draft, cookie_id: event.target.value, item_id: '' })} className="ios-input mt-2 w-full rounded-lg p-3"><option value="">我的全部账号</option>{accounts.map(/* account 是一个可见账号摘要。 */ account => <option key={account.id} value={account.id}>{account.label}</option>)}</select></label>
      <label className="text-sm font-bold">商品 ID（留空为全部）<input disabled={!draft.cookie_id} value={draft.item_id} onChange={/* event 更新所选账号下的商品范围。 */ event => blocks.setDraft({ ...draft, item_id: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3 disabled:opacity-50" /></label>
      <label className="text-sm font-bold">管理备注<input maxLength={500} value={draft.reason} onChange={/* event 更新仅供管理者查看的备注。 */ event => blocks.setDraft({ ...draft, reason: event.target.value })} className="ios-input mt-2 w-full rounded-lg p-3" /></label>
      <label className="flex gap-2 items-center text-sm"><input type="checkbox" checked={draft.enabled} onChange={/* event 明确选择是否启用交易拦截。 */ event => blocks.setDraft({ ...draft, enabled: event.target.checked })} />启用交易拦截</label>
      <label className="flex gap-2 items-center text-sm"><input type="checkbox" checked={draft.auto_close} onChange={/* event 显式授权系统事件命中后的一次关单。 */ event => blocks.setDraft({ ...draft, auto_close: event.target.checked })} />命中时尝试关闭未发货订单</label>
      {draft.auto_close && <p className="sm:col-span-2 text-sm leading-6 text-amber-800">关单会改变真实交易。每单最多请求一次；已有发送内容或结果不确定时停止并通知人工核对，不自动重复关单。</p>}
      <div className="sm:col-span-2 flex justify-end gap-3"><button type="button" disabled={blocks.busy} className="rounded-xl px-4 py-2 text-sm bg-white" onClick={/* 用户放弃未保存草稿，不影响已保存记录。 */ () => blocks.setDraft(null)}>取消</button><button disabled={blocks.busy} className="ios-btn-primary rounded-xl px-5 py-2 text-sm font-bold">{blocks.busy ? '保存中…' : '保存黑名单'}</button></div>
      </fieldset>
    </form>}
    {blocks.loading ? <p className="text-sm text-gray-500">正在读取黑名单…</p> : <BuyerBlockTable rows={blocks.rows} selected={blocks.selected} busy={blocks.busy} select={blocks.setSelected} edit={blocks.setDraft} remove={/* id 为用户确认删除的主键。 */ id => void blocks.remove(id)} accountLabel={/* id 是当前记录所属账号。 */ id => accounts.find(/* account 匹配展示名。 */ account => account.id === id)?.label || id} />}
    <footer className="flex items-center justify-between text-sm text-gray-600"><span>共 {blocks.total} 条 · 第 {blocks.page} 页</span><div className="flex gap-4"><button disabled={blocks.busy || blocks.loading || blocks.page <= 1} onClick={/* 用户读取上一页。 */ () => blocks.setPage(blocks.page - 1)}>上一页</button><button disabled={blocks.busy || blocks.loading || blocks.page * 20 >= blocks.total} onClick={/* 用户读取下一页。 */ () => blocks.setPage(blocks.page + 1)}>下一页</button></div></footer>
  </section>;
}
