import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { command, ensureCI, stableTag, synchronize, upstream } from './upstream-sync.mjs';

/** 在操作系统临时目录建立独立 Git 夹具；不接触真实 GitHub 仓库。 */
function fixture({ conflict = false, ahead = true } = {}) {
  const root = mkdtempSync(join(tmpdir(), 'ydisks-sync-test-'));
  const source = join(root, 'official');
  const origin = join(root, 'fork.git');
  const checkout = join(root, 'checkout');
  const run = (cwd, args, allowed = [0]) => {
    const result = command('git', args, { cwd });
    assert.ok(allowed.includes(result.status), `git ${args.join(' ')}: ${result.error}`);
    return result;
  };
  mkdirSync(source);
  run(source, ['init', '--initial-branch=main']);
  run(source, ['config', 'user.name', 'fixture']);
  run(source, ['config', 'user.email', 'fixture@example.invalid']);
  writeFileSync(join(source, 'shared.txt'), 'base\n');
  run(source, ['add', '.']);
  run(source, ['commit', '-m', '基础版本']);
  run(source, ['tag', 'v1.0.0']);
  run(root, ['clone', '--bare', source, origin]);
  run(root, ['clone', origin, checkout]);
  run(checkout, ['config', 'user.name', 'fixture']);
  run(checkout, ['config', 'user.email', 'fixture@example.invalid']);
  run(checkout, ['checkout', '-b', 'codex/custom']);
  writeFileSync(join(checkout, conflict ? 'shared.txt' : 'custom.txt'), 'custom change\n');
  run(checkout, ['add', '.']);
  run(checkout, ['commit', '-m', '保留定制']);
  run(checkout, ['push', 'origin', 'codex/custom']);
  if (ahead) {
    writeFileSync(join(source, conflict ? 'shared.txt' : 'upstream.txt'), 'official update\n');
    run(source, ['add', '.']);
    run(source, ['commit', '-m', '官方更新']);
    run(source, ['tag', 'v1.0.1']);
  }
  const baseSHA = run(checkout, ['rev-parse', 'origin/codex/custom']).out;
  const tag = ahead ? 'v1.0.1' : 'v1.0.0';
  const calls = [];
  const repository = 'fixture/fork';
  const pull = { number: 1, state: 'open', html_url: 'https://github.com/fixture/fork/pull/1' };
  let existingPull = null;
  const dispatchedSHAs = [];
  const api = (path, method = 'GET', body) => {
    calls.push({ path, method, body });
    if (path === `repos/${repository}`) return { fork: true, parent: { full_name: upstream }, default_branch: 'codex/custom' };
    if (path === `repos/${upstream}/releases/latest`) return { tag_name: tag, draft: false, prerelease: false };
    if (path.includes('/pulls?')) return existingPull ? [existingPull] : [];
    if (path.endsWith('/pulls') && method === 'POST') {
      existingPull = { ...pull, draft: body.draft };
      return existingPull;
    }
    if (path.endsWith('/pulls/1')) return { ...existingPull, mergeable: null, head: { sha: run(checkout, ['rev-parse', 'HEAD']).out } };
    if (path.includes('/actions/workflows/ci.yml/runs')) return { workflow_runs: dispatchedSHAs.map(sha => ({ head_sha: sha })) };
    if (path.endsWith('/dispatches') && method === 'POST') { dispatchedSHAs.push(run(checkout, ['rev-parse', 'HEAD']).out); return null; }
    throw new Error(`未声明的 API：${method} ${path}`);
  };
  return { root, checkout, origin, source, baseSHA, calls, run, api,
    options: { repository, api, repoURL: source, git: (args, allowed) => run(checkout, args, allowed) },
    setPull(value) { existingPull = value; }, getDispatches() { return dispatchedSHAs.length; } };
}

test('正式版本校验拒绝预发布、草稿和恶意 Git 参数', () => {
  assert.equal(stableTag({ tag_name: 'v1.2.3' }), 'v1.2.3');
  for (const release of [{ tag_name: '--upload-pack=bad' }, { tag_name: 'v1.2.3-rc1' },
    { tag_name: 'v1.2.3', prerelease: true }, { tag_name: 'v1.2.3', draft: true }]) {
    assert.throws(() => stableTag(release));
  }
});

test('已包含官方版本时不创建分支、PR或重复CI', () => {
  const f = fixture({ ahead: false });
  assert.equal(synchronize(f.options).status, 'up-to-date');
  assert.equal(f.calls.filter(c => c.method !== 'GET').length, 0);
  assert.equal(f.run(f.checkout, ['ls-remote', '--heads', 'origin', 'refs/heads/codex/upstream-*']).out, '');
});

test('只读预检发现更新，但不写远端', () => {
  const f = fixture();
  assert.equal(synchronize({ ...f.options, dryRun: true }).status, 'update-available');
  assert.equal(f.calls.filter(c => c.method !== 'GET').length, 0);
  assert.equal(f.run(f.checkout, ['ls-remote', '--heads', 'origin', 'refs/heads/codex/upstream-*']).out, '');
});

test('真实Git合并保留两边内容，只推候选分支并显式调度CI', () => {
  const f = fixture();
  const result = synchronize(f.options);
  assert.equal(result.status, 'created-pr');
  assert.equal(f.run(f.checkout, ['show', 'HEAD:custom.txt']).out, 'custom change');
  assert.equal(f.run(f.checkout, ['show', 'HEAD:upstream.txt']).out, 'official update');
  assert.equal(f.run(f.checkout, ['ls-remote', 'origin', 'refs/heads/codex/custom']).out.split('\t')[0], f.baseSHA);
  assert.equal(f.calls.find(c => c.method === 'POST' && c.path.endsWith('/pulls')).body.draft, false);
  assert.equal(f.getDispatches(), 1);
  assert.equal(synchronize(f.options).status, 'existing-pr');
  assert.equal(f.getDispatches(), 1);
});

test('冲突创建草稿，默认分支和定制文件都不被覆盖', () => {
  const f = fixture({ conflict: true });
  const result = synchronize(f.options);
  assert.equal(result.status, 'conflict-pr');
  assert.deepEqual(result.conflicts, ['shared.txt']);
  assert.equal(f.getDispatches(), 0);
  assert.equal(f.run(f.checkout, ['show', `${f.baseSHA}:shared.txt`]).out, 'custom change');
  assert.equal(f.run(f.checkout, ['ls-remote', 'origin', 'refs/heads/codex/custom']).out.split('\t')[0], f.baseSHA);
  assert.equal(f.calls.find(c => c.method === 'POST' && c.path.endsWith('/pulls')).body.draft, true);
});

test('人工关闭的版本不自动重开', () => {
  const f = fixture();
  f.setPull({ number: 3, state: 'closed', html_url: 'https://github.com/fixture/fork/pull/3' });
  assert.equal(synchronize(f.options).status, 'previously-closed');
  assert.equal(f.calls.filter(c => c.method !== 'GET').length, 0);
});

test('PR创建失败重跑复用候选分支，不强推', () => {
  const f = fixture();
  const originalAPI = f.api;
  let failCreate = true;
  const api = (path, method, body) => {
    if (failCreate && method === 'POST' && path.endsWith('/pulls')) throw new Error('模拟创建失败');
    return originalAPI(path, method, body);
  };
  assert.throws(() => synchronize({ ...f.options, api }), /模拟创建失败/);
  const candidate = f.run(f.checkout, ['ls-remote', 'origin', 'refs/heads/codex/upstream-v1.0.1']).out;
  failCreate = false;
  assert.equal(synchronize({ ...f.options, api }).status, 'created-pr');
  assert.equal(f.run(f.checkout, ['ls-remote', 'origin', 'refs/heads/codex/upstream-v1.0.1']).out, candidate);
});

test('默认分支前进后追加合并，并为新候选调度CI，不依赖mergeable暂态', () => {
  const f = fixture();
  synchronize(f.options);
  const oldCandidate = f.run(f.checkout, ['rev-parse', 'HEAD']).out;
  f.run(f.checkout, ['checkout', 'codex/custom']);
  writeFileSync(join(f.checkout, 'new-custom.txt'), 'later customization\n');
  f.run(f.checkout, ['add', '.']);
  f.run(f.checkout, ['commit', '-m', '后续定制']);
  f.run(f.checkout, ['push', 'origin', 'codex/custom']);
  const newBase = f.run(f.checkout, ['rev-parse', 'HEAD']).out;
  assert.equal(synchronize(f.options).status, 'existing-pr');
  assert.equal(f.getDispatches(), 2);
  f.run(f.checkout, ['merge-base', '--is-ancestor', oldCandidate, 'HEAD']);
  f.run(f.checkout, ['merge-base', '--is-ancestor', newBase, 'HEAD']);
  assert.equal(f.run(f.checkout, ['show', 'HEAD:new-custom.txt']).out, 'later customization');
  assert.equal(f.run(f.checkout, ['ls-remote', 'origin', 'refs/heads/codex/custom']).out.split('\t')[0], newBase);
});

test('后续定制与候选冲突时不覆盖分支，也不沿用旧候选调度CI', () => {
  const f = fixture();
  synchronize(f.options);
  const oldCandidate = f.run(f.checkout, ['rev-parse', 'HEAD']).out;
  f.run(f.checkout, ['checkout', 'codex/custom']);
  writeFileSync(join(f.checkout, 'upstream.txt'), 'conflicting customization\n');
  f.run(f.checkout, ['add', '.']);
  f.run(f.checkout, ['commit', '-m', '后续冲突']);
  f.run(f.checkout, ['push', 'origin', 'codex/custom']);
  const result = synchronize(f.options);
  assert.equal(result.status, 'conflict-pr');
  assert.deepEqual(result.conflicts, ['upstream.txt']);
  assert.equal(f.getDispatches(), 1);
  assert.equal(f.run(f.checkout, ['ls-remote', 'origin', 'refs/heads/codex/upstream-v1.0.1']).out.split('\t')[0], oldCandidate);
});

test('冲突草稿由用户决定何时就绪，不自动调度测试或覆盖', () => {
  const f = fixture({ conflict: true });
  synchronize(f.options);
  const candidate = f.run(f.checkout, ['rev-parse', 'HEAD']).out;
  assert.equal(synchronize(f.options).status, 'draft-pr');
  assert.equal(f.getDispatches(), 0);
  assert.equal(f.run(f.checkout, ['rev-parse', 'HEAD']).out, candidate);
});

test('拒绝脏工作区、非官方Fork和写回原作者仓库', () => {
  const f = fixture();
  writeFileSync(join(f.checkout, 'unsaved.txt'), 'do not touch');
  assert.throws(() => synchronize(f.options), /未提交/);
  assert.throws(() => synchronize({ ...f.options, repository: upstream }), /自己的 Fork/);
  assert.throws(() => synchronize({ ...f.options, api: () => ({ fork: false }) }), /官方 Fork/);
});

test('已有CI不重复调度，缺少时才创建dispatch', () => {
  const calls = [];
  const api = (path, method, body) => { calls.push({ path, method, body }); return { workflow_runs: [{ head_sha: 'abc' }] }; };
  assert.equal(ensureCI(api, 'owner/repo', 'codex/upstream-v1.0.1', 'abc'), 'already-dispatched');
  assert.equal(calls.length, 1);
  assert.equal(ensureCI(api, 'owner/repo', 'codex/upstream-v1.0.1', 'def'), 'dispatched');
  assert.equal(calls.at(-1).method, 'POST');
});
