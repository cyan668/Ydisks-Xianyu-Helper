import { useEffect, useRef, useState } from 'react';
import { getContentCooldown, listContentCooldownReviews, resolveContentCooldownReview, saveContentCooldown } from './api';
import type { ContentCooldownPage, ContentCooldownReview } from './contentCooldownModels';

/** useContentCooldown 拥有账号秒数草稿和独立读写取消器，切换/卸载后旧响应不能改变新草稿。 */
export function useContentCooldown(accountID: string) {
  // seconds、setSeconds 保留原始输入，空输入不会被自动解释为关闭。
  const [seconds, setSeconds] = useState('');
  // ready、setReady 只在成功读取后开放保存。
  const [ready, setReady] = useState(false);
  // loading、setLoading 描述本地配置读取。
  const [loading, setLoading] = useState(false);
  // busy、setBusy 描述唯一配置保存。
  const [busy, setBusy] = useState(false);
  // feedback、setFeedback 为当前账号的成功或错误。
  const [feedback, setFeedback] = useState('');
  // failed、setFailed 控制反馈的可访问语义。
  const [failed, setFailed] = useState(false);
  // epoch、setEpoch 由用户明确重试读取。
  const [epoch, setEpoch] = useState(0);
  // active 保证 cleanup 前的旧响应也不能跨账号更新。
  const active = useRef(accountID);
  active.current = accountID;
  // writing 拥有唯一写入取消器，并同步防止同一渲染周期的重复点击。
  const writing = useRef<AbortController | null>(null);
  useEffect(/* 配置只从本地读取，切账号与卸载取消旧读写。 */ () => {
    // controller 是本次账号读取代次。
    const controller = new AbortController();
    setLoading(Boolean(accountID)); setReady(false); setBusy(false); setFeedback(''); setFailed(false); setSeconds('');
    if (accountID) void getContentCooldown(accountID, { signal: controller.signal }).then(/* value 是读取成功的秒数。 */ value => {
      if (!controller.signal.aborted && active.current === accountID) { setSeconds(String(value)); setReady(true); }
    }).catch(/* error 不能被错误解释为默认关闭。 */ error => {
      if (!controller.signal.aborted && active.current === accountID) { setFailed(true); setFeedback(error instanceof Error ? error.message : '读取相同消息冷却失败'); }
    }).finally(/* 只结束当前读取代次。 */ () => {
      if (!controller.signal.aborted && active.current === accountID) setLoading(false);
    });
    return /* 释放本账号在途读写，晚到反馈随之失效。 */ () => { controller.abort(); writing.current?.abort(); writing.current = null; };
  }, [accountID, epoch]);
  // valid 明确要求非空、整数、秒级上限，不静默修正用户输入。
  const valid = seconds.trim() !== '' && Number.isInteger(Number(seconds)) && Number(seconds) >= 0 && Number(seconds) <= 86400;
  /** save 提交当前账号秒数，故障保留草稿，重复点击只发一次本地请求。 */
  const save = async () => {
    if (!accountID || !ready || !valid || loading || writing.current) return;
    // value 固定此提交的输入，不受在途草稿变化影响。
    const value = Number(seconds);
    // controller 独占本次配置写入。
    const controller = new AbortController();
    writing.current = controller; setBusy(true); setFailed(false); setFeedback('');
    try {
      // saved 是服务器确认的秒数，不与人工暂停合并保存。
      const saved = await saveContentCooldown(accountID, value, { signal: controller.signal });
      if (!controller.signal.aborted && active.current === accountID) { setSeconds(String(saved)); setFeedback(saved === 0 ? '已关闭相同消息冷却；发送结果待核对的保护仍保留。' : `已保存：相同买家消息在成功回复后 ${saved} 秒内不再自动回复。`); }
    } catch (/* error 是本地配置保存故障。 */ error) {
      if (!controller.signal.aborted && active.current === accountID) { setFailed(true); setFeedback(error instanceof Error ? error.message : '保存相同消息冷却失败'); }
    } finally {
      if (writing.current === controller) { writing.current = null; if (!controller.signal.aborted && active.current === accountID) setBusy(false); }
    }
  };
  return { seconds, setSeconds, valid, ready, loading, busy, feedback, failed, save, reload: /* 用户重试读取当前账号配置。 */ () => setEpoch(/* value 为读取代次。 */ value => value + 1) };
}

/** useContentCooldownReviews 拥有本地分页、明确核对和取消责任，不轮询平台；旧页面不能解除新状态。 */
export function useContentCooldownReviews(accountID: string) {
  // offset、setOffset 是用户当前选择的页位置。
  const [offset, setOffset] = useState(0);
  // page、setPage 只在读取成功后替换服务端快照。
  const [page, setPage] = useState<ContentCooldownPage | null>(null);
  // loading、setLoading 防止读取期间使用旧分页。
  const [loading, setLoading] = useState(false);
  // error、setError 是列表或解除故障，不能显示为已解除。
  const [error, setError] = useState('');
  // notice、setNotice 是明确解除成功反馈。
  const [notice, setNotice] = useState('');
  // pending、setPending 固定用户打开确认时看到的主键和版本。
  const [pending, setPending] = useState<ContentCooldownReview | null>(null);
  // busy、setBusy 描述唯一解除操作。
  const [busy, setBusy] = useState(false);
  // epoch、setEpoch 由刷新或明确解除成功推进。
  const [epoch, setEpoch] = useState(0);
  // active 阻止渲染已切账号但 cleanup 尚未执行的旧响应写回。
  const active = useRef(accountID);
  active.current = accountID;
  // writing 拥有唯一的解除请求。
  const writing = useRef<AbortController | null>(null);
  useEffect(/* 账号变化先回第一页，并使旧确认失效。 */ () => { setOffset(0); setPending(null); setNotice(''); }, [accountID]);
  useEffect(/* 列表只访问本地接口，翻页、刷新或切账号取消旧请求。 */ () => {
    // controller 是本次分页读取代次。
    const controller = new AbortController();
    setPage(null); setLoading(Boolean(accountID)); setError(''); setPending(null); setBusy(false);
    if (accountID) void listContentCooldownReviews(accountID, offset, { signal: controller.signal }).then(/* result 是当前账号及页位置的保护快照。 */ result => {
      if (!controller.signal.aborted && active.current === accountID) {
        if (offset > 0 && result.items.length === 0) setOffset(Math.max(0, offset - result.limit));
        else setPage(result);
      }
    }).catch(/* failure 是本地列表故障，保留明确重试入口。 */ failure => {
      if (!controller.signal.aborted && active.current === accountID) setError(failure instanceof Error ? failure.message : '读取待核对保护失败');
    }).finally(/* 只结束当前页读取。 */ () => { if (!controller.signal.aborted && active.current === accountID) setLoading(false); });
    return /* 翻页或切账号时同时取消写反馈，不能留下跨账号确认。 */ () => { controller.abort(); writing.current?.abort(); writing.current = null; };
  }, [accountID, offset, epoch]);
  /** resolve 由明确核对按钮触发，只解除当前快照保护，不补发旧消息。 */
  const resolve = async () => {
    if (!accountID || !pending || writing.current || loading) return;
    // review 固定此操作的旧保护，列表刷新后不能自动替换版本。
    const review = pending;
    // controller 控制本次解除反馈。
    const controller = new AbortController();
    writing.current = controller; setBusy(true); setError(''); setNotice('');
    try {
      await resolveContentCooldownReview(accountID, review, { signal: controller.signal });
      if (!controller.signal.aborted && active.current === accountID) { setPending(null); setNotice('保护已解除，仅后续新消息重新参与回复。'); setEpoch(/* value 推进本地列表读取。 */ value => value + 1); }
    } catch (/* failure 是状态冲突或存储故障，不能把它当作解除成功。 */ failure) {
      if (!controller.signal.aborted && active.current === accountID) setError(failure instanceof Error ? failure.message : '解除保护失败，请刷新列表后核对');
    } finally {
      if (writing.current === controller) { writing.current = null; if (!controller.signal.aborted && active.current === accountID) setBusy(false); }
    }
  };
  return { page, loading, error, notice, pending, setPending, busy, resolve, setOffset, reload: /* 只刷新本地待核对记录。 */ () => setEpoch(/* value 为列表读取代次。 */ value => value + 1) };
}
