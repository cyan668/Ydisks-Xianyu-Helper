// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { getReplyProtection, saveReplyProtection } from './api';
import ReplyProtectionPanel from './components/ReplyProtectionPanel';
import { useReplyProtection } from './replyProtection';

vi.mock('./api', /* 仅替换本地配置接口，不连接平台。 */ () => ({ getReplyProtection: vi.fn(), saveReplyProtection: vi.fn() }));
vi.mock('./components/ContentCooldownPanel', /* 独立内容冷却由自己的页面与 Hook 行为测试覆盖。 */ () => ({ default: /* 本测试只聚焦人工接管设置。 */ () => null }));
beforeEach(/* 重置配置读写替身。 */ () => { vi.resetAllMocks(); vi.mocked(getReplyProtection).mockResolvedValue({ manual_pause_minutes: 0 }); vi.mocked(saveReplyProtection).mockResolvedValue({ manual_pause_minutes: 10 }); });
afterEach(/* 卸载时取消读写请求。 */ () => cleanup());

test('停用账号可保存十分钟设置，失败保留草稿且反馈准确', /* 新能力不需要账号在线。 */ async () => {
  render(<ReplyProtectionPanel accountID="a" />);
  await waitFor(/* 读取默认关闭配置后开放保存。 */ () => expect(screen.getByRole('button', { name: '保存回复保护' }).hasAttribute('disabled')).toBe(false));
  fireEvent.change(screen.getByLabelText('人工回复后暂停（分钟）'), { target: { value: '10' } });
  vi.mocked(saveReplyProtection).mockRejectedValueOnce(new Error('fixture failure'));
  fireEvent.click(screen.getByRole('button', { name: '保存回复保护' }));
  await waitFor(/* 失败必须保留可重试草稿。 */ () => expect(screen.getByRole('alert').textContent).toBe('fixture failure'));
  expect((screen.getByLabelText('人工回复后暂停（分钟）') as HTMLInputElement).value).toBe('10');
  fireEvent.click(screen.getByRole('button', { name: '保存回复保护' }));
  await waitFor(/* 成功反馈须说明只暂停会话回复。 */ () => expect(screen.getByRole('status').textContent).toContain('暂停自动回复 10 分钟'));
  expect(saveReplyProtection).toHaveBeenLastCalledWith('a', 10, expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('切账号取消旧读取且晚到不覆盖，读取失败禁止保存并可重试', /* 不能将旧账号分钟数带入新账号。 */ async () => {
  // finish 延迟第一账号读取，不响应取消以验证代次隔离。
  let finish!: (value: { /** manual_pause_minutes 为旧账号读取返回的分钟配置。 */ manual_pause_minutes: number }) => void;
  vi.mocked(getReplyProtection).mockReturnValueOnce(new Promise(/* resolve 捕获第一账号的迟到响应。 */ resolve => { finish = resolve; })).mockResolvedValueOnce({ manual_pause_minutes: 5 });
  // hook 拥有随账号变化的真实状态机。
  const hook = renderHook(/* props.account 为当前选择账号。 */ ({ account }) => useReplyProtection(account), { initialProps: { account: 'a' } });
  // signal 为第一账号旧请求的取消信号。
  const signal = vi.mocked(getReplyProtection).mock.calls[0][1]?.signal;
  hook.rerender({ account: 'b' });
  await waitFor(/* 只接纳第二账号的本地设置。 */ () => expect(hook.result.current.minutes).toBe(5));
  expect(signal?.aborted).toBe(true);
  await act(/* 旧响应即使成功也无法覆盖当前账号。 */ async () => finish({ manual_pause_minutes: 99 }));
  expect(hook.result.current.minutes).toBe(5);
  vi.mocked(getReplyProtection).mockRejectedValueOnce(new Error('read failed'));
  hook.rerender({ account: 'c' });
  await waitFor(/* 读取失败明确保留不可保存状态。 */ () => expect(hook.result.current.failed).toBe(true));
  await act(/* 无权威配置时不提交默认零值。 */ async () => hook.result.current.save());
  expect(saveReplyProtection).not.toHaveBeenCalled();
  act(/* 用户重试读取，不自动重试任何平台动作。 */ () => hook.result.current.reload());
  await waitFor(/* 默认本地读取替身恢复后才开放配置。 */ () => expect(hook.result.current.ready).toBe(true));
});
