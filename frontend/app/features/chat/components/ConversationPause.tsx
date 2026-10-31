import { useConversationPause } from '../conversationPause';

/** ConversationPause 在会话工具栏显示人工接管和明确恢复入口，保留人工输入和订单动作。 */
export function ConversationPause({ accountID, chatID }: { /** accountID 是当前选择账号。 */ accountID: string; /** chatID 是当前会话。 */ chatID: string }) {
  // pause 拥有当前会话的本地读取、取消与恢复状态。
  const pause = useConversationPause(accountID, chatID);
  return <div className="flex flex-1 flex-wrap items-center gap-3 text-sm">
    <span className={pause.paused ? 'font-medium text-amber-800' : 'text-gray-500'}>{pause.state === null ? '接管状态待查询' : pause.paused ? `人工接管中 · 自动回复暂停至 ${new Date(pause.state.paused_until).toLocaleTimeString()}` : '自动回复未因人工接管暂停'}</span>
    {pause.paused && <button disabled={pause.busy} className="rounded-lg bg-amber-50 px-3 py-1.5 text-amber-800 disabled:opacity-50" onClick={/* 用户明确恢复此会话，服务端会比较截止时间。 */ () => void pause.resume()}>{pause.busy ? '正在恢复…' : '恢复自动回复'}</button>}
    <button disabled={pause.busy} className="text-xs text-gray-500 underline" onClick={/* 用户手动读取当前会话状态，无平台请求。 */ () => void pause.refresh()}>刷新接管状态</button>
    {pause.error && <span role="alert" className="text-red-600">{pause.error}</span>}
  </div>;
}
