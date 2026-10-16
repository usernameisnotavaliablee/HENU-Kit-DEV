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
- 规格已据源码纠正供应商适配、任务执行、HTTP、界面及权益接线的旧描述；真实调用与人工验收仍未完成。
- 发布镜像脚本烘焙浏览器报告入口为 `1`；Dockerfile、env 示例及 Compose 默认仍为 `0`。服务端开关需部署配置。
- 未检查生产服务器；仓库配置不证明线上开放。本轮仅同步和记录，不执行生产开放、食评数据维护或人工审批。

## 105：同步 Jerry 的整合与合入门禁

用户已授权：同步分支 commit/push → PR 检查 → **merge commit** 合入自己的 fork main。
保留学习报告并接入 Jerry 全部改动；接受校园互助入口暂缓与 Food MCP 新身份接口。

### 本次固定范围

- 2026-10-16 01:55 +08:00 刷新后固定：fork 基线 `d187164593521e4795ce4e9bcea347879ed86e0d`；Jerry 目标 `11bf9e656b1253caddbd60afc9e77f5a07a812e4`。
- 承接 `codex/sync-jerry-main-20261016` 与 HANDOFF 摘要提交 `b97a1f3e`；helper 分支独有补丁不进入本次同步。
- Portal 测试列表取双方并集；报告练习路由/返回语义与上游选对、选错、漏选反馈一并保留。
- Food ADR 保留 `0047`；学习报告 ADR 改为 `0048`、仍为 Proposed。历史学习报告 ADR-0047 指改号前的报告决策，不是 Food 决策。
- Food MCP 的 create/mine 工具不再接收用户提供的身份参数，客户端须刷新 schema。共享 QQ/LangBot 部署还须插件工具、匹配的专用签名密钥与 `FOOD_MCP_REQUIRE_ACTOR_CONTEXT=true`；真实插件接入仍待验收。食评 SQL 保留为独立维护脚本，本次不执行。
- 学习报告 API、授权、数据结构、开放开关及既有发布构建行为不变。

### 本轮验收与证据

- 交叉回归扩展现有学习报告 E2E：报告创建练习 → 错答显示错误与正确项 → 结算 → “再来一组”回当前课程报告；普通练习创建 POST 必须为零，保留失效/空会话回归。
- 本机 Playwright 缺少匹配的 headless shell，已安装到仓库 `.cache/playwright`；随后合并前回归因缺少 `practice-verdict` 失败，整合后同一交叉用例通过。全套检查结果、桌面/移动截图、最终 head 与审查证据以 PR 为准；本机日志在 `.cache/sync-jerry-main-20261016/`。
- Go 1.26.9 已准备；Food MCP 独立 vet/race 必须通过。Console Gateway 与 Deploy webhook 同样执行 vet/race，数据库、Docker、Linux 门禁由 CI 完成。
- 按当前范围，必需 workflow 为 Governance、发布制品、Console Gateway、Deploy webhook、Food、Library、QuizCraft Go；最后提交仍须按实际 diff 重算。发布制品包含浏览器门禁、完整镜像矩阵与 runtime-config。仅 main 执行的发布证明在 PR 阶段为不适用。
- fork main 当前无保护/必需检查/ruleset。必须逐项核对本次 head/base 的预期作业；缺失、失败、取消或因前置失败跳过均阻止合入，不以“已出现的检查全绿”替代完整门禁。
- Standards / Spec / Public-ready Copy 三轴审查；最后编辑正文后重新确认治理检查，`Review-Head` 绑定最终 SHA。
- 当前摘要不宣称 PR 已合入。只有必需门禁全部成功，才按已授权路径 merge commit 合入；失败留同步分支修复。禁止直推 main、force push、squash/rebase 合并。
- 合入后快进本地 main，核对目标 Jerry SHA 为其祖先、相对固定目标 behind 0；合并 SHA、真实检查结果与新计数记录在 PR 和交付回复，不为自记 SHA 追加提交。

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
