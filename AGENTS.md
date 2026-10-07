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
- 生成物与契约是否一致，由「重新生成 + `git diff --exit-code`」判定，**不由 build 判定**。

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
- 按 `.github/pull_request_template.md` 填背景、范围、非目标、实际验证、风险、发布回滚；关联 Issue。
- 前端改动附桌面 + 移动端截图；安全关键改动附当前 SHA 的 Standards/Spec 审查与失败路径证据。

## 安全

- 不提交密钥、Token、Cookie、真实学生数据、`资料库/` 内容。
- 不跨服务直连数据库；数据 Owner 唯一。
- 数据库迁移 expand → migrate → contract。

## 协作入口

- 先咨询 `~/.agents/skills/ask-matt/SKILL.md`；缺失则明确报告，别默默跳过。
- 已有代码从 `/grill-with-docs` 开始；多会话经 `/to-spec`、`/to-tickets`，再按依赖逐票 `/implement`；实现走 TDD。
- 提交前跑 Standards / Spec / Public-ready Copy 三轴审查；无可见文案改动写 `Public-ready Copy: not applicable`。
- Issue、标签、领域规则见 `docs/agents/`；目录级 `AGENTS.md` 更具体时优先。

## Portal（Next.js）

- 版本 16.3.2，与训练数据不同：写任何 Next 代码前先读 `apps/portal/node_modules/next/dist/docs/`（monorepo 根看不到 `next` 包）；注意 deprecation。
- 该 Next 块由 `next dev` 重写，生成器 `apps/portal/node_modules/next/dist/server/lib/generate-agent-files.js`，同时写 `AGENTS.md` 与 `CLAUDE.md`。两文件已删并进 `.gitignore` → 重新出现属正常，别提交。
- 浏览器门禁 = `pnpm --filter @henukit/portal test:e2e:*`（Playwright，不需 Docker）；环境变量陷阱见下方「本机环境」。

## CI 现状（实测，代价以天计）

- `.github/workflows/` 共 14 个 workflow：14 个配 `pull_request:`，13 个配 `push: branches: [main]`，只有 2 个配 `workflow_dispatch`（`deploy-henukit.yml`、`quizcraft-go.yml`）。
- 本 fork 的 PR 事件不产生 run：PR #2 的 opened / reopened / synchronize 三次都是 0 run（`gh api …/actions/runs` 的 `total_count` 不增，`gh pr checks 2` 只报 `no checks reported`），而同一时段 `gh workflow run` 立刻出 run。→ 别等 PR 红绿；要真 CI 用 `gh workflow run <workflow> --ref <branch>`。
- `pull-request-governance.yml` 两个 job：`branch-name` 要求 head 分支名匹配 `^(feature|fix)/[a-z0-9][a-z0-9-]*/hc-[0-9]+$`，而本仓 issues 关闭（`hasIssuesEnabled: false`）→ 无 `hc-<n>` 可引用，`codex/learning-feedback` 必红；`review-evidence` 要求 PR 正文逐字含 `Review-Head: <当前 head SHA>`、`Standards-Review: 0 findings`、`Spec-Review: 0 findings`。

## 经验教训

### 取证

- 计数、全称断言前先回查原物并逐个打开候选——先写结论后看证据是本分支返工主因。
- 别从被截断的输出里取数：`| tail -5` 曾把 deploy-webhook 的 6 个 `ok` 包写成 5 个。
- 别在管道里取退出码：`staticcheck ./... | tail -4` 的 `$?` 是 `tail` 的；门禁直接跑、直接看 `$?`。
- 先枚举清单再报数量：`*/cmd/*contractgen*` 命中 15 个目录，其中 1 个由 `products/quizcraft/go-service/scripts/generate-contract.sh` 驱动（直接 `go run` 的是 14 个）。
- 按用例分块统计，别数字符串出现次数：一次失败会重复打印同句 → 17 条红里 16 条同因，按块是 seal 14 / prepare 1 / activate 1，按字符串是 19/1/1。
- 条件句别写成 CI 事实：CI 有 Docker，三批都跑 → 写「本机因 X 在第一处中止；CI 里会…」。
- 引日志条目先 `git log -S'### 86 —' -- HANDOFF.md` 定位引入它的提交，否则会错引条目号。

### 本机环境（本机 ≠ CI）

- `HOME` **不能**重定向给需要 Next dev 的浏览器门禁（`Could not find the Next.js package` → `PageNotFoundError` / `.next` 的 `ENOENT` → Playwright `Timed out waiting 120000ms from config.webServer`）；Go 侧相反，必须显式给 `GOCACHE`/`GOMODCACHE`/`GOPATH`，否则 `failed to initialize build cache`。两套重定向互斥 → 配方见 `docs/DEVELOPMENT.md` §14。
- npm 要同时给 `npm_config_cache` 与 `npm_config_logs_dir`，否则以 `npm error Log files were not written…` 中止；语境与反例见 `docs/DEVELOPMENT.md` §14。
- 本机 Node 26 默认开 experimental webstorage → `apps/console` 的 `src/lib/pending-operations.spec.ts` 挂 1 条；加 `NODE_OPTIONS=--no-experimental-webstorage` 即 19/19。CI 固定 node 22。
- 无 Docker 时的红必须点名文件 + 错误再归因：`spawnSync docker ENOENT`、`panic: rootless Docker not found`（testcontainers）、`JSON.parse` 空 compose 输出、materials 的 `fixed Node runtime is unavailable`、缺 `shellcheck`/systemd。「环境问题」不算归因。
- 需要 Docker 的步骤先找该工具的独立二进制：`sqlc` 官方 release 是纯 Go 静态二进制，可直接复现 `docker run sqlc/sqlc:1.31.0 generate`（`go install …@v1.31.0` 因 go.mod 带 replace 指令不可行）。真 CI 第一次（run `37657421506`，main）就红在只有 Docker 那半跑得到的 `sqlc generate` 缺 75 行。
- 子代理环境可能与本机不同（它要 `chromium_headless_shell-1228`，本机缓存 1243）：依赖本机缓存/二进制的门禁（浏览器 e2e）它只能 `--list` 数用例、验不了通过 → 别把它的「跑不了」当证据；它跑得了的门禁照样给出精确数。

### 流程

- 先算触发面再决定验什么：把每个 workflow 的 `paths:`（`pull_request` 与 `push` 两处）对本次改动做 glob，列出真正会跑的作业。从日志条目出发会漏整条作业——`services/console-gateway` 的 account-portfolio 生成物陈旧就是这样才逮到的。
- 改了契约/生成物就重跑所有消费方生成器并 `git diff --exit-code`；漏一个 `cmd/*contractgen*`，CI 的 `git diff --exit-code` 就红（只改文件头 SHA 也算）。
- 一个回合只做一个大操作，push 后再写日志；日志条目要能被仓库证据复核（数字、条目号、文件路径）。
- 三轴评审的返工几乎都出在计数与口径，不出在代码：写完自查「计数 / 全称 / 条件句 / 引用出处」四项。
- 分清「文档还能再打磨」与「目标是否达成」：本分支真 CI 已全绿（`gh workflow run quizcraft-go.yml --ref codex/learning-feedback`，run `37659720284`，13 步全过），剩下的 `branch-name` 必红与学习报告切流窗口都要人工决定 → 继续润色文档不是进展。
