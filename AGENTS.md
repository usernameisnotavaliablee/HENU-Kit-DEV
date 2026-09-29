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
