# HANDOFF — 可信学习反馈实施交接

## Agent 接手入口

- 工作区：`/Users/mac/Documents/HENU-Kit-DEV`。日期：2026-09-29。
- 先读根 `AGENTS.md`、`PLAN.md`，再读本文件最新记录。每次大操作追加做了什么、验证证据、下一步、阻塞。
- 目标是落实完整计划并开始实施；不得把文档完成或少量单测通过写成产品全部完成。
- 不修改或提交原有 `AGENTS.md`；恢复点中的用户提交保留不动。每次大操作更新本文件并单独本地 commit；不 push、不做生产变更、不伪造人工审核。
- `ask-matt` 技能不存在，已报告；不声称执行成功。Ponytail/Caveman 生效：复用现有能力，不建多余框架。

## 当前状态

用户手工恢复检查点 `6eda2bd7`，当前分支 `codex/learning-feedback`。工具权限已恢复，不再受历史审批适配器阻塞。000012 学习报告迁移、运行验证与指定数据库回归通过；本阶段独立提交，提交 SHA 以 git log 为准。七个学习报告操作的 Go/TypeScript 生成同步及契约验证已完成；内容包、真实证据、模型输入/输出校验及报告组合已完成；偏好与原子清除已完成；任务去重/租约、真实模型调用/后台任务、会员/UI、人工审核及发布验收仍待完成；功能保持默认关闭。后续每次大操作包含 HANDOFF 并单独 commit；不改 AGENTS.md，不 push。

## 执行记录（只追加）

### 2026-09-29 / 01 — 恢复现场并写入计划主体

- 核对：`git status --short` 仅有 ` M AGENTS.md`、`?? PLAN.md`；HANDOFF 不存在。未发现中断后额外变更。
- 已做：读取 Ponytail；替换旧 PLAN 主线，写入人类摘要、证据/LLM/内容边界、服务与权限机制；创建本交接。
- 验证：确认 python3、go、pnpm、node、gh 在 PATH；尚未运行产品测试。
- 下一步：补全 PLAN 阶段、验收与默认值；读取工程规格/契约生成流程，开始 ADR、内容盘点及契约基础。
- 阻塞：此前远程切换/联网遭审批系统异常；本轮未重试，不能声称远程已连接。

### 2026-09-29 / 02 — 完成新版 PLAN

- 已做：补全 P0–P5 依赖、接口边界、测试/发布门槛、1–30 天周期等明确默认值、远程与授权限制；无可视化。
- 验证：文档完整性检查通过（两部分、P0–P5、会员/周期/证据/讲解/回退/HANDOFF）；尚未执行产品测试。
- 下一步：检查 Accepted ADR、API 生成器、服务数据模型，新增本迭代 ADR/内容基础和契约后开始 TDD。
- 未完成：人工标签/讲解审核、真实模型评测、后端/前端实现及所有发布门禁。

### 2026-09-29 / 03 — 新迭代规格与基线阻塞

- 已做：新增 Proposed ADR-0047、LF-01～LF-07 规格；更新 ADR 索引及 QuizCraft Context。未伪造 Accepted/上线审批。第一次写入因 Python 换行转义 SyntaxError 未执行，修正后成功。
- 基线：`go -C products/quizcraft/go-service test .` 失败，无权创建 `/Users/mac/go` 缓存。
- require_escalated 重试被审批系统异常拒绝：`oai-basispoints does not implement structured text.format output`。未绕审批、未搬缓存重跑、未降级 go.mod，不声称测试通过。
- 本机 Go 1.26.5，模块要求 1.26.6；运行需获准缓存、工具链/依赖访问。
- 下一步：完成可离线运行的课程盘点及审核缺口，继续契约和测试；Go 验证保持待执行。

### 2026-09-29 / 04 — P1 可复现内容盘点（TDD）

- 已做：新增 `scripts/learning_feedback_inventory.py`（QuizCraft 下）、对应 unittest、生成候选盘点 JSON。源题库未改。
- 红灯：先运行 unittest，因实现模块不存在得到 ModuleNotFoundError。实现后同命令 4 项通过；`--write`、`--check` 通过。
- 事实：计算机基础 556、Java 148、Web 119、软件工程 90，共 913 题；这是本地候选，不是上线题量或合格标签数量。
- 审核：盘点输出始终不授予发布许可；文件名含 reviewed 不代表知识点/能力题型、讲解、资料使用或生产映射已审核。
- 下一步：固定分类/讲解审核格式；新增独立 Learning Reports 契约及数据库基础，继续证据/任务实现。
- 未完成：真实标签与讲解审核、全部 Go 验证、真实模型评测；不能因这 4 项测试通过而标记 P1 或全项目完成。

### 2026-09-29 / 05 — 报告契约、审核规则和提交约定

- 已做：quizcraft.yaml 新增 LearningReports、7 个带服务签名/actor 的内部操作、15 个 schema；覆盖偏好、异步汇总、最新报告、任务、清除和练习。
- TDD：契约用例先有 3 项因缺路由/schema 失败；写入后 4 项通过。Ruby/Psych 仅作离线 YAML/引用/约束检查，不能替代 Go OpenAPI validator；无 Ruby 环境明确跳过该辅助检查。
- 新增 `docs/development/quizcraft-learning-content.md`，明确标签、资料使用、逐条讲解审核与版本撤回。前次脚本因换行 SyntaxError 未执行，本次修正。
- 实施默认：按 bank_id 保存课程设置，不跨题库猜课程；撤销会员仍允许所有者关闭/清除派生记录，但不能生成/读取会员报告。
- 未完成：标准 Go/TS 生成器因权限/依赖未运行；生成产物存在待同步漂移。没有手改生成文件，新端点未接线。
- 用户新增授权：每次大操作独立 commit，并包含 HANDOFF。只提交本任务文件，不含既有 AGENTS.md；不推送。当前尚无新 commit。
- 下一步：将文档、内容盘点、契约拆分提交；随后推进 000012 迁移及证据/任务测试。

### 2026-09-29 / 06 — 按用户要求提交，但审批服务阻塞

- 用户已明确授权每次大操作独立 commit；不需要把这次失败解释成用户没有授权。
- 尝试：创建 `codex/learning-feedback`，仅暂存 PLAN、HANDOFF、ADR-0047/索引、执行规格、QuizCraft Context，然后提交 `docs(quizcraft): freeze evidence-based learning feedback plan`。
- 结果：require_escalated 在创建进程前被审批服务异常拒绝，错误仍是 `oai-basispoints does not implement structured text.format output`。未执行 branch/add/commit，未 push，未动 AGENTS.md。禁止绕过审批。
- 最近验证：`python3 -m unittest discover -s products/quizcraft/tests -p 'test_learning*py'` 共 8 项通过；课程盘点 `--check` 和 `git diff --check` 通过。它们不是 Go、数据库、完整 OpenAPI codegen 或产品 E2E 验证。
- 下一步（先恢复 Git 写入，再继续大操作）：
  1. 核对无外来暂存，创建 codex/learning-feedback；提交上述 P0 文档，HANDOFF 随提交。
  2. 在 HANDOFF 追加 P0 实际 SHA；独立提交内容规则、盘点脚本/JSON/测试（P1 工具，不冒充人工审核通过）。
  3. 在 HANDOFF 追加 P1 SHA；独立提交 quizcraft.yaml 与契约辅助测试，明确生成客户端待同步、端点未接线。
  4. 补齐标准生成流程及 Go 验证权限，再推进 000012 迁移、证据聚合/校验、模型/任务、会员/UI 与门禁；每次大操作同步 HANDOFF 并 commit。
- 暂停新增大操作，避免违背用户“干一次就 commit 一次”的要求。整体目标仍未完成，不能标记 complete。

### 2026-09-29 / 07 — 连续阻塞复核（第 2 个 goal turn）

- 本轮只读核验：仍在 main，HEAD 为 8d52d8b3，暂存区为空；文件改动与记录 06 一致，无阶段提交。
- 未收到审批服务恢复或用户在获知失败后重新明确放行的证据；未重复已被拒绝的 Git/Go 操作，也未改 Git 写入路径来绕过审批。
- 没有开展新的大操作，遵守每阶段先 commit 再推进的要求。仅补充本交接核验记录。
- 当前解除条件：恢复 Git 写入审批；随后按记录 06 先提交 P0/P1/P2 已有工作，再继续实现与真实验证。
- 目标保持未完成。本轮是该 Git 阻塞的第 2 个连续 goal turn；若下一轮仍无解除证据且无可推进工作，按目标规则标 blocked，避免空转。

### 2026-09-29 / 08 — 第三轮阻塞审计

- 当前证据：分支 main、HEAD 8d52d8b3、暂存区为空，任务文件仍未提交；没有发现外部恢复或新提交。
- 同一 Git 写入审批阻塞已持续三个连续 goal turn（记录 06、07、本轮）。原用户授权存在，缺的是可工作的审批链路，不是产品决策。
- 不能绕过已拒绝操作；也不能在用户要求每次大操作提交的前提下继续堆未提交实现。可做的交接和分阶段提交准备已完成，无其他能解除该前置条件的安全操作。
- 满足 blocked 条件；目标不得标 complete。暂停自动推进，保留全部当前改动，不重置、不清理、不替用户提交原有 AGENTS.md。
- 恢复条件：修复审批服务的 structured text.format 错误，恢复 Git 写权限；重新核验现场，先按记录 06 完成独立阶段提交，再继续 P2–P5。Go 缓存/工具链审批与人工内容审核仍是后续未完成项。

### 2026-09-29 / 09 — 从手工检查点恢复，新增持久化基础

- 恢复证据：`git branch --show-current` 为 codex/learning-feedback，HEAD 为 6eda2bd7，开始时工作区和暂存区干净。手工检查点包含之前工作（包括用户已提交的 AGENTS.md）；不重写该提交，后续不修改/暂存 AGENTS.md。
- 本次大操作：新增 000012 up/down 迁移，6 张 QuizCraft 自有表：审核内容版本、发布指针、审核事件、用户偏好、持久化任务、派生报告。旧评分、作答与 mastery 不改。
- 保护：默认关闭/每 7 天/1–30 天约束；显式同意及代次；内容审核后不可变；租约、同意代次、题库/内容版本联合校验；短事务锁住发布输入，不在模型调用期间持锁。撤销后重新开启不能复活旧代次任务。
- 测试先写：新增数据库用例覆盖默认值、周期/同意限制、跨用户/跨题库、错误/过期租约、关闭目录、内容撤回、回滚后题目保留和再应用；同步旧 baseline adoption 对 000012 的计数预期。
- 实际验证：新 Go 文件 gofmt 解析/格式检查通过；`git diff --check` 通过；既有 Python 学习反馈辅助检查 8 项通过。
- 未验证：没有 Go 模块缓存，系统 Go 1.26.5 与项目要求 1.26.6 不同，PATH 无 Docker/psql；此前缓存/工具链审批拒绝尚未解除。未重试被拒绝操作、未绕缓存权限、未执行真实 PostgreSQL/Go 测试；不能称这次迁移已通过运行验收或 TDD 红绿闭环。
- 提交主题：`feat(quizcraft): add guarded learning report persistence`。本阶段是否成功提交以实际 git log 为准，不把写文件视为 commit。
- 下一步：先完成本阶段独立 commit；获准测试环境后运行迁移/权限并发/回滚集成测试。然后实现证据快照与内容校验、报告任务仓储；同步标准契约生成产物，再接模型/worker/Gateway/Portal。
- 审查状态：Standards/Spec 仅本地静态自查，独立安全审查未完成；Public-ready Copy: not applicable（本阶段无用户可见界面文案）。功能仍未启用。

### 2026-09-29 / 10 — 数据层提交失败（恢复后第 1 次阻塞）

- 已按本次明确授权尝试本地提交，主题 `feat(quizcraft): add guarded learning report persistence`；请求只含本阶段六个文件，不含 AGENTS.md，不 push。
- require_escalated 再次在创建进程前失败：`oai-basispoints does not implement structured text.format output; omit the format or use type=text`。手工恢复检查点恢复了 Git 状态，但没有修复工具审批适配器。
- 未执行 git add/commit，禁止通过换命令入口、写 Git 索引/对象或更改审批配置绕过拒绝。没有新提交；不能将本阶段写成已交付或已验证上线。
- 待提交范围：
  - HANDOFF.md
  - docs/development/quizcraft-learning-feedback-spec.md
  - products/quizcraft/go-service/db/migrations/000012_learning_reports.up.sql
  - products/quizcraft/go-service/db/migrations/000012_learning_reports.down.sql
  - products/quizcraft/go-service/tests/learning_reports_migration_test.go
  - products/quizcraft/go-service/tests/migration_artifacts_test.go
- 恢复方式：修复审批适配器，或用户在自己的终端仅提交上述六文件并提供 SHA；不要使用 git add -A，不要推送。恢复后核验提交再开始下一次大操作。
- 验证边界不变：仅 gofmt、差异检查和原有 8 项辅助测试通过；PostgreSQL 回归、Go 编译/集成、并发恢复和生成产物同步仍未完成。数据库发布约束不代替后续证据内容校验或实时会员校验。
- 本次是用户恢复后的第 1 个连续阻塞 turn，不沿用上一轮阻塞计数；停止新增大操作，但不将完整目标标为完成。

### 2026-09-29 / 11 — 恢复验证并交付持久化基础

- 权限恢复；记录 06–10 的审批失败仅为历史，不再构成执行阻塞。沿用用户恢复点与分支，不改写用户历史、不提交 AGENTS.md、不 push。
- 完成六张学习反馈自有表及撤销同意、审核版本、租约、发布、删除隔离保护；新增任务输入不可变约束，禁止改代次/snapshot 复活旧任务。发布时仅短事务锁定可撤销输入，模型调用不得持长事务。
- Go 按 go.mod 自动取得 1.26.6，未降低版本。Homebrew PostgreSQL 16.15 使用工作区 `.cache/learning-feedback-postgres` 隔离集群，仅监听 `127.0.0.1:55432`；未启动系统服务，无生产数据。测试 URL：`postgres://quizcraft@127.0.0.1:55432/quizcraft_test?sslmode=disable`。本机 trust 仅用于隔离测试，禁止用于部署。
- 已执行：全部 up 迁移及 000012 重应用；回滚式 SQL 事务核验保护与级联清除。`go -C products/quizcraft/go-service test .` 通过。指定测试 `QUIZCRAFT_TEST_DATABASE_URL=上述URL go -C products/quizcraft/go-service test ./tests -run 'TestLearningReport|TestVersionedMigrationArtifacts' -count=1` 再次通过（2.062s）；覆盖 9 个顶层测试，含 5 个 baseline 子测试。`git diff --check` 通过。
- 尚未执行完整 `go test ./...`、race、并发清除/发布竞态及产品 E2E。数据库约束不替代证据语义校验、实时会员校验或人工审核。
- 提交范围仅本阶段六文件；主题 `feat(quizcraft): add guarded learning report persistence`。交接脚本首次解析失败后已修正并补入同一阶段本地提交；以最终 git log 为准。下一步同步权威契约生成物并验证漂移，再实现证据/内容校验、仓储、worker、Gateway 与 Portal。
- Standards/Spec：本地复核通过；独立审查未完成。Public-ready Copy: not applicable。ADR-0047 仍为 Proposed，真实标签/讲解审核、资料授权、模型评测及发布批准未完成。

### 2026-09-30 / 12 — 同步学习报告契约生成产物

- 前阶段提交 `b31864cf` 已核验；本阶段使用现有 `generate-contract.sh` 与锁定版本生成器，不手改生成代码。新增 LearningReports 必需域、七个操作的路由常量、Go 类型及 TypeScript 服务/模型。
- 新增契约回归先因七个生成路由常量缺失失败，再修改生成器使其通过；断言独立域、服务身份与签名同时必需、必填 actor、依赖故障、周期 1–30 整天、同意字段、禁止 JSON 伪造身份及最多三项结论。
- Redocly 发现新任务/练习路径存在歧义，已将未发布的练习入口改为 `/api/v1/portal/practice/banks/{bank_id}/learning-reports/results/{report_id}/practice-sessions`；操作 ID 不变，未上线接口无兼容迁移。后续 Gateway/Portal 必须使用此路径。
- 验证：指定学习报告契约两项及原导入契约测试通过（0.578s）；Go 根包及生成契约编译通过；重复生成 100 文件逐字节一致；TypeScript 5.2.2 对生成客户端独立 noEmit 检查通过。Redocly 2.39.0 lint 通过，仅三项原有 health/readiness/legacy-ranking 的 4XX 告警，无新增告警。`git diff --check` 通过。
- 未运行完整前端构建、全量后端回归或端到端测试。客户端生成不代表路由已实现。
- 下一步实现严格的版本化内容包校验和真实作答证据聚合，再接仓储、模型、任务与会员界面。真实资料权利/标签/讲解审核、独立审查和发布授权仍未完成，功能保持关闭。
- Standards/Spec：本地检查通过；独立三轴审查未完成。Public-ready Copy: not applicable。单独本地提交，不包含 AGENTS.md，不 push。

### 2026-09-30 / 13 — 内容包结构、版本与来源门禁

- 前阶段契约提交 `98455b40` 已核验。新增 `learning_content.go` / 测试，复用原 JSON EOF 与 SHA256 助手，无新运行依赖。
- 仅解析受限草稿，拒绝未知审核字段、多份 JSON、超限内容、错版本/跨题库成员、重复 ID/引用、错标签类型、可变资料版本和失效来源引用；允许明确的讲解缺口，不在线编造内容。摘要不受 JSON 排版影响。
- TDD：新增测试先因缺少实现编译失败；实现后 3 个顶层测试及 18 个失败路径子用例通过。Go 根包完整测试、`go test -race .`、`go vet .` 和差异检查通过。
- 测试为显式合成样本。校验只能确认结构和权威成员归属，不能验证声明的来源真实性、版权或语义，也不授予审核/发布权限；相关文档已明确。
- 下一步从真实 `quizcraft_practice_attempts` 聚合当前用户/课程/版本的首答、重答、最近答案和去重证据，再将内容校验接入受权导入/审核仓储。模型、worker、会员/界面、E2E 及真实人工门禁仍未完成，保持默认关闭。
- Standards/Spec：本地复核通过；独立审查未完成。Public-ready Copy: not applicable。本阶段独立 commit，不含 AGENTS.md，不 push。

### 2026-09-30 / 14 — 真实作答证据、范围隔离与稳定指纹

- 前阶段内容校验提交 `75ba4431` 已核验。新增 `learning_evidence.go`、单元及数据库集成测试；只读现有事实，不改原作答、评分或 mastery。
- 快照由服务端已同意偏好、已审核目录和当前发布题库共同约束。单事务读取，按题目版本保留可用历史；首答/重答/最近表现分开计数；章节限定和跨用户/课程隔离，缺证据保留零覆盖而不下薄弱结论。
- 最多 24 个去重样本，轮流覆盖标签；输入顺序和单独变化的截止时间不改变指纹。指纹考虑完整题目聚合而非仅截断样本。模型适配仍须额外最小化，不能把含自由文本的内部快照直接外发。
- TDD：集成测试先因方法缺失失败，实现后通过。测试通过现有刷题 HTTP 写入真实记录并重放请求，覆盖首答/重答、历史截止、他人数据、缺同意、跨题库、保留未变题版本、拒绝陈旧内容、更换题版本及未知章节。采样单测验证 24 上限、多标签去重、代表性和乱序稳定。
- 全量 `QUIZCRAFT_TEST_DATABASE_URL=本地隔离数据库 go test -race ./...` 已执行：根包、备份/迁移 CLI 与整个 `./tests` 集成包通过（集成包 10.029s）；唯一失败为未修改的 `cmd/reconcile` 容器测试因 `rootless Docker not found` panic。该测试硬编码 Testcontainers，无本地数据库覆盖入口；未跳过或改写它。不能声称全量通过。日志 `.cache/learning-feedback-go-tests.log`。
- `go vet ./...` 通过；最终字段收紧后指定学习反馈根包/集成包 race 回归再次通过。后续完整 CI 仍需可用 Docker，产品 E2E、真实模型及人工评测未完成。
- 下一步实现受限模型输入、结构/数字/引用验证和审核讲解/已有题组合；然后任务仓储、调度、网关会员与 Portal。功能仍默认关闭。Standards/Spec 本地复核通过，独立审查未完成；Public-ready Copy: not applicable。独立 commit，不含 AGENTS.md，不 push。

### 2026-09-30 / 15 — 模型白名单、结论校验与可信报告组合

- 以 `62ef6fc4` 干净工作区继续。新增 `learning_analysis.go`、`learning_report.go` 和针对性测试；快照 v2 增加样本题型/选项和每标签最多五道现有候选题。无新依赖，闭合答案复用旧评分归一化，不更改旧判分语义。
- 外发对象单独构造，去除身份/数据库实体 UUID/时间/完整历史；自由用户答案默认扣留。模型仅推断、排序和引用，统计和数字观察由服务器生成；拒绝越界引用、错版本、篡改摘要、模型自报统计、过强证据状态和未知练习题。
- 报告复用审核包讲解，既有题诊断/练习、内容缺口和冷启动都有明确分支；零历史不需要模型。单题重做不冒充新增独立证据。来源定位长度与 OpenAPI 对齐，异常历史 null 标准答案不参与评价。标准导入本来拒绝缺答案，未声称发现现网数据污染。
- TDD：测试先因实现缺失失败，再实现通过。定向学习反馈 race 最终通过（根包 1.488s、集成包 2.586s）；真实 HTTP 作答→快照→模型白名单→报告组合通过，最终 JSON 经 OpenAPI schema 校验；供应商响应为显式合成测试数据，未调用真实模型。
- 首次完整根包/集成包重跑因复用持久测试库，出现旧固定 ID/幂等键夹具冲突；未修改或跳过测试、未清理原库。随后新建专用空白库，按 TestMain 两遍应用原始 up 迁移，`go test -race . ./tests -count=1` 通过（1.522s / 7.124s），结束仅删除本次新建库。日志 `.cache/learning-feedback-fresh-race.log` / `learning-feedback-fresh-migrations.log`。后续完整重跑必须同样用新库；原 `quizcraft_test` 仅用于隔离型定向测试。
- `go vet ./...`、`git diff --check` 通过。历史全量 `cmd/reconcile` Docker 环境缺口未解除，不称所有 Go 包的运行验收通过。
- 仅结构/引用校验不保证语义正确。支持性阈值、推断文案、源内容权利和真实模型评测尚未经人工批准；ADR 仍 Proposed，功能关闭。Public-ready Copy：本地检查克制表达、缺口、可跳过及不承诺提分；独立文案/语义审查未完成。Standards/Spec 本地复核通过，独立审查未完成。
- 下一步：运行仓储（偏好代次、自动周期、手动不重置、输入/版本去重、清除失效、租约），再实际供应商适配与 worker、会员网关、Portal 和人工发布验收。本阶段独立 commit，包含 HANDOFF，不包含 AGENTS.md，不 push。

### 2026-09-30 / 16 — 偏好周期、同意代次与原子清除

- 前阶段 `a7995a4d` 已提交。新增 `learning_preferences.go` 与真实数据库测试；复用现有六张表，无新迁移或依赖。
- 默认偏好只读不落库；开启校验课程/章节与同意。周期单独修改不失效评价代次，无变化更新不刷新时间；目标/范围改变使旧任务 cancelled、报告 stale，自动时间不被这些输入变化重置。手动汇总入队仍待实现，不声称已经验收其周期行为。
- 关闭和 DELETE 清除均允许在目录撤回后由受信 owner 调用；清除按契约同时撤销同意、递增代次、删除派生任务/报告，保留原作答。以偏好行作为先取得的锁与清除后的代次占位；后续队列/发布必须同样先锁偏好。报告 stale 列与不可变 body 分离，后续读取不可忽略列状态。
- 同意版本集中为 v1，证据读取拒绝过期版本，普通设置更新不能静默升级旧同意。真实会员和签名验证仍属于未来 HTTP/worker 边界，本阶段不把存储方法当鉴权实现。
- TDD：新增两项数据库测试先因方法缺失失败，实现后通过。覆盖默认不写入、无效更新回滚、1–30 天与明确同意、章节排序/no-op、周期与评价输入分开处理、旧任务/报告失效、过期同意、撤回目录后的关闭、跨用户隔离、原作答保留、重复清除及清除后旧租约晚写拒绝。
- 学习反馈定向 race 通过（1.516s / 2.828s）；新建空白库、按 TestMain 两遍应用 up 迁移后，完整 `go test -race . ./tests -count=1` 通过（1.626s / 7.475s）。临时库已删除，原测试库未清空。日志 `.cache/learning-feedback-preferences-race.log` 与 `learning-feedback-preferences-migrations.log`。`go vet ./...`、差异检查通过；旧 reconcile 的 Docker 运行缺口仍在。
- 下一步仍为运行任务仓储：输入/模型/提示词版本去重、手动不重置周期、租约与恢复、发布/清除并发；然后供应商、worker、网关会员、Portal。入库前还要补内部单答案/快照大小上限和大课程零历史边界测试：外发 128 KiB 上限不等于内部快照内存/持久化上限，不能据此宣布资源门禁完成。
- Standards/Spec 本地复核通过，独立审查未完成；Public-ready Copy: not applicable（本阶段不新增用户文案）。真实人工内容/模型评测与发布批准未完成，功能关闭。独立 commit，包含 HANDOFF，不包含 AGENTS.md，不 push。

### 2026-10-01 / 17 — 模型切换前检查点（测试仍红）

- 用户要求立即用指定 message 提交一次，然后继续实施。本检查点仅收录 `learning_resources_test.go` 与本交接；不碰 AGENTS.md，不 push。
- 两项新增回归先红：300 标签合法课程冷启动被 128 KiB **模型外发**上限误拦；过大用户自由答案从模型输入扣留，但仍可能留在内部快照/报告。`go test . -run 'TestLearning(ReportLargeCourseColdStart|SnapshotRejectsOversizedAnswerBeforeWithholding)$' -count=1` 失败两项，记录在 `.cache/learning-feedback-resource-red.log`（本地忽略文件）。**此 commit 不是测试通过的交付**。
- 下一步：修内部答案和快照字节边界、分离无历史报告验证与外发上限；补 SQL/DB 验证及既有测试，单独写 HANDOFF 并提交。后续再接任务去重/租约。不声称真实会员/模型或人工审核已完成。
- Standards/Spec：只保存明确失败测试，尚不能验收。Public-ready Copy: not applicable。

### 2026-10-01 / 18 — 答案与内部快照资源边界、冷启动解耦

- 上阶段检查点 `626e3a80` 按用户指定 message 已提交，保留了明确的红测试。本阶段修复并补齐 DB 回归：读作答聚合时不返回任何原始答案，先在 SQL 检查首答/最近答案 JSON 的字节数；仅为选出的最多 24 条证据在同一只读事务二次获取答案，超过 4 KiB 直接报错而非截断。旧原始作答不改。
- 课程题干/选项读入总量上限 16 MiB，入库前/模型分析前/报告组合前内部快照 JSON 上限 1 MiB。稳定指纹按全部相关题目的不可变 ID、版本、首答/最近时间/正确性与聚合构造，不再序列化非样本原始答案；单纯墙钟前移仍不创建新证据。
- 合法 300 标签的大课程零历史仍校验摘要与内部快照，但跳过模型请求的 128 KiB 门禁、直接给已有诊断题；**有证据的模型输入上限仍是 128 KiB**。报告组合仍不发布、不接入真实模型。
- TDD：前一检查点两项失败测试，新增内部序列化上限与真实 PG 大答案失败测试；定向学习反馈 `go test -race . ./tests -run '^TestLearning' -count=1` 通过（根包 1.622s / 集成 3.117s）。新建临时空白数据库、两遍执行所有 up 迁移后完整 `go test -race . ./tests -count=1` 通过（1.403s / 7.766s），只删除该临时库。`go vet ./...`、`git diff --check` 通过。日志位于工作区 `.cache/learning-feedback-resources-*`。本机缺 Docker 的旧 reconcile 测试仍未跑通，不能声称所有 Go 包全绿。
- Standards/Spec 本地复核：不传未经白名单的快照，不把过大答案伪装成可用证据；资源限额是安全门禁，真实用户数据容量/性能和教育评测仍待校准。Public-ready Copy: not applicable（本阶段无新用户可见文案）。下一步：偏好锁顺序下入队/去重、手动与自动周期、租约/过期恢复及清除并发；再接 HTTP/会员、供应商/worker 和 UI/人工审核。当前仍默认关闭；不含 AGENTS.md、不 push。

### 19 — 入队、版本去重与周期写入（任务仓储第一步）

- 上阶段 `98574413` 已提交。新增内部 `QueueLearningReport` 与真实 PostgreSQL 集成测试：只读采集证据后，先锁 owner/bank 偏好，再核验同意代次、当前发布内容/题库及版本；将证据与服务端模型/提示词/策略版本一起持久化，最终唯一键区分相关配置，不跨用户复用。
- 相同输入（即使墙钟前移）复用原任务；手动汇总不改自动周期，到期自动汇总顺延周期，即使复用也不造新任务；改模型/提示词的新任务会取消同代次旧排队/运行任务，保留已完成报告。失败/暂停任务冷却后可重排，受限期返回重试秒数；原 snapshot 不可变。当前仅仓储层，不是对外 API。
- TDD：新增接口前集成测试先因类型/方法缺失失败；随后覆盖无同意、无效版本、未到期、手动周期、重复点击、模型/提示词更新、旧任务取消、自动复用顺延、失败冷却与重排、清除及手动/自动并发同一任务。定向 race 根包/集成包通过（1.402s / 2.431s）。新建临时空库，两遍 up 迁移后完整 `go test -race . ./tests -count=1` 通过（1.385s / 7.993s）；临时库已按 trap 删除。`go vet ./...` 与差异检查通过。日志 `.cache/learning-feedback-queue-*`；旧 reconcile 测试需 Docker，未宣称全模块通过。
- Standards/Spec 本地复核：锁序遵循偏好→任务；不持有数据库事务调用模型；未实现实时会员 HTTP 校验、手动防滥用限流、任务租约与发布，不能对外开放；合成审核内容不构成真实发布资格。Public-ready Copy: not applicable。下一步单独实现租约认领/过期恢复/有限重试及旧租约晚写保护，再发布和清除并发；每步另记 HANDOFF/commit。此提交不含 AGENTS.md，不 push。

### 20 — 租约认领、过期恢复与有限重试（任务仓储第二步）

- 上阶段 `66005d20` 已提交。本阶段新增 `learning_leases.go` 与真实 DB 测试；同时把入队的最终指纹/当前审核内容/同意校验提取为共用函数，不引入依赖或新迁移。
- 候选任务先无锁读取，再按偏好→任务加锁并**重新检查**资格和 run_after，防止两个 worker 同时看见过期任务后绕过退避；恢复时先清旧 Token，退避后最多自动发放 3 次租约。快照异常、内容撤回、同意失效分别置 failed/cancelled，不把异常任务交给模型。续期要求有效 Token、内容与同意仍有效，不缩短原期限；失败写入拒绝伪造、过期、被清除或被新租约替换的 Token。权益依赖错误置 paused，不自动向模型重试。
- TDD：接口缺失测试先红；覆盖并发同任务仅一租约、伪造/晚写拒绝、重启式过期恢复、退避、3 次上限、清除后旧 Token 失效、撤回内容与伪造快照、权益依赖暂停后显式重排。定向 `go test -race . ./tests -run '^TestLearning(Leases|Lease|Queue)' -count=1` 通过（根包 1.398s、集成 2.448s）。新建临时空库、两遍 up 迁移后完整 `go test -race . ./tests -count=1` 通过（1.395s / 8.538s），仅删除该临时库；`go vet ./...` 与差异检查通过。日志 `.cache/learning-feedback-leases-*`。旧 reconcile 包仍受 Docker 环境限制，不称全 Go 包运行通过。
- Standards/Spec 本地复核：只交付仓储，不伪称外部权益重检/实际模型调用或安全发布完成；人工内容审核与评测仍缺。Public-ready Copy: not applicable。下一步：服务端报告发布接口，短事务复核租约/同意/内容，模型调用在事务外；然后实时会员网关/worker/前端与真实人工发布验收。此提交不含 AGENTS.md，不 push。

### 21 — 报告幂等发布、实时检查入口与清除竞争保护

- 上阶段 `eb4454a4` 已提交。新增内部 `PublishLearningReport`：要求非空会员权益检查回调；在事务前调用，错误/拒绝 fail closed。短事务内先锁偏好再锁任务，复核同意代次、租约唯一 Token/期限、当前审核内容和题库、持久快照/版本指纹，仅从数据库快照及已审核内容组合报告；报告 INSERT 与 job 置 ready 一起提交。不持 DB 长事务调用外部服务或模型。只允许同一仍有效租约、同意及会员重取既有报告，stale 不返回。
- 修正已就绪 `insufficient_evidence` 报告的任务复用读取条件；它是有效冷启动报告，不可误当缺失而重复生成。合成模型假引用拒绝，非零真实作答无模型决策不得发布。权益检查器目前**只是必须提供的接口**，尚未接真实 Account Portfolio；生产不可用测试合成回调顶替。
- TDD：发布方法缺失先红；新增真实 PostgreSQL 测试覆盖无会员/权益依赖错误、伪造 Token/版本、冷启动幂等与 OpenAPI 校验、真实作答无决策或假引用、慢权益检查期间的清除不受 DB 锁阻塞、发布/清除并发后无复活、撤回内容拒绝。定向 race 通过（根包 1.212s / 集成 3.226s）。新建临时空库、所有 up 迁移两遍后完整 `go test -race . ./tests -count=1` 通过（1.501s / 9.302s），只删除临时库；`go vet ./...` 与差异检查通过。日志 `.cache/learning-feedback-publish-*`。旧 reconcile Docker 缺口仍在。
- Standards/Spec 本地复核：模型结果无法绕过证据/内容复核；发布通过不代表真实内容审核、会员服务或模型评测完成。Public-ready Copy: not applicable（本阶段无新增界面文案）。下一步：真实权益客户端及 HTTP/worker 边界，自动调度与供应商适配，后续 Portal/管理审核/E2E/人工发布门禁。功能继续关闭；不含 AGENTS.md、不 push。
