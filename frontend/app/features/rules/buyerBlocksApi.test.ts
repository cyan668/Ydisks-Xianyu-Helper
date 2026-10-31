import { beforeEach, expect, test, vi } from 'vitest';
import type { BuyerBlock } from './buyerBlockModels';

// clientMock 记录实际 adapter 的读写路径与正文，全部在内存中执行。
const clientMock = vi.hoisted(/* 创建契约客户端替身。 */ () => ({ GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() }));
vi.mock('../../../shared/api-contract/client', /* 替换网络边界，保留真实 feature adapter。 */ () => ({
  contractClient: clientMock,
  contractMultipartBody: /* form 保留原生边界和 File，不把文件 JSON 化。 */ (form: FormData) => form,
  runContractRequest: /* execute 为实际 adapter 构造的可取消请求。 */ async (execute: (signal: AbortSignal) => Promise<{ /** data 是替身响应。 */ data: unknown }>) => (await execute(new AbortController().signal)).data,
}));
import { batchBuyerBlocks, deleteBuyerBlock, exportBuyerBlocks, getBuyerBlockPage, getBuyerBlocks, importBuyerBlocks, saveBuyerBlock } from './api';

// fixture 为明确停用的用户级配置。
const fixture: BuyerBlock = { id: 4, cookie_id: '', item_id: '', buyer_id: '123', reason: 'fixture', enabled: false, auto_close: false };
beforeEach(/* 各例隔离请求记录。 */ () => vi.resetAllMocks());

test('列表转换不传播多余字段，创建与更新保留停用和关单状态', /* 使用真实 adapter 验证端点与持久化形状。 */ async () => {
  clientMock.GET.mockResolvedValue({ data: [{ ...fixture, unexpected: 'ignored' }] });
  expect(await getBuyerBlocks('a')).toEqual([fixture]);
  expect(clientMock.GET.mock.calls[0][1].params.query.cookie_id).toBe('a');
  clientMock.POST.mockResolvedValue({ data: fixture });
  clientMock.PUT.mockResolvedValue({ data: fixture });
  await saveBuyerBlock({ ...fixture, id: 0 });
  expect(clientMock.POST.mock.calls[0][1].body).toEqual({ cookie_id: '', item_id: '', buyer_id: '123', reason: 'fixture', enabled: false, auto_close: false });
  await saveBuyerBlock({ ...fixture, enabled: true, auto_close: true });
  expect(clientMock.PUT.mock.calls[0][1]).toMatchObject({ params: { path: { id: 4 } }, body: { enabled: true, auto_close: true } });
  clientMock.DELETE.mockResolvedValue({ data: { success: true } });
  await deleteBuyerBlock(4);
  expect(clientMock.DELETE.mock.calls[0][1].params.path.id).toBe(4);
});

test('分页批量及文件适配使用准确端点、原生 FormData 和 Blob', /* 不依赖源码字符串检查真实请求对象与文件字节。 */ async () => {
  clientMock.GET.mockResolvedValueOnce({ data: { data: [fixture], total: 45, page: 2, page_size: 20 } });
  expect(await getBuyerBlockPage('a', 2, 'fixture')).toMatchObject({ rows: [fixture], total: 45, page: 2 });
  expect(clientMock.GET.mock.calls[0][1].params.query).toMatchObject({ cookie_id: 'a', page: 2, page_size: 20, search: 'fixture' });
  clientMock.POST.mockResolvedValue({ data: { created: 1, skipped: 2, affected: 0 } });
  await batchBuyerBlocks([4, 5], 'disable');
  expect(clientMock.POST.mock.calls[0][1].body).toEqual({ ids: [4, 5], action: 'disable' });
  // file 是虚构原系统表格，所有内容只驻留测试内存。
  const file = new File(['买家ID,是否启用\n123,是\n'], 'fixture.csv', { type: 'text/csv' });
  await importBuyerBlocks(file);
  expect(clientMock.POST.mock.calls[1][0]).toBe('/api/v1/buyer-blocks/import');
  expect(clientMock.POST.mock.calls[1][1].body).toBeInstanceOf(FormData);
  expect(clientMock.POST.mock.calls[1][1].body.get('file').name).toBe('fixture.csv');
  // blob 是服务端文件流的实际返回，不应被 adapter JSON 化。
  const blob = new Blob(['fixture']);
  clientMock.GET.mockResolvedValueOnce({ data: blob });
  expect(await exportBuyerBlocks('a')).toBe(blob);
  expect(clientMock.GET.mock.calls.at(-1)?.[1]).toMatchObject({ parseAs: 'blob', params: { query: { cookie_id: 'a' } } });
});
