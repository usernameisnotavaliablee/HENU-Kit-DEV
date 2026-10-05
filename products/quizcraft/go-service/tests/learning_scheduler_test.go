package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

// These tests drive the automatic scheduler against a real PostgreSQL: the
// scheduler may only pick up work the repository would accept anyway, and one
// broken row must never stop the rest of the sweep.

func TestLearningSchedulerQueuesOnlyDueConsentedWork(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-schedule")
	installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	bankID := uuid.MustParse(bank.BankID)
	versions := quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion}

	enable := func(owner uuid.UUID) {
		t.Helper()
		if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{
			Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{},
		}); err != nil {
			t.Fatal(err)
		}
	}
	healthy, notDue, staleConsent, disabled := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	enable(healthy)
	enable(notDue)
	enable(staleConsent)
	enable(disabled)

	// 表约束保证「开启 ⇒ 已同意且同意版本非空」，所以撤销同意只能靠关闭或清除；
	// 这里另外造一个同意版本过期的到期行：必须先重新确认才能自动生成。
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET consent_version='v0', next_due_at=clock_timestamp()-interval '1 minute' WHERE user_id=$1 AND bank_id=$2`, staleConsent, bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET enabled=false, next_due_at=clock_timestamp()-interval '1 minute' WHERE user_id=$1 AND bank_id=$2`, disabled, bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=clock_timestamp()-interval '1 minute' WHERE user_id=$1 AND bank_id=$2`, healthy, bankID); err != nil {
		t.Fatal(err)
	}
	before, err := service.GetLearningReportPreferences(ctx, notDue, bankID)
	if err != nil || before.NextDueAt == nil {
		t.Fatalf("preferences read = %+v %v", before, err)
	}

	queued, skipped, err := service.QueueDueLearningReports(ctx, versions, 10)
	if err != nil || queued != 1 || skipped != 0 {
		t.Fatalf("first sweep = %d/%d %v", queued, skipped, err)
	}
	for _, owner := range []uuid.UUID{healthy} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("due owner jobs = %d %v", count, err)
		}
	}
	for _, owner := range []uuid.UUID{notDue, staleConsent, disabled} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("owner %s should not be scheduled: %d %v", owner, count, err)
		}
	}
	advanced, err := service.GetLearningReportPreferences(ctx, healthy, bankID)
	if err != nil || advanced.NextDueAt == nil || !advanced.NextDueAt.After(time.Now()) {
		t.Fatalf("automatic run did not advance the interval: %+v %v", advanced, err)
	}
	untouched, err := service.GetLearningReportPreferences(ctx, notDue, bankID)
	if err != nil || !untouched.NextDueAt.Equal(*before.NextDueAt) {
		t.Fatal("sweep moved a preference that was not due")
	}
	// 同一周期内再扫一次不应该重复入队：到期时间已被顺延。
	queued, _, err = service.QueueDueLearningReports(ctx, versions, 10)
	if err != nil || queued != 0 {
		t.Fatalf("second sweep queued %d %v", queued, err)
	}
}

func TestLearningSchedulerSkipsBrokenRowsAndRespectsLimit(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	healthyBank := importPracticeBank(t, pool, "learning-schedule-healthy")
	installLearningTestContent(t, pool, healthyBank)
	brokenBank := importPracticeBank(t, pool, "learning-schedule-broken")
	installLearningTestContent(t, pool, brokenBank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	versions := quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion}
	// dueAt 用显式偏移排定到期顺序，避免 clock_timestamp 抖动让断言不确定。
	ownerFor := func(bank quizcraft.ImportReport, age string) uuid.UUID {
		t.Helper()
		owner := uuid.New()
		if _, err := service.UpdateLearningReportPreferences(ctx, owner, uuid.MustParse(bank.BankID), contract.LearningReportPreferencesUpdate{
			Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=clock_timestamp()-interval '1 minute'*$3 WHERE user_id=$1 AND bank_id=$2`, owner, uuid.MustParse(bank.BankID), age); err != nil {
			t.Fatal(err)
		}
		return owner
	}
	first := ownerFor(healthyBank, "2")
	second := ownerFor(healthyBank, "1")
	broken := ownerFor(brokenBank, "0.5")
	// 同意已建立之后再停用这门课的学习目录：仓储会拒绝入队，调度只能跳过它。
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_catalogs SET enabled=false WHERE bank_id=$1`, brokenBank.BankID); err != nil {
		t.Fatal(err)
	}

	// limit 只放行一条：另一条仍然到期，下一轮才会被处理。
	queued, skipped, err := service.QueueDueLearningReports(ctx, versions, 1)
	if err != nil || queued != 1 || skipped != 0 {
		t.Fatalf("limited sweep = %d/%d %v", queued, skipped, err)
	}
	queued, skipped, err = service.QueueDueLearningReports(ctx, versions, 10)
	if err != nil || queued != 1 || skipped != 1 {
		t.Fatalf("second sweep = %d/%d %v", queued, skipped, err)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE bank_id=$1`, healthyBank.BankID).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("healthy bank jobs = %d %v", jobs, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1`, broken).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("broken row was queued: %d %v", jobs, err)
	}
	// 跳过的行保留到期时间：依赖恢复后下一轮仍会处理它。
	kept, err := service.GetLearningReportPreferences(ctx, broken, uuid.MustParse(brokenBank.BankID))
	if err != nil || kept.NextDueAt == nil || !kept.NextDueAt.Before(time.Now()) {
		t.Fatalf("skipped row lost its due time: %+v %v", kept, err)
	}
	_ = first
	_ = second
}

func TestLearningSchedulerRejectsInvalidInputAndSurfacesSelectionFailure(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	valid := quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion}
	for name, versions := range map[string]quizcraft.LearningJobVersions{
		"missing model":  {Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion},
		"missing prompt": {Model: "synthetic-model-1", Policy: quizcraft.LearningAnalysisPolicyVersion},
		"unreviewed":     {Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: "unreviewed"},
	} {
		if _, _, err := service.QueueDueLearningReports(ctx, versions, 10); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	for _, limit := range []int{0, -1, 5000} {
		if _, _, err := service.QueueDueLearningReports(ctx, valid, limit); err == nil {
			t.Fatalf("limit %d accepted", limit)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := service.QueueDueLearningReports(cancelled, valid, 10); err == nil {
		t.Fatal("cancelled sweep reported success")
	}
}

func TestLearningSchedulerConcurrentSweepsQueueOnce(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-schedule-race")
	installLearningTestContent(t, pool, bank)
	service, err := quizcraft.New(quizcraft.Config{Database: pool})
	if err != nil {
		t.Fatal(err)
	}
	bankID := uuid.MustParse(bank.BankID)
	owner := uuid.New()
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{
		Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=clock_timestamp()-interval '1 minute' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	versions := quizcraft.LearningJobVersions{Model: "synthetic-model-1", Prompt: "prompt-v1", Policy: quizcraft.LearningAnalysisPolicyVersion}
	var wg sync.WaitGroup
	results := make([]int, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			queued, _, err := service.QueueDueLearningReports(ctx, versions, 10)
			results[i], errs[i] = queued, err
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent sweeps failed: %v %v", errs[0], errs[1])
	}
	if results[0]+results[1] != 1 {
		t.Fatalf("concurrent sweeps queued %d work", results[0]+results[1])
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1`, owner).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("job rows = %d %v", jobs, err)
	}
}
