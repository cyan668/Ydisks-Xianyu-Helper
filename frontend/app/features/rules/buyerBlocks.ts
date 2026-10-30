import { useEffect, useRef, useState } from 'react';
import { batchBuyerBlocks, deleteBuyerBlock, exportBuyerBlocks, getBuyerBlockPage, importBuyerBlocks, saveBuyerBlock } from './api';
import { downloadBlob } from '../../../shared/browser/download';
import type { BuyerBlock } from './buyerBlockModels';

/** emptyBuyerBlock 为 accountID 创建停用草稿；关单必须由用户明确勾选。 */
export const emptyBuyerBlock = (accountID: string): BuyerBlock => ({ id: 0, cookie_id: accountID, item_id: '', buyer_id: '', reason: '', enabled: false, auto_close: false });

/** useBuyerBlocks 拥有列表、草稿和写入状态；切换账号或卸载时取消请求并丢弃晚到结果。 */
export const useBuyerBlocks = (accountID: string) => {
  // rows、setRows 保存当前筛选下的服务端记录。
  const [rows, setRows] = useState<BuyerBlock[]>([]);
  // total、setTotal 保存服务器筛选总数，不通过当前页条数猜测。
  const [total, setTotal] = useState(0);
  // pageState、setPageState 把页码绑定账号，切账号首次渲染直接回第一页。
  const [pageState, setPageState] = useState({ accountID, number: 1 });
  // page 是当前账号有效的一基页码。
  const page = pageState.accountID === accountID ? pageState.number : 1;
  // search、setSearch 是用户明确提交的查询词，不在每次输入时请求。
  const [search, setSearch] = useState('');
  // selected、setSelected 只表示当前页面明确勾选的主键，翻页/切账号时清空。
  const [selected, setSelected] = useState<number[]>([]);
  // draft、setDraft 是未保存的编辑器状态，null 表示关闭。
  const [draft, setDraft] = useState<BuyerBlock | null>(null);
  // loading、setLoading 表示列表读取是否在途。
  const [loading, setLoading] = useState(true);
  // busy、setBusy 表示唯一写操作是否在途。
  const [busy, setBusy] = useState(false);
  // message、setMessage 提供成功或失败的可读反馈。
  const [message, setMessage] = useState('');
  // hasError、setHasError 区分成功反馈与错误，成功不能显示为红色警告。
  const [hasError, setHasError] = useState(false);
  // epoch、setEpoch 在写入成功或点击刷新时触发重读。
  const [epoch, setEpoch] = useState(0);
  // generation 标识当前列表和写入所属的视图代次。
  const generation = useRef(0);
  // mutation 保存唯一写请求的取消器，同时同步阻断重复点击。
  const mutation = useRef<AbortController | null>(null);
  // accountRef 在渲染时更新，防止 effect cleanup 前的旧响应污染新账号。
  const accountRef = useRef(accountID);
  accountRef.current = accountID;

  useEffect(/* 切账号时清除旧账号反馈，不清除同账号写入后重读的成功计数。 */ () => { setMessage(''); setHasError(false); }, [accountID]);

  useEffect(/* 同步当前账号列表；刷新、账号切换和卸载均取消旧请求并使旧响应失效。 */ () => {
    // current、controller 标识此轮读取和它的取消责任。
    const current = ++generation.current;
    // controller 只属于当前列表请求。
    const controller = new AbortController();
    setRows([]); setSelected([]); setLoading(true); setDraft(null); setBusy(false);
    void getBuyerBlockPage(accountID, page, search, { signal: controller.signal }).then(/* next 是本轮的实际分页结果。 */ next => {
      if (current === generation.current && accountRef.current === accountID) { setRows(next.rows); setTotal(next.total); }
    }).catch(/* error 是读取失败原因；取消或旧响应不得展示为新账号错误。 */ error => {
      if (!controller.signal.aborted && current === generation.current && accountRef.current === accountID) { setHasError(true); setMessage(error instanceof Error ? error.message : '读取黑名单失败'); }
    }).finally(/* 当前请求完成后收口加载状态。 */ () => {
      if (current === generation.current && accountRef.current === accountID) setLoading(false);
    });
    return /* 清理当前视图的读写请求；已经到达服务端的写入可能成功，返回后应重新查询。 */ () => { ++generation.current; controller.abort(); mutation.current?.abort(); mutation.current = null; };
  }, [accountID, epoch, page, search]);

  /** run 串行执行 operation；success 按本次动作结果生成反馈，reload 决定是否关闭草稿并重读。 */
  const run = async <T>(operation: (signal: AbortSignal) => Promise<T>, success: string | ((result: T) => string) = '操作已保存', reload = true): Promise<void> => {
    if (mutation.current) return;
    // current、key 捕获写入所属的视图与账号，晚到响应不能关闭其他草稿。
    const current = generation.current;
    // key 保存此写入所属的账号筛选。
    const key = accountID;
    // controller 由写入结束或视图 cleanup 取消和释放。
    const controller = new AbortController();
    mutation.current = controller; setBusy(true); setMessage(''); setHasError(false);
    try {
      // result 保留当前动作的类型化结果，批量计数不能被误当作导入计数。
      const result = await operation(controller.signal);
      if (current === generation.current && accountRef.current === key) { if (reload) { setDraft(null); setEpoch(/* value 递增读取代次。 */ value => value + 1); } setMessage(typeof success === 'function' ? success(result) : success); }
    } catch (/* error 为当前写入失败原因；不把取消或其他账号错误写入本视图。 */ error) {
      if (!controller.signal.aborted && current === generation.current && accountRef.current === key) { setHasError(true); setMessage(error instanceof Error ? error.message : '保存黑名单失败'); }
    } finally {
      if (mutation.current === controller) mutation.current = null;
      if (current === generation.current && accountRef.current === key) setBusy(false);
    }
  };

  return { rows, draft, setDraft, loading, busy, message, hasError, total, page, selected, setSelected,
    setPage: /* number 是用户请求的一基页码。 */ (number: number) => setPageState({ accountID, number: Math.max(1, number) }),
    applySearch: /* value 是用户明确提交的查询文本。 */ (value: string) => { setSearch(value.trim()); setPageState({ accountID, number: 1 }); },
    refresh: /* 用户手动刷新当前筛选。 */ () => setEpoch(/* value 是当前重读计数。 */ value => value + 1),
    save: /* 保存当前草稿，服务端负责最终身份及归属校验。 */ () => draft ? run(/* signal 传给写入请求的取消器。 */ signal => saveBuyerBlock(draft, { signal })) : Promise.resolve(),
    remove: /* id 是用户确认删除的记录。 */ (id: number) => run(/* signal 控制删除请求。 */ signal => deleteBuyerBlock(id, { signal })),
    batch: /* action 只作用于当前明确勾选主键，删除须由组件二次确认。 */ (action: 'enable' | 'disable' | 'delete') => run(/* signal 控制原子批量操作。 */ signal => batchBuyerBlocks(selected, action, { signal }), /* result 的 affected 是服务端实际处理数，不是新增数。 */ result => `已${{ enable: '启用', disable: '停用', delete: '删除' }[action]} ${result.affected} 条个人名单。`),
    importFile: /* file 为用户选择的原系统 XLSX/CSV 文件。 */ (file: File) => run(/* signal 控制上传并保持晚到隔离。 */ signal => importBuyerBlocks(file, { signal }), /* result 只在文件导入时报告新增与重复跳过数。 */ result => `新增 ${result.created} 条，跳过 ${result.skipped} 条，重复记录保持原配置。`),
    exportFile: /* 导出不关闭未保存草稿、不改变筛选，也不请求平台。 */ () => run(/* signal 控制下载。 */ async signal => { const blob = await exportBuyerBlocks(accountID, { signal }); /* blob 是当前用户下载的配置文件。 */ if (!signal.aborted) downloadBlob(blob, '个人黑名单.csv'); }, '名单已导出', false),
  };
};
