"use client";

import { useState } from "react";
import type {
  LearningReportGoal,
  LearningReportPreferences,
  LearningReportPreferencesUpdate,
} from "@/lib/api/types";
import type { QuizCraftCatalogChapter } from "@/lib/api/types";

const GOALS: { value: LearningReportGoal; label: string }[] = [
  { value: "follow_course", label: "跟着课程进度" },
  { value: "exam_review", label: "准备考试复习" },
];

const INTERVAL_OPTIONS = [3, 7, 14, 30];

/**
 * The member's own settings. Enabling generation and handing the minimized
 * statistics and question samples to an outside model are two separate choices: Core refuses to
 * generate without the second one, so this form never implies otherwise.
 */
export default function LearningReportSettings({
  preferences,
  chapters,
  saving,
  saved,
  onSave,
}: {
  preferences: LearningReportPreferences;
  chapters: QuizCraftCatalogChapter[];
  saving: boolean;
  /** Owned by the page: a re-read of the saved revision must not hide it. */
  saved: boolean;
  onSave: (input: LearningReportPreferencesUpdate) => Promise<void>;
}) {
  const [enabled, setEnabled] = useState(preferences.enabled);
  const [consent, setConsent] = useState(preferences.external_analysis_consent);
  const [goal, setGoal] = useState<LearningReportGoal>(preferences.goal);
  const [intervalDays, setIntervalDays] = useState(preferences.interval_days);
  const [chapterIDs, setChapterIDs] = useState<string[]>(preferences.chapter_ids);

  const toggleChapter = (chapterID: string) => {
    setChapterIDs((current) =>
      current.includes(chapterID)
        ? current.filter((id) => id !== chapterID)
        : [...current, chapterID]
    );
  };

  const submit = async () => {
    await onSave({
      enabled,
      interval_days: intervalDays,
      goal,
      chapter_ids: chapterIDs,
      external_analysis_consent: consent,
    });
  };

  return (
    <section
      data-testid="practice-reports-settings"
      data-block
      data-enter
      className="mt-10 border border-ink/25 p-5 md:p-7"
    >
      <p className="font-mono text-xs text-ink/60">
        <span className="tracking-[0.25em]">SETTINGS</span> / 学习报告设置
      </p>

      <div className="mt-5 space-y-5">
        <label className="flex items-start gap-3">
          <input
            type="checkbox"
            data-testid="practice-reports-enabled"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
            className="mt-1 h-5 w-5"
          />
          <span className="text-sm leading-7">
            定期为我生成这门课的学习报告
            <span className="mt-1 block text-ink/60">
              只有你主动开启后才会生成；取消勾选并保存即关闭，也可以清除已经生成的报告。
            </span>
          </span>
        </label>

        <label className="flex items-start gap-3">
          <input
            type="checkbox"
            data-testid="practice-reports-consent"
            checked={consent}
            onChange={(event) => setConsent(event.target.checked)}
            className="mt-1 h-5 w-5"
          />
          <span className="text-sm leading-7">
            允许把最少的作答统计和题目样本交给外部模型，用于判断需要优先加强的内容并给出可能的原因
            <span className="mt-1 block text-ink/60">
              只包含题目内容、课程标签、你选择的学习目标和你在这门课里的作答表现，不含账户信息；不开这一项就不会生成报告。
            </span>
          </span>
        </label>

        <div>
          <p className="font-mono text-xs text-ink/60">学习目标</p>
          <div className="mt-2 flex flex-wrap gap-2">
            {GOALS.map((item) => (
              <button
                key={item.value}
                type="button"
                aria-pressed={goal === item.value}
                onClick={() => setGoal(item.value)}
                className={`inline-flex min-h-11 items-center border px-4 font-mono text-xs transition-colors ${
                  goal === item.value
                    ? "border-ink bg-ink text-paper"
                    : "border-ink/25 hover:bg-ink/5"
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>

        <div>
          <p className="font-mono text-xs text-ink/60">生成间隔</p>
          <div className="mt-2 flex flex-wrap gap-2">
            {INTERVAL_OPTIONS.map((days) => (
              <button
                key={days}
                type="button"
                aria-pressed={intervalDays === days}
                onClick={() => setIntervalDays(days)}
                className={`inline-flex min-h-11 items-center border px-4 font-mono text-xs transition-colors ${
                  intervalDays === days
                    ? "border-ink bg-ink text-paper"
                    : "border-ink/25 hover:bg-ink/5"
                }`}
              >
                每 {days} 天
              </button>
            ))}
          </div>
        </div>

        {chapters.length > 0 && (
          <div>
            <p className="font-mono text-xs text-ink/60">关注章节（不选表示整门课）</p>
            <div className="mt-2 flex flex-wrap gap-2">
              {chapters.map((chapter) => {
                const selected = chapterIDs.includes(chapter.id);
                return (
                  <button
                    key={chapter.id}
                    type="button"
                    aria-pressed={selected}
                    onClick={() => toggleChapter(chapter.id)}
                    className={`inline-flex min-h-11 items-center border px-4 text-xs transition-colors ${
                      selected ? "border-ink bg-ink text-paper" : "border-ink/25 hover:bg-ink/5"
                    }`}
                  >
                    {chapter.name}
                  </button>
                );
              })}
            </div>
          </div>
        )}
      </div>

      <div className="mt-7 flex flex-wrap items-center gap-4 border-t border-line pt-5">
        <button
          type="button"
          data-testid="practice-reports-save"
          onClick={() => void submit()}
          disabled={saving}
          className="inline-flex min-h-11 items-center border border-ink px-4 font-mono text-xs transition-colors hover:bg-ink hover:text-paper disabled:opacity-60"
        >
          {saving ? "正在保存" : "保存设置"}
        </button>
        {saved && (
          <p data-testid="practice-reports-saved" className="font-mono text-xs text-ink/60">
            已保存。
          </p>
        )}
        <p className="font-mono text-xs text-ink/60">
          改课程范围、目标或授权后，已生成的报告会失效，需要重新生成
        </p>
      </div>
    </section>
  );
}
