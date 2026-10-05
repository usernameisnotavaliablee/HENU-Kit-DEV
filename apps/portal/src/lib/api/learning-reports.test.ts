import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const bankID = "10ca9b18-c303-4b7a-ab14-1241e41b665a";
const taskID = "c1d2e3f4-a5b6-4c7d-8e9f-0a1b2c3d4e5f";
const reportID = "9f8e7d6c-5b4a-4938-8271-6a5b4c3d2e1f";
const idempotencyKey = "learning-report-idempotency-key";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const preferencesEnvelope = {
  request_id: "req_preferences",
  data: {
    enabled: true,
    interval_days: 7,
    goal: "follow_course",
    chapter_ids: ["ch01"],
    external_analysis_consent: true,
    bank_id: bankID,
    revision: 3,
    next_due_at: "2026-10-09T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
  },
};

const taskEnvelope = {
  request_id: "req_task",
  data: { task_id: taskID, bank_id: bankID, status: "queued", created_at: "2026-10-02T00:00:00Z" },
};

describe("QuizCraft learning-report client", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.stubEnv("NEXT_PUBLIC_PORTAL_REQUIRE_GATEWAY", "1");
    vi.stubEnv("NODE_ENV", "test");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("reads preferences, the latest report and one task from the bank paths", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(preferencesEnvelope))
      .mockResolvedValueOnce(
        jsonResponse({
          request_id: "req_report",
          data: {
            report_id: reportID,
            bank_id: bankID,
            content_version_id: "11111111-1111-4111-8111-111111111111",
            status: "insufficient_evidence",
            goal: "follow_course",
            evidence_until: "2026-10-01T00:00:00Z",
            created_at: "2026-10-02T00:00:00Z",
            statistics: [],
            evidence: [],
            findings: [],
            next_step: { kind: "no_action", reason: "当前作答证据不足，暂不判断薄弱点。" },
          },
        })
      )
      .mockResolvedValueOnce(jsonResponse(taskEnvelope));
    vi.stubGlobal("fetch", fetch);

    const {
      fetchLearningReportPreferences,
      fetchLatestLearningReport,
      fetchLearningReportTask,
    } = await import("./client");

    expect((await fetchLearningReportPreferences(bankID)).data.revision).toBe(3);
    expect((await fetchLatestLearningReport(bankID)).data.next_step.kind).toBe("no_action");
    expect((await fetchLearningReportTask(bankID, taskID)).data.status).toBe("queued");

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      `/api/v1/practice/banks/${bankID}/learning-reports/preferences`,
      expect.objectContaining({ credentials: "include" })
    );
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      `/api/v1/practice/banks/${bankID}/learning-reports/latest`,
      expect.objectContaining({ credentials: "include" })
    );
    expect(fetch).toHaveBeenNthCalledWith(
      3,
      `/api/v1/practice/banks/${bankID}/learning-reports/tasks/${taskID}`,
      expect.objectContaining({ credentials: "include" })
    );
  });

  it("keeps a missing report a 404 instead of inventing one", async () => {
    const fetch = vi.fn().mockResolvedValue(
      jsonResponse({ error: "learning report not found", message: "暂时没有可查看的学习报告", request_id: "req_missing" }, 404)
    );
    vi.stubGlobal("fetch", fetch);

    const { fetchLatestLearningReport } = await import("./client");
    await expect(fetchLatestLearningReport(bankID)).rejects.toMatchObject({
      status: 404,
      errorCode: "learning report not found",
      serverMessage: "暂时没有可查看的学习报告",
    });
  });

  it("surfaces the dark gateway flag as an unavailable error, not as data", async () => {
    const fetch = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          error: "practice learning reports are not enabled",
          message: "学习报告暂时不可用，请稍后再试",
          request_id: "req_dark",
        },
        503
      )
    );
    vi.stubGlobal("fetch", fetch);

    const { formatPortalError, fetchLearningReportPreferences } = await import("./client");
    const error = await fetchLearningReportPreferences(bankID).catch((caught: unknown) => caught);
    expect(error).toMatchObject({ status: 503, errorCode: "practice learning reports are not enabled" });
    expect(formatPortalError(error)).toBe("学习报告暂时不可用，请稍后再试");
  });

  it("writes preferences as one idempotent PUT with the member's consent body", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse(preferencesEnvelope));
    vi.stubGlobal("fetch", fetch);

    const { updateLearningReportPreferences } = await import("./client");
    const input = {
      enabled: true,
      interval_days: 7,
      goal: "follow_course" as const,
      chapter_ids: ["ch01"],
      external_analysis_consent: true,
    };
    const result = await updateLearningReportPreferences(bankID, input, idempotencyKey);

    expect(result.data.enabled).toBe(true);
    expect(fetch).toHaveBeenCalledWith(
      `/api/v1/practice/banks/${bankID}/learning-reports/preferences`,
      expect.objectContaining({
        method: "PUT",
        cache: "no-store",
        credentials: "same-origin",
        body: JSON.stringify(input),
        headers: expect.objectContaining({ "Idempotency-Key": idempotencyKey }),
      })
    );
  });

  it("requests, clears and pins a session on the documented command paths", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(taskEnvelope, 202))
      .mockResolvedValueOnce(jsonResponse({ request_id: "req_clear", data: { cleared: true, revision: 4 } }))
      .mockResolvedValueOnce(
        jsonResponse(
          {
            request_id: "req_session",
            data: {
              session_id: "22222222-2222-4222-8222-222222222222",
              bank_id: bankID,
              bank_version_id: "33333333-3333-4333-8333-333333333333",
              mode: "report",
              excluded_unavailable_count: 0,
              questions: [],
            },
          },
          201
        )
      );
    vi.stubGlobal("fetch", fetch);

    const { requestLearningReport, clearLearningReports, createLearningReportSession } =
      await import("./client");

    expect((await requestLearningReport(bankID, idempotencyKey)).data.status).toBe("queued");
    expect((await clearLearningReports(bankID, idempotencyKey)).data.cleared).toBe(true);
    expect((await createLearningReportSession(bankID, reportID, idempotencyKey)).data.mode).toBe("report");

    // The report route is the only one that carries a report id, and it is
    // POSTed with no body: Core re-selects every question itself.
    expect(fetch).toHaveBeenNthCalledWith(
      1,
      `/api/v1/practice/banks/${bankID}/learning-reports`,
      expect.objectContaining({ method: "POST", body: "{}" })
    );
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      `/api/v1/practice/banks/${bankID}/learning-reports`,
      expect.objectContaining({ method: "DELETE", body: "{}" })
    );
    expect(fetch).toHaveBeenNthCalledWith(
      3,
      `/api/v1/practice/banks/${bankID}/learning-reports/results/${reportID}/practice-sessions`,
      expect.objectContaining({ method: "POST" })
    );
  });

  it("refuses a short idempotency key or a missing bank before any request", async () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);

    const { clearLearningReports, createLearningReportSession } = await import("./client");
    await expect(clearLearningReports(bankID, "too-short")).rejects.toMatchObject({
      code: "PORTAL_INVALID_PRACTICE_IDEMPOTENCY_KEY",
    });
    await expect(createLearningReportSession("", reportID, idempotencyKey)).rejects.toMatchObject({
      code: "PORTAL_INVALID_PRACTICE_BANK",
    });
    await expect(createLearningReportSession(bankID, " ", idempotencyKey)).rejects.toMatchObject({
      code: "PORTAL_INVALID_LEARNING_REPORT",
    });
    expect(fetch).not.toHaveBeenCalled();
  });
});
