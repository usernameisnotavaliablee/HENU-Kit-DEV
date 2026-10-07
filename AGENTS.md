# Repository Guidelines

## 项目结构与模块边界

- `apps/portal` 是 Next.js 公共站，`apps/console` 是 Vue 管理端；`apps/web`、`apps/study-legacy-admin` 属于旧 Study 链路。
- `services/` 存放独立 Go 服务；`products/quizcraft/` 包含刷题前端、Python 后端和 Go 服务。QuizCraft 是唯一刷题产品。
- `packages/api-contracts`、`packages/design-tokens` 分别维护共享契约与设计变量；`infra/`、`scripts/` 管理部署和工具。
- 前端源码在各应用 `src/`，测试分布于源码旁及 `tests/`；静态资源见 `apps/portal/public/`、`products/quizcraft/assets/`。
- 先读 `CONTEXT-MAP.md` 与 `docs/README.md`；`legacy/`、`archive/` 仅供历史参考，不新增业务代码。

## 构建、测试与本地开发

使用根 `package.json` 指定的 pnpm 11.9.0；完整环境需要 Docker Compose。以下命令从仓库根执行：

| 命令 | 用途 |
| --- | --- |
| `pnpm install --frozen-lockfile` | 按锁文件安装依赖 |
| `cp .env.henukit.example .env.henukit` | 首次创建本地配置；勿覆盖已有配置 |
| `pnpm run dev:henukit` | 启动推荐完整栈，入口端口 8088 |
| `pnpm --filter @henukit/portal dev` | 单独启动 Portal 开发服务 |
| `pnpm run lint` | 运行默认前端静态检查 |
| `pnpm run build` | 构建 Portal、Console、QuizCraft 前端 |
| `pnpm test` | 运行根脚本定义的默认测试组合 |
| `pnpm --filter @henukit/portal test` | 补跑 Portal API/逻辑测试 |
| `go -C services/library test ./...` | 测试指定 Go 模块；按改动替换目录 |

`pnpm dev` 是兼容 Study 栈，不是推荐完整栈。

## 编码风格与命名

沿用相邻文件：TypeScript/Vue 通常使用两空格缩进、双引号、分号；组件采用 PascalCase，函数与变量采用 camelCase。Portal 文件多用 kebab-case，Vue 组件遵循已有文件名。Go 使用 `gofmt`，文件名采用 snake_case。Portal/QuizCraft 使用 ESLint；Console 的 lint 是 `vue-tsc --noEmit`，不要假定存在统一格式化命令。

## 测试要求

使用 Vitest、Node test runner、Playwright、Go testing，以及 QuizCraft 的 Pytest。沿用 `*.test.ts`、`*.test.mjs`、`*.spec.ts`、`*_test.go`、`test_*.py` 命名。根 `pnpm test` 不涵盖 Portal 全套、Go 或 Python 测试，必须补跑受影响模块检查。

Bug 修复补回归；覆盖失败、权限、并发与重试路径。共享契约修改验证消费方。具体验收遵循 `docs/development/testing-acceptance-spec.md` 与模块 CI，不擅设统一覆盖率数字；集成测试按模块要求准备 PostgreSQL/Redis。

## 提交与 Pull Request

提交遵循历史中的 `type(scope): description`，如 `fix(portal): restore account navigation`；常用类型有 feat、fix、test、docs、refactor、chore。禁止直接 push main，一个 PR 只解决一个问题。

关联 Issue，按 `.github/pull_request_template.md` 填写背景、范围、非目标、实际验证结果、风险、发布及回滚。前端改动附桌面和移动端截图；安全关键改动附当前 SHA 的 Standards/Spec 审查及失败路径证据。

## 安全与代理协作

不提交密钥、Token、Cookie、真实学生数据或 `资料库/` 内容；不跨服务直连数据库。数据库迁移遵循 expand → migrate → contract。

保留工程入口约定：先咨询 `~/.agents/skills/ask-matt/SKILL.md`，缺失时明确报告。已有代码从 `/grill-with-docs` 开始；多会话经 `/to-spec`、`/to-tickets` 后按依赖逐票 `/implement`。实现遵循 TDD，提交前执行 Standards、Spec、Public-ready Copy 三轴审查；无可见文案改动注明 `Public-ready Copy: not applicable`。Issue、标签、领域规则见 `docs/agents/`；遵守更具体的目录级 `AGENTS.md`。

## 经验教训（2026-10 学习报告分支踩过的弯路，别再走）

### 取值与取证
- **别从被截断的输出里取数字**：`| tail -5` 切掉了行，曾把 deploy-webhook 的 6 个 `ok` 包写成 5 个。要计数就重跑一遍不打管道，或数原始日志文件。
- **别在管道里取退出码**：`staticcheck ./... | tail -4` 的退出码是 `tail` 的，门禁判定必须直接跑、直接看 `$?`。
- **先枚举清单再报数量**：`*/cmd/*contractgen*` 命中 15 个目录，其中 1 个由 `products/quizcraft/go-service/scripts/generate-contract.sh` 驱动（直接 `go run` 的是 14 个）；「全仓 14 个」这类话先数一遍。
- **按用例分块统计，别数字符串出现次数**：一次失败会重复打印同一句——17 条红里 16 条同因，按块是 seal 14 / prepare 1 / activate 1，按字符串却是 19/1/1。
- **条件句不要写成 CI 事实**：「本机第一批红了，CI 里后两批也不跑」是错的（CI 有 Docker，三批都跑）。要写「本机因 X 在第一处中止；CI 里会…」。
- **引用日志条目先定位引入它的提交**：`git log -S'### NN —'`；否则会把第 53/71 条的表格记成第 81 条的。
- **写完「已做 / 已过 / 都是环境 / 只有 / 全部」立刻回查原物**：本分支绝大多数返工都来自「先写结论、后看证据」。全称断言前逐个打开候选。

### 本机环境（本机 ≠ CI）
- `HOME` **不能**重定向给需要 Next dev 的浏览器门禁（症状链：`Could not find the Next.js package` → `PageNotFoundError` / `.next` 的 `ENOENT` → Playwright `Timed out waiting 120000ms from config.webServer`）；Go 侧相反，必须显式给 `GOCACHE`/`GOMODCACHE`/`GOPATH`（否则 `failed to initialize build cache`）。两类命令的环境变量互相冲突，没有一套通吃。配方见 `docs/DEVELOPMENT.md` §14。
- npm 要同时给 `npm_config_cache` **和** `npm_config_logs_dir`，否则脚本以无关错误中止（`npm error Log files were not written…`）。
- 本机 Node v26 默认开 experimental webstorage，会让 Console 的 `pending-operations.spec.ts` 挂 1 条；加 `NODE_OPTIONS=--no-experimental-webstorage` 即 19/19。CI 固定 node 22（该版本默认不启用）。
- 无 Docker 时的红必须**点名文件+错误**再归因：`spawnSync docker ENOENT`、`panic: rootless Docker not found`（testcontainers）、空 compose 输出的 `JSON.parse`、materials 的 `fixed Node runtime is unavailable`、缺 `shellcheck`/systemd。只写「环境问题」不算归因。
- 子代理环境可能与本机不同（如它要 `chromium_headless_shell-1228` 而本机缓存是 1243）：它能 `--list` 数用例但验不了通过，别把它的「跑不了」当证据。

### 流程
- **先算触发面，再决定验什么**：把每个 workflow 的 `paths:`（`pull_request` 与 `push` 两处）对本次改动做 glob，列出真正会跑的作业。从日志条目出发会漏整条作业——本分支第 4 处必红缺陷（`services/console-gateway` 的 account-portfolio 生成物陈旧）就是这样才被逮到。
- **改了契约/生成物就重跑所有消费方生成器并 `git diff --exit-code`**：改 `packages/api-contracts/**` 后漏跑一个 `cmd/*contractgen*`，CI 的 `git diff --exit-code` 就红（只改文件头 SHA 也算）。
- **一个回合只做一个大操作**，commit & push 后再写日志；日志条目要能被仓库证据复核（数字、条目号、文件路径）。
- **三轴评审的返工几乎都出在计数与口径，不出在代码**：写完先自查「计数 / 全称 / 条件句 / 引用出处」四项，能省 2–3 轮。
- **分清「文档还能再打磨」与「目标是否达成」**：真实 CI 从未跑过、`branch-name` 门禁必然失败、`#166` 切流均需人工决定；在这些之前继续润色日志不是进展。
