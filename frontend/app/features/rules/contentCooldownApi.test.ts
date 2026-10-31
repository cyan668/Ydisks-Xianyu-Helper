import { afterEach, expect, test, vi } from 'vitest';
import { getContentCooldown, listContentCooldownReviews, resolveContentCooldownReview, saveContentCooldown } from './api';

afterEach(/* 恢复全局 HTTP 替身，避免跨测试泄漏。 */ () => vi.unstubAllGlobals());

test('冷却 adapter 使用生成路径并转换 UI 模型，恢复携带旧版本和明确确认', /* 所有 HTTP 请求由本地响应替身提供。 */ async () => {
  // fetchMock 记录真实契约客户端生成的 Request，不访问网络。
  const fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ seconds: 3600 }), { status: 200 }));
  expect(await getContentCooldown('account')).toBe(3600);
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ seconds: 0 }), { status: 200 }));
  expect(await saveContentCooldown('account', 0)).toBe(0);
  // settingRequest 是独立秒数字段的真实请求。
  const settingRequest = fetchMock.mock.calls[1][0] as Request;
  expect(await settingRequest.json()).toEqual({ account_id: 'account', seconds: 0 });
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ items: [{ id: 7, version: 3, chat_id: 'chat', message_key: 'message', claimed_at: 1000 }], total: 21, limit: 20, offset: 20 }), { status: 200 }));
  // page 是由 transport 转换后的 UI 模型，snake_case 不进入 Hook。
  const page = await listContentCooldownReviews('account', 20);
  expect(page.items).toEqual([{ id: 7, version: 3, chatID: 'chat', messageKey: 'message', claimedAt: 1000 }]);
  expect((fetchMock.mock.calls[2][0] as Request).url).toContain('limit=20&offset=20');
  fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ resolved: true }), { status: 200 }));
  await resolveContentCooldownReview('account', page.items[0]);
  // resolveRequest 是携带明确账号、旧版本和确认的单次本地写入。
  const resolveRequest = fetchMock.mock.calls[3][0] as Request;
  expect(resolveRequest.url).toContain('/api/v1/chat/content-cooldown/reviews/7/resolve');
  expect(await resolveRequest.json()).toEqual({ account_id: 'account', expected_version: 3, confirmed: true });
});
