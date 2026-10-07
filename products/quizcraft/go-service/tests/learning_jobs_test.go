package tests

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

func TestLearningQueueDeduplicatesVersionsAndKeepsManualCadence(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-queue")
	contentID := installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	bankID, owner := uuid.MustParse(bank.BankID), uuid.New()
	versions := quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion}
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err == nil {
		t.Fatal("consent missing")
	}
	pref, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	due := *pref.NextDueAt
	for _, bad := range []quizcraft.LearningJobVersions{
		{Model: "", Prompt: "p", Policy: quizcraft.LearningAnalysisPolicyVersion},
		{Model: "synthetic-model-1", Prompt: "", Policy: quizcraft.LearningAnalysisPolicyVersion},
		{Model: "synthetic-model-1", Prompt: "p", Policy: "unreviewed-policy"},
	} {
		if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", bad); err == nil {
			t.Fatal("invalid model version accepted")
		}
	}
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "alien", versions); err == nil {
		t.Fatal("invalid queue source accepted")
	}
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "automatic", versions); err == nil {
		t.Fatal("early automatic run queued")
	}
	first, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil || reused || first.Status != "queued" {
		t.Fatalf("first queue = %+v %t %v", first, reused, err)
	}
	again, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now().Add(time.Second), "manual", versions)
	if err != nil || !reused || again.TaskId != first.TaskId {
		t.Fatalf("same evidence created new job: %+v %t %v", again, reused, err)
	}
	current, err := service.GetLearningReportPreferences(ctx, owner, bankID)
	if err != nil || !current.NextDueAt.Equal(due) {
		t.Fatal("manual queue reset automatic interval")
	}
	var stored []byte
	var key string
	if err := pool.QueryRow(ctx, `SELECT snapshot,input_sha256 FROM quizcraft_learning_report_jobs WHERE id=$1`, first.TaskId).Scan(&stored, &key); err != nil {
		t.Fatal(err)
	}
	var payload quizcraft.LearningJobSnapshot
	if err := json.Unmarshal(stored, &payload); err != nil || payload.Versions != versions || payload.Evidence.InputSHA256 == key || payload.Evidence.ContentVersionID != contentID {
		t.Fatalf("job version/evidence identity missing: %+v %s %v", payload, key, err)
	}
	bumped := versions
	bumped.Prompt = "prompt-v2"
	changed, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", bumped)
	if err != nil || reused || changed.TaskId == first.TaskId {
		t.Fatalf("prompt version did not invalidate work: %+v %t %v", changed, reused, err)
	}
	bumped.Model = "synthetic-model-2"
	newer, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", bumped)
	if err != nil || reused || newer.TaskId == changed.TaskId {
		t.Fatalf("model version did not invalidate work: %+v %t %v", newer, reused, err)
	}
	var superseded string
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_report_jobs WHERE id=$1`, first.TaskId).Scan(&superseded); err != nil || superseded != "cancelled" {
		t.Fatalf("older pending version still eligible: %s %v", superseded, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=now()-interval '1 second' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	auto, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "automatic", bumped)
	if err != nil || !reused || auto.TaskId != newer.TaskId {
		t.Fatalf("unchanged due evidence must reuse: %+v %t %v", auto, reused, err)
	}
	advanced, err := service.GetLearningReportPreferences(ctx, owner, bankID)
	if err != nil || advanced.NextDueAt == nil || advanced.NextDueAt.Before(time.Now().Add(6*24*time.Hour)) {
		t.Fatalf("automatic cadence did not advance: %+v %v", advanced, err)
	}
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "automatic", bumped); err == nil {
		t.Fatal("automatic schedule ignored")
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='failed', attempts=3,reason_code='model_error',run_after=now()+interval '1 minute' WHERE id=$1`, newer.TaskId); err != nil {
		t.Fatal(err)
	}
	cooldown, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", bumped)
	if err != nil || !reused || cooldown.Status != "failed" || cooldown.RetryAfterSeconds == nil {
		t.Fatalf("retry skipped cooldown: %+v %t %v", cooldown, reused, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET run_after=now()-interval '1 second' WHERE id=$1`, newer.TaskId); err != nil {
		t.Fatal(err)
	}
	retry, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", bumped)
	if err != nil || reused || retry.Status != "queued" || retry.TaskId != newer.TaskId {
		t.Fatalf("failed work not requeued safely: %+v %t %v", retry, reused, err)
	}
	if _, err := service.ClearLearningReports(ctx, owner, bankID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", bumped); err == nil {
		t.Fatal("clear revived job")
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1`, owner).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("clear left jobs: %d %v", remaining, err)
	}
}

func TestLearningQueueConcurrentManualAndAutomaticAreOneJob(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-queue-race")
	installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	owner, bankID := uuid.New(), uuid.MustParse(bank.BankID)
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=now()-interval '1 second' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	versions := quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "p1", Policy: quizcraft.LearningAnalysisPolicyVersion}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	ids := make(chan uuid.UUID, 2)
	for _, source := range []string{"manual", "automatic"} {
		wg.Add(1)
		go func(source string) {
			defer wg.Done()
			task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), source, versions)
			errs <- err
			ids <- task.TaskId
		}(source)
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	a, b := <-ids, <-ids
	if a == uuid.Nil || a != b {
		t.Fatalf("concurrent queue duplicated: %s %s", a, b)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1`, owner).Scan(&count); err != nil || count != 1 {
		t.Fatalf("job count = %d %v", count, err)
	}
}
