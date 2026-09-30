package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

func newLearningLeaseTest(t *testing.T, name string) (*pgxpool.Pool, *quizcraft.Service, uuid.UUID, uuid.UUID, quizcraft.LearningJobVersions) {
	t.Helper()
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, name)
	installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	owner, bankID := uuid.New(), uuid.MustParse(bank.BankID)
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	return pool, service, owner, bankID, quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion}
}

func TestLearningLeasesRecoverAndCapAttemptsWithoutOldTokenWrites(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-leases")
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Second); err == nil || ok {
		t.Fatal("tiny lease accepted")
	}
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok || first.JobID != task.TaskId || first.Attempts != 1 || first.LeaseToken == uuid.Nil || first.Snapshot.Versions != versions {
		t.Fatalf("first lease: %+v %t %v", first, ok, err)
	}
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || ok {
		t.Fatalf("live lease doubled: %t %v", ok, err)
	}
	fake := first
	fake.LeaseToken = uuid.New()
	if _, err := service.RenewLearningLease(ctx, fake, time.Minute); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("forged renewal: %v", err)
	}
	if _, err := service.FailLearningLease(ctx, fake, "provider_error"); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("forged failure: %v", err)
	}
	renewed, err := service.RenewLearningLease(ctx, first, time.Minute)
	if err != nil || !renewed.After(first.LeaseUntil) {
		t.Fatalf("renewal: %s %v", renewed, err)
	}
	first.LeaseUntil = renewed
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, task.TaskId); err != nil {
		t.Fatal(err)
	}
	var expiredWorkers sync.WaitGroup
	expiredResults := make(chan error, 2)
	for i := 0; i < 2; i++ {
		expiredWorkers.Add(1)
		go func() {
			defer expiredWorkers.Done()
			_, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
			if err != nil {
				expiredResults <- err
			} else if ok {
				expiredResults <- errors.New("expired lease bypassed retry cooldown")
			} else {
				expiredResults <- nil
			}
		}()
	}
	expiredWorkers.Wait()
	close(expiredResults)
	for err := range expiredResults {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.FailLearningLease(ctx, first, "provider_error"); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("old lease mutated task: %v", err)
	}
	var state, reason string
	if err := pool.QueryRow(ctx, `SELECT status,reason_code FROM quizcraft_learning_report_jobs WHERE id=$1`, task.TaskId).Scan(&state, &reason); err != nil || state != "queued" || reason != "lease_expired" {
		t.Fatalf("expiry = %s %s %v", state, reason, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET run_after=now()-interval '1 second' WHERE id=$1`, task.TaskId); err != nil {
		t.Fatal(err)
	}
	second, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok || second.Attempts != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("reclaim = %+v %t %v", second, ok, err)
	}
	if _, err := service.RenewLearningLease(ctx, first, time.Minute); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("old token renewed new lease: %v", err)
	}
	state, err = service.FailLearningLease(ctx, second, "provider_error")
	if err != nil || state != "queued" {
		t.Fatalf("retry = %s %v", state, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET run_after=now()-interval '1 second' WHERE id=$1`, task.TaskId); err != nil {
		t.Fatal(err)
	}
	third, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok || third.Attempts != 3 {
		t.Fatalf("third lease = %+v %t %v", third, ok, err)
	}
	state, err = service.FailLearningLease(ctx, third, "provider_error")
	if err != nil || state != "failed" {
		t.Fatalf("bounded failure = %s %v", state, err)
	}
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || ok {
		t.Fatalf("exhausted task was reclaimed: %t %v", ok, err)
	}
}

func TestLearningLeaseStopsAfterClearAndContendingClaim(t *testing.T) {
	ctx := context.Background()
	_, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-lease-clear")
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	got := make(chan quizcraft.LearningJobLease, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
			if ok {
				got <- lease
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(got)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	lease, ok := <-got
	if !ok || lease.JobID != task.TaskId {
		t.Fatalf("wrong claimed job: %+v", lease)
	}
	if _, extra := <-got; extra {
		t.Fatal("two workers received same lease")
	}
	if _, err := service.ClearLearningReports(ctx, owner, bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RenewLearningLease(ctx, lease, time.Minute); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("revoked lease renewed: %v", err)
	}
	if _, err := service.FailLearningLease(ctx, lease, "provider_error"); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("revoked lease wrote: %v", err)
	}
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || ok {
		t.Fatalf("cleared job resurrected: %t %v", ok, err)
	}
}

func TestLearningLeaseCancelsWithdrawnContentAndRejectsMalformedSnapshot(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-lease-withdrawn")
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_catalogs SET enabled=false WHERE bank_id=$1`, bankID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || ok {
		t.Fatalf("withdrawn content was leased: %t %v", ok, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_report_jobs WHERE id=$1`, task.TaskId).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("withdrawn job = %s %v", status, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_catalogs SET enabled=true WHERE bank_id=$1`, bankID); err != nil {
		t.Fatal(err)
	}
	var contentID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT active_content_version_id FROM quizcraft_learning_catalogs WHERE bank_id=$1`, bankID).Scan(&contentID); err != nil {
		t.Fatal(err)
	}
	invalid := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_report_jobs(id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot) VALUES($1,$2,$3,1,$4,repeat('a',64),'{}')`, invalid, owner, bankID, contentID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || ok {
		t.Fatalf("malformed snapshot was leased: %t %v", ok, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_report_jobs WHERE id=$1`, invalid).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("malformed job = %s %v", status, err)
	}
}

func TestLearningLeasePausesOnEntitlementDependencyError(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-lease-entitlement")
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	status, err := service.FailLearningLease(ctx, lease, "entitlement_unavailable")
	if err != nil || status != "paused" {
		t.Fatalf("entitlement failure = %s %v", status, err)
	}
	if _, ok, err := service.ClaimNextLearningReport(ctx, time.Minute); err != nil || ok {
		t.Fatalf("paused dependency retried automatically: %t %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET run_after=now()-interval '1 second' WHERE id=$1`, task.TaskId); err != nil {
		t.Fatal(err)
	}
	resumed, reused, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil || reused || resumed.Status != "queued" || resumed.TaskId != task.TaskId {
		t.Fatalf("explicit retried request = %+v %t %v", resumed, reused, err)
	}
}
