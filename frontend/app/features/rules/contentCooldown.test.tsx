// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { getContentCooldown, listContentCooldownReviews, resolveContentCooldownReview, saveContentCooldown } from './api';
import { useContentCooldown, useContentCooldownReviews } from './contentCooldown';
import type { ContentCooldownPage } from './contentCooldownModels';
import ContentCooldownPanel from './components/ContentCooldownPanel';

vi.mock('./api', /* 只替换本地配置与待核对接口，不启动平台账号。 */ () => ({
  getContentCooldown: vi.fn(), saveContentCooldown: vi.fn(), listContentCooldownReviews: vi.fn(), resolveContentCooldownReview: vi.fn(),
}));

/** reviewPage 构建可控制总数与页位置的保护定位夹具，全部标识均为虚构值。 */
function reviewPage(offset = 0, total = 1): ContentCooldownPage {
  return { items: total > 0 ? [{ id: offset + 1, version: 2, chatID: `chat-${offset}`, messageKey: 'buyer-message', claimedAt: 1000 }] : [], limit: 20, offset, total };
}

beforeEach(/* 每项行为使用独立的本地接口替身。 */ () => {
  vi.resetAllMocks();
  vi.mocked(getContentCooldown).mockResolvedValue(0);
  vi.mocked(saveContentCooldown).mockImplementation(/* account、seconds 分别为当前账号与提交秒数。 */ async (_account, seconds) => seconds);
  vi.mocked(listContentCooldownReviews).mockResolvedValue(reviewPage(0, 0));
  vi.mocked(resolveContentCooldownReview).mockResolvedValue();
});
afterEach(/* 卸载组件会取消在途读写和解除反馈。 */ () => cleanup());

test('成功读取前不能覆盖默认值，保存一小时失败保留草稿，零保留待核对保护', /* 验证可见配置闭环。 */ async () => {
  render(<ContentCooldownPanel accountID="a" />);
  await waitFor(/* 权威设置读取完成后才开放保存。 */ () => expect(screen.getByRole('button', { name: '保存消息冷却' }).hasAttribute('disabled')).toBe(false));
  fireEvent.change(screen.getByLabelText('相同消息等待时间（秒）'), { target: { value: '3600' } });
  vi.mocked(saveContentCooldown).mockRejectedValueOnce(new Error('save failed'));
  fireEvent.click(screen.getByRole('button', { name: '保存消息冷却' }));
  await waitFor(/* 保存失败保留明确错误。 */ () => expect(screen.getByRole('alert').textContent).toBe('save failed'));
  expect((screen.getByLabelText('相同消息等待时间（秒）') as HTMLInputElement).value).toBe('3600');
  fireEvent.click(screen.getByRole('button', { name: '保存消息冷却' }));
  await waitFor(/* 一小时设置反馈使用服务器确认秒数。 */ () => expect(screen.getByRole('status').textContent).toContain('3600 秒'));
  expect(saveContentCooldown).toHaveBeenLastCalledWith('a', 3600, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  fireEvent.change(screen.getByLabelText('相同消息等待时间（秒）'), { target: { value: '0' } });
  fireEvent.click(screen.getByRole('button', { name: '保存消息冷却' }));
  await waitFor(/* 零配置明确说明未知保护仍存在。 */ () => expect(screen.getByRole('status').textContent).toContain('保护仍保留'));
});

test('空值、小数和超限秒数不能保存，读取故障允许明确重试', /* 防止未知配置被零覆盖。 */ async () => {
  vi.mocked(getContentCooldown).mockRejectedValueOnce(new Error('read failed'));
  // hook 拥有真实配置状态机，重试只发生在用户动作后。
  const hook = renderHook(/* 当前测试始终使用同一停用账号。 */ () => useContentCooldown('a'));
  await waitFor(/* 读取故障保留不可保存状态。 */ () => expect(hook.result.current.failed).toBe(true));
  await act(/* 无权威配置不能提交默认零。 */ async () => hook.result.current.save());
  expect(saveContentCooldown).not.toHaveBeenCalled();
  act(/* 用户显式重试读取。 */ () => hook.result.current.reload());
  await waitFor(/* 成功读取后才开放草稿。 */ () => expect(hook.result.current.ready).toBe(true));
  for (const /* value 是不能静默归一为关闭或有效秒数的非法草稿。 */ value of ['', '-1', '86401', '1.5']) {
    act(/* 只更新表单，不发请求。 */ () => hook.result.current.setSeconds(value));
    expect(hook.result.current.valid).toBe(false);
    await act(/* 程序调用同样受输入校验保护。 */ async () => hook.result.current.save());
  }
  expect(saveContentCooldown).not.toHaveBeenCalled();
});

test('切账号取消旧读取和保存，旧响应不能覆盖新秒数或反馈', /* 故意让替身忽略 AbortSignal 检验隔离。 */ async () => {
  // finishRead 捕获旧账号的迟到读取。
  let finishRead!: (seconds: number) => void;
  vi.mocked(getContentCooldown).mockReturnValueOnce(new Promise(/* resolve 为旧读取的可控完成器。 */ resolve => { finishRead = resolve; })).mockResolvedValueOnce(10);
  // hook 拥有随账号变化的真实配置。
  const hook = renderHook(/* account 是当前规则页选中账号。 */ ({ account }) => useContentCooldown(account), { initialProps: { account: 'a' } });
  // oldSignal 是旧读取取消信号。
  const oldSignal = vi.mocked(getContentCooldown).mock.calls[0][1]?.signal;
  hook.rerender({ account: 'b' });
  await waitFor(/* 只接纳 b 的配置。 */ () => expect(hook.result.current.seconds).toBe('10'));
  expect(oldSignal?.aborted).toBe(true);
  await act(/* 旧读取晚到仍不能污染新账号。 */ async () => finishRead(999));
  expect(hook.result.current.seconds).toBe('10');
  // finishSave、saving 捕获 b 的保存及其结果，切到 c 后仍会返回。
  let finishSave!: (seconds: number) => void;
  vi.mocked(saveContentCooldown).mockReturnValueOnce(new Promise(/* resolve 保留 b 的保存完成器。 */ resolve => { finishSave = resolve; }));
  // saving 是必须在测试结束前等待的用户保存操作。
  let saving!: Promise<void>;
  act(/* 同一渲染周期双击只能提交一次。 */ () => { saving = hook.result.current.save(); void hook.result.current.save(); });
  // writeSignal 为 b 的唯一保存信号。
  const writeSignal = vi.mocked(saveContentCooldown).mock.calls[0][2]?.signal;
  hook.rerender({ account: 'c' });
  await waitFor(/* c 只接受自己的默认关闭配置。 */ () => expect(hook.result.current.seconds).toBe('0'));
  expect(writeSignal?.aborted).toBe(true);
  await act(/* b 的成功结果在切账号后被丢弃。 */ async () => { finishSave(10); await saving; });
  expect(hook.result.current.feedback).toBe('');
  expect(saveContentCooldown).toHaveBeenCalledTimes(1);
});

test('核对先确认可取消，失败不报成功，成功后刷新本地列表', /* 不直接从列表按钮执行解除。 */ async () => {
  vi.mocked(listContentCooldownReviews).mockResolvedValue(reviewPage());
  render(<ContentCooldownPanel accountID="a" />);
  await screen.findByText('会话 chat-0');
  fireEvent.click(screen.getByRole('button', { name: '核对并解除' }));
  expect(screen.getByRole('dialog').textContent).toContain('不会补发原消息');
  expect(resolveContentCooldownReview).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '取消' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '核对并解除' }));
  vi.mocked(resolveContentCooldownReview).mockRejectedValueOnce(new Error('state changed'));
  fireEvent.click(screen.getByRole('button', { name: '我已核对，解除保护' }));
  await waitFor(/* 冲突不能被误显示为解除成功。 */ () => expect(screen.getByRole('alert').textContent).toBe('state changed'));
  expect(screen.getByRole('dialog')).not.toBeNull();
  vi.mocked(listContentCooldownReviews).mockResolvedValueOnce(reviewPage(0, 0));
  fireEvent.click(screen.getByRole('button', { name: '我已核对，解除保护' }));
  await waitFor(/* 已确认解除后列表与反馈同时收口。 */ () => expect(screen.getByText('没有需要核对的重复消息保护。')).not.toBeNull());
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(screen.getByRole('status').textContent).toContain('仅后续新消息');
  expect(resolveContentCooldownReview).toHaveBeenLastCalledWith('a', expect.objectContaining({ id: 1, version: 2 }), expect.objectContaining({ signal: expect.any(AbortSignal) }));
});

test('分页清除旧确认，列表故障可刷新且空尾页回退', /* 全部请求均为有界本地分页。 */ async () => {
  vi.mocked(listContentCooldownReviews).mockImplementation(/* offset 是用户明确选择的分页位置。 */ async (_account, offset) => reviewPage(offset, 21));
  render(<ContentCooldownPanel accountID="a" />);
  await screen.findByText('会话 chat-0');
  fireEvent.click(screen.getByRole('button', { name: '核对并解除' }));
  fireEvent.click(screen.getByRole('button', { name: '下一页' }));
  await screen.findByText('会话 chat-20');
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(listContentCooldownReviews).toHaveBeenLastCalledWith('a', 20, expect.objectContaining({ signal: expect.any(AbortSignal) }));
  vi.mocked(listContentCooldownReviews).mockRejectedValueOnce(new Error('list failed'));
  fireEvent.click(screen.getByRole('button', { name: '刷新待核对' }));
  await screen.findByText('list failed');
  vi.mocked(listContentCooldownReviews).mockResolvedValueOnce({ ...reviewPage(20, 1), items: [] });
  fireEvent.click(screen.getByRole('button', { name: '刷新待核对' }));
  await screen.findByText('会话 chat-0');
});

test('切账号取消旧列表和解除，晚到成功不污染新账号', /* 页面切换不会将旧保护应用到另一个账号。 */ async () => {
  // finishList 捕获 a 的迟到列表。
  let finishList!: (page: ContentCooldownPage) => void;
  vi.mocked(listContentCooldownReviews).mockReturnValueOnce(new Promise(/* resolve 为旧账号列表的完成器。 */ resolve => { finishList = resolve; })).mockResolvedValue(reviewPage());
  // hook 拥有本地保护状态及其请求取消。
  const hook = renderHook(/* account 是规则页当前选择账号。 */ ({ account }) => useContentCooldownReviews(account), { initialProps: { account: 'a' } });
  // oldSignal 是 a 的读取信号。
  const oldSignal = vi.mocked(listContentCooldownReviews).mock.calls[0][2]?.signal;
  hook.rerender({ account: 'b' });
  await waitFor(/* b 的保护列表先完成。 */ () => expect(hook.result.current.page?.total).toBe(1));
  await act(/* a 的晚到列表必须被取消代次隔离。 */ async () => finishList(reviewPage(0, 99)));
  expect(oldSignal?.aborted).toBe(true);
  expect(hook.result.current.page?.total).toBe(1);
  act(/* 用户选择 b 的具体旧保护。 */ () => hook.result.current.setPending(reviewPage().items[0]));
  // finishResolve 捕获尚未完成的 b 解除操作。
  let finishResolve!: () => void;
  vi.mocked(resolveContentCooldownReview).mockReturnValueOnce(new Promise(/* resolve 为旧解除请求的完成器。 */ resolve => { finishResolve = resolve; }));
  // resolving 是用户操作的完成承诺，测试必须等到它结束。
  let resolving!: Promise<void>;
  act(/* 同一渲染周期只允许一个明确解除。 */ () => { resolving = hook.result.current.resolve(); void hook.result.current.resolve(); });
  // writeSignal 是旧解除信号。
  const writeSignal = vi.mocked(resolveContentCooldownReview).mock.calls[0][2]?.signal;
  hook.rerender({ account: 'c' });
  await waitFor(/* c 独立读取保护，旧确认清空。 */ () => expect(hook.result.current.pending).toBeNull());
  await act(/* b 解除成功晚到不能给 c 显示成功。 */ async () => { finishResolve(); await resolving; });
  expect(writeSignal?.aborted).toBe(true);
  expect(hook.result.current.notice).toBe('');
  expect(resolveContentCooldownReview).toHaveBeenCalledTimes(1);
});

test('未选择账号时不读取配置或保护', /* 空账号不允许批量覆盖。 */ () => {
  render(<ContentCooldownPanel accountID="" />);
  expect(screen.getByText('请先选择一个账号。')).not.toBeNull();
  expect(getContentCooldown).not.toHaveBeenCalled();
  expect(listContentCooldownReviews).not.toHaveBeenCalled();
});
