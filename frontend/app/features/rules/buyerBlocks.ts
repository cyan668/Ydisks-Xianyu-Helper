import { useEffect, useRef, useState } from 'react';
import { deleteBuyerBlock, getBuyerBlocks, saveBuyerBlock } from './api';
import type { BuyerBlock } from './buyerBlockModels';

/** emptyBuyerBlock 为 accountID 创建停用草稿；关单必须由用户明确勾选。 */
export const emptyBuyerBlock = (accountID: string): BuyerBlock => ({ id: 0, cookie_id: accountID, item_id: '', buyer_id: '', reason: '', enabled: false, auto_close: false });

/** useBuyerBlocks 拥有列表、草稿和写入状态；切换账号或卸载时取消请求并丢弃晚到结果。 */
export const useBuyerBlocks = (accountID: string) => {
  // rows、setRows 保存当前筛选下的服务端记录。
  const [rows, setRows] = useState<BuyerBlock[]>([]);
  // draft、setDraft 是未保存的编辑器状态，null 表示关闭。
  const [draft, setDraft] = useState<BuyerBlock | null>(null);
  // loading、setLoading 表示列表读取是否在途。
  const [loading, setLoading] = useState(true);
  // busy、setBusy 表示唯一写操作是否在途。
  const [busy, setBusy] = useState(false);
  // message、setMessage 提供成功或失败的可读反馈。
  const [message, setMessage] = useState('');
  // epoch、setEpoch 在写入成功或点击刷新时触发重读。
  const [epoch, setEpoch] = useState(0);
  // generation 标识当前列表和写入所属的视图代次。
  const generation = useRef(0);
  // mutation 保存唯一写请求的取消器，同时同步阻断重复点击。
  const mutation = useRef<AbortController | null>(null);
  // accountRef 在渲染时更新，防止 effect cleanup 前的旧响应污染新账号。
  const accountRef = useRef(accountID);
  accountRef.current = accountID;

  useEffect(/* 同步当前账号列表；刷新、账号切换和卸载均取消旧请求并使旧响应失效。 */ () => {
    // current、controller 标识此轮读取和它的取消责任。
    const current = ++generation.current;
    // controller 只属于当前列表请求。
    const controller = new AbortController();
    setRows([]); setLoading(true); setMessage(''); setDraft(null); setBusy(false);
    void getBuyerBlocks(accountID, { signal: controller.signal }).then(/* next 是本轮返回的非敏感规则列表。 */ next => {
      if (current === generation.current && accountRef.current === accountID) setRows(next);
    }).catch(/* error 是读取失败原因；取消或旧响应不得展示为新账号错误。 */ error => {
      if (!controller.signal.aborted && current === generation.current && accountRef.current === accountID) setMessage(error instanceof Error ? error.message : '读取黑名单失败');
    }).finally(/* 当前请求完成后收口加载状态。 */ () => {
      if (current === generation.current && accountRef.current === accountID) setLoading(false);
    });
    return /* 清理当前视图的读写请求；已经到达服务端的写入可能成功，返回后应重新查询。 */ () => { ++generation.current; controller.abort(); mutation.current?.abort(); mutation.current = null; };
  }, [accountID, epoch]);

  /** run 串行执行 operation；成功关闭编辑器并重读，失败保留草稿。 */
  const run = async (operation: (signal: AbortSignal) => Promise<unknown>): Promise<void> => {
    if (mutation.current) return;
    // current、key 捕获写入所属的视图与账号，晚到响应不能关闭其他草稿。
    const current = generation.current;
    // key 保存此写入所属的账号筛选。
    const key = accountID;
    // controller 由写入结束或视图 cleanup 取消和释放。
    const controller = new AbortController();
    mutation.current = controller; setBusy(true); setMessage('');
    try {
      await operation(controller.signal);
      if (current === generation.current && accountRef.current === key) { setDraft(null); setEpoch(/* value 递增读取代次。 */ value => value + 1); }
    } catch (/* error 为当前写入失败原因；不把取消或其他账号错误写入本视图。 */ error) {
      if (!controller.signal.aborted && current === generation.current && accountRef.current === key) setMessage(error instanceof Error ? error.message : '保存黑名单失败');
    } finally {
      if (mutation.current === controller) mutation.current = null;
      if (current === generation.current && accountRef.current === key) setBusy(false);
    }
  };

  return { rows, draft, setDraft, loading, busy, message,
    refresh: /* 用户手动刷新当前筛选。 */ () => setEpoch(/* value 是当前重读计数。 */ value => value + 1),
    save: /* 保存当前草稿，服务端负责最终身份及归属校验。 */ () => draft ? run(/* signal 传给写入请求的取消器。 */ signal => saveBuyerBlock(draft, { signal })) : Promise.resolve(),
    remove: /* id 是用户确认删除的记录。 */ (id: number) => run(/* signal 控制删除请求。 */ signal => deleteBuyerBlock(id, { signal })),
  };
};
