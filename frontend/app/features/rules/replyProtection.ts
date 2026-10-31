import { useEffect, useRef, useState } from 'react';
import { getReplyProtection, saveReplyProtection } from './api';

/** useReplyProtection 拥有账号设置草稿、读写取消器与反馈；切账号时丢弃旧响应。 */
export function useReplyProtection(accountID: string) {
  // minutes、setMinutes 是本次账号的未提交分钟草稿。
  const [minutes, setMinutes] = useState(0);
  // loading、setLoading 避免配置读取前以默认值覆盖已有配置。
  const [loading, setLoading] = useState(true);
  // ready、setReady 只在成功读回配置后开放保存，读取故障不能把默认零写回服务器。
  const [ready, setReady] = useState(false);
  // epoch、setEpoch 为用户重试本地读取的代次。
  const [epoch, setEpoch] = useState(0);
  // busy、setBusy 隔离唯一写操作。
  const [busy, setBusy] = useState(false);
  // message、setMessage 与 failed、setFailed 分开表达成功和错误。
  const [message, setMessage] = useState('');
  // failed、setFailed 控制错误语义与颜色。
  const [failed, setFailed] = useState(false);
  // active 在渲染时保留账号，cleanup 前到达的旧结果也不能更新新账号。
  const active = useRef(accountID);
  active.current = accountID;
  // request 保存读写统一取消责任，卸载与切账号会释放。
  const request = useRef<AbortController | null>(null);
  // write 同步阻止同一渲染周期的重复保存，读请求不会占用写入权。
  const write = useRef<AbortController | null>(null);
  useEffect(/* 仅从本地配置接口读取，账号变化取消旧操作。 */ () => {
    // controller 是当前账号配置读取的取消器。
    const controller = new AbortController();
    request.current = controller;
    setLoading(Boolean(accountID)); setReady(false); setBusy(false); setMessage(''); setFailed(false); setMinutes(0);
    if (accountID) void getReplyProtection(accountID, { signal: controller.signal }).then(/* result 是当前账号保存的独立配置。 */ result => {
      if (!controller.signal.aborted && active.current === accountID) { setMinutes(result.manual_pause_minutes); setReady(true); }
    }).catch(/* error 不能被误显示成成功关闭接管。 */ error => {
      if (!controller.signal.aborted && active.current === accountID) { setFailed(true); setMessage(error instanceof Error ? error.message : '读取回复保护失败'); }
    }).finally(/* 当前账号读请求完成才开放表单。 */ () => {
      if (!controller.signal.aborted && active.current === accountID) setLoading(false);
    });
    return /* 切账号和卸载后所有在途反馈失效。 */ () => { controller.abort(); request.current?.abort(); write.current?.abort(); write.current = null; request.current = null; };
  }, [accountID, epoch]);
  /** save 由用户保存当前分钟数；晚到响应不能清空另一个账号的草稿。 */
  const save = async () => {
    if (loading || !ready || write.current || !accountID || !Number.isInteger(minutes) || minutes < 0 || minutes > 1440) return;
    // controller 标记当前写入，阻止连续点击并控制取消。
    const controller = new AbortController();
    write.current = controller;
    request.current?.abort(); request.current = controller;
    setBusy(true); setMessage(''); setFailed(false);
    try {
      await saveReplyProtection(accountID, minutes, { signal: controller.signal });
      if (!controller.signal.aborted && active.current === accountID) setMessage(minutes === 0 ? '人工接管已关闭，此账号现有会话暂停已结束。' : `已保存：人工回复后，该会话暂停自动回复 ${minutes} 分钟。`);
    } catch (/* error 是保存失败原因，保留当前草稿。 */ error) {
      if (!controller.signal.aborted && active.current === accountID) { setFailed(true); setMessage(error instanceof Error ? error.message : '保存回复保护失败'); }
    } finally { if (write.current === controller) { write.current = null; if (!controller.signal.aborted && active.current === accountID) setBusy(false); } }
  };
  return { minutes, setMinutes, loading, ready, busy, message, failed, save, reload: /* 用户重试当前账号的本地配置读取。 */ () => setEpoch(/* value 为读取重试代次。 */ value => value + 1) };
}
