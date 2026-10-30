import { beforeEach, expect, test, vi } from 'vitest';
import type { BuyerBlock } from './buyerBlockModels';

// clientMock 记录实际 adapter 的读写路径与正文，全部在内存中执行。
const clientMock = vi.hoisted(/* 创建契约客户端替身。 */ () => ({ GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() }));
vi.mock('../../../shared/api-contract/client', /* 替换网络边界，保留真实 feature adapter。 */ () => ({
  contractClient: clientMock,
  runContractRequest: /* execute 为实际 adapter 构造的可取消请求。 */ async (execute: (signal: AbortSignal) => Promise<{ /** data 是替身响应。 */ data: unknown }>) => (await execute(new AbortController().signal)).data,
}));
import { deleteBuyerBlock, getBuyerBlocks, saveBuyerBlock } from './api';

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
