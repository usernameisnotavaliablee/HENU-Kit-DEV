"use client";

import { learningNextStepLabel, learningReportStatusCopy } from "@/lib/practice/learning-reports";
import type { LearningReport } from "@/lib/api/types";

function formatAnswer(value: unknown): string {
  if (value === null || value === undefined) return "未作答";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value)) return value.map(formatAnswer).join("、");
  return JSON.stringify(value);
}

function StatisticRow({
  label,
  attempts,
  unique,
  firstCorrect,
}: {
  label: string;
  attempts: number;
  unique: number;
  firstCorrect: number;
}) {
  return (
    <div className="border-t border-line py-4 first:border-t-0">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="font-medium">{label}</p>
        <p className="font-mono text-xs text-ink/60 tabular-nums">
          作答 {attempts} 次 · 独立题 {unique} 道 · 首次答对 {firstCorrect} 次
        </p>
      </div>
    </div>
  );
}

/**
 * One report exactly as the server composed it. The browser adds no numbers and
 * no conclusions: statistics, findings and the suggested next step all come from
 * the report, and the evidence list is what those claims may rest on.
 */
export default function LearningReportView({
  report,
  starting,
  onStartPractice,
}: {
  report: LearningReport;
  starting: boolean;
  onStartPractice: () => void;
}) {
  const status = learningReportStatusCopy(report.status);
  const action = report.next_step;
  const startLabel = learningNextStepLabel(action.kind);
  const evidenceByID = new Map(report.evidence.map((item) => [item.evidence_id, item]));
  // Findings carry the internal tag id; members see the same label the
  // statistics use for that tag.
  const labelByTag = new Map(report.statistics.map((item) => [item.tag_id, item.label]));

  return (
    <section
      data-testid="practice-reports-report"
      data-block
      data-enter
      className="mt-10 border border-ink/25 p-5 md:p-7"
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="font-mono text-xs text-ink/60">
            <span className="tracking-[0.25em]">REPORT</span> / {status.title}
          </p>
          <p className="mt-2 max-w-2xl text-sm leading-7 text-ink/65">{status.detail}</p>
        </div>
        <p className="font-mono text-xs text-ink/60">
          {new Date(report.created_at).toLocaleDateString("zh-CN")} 生成
        </p>
      </div>

      {report.findings.length > 0 && (
        <div className="mt-8 space-y-5">
          {report.findings.map((finding) => (
            <div key={finding.tag_id} className="border border-line p-5">
              <div className="flex flex-wrap items-center gap-2">
                <p className="font-medium">{labelByTag.get(finding.tag_id) ?? "本课"}</p>
                <p className="font-mono text-xs text-ink/60">
                  {finding.status === "supported"
                    ? "证据支持"
                    : finding.status === "tentative"
                      ? "暂定"
                      : "证据不足"}
                </p>
              </div>
              <p className="mt-3 text-sm leading-7">{finding.observation}</p>
              {finding.possible_reason && (
                <p className="mt-2 text-sm leading-7 text-ink/65">
                  可能的原因：{finding.possible_reason}
                </p>
              )}
              <ul className="mt-4 space-y-2">
                {finding.evidence_ids.map((evidenceID, index) => {
                  const evidence = evidenceByID.get(evidenceID);
                  if (!evidence) return null;
                  return (
                    <li key={evidenceID} className="border-l border-ink/25 pl-3">
                      <p className="font-mono text-xs text-ink/60">
                        <span className="tracking-[0.2em]">E{String(index + 1).padStart(2, "0")}</span>
                        {" "}
                        {evidence.correct ? "答对" : "答错"}
                      </p>
                      <p className="mt-1 text-sm leading-7">{evidence.question}</p>
                      <p className="mt-1 font-mono text-xs text-ink/60">
                        你的作答 {formatAnswer(evidence.submitted_answer)} · 正确答案{" "}
                        {formatAnswer(evidence.expected_answer)}
                      </p>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      )}

      <div className="mt-8 border border-ink/25 p-5">
        <p className="font-mono text-xs text-ink/60">
          <span className="tracking-[0.25em]">NEXT</span> / 下一步
        </p>
        <p className="mt-3 text-sm leading-7">{action.reason}</p>
        {action.lesson && (
          <div className="mt-4 border-t border-line pt-4">
            <p className="font-medium">{action.lesson.title}</p>
            <p className="mt-2 text-sm leading-7 text-ink/65">{action.lesson.body}</p>
            {action.lesson.sources.length > 0 && (
              <ul className="mt-3 space-y-1">
                {action.lesson.sources.map((source) => (
                  <li key={source.source_id} className="font-mono text-xs text-ink/60">
                    {source.title} · {source.version} · {source.locator}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        {startLabel && (
          <button
            type="button"
            data-testid="practice-reports-start"
            onClick={onStartPractice}
            disabled={starting}
            className="mt-5 inline-flex min-h-11 items-center border border-ink px-4 font-mono text-xs transition-colors hover:bg-ink hover:text-paper disabled:opacity-60"
          >
            {starting ? "正在准备练习" : startLabel}
          </button>
        )}
      </div>

      {report.statistics.length > 0 && (
        <div className="mt-8">
          <p className="font-mono text-xs text-ink/60">
            <span className="tracking-[0.25em]">STATS</span> / 这门课里的作答
          </p>
          <div className="mt-3">
            {report.statistics.map((statistic) => (
              <StatisticRow
                key={statistic.tag_id}
                label={statistic.label}
                attempts={statistic.attempt_count}
                unique={statistic.unique_question_count}
                firstCorrect={statistic.first_correct_count}
              />
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
