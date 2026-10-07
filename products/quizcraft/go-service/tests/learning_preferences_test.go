package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

func TestLearningPreferencesDefaultsCadenceAndInvalidation(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-preferences")
	contentID := installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	user, bankID := uuid.New(), uuid.MustParse(bank.BankID)
	defaults, err := service.GetLearningReportPreferences(ctx, user, bankID)
	if err != nil || defaults.Enabled || defaults.ExternalAnalysisConsent || defaults.IntervalDays != 7 || defaults.Goal != "follow_course" || defaults.Revision != 0 || defaults.NextDueAt != nil {
		t.Fatalf("defaults = %+v %v", defaults, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_preferences`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read created preferences: %d %v", count, err)
	}
	input := contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{"ch02", "ch01"}}
	for _, mutate := range []func(*contract.LearningReportPreferencesUpdate){
		func(p *contract.LearningReportPreferencesUpdate) { p.IntervalDays = 0 }, func(p *contract.LearningReportPreferencesUpdate) { p.IntervalDays = 31 },
		func(p *contract.LearningReportPreferencesUpdate) { p.ExternalAnalysisConsent = false }, func(p *contract.LearningReportPreferencesUpdate) { p.ChapterIds = []string{"foreign-chapter"} },
	} {
		bad := input
		mutate(&bad)
		if _, err := service.UpdateLearningReportPreferences(ctx, user, bankID, bad); err == nil {
			t.Fatal("invalid preferences accepted")
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_preferences`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid update persisted partial preferences: %d %v", count, err)
	}
	before := time.Now()
	active, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !active.Enabled || !active.ExternalAnalysisConsent || active.NextDueAt == nil || active.NextDueAt.Before(before.Add(7*24*time.Hour)) || active.ChapterIds[0] != "ch01" {
		t.Fatalf("enabled = %+v", active)
	}
	input.ChapterIds = []string{"ch01", "ch02"}
	same, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input)
	if err != nil || same.Revision != active.Revision || !same.NextDueAt.Equal(*active.NextDueAt) || !same.UpdatedAt.Equal(*active.UpdatedAt) {
		t.Fatalf("no-op changed consent/schedule: %+v %v", same, err)
	}
	input.IntervalDays = 3
	faster, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input)
	if err != nil || faster.Revision != active.Revision || faster.NextDueAt == nil || !faster.NextDueAt.Before(*active.NextDueAt) {
		t.Fatalf("period edit invalidated analysis: %+v %v", faster, err)
	}
	jobID, leaseID, reportID := uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO quizcraft_learning_report_jobs(id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot,status,lease_token,lease_until) VALUES($1,$2,$3,$4,$5,repeat('a',64),'{}','running',$6,now()+interval '1 hour')`, jobID, user, bankID, faster.Revision, contentID, leaseID)
	exec(`INSERT INTO quizcraft_learning_reports(id,job_id,user_id,bank_id,content_version_id,lease_token,evidence_until,status,body) VALUES($1,$2,$3,$4,$5,$6,now(),'ready','{}')`, reportID, jobID, user, bankID, contentID, leaseID)
	exec(`UPDATE quizcraft_learning_report_preferences SET consent_version='outdated' WHERE user_id=$1 AND bank_id=$2`, user, bankID)
	if _, err := service.BuildLearningEvidence(ctx, user, bankID, time.Now()); err == nil {
		t.Fatal("outdated consent permitted evidence generation")
	}
	if _, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input); err == nil {
		t.Fatal("period update silently renewed consent")
	}
	exec(`UPDATE quizcraft_learning_report_preferences SET consent_version='v1' WHERE user_id=$1 AND bank_id=$2`, user, bankID)
	input.Goal = "exam_review"
	changed, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Revision <= faster.Revision || !changed.NextDueAt.Equal(*faster.NextDueAt) {
		t.Fatal("goal edit must invalidate old inputs without resetting schedule")
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_report_jobs WHERE id=$1`, jobID).Scan(&state); err != nil || state != "cancelled" {
		t.Fatalf("old job = %s %v", state, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_reports WHERE id=$1`, reportID).Scan(&state); err != nil || state != "stale" {
		t.Fatalf("old report = %s %v", state, err)
	}
	exec(`UPDATE quizcraft_learning_catalogs SET enabled=false WHERE bank_id=$1`, bankID)
	input.Enabled = false
	input.ExternalAnalysisConsent = false
	disabled, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input)
	if err != nil || disabled.Enabled || disabled.ExternalAnalysisConsent || disabled.NextDueAt != nil || disabled.Revision <= changed.Revision {
		t.Fatalf("cleanup failed after content withdrawal: %+v %v", disabled, err)
	}
	input.Enabled = true
	input.ExternalAnalysisConsent = true
	if _, err := service.UpdateLearningReportPreferences(ctx, user, bankID, input); err == nil {
		t.Fatal("withdrawn content enabled")
	}
}

func TestLearningClearDisablesConsentAndPreservesOtherOwnersAndAttempts(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-clear")
	contentID := installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	owner, other, bankID := uuid.New(), uuid.New(), uuid.MustParse(bank.BankID)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	var ownerRevision int64
	var ownerJob, ownerLease uuid.UUID
	for _, user := range []uuid.UUID{owner, other} {
		preferences, err := service.UpdateLearningReportPreferences(ctx, user, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		if user == owner {
			ownerRevision = preferences.Revision
		}
		job, lease := uuid.New(), uuid.New()
		if user == owner {
			ownerJob, ownerLease = job, lease
		}
		exec(`INSERT INTO quizcraft_learning_report_jobs(id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot,status,lease_token,lease_until) VALUES($1,$2,$3,$4,$5,repeat('b',64),'{}','running',$6,now()+interval '1 hour')`, job, user, bankID, preferences.Revision, contentID, lease)
		exec(`INSERT INTO quizcraft_learning_reports(id,job_id,user_id,bank_id,content_version_id,lease_token,evidence_until,status,body) VALUES($1,$2,$3,$4,$5,$6,now(),'ready','{}')`, uuid.New(), job, user, bankID, contentID, lease)
	}
	q := bank.Questions[0]
	session := uuid.New()
	exec(`INSERT INTO quizcraft_practice_sessions(id,bank_id,bank_version_id,user_id,actor_key,mode) VALUES($1,$2,$3,$4,'user:'||$4::uuid::text,'random')`, session, bankID, bank.BankVersionID, owner)
	exec(`INSERT INTO quizcraft_practice_session_questions(session_id,bank_id,bank_version_id,question_id,question_version_id,position) VALUES($1,$2,$3,$4,$5,1)`, session, bankID, bank.BankVersionID, q.QuestionID, q.QuestionVersionID)
	exec(`INSERT INTO quizcraft_practice_attempts(id,session_id,bank_id,bank_version_id,question_id,question_version_id,user_id,submitted_answer,correct,expected_answer,response_body) VALUES($1,$2,$3,$4,$5,$6,$7,'0',false,'1','{}')`, uuid.New(), session, bankID, bank.BankVersionID, q.QuestionID, q.QuestionVersionID, owner)
	exec(`UPDATE quizcraft_learning_catalogs SET enabled=false WHERE bank_id=$1`, bankID)
	cleared, err := service.ClearLearningReports(ctx, owner, bankID)
	if err != nil || !cleared.Cleared || cleared.Revision <= ownerRevision {
		t.Fatalf("clear = %+v %v", cleared, err)
	}
	profile, err := service.GetLearningReportPreferences(ctx, owner, bankID)
	if err != nil || profile.Enabled || profile.ExternalAnalysisConsent || profile.NextDueAt != nil {
		t.Fatalf("clear left active consent: %+v %v", profile, err)
	}
	for _, table := range []string{"quizcraft_learning_report_jobs", "quizcraft_learning_reports"} {
		var own, foreign int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE user_id=$1", owner).Scan(&own); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE user_id=$1", other).Scan(&foreign); err != nil {
			t.Fatal(err)
		}
		if own != 0 || foreign != 1 {
			t.Fatalf("%s owner isolation = %d/%d", table, own, foreign)
		}
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_attempts WHERE user_id=$1`, owner).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("original attempts were removed: %d %v", attempts, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_reports(id,job_id,user_id,bank_id,content_version_id,lease_token,evidence_until,status,body) VALUES($1,$2,$3,$4,$5,$6,now(),'ready','{}')`, uuid.New(), ownerJob, owner, bankID, contentID, ownerLease); err == nil {
		t.Fatal("late result resurrected cleared feedback")
	}
	again, err := service.ClearLearningReports(ctx, owner, bankID)
	if err != nil || again.Revision <= cleared.Revision {
		t.Fatalf("repeat clear = %+v %v", again, err)
	}
}
