# Practice 接线矩阵（ADR-0036 / #166 切流配置门禁）

> 状态：**ADR-0036 已接受的实施接线**。本文是 #166 切流窗口的配置门禁：
> 任何部署若要启用 Portal `/practice` 的 QuizCraft 读/写，必须按本表一次性对齐
> 服务端开关、浏览器烘焙开关与地址变量，两两配对，禁止部分启用。
> 对应决策：`docs/adr/0036-portal-practice-read-path-owner-go-core.md`；
> 研究依据：`docs/research/practice-read-path-ownership.md` §1.5 / §1.6 / §4.4。

## 1. 一句话接线图

```
浏览器 ── /api/v1/practice/catalog、/api/v1/rankings/*、/api/v1/practice/stats、
          /api/v1/practice/favorites*、/api/v1/practice/feedback/{id}/status（读）
       ── /api/v1/practice/sessions|answers|feedback、favorites 写（命令）
            │
            ▼
      Portal Gateway（精确路由，ADR-0036）
            │ 读写走 QuizCraft Core 地址（PRACTICE_SERVICE_URL 单一语义）
            ▼
      QuizCraft Go core（:10089，quizcraft 库唯一直读方；方案 2 已容器化为 compose 服务 `quizcraft`）
```

- Portal Gateway 的 `PORTAL_ENABLE_QUIZCRAFT_CATALOG` / `PORTAL_ENABLE_QUIZCRAFT_V2_READS` /
  `PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS` 只决定**读客户端是否存在**，不再决定路由是否注册；路由常态注册，客户端缺省时
  诚实 404（公共读）或 503（actor-bound 读）。
- portal-api 不再直读 quizcraft 库（`internal/practice/` 已删），也不再接收
  `/api/v1/practice/{banks,schools,lists,leaderboard,stats}` 的代理。
- Gateway 不持有任何产品数据库连接（ADR-0013 不变）。

## 2. 变量总表（#166 切流必须同设）

| 变量 | 默认（fail-closed） | 切流值（#166） | 语义 / 位置 |
|---|---|---|---|
| `PRACTICE_SERVICE_URL` | compose 默认 `http://portal-api:8085`（本地占位） | **QuizCraft Core 地址**（方案 2 容器化后：`http://quizcraft:10089`；旧 env 值 `http://host.docker.internal:10089` 仅过渡兼容） | **唯一语义 = QuizCraft Core 服务地址**：命令客户端 + catalog 读客户端共用。`mustEnv`（`portal-gateway/internal/config/config.go`）。生产 prebuilt compose 已强制（`docker-compose.henukit.prebuilt.yml`） |
| `QUIZCRAFT_CORE_URL` | 空 | Core 地址（可与 `PRACTICE_SERVICE_URL` 相同或不同） | V2 读客户端（rankings/stats/favorites/feedback）baseURL。与 `PRACTICE_SERVICE_URL` 的关系：两者职责分离，V2 读走 `QUIZCRAFT_CORE_URL` + `QUIZCRAFT_PORTAL_CATALOG_*` 凭据，命令/目录读走 `PRACTICE_SERVICE_URL` + `PRACTICE_*`/`PRACTICE_COMMAND_*` 凭据。**合并建议**：后续可把 `QUIZCRAFT_CORE_URL` 并入 `PRACTICE_SERVICE_URL`，本窗口保持并存但两两同设 |
| `PORTAL_ENABLE_QUIZCRAFT_CATALOG` | `0` | `1` | Gateway catalog 读客户端（`/api/v1/practice/catalog`）。**独立于 V2 读**：目录读凭据 `PRACTICE_*` 与 V2 读凭据 `QUIZCRAFT_PORTAL_CATALOG_*` 不同源，保持独立开关以免误并 |
| `PORTAL_ENABLE_QUIZCRAFT_V2_READS` | `0` | `1` | **V2 读总门禁**：rankings（`/api/v1/rankings/*`）、stats、favorites 读、feedback 状态读共用一个客户端，一开关全开/全关 |
| `PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS` | `0` | `1`（依赖 V2 读门禁） | **学习报告读门禁**：偏好 / latest / 任务进度三个读路由复用 V2 读客户端与凭据，但不随 V2 读自动开；置 `1` 时若 `PORTAL_ENABLE_QUIZCRAFT_V2_READS=0` 则启动报错（避免静默全 503） |
| `PORTAL_PRACTICE_COMMANDS_ENABLED` | `0` | `1` | **命令（写）门禁**：session/answer/feedback/favorites 写。与读门禁**必须独立**——命令凭据 `PRACTICE_COMMAND_*` 与读凭据强制不同，读并入命令门禁会把读写可用性错误耦合 |
| `NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_CATALOG` | `0` | `1`（构建时烘焙） | 浏览器目录页是否请求/渲染 V2 catalog（`apps/portal/src/lib/api/env.ts`） |
| `NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_V2_READS` | `0` | `1`（构建时烘焙） | 浏览器排行榜 tab / stats 请求（`personal-stats.ts`、`practice-nav.tsx`） |
| `NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS` | `0` | `1`（构建时烘焙，暂不随 #166 烘焙） | 浏览器学习报告入口与请求（`lib/practice/learning-reports.ts`、`practice-nav.tsx`、`/practice/reports`）。界面已落地，但仍**保持 0**：烘焙 1 会让入口出现，而生产尚无已审核内容可用；是否随切流烘焙属未决的发布决定 |
| `NEXT_PUBLIC_PORTAL_REQUIRE_GATEWAY` | `0`（dev）/ `1`（prod） | `1` | 强制真实 Gateway、禁 mock（生产必须 `1`） |

## 3. 默认值与烘焙的关系（消除歧义）

- **仓库默认全部为 0/空（fail-closed）**：compose（`docker-compose.henukit.yml`）、
  `.env.henukit.example`、网关 `config.go`、浏览器 `env.ts` 的默认一致。
- **`scripts/ops/henukit-release-images.sh` 把两个浏览器开关烘焙为 1**：该清单描述的
  是 **#166 切流发布构建**。烘焙 1 与网关默认 0 的表面不一致是**有意的**：
  用该脚本产出的发布镜像**必须**在同一个发布 bundle 里把三个服务端开关
  （`PORTAL_ENABLE_QUIZCRAFT_CATALOG`、`PORTAL_ENABLE_QUIZCRAFT_V2_READS`、
  `PORTAL_PRACTICE_COMMANDS_ENABLED`）与 `PRACTICE_SERVICE_URL`/`QUIZCRAFT_CORE_URL`
  同设，否则浏览器渲染读面而 Gateway 返回诚实 404/503（绝不 mock/legacy 兜底）。
- 本地/预发布 compose 构建参数默认 0，属正常关闭状态；**不允许**「浏览器开、
  网关关」之外的任何部分启用组合作为生产长期状态。

## 4. 凭据对（强制不同，不可共享）

| 凭据组 | 用途 | 变量 |
|---|---|---|
| catalog key ring（读） | catalog 读客户端（`PRACTICE_SERVICE_URL`） | `PRACTICE_CLIENT_ID/SECRET/KEY_ID` |
| V2 读 key ring（读） | rankings/stats/favorites/feedback 读客户端（`QUIZCRAFT_CORE_URL`） | `QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID/SECRET/KEY_ID` |
| command key ring（写） | session/answer/feedback/favorites 写客户端 | `PRACTICE_COMMAND_CLIENT_ID/SECRET/KEY_ID` |

读凭据（catalog / V2）与写凭据（command）**强制不同**（Go core `practice_http.go`
已强制校验）。Gateway 只持有服务凭据，不持有产品库连接。

## 5. 路由与数据源（切流后契约）

| 浏览器端点 | 数据源 | 未启用时（fail-closed） |
|---|---|---|
| `GET /api/v1/practice/catalog` | Core 目录契约（catalog 客户端） | 404 |
| `GET /api/v1/rankings/overall`、`/api/v1/banks/{bank_id}/rankings` | Core 排行契约（V2 客户端） | 404 |
| `GET /api/v1/practice/stats` | Core 个人统计（V2 客户端） | 503 |
| `GET /api/v1/practice/favorites`、`/banks/{bank_id}/favorites`、`/feedback/{feedback_id}/status` | Core actor-bound 读（V2 客户端） | 503 |
| `GET /api/v1/practice/banks/{bank_id}/learning-reports/preferences`、`/latest`、`/tasks/{task_id}` | Core actor-bound 读（V2 客户端，学习报告镜像类型） | 503（`PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS=0` 或缺客户端）；Core 无报告时透传 404；**撤权会员的 `/latest` 与 `/tasks/{id}` 是 403 `learning_entitlement_required`，不是 503**（Core 先校验实时会员权益再查报告，所以没有报告也先给 403；偏好读不受会员门禁，仍是 200） |
| `POST /api/v1/practice/sessions`、`.../answers`、`/feedback`、favorites 写 | Core 命令（命令客户端） | 503 |
| `PUT /api/v1/practice/banks/{bank_id}/learning-reports/preferences`、`POST /banks/{bank_id}/learning-reports`、`POST .../results/{report_id}/practice-sessions` | Core 命令（命令客户端；需 `Idempotency-Key`） | 503（`PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS=0` 或命令客户端缺失）；会员能自行处理的拒绝带 Core 的码原样转达：`learning_consent_outdated`（400）、`learning_entitlement_required`（403）、`practice_command_rate_limited`（429，手动生成超限） |
| `DELETE /api/v1/practice/banks/{bank_id}/learning-reports` | Core 命令（命令客户端；需 `Idempotency-Key`） | **唯一豁免暗态门的写路由**：`PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS=0` 时仍转发（回退后会员必须还能清除报告并撤回同意）。缺登录/命令客户端时仍是 401/503 |
| `GET /api/v1/practice/banks`、`/schools`、`/lists/{id}`、`/leaderboard` | **已下线**（ADR-0036，portal-api 直读删除） | 404 + 迁移提示 |

排行隐私契约：公开排行响应只含 `rank / nickname / system_avatar / correct_answer_count`，
无 `user_id`；昵称空 → 匿名学习者（Core 归一化）；网关侧 `DisallowUnknownFields`
拒绝任何含 `user_id` 的 Core 排行响应（Gateway 契约测试已覆盖）。

## 6. #166 切流检查单（配置门禁）

1. 服务器核验 quizcraft 库表结构与 `quizcraft_migration_events` 版本（`docs/operations/CURRENT_PRODUCTION_STATE.md`）。
2. `migrate`/`reconcile` 全量对账、`quizcraft_v2` 快照、`run_id` 固化（`products/quizcraft/go-service/README.md`）。
3. 本矩阵核对：上述变量两两对齐（服务端 × 浏览器 × 地址）。
4. 发布单个切流 bundle：构建脚本烘焙 1 的 Portal 镜像 + 服务端开关同设 1 + `PRACTICE_SERVICE_URL`/`QUIZCRAFT_CORE_URL` 指向 Core。
5. Browser gate（desktop + 390px）验证练习/收藏/反馈/排行通过后，才允许移除
   portal-api 残留直读（已删）并停服 FastAPI（ADR-0013-cutover）。
6. 读失败为诚实 503/404，禁止任何 mock/legacy 兜底（ADR-0036 强制）。

## 7. 学习反馈切流前置（内容审核与运行监测）

- **无已审核内容不得开放**：学习报告的读/写路由即使全部打开，只要课程没有 `status='approved'` 的当前内容版本，
  `BuildLearningEvidence` 会返回「不可用」，会员侧就是诚实的不可用状态（不 mock、不兜底）。内容只能经
  Workshop 审核接线进入：`GET/POST /api/v1/workshop/banks/{bank_id}/learning-content`、
  `POST .../learning-content/{content_version_id}/approve|retire`（读/写/发布三种权限，写操作需
  `Idempotency-Key`）。导入只产生草稿，审批才写入审核人，`activate` 与 `enable` 分开。
- **切流前必须做的内容动作**：导入候选内容包 → 人工审核（记录审核人）→ `activate` 指向该版本 → 确认
  会员侧读取可用后才 `enable` 该课程。退役仍生效的内容会被拒绝：必须先激活替代版本。
- **运行监测**：`QUIZCRAFT_V2_DATABASE_URL`（必须 `quizcraft_v2`）下运行
  `go run ./cmd/learninghealth -json -fail-on-alert`（`-queued-behind` 默认 30m，`-failure-budget` 默认 0）。
  告警项：过期租约、排队超阈值、24h 失败超预算、已同意会员但无启用课程。暗态功能不产生告警。
- **手动生成限流**：`QUIZCRAFT_LEARNING_MANUAL_LIMIT`（默认 `10`，`0` 关闭）限制同一会员对同一课程每小时的生成任务数，
  超限为 429 `rate_limited`（网关转 429 `practice_command_rate_limited`）。这是成本/滥用保护，不是配额；计划任务的请求永不被拒，但其任务计入窗口工作量。
- **关闭回退**：`PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS=0` → 浏览器开关烘焙 0 并重建 Portal →
  `QUIZCRAFT_LEARNING_WORKER_ENABLED=0` → `QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL=0`。已发布报告、
  偏好、任务与审核记录都保留；会员同意不被清除，重新开启仍需权益与同意校验。
  回退后**清除接口仍可用**（`DELETE .../learning-reports` 是唯一豁免暗态门的写路由，`internal/httpapi/learning_reports.go` 的 `clearLearningReports` 不经 `learningReportWrite`）：
  会员必须还能撤回同意并清除报告，Core 侧这条路由本来就不校验会员与学习内容审核（只要求该课程有已发布版本）。其余写路由（含 `PUT .../preferences` 的关闭）在暗态下仍是 503——
  网关不解析请求体，无法在不读 body 的前提下区分「关闭」与「开启」；要撤回同意就用清除。

## 8. 学习报告的会员侧拒绝：谁写的、会员看到什么、值班怎么办

先分清两类东西：**会员状态**（会员自己能处理，必须原样说明）与**依赖故障**（会员处理不了，才是 503「稍后再试」）。把这层说成那层，值班会去翻队列，而会员只是权益过期。

| 会员看到的状态码 | 谁写的 | 触发条件 | 会员侧表现 |
|---|---|---|---|
| 400 `learning_consent_outdated` | Core（`learning_reports_http.go:88`），网关转达 | 存库的分析授权代次落后于当前版本，且会员请求**开启** | 横幅：「分析授权已过期，请先关闭学习报告，再重新开启」（关闭再开启两步自愿、可自查） |
| 403 `learning_entitlement_required` | Core（`requireLearningLifetime`，`:296`），网关转达 | 实时会员权益校验不过 | 会员区块：「学习报告需要有效的会员权益，请确认会员状态后再试」+「去会员中心」入口。**只有这个码**会被读路径按名转达；Core 没给码或给了别的码时读路由落 `practice access denied`（`learningReportRead` 的兜底，`internal/httpapi/learning_reports.go`），不会替 Core 断言会员问题 |
| 429 `practice_command_rate_limited` | 网关（`writePracticeCommandFailure`，`internal/httpapi/handler.go`，Core 429 转达） | 手动生成超过 `QUIZCRAFT_LEARNING_MANUAL_LIMIT`（默认 10/小时/会员/课程） | 横幅：「操作太频繁了，请稍后再试」。**不是配额，也别当故障**。注意这句文案是**全站练习命令共用**的（会话、作答、收藏写等都会用到），不是学习报告专属，改它会波及所有刷题命令 |
| 503 `practice learning reports are temporarily unavailable` | 网关（`learningReportRead` 的依赖不可用分支，`internal/httpapi/learning_reports.go`） | 依赖不可用、凭据缺失、账号/权益服务报错 | 横幅：暂时不可用，稍后再试（唯一的「重试」语义） |
| 503 `practice learning reports are not enabled` | 网关（`learningReportRead` 读、`learningReportWrite` 写，`internal/httpapi/learning_reports.go`） | 暗态开关关闭或客户端未接线。**清除接口不在此列**（唯一豁免，见 §7） | 同一句「学习报告暂时不可用，请稍后再试」——**与上面共用文案，值班必须看码**：这个码代表「没开」，不是「挂了」 |
| 404 `learning report not found` | 网关（Core 404 映射） | 该课程确实还没有报告 | 空态 + 「生成报告」入口，不是错误 |

实时会员门禁的位置（读代码确认，不是推测）：`portalUpdateLearningReportPreferences` **只在 `input.Enabled` 为真时**校验（`:213`），
所以撤权后会员仍能关闭与清除——撤回同意是会员的数据权利，不能被权益挡住；`portalRequestLearningReport`（`:240`）、
`portalLatestLearningReport`（`:330`）、`portalLearningReportTask`（`:343`）、`portalCreateLearningReportPracticeSession`（`:382`）
每次都校验；偏好读**不**校验。

值班决策顺序（会员说「学习报告用不了」）：

1. 看浏览器实际状态码（信封里的 `error` 码，不要只看文案）。401 → 登录态过期，重新登录；**403/400 → 别查服务**：403 查会员权益状态（续费/权益服务），400 让会员按文案关闭再开启。
2. 429 → 看该会员该课程一小时的生成次数，属于滥用保护，等待窗口即可。
3. 503/404 → 才是依赖与内容问题：先跑下面的运行监测，再看内容审核状态（§7：无 `status='approved'` 当前版本时会员侧就是诚实的不可用）。
4. 撤权会员批量看到 403 属于**预期状态，不产生告警**（`learninghealth` 的告警项与会员权益无关），不要据此开工单。

改这两类拒绝时，同一提交必须动三处，否则会静默退化：Core 的码与语义、网关的转达分支
（写路径 `internal/httpapi/handler.go` 的 `writePracticeCommandFailure`、读路径
`internal/httpapi/learning_reports.go` 与 `internal/practice/learning_reports.go` 的 403 分类）、
Portal 的登记（`apps/portal/src/lib/api/gateway-errors.ts`，未登记会被 `gateway-errors.test.ts` 判红）
与按码分支（`apps/portal/src/app/practice/reports/page.tsx`）。

本机可复现的证据（不需要生产权限）：

以下命令都从仓库根执行（子 shell 里的 `cd` 不污染后续命令）：

```bash
# 1. 网关：码的转达与分类（含「依赖故障仍是 503」的反例）
(cd services/portal-gateway && go test -race -count=1 ./internal/practice ./internal/httpapi)

# 2. 真实 Core 联合链路：撤权会员 /latest 与 /tasks/{id} 都是 403，偏好读仍是 200
(cd services/portal-gateway && QUIZCRAFT_JOINT_ALLOW_DESTRUCTIVE_RECREATE=1 \
  QUIZCRAFT_JOINT_DATABASE_URL='postgres://<role>@127.0.0.1:5432/postgres?sslmode=disable' \
  go test ./internal/httpapi -run TestQuizCraftLearningReportMemberChainAcrossARealCore -count=1 -v)

# 3. Portal：拒绝码必须登记 + 页面按码分支（含桌面/移动端截图）
(cd apps/portal && npx vitest run src/lib/api/portal-error.test.ts)
(cd apps/portal && npx playwright test --config playwright.learning-reports.config.ts)
# 截图落在 .cache/screenshots/learning-reports-membership-{desktop,mobile}.png；
# 浏览器二进制不在默认缓存目录时，给第 3 步补 PLAYWRIGHT_BROWSERS_PATH=<浏览器目录>
```
