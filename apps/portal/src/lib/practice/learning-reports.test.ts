import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const bankID = "10ca9b18-c303-4b7a-ab14-1241e41b665a";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("learning report state helpers", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.stubEnv("NEXT_PUBLIC_PORTAL_REQUIRE_GATEWAY", "1");
    vi.stubEnv("NODE_ENV", "test");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("reads a missing report as 'none yet' instead of a failure to load", async () => {
    const fetch = vi.fn().mockResolvedValue(
      jsonResponse({ error: "learning report not found", message: "暂时没有可查看的学习报告" }, 404)
    );
    vi.stubGlobal("fetch", fetch);
    const { readLatestLearningReport } = await import("./learning-reports");

    await expect(readLatestLearningReport(bankID)).resolves.toBeNull();
  });

  it("keeps a dependency failure an error, so a dark surface is never shown as empty", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          { error: "practice learning reports are not enabled", message: "学习报告暂时不可用，请稍后再试" },
          503
        )
      )
    );
    const { readLatestLearningReport, isMissingLearningReport } = await import(
      "./learning-reports"
    );

    const error = await readLatestLearningReport(bankID).catch((caught: unknown) => caught);
    expect(isMissingLearningReport(error)).toBe(false);
    expect(error).toMatchObject({ status: 503 });
  });

  it("stops following a task once its status is settled", async () => {
    const { learningTaskIsSettled } = await import("./learning-reports");
    const task = (status: string) => ({ status }) as never;

    for (const status of ["ready", "failed", "cancelled", "paused"]) {
      expect(learningTaskIsSettled(task(status))).toBe(true);
    }
    for (const status of ["queued", "running"]) {
      expect(learningTaskIsSettled(task(status))).toBe(false);
    }
  });

  it("explains each server status without claiming more than the evidence shows", async () => {
    const { learningReportStatusCopy } = await import("./learning-reports");

    expect(learningReportStatusCopy("ready").title).toBe("报告已生成");
    expect(learningReportStatusCopy("insufficient_evidence").title).toBe("证据还不够");
    expect(learningReportStatusCopy("insufficient_evidence").detail).toContain("不做结论");
    expect(learningReportStatusCopy("stale").detail).toContain("重新生成");
  });

  it("offers a start button only for the recommendations the server can start", async () => {
    const { learningNextStepLabel } = await import("./learning-reports");

    expect(learningNextStepLabel("practice")).toBe("开始练习");
    expect(learningNextStepLabel("diagnostic")).toBe("做诊断题");
    expect(learningNextStepLabel("content_unavailable")).toBeNull();
    expect(learningNextStepLabel("no_action")).toBeNull();
  });

  it("requires both the member's opt-in and the external-analysis consent", async () => {
    const { learningPreferencesAllowGeneration } = await import("./learning-reports");
    const preferences = (enabled: boolean, consent: boolean) =>
      ({ enabled, external_analysis_consent: consent }) as never;

    expect(learningPreferencesAllowGeneration(preferences(true, true))).toBe(true);
    expect(learningPreferencesAllowGeneration(preferences(true, false))).toBe(false);
    expect(learningPreferencesAllowGeneration(preferences(false, true))).toBe(false);
    expect(learningPreferencesAllowGeneration(preferences(false, false))).toBe(false);
  });
});
