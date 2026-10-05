import { expect, test, type Page } from "@playwright/test";
import { SIGNED_IN_SESSION, mockSignedInGateway } from "./support/gateway";

/**
 * 学习报告界面（LF-06）。浏览器开关默认关闭，这一组用专用配置把
 * catalog / V2 读 / 学习报告三个开关都打开，并 mock 网关响应，
 * 断言「设置 → 生成 → 报告 → 按建议练习 → 清除」这条会员路径。
 */

const BANK_ID = "10ca9b18-c303-4b7a-ab14-1241e41b665a";
const BANK_VERSION_ID = "11111111-1111-4111-8111-111111111111";
const REPORT_ID = "9f8e7d6c-5b4a-4938-8271-6a5b4c3d2e1f";
const TASK_ID = "c1d2e3f4-a5b6-4c7d-8e9f-0a1b2c3d4e5f";
const SESSION_ID = "22222222-2222-4222-8222-222222222222";

const catalog = {
  request_id: "req_catalog",
  banks: [
    {
      bank_id: BANK_ID,
      bank_version_id: BANK_VERSION_ID,
      name: "高等数学",
      question_count: 42,
      available: true,
      chapters: [
        { id: "ch01", name: "第一章 极限" },
        { id: "ch02", name: "第二章 导数" },
      ],
    },
  ],
};

const preferences = {
  request_id: "req_preferences",
  data: {
    enabled: true,
    interval_days: 7,
    goal: "follow_course",
    chapter_ids: [],
    external_analysis_consent: true,
    bank_id: BANK_ID,
    revision: 3,
  },
};

const report = {
  request_id: "req_report",
  data: {
    report_id: REPORT_ID,
    bank_id: BANK_ID,
    content_version_id: BANK_VERSION_ID,
    status: "ready",
    goal: "follow_course",
    evidence_until: "2026-10-01T00:00:00Z",
    created_at: "2026-10-02T00:00:00Z",
    statistics: [
      {
        tag_id: "math.limit",
        tag_kind: "knowledge",
        label: "极限",
        attempt_count: 6,
        unique_question_count: 4,
        first_correct_count: 2,
        repeat_attempt_count: 2,
        repeat_correct_count: 1,
        latest_correct_count: 3,
      },
    ],
    evidence: [
      {
        evidence_id: "ev-1",
        question_id: "33333333-3333-4333-8333-333333333333",
        question_version_id: "44444444-4444-4444-8444-444444444444",
        submitted_at: "2026-10-01T00:00:00Z",
        correct: false,
        question: "求 lim(x→0) sin x / x",
        submitted_answer: "1/2",
        expected_answer: "1",
      },
    ],
    findings: [
      {
        tag_id: "math.limit",
        status: "supported",
        observation: "极限的独立作答里错题集中在等价无穷小替换。",
        possible_reason: "可能在替换条件不满足时直接套用公式。",
        evidence_ids: ["ev-1"],
      },
    ],
    next_step: {
      kind: "practice",
      reason: "先用同类型的极限题确认替换条件。",
      tag_id: "math.limit",
      lesson: {
        lesson_id: "lesson-1",
        title: "等价无穷小替换的前提",
        body: "只在乘除结构中使用等价替换，加减结构要先通分或提取公因子。",
        sources: [
          {
            source_id: "src-1",
            title: "高等数学（上）",
            version: "2024 版",
            locator: "第 1 章 1.4 节",
          },
        ],
      },
      question_ids: ["33333333-3333-4333-8333-333333333333"],
    },
  },
};

const reportSession = {
  request_id: "req_session",
  data: {
    session_id: SESSION_ID,
    bank_id: BANK_ID,
    bank_version_id: BANK_VERSION_ID,
    mode: "report",
    excluded_unavailable_count: 0,
    questions: [
      {
        question_id: "33333333-3333-4333-8333-333333333333",
        question_version_id: "44444444-4444-4444-8444-444444444444",
        type: "single",
        chapter_id: "ch01",
        chapter: "第一章 极限",
        content: "求 lim(x→0) sin x / x",
        options: ["0", "1", "不存在", "无穷"],
      },
    ],
  },
};

type Mocks = {
  report?: unknown | null;
  preferences?: unknown;
  taskStatus?: string;
  /** Records request bodies/headers so tests can assert the writes. */
};

async function mockLearningReportGateway(page: Page, mocks: Mocks = {}) {
  await mockSignedInGateway(page, SIGNED_IN_SESSION);
  await page.route("**/api/v1/practice/catalog", (route) =>
    route.fulfill({ json: catalog })
  );
  await page.route(
    `**/api/v1/practice/banks/${BANK_ID}/learning-reports/preferences`,
    async (route) => {
      if (route.request().method() === "PUT") {
        await route.fulfill({ json: preferences });
        return;
      }
      await route.fulfill({ json: mocks.preferences ?? preferences });
    }
  );
  await page.route(
    `**/api/v1/practice/banks/${BANK_ID}/learning-reports/latest`,
    async (route) => {
      const value = mocks.report === undefined ? report : mocks.report;
      if (value === null) {
        await route.fulfill({
          status: 404,
          json: { error: "learning report not found", message: "暂时没有可查看的学习报告" },
        });
        return;
      }
      await route.fulfill({ json: value });
    }
  );
  await page.route(
    `**/api/v1/practice/banks/${BANK_ID}/learning-reports/tasks/${TASK_ID}`,
    (route) =>
      route.fulfill({
        json: {
          request_id: "req_task",
          data: {
            task_id: TASK_ID,
            bank_id: BANK_ID,
            status: mocks.taskStatus ?? "ready",
            created_at: "2026-10-02T00:00:00Z",
            report_id: REPORT_ID,
          },
        },
      })
  );
  await page.route(
    `**/api/v1/practice/banks/${BANK_ID}/learning-reports`,
    async (route) => {
      if (route.request().method() === "POST") {
        await route.fulfill({
          status: 202,
          json: {
            request_id: "req_task",
            data: {
              task_id: TASK_ID,
              bank_id: BANK_ID,
              status: "queued",
              created_at: "2026-10-02T00:00:00Z",
            },
          },
        });
        return;
      }
      await route.fulfill({
        json: { request_id: "req_clear", data: { cleared: true, revision: 4 } },
      });
    }
  );
  await page.route(
    `**/api/v1/practice/banks/${BANK_ID}/learning-reports/results/${REPORT_ID}/practice-sessions`,
    (route) => route.fulfill({ status: 201, json: reportSession })
  );
}

test("会员可以查看设置与报告，并按建议开始练习", async ({ page }) => {
  await mockLearningReportGateway(page);
  await page.goto("/practice/reports");

  // 入口只在开关打开时出现，且当前页高亮。
  const nav = page.getByRole("navigation");
  await expect(nav.getByRole("link", { name: /学习报告/ })).toBeVisible();

  await expect(page.getByTestId("practice-reports-settings")).toBeVisible();
  await expect(page.getByTestId("practice-reports-report")).toBeVisible();
  await expect(page.getByText("极限的独立作答里错题集中在等价无穷小替换。")).toBeVisible();
  // 会员看到标签名，不是内部 tag id。
  await expect(page.getByTestId("practice-reports-report")).not.toContainText("math.limit");
  await expect(page.getByText("求 lim(x→0) sin x / x")).toBeVisible();
  await expect(page.getByText("等价无穷小替换的前提")).toBeVisible();
  await expect(page.getByTestId("practice-reports-report")).toContainText("这门课里的作答");

  const screenshotDir = process.env.PLAYWRIGHT_SCREENSHOT_DIR;
  if (screenshotDir) {
    await page.setViewportSize({ width: 1280, height: 1400 });
    await page.screenshot({
      path: `${screenshotDir}/learning-reports-desktop.png`,
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: `${screenshotDir}/learning-reports-mobile.png`,
      fullPage: true,
    });
    await page.setViewportSize({ width: 1280, height: 1400 });
  }

  await page.getByTestId("practice-reports-start").click();
  await expect(page).toHaveURL(
    new RegExp(`/practice/quiz\\?session_id=${SESSION_ID}&from=report`)
  );
});

test("保存设置会带着会员的选择写入", async ({ page }) => {
  await mockLearningReportGateway(page);
  let body = "";
  page.on("request", (request) => {
    if (request.method() === "PUT" && request.url().includes("learning-reports")) {
      body = request.postData() ?? "";
    }
  });
  await page.goto("/practice/reports");
  await expect(page.getByTestId("practice-reports-settings")).toBeVisible();

  await page.getByTestId("practice-reports-consent").uncheck();
  await page.getByTestId("practice-reports-save").click();
  await expect(page.getByTestId("practice-reports-saved")).toBeVisible();
  expect(JSON.parse(body)).toMatchObject({
    enabled: true,
    external_analysis_consent: false,
    interval_days: 7,
  });
});

test("还没有报告时给出去处，而不是空面板", async ({ page }) => {
  await mockLearningReportGateway(page, { report: null });
  await page.goto("/practice/reports");

  const empty = page.getByTestId("practice-reports-empty");
  await expect(empty).toBeVisible();
  await expect(empty.getByRole("link")).toBeVisible();
  await expect(page.getByTestId("practice-reports-report")).toHaveCount(0);
});

test("生成报告会排队并跟随任务进度", async ({ page }) => {
  await mockLearningReportGateway(page, { report: null, taskStatus: "ready" });
  await page.goto("/practice/reports");
  await expect(page.getByTestId("practice-reports-empty")).toBeVisible();

  const request = page.waitForRequest(
    (req) =>
      req.method() === "POST" &&
      req.url().endsWith(`/practice/banks/${BANK_ID}/learning-reports`)
  );
  await page.getByTestId("practice-reports-generate").click();
  const posted = await request;
  expect(posted.headers()["idempotency-key"]).toBeTruthy();
  await expect(page.getByTestId("practice-reports-task")).toBeVisible();
});

test("清除报告需要二次确认", async ({ page }) => {
  await mockLearningReportGateway(page);
  await page.goto("/practice/reports");
  await expect(page.getByTestId("practice-reports-report")).toBeVisible();

  const clear = page.waitForRequest(
    (req) =>
      req.method() === "DELETE" &&
      req.url().endsWith(`/practice/banks/${BANK_ID}/learning-reports`)
  );
  await page.getByTestId("practice-reports-clear").click();
  await expect(page.getByTestId("practice-reports-clear")).toHaveText("确认清除报告");
  await page.getByTestId("practice-reports-clear").click();
  await clear;
});
