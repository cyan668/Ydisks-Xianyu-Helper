// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { addOrderBuyerBlock } from '../api';
import { OrderBuyerBlock } from './OrderBuyerBlock';

vi.mock('../api', /* 本地订单命令替身，不查询平台或真实订单。 */ () => ({ addOrderBuyerBlock: vi.fn() }));
afterEach(/* 每例清理组件，触发请求取消。 */ () => { cleanup(); vi.resetAllMocks(); });

test('订单一键拉黑必须选择范围，失败保留输入，成功报告重复结果', /* 通过真实点击确保不能在打开弹层时默认拉黑全用户。 */ async () => {
  // save 为单次本地命令替身。
  const save = vi.mocked(addOrderBuyerBlock);
  render(<OrderBuyerBlock orderID="order-fixture" buyerID="123" />);
  fireEvent.click(screen.getByRole('button', { name: '加入个人黑名单' }));
  expect((screen.getByRole('button', { name: '确认加入个人黑名单' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('黑名单生效范围'), { target: { value: 'user' } });
  save.mockRejectedValueOnce(new Error('fixture failure')).mockResolvedValueOnce({ created: 0, skipped: 1, affected: 0 });
  fireEvent.click(screen.getByRole('button', { name: '确认加入个人黑名单' }));
  await waitFor(/* 失败后仍留在同一确认界面。 */ () => expect(screen.getByRole('status').textContent).toBe('fixture failure'));
  fireEvent.click(screen.getByRole('button', { name: '确认加入个人黑名单' }));
  await waitFor(/* 重复记录必须说明没有覆盖旧配置。 */ () => expect(screen.getByRole('status').textContent).toContain('原配置保持不变'));
  expect(save).toHaveBeenCalledWith('order-fixture', 'user', '', expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('切换订单卸载时取消旧请求，不显示旧订单成功反馈', /* 模拟父组件按订单主键重建确认组件。 */ async () => {
  // resolve 延迟完成旧订单请求。
  let resolve!: (value: { /** created 是新建条数。 */ created: number; /** skipped 是重复跳过数。 */ skipped: number; /** affected 是本地变更数。 */ affected: number }) => void;
  vi.mocked(addOrderBuyerBlock).mockReturnValueOnce(new Promise(/* finish 捕获旧请求完成能力。 */ finish => { resolve = finish; }));
  // view 可以模拟选择另一笔订单。
  const view = render(<OrderBuyerBlock key="a" orderID="a" buyerID="123" />);
  fireEvent.click(screen.getByRole('button', { name: '加入个人黑名单' }));
  fireEvent.change(screen.getByLabelText('黑名单生效范围'), { target: { value: 'account' } });
  fireEvent.click(screen.getByRole('button', { name: '确认加入个人黑名单' }));
  // signal 是旧订单请求的取消器。
  const signal = vi.mocked(addOrderBuyerBlock).mock.calls[0][3]?.signal;
  view.rerender(<OrderBuyerBlock key="b" orderID="b" buyerID="222" />);
  expect(signal?.aborted).toBe(true);
  await act(/* 旧请求成功也不能污染新订单。 */ async () => resolve({ created: 1, skipped: 0, affected: 0 }));
  expect(screen.queryByRole('status')).toBeNull();
});
