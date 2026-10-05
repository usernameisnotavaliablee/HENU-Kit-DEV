package quizcraft

import (
	"context"
	"strings"
	"testing"
	"time"
)

// HealthAlerts is the monitoring contract: a dark feature must stay silent, a
// genuinely stuck or failing one must speak, and thresholds must be honoured.
func TestHealthAlertsReportsOnlyRealOperationalProblems(t *testing.T) {
	behind := 30 * time.Minute
	age := 45 * time.Minute
	fresh := 5 * time.Minute

	if alerts := HealthAlerts(LearningFeedbackHealth{}, behind, 0); len(alerts) != 0 {
		t.Fatalf("dark feature raised alerts: %v", alerts)
	}
	if alerts := HealthAlerts(LearningFeedbackHealth{JobsByStatus: map[string]int{"ready": 3}, ReportsByStatus: map[string]int{"ready": 3}, EnabledCourses: 1, ConsentedCourses: 2, LatestReportAt: ptrTime(time.Now())}, behind, 0); len(alerts) != 0 {
		t.Fatalf("healthy feature raised alerts: %v", alerts)
	}

	stale := HealthAlerts(LearningFeedbackHealth{StaleLeases: 2}, behind, 0)
	if len(stale) != 1 || !strings.Contains(stale[0], "expired") {
		t.Fatalf("stale lease alert = %v", stale)
	}
	if alerts := HealthAlerts(LearningFeedbackHealth{QueuedOldestAge: &fresh}, behind, 0); len(alerts) != 0 {
		t.Fatalf("fresh queue raised alerts: %v", alerts)
	}
	queued := HealthAlerts(LearningFeedbackHealth{QueuedOldestAge: &age}, behind, 0)
	if len(queued) != 1 || !strings.Contains(queued[0], "behind or disabled") {
		t.Fatalf("queue alert = %v", queued)
	}
	// The boundary is exclusive: exactly at the threshold is not yet late.
	atThreshold := behind
	if alerts := HealthAlerts(LearningFeedbackHealth{QueuedOldestAge: &atThreshold}, behind, 0); len(alerts) != 0 {
		t.Fatalf("threshold boundary raised alerts: %v", alerts)
	}

	if alerts := HealthAlerts(LearningFeedbackHealth{Failures24h: map[string]int{"unspecified": 1}}, behind, 1); len(alerts) != 0 {
		t.Fatalf("failure budget raised alerts: %v", alerts)
	}
	failed := HealthAlerts(LearningFeedbackHealth{Failures24h: map[string]int{"provider_error": 2, "unspecified": 1}}, behind, 2)
	if len(failed) != 1 || !strings.Contains(failed[0], "provider_error=2") || !strings.Contains(failed[0], "budget 2") {
		t.Fatalf("failure alert = %v", failed)
	}

	drift := HealthAlerts(LearningFeedbackHealth{ConsentedCourses: 4}, behind, 0)
	if len(drift) != 1 || !strings.Contains(drift[0], "no learning course is enabled") {
		t.Fatalf("gating drift alert = %v", drift)
	}
	if alerts := HealthAlerts(LearningFeedbackHealth{ConsentedCourses: 0, EnabledCourses: 0}, behind, 0); len(alerts) != 0 {
		t.Fatalf("no members and no courses raised alerts: %v", alerts)
	}
	if alerts := HealthAlerts(LearningFeedbackHealth{ConsentedCourses: 3, EnabledCourses: 1}, behind, 0); len(alerts) != 0 {
		t.Fatalf("enabled course with consented members raised alerts: %v", alerts)
	}
}

func TestReadLearningFeedbackHealthRejectsInvalidInput(t *testing.T) {
	if _, err := ReadLearningFeedbackHealth(context.Background(), nil, time.Time{}); err != ErrLearningUnavailable {
		t.Fatalf("zero clock = %v", err)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
