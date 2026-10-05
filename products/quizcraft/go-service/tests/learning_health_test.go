package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

// A consent from an older generation is not consent. The operator count must use
// the same gate as every queue and evidence read, otherwise a stale consent looks
// live and raises the "consented member has no enabled course" alert.
func TestLearningFeedbackHealthIgnoresStaleConsentGeneration(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, _ := newLearningLeaseTest(t, "learning-health-consent")
	const behind = 30 * time.Minute

	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET consent_version='consent-v0' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	health, err := quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health.EnabledCourses != 1 || health.ConsentedCourses != 0 {
		t.Fatalf("stale consent counted as live: %+v", health)
	}
	if alerts := quizcraft.HealthAlerts(health, behind, 0); len(alerts) != 0 {
		t.Fatalf("stale consent raised the live-consent alert: %v", alerts)
	}

	// Live consent needs the documented two-step renewal: an outdated generation
	// cannot be renewed in place, the owner opts out and opts in again.
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: false, ExternalAnalysisConsent: false, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	health, err = quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health.ConsentedCourses != 1 {
		t.Fatalf("live consent was not counted: %+v", health)
	}
}

// The health read is the operator's only view of a dark feature, so it must
// report real stored state: a fresh queue is not an alert, a queue that is hours
// behind is, an expired worker lease is, and failure reasons are bucketed by the
// 24-hour window. It must also stay read-only.
func TestLearningFeedbackHealthReflectsStoredJobAndReportState(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-health")
	const behind = 30 * time.Minute

	health, err := quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(health.JobsByStatus) != 0 || len(health.ReportsByStatus) != 0 || health.QueuedOldestAge != nil || health.LatestReportAt != nil {
		t.Fatalf("empty feature reported activity: %+v", health)
	}
	if health.EnabledCourses != 1 || health.ConsentedCourses != 1 {
		t.Fatalf("gating counts = enabled %d consented %d", health.EnabledCourses, health.ConsentedCourses)
	}
	if alerts := quizcraft.HealthAlerts(health, behind, 0); len(alerts) != 0 {
		t.Fatalf("idle but correctly configured feature raised alerts: %v", alerts)
	}

	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
		t.Fatal(err)
	}
	health, err = quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health.JobsByStatus["queued"] != 1 || health.QueuedOldestAge == nil || *health.QueuedOldestAge > time.Minute {
		t.Fatalf("fresh queue = %+v", health)
	}
	if alerts := quizcraft.HealthAlerts(health, behind, 0); len(alerts) != 0 {
		t.Fatalf("fresh queue raised alerts: %v", alerts)
	}

	// A queue that is hours old is the signal that the worker is not running.
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET run_after=now()-interval '2 hours' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	health, err = quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	alerts := quizcraft.HealthAlerts(health, behind, 0)
	if len(alerts) != 1 || health.QueuedOldestAge == nil || *health.QueuedOldestAge < time.Hour {
		t.Fatalf("stale queue = %+v alerts=%v", health.QueuedOldestAge, alerts)
	}

	// A lease that already expired means a worker stopped mid-run.
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || !ok {
		t.Fatalf("claim lease: %t %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET lease_until=now()-interval '1 minute' WHERE status='running'`); err != nil {
		t.Fatal(err)
	}
	health, err = quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health.StaleLeases != 1 || health.JobsByStatus["running"] != 1 {
		t.Fatalf("stale lease read = %+v", health)
	}
	// Claiming the job emptied the queue, so only the expired lease speaks.
	alerts = quizcraft.HealthAlerts(health, behind, 0)
	if len(alerts) != 1 || health.QueuedOldestAge != nil {
		t.Fatalf("stale lease alerts = %v queue=%v", alerts, health.QueuedOldestAge)
	}

	// Failure reasons are bucketed inside the last 24 hours only.
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='failed',lease_token=NULL,lease_until=NULL,reason_code='provider_error',updated_at=now() WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	health, err = quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if health.Failures24h["provider_error"] != 1 || health.JobsByStatus["failed"] != 1 {
		t.Fatalf("failure read = %+v", health)
	}
	alerts = quizcraft.HealthAlerts(health, behind, 0)
	if len(alerts) != 1 || !strings.Contains(alerts[0], "provider_error=1") {
		t.Fatalf("failure alerts = %v", alerts)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET updated_at=now()-interval '48 hours' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	health, err = quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(health.Failures24h) != 0 {
		t.Fatalf("old failure still counted: %+v", health.Failures24h)
	}

	// Monitoring must not mutate anything it reads.
	var before, after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	first, err := quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := quizcraft.ReadLearningFeedbackHealth(ctx, pool, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || first.JobsByStatus["failed"] != second.JobsByStatus["failed"] || first.EnabledCourses != second.EnabledCourses {
		t.Fatalf("health read changed state: %d -> %d, %+v vs %+v", before, after, first, second)
	}
}
