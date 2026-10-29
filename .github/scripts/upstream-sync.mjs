import { spawnSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

// 只跟踪原作者的正式发布，不使用开发分支、第三方镜像或部署凭据。
export const upstream = 'Christ9038/Ydisks-Xianyu-Helper';
export const upstreamURL = `https://github.com/${upstream}.git`;

/** 运行具名命令；参数不经 shell 拼接，非预期退出码立即终止同步。 */
export function command(program, args, options = {}) {
  const result = spawnSync(program, args, {
    encoding: 'utf8', maxBuffer: 8 * 1024 * 1024, ...options,
  });
  if (result.error) throw result.error;
  return { status: result.status, out: result.stdout.trim(), error: result.stderr.trim() };
}

/** Git 状态码 1 只在调用者明确处理“非祖先/合并冲突”时允许。 */
export function gitCommand(args, allowed = [0]) {
  const result = command('git', args);
  if (!allowed.includes(result.status)) {
    throw new Error(`git ${args[0]} 失败：${result.error || result.out}`);
  }
  return result;
}

/** 使用工作流短期令牌交给 gh 鉴权；配置、日志和文件均不保存令牌。 */
export function githubAPI(path, method = 'GET', body) {
  const args = ['api', path, '--method', method];
  if (body !== undefined) args.push('--input', '-');
  const result = command('gh', args, body === undefined ? {} : { input: JSON.stringify(body) });
  if (result.status !== 0) throw new Error(`GitHub ${method} ${path} 失败：${result.error}`);
  return result.out ? JSON.parse(result.out) : null;
}

/** 正式版标签同时用作分支名及 Git 参数，因此只接受明确的稳定版本格式。 */
export function stableTag(release) {
  if (release.draft || release.prerelease || !/^v\d+\.\d+\.\d+$/.test(release.tag_name || '')) {
    throw new Error('官方最新 Release 不是受支持的 v主.次.补丁 正式版；请人工核对。');
  }
  return release.tag_name;
}

/** 显式调度原有 CI，避免依赖机器人 PR 自动触发；同一提交已有调度则不重复跑。 */
export function ensureCI(api, repository, branch, sha) {
  const runs = api(`repos/${repository}/actions/workflows/ci.yml/runs?event=workflow_dispatch&branch=${encodeURIComponent(branch)}&per_page=100`);
  if (runs.workflow_runs.some(run => run.head_sha === sha)) return 'already-dispatched';
  api(`repos/${repository}/actions/workflows/ci.yml/dispatches`, 'POST', { ref: branch });
  return 'dispatched';
}

/** 官方更改工作流时保留人工授权边界；同版本只创建一个提醒，不存长期高权限令牌。 */
export function requestWorkflowReview(api, repository, tag, base, paths) {
  const marker = `<!-- upstream-workflow-review:${tag} -->`;
  let issue;
  // 分页查找自己的标记，避免仓库已有较多 Issue 时重复创建提醒。
  for (let page = 1; ; page++) {
    const entries = api(`repos/${repository}/issues?state=all&per_page=100&page=${page}`);
    issue = entries.find(entry => !entry.pull_request && entry.body?.includes(marker));
    if (issue || entries.length < 100) break;
  }
  if (!issue) {
    issue = api(`repos/${repository}/issues`, 'POST', {
      title: `[上游同步需审核工作流] ${tag}`,
      body: [marker,
        `官方正式版 [${tag}](https://github.com/${upstream}/releases/tag/${tag}) 修改了 GitHub 工作流：`,
        paths.map(path => `- \`${path}\``).join('\n'),
        '当前同步使用短期 GITHUB_TOKEN，不能自行获取 workflows 权限。未推送升级分支、未丢弃工作流文件、未更新默认分支或线上服务。',
        `请按 [定制维护说明](https://github.com/${repository}/blob/${base}/CUSTOMIZATION.md#官方修改了工作流时) 在已授权的本地 Git 环境合并完整版本并提出 PR。`,
        `候选分支名：\`codex/upstream-${tag}\`；基线：\`${base}\`。`,
        '检查新增工作流是否会发布镜像或部署；准备好候选后再运行同步工作流，它会复用该分支并调度 CI。关闭本提醒表示已知悉，不会每天重复创建。',
      ].join('\n\n'),
    });
  }
  return { status: 'workflow-review-required', tag, issue: issue.html_url, workflowPaths: paths };
}

/**
 * 从官方正式版建立合并候选及 PR，永不推送默认分支、强推、合并 PR 或部署。
 * api/git 可注入本地替身；repoURL 仅供真实临时 Git 仓库测试替换，生产固定为官方地址。
 */
export function synchronize({ repository, base = 'codex/custom', dryRun = false,
  api = githubAPI, git = gitCommand, repoURL = upstreamURL }) {
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository || '') || repository.toLowerCase() === upstream.toLowerCase()) {
    throw new Error('只能在自己的 Fork 中运行同步。');
  }
  git(['check-ref-format', '--branch', base]);
  const metadata = api(`repos/${repository}`);
  if (!metadata.fork || metadata.parent?.full_name !== upstream || metadata.default_branch !== base) {
    throw new Error('仓库必须是官方 Fork，且定制分支必须是默认分支。');
  }
  if (git(['status', '--porcelain']).out) throw new Error('工作区有未提交改动，拒绝同步。');

  const release = api(`repos/${upstream}/releases/latest`);
  const tag = stableTag(release);
  git(['fetch', '--no-tags', 'origin', `refs/heads/${base}`]);
  const baseSHA = git(['rev-parse', 'FETCH_HEAD^{commit}']).out;
  git(['fetch', '--no-tags', repoURL, `refs/tags/${tag}`]);
  const releaseSHA = git(['rev-parse', 'FETCH_HEAD^{commit}']).out;
  if (git(['merge-base', '--is-ancestor', releaseSHA, baseSHA], [0, 1]).status === 0) {
    return { status: 'up-to-date', tag, base, releaseSHA };
  }
  if (dryRun) return { status: 'update-available', tag, base, releaseSHA };

  const branch = `codex/upstream-${tag}`;
  const owner = repository.split('/')[0];
  const pulls = api(`repos/${repository}/pulls?state=all&head=${encodeURIComponent(`${owner}:${branch}`)}&base=${encodeURIComponent(base)}&per_page=100`);
  const existing = pulls[0];
  if (existing?.state === 'closed') {
    // 用户关闭的升级提案不自动重开；之后的新正式版本仍会形成独立提案。
    return { status: 'previously-closed', tag, pullRequest: existing.html_url };
  }
  if (existing) {
    const detail = api(`repos/${repository}/pulls/${existing.number}`);
    if (detail.draft) {
      return { status: 'draft-pr', tag, pullRequest: existing.html_url, ci: 'manual-resolution-required' };
    }
  }

  let conflicts = [];
  const remoteBranch = git(['ls-remote', '--heads', 'origin', `refs/heads/${branch}`]).out;
  if (!remoteBranch && !existing) {
    const commonSHA = git(['merge-base', baseSHA, releaseSHA]).out;
    const workflowPaths = git(['diff', '--name-only', commonSHA, releaseSHA, '--', '.github/workflows']).out.split('\n').filter(Boolean);
    if (workflowPaths.length) return requestWorkflowReview(api, repository, tag, base, workflowPaths);
  }
  git(['config', 'user.name', 'github-actions[bot]']);
  git(['config', 'user.email', '41898282+github-actions[bot]@users.noreply.github.com']);
  if (remoteBranch) {
    // PR 创建失败后可重跑；已有分支绝不强推或覆盖人工修复。
    git(['fetch', '--no-tags', 'origin', `refs/heads/${branch}`]);
    git(['checkout', '--detach', 'FETCH_HEAD']);
    const head = git(['rev-parse', 'HEAD']).out;
    if (git(['merge-base', '--is-ancestor', releaseSHA, head], [0, 1]).status !== 0) {
      throw new Error(`已存在的 ${branch} 不包含官方发布提交，拒绝覆盖。`);
    }
    if (git(['merge-base', '--is-ancestor', baseSHA, head], [0, 1]).status !== 0) {
      // 默认分支前进时追加合并，不改写候选或人工修复提交；CI只验证包含本次基线的候选。
      const refreshed = git(['merge', '--no-ff', '--no-edit', baseSHA], [0, 1]);
      if (refreshed.status !== 0) {
        conflicts = git(['diff', '--name-only', '--diff-filter=U']).out.split('\n').filter(Boolean);
        if (!conflicts.length) throw new Error(`更新候选基线失败：${refreshed.error || refreshed.out}`);
        git(['merge', '--abort']);
      } else {
        git(['push', '--no-follow-tags', 'origin', `HEAD:refs/heads/${branch}`]);
      }
    }
  } else {
    if (existing) throw new Error('现有 PR 的候选分支已删除，请人工核对，不自动重建。');
    git(['checkout', '--detach', baseSHA]);
    const merged = git(['merge', '--no-ff', '--no-edit', releaseSHA], [0, 1]);
    if (merged.status !== 0) {
      conflicts = git(['diff', '--name-only', '--diff-filter=U']).out.split('\n').filter(Boolean);
      if (!conflicts.length) throw new Error(`合并失败但不是文件冲突：${merged.error || merged.out}`);
      git(['merge', '--abort']);
      // 保留官方原提交作为冲突 PR，不选择 ours/theirs，也不丢弃定制代码。
      git(['checkout', '--detach', releaseSHA]);
    }
    git(['push', '--no-follow-tags', 'origin', `HEAD:refs/heads/${branch}`]);
  }

  const headSHA = git(['rev-parse', 'HEAD']).out;
  if (existing) {
    const ci = conflicts.length ? 'manual-resolution-required' : ensureCI(api, repository, branch, headSHA);
    return { status: conflicts.length ? 'conflict-pr' : 'existing-pr', tag, pullRequest: existing.html_url, ci, conflicts };
  }
  const body = [
    `同步官方正式版 [${tag}](https://github.com/${upstream}/releases/tag/${tag})。`,
    `官方提交：\`${releaseSHA}\`；创建时定制基线：\`${baseSHA}\`。`,
    conflicts.length ? `需要人工解决冲突：\n${conflicts.map(path => `- \`${path}\``).join('\n')}` : '已在独立分支合并官方更新，保留定制提交。',
    `CI：[原有质量门禁](https://github.com/${repository}/actions/workflows/ci.yml?query=branch%3A${encodeURIComponent(branch)})。自动调度检查候选合并提交，而非仅检查官方原版。`,
    '- 不自动合并，不自动发布镜像，不操作线上数据库、账号或订单。',
    '- 合并前检查数据库迁移、定制功能回归和部署备份；冲突 PR 解决后请标记就绪并重跑同步工作流以调度 CI。',
    '- 需要保留上游祖先关系，请使用 **Create a merge commit**，不要 Squash/Rebase。',
    '- 关闭此 PR 表示跳过本版本；机器人不会自动重开。',
  ].join('\n\n');
  const pull = api(`repos/${repository}/pulls`, 'POST', {
    title: `[上游同步] ${tag}`, head: branch, base, body, draft: conflicts.length > 0,
  });
  const ci = conflicts.length ? 'manual-resolution-required' : ensureCI(api, repository, branch, headSHA);
  return { status: conflicts.length ? 'conflict-pr' : 'created-pr', tag, branch,
    pullRequest: pull.html_url, ci, conflicts };
}

/** 生产入口只运行在 Actions；本地通过测试夹具验证，不意外推送真实远端。 */
function main() {
  if (process.env.GITHUB_ACTIONS !== 'true') throw new Error('请通过 GitHub Actions 手动运行；本地使用 node --test。');
  const result = synchronize({ repository: process.env.GITHUB_REPOSITORY,
    base: process.env.SYNC_BASE_BRANCH || 'codex/custom', dryRun: process.env.SYNC_DRY_RUN === 'true' });
  console.log(JSON.stringify(result, null, 2));
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY,
    `## 官方正式版同步\n\n状态：**${result.status}**\n\n版本：${result.tag}\n\n${result.pullRequest ? `[查看更新 PR](${result.pullRequest})` : result.issue ? `[查看工作流审核提醒](${result.issue})` : '没有改动默认分支或线上服务。'}\n\n${result.conflicts?.length ? '冲突文件：\n' + result.conflicts.map(path => `- \`${path}\``).join('\n') : ''}\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { main(); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
