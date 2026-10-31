import { useContentCooldown, useContentCooldownReviews } from '../contentCooldown';

/** ContentCooldownPanel 在回复保护中提供独立秒数配置和用户明确核对后的本地恢复入口。 */
export default function ContentCooldownPanel({ accountID }: { /** accountID 是当前规则页明确选中的账号。 */ accountID: string }) {
  // settings 拥有秒数草稿和配置请求取消。
  const settings = useContentCooldown(accountID);
  // reviews 拥有本地待核对分页及明确恢复快照。
  const reviews = useContentCooldownReviews(accountID);
  return <section className="rounded-xl border border-gray-100 bg-white p-6">
    <h2 className="text-lg font-bold text-gray-900">相同消息冷却</h2>
    <p className="mt-2 text-sm leading-6 text-gray-600">同一账号的同一会话中，买家再次发送完全相同的文字时，在冷却时间内跳过自动回复。</p>
    {!accountID ? <p className="mt-6 text-sm text-gray-500">请先选择一个账号。</p> : <>
      <form className="mt-6 max-w-xl space-y-4" onSubmit={/* event 只提交独立秒数配置。 */ event => { event.preventDefault(); void settings.save(); }}>
        <label className="block text-sm font-bold text-gray-800">相同消息等待时间（秒）
          <input type="number" min={0} max={86400} step={1} value={settings.seconds} disabled={!settings.ready || settings.busy} onChange={/* event 保留原始草稿，空值不自动写零。 */ event => settings.setSeconds(event.target.value)} className="ios-input mt-2 block w-48 rounded-xl p-3" />
        </label>
        <p className="text-sm leading-6 text-gray-500">填 0 关闭，填 3600 可沿用 Auto Replay 的一小时设置。成功回复后开始计时；没有回复或明确发送失败不占用冷却时间。不同文字、会话和账号分别处理。</p>
        <button disabled={!settings.ready || !settings.valid || settings.busy} className="ios-btn-primary rounded-xl px-5 py-3 text-sm font-bold disabled:opacity-50">{settings.loading ? '正在读取…' : settings.busy ? '正在保存…' : '保存消息冷却'}</button>
        {!settings.loading && !settings.ready && <button type="button" className="ml-3 text-sm text-gray-600 underline" onClick={settings.reload}>重试读取冷却</button>}
        {settings.feedback && <p role={settings.failed ? 'alert' : 'status'} className={`text-sm ${settings.failed ? 'text-red-600' : 'text-emerald-700'}`}>{settings.feedback}</p>}
      </form>
      <div className="mt-8 border-t border-gray-100 pt-6">
        <div className="flex items-center justify-between gap-4"><h3 className="font-bold text-gray-900">发送结果待核对{reviews.page ? `（${reviews.page.total}）` : ''}</h3><button type="button" disabled={reviews.loading || reviews.busy} onClick={reviews.reload} className="text-sm text-gray-600 underline disabled:opacity-50">刷新待核对</button></div>
        <p className="mt-2 text-sm leading-6 text-gray-500">结果未知时会继续保护该条重复消息，关闭冷却也会保留。请先到闲鱼核对回复是否送达，再解除；解除后不补发旧消息。</p>
        {reviews.loading && <p role="status" className="mt-4 text-sm text-gray-500">正在读取待核对记录…</p>}
        {reviews.error && <p role="alert" className="mt-4 text-sm text-red-600">{reviews.error}</p>}
        {reviews.notice && <p role="status" className="mt-4 text-sm text-emerald-700">{reviews.notice}</p>}
        {reviews.page && <><ul className="mt-4 divide-y divide-gray-100">{reviews.page.items.map(/* review 是本账号待核对保护快照。 */ review => <li key={review.id} className="flex flex-wrap items-center justify-between gap-3 py-4">
          <div className="min-w-0 text-sm"><p className="break-all font-medium text-gray-800">会话 {review.chatID}</p><p className="mt-1 break-all text-gray-500">消息 {review.messageKey || '未提供平台标识'} · {new Date(review.claimedAt).toLocaleString()}</p></div>
          <button type="button" disabled={reviews.busy} className="text-sm font-bold text-blue-600 disabled:opacity-50" onClick={/* 用户先查看明确核对说明，尚未提交解除。 */ () => reviews.setPending(review)}>核对并解除</button>
        </li>)}</ul>
          {reviews.page.total === 0 && <p className="mt-4 text-sm text-gray-500">没有需要核对的重复消息保护。</p>}
          {reviews.page.total > reviews.page.limit && <div className="mt-4 flex items-center gap-4 text-sm"><button type="button" disabled={reviews.busy || reviews.page.offset === 0} onClick={/* 用户明确读取上一页本地记录。 */ () => reviews.setOffset(Math.max(0, reviews.page!.offset - reviews.page!.limit))}>上一页</button><span>{Math.floor(reviews.page.offset / reviews.page.limit) + 1} / {Math.ceil(reviews.page.total / reviews.page.limit)}</span><button type="button" disabled={reviews.busy || reviews.page.offset + reviews.page.limit >= reviews.page.total} onClick={/* 用户明确读取下一页本地记录。 */ () => reviews.setOffset(reviews.page!.offset + reviews.page!.limit)}>下一页</button></div>}
        </>}
        {reviews.pending && <div role="dialog" aria-label="确认解除重复消息保护" className="mt-5 rounded-xl border border-amber-200 bg-amber-50 p-5 text-sm">
          <p className="font-bold text-gray-900">确认已核对会话 {reviews.pending.chatID} 的发送结果？</p>
          <p className="mt-2 leading-6 text-gray-700">解除后，买家后续发送相同文字可以重新触发自动回复。请确认不会造成重复回复；此操作不会补发原消息。</p>
          <div className="mt-4 flex flex-wrap gap-4"><button type="button" disabled={reviews.busy} className="ios-btn-primary rounded-xl px-4 py-2 font-bold disabled:opacity-50" onClick={/* 用户明确确认平台已核对，提交旧版本。 */ () => void reviews.resolve()}>{reviews.busy ? '正在解除…' : '我已核对，解除保护'}</button><button type="button" disabled={reviews.busy} onClick={/* 取消不产生任何写入。 */ () => reviews.setPending(null)}>取消</button></div>
        </div>}
      </div>
    </>}
  </section>;
}
