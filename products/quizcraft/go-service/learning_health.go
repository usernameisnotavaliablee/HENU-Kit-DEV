package quizcraft

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// LearningFeedbackHealth is the operator's read-only view of whether automatic
// learning feedback is actually running. Counts are a cost proxy, not billing:
// the provider adapter does not report token usage, so no token or currency
// accounting is claimed here.
type LearningFeedbackHealth struct {
	GeneratedAt      time.Time      `json:"generated_at"`
	JobsByStatus     map[string]int `json:"jobs_by_status"`
	QueuedOldestAge  *time.Duration `json:"queued_oldest_age,omitempty"`
	StaleLeases      int            `json:"stale_leases"`
	Failures24h      map[string]int `json:"failures_24h"`
	ReportsByStatus  map[string]int `json:"reports_by_status"`
	EnabledCourses   int            `json:"enabled_courses"`
	ConsentedCourses int            `json:"consented_member_courses"`
	LatestReportAt   *time.Time     `json:"latest_report_at,omitempty"`
}

func (health LearningFeedbackHealth) failureTotal() int {
	total := 0
	for _, count := range health.Failures24h {
		total += count
	}
	return total
}

// HealthAlerts turns the health read into operator-visible problems. It is a pure
// function so the thresholds can be reviewed and tested without a database, and
// it never reports a healthy feature as broken: a dark feature (nothing enabled,
// no queued work, no failures) produces no alerts.
func HealthAlerts(health LearningFeedbackHealth, queuedBehind time.Duration, failureBudget int) []string {
	alerts := []string{}
	if health.StaleLeases > 0 {
		alerts = append(alerts, fmt.Sprintf("%d learning report job(s) hold a lease that already expired; a worker likely stopped mid-run", health.StaleLeases))
	}
	if health.QueuedOldestAge != nil && *health.QueuedOldestAge > queuedBehind {
		alerts = append(alerts, fmt.Sprintf("oldest queued learning report request is %s old; the worker is behind or disabled", health.QueuedOldestAge.Round(time.Second)))
	}
	if total := health.failureTotal(); total > failureBudget {
		reasons := make([]string, 0, len(health.Failures24h))
		for reason, count := range health.Failures24h {
			reasons = append(reasons, fmt.Sprintf("%s=%d", reason, count))
		}
		sort.Strings(reasons)
		alerts = append(alerts, fmt.Sprintf("%d learning report job(s) failed in the last 24h (budget %d): %v", total, failureBudget, reasons))
	}
	if health.ConsentedCourses > 0 && health.EnabledCourses == 0 {
		alerts = append(alerts, fmt.Sprintf("%d member course(s) consented to generation but no learning course is enabled; the feature cannot serve them", health.ConsentedCourses))
	}
	return alerts
}

// ReadLearningFeedbackHealth aggregates job, report and gating state. It only
// reads, so it is safe to run against production for monitoring.
func ReadLearningFeedbackHealth(ctx context.Context, query learningQuerier, now time.Time) (LearningFeedbackHealth, error) {
	if now.IsZero() {
		return LearningFeedbackHealth{}, ErrLearningUnavailable
	}
	health := LearningFeedbackHealth{
		GeneratedAt:     now.UTC(),
		JobsByStatus:    map[string]int{},
		Failures24h:     map[string]int{},
		ReportsByStatus: map[string]int{},
	}
	if err := countByStatus(ctx, query, `SELECT status,count(*) FROM quizcraft_learning_report_jobs GROUP BY status`, health.JobsByStatus); err != nil {
		return LearningFeedbackHealth{}, err
	}
	if err := countByStatus(ctx, query, `SELECT status,count(*) FROM quizcraft_learning_reports GROUP BY status`, health.ReportsByStatus); err != nil {
		return LearningFeedbackHealth{}, err
	}
	if err := countByStatus(ctx, query, `SELECT COALESCE(NULLIF(reason_code,''),'unspecified'),count(*) FROM quizcraft_learning_report_jobs
        WHERE status='failed' AND updated_at >= $1::timestamptz - interval '24 hours' GROUP BY 1`, health.Failures24h, now); err != nil {
		return LearningFeedbackHealth{}, err
	}
	// Queue latency is measured against the due time, not the insert time: a job
	// that is not due yet is not late, and run_after is the only mutable clock on
	// the row (created_at is a frozen consent-generation fact).
	var oldestDue *time.Time
	if err := query.QueryRow(ctx, `SELECT min(run_after) FROM quizcraft_learning_report_jobs WHERE status='queued'`).Scan(&oldestDue); err != nil {
		return LearningFeedbackHealth{}, err
	}
	if oldestDue != nil {
		age := now.Sub(*oldestDue)
		if age < 0 {
			age = 0
		}
		health.QueuedOldestAge = &age
	}
	var latestReport *time.Time
	if err := query.QueryRow(ctx, `SELECT max(created_at) FROM quizcraft_learning_reports`).Scan(&latestReport); err != nil {
		return LearningFeedbackHealth{}, err
	}
	health.LatestReportAt = latestReport
	if err := query.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE status='running' AND lease_until < $1`, now).
		Scan(&health.StaleLeases); err != nil {
		return LearningFeedbackHealth{}, err
	}
	if err := query.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_catalogs WHERE enabled`).Scan(&health.EnabledCourses); err != nil {
		return LearningFeedbackHealth{}, err
	}
	// Same gate as every queue and evidence read: consent from an older
	// generation is not consent, so it must not raise the "consented but no
	// enabled course" alert.
	if err := query.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_preferences WHERE enabled AND external_analysis_consent AND consent_version=$1`, learningConsentVersion).Scan(&health.ConsentedCourses); err != nil {
		return LearningFeedbackHealth{}, err
	}
	return health, nil
}

func countByStatus(ctx context.Context, query learningQuerier, sql string, target map[string]int, args ...any) error {
	rows, err := query.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		target[status] = count
	}
	return rows.Err()
}
