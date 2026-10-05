"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  fetchLearningReportTask,
  fetchQuizCraftCatalog,
  PortalNetworkError,
  portalErrorRequestId,
  redirectToLogin,
} from "@/lib/api/client";
import { quizCraftLearningReportsEnabled } from "@/lib/api/env";
import type { LearningReportTask, QuizCraftCatalogBank } from "@/lib/api/types";
import {
  LEARNING_REPORT_POLL_INTERVAL_MS,
  LEARNING_REPORT_POLL_LIMIT,
  learningPreferencesAllowGeneration,
  learningTaskIsSettled,
  useLatestLearningReport,
  useLearningReportCommands,
  useLearningReportPreferences,
} from "@/lib/practice/learning-reports";
import { EmptyBlock, ErrorBanner, LoadingBlock } from "@/components/data-state";
import { usePageEnter } from "@/components/practice/transition/use-page-enter";
import LearningReportSettings from "@/components/practice/learning-report-settings";
import LearningReportView from "@/components/practice/learning-report-view";

const reportsEnabled = quizCraftLearningReportsEnabled();

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function bankIDFromLocation(): string {
  const value = new URLSearchParams(window.location.search).get("bank_id")?.trim() ?? "";
  return UUID_PATTERN.test(value) ? value : "";
}

type CatalogState =
  | { status: "loading" }
  | { status: "ready"; banks: QuizCraftCatalogBank[] }
  | { status: "error"; message: string; requestId: string | null };

function Header() {
  return (
    <div data-block data-enter>
      <p className="font-mono text-xs tracking-[0.3em] text-ink/60">
        <span className="text-accent-text">REPORTS</span>
        <span className="mx-2">/</span>
        MY COURSE
      </p>
      <h1 className="mt-3 font-display text-5xl font-bold tracking-tight md:text-6xl">
        学习报告
      </h1>
      <p className="mt-4 max-w-2xl text-sm leading-7 text-ink/65">
        根据你在这门课里的作答，指出下一步练什么，并给出可核对的依据。只有你主动开启并同意后才会生成，
        随时可以关闭或清除。
      </p>
    </div>
  );
}

function ReportsSurface() {
  const router = useRouter();
  const [catalog, setCatalog] = useState<CatalogState>({ status: "loading" });
  const [bankID, setBankID] = useState<string | null>(null);
  const [task, setTask] = useState<LearningReportTask | null>(null);
  const [taskAttempts, setTaskAttempts] = useState(0);
  const [confirmingClear, setConfirmingClear] = useState(false);
  const [savedPreferences, setSavedPreferences] = useState(false);

  const preferences = useLearningReportPreferences(bankID);
  const latest = useLatestLearningReport(bankID);
  const { command, savePreferences, requestReport, clearReports, startPractice } =
    useLearningReportCommands(bankID);

  const loadCatalog = useCallback(async () => {
    setCatalog({ status: "loading" });
    try {
      const response = await fetchQuizCraftCatalog();
      const banks = response.banks;
      setCatalog({ status: "ready", banks });
      const requested = bankIDFromLocation();
      const chosen =
        banks.find((bank) => bank.bank_id === requested) ??
        banks.find((bank) => bank.available) ??
        banks[0];
      setBankID(chosen ? chosen.bank_id : null);
    } catch (error) {
      // The catalog is the only browser-visible list of banks. A failed read
      // stays a failure: never fall back to a remembered or invented bank.
      setCatalog({
        status: "error",
        message:
          error instanceof PortalNetworkError
            ? "题库暂时加载不出来，请检查网络后重试。"
            : "题库暂时加载不出来，请稍后重试。",
        requestId: portalErrorRequestId(error),
      });
    }
  }, []);

  useEffect(() => {
    // Deferred to a macrotask so the first paint happens before the request
    // starts, same as the catalog page.
    const timer = window.setTimeout(() => void loadCatalog(), 0);
    return () => window.clearTimeout(timer);
  }, [loadCatalog]);

  const selectedBank =
    catalog.status === "ready"
      ? catalog.banks.find((bank) => bank.bank_id === bankID)
      : undefined;

  // Follow a queued request only while it can still make progress, and stop at
  // the limit so a slow generator never turns into an endless spinner.
  useEffect(() => {
    if (!task || !bankID || learningTaskIsSettled(task)) return;
    if (taskAttempts >= LEARNING_REPORT_POLL_LIMIT) return;
    const timer = window.setTimeout(() => {
      setTaskAttempts((current) => current + 1);
      void fetchLearningReportTask(bankID, task.task_id)
        .then((response) => setTask(response.data))
        .catch(() => setTaskAttempts(LEARNING_REPORT_POLL_LIMIT));
    }, LEARNING_REPORT_POLL_INTERVAL_MS);
    return () => window.clearTimeout(timer);
  }, [task, bankID, taskAttempts]);

  const retryLatest = latest.retry;
  useEffect(() => {
    if (task?.status === "ready") retryLatest();
  }, [task?.status, retryLatest]);

  const setPreferencesState = preferences.setState;
  const onSave = useCallback(
    async (input: Parameters<typeof savePreferences>[0]) => {
      setSavedPreferences(false);
      const saved = await savePreferences(input);
      if (!saved) return;
      // Apply the saved revision in place: a re-read would blank the form and
      // drop the confirmation the member just earned.
      setPreferencesState({ status: "ready", data: saved });
      setSavedPreferences(true);
      latest.retry();
    },
    [savePreferences, setPreferencesState, latest]
  );

  const onGenerate = useCallback(async () => {
    setTaskAttempts(0);
    const created = await requestReport();
    if (created) setTask(created);
  }, [requestReport]);

  const onClear = useCallback(async () => {
    const cleared = await clearReports();
    setConfirmingClear(false);
    setSavedPreferences(false);
    setTask(null);
    if (cleared) {
      preferences.retry();
      latest.retry();
    }
  }, [clearReports, preferences, latest]);

  const onStartPractice = useCallback(
    async (reportID: string) => {
      const sessionID = await startPractice(reportID);
      if (sessionID) {
        router.push(
          `/practice/quiz?session_id=${encodeURIComponent(sessionID)}&from=report`
        );
      }
    },
    [startPractice, router]
  );

  const readState = latest.state;
  const report = readState.status === "ready" ? readState.data : null;
  const preferencesState = preferences.state;

  if (readState.status === "anonymous" || preferencesState.status === "anonymous") {
    return (
      <main className="mx-auto max-w-site px-5 py-12 md:px-8 md:py-16">
        <Header />
        <section
          data-testid="practice-reports-unauthenticated"
          className="mt-10 border border-ink/25 p-6"
        >
          <p className="font-mono text-xs text-ink/60">
            <span className="tracking-[0.2em]">SIGN IN REQUIRED</span> / 请先登录后查看学习报告
          </p>
          <button
            type="button"
            onClick={() =>
              redirectToLogin(window.location.pathname + window.location.search)
            }
            className="mt-5 inline-flex min-h-11 items-center border border-ink px-4 py-2 font-mono text-xs transition-colors hover:bg-ink hover:text-paper"
          >
            登录查看
          </button>
        </section>
      </main>
    );
  }

  const commandFailed = command.status === "error" ? command : null;
  const working = command.status === "working" ? command.action : null;
  const canGenerate =
    preferencesState.status === "ready" &&
    learningPreferencesAllowGeneration(preferencesState.data);
  const taskSettled = task ? learningTaskIsSettled(task) : true;
  const taskStopped = Boolean(task) && !taskSettled && taskAttempts >= LEARNING_REPORT_POLL_LIMIT;
  const taskFailed = task?.status === "failed" || task?.status === "paused";

  return (
    <main className="mx-auto max-w-site px-5 py-12 md:px-8 md:py-16">
      <Header />

      {catalog.status === "loading" && (
        <section data-testid="practice-reports-loading" className="mt-10">
          <LoadingBlock label="正在读取题库目录" />
        </section>
      )}

      {catalog.status === "error" && (
        <section data-testid="practice-reports-catalog-error" className="mt-10">
          <ErrorBanner
            message={catalog.message}
            requestId={catalog.requestId}
            onRetry={() => void loadCatalog()}
          />
        </section>
      )}

      {catalog.status === "ready" && !selectedBank && (
        <section data-testid="practice-reports-no-bank" className="mt-10">
          <EmptyBlock
            label="先在题库目录里选一门课，学习报告按课程分别生成"
            action={{ label: "去题库", href: "/practice" }}
          />
        </section>
      )}

      {catalog.status === "ready" && selectedBank && (
        <>
          <div data-block className="mt-10 flex flex-wrap items-center gap-4 border-t border-line pt-5">
            {catalog.banks.length > 1 ? (
              <label className="flex items-center gap-3">
                <span className="font-mono text-xs text-ink/60">课程</span>
                <select
                  data-testid="practice-reports-bank"
                  value={selectedBank.bank_id}
                  onChange={(event) => {
                    setTask(null);
                    setBankID(event.target.value);
                  }}
                  className="min-h-11 border border-ink/25 bg-paper px-3 text-sm"
                >
                  {catalog.banks.map((bank) => (
                    <option key={bank.bank_id} value={bank.bank_id}>
                      {bank.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <p className="font-mono text-xs text-ink/60">{selectedBank.name}</p>
            )}
            <p className="font-mono text-xs text-ink/60">
              {selectedBank.question_count} 题
            </p>
          </div>

          {preferencesState.status === "loading" && (
            <section data-testid="practice-reports-loading" className="mt-10">
              <LoadingBlock label="正在读取学习报告设置" />
            </section>
          )}

          {preferencesState.status === "error" && (
            <section data-testid="practice-reports-preferences-error" className="mt-10">
              <ErrorBanner
                message={preferencesState.message}
                requestId={preferencesState.requestId}
                onRetry={preferences.retry}
              />
            </section>
          )}

          {preferencesState.status === "ready" && (
            <LearningReportSettings
              key={`${selectedBank.bank_id}:${preferencesState.data.revision}`}
              preferences={preferencesState.data}
              chapters={selectedBank.chapters}
              saving={working === "save"}
              saved={savedPreferences}
              onSave={onSave}
            />
          )}

          {commandFailed && (
            <section data-testid="practice-reports-command-error" className="mt-10">
              <ErrorBanner
                message={commandFailed.message}
                requestId={commandFailed.requestId}
              />
            </section>
          )}

          {preferencesState.status === "ready" && (
            <div data-block className="mt-10 flex flex-wrap items-center gap-4 border-t border-line pt-5">
              <button
                type="button"
                data-testid="practice-reports-generate"
                onClick={() => void onGenerate()}
                disabled={!canGenerate || working === "request"}
                className="inline-flex min-h-11 items-center border border-ink px-4 font-mono text-xs transition-colors hover:bg-ink hover:text-paper disabled:opacity-60"
              >
                {working === "request" ? "正在生成" : "生成报告"}
              </button>
              {!canGenerate && (
                <p className="font-mono text-xs text-ink/60">
                  勾选「定期生成」并同意后即可生成报告。
                </p>
              )}
              <button
                type="button"
                data-testid="practice-reports-clear"
                onClick={() =>
                  confirmingClear ? void onClear() : setConfirmingClear(true)
                }
                disabled={working === "clear"}
                className="inline-flex min-h-11 items-center border border-ink/25 px-4 font-mono text-xs transition-colors hover:bg-ink/5 disabled:opacity-60"
              >
                {confirmingClear ? "确认清除报告" : "清除报告"}
              </button>
              <p className="font-mono text-xs text-ink/60">
                清除会撤回已生成的报告和排队中的生成，原始作答不会被删除。
              </p>
            </div>
          )}

          {task && !taskSettled && (
            <section
              data-testid="practice-reports-task"
              className="mt-10 border border-ink/25 p-5"
            >
              <p className="text-sm leading-7">
                {taskStopped
                  ? "报告还在生成，过一会儿刷新本页就能看到。"
                  : "报告正在生成，稍候会自动刷新。"}
              </p>
            </section>
          )}

          {taskFailed && (
            <section data-testid="practice-reports-task-failed" className="mt-10">
              <EmptyBlock
                label="这次没能生成报告，可以稍后再试一次"
                action={{ label: "去刷题", href: "/practice" }}
              />
            </section>
          )}

          {latest.state.status === "loading" && (
            <section data-testid="practice-reports-loading" className="mt-10">
              <LoadingBlock label="正在读取学习报告" />
            </section>
          )}

          {latest.state.status === "error" && (
            <section data-testid="practice-reports-error" className="mt-10">
              <ErrorBanner
                message={latest.state.message}
                requestId={latest.state.requestId}
                onRetry={latest.retry}
              />
            </section>
          )}

          {readState.status === "ready" && !report && (
            <section data-testid="practice-reports-empty" className="mt-10">
              <EmptyBlock
                label="这门课还没有学习报告，开启设置后生成第一份"
                action={{ label: "去刷题", href: "/practice" }}
              />
            </section>
          )}

          {report && (
            <LearningReportView
              report={report}
              starting={working === "practice"}
              onStartPractice={() => void onStartPractice(report.report_id)}
            />
          )}
        </>
      )}
    </main>
  );
}

export default function ReportsPage() {
  // Runs for both the open and the dark branch so the header reveal behaves the
  // same whichever state the flag puts this page in.
  usePageEnter(null);

  if (!reportsEnabled) {
    return (
      <main className="mx-auto max-w-site px-5 py-12 md:px-8 md:py-16">
        <Header />
        <section data-testid="practice-reports-disabled" className="mt-10">
          <EmptyBlock
            label="学习报告暂未开放，先去刷题"
            action={{ label: "去题库", href: "/practice" }}
          />
        </section>
      </main>
    );
  }
  return <ReportsSurface />;
}
