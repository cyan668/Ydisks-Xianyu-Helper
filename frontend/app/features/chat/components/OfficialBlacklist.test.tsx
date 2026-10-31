// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { officialChatBlacklist } from '../api';
import { OfficialBlacklist } from './OfficialBlacklist';

vi.mock('../api', /* 只测试单次 UI 命令和取消状态，不访问闲鱼。 */ () => ({ officialChatBlacklist: vi.fn() }));
afterEach(/* 每例清理会话请求与确认框替身。 */ () => { cleanup(); vi.resetAllMocks(); vi.restoreAllMocks(); });

test('不自动查询，写平台前明确确认，结果不确定后只允许手动查询', /* 区分平台会话名单与个人交易名单，不把失败显示为已解除。 */ async () => {
  // api 为平台命令的可控替身。
  const api = vi.mocked(officialChatBlacklist);
  render(<OfficialBlacklist accountID="a" chatID="chat" />);
  fireEvent.click(screen.getByRole('button', { name: '闲鱼拉黑管理' }));
  expect(api).not.toHaveBeenCalled();
  api.mockResolvedValueOnce({ blocked: false });
  fireEvent.click(screen.getByRole('button', { name: '查询平台状态' }));
  await waitFor(/* 查询结果决定当前可用的变更动作。 */ () => expect(screen.getByRole('button', { name: '加入闲鱼黑名单' })).toBeTruthy());
  // confirm 验证平台变更必须再次确认，不隐式提交。
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
  fireEvent.click(screen.getByRole('button', { name: '加入闲鱼黑名单' }));
  expect(api).toHaveBeenCalledTimes(1);
  confirm.mockReturnValue(true); api.mockRejectedValueOnce(new Error('结果未确认'));
  fireEvent.click(screen.getByRole('button', { name: '加入闲鱼黑名单' }));
  await waitFor(/* 失败后不能继续依据旧状态再次提交。 */ () => expect(screen.queryByRole('button', { name: '加入闲鱼黑名单' })).toBeNull());
  expect(screen.getByText('尚未查询 / 结果待核对')).toBeTruthy();
  expect(api).toHaveBeenCalledTimes(2);
});

test('切换会话取消旧查询并丢弃晚到的平台状态', /* 页面按账号及会话主键重建独立的状态机。 */ async () => {
  // resolve 控制旧查询何时返回。
  let resolve!: (value: { /** blocked 是旧会话查询到的平台拉黑状态，不能用于新会话。 */ blocked: boolean }) => void;
  vi.mocked(officialChatBlacklist).mockReturnValueOnce(new Promise(/* finish 捕获旧会话的完成能力。 */ finish => { resolve = finish; }));
  // view 模拟用户选择其他会话。
  const view = render(<OfficialBlacklist key="a" accountID="a" chatID="a" />);
  fireEvent.click(screen.getByRole('button', { name: '闲鱼拉黑管理' }));
  fireEvent.click(screen.getByRole('button', { name: '查询平台状态' }));
  // signal 属于旧会话的唯一请求。
  const signal = vi.mocked(officialChatBlacklist).mock.calls[0][3]?.signal;
  view.rerender(<OfficialBlacklist key="b" accountID="a" chatID="b" />);
  expect(signal?.aborted).toBe(true);
  await act(/* 旧状态不得让新会话显示“已拉黑”。 */ async () => resolve({ blocked: true }));
  expect(screen.queryByText('当前对方已被闲鱼拉黑')).toBeNull();
});
