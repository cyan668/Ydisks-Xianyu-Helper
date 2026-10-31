import { useEffect, useRef, useState } from 'react';
import { getConversationReplyPause, resumeConversationReply } from './api';
import { subscribeToChatLiveEvents } from './liveEvents';

/** useConversationPause 拥有当前会话的接管状态；仅选中、手动刷新或收到当前会话消息时读取本地状态，无平台轮询。 */
export function useConversationPause(accountID: string, chatID: string) {
  // state、setState 保留服务端截止时间；null 表示尚未成功查询，不能猜测已恢复。
  const [state, setState] = useState<{ /** version 为读取快照的递增版本。 */ version: number; /** paused_until 是服务端毫秒截止时间。 */ paused_until: number; /** paused 是读取时服务端判定。 */ paused: boolean } | null>(null);
  // now、setNow 用于到期后更新展示；只有一个到期定时器，不轮询网络。
  const [now, setNow] = useState(Date.now());
  // error、setError 为本地状态操作反馈。
  const [error, setError] = useState('');
  // busy、setBusy 防止恢复重复提交。
  const [busy, setBusy] = useState(false);
  // generation、request 拥有最新本地请求；切换和卸载时终止。
  const generation = useRef(0);
  // request 是当前读取的取消器。
  const request = useRef<AbortController | null>(null);
  // write 是当前恢复的取消器，读取不得取消已提交的恢复反馈。
  const write = useRef<AbortController | null>(null);
  // keyRef 在 render 同步更新，effect cleanup 前的响应也无法污染新会话。
  const keyRef = useRef(accountID + ':' + chatID);
  keyRef.current = accountID + ':' + chatID;
  /** refresh 只请求当前用户的本地会话接管状态，晚到读取不能覆盖新请求。 */
  const refresh = async () => {
    // key、epoch 捕获本次视图和读取顺序。
    const key = accountID + ':' + chatID;
    // epoch 是本次本地读取的序号。
    const epoch = ++generation.current;
    request.current?.abort();
    // controller 为此读取独占的取消器。
    const controller = new AbortController(); request.current = controller;
    try {
      // next 是当前会话的真实本地截止时间。
      const next = await getConversationReplyPause(accountID, chatID, { signal: controller.signal });
      if (!controller.signal.aborted && epoch === generation.current && keyRef.current === key) { setState(next); setNow(Date.now()); setError(''); }
    } catch (/* cause 为当前读取失败，不能伪造“已恢复”。 */ cause) {
      if (!controller.signal.aborted && epoch === generation.current && keyRef.current === key) { setState(null); setError(cause instanceof Error ? cause.message : '读取会话接管失败'); }
    }
  };
  useEffect(/* 选中后只读一次；实时消息触发本地查询，清理订阅和读写取消器。 */ () => {
    setState(null); setBusy(false); setError('');
    void refresh();
    // unsubscribe 只监听当前账号/会话出站终态，不监听输入草稿或发送中事件。
    const unsubscribe = subscribeToChatLiveEvents(/* event 是应用壳已认证的消息事件。 */ event => {
      if (event.type === 'message' && event.message.account_id === accountID && event.message.chat_id === chatID && event.message.direction === 'outgoing' && (event.message.status === 'sent' || event.message.status === 'uncertain')) void refresh();
    });
    return /* 旧会话卸载时丢弃所有在途状态，不能撤销服务端已接受的恢复。 */ () => { ++generation.current; request.current?.abort(); write.current?.abort(); write.current = null; unsubscribe(); };
  }, [accountID, chatID]);
  useEffect(/* 到达截止时间时只更新本地时间，服务端发送门禁独立判断。 */ () => {
    if (!state || state.paused_until <= Date.now()) return;
    // timer 是当前截止时间的一次性计时器，状态改变时清理。
    const timer = window.setTimeout(/* 到期只更新显示，不产生请求。 */ () => setNow(Date.now()), Math.min(state.paused_until - Date.now() + 10, 2147483647));
    return /* 状态更新后取消旧截止计时。 */ () => window.clearTimeout(timer);
  }, [state]);
  /** resume 只提交当前显示的版本，若新人工消息更新了暂停则展示冲突并要求重新读取。 */
  const resume = async () => {
    if (!state || write.current) return;
    // key 绑定当前恢复的所属会话。
    const key = accountID + ':' + chatID;
    // controller 拥有此恢复请求。
    const controller = new AbortController(); write.current = controller; setBusy(true);
    request.current?.abort(); ++generation.current;
    // epoch 使恢复在途期间新消息触发的更新优先于旧恢复响应。
    const epoch = generation.current;
    try {
      // next 只有服务端比较截止时间成功后才表示恢复。
      const next = await resumeConversationReply(accountID, chatID, state.version, { signal: controller.signal });
      if (!controller.signal.aborted && keyRef.current === key && epoch === generation.current) { setState(next); setNow(Date.now()); setError(''); }
    } catch (/* cause 可能是新人工消息导致的冲突；用户可手动刷新再决定。 */ cause) {
      if (!controller.signal.aborted && keyRef.current === key && epoch === generation.current) { setState(null); setError(cause instanceof Error ? cause.message : '恢复失败，请刷新状态'); }
    } finally { if (write.current === controller) { write.current = null; if (keyRef.current === key) setBusy(false); } }
  };
  return { state, paused: Boolean(state && state.paused_until > now), error, busy, refresh, resume };
}
