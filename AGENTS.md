# AGENTS.md — HENU-Kit-DEV 全仓 agent 规则

全仓唯一 agent 文档。`apps/portal/AGENTS.md`、`apps/portal/CLAUDE.md` 是 `next dev` 自动生成的 Next 块，规则已并入下方「Portal」。

## 结构与边界

- `apps/portal` Next.js 公共站；`apps/console` Vue 管理端；`apps/web`、`apps/study-legacy-admin` 旧 Study 链路。
- `services/` 独立 Go 服务；`products/quizcraft/` = 刷题前端 + Python 后端 + Go 服务。QuizCraft 是唯一刷题产品。
- `packages/api-contracts` 共享契约、`packages/design-tokens` 设计变量；`infra/`、`scripts/` 部署与工具。
- 前端源码在各 app 的 `src/`，测试在源码旁与 `tests/`；静态资源 `apps/portal/public/`、`products/quizcraft/assets/`。
- 先读 `CONTEXT-MAP.md`、`docs/README.md`。`legacy/`、`archive/` 只读 → 不在此新增业务代码。

## 命令（仓库根执行）

- `pnpm install --frozen-lockfile` → 按锁文件装依赖。pnpm 11.9.0（`package.json` 的 `packageManager`）。
- `cp .env.henukit.example .env.henukit` → 首次配置；勿覆盖已有配置。
- `pnpm run dev:henukit` → 推荐完整栈，边缘 http://localhost:8088（`docker-compose.henukit.yml`）；完整环境需 Docker Compose。
- `pnpm dev` → 旧 Study 栈（`docker-compose.dev.yml`），非推荐栈。
- `pnpm --filter @henukit/portal dev` → 单独起 Portal 开发服务。
- `pnpm run lint` → 默认前端静态检查，覆盖 `@final-review/web`、`@henukit/portal`、`@henukit/console`、`quiz-app` 四个包。
- `pnpm run build` → `build:portal` + `build:console` + `build:quizcraft`。
- `pnpm test` → 根组合（libraryctl / ops / web-ui / console / quizcraft），**不含** Portal 全套、Go、Python → 受影响模块必须补跑：`pnpm --filter @henukit/portal test`、`go -C services/library test ./...`（按改动换目录）。
- 生成物与契约是否一致，由「重新生成 + `git diff --exit-code`」判定，**不由 build 判定**（并非不用跑 build：契约改动照样要连着生成器与构建一起跑，见 `docs/DEVELOPMENT.md` §14）。

## 编码风格

- TS/Vue：两空格、双引号、分号；组件 PascalCase，函数与变量 camelCase。Portal 文件多用 kebab-case。
- Go：`gofmt`，文件名 snake_case。一律沿用相邻文件。
- lint：Portal/QuizCraft 用 ESLint；Console 的 lint 是 `vue-tsc --noEmit`。无统一格式化命令，别假定。

## 测试要求

- 框架：Vitest、Node test runner、Playwright、Go testing、QuizCraft Pytest。
- 命名：`*.test.ts`、`*.test.mjs`、`*.spec.ts`、`*_test.go`、`test_*.py`。
- Bug 修复补回归；覆盖失败、权限、并发、重试路径；改共享契约须验证消费方。
- 验收遵循 `docs/development/testing-acceptance-spec.md` 与模块 CI；不自设统一覆盖率数字；集成测试按模块要求准备 PostgreSQL/Redis。

## 提交与 PR

- Commit 用 `type(scope): description`（feat/fix/test/docs/refactor/chore），如 `fix(portal): restore account navigation`。
- 禁直接 push main；一个 PR 只解一个问题。
- 按 `.github/pull_request_template.md` 填背景、范围、非目标、实际验证、风险、发布回滚；关联 Issue（本 fork `hasIssuesEnabled: false`，无 Issue 可关联时写 N/A）。
- 前端改动附桌面 + 移动端截图；安全关键改动附当前 SHA 的 Standards/Spec 审查与失败路径证据。

## 安全

- 不提交密钥、Token、Cookie、真实学生数据、`资料库/` 内容。
- 不跨服务直连数据库；数据 Owner 唯一。
- 数据库迁移 expand → migrate → contract。

## 协作入口

- 先咨询 `~/.agents/skills/ask-matt/SKILL.md`；缺失则明确报告，别默默跳过。
- 已有代码从 `/grill-with-docs` 开始；多会话经 `/to-spec`、`/to-tickets`，再按依赖逐票 `/implement`；实现走 TDD。
- 提交前跑 Standards / Spec / Public-ready Copy 三轴审查；无可见文案改动写 `Public-ready Copy: not applicable`。
- Issue、标签、领域规则见 `docs/agents/`；目录级 `AGENTS.md` 更具体时优先——被 `.gitignore` 的 `apps/portal/AGENTS.md` 是 `next dev` 生成物，不算目录级规则。

## Portal（Next.js）

- 版本 16.3.2，与训练数据不同：写任何 Next 代码前先读 `apps/portal/node_modules/next/dist/docs/`（monorepo 根看不到 `next` 包）；注意 deprecation。
- 该 Next 块由 `next dev` 重写，生成器 `apps/portal/node_modules/next/dist/server/lib/generate-agent-files.js`，同时写 `AGENTS.md` 与 `CLAUDE.md`。两文件已删并进 `.gitignore` → 重新出现属正常，别提交。
- 浏览器门禁 = `pnpm --filter @henukit/portal test:e2e:<名字>`（Playwright，不需 Docker；现有 11 个 `test:e2e:*` 脚本，清单见 `apps/portal/package.json`）；环境变量陷阱见下方「本机环境」。

## CI 现状（实测，代价以天计）

- `.github/workflows/` 共 14 个 workflow：14 个配 `pull_request:`，13 个配 `push: branches: [main]`，4 个配 `workflow_dispatch`：原有的 `deploy-henukit.yml`、`quizcraft-go.yml`，加上 PR #2 补的 `console-gateway.yml`、`portal-gateway.yml`。
- **PR 事件会真的产生 run**（至少 2026-10-08T17:58Z 起如此）。**别把会增长的计数写进文档**：`pull_request` 每开一个 PR 或往 PR 分支 push 一次就涨，`workflow_dispatch` 只在手动触发时涨。要知道当前值就现查——`gh api "repos/:owner/:repo/actions/runs?event=<事件>&per_page=1" -q .total_count`——写进文档时必须带「截至 `<时刻>`」。要引用固定证据就用 run 号（run 的编号与创建时刻不会再变；但同一个 run 会追加作业、重跑会改结论，别把「某个 run 的作业集合」当不变量）。第一次观测到的是 PR #4（创建于 17:57:57Z，第一个 run 在 17:58:01Z）。**会影响哪些作业要看各自的 `paths:`**：改某个 workflow 文件本身会触发它自己（12 个 workflow 的 `paths:` 里有 `.github/workflows/<自身>.yml` 自指条目，PR #4 的 `cfef5a27` 就是这样让 `Portal Gateway` 与 `Console Gateway` 跑起来的；`deploy-henukit.yml` 与 `pull-request-governance.yml` 压根没有 `pull_request.paths:`，任何改动都会触发），改 `docker-compose.henukit.yml` 会让 `Library` 与 `Deploy webhook` 跑（PR #6 的 diff 命中这两份 `paths:`）。**PR #1–#3 期间不是这样**：那时 PR #2 的 opened / reopened / synchronize 三次都是 0 run、`gh pr checks 2` / `gh pr checks 3` 都是 `no checks reported`——那些观测只对**那个窗口**成立，17:58Z 前后到底什么变了没人知道。覆盖面：PR run 里能跑完的作业不止 lint（run `37820701120` 就是例子；某个 run 现在有几个作业、结论如何，用 `gh api repos/:owner/:repo/actions/runs/<run-id>/jobs` 现查，别把某次读到的作业表当成不变量）——不需要手动 dispatch。`review-evidence` 的 fail 要按 run 找原因：脚本按 `Review-Head` → `Standards-Review` → `Spec-Review` 顺序**只报第一个**不匹配的字段（run `37823208131` 报 `Review-Head`，run `37820839366` 报 `Standards-Review`）——「钉着 `pending`」只是其中一种。`pull_request` 的作业跑在 `refs/pull/N/merge` 上（作业日志里能看到 `refs/remotes/pull/N/merge`），这个 ref 由 GitHub 按 head 与 base 生成、改正文不会让它重算，只有 head 收到 push 或 base 前进才刷新；但「检查怎么才会变绿」看它读什么：**读事件载荷的**（`review-evidence`，`pull-request-governance.yml` 声明了 `types: [… edited]` 且从载荷取 `PR_BODY`/`HEAD_SHA`）改正文就重跑、读到的就是新正文，重新钉 pin **不需要** push（证据：head `343e682d` 没动，run `37864441464`（00:22:28Z）failure → run `37864501891`（00:23:11Z，只能来自 `edited`：head 未变、时间线无 `reopened`/`ready_for_review`）success）；**读 ref 内容的**（`branch-name` 读那次 run 的 ref 里那份 workflow 文件）要修复进到那个 ref，head 带上或 base 带上都行（head `b00b9d25` 的 run `37842267527` 在仍是旧正则的 base `3fdf5dd3` 上就 success；head 与 base 都旧时才红，如 head `dbec9079` 的 run `37826553193`；落到含 PR #4 的 base 之后 run `37842923610` 的 `branch-name` 也 success——该 run 整体仍 failure）。两类都一样的是：正文里引用的作业数、逐项结论、run 号与 findings 计数会随 head 与时间失效，必须在最后一次编辑之后按当时的 head 重查。
- **`push` 的 run 计数是 0（截至 2026-10-08T19:06:47Z），但「push 不触发」只能算有条件的观测**：事件流里 `refs/heads/main` 的 `PushEvent` 只有 PR #1 合入那一次（合入 2026-10-07T16:29:56Z → 事件 16:29:58Z），那段时间 `push` run 也是 0；而 PR #2（合入 07:19:32Z）与 PR #3（07:37:49Z rebase 合入 `3fdf5dd3`）两次把 `main` 往前推的合入，**在事件流里没有对应的 `PushEvent`**。所以「有一次 main push 事件而 run 为 0」被观测到过，但 `HANDOFF.md` 第 97 条据以推理的那次合入（PR #2，07:19:32Z）算不算 push 反而查不到——第 98 条记的 PR #3 合入没有配套的 push 结论，两种推理各有缺口，别写成结论。要真 CI：开 PR 后看 `gh pr checks`，或 `gh workflow run <workflow> --ref <branch>`。
- `pull-request-governance.yml` 两个 job：`branch-name` 要求 head 分支名匹配 `^((feature|fix)/[a-z0-9][a-z0-9-]*/hc-[0-9]+|(feature|fix|codex)/[a-z0-9][a-z0-9-]*)$`。原规则只认 `feature|fix/<area>/hc-<n>`：本仓 issues 关闭（`hasIssuesEnabled: false`），没有真实 issue 号可引用，本仓在用的 `codex/*` 一定会被它拒（PR #1–#6 的 head 全是 `codex/*`；`main` 也被拒）；不带编号的 `feature/<area>` 同样被拒，不过本仓从没用过这种名字，这一条是按正则推的、不是观测。规则**只看名字、不查 issue 是否存在**，所以「必失败」这个说法不对——`feature/portal/hc-166` 按正则会过，只是本仓没有这种分支，没人见它真跑过。PR #4 放宽成两种形状都收：**issue 号的要求对上游也一并放掉了**（`pull-request-governance.yml` 里没有任何 `has_issues` 引用，不是条件分支），`docs/DEVELOPMENT.md` 的「规范分支」块与 `docs/development/engineering-release-spec.md` 的「## 1. 分支与 PR」都改写成无条件接受两种形状。真实 CI 证据：run `37820839366`（head `da0fa544`）与 `37823208131`（head `00df2ab7`）的 `branch-name` 都 pass。`review-evidence` 要求 PR 正文里每个字段各自独占一行、值两侧可各带一个反引号，比较前做 `strip().lower()`（大小写不敏感、忽略值两侧空白，值内部空白仍算差异）地含 `Review-Head: <当前 head SHA>`、`Standards-Review: 0 findings`、`Spec-Review: 0 findings`。

## 经验教训

### 取证

- 计数、全称断言前先回查原物并逐个打开候选——先写结论后看证据是本分支返工主因。
- 别从被截断的输出里取数：`| tail -5` 曾把 deploy-webhook 的 6 个 `ok` 包写成 5 个。
- 退出码才是判据，而且要看对：`staticcheck ./... | tail -4` 的 `$?` 是 `tail` 的；`git rev-parse --short A B` 给两个 rev 会 exit 128（`fatal: Needed a single revision`）而 stdout 为空，忽略 `$?` 就当成没事。门禁直接跑、直接看 `$?`；**引用一条命令之前先把它真跑一遍**。
- 观测要带时间戳与窗口，别写成永久事实：PR #3 时「`main` 收到 push 却 0 run」+「PR 事件 0 run」被写成「本 fork 只有 `workflow_dispatch` 会产生 run」（那句话由 `6b46a599` 在 2026-10-08T07:26Z 写入），约 10 小时后 PR #4 的第一个 run（17:58:01Z）就把它推翻了。写「截至 `<时间>`、按 `<命令>` 实测 …」，别写「永远 / 只有」。
- 先枚举清单再报数量：`*/cmd/*contractgen*` 命中 15 个目录，其中 1 个由 `products/quizcraft/go-service/scripts/generate-contract.sh` 驱动（直接 `go run` 的是 14 个）。
- 按用例分块统计，别数字符串出现次数：一次失败会重复打印同句 → 17 条红里 16 条同因，按块是 seal 14 / prepare 1 / activate 1，按字符串是 19/1/1。
- 条件句别写成 CI 事实：CI 有 Docker，三批都跑 → 写「本机因 X 在第一处中止；CI 里会…」。
- 引日志条目先 `git log -S'### 86 —' -- HANDOFF.md` 定位引入它的提交，否则会错引条目号。
- 生成物记 SHA 的方式不止一种，报数前先枚举形状：①头注释 `// Code generated from <yaml> (SHA256 …)`；②头注释 `// Code generated by <pkg> from <yaml> (SHA256 …)`；③头里没有 SHA、正文常量里带（`SourceSHA256`、`PortalSessionSourceSHA256`、`QuizCraftCatalogContractSHA256`）；④摘要按 `sha256(A‖B)` 算（`services/portal-summary/cmd/contractgen`，但落盘用的还是 ① 的头）。**完全不含 SHA** 的是 sqlc（9 个文件）、oapi-codegen（`products/quizcraft/go-service/internal/contract/types.gen.go`）与 `openapi-typescript-codegen`（`products/quizcraft/web-app/src/generated/quizcraft-api/` 下 107 个 tracked 文件）→ 只能靠重生成 + `git diff` 验。按一种正则数必漏数：用 `SourceSHA256 =` 去数就漏掉另两个常量名。

### 本机环境（本机 ≠ CI）

- `HOME` **不能**重定向给需要 Next dev 的浏览器门禁（`Could not find the Next.js package` → `PageNotFoundError` / `.next` 的 `ENOENT` → Playwright `Timed out waiting 120000ms from config.webServer`）；Go 侧相反，必须显式给 `GOCACHE`/`GOMODCACHE`/`GOPATH`，否则 `failed to initialize build cache`。两套重定向互斥 → 配方见 `docs/DEVELOPMENT.md` §14。
- 在把 `HOME` 指向 fake home 的那套配方里，npm 还要同时给 `npm_config_logs_dir`（只给 `npm_config_cache` 会踩到 `npm error Log files were not written…` 中止）；不动 `HOME`、只重定向 `npm_config_cache` 时无此问题。语境与实测见 `docs/DEVELOPMENT.md` §14。
- 本机 Node 26 默认开 experimental webstorage → `apps/console` 的 `src/lib/pending-operations.spec.ts` 挂 1 条；加 `NODE_OPTIONS=--no-experimental-webstorage` 即 19/19。CI 固定 node 22（14 个 workflow 里 12 个 pin 22，`portal-summary.yml` 与 `pull-request-governance.yml` 不装 Node）。
- 无 Docker 时的红必须点名文件 + 错误再归因：`spawnSync docker ENOENT`、`panic: rootless Docker not found`（testcontainers）、`JSON.parse` 空 compose 输出、materials 的 `fixed Node runtime is unavailable`、缺 `shellcheck`/systemd。「环境问题」不算归因。
- 需要 Docker 的步骤先找该工具的独立二进制：`sqlc` 官方 release 是纯 Go 静态二进制，可直接复现 `docker run sqlc/sqlc:1.31.0 generate`（`go install …@v1.31.0` 因 go.mod 带 replace 指令不可行）。真 CI 第一次（run `37657421506`，main）就红在只有 Docker 那半跑得到的 `sqlc generate` 缺 75 行。
- `gh` 写不了 `~/.cache/gh` 时，`gh run view --log-failed <id>` 直接失败；给个可写的缓存目录就能拿到原始日志：`XDG_CACHE_HOME=/tmp/ghcache gh run view --log-failed <id>`。
- 子代理环境可能与本机不同（它要 `chromium_headless_shell-1228`，本机缓存 1243）：依赖本机缓存/二进制的门禁（浏览器 e2e）它只能 `--list` 数用例、验不了通过 → 别把它的「跑不了」当证据；它跑得了的门禁照样给出精确数。

### 流程

- 先算触发面再决定验什么：把每个 workflow 的 `paths:`（`pull_request` 与 `push` 两处）对要验的那次改动做 glob，列出**配置层**会匹配哪些作业，再按「CI 现状」判断哪些真的会跑。从日志条目出发会漏整条作业——`services/console-gateway` 的 account-portfolio 生成物陈旧就是这样才逮到的。
- 改了契约/生成物就重跑所有消费方生成器并 `git diff --exit-code`；漏一个 `cmd/*contractgen*`，CI 的 `git diff --exit-code` 就红（只改文件头 SHA 也算）。
- 一个回合只做一个大操作，push 后再写日志；日志条目要能被仓库证据复核（数字、条目号、文件路径）。
- 校验与动作之间要有失败短路：`python3 - <<'PY' … PY` 之后**换行另起**的命令与前一条不构成 `&&` 链，断言 exit 1 也拦不住后面的 `gh pr merge`（PR #3 就是在正文还写着 `Standards-Review: pending` 时被合并的）→ 放进同一条 `&&` 链，或让脚本 `set -e` / 显式检查 `$?`。
- 三轴评审的返工几乎都出在计数与口径，不出在代码：写完自查「计数 / 全称 / 条件句 / 引用出处」四项。
- 分清「文档还能再打磨」与「目标是否达成」：PR #2 的触发面是 5 个 workflow（其中两个没有 `paths:` 过滤），能跑通的三个作业在 `a802b5c2` 上真 CI 全绿（QuizCraft Go `37661750680`、Console Gateway `37661761404`、Portal Gateway `37661770849`，均 `gh workflow run <wf>.yml --ref <branch>`），剩下的学习报告切流窗口要人工决定（`branch-name` 已在 PR #4 放宽成可满足，见「CI 现状」） → 继续润色文档不是进展。
