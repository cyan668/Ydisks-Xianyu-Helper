import { useReplyProtection } from '../replyProtection';
import ContentCooldownPanel from './ContentCooldownPanel';

/** ReplyProtectionPanel 展示彼此独立的接管与内容冷却设置，沿用规则页已有字号与间距。 */
export default function ReplyProtectionPanel({ accountID }: { /** accountID 为规则页选中的账号，空值不批量修改。 */ accountID: string }) {
  // protection 拥有本账号配置和取消责任。
  const protection = useReplyProtection(accountID);
  return <div className="space-y-6"><section className="rounded-xl border border-gray-100 bg-white p-6">
    <h2 className="text-lg font-bold text-gray-900">人工接管暂停</h2>
    <p className="mt-2 text-sm leading-6 text-gray-600">人工发送文字、图片或商品卡片后，暂停该会话的自动客服回复。其他会话和订单自动发货继续按原规则处理。</p>
    {!accountID ? <p className="mt-6 text-sm text-gray-500">请先选择一个账号。</p> : <form className="mt-6 max-w-xl space-y-4" onSubmit={/* event 只提交当前账号配置。 */ event => { event.preventDefault(); void protection.save(); }}>
      <label className="block text-sm font-bold text-gray-800">人工回复后暂停（分钟）
        <input type="number" min={0} max={1440} step={1} value={protection.minutes} disabled={!protection.ready || protection.busy} onChange={/* event 只更新分钟草稿，不立即开始接管。 */ event => protection.setMinutes(Number(event.target.value))} className="ios-input mt-2 block w-48 rounded-xl p-3" />
      </label>
      <p className="text-sm leading-6 text-gray-500">填 0 关闭，填 10 可沿用 Auto Replay 的 10 分钟设置。自动回复和自动发货消息不会触发或延长暂停；官方客户端消息须有可信来源和发送时间，历史回放不重新计时。</p>
      <button disabled={!protection.ready || protection.busy} className="ios-btn-primary rounded-xl px-5 py-3 text-sm font-bold disabled:opacity-50">{protection.loading ? '正在读取…' : protection.busy ? '正在保存…' : '保存回复保护'}</button>
      {!protection.loading && !protection.ready && <button type="button" className="ml-3 text-sm text-gray-600 underline" onClick={protection.reload}>重试读取</button>}
      {protection.message && <p role={protection.failed ? 'alert' : 'status'} className={`text-sm ${protection.failed ? 'text-red-600' : 'text-emerald-700'}`}>{protection.message}</p>}
    </form>}
  </section><ContentCooldownPanel accountID={accountID} /></div>;
}
