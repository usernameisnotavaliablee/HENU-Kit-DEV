# HANDOFF — 当前接手摘要

更新：2026-10-16（北京时间）。先读本页，再按需打开证据；不再从头载入历史流水账。
本页只维护当前目标、状态、验收和下一步。历史记录查 Git，不再逐轮追加长日志。

## 接手入口

- 规则与领域边界：[AGENTS.md](AGENTS.md)、[CONTEXT-MAP.md](CONTEXT-MAP.md)、[文档中心](docs/README.md)。
- 产品主线：[PLAN.md](PLAN.md)；行为验收：[学习反馈规格 LF-01～LF-07](docs/development/quizcraft-learning-feedback-spec.md)。
- 先核对工作区与远端；保留用户修改。中文简写，复用现有能力，避免新框架。
- `ask-matt` 现在存在；按 AGENTS 咨询。不沿用旧记录中的“技能缺失”阻塞。

## 104：已核查基线

以下为 **2026-10-16 00:44 +08:00** 的 Git/REST 快照，执行同步前仍须刷新远端。

| 对象 | 固定基线 | 状态 |
| --- | --- | --- |
| 本地 main / fork main | `d1871645` | 两者相同；fork 为 `usernameisnotavaliablee/HENU-Kit-DEV` |
| Jerry main | `11bf9e65` | 源仓库为 `jry21223/HENU-Kit-DEV` |
| fork 相对 Jerry | ahead **140** / behind **24** | 共同祖先 `8d52d8b3`；提交数不是功能数 |

- fork 最后提交 2026-10-09，Jerry 最后提交 2026-10-12；当时 fork PR #1～#8 均已合并，无打开 PR。
- 主线已合入：证据与模型校验、报告存储/清除、worker/调度、会员鉴权、Gateway 与 Portal 报告练习闭环。
- 工程收尾包括契约/sqlc 生成物、CI/分支治理、文案与发布配置；140 笔中 82 笔以 `docs` 开头。
- Jerry 的 24 笔含 11 笔非 merge、13 笔 merge：答题反馈、资料推荐、校园互助降级、中文文案、Food 身份与食评修复。
- 当时只有 `origin` remote；`upstream/main` 是通过 URL 取回的 ref，不等于存在 `upstream` remote。
- `codex/local-playwright-recipe@9ef93341` 保留：`45ac24b2` 已有等价补丁，`9ef93341` 独有补丁不夹带进同步。

计数可复现：

```bash
git rev-list --left-right --count d187164593521e4795ce4e9bcea347879ed86e0d...11bf9e656b1253caddbd60afc9e77f5a07a812e4
```

## 主线还差什么

**代码已接线；产品真实验收与开放门槛未完成。** 不用 commit 数或单测通过率推导完成百分比。

| 阶段 | 已有证据 | 剩余门槛 |
| --- | --- | --- |
| P0 规格 | PLAN、执行规格、学习报告 ADR | ADR 仍 Proposed，待正式工程接受 |
| P1 内容 | 盘点、格式校验、内容导入/审批接口 | 四门课 913 题均 `ready_for_learning_feedback=false`；待标签人工审核、已审讲解、来源使用核验、生产题目映射 |
| P2 契约/数据 | API、生成物、迁移、隔离与清除 | 整合后重验生成一致性、迁移和消费方 |
| P3 评价任务 | 模型适配、worker、自动调度 | 真实供应商调用、人工语义评测 |
| P4 会员闭环 | Gateway、Portal、报告针对性练习 | 真实账号与真实后端联合验收 |
| P5 受控开放 | 健康检查、发布配置、关闭回退步骤 | 灰度观测、回退演练、发布授权 |

- 内容证据：[候选盘点](products/quizcraft/generated/learning_feedback_inventory.json)、[审核规则](docs/development/quizcraft-learning-content.md)。候选题不是可上线题量。
- 规格仍有“供应商/任务/HTTP/界面尚未接线”等旧段落，须据源码纠正；保留真实调用和人工验收待完成结论。
- 发布镜像脚本烘焙浏览器报告入口为 `1`；Dockerfile、env 示例及 Compose 默认仍为 `0`。服务端开关需部署配置。
- 未检查生产服务器；仓库配置不证明线上开放。本轮仅同步和记录，不执行生产开放、食评数据维护或人工审批。

## 105：当前批准的整合任务

用户已授权：同步分支 commit/push → PR 检查 → **merge commit** 合入自己的 fork main。
保留学习报告并接入 Jerry 全部改动；接受校园互助入口暂缓与 Food MCP 新身份接口。
以下为待执行项；本摘要压缩不代表整合、测试、提交或合入已经完成。

- [ ] 刷新 fork/Jerry refs，固定本次 SHA；建 `codex/sync-jerry-main-20261016`，保存并带入 HANDOFF 修改。
- [ ] Merge Jerry。虚拟合并预计唯一文本冲突为 `apps/portal/package.json` 的 `test`：取双方测试并集。
- [ ] 保留报告练习路由、失效返回和完成回报告，同时保留上游选对/选错/漏选反馈；文本自动合并不等于行为正确。
- [ ] Jerry Food ADR 保留 `0047`；本地学习报告 ADR `0047` → `0048`，更新索引、规格、QuizCraft Context。
- [ ] 旧历史中的学习报告 ADR-0047 指改号前的报告决策，不是 Food 决策；改号后更新本页完成状态。
- [ ] Food MCP 客户端刷新工具 schema；共享 QQ/LangBot 部署另需插件、签名密钥与 actor-context 配置。SQL 留作独立维护。
- [ ] 修正规格过期表述，补交叉回归；同步不改变报告开放门禁，不加入 helper 分支独有补丁。

### 整合验收（待执行）

- 报告创建练习 → 错答显示错选/正确项 → 完成 → “再来一组”回当前课程报告；不得误建普通练习会话。
- 保留失效/空会话回归；提供报告与答题页面桌面、移动截图。
- Portal：lint、unit、build；practice / learning-reports / responsive E2E，脚本见 `apps/portal/package.json`。
- Go：使用兼容 1.26.9 的工具链；Food MCP、Console Gateway、Deploy webhook 各跑 vet 与 race 测试。
- 最终 diff 重算 CI 触发面；Food workflow 不跑 Food MCP 单测，必须另验，镜像构建不能代替。
- 按实际改动重验生成物及消费方；Docker/数据库/Linux 门禁在对应环境跑。缺环境、跳过不算通过。
- Standards / Spec / Public-ready Copy 三轴审查；PR 正文 `Review-Head` 绑定最终 SHA，结果如实填写。
- 必需检查全部通过再 merge；禁止直推 main、force push、squash/rebase 合并。失败留同步分支修复。
- 合入后快进本地 main，验证目标 Jerry SHA 是其祖先、对固定目标 behind 0；记录合并 SHA、真实验证与新计数。

## 最少环境与安全提醒

- Portal E2E 保持真实 `HOME`；Go 显式设置 `GOCACHE/GOMODCACHE/GOPATH`。配方与其余陷阱查 [开发指南 §14](docs/DEVELOPMENT.md)。
- 写 Next 代码先读本地 Next 文档。工具/运行时版本、Docker 可用性先实查；旧快照不代替本轮检查。
- 不改原有 AGENTS；不提交密钥、Cookie、真实学生数据或 `资料库/` 内容；不跨服务直连 DB。
- 不伪造人工审核、供应商评测或发布授权；保存原始作答。安全语义和失败路径按执行规格验收。
- 真实 CI 读对应 run 与最终 head；不把历史绿灯、workflow 配置推导或镜像构建写成当前验收通过。

## 历史与后续维护

记录 1～103 在固定提交中，按需定向检索：

```bash
git show d187164593521e4795ce4e9bcea347879ed86e0d:HANDOFF.md
```

压缩前完整原文（含未提交的 104 与用户入口修改）：[本机备份](.cache/sync-jerry-main-20261016/HANDOFF-before-compression.md)。
备份已确认被 Git 忽略，仅本机保存，不随 PR 分发；迁移机器时另行保存。
本页保留有效 commit/push 授权，以最新 PR 合入路径为准。后续覆盖更新状态，详细测试日志放 PR/现有证据，不恢复流水账。
