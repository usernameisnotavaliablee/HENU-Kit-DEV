import path from "node:path";

import { defineConfig, devices } from "@playwright/test";

// 桌面与移动端截图是本仓库对前端改动的验收证据：跑这个配置就默认产出到仓库根
// `.cache/screenshots/`（由配置文件位置推导，不依赖调用目录），无需额外导出
// 环境变量；需要换目录时用 PLAYWRIGHT_SCREENSHOT_DIR 覆盖。
process.env.PLAYWRIGHT_SCREENSHOT_DIR ??= path.resolve(__dirname, "../../.cache/screenshots");

// The learning-report surface is cutover-only: its browser flag defaults to 0,
// so it needs its own dev server instead of borrowing a locally reused default
// one that would render the dark state.
export default defineConfig({
  testDir: "./tests",
  testMatch: "learning-reports.spec.ts",
  timeout: 45_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: "http://127.0.0.1:3003",
    ...devices["Desktop Chrome"],
    trace: "retain-on-failure",
  },
  webServer: {
    command: "pnpm --filter @henukit/portal exec next dev -p 3003",
    url: "http://127.0.0.1:3003",
    env: {
      ...process.env,
      NEXT_PUBLIC_PORTAL_REQUIRE_GATEWAY: "1",
      NEXT_PUBLIC_PORTAL_ALLOW_MOCK: "0",
      NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_CATALOG: "1",
      NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_V2_READS: "1",
      NEXT_PUBLIC_PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS: "1",
    },
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
