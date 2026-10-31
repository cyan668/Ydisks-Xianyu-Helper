// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { getConversationReplyPause, resumeConversationReply } from './api';
import { publishChatLiveMessage } from './liveEvents';
import { useConversationPause } from './conversationPause';
import type { ChatMessage } from './models';

vi.mock('./api', /* 状态与恢复只用本地替身，不发送平台消息。 */ () => ({ getConversationReplyPause: vi.fn(), resumeConversationReply: vi.fn() }));
beforeEach(/* 设置当前会话的确定性接管窗口。 */ () => { vi.resetAllMocks(); vi.mocked(getConversationReplyPause).mockResolvedValue({ version: 1, paused_until: Date.now() + 600000, paused: true }); vi.mocked(resumeConversationReply).mockResolvedValue({ version: 2, paused_until: 0, paused: false }); });
afterEach(/* 清理计时器与消息事件订阅。 */ () => cleanup());

test('恢复只提交所见状态版本，冲突不能假报恢复', /* 真实状态机消费精确比较更新接口。 */ async () => {
  // hook 使用已选择的虚构会话。
  const hook = renderHook(/* 当前账号会话固定。 */ () => useConversationPause('a', 'c'));
  await waitFor(/* 初次本地读取后显示接管。 */ () => expect(hook.result.current.paused).toBe(true));
  // expected 是用户本次看到的精确状态版本。
  const expected = hook.result.current.state!.version;
  vi.mocked(resumeConversationReply).mockRejectedValueOnce(new Error('出现了新的人工接管'));
  await act(/* 旧状态版本恢复被拒绝。 */ async () => hook.result.current.resume());
  expect(hook.result.current.state).toBeNull();
  expect(hook.result.current.error).toContain('新的人工接管');
  expect(resumeConversationReply).toHaveBeenCalledWith('a', 'c', expected, expect.any(Object));
  await act(/* 用户重新读取当前窗口，再明确恢复。 */ async () => hook.result.current.refresh());
  await act(/* 成功后才显示未暂停。 */ async () => hook.result.current.resume());
  expect(hook.result.current.paused).toBe(false);
  expect(hook.result.current.error).toBe('');
});

test('切换取消旧状态，当前出站终态才重读且没有后台轮询', /* 实时事件只触发本地接口。 */ async () => {
  // finish 保存第一会话的迟到响应。
  let finish!: (value: { /** version 是旧快照的版本。 */ version: number; /** paused_until 是旧会话毫秒截止。 */ paused_until: number; /** paused 是旧会话读取时状态。 */ paused: boolean }) => void;
  vi.mocked(getConversationReplyPause).mockReturnValueOnce(new Promise(/* resolve 捕获旧会话完成能力。 */ resolve => { finish = resolve; })).mockResolvedValue({ version: 0, paused_until: 0, paused: false });
  // hook 按当前会话变化，旧响应必须失效。
  const hook = renderHook(/* props.chat 是用户选择的会话。 */ ({ chat }) => useConversationPause('a', chat), { initialProps: { chat: 'old' } });
  // signal 属于第一会话读取。
  const signal = vi.mocked(getConversationReplyPause).mock.calls[0][2]?.signal;
  hook.rerender({ chat: 'new' });
  await waitFor(/* 第二会话应显示服务器未暂停状态。 */ () => expect(hook.result.current.state?.paused_until).toBe(0));
  await act(/* 迟到旧结果不能把新会话变为接管。 */ async () => finish({ version: 1, paused_until: Date.now() + 600000, paused: true }));
  expect(signal?.aborted).toBe(true);
  expect(hook.result.current.paused).toBe(false);
  // message 是当前会话出站消息的最小完整夹具。
  const message: ChatMessage = { id: 1, account_id: 'a', chat_id: 'new', message_key: 'key', direction: 'outgoing', sender_id: 'a', sender_name: '我', message_type: 'text', content: 'fixture', status: 'sent', sent_at: Date.now() };
  // before 保存当前本地读取次数。
  const before = vi.mocked(getConversationReplyPause).mock.calls.length;
  act(/* 其他会话与 sending 状态都不触发查询。 */ () => { publishChatLiveMessage({ ...message, chat_id: 'other' }); publishChatLiveMessage({ ...message, status: 'sending' }); });
  expect(getConversationReplyPause).toHaveBeenCalledTimes(before);
  await act(/* 只有当前会话已发送事件刷新本地暂停。 */ async () => publishChatLiveMessage(message));
  expect(getConversationReplyPause).toHaveBeenCalledTimes(before + 1);
});

test('恢复在途的新人工事件读取优先，旧恢复响应不能把新窗口显示为已恢复', /* 验证恢复与实时事件的交错，不按响应完成顺序覆盖。 */ async () => {
  // finish 延迟已提交恢复的成功响应。
  let finish!: (value: { /** version 为旧恢复后的版本。 */ version: number; /** paused_until 为旧恢复截止。 */ paused_until: number; /** paused 为旧状态。 */ paused: boolean }) => void;
  vi.mocked(resumeConversationReply).mockReturnValueOnce(new Promise(/* resolve 捕获旧恢复完成能力。 */ resolve => { finish = resolve; }));
  // hook 为当前会话真实状态机。
  const hook = renderHook(/* 固定账号与会话，交错只发生在操作代次。 */ () => useConversationPause('a', 'c'));
  await waitFor(/* 初始接管已显示后开始恢复。 */ () => expect(hook.result.current.paused).toBe(true));
  // pending 保存旧恢复的在途生命周期。
  let pending!: Promise<void>;
  act(/* 用户提交明确恢复，但响应暂时挂起。 */ () => { pending = hook.result.current.resume(); });
  vi.mocked(getConversationReplyPause).mockResolvedValueOnce({ version: 3, paused_until: Date.now() + 600000, paused: true });
  await act(/* 模拟恢复之后又产生人工消息，触发读取更新版本。 */ async () => publishChatLiveMessage({ id: 2, account_id: 'a', chat_id: 'c', message_key: 'new-manual', direction: 'outgoing', sender_id: 'a', sender_name: '我', message_type: 'text', content: 'fixture', status: 'sent', sent_at: Date.now() }));
  expect(hook.result.current.state?.version).toBe(3);
  await act(/* 旧恢复成功响应不能覆盖更新的版本。 */ async () => { finish({ version: 2, paused_until: 0, paused: false }); await pending; });
  expect(hook.result.current.paused).toBe(true);
  expect(hook.result.current.state?.version).toBe(3);
});
