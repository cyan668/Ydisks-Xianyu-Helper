// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { batchBuyerBlocks, deleteBuyerBlock, exportBuyerBlocks, getBuyerBlockPage, importBuyerBlocks, saveBuyerBlock } from './api';
import { downloadBlob } from '../../../shared/browser/download';
import { emptyBuyerBlock, useBuyerBlocks } from './buyerBlocks';
import BuyerBlockPanel from './components/BuyerBlockPanel';
import type { BuyerBlock } from './buyerBlockModels';

vi.mock('./api', /* 仅使用内存持久化替身，不发网络或关单请求。 */ () => ({ getBuyerBlockPage: vi.fn(), saveBuyerBlock: vi.fn(), deleteBuyerBlock: vi.fn(), batchBuyerBlocks: vi.fn(), importBuyerBlocks: vi.fn(), exportBuyerBlocks: vi.fn() }));
vi.mock('../../../shared/browser/download', /* 下载仅记录用户文件，不触发真实浏览器写盘。 */ () => ({ downloadBlob: vi.fn() }));

// list、save、remove 是可控业务 API 替身。
const list = vi.mocked(getBuyerBlockPage);
// save 记录草稿与取消器，不调用平台。
const save = vi.mocked(saveBuyerBlock);
// remove 只记录需要删除的主键。
const remove = vi.mocked(deleteBuyerBlock);
// fixture 为停用的用户级虚构记录。
/** pageOf 形成真实分页响应，不通过客户端列表推断总数。 */
const pageOf = (rows: BuyerBlock[]) => ({ rows, total: rows.length, page: 1, pageSize: 20 });
/** fixture 是停用的用户级虚构记录，不涉及平台账号。 */
const fixture: BuyerBlock = { ...emptyBuyerBlock(''), id: 1, buyer_id: '123' };

beforeEach(/* 每例重置替身并默认返回空列表。 */ () => { vi.resetAllMocks(); list.mockResolvedValue(pageOf([])); save.mockResolvedValue(fixture); remove.mockResolvedValue(); });
afterEach(/* 清理 React DOM 与 Hook，触发在途请求取消。 */ () => cleanup());

test('账号切换取消旧读取，晚到响应不能覆盖新列表', /* 请求代次必须在 React 状态之外同步隔离。 */ async () => {
  // resolve 允许测试在切换账号后完成旧读取。
  let resolve!: (page: ReturnType<typeof pageOf>) => void;
  list.mockReturnValueOnce(new Promise<ReturnType<typeof pageOf>>(/* finish 捕获旧响应的完成能力。 */ finish => { resolve = finish; })).mockResolvedValueOnce(pageOf([{ ...fixture, id: 2 }]));
  // hook 保存随账号变化的真实业务 Hook。
  const hook = renderHook(/* props 提供当前账号筛选。 */ ({ accountID }: { /** accountID 是本次渲染的账号。 */ accountID: string }) => useBuyerBlocks(accountID), { initialProps: { accountID: 'a' } });
  // signal 为被替代读取的取消器。
  const signal = list.mock.calls[0][3]?.signal;
  hook.rerender({ accountID: 'b' });
  await waitFor(/* 等待新列表完成。 */ () => expect(hook.result.current.rows[0]?.id).toBe(2));
  expect(signal?.aborted).toBe(true);
  await act(/* 完成旧响应后状态仍应属于新账号。 */ async () => resolve(pageOf([fixture])));
  expect(hook.result.current.rows[0]?.id).toBe(2);
});

test('写入失败保留草稿，重复点击只提交一次，成功后重读', /* 写入生命期与列表读取分离，但共用视图代次。 */ async () => {
  // hook 提供真实草稿和写入协调。
  const hook = renderHook(/* 创建固定账号视图。 */ () => useBuyerBlocks('a'));
  await waitFor(/* 等待初次读取。 */ () => expect(hook.result.current.loading).toBe(false));
  act(/* 用户开始编辑一个新草稿。 */ () => hook.result.current.setDraft({ ...fixture, id: 0 }));
  save.mockRejectedValueOnce(new Error('fixture failure'));
  await act(/* 保存失败不能丢弃用户输入。 */ async () => hook.result.current.save());
  expect(hook.result.current.draft?.buyer_id).toBe('123');
  expect(hook.result.current.message).toBe('fixture failure');
  // finish 为后续挂起写入的完成能力。
  let finish!: (row: BuyerBlock) => void;
  save.mockReturnValueOnce(new Promise<BuyerBlock>(/* resolve 暂存成功响应。 */ resolve => { finish = resolve; }));
  act(/* 同一渲染中重复点击也只能发送一个请求。 */ () => { void hook.result.current.save(); void hook.result.current.save(); });
  expect(save).toHaveBeenCalledTimes(2);
  list.mockResolvedValueOnce(pageOf([fixture]));
  await act(/* 写入完成后关闭草稿并重读。 */ async () => finish(fixture));
  await waitFor(/* 新列表应显示服务端结果。 */ () => expect(hook.result.current.rows).toEqual([fixture]));
  expect(hook.result.current.draft).toBeNull();
  await act(/* 删除也走同一写入协调。 */ async () => hook.result.current.remove(1));
  expect(remove).toHaveBeenCalledWith(1, expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('面板新建默认停用且关单需显式选择', /* 通过真实表单检查可见开关与危险动作说明。 */ async () => {
  render(<BuyerBlockPanel accountID="a" accounts={[{ id: 'a', label: '测试账号' }]} />);
  await waitFor(/* 初始列表读取完成后才打开编辑器。 */ () => expect(screen.queryByText('正在读取黑名单…')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: '新增黑名单' }));
  expect((screen.getByLabelText('启用交易拦截') as HTMLInputElement).checked).toBe(false);
  expect((screen.getByLabelText('命中时尝试关闭未发货订单') as HTMLInputElement).checked).toBe(false);
  fireEvent.change(screen.getByLabelText('买家 ID'), { target: { value: '123' } });
  fireEvent.click(screen.getByLabelText('命中时尝试关闭未发货订单'));
  expect(screen.getByText(/每单最多请求一次/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '保存黑名单' }));
  await waitFor(/* 保存必须保留停用状态与明确的关单选项。 */ () => expect(save).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, auto_close: true, buyer_id: '123' }), expect.any(Object)));
});

test('列表失败可重读，删除取消不提交且失败保留记录', /* 空数据不能掩盖读取错误，删除不确定结果必须显示反馈并允许重读。 */ async () => {
  list.mockRejectedValueOnce(new Error('读取失败')).mockResolvedValue(pageOf([fixture]));
  render(<BuyerBlockPanel accountID="a" accounts={[{ id: 'a', label: '测试账号' }]} />);
  await waitFor(/* 先显示读取失败的实际原因。 */ () => expect(screen.getByRole('alert').textContent).toBe('读取失败'));
  fireEvent.click(screen.getByRole('button', { name: '刷新黑名单' }));
  await waitFor(/* 重读成功后展示用户级记录。 */ () => expect(screen.getByText('我的全部账号')).toBeTruthy());
  // confirm 只替换本地确认框，不发送任何外部删除操作。
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  fireEvent.click(screen.getByRole('button', { name: /^删除$/ }));
  expect(remove).not.toHaveBeenCalled();
  confirm.mockReturnValue(true); remove.mockRejectedValueOnce(new Error('删除失败'));
  fireEvent.click(screen.getByRole('button', { name: /^删除$/ }));
  await waitFor(/* 删除失败时仍显示原记录和准确错误。 */ () => expect(screen.getByRole('alert').textContent).toBe('删除失败'));
  expect(screen.getByText('123')).toBeTruthy();
  confirm.mockRestore();
});

test('编辑范围时切换全部账号清空商品，切换页面账号关闭旧编辑器', /* 草稿不能把前一账号的商品范围带到另一账号。 */ async () => {
  list.mockResolvedValue(pageOf([{ ...fixture, cookie_id: 'a', item_id: 'item-a' }]));
  // view 用于在用户编辑时切换账号筛选。
  const view = render(<BuyerBlockPanel accountID="a" accounts={[{ id: 'a', label: '测试账号' }]} />);
  await waitFor(/* 等待已有记录加载。 */ () => expect(screen.getByText('商品 item-a')).toBeTruthy());
  fireEvent.click(screen.getByRole('button', { name: /^编辑$/ }));
  expect((screen.getByLabelText('商品 ID（留空为全部）') as HTMLInputElement).value).toBe('item-a');
  fireEvent.change(screen.getByLabelText('适用账号'), { target: { value: '' } });
  expect((screen.getByLabelText('商品 ID（留空为全部）') as HTMLInputElement).value).toBe('');
  expect((screen.getByLabelText('商品 ID（留空为全部）') as HTMLInputElement).disabled).toBe(true);
  view.rerender(<BuyerBlockPanel accountID="b" accounts={[{ id: 'b', label: '另一个账号' }]} />);
  await waitFor(/* 切换账号后旧草稿应已关闭。 */ () => expect(screen.queryByRole('button', { name: '保存黑名单' })).toBeNull());
});

test('翻页清空选择，搜索回首页，导入报告去重，导出不丢弃草稿', /* 验证新增管理动作的实际状态机，而非只检验 API 路径。 */ async () => {
  list.mockResolvedValue({ ...pageOf([fixture]), total: 45 });
  // hook 是固定账号的完整管理状态机。
  const hook = renderHook(/* 创建本地名单管理视图。 */ () => useBuyerBlocks('a'));
  await waitFor(/* 等待当前页读取完成。 */ () => expect(hook.result.current.rows).toEqual([fixture]));
  act(/* 用户选择本页记录并切换第二页。 */ () => { hook.result.current.setSelected([1]); hook.result.current.setPage(2); });
  await waitFor(/* 翻页只读明确的新页并清空旧集合。 */ () => expect(list).toHaveBeenLastCalledWith('a', 2, '', expect.any(Object)));
  expect(hook.result.current.selected).toEqual([]);
  act(/* 搜索必须回到第一页。 */ () => hook.result.current.applySearch('fixture'));
  await waitFor(/* 查询参数不能沿用上一页。 */ () => expect(list).toHaveBeenLastCalledWith('a', 1, 'fixture', expect.any(Object)));
  vi.mocked(batchBuyerBlocks).mockResolvedValue({ created: 0, skipped: 0, affected: 1 });
  act(/* 用户明确重新选择当前页的一条记录。 */ () => hook.result.current.setSelected([1]));
  await act(/* 批量只提交所选集合。 */ async () => hook.result.current.batch('disable'));
  expect(batchBuyerBlocks).toHaveBeenCalledWith([1], 'disable', expect.any(Object));
  vi.mocked(importBuyerBlocks).mockResolvedValue({ created: 3, skipped: 2, affected: 0 });
  // file 是只有测试文本的导入文件。
  const file = new File(['fixture'], 'fixture.csv');
  await act(/* 文件导入成功后显示精确新增/跳过计数。 */ async () => hook.result.current.importFile(file));
  expect(hook.result.current.message).toContain('新增 3 条，跳过 2 条');
  expect(hook.result.current.hasError).toBe(false);
  act(/* 用户未保存的草稿不能因下载文件被关闭。 */ () => hook.result.current.setDraft(fixture));
  // blob 是用户当前个人名单的二进制下载结果。
  const blob = new Blob(['fixture']); vi.mocked(exportBuyerBlocks).mockResolvedValue(blob);
  await act(/* 导出是只读动作，不刷新或清空编辑器。 */ async () => hook.result.current.exportFile());
  expect(downloadBlob).toHaveBeenCalledWith(blob, '个人黑名单.csv');
  expect(hook.result.current.draft).toEqual(fixture);
});
