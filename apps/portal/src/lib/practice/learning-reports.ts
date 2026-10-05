"use client";

import {
  useCallback,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import {
  clearLearningReports,
  createLearningReportSession,
  fetchLatestLearningReport,
  fetchLearningReportPreferences,
  formatPortalError,
  PortalHttpError,
  portalErrorRequestId,
  requestLearningReport,
  updateLearningReportPreferences,
} from "@/lib/api/client";
import { useFetchState, type FetchState } from "@/lib/api/use-fetch-state";
import { writePracticeSessionHandoff } from "@/lib/practice/session-handoff";
import { useIdempotencyKey } from "@/lib/practice/use-idempotency-key";
import type {
  LearningReport,
  LearningReportAction,
  LearningReportPreferences,
  LearningReportPreferencesUpdate,
  LearningReportTask,
} from "@/lib/api/types";

/** How often a queued report request is followed, and when to stop watching. */
export const LEARNING_REPORT_POLL_INTERVAL_MS = 3000;
export const LEARNING_REPORT_POLL_LIMIT = 20;

/**
 * A 404 from the report read is the honest "this member has no report yet"
 * state, not a load failure: the surface shows an empty block with a way
 * forward instead of an error, and never invents sample content.
 */
export function isMissingLearningReport(error: unknown): boolean {
  return error instanceof PortalHttpError && error.status === 404;
}

async function nullWhenMissing<T>(
  promise: Promise<{ data: T }>
): Promise<{ data: T | null }> {
  try {
    return await promise;
  } catch (error) {
    if (isMissingLearningReport(error)) return { data: null };
    throw error;
  }
}

/** A task is settled when following it can stop: no more progress is coming. */
export function learningTaskIsSettled(task: LearningReportTask): boolean {
  return (
    task.status === "ready" ||
    task.status === "failed" ||
    task.status === "cancelled" ||
    task.status === "paused"
  );
}

/**
 * Server status copy. The numbers and observations in a report are always the
 * server's; these lines only explain a status the server already decided, so
 * they never claim a weakness the evidence does not support.
 */
export function learningReportStatusCopy(
  status: LearningReport["status"]
): { title: string; detail: string } {
  switch (status) {
    case "ready":
      return { title: "报告已生成", detail: "以下判断都只来自你在这门课里的作答记录。" };
    case "insufficient_evidence":
      return {
        title: "证据还不够",
        detail: "这门课的作答还不足以支撑判断，报告只给出下一步建议，不做结论。",
      };
    case "stale":
      return {
        title: "报告已过期",
        detail: "你关注的课程内容有更新，重新生成后再看结论。",
      };
  }
}

/** Only a verified recommendation becomes a start button; the rest are advice. */
export function learningNextStepLabel(
  kind: LearningReportAction["kind"]
): string | null {
  switch (kind) {
    case "practice":
      return "开始练习";
    case "diagnostic":
      return "做诊断题";
    default:
      return null;
  }
}

/**
 * Generation needs both the member's opt-in and the external-analysis consent.
 * Core re-checks entitlement and consent on every write, so this only decides
 * whether the page offers the button at all.
 */
export function learningPreferencesAllowGeneration(
  preferences: LearningReportPreferences
): boolean {
  return preferences.enabled && preferences.external_analysis_consent;
}

export function useLearningReportPreferences(bankID: string | null): {
  state: FetchState<LearningReportPreferences>;
  setState: Dispatch<SetStateAction<FetchState<LearningReportPreferences>>>;
  retry: () => void;
} {
  return useFetchState<LearningReportPreferences>(
    () => (bankID ? fetchLearningReportPreferences(bankID) : undefined),
    [bankID]
  );
}

/** Reads the newest report, answering null when this member has none yet. */
export async function readLatestLearningReport(
  bankID: string
): Promise<LearningReport | null> {
  return (await nullWhenMissing(fetchLatestLearningReport(bankID))).data;
}

export function useLatestLearningReport(bankID: string | null): {
  state: FetchState<LearningReport | null>;
  setState: Dispatch<SetStateAction<FetchState<LearningReport | null>>>;
  retry: () => void;
} {
  return useFetchState<LearningReport | null>(
    () =>
      bankID
        ? readLatestLearningReport(bankID).then((data) => ({ data }))
        : undefined,
    [bankID]
  );
}

export type LearningReportCommand =
  | { status: "idle" }
  | { status: "working"; action: "save" | "request" | "clear" | "practice" }
  | { status: "error"; message: string; requestId: string | null };

/**
 * Learning-report writes. Every action keeps one idempotency key per logical
 * request until it succeeds, so a retry after a network failure replays the same
 * Core command instead of queuing a second report.
 */
export function useLearningReportCommands(bankID: string | null) {
  const keys = useIdempotencyKey("learning-report");
  const [command, setCommand] = useState<LearningReportCommand>({ status: "idle" });

  const settle = useCallback((error: unknown): void => {
    setCommand({
      status: "error",
      message: formatPortalError(error),
      requestId: portalErrorRequestId(error),
    });
  }, []);

  const savePreferences = useCallback(
    async (
      input: LearningReportPreferencesUpdate
    ): Promise<LearningReportPreferences | null> => {
      if (!bankID) return null;
      setCommand({ status: "working", action: "save" });
      try {
        const response = await updateLearningReportPreferences(
          bankID,
          input,
          keys.obtain("preferences")
        );
        keys.clear("preferences");
        setCommand({ status: "idle" });
        return response.data;
      } catch (error) {
        settle(error);
        return null;
      }
    },
    [bankID, keys, settle]
  );

  const requestReport = useCallback(async (): Promise<LearningReportTask | null> => {
    if (!bankID) return null;
    setCommand({ status: "working", action: "request" });
    try {
      const response = await requestLearningReport(bankID, keys.obtain("request"));
      keys.clear("request");
      setCommand({ status: "idle" });
      return response.data;
    } catch (error) {
      settle(error);
      return null;
    }
  }, [bankID, keys, settle]);

  const clearReports = useCallback(async (): Promise<boolean> => {
    if (!bankID) return false;
    setCommand({ status: "working", action: "clear" });
    try {
      const response = await clearLearningReports(bankID, keys.obtain("clear"));
      keys.clear("clear");
      // Clearing withdraws consent too, so any half-typed request key belongs to
      // a request Core will refuse now; drop it instead of replaying it later.
      keys.clear("request");
      setCommand({ status: "idle" });
      return response.data.cleared;
    } catch (error) {
      settle(error);
      return false;
    }
  }, [bankID, keys, settle]);

  const startPractice = useCallback(
    async (reportID: string): Promise<string | null> => {
      if (!bankID) return null;
      setCommand({ status: "working", action: "practice" });
      try {
        const response = await createLearningReportSession(
          bankID,
          reportID,
          keys.obtain("session")
        );
        keys.clear("session");
        try {
          // Same handoff as the favorites flow: no endpoint re-reads a session
          // by id, so the created session travels through sessionStorage.
          writePracticeSessionHandoff(response.data);
        } catch {
          setCommand({
            status: "error",
            message: "无法在本浏览器保存练习会话，请检查隐私设置后重试。",
            requestId: null,
          });
          return null;
        }
        setCommand({ status: "idle" });
        return response.data.session_id;
      } catch (error) {
        settle(error);
        return null;
      }
    },
    [bankID, keys, settle]
  );

  return {
    command,
    savePreferences,
    requestReport,
    clearReports,
    startPractice,
  };
}
