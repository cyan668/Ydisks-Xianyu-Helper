# 定制版维护与官方升级

此仓库是 [官方 Ydisks](https://github.com/Christ9038/Ydisks-Xianyu-Helper) 的公开 Fork。
定制基线为 **v1.0.15**；代码入库不代表线上服务已升级。原作者的 LICENSE、NOTICE 和版权声明保持不变。

## 日常只维护 `codex/custom`

- 默认分支 `codex/custom`：已经接受的官方正式版和本仓库定制改动。
- `codex/<功能名>`：从默认分支新建，完成测试后通过 PR 合并。
- `codex/upstream-vX.Y.Z`：机器人创建的官方升级候选。无冲突时包含真实合并提交；有冲突时保留官方提交并创建草稿 PR。
- `main`：Fork 创建时保留的官方分支，不用于定制、定时同步或部署。
- 本地 `origin` 指向此 Fork，`upstream` 指向原作者；推送默认使用 `origin`。

不要用 GitHub 的 **Sync fork / Discard changes** 覆盖定制分支，也不要对默认分支强推。
默认分支保护要求通过 PR、保持基线最新并通过 Go、前端、冒烟及多数据库四项 CI；禁止强推和删除。
单人仓库不要求额外审批者，但保留讨论解决检查，合并方式只允许 merge commit。

## 官方升级会自动变成 PR

“同步官方正式版”每天北京时间 **03:17 左右**检查官方最新非草稿、非预发布的 `v主.次.补丁` Release。
GitHub 定时任务可能延迟；也可以在 Actions 页点击 **Run workflow**，不必等到凌晨。

1. 已包含此正式版提交：不创建 PR、不重复运行整套应用测试。
2. 有新版且未修改官方工作流：在新分支合并，提出 PR，显式调度官方原有 `ci.yml`，检查候选合并结果。
3. 合并冲突：提出草稿 PR，列出冲突文件，不自动选择一方、不覆盖定制。
4. 已有同版本 PR：复用；若默认分支前进，在候选上追加合并并检查新提交，不改写人工修复。发生新冲突则停止并在运行摘要中列出文件。已被人工关闭：视为跳过该版本，不自动重开。
5. **不会自动合并、发布 Release/镜像、更新线上容器或访问闲鱼。**
6. 新版修改了 `.github/workflows/`：创建一次 Issue 提醒人工审核并授权推送完整候选，不跳过工作流文件，不用长期 PAT 解除权限边界。

升级 PR 必须使用 **Create a merge commit**。Squash/Rebase 会丢失官方发布提交的祖先关系，使后续同步无法可靠判断该版本已接受。

本仓库只启用原有质量门禁和新增同步工作流；官方桌面构建、Docker 发布、Release、Wiki 同步工作流在 Fork 中保持禁用。
特别是上游 Wiki 工作流写死了原作者仓库地址，不应在此 Fork 启用。将来需要发布定制镜像时，另行配置独立命名与发布权限。

## 冲突和检查失败时怎么做

在升级分支解决冲突，保留官方及定制两边的提交历史；不要删除失败测试或用 `ours/theirs` 整体覆盖。
解决后把草稿 PR 标记为 Ready，再运行“同步官方正式版”；它会为尚未检查的候选提交调度 `ci.yml`。
同一提交已经存在 CI 时不会每日重跑。修复代码后产生新提交，再从 Actions 运行同步即可；普通人工 PR 更新也沿用官方 CI。
CI 验证本次同步读取到的默认分支与候选的合并结果；合并 PR 前仍须确认基线最新，默认分支又有新提交时重新运行同步。

手动检查命令：

```powershell
gh workflow run upstream-sync.yml --repo cyan668/Ydisks-Xianyu-Helper
gh run list --repo cyan668/Ydisks-Xianyu-Helper --workflow upstream-sync.yml --limit 5
```

只查看是否有新版、完全不写远端：

```powershell
gh workflow run upstream-sync.yml --repo cyan668/Ydisks-Xianyu-Helper -f dry_run=true
```

## 权限与定时任务维护

使用每次任务自动生成的 `GITHUB_TOKEN`，不需要保存个人 PAT、部署 SSH 密钥或生产凭据。
仓库默认工作流权限保持只读；同步 job 单独获得 contents、pull-requests、issues、actions 写权限，用于候选分支、PR、审核提醒及显式调度 CI。
仓库 Actions 设置允许工作流创建 PR；代码不执行批准或合并。

GitHub 对公开仓库有 **60 天无活动后停用定时工作流**的限制。若 Actions 显示停用，先启用工作流，再手动运行一次；本流程不会制造无意义提交来绕过该规则。
机器人 PR 的事件型 CI 可能等待人工批准，因此同步流程显式使用 `workflow_dispatch` 调度原有 CI，而不依赖机器人 push 自动触发。

- [GitHub 定时与手动事件](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows)
- [GITHUB_TOKEN 的事件触发限制](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow)

## 官方修改了工作流时

`GITHUB_TOKEN` 不提供独立的 workflows 写权限。这类升级由 Issue 提醒，不能承诺所有版本都无需人工参与。
在已经通过 `gh auth login` 授权且具备 workflow 权限的本地环境，先检查官方工作流变更，再完整合并：

```powershell
# 将 vX.Y.Z 换成提醒中的正式版本；不要在未提交改动的工作区执行。
git fetch origin codex/custom
git fetch --no-tags upstream tag vX.Y.Z
git switch -c codex/upstream-vX.Y.Z origin/codex/custom
git merge --no-ff vX.Y.Z
# 若有冲突，人工解决并提交；不要直接选择整个 ours/theirs。
git push --no-follow-tags -u origin codex/upstream-vX.Y.Z
gh workflow run upstream-sync.yml --repo cyan668/Ydisks-Xianyu-Helper
```

同步会复用已经完整包含官方版本的候选分支、创建 PR 并调度 CI。合并完成后关闭对应提醒。
若候选分支已存在，直接检出并核对，不能使用 `-B`、强推或删除分支来覆盖人工工作。

## 定制功能与部署边界

v1.0.15 已新增商品首次回复、卡密售出台账及多项交易可靠性修复。此前对 v1.0.14 做出的功能缺口结论必须先对照新版本复核，避免重复实现。
后续功能按独立 PR 开发，仍遵守 AGENTS.md 和上游的架构、三数据库迁移、协议限频及测试约束。

本仓库只能包含代码、脱敏测试夹具及维护文档。不要上传迁移导出、真实卡密、订单、Cookie、Webhook、数据库、浏览器资料或线上截图。
本次不改变线上版本、库存、账号启用状态和自动化规则；部署前须单独完成数据库与上传目录备份、迁移验证、健康检查及回滚准备。
