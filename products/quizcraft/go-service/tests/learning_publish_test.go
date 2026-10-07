package tests

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

func TestLearningPublishChecksLiveEntitlementAndIdempotentColdStart(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-publish-cold")
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	allowed := func(_ context.Context, id uuid.UUID) (bool, error) { return id == owner, nil }
	for _, checker := range []quizcraft.LearningEntitlementCheck{
		nil,
		func(context.Context, uuid.UUID) (bool, error) { return false, nil },
		func(context.Context, uuid.UUID) (bool, error) { return false, errors.New("portfolio unavailable") },
	} {
		if _, err := service.PublishLearningReport(ctx, lease, nil, versions, checker); err == nil {
			t.Fatal("report published without live entitlement")
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1`, owner).Scan(&count); err != nil || count != 0 {
		t.Fatalf("denied publication persisted %d %v", count, err)
	}
	badVersions := versions
	badVersions.Prompt = "unreviewed-prompt"
	if _, err := service.PublishLearningReport(ctx, lease, nil, badVersions, allowed); err == nil {
		t.Fatal("stale prompt version published")
	}
	fake := lease
	fake.LeaseToken = uuid.New()
	if _, err := service.PublishLearningReport(ctx, fake, nil, versions, allowed); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("forged token: %v", err)
	}
	if _, err := service.PublishLearningReport(ctx, lease, []byte(`{"findings":[]}`), versions, allowed); err == nil {
		t.Fatal("model output accepted for zero history")
	}
	report, err := service.PublishLearningReport(ctx, lease, nil, versions, allowed)
	if err != nil || report.Status != "insufficient_evidence" || report.NextStep.Kind != "diagnostic" || len(report.Findings) != 0 {
		t.Fatalf("cold report = %+v %v", report, err)
	}
	repeat, err := service.PublishLearningReport(ctx, lease, []byte(`{"ignored":true}`), versions, allowed)
	if err != nil || repeat.ReportId != report.ReportId {
		t.Fatalf("same valid lease duplicated report: %+v %v", repeat, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE job_id=$1`, task.TaskId).Scan(&count); err != nil || count != 1 {
		t.Fatalf("idempotence count = %d %v", count, err)
	}
	reused, yes, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil || !yes || reused.Status != "ready" || reused.ReportId == nil || *reused.ReportId != report.ReportId {
		t.Fatalf("ready cold report not reusable: %+v %t %v", reused, yes, err)
	}
	schema, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Components.Schemas["LearningReport"].Value.VisitJSON(value); err != nil {
		t.Fatalf("published body violates contract: %v", err)
	}
}

func TestLearningPublishRealEvidenceRequiresValidModelDecision(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-publish-evidence")
	var bankVersion, questionID, questionVersion uuid.UUID
	err := pool.QueryRow(ctx, `SELECT b.active_version_id,q.question_id,q.id FROM quizcraft_banks b
        JOIN quizcraft_bank_version_questions m ON m.bank_id=b.id AND m.bank_version_id=b.active_version_id
        JOIN quizcraft_question_versions q ON q.bank_id=b.id AND q.id=m.question_version_id
        WHERE b.id=$1 AND q.type='single' ORDER BY q.question_id LIMIT 1`, bankID).Scan(&bankVersion, &questionID, &questionVersion)
	if err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_sessions(id,bank_id,bank_version_id,user_id,actor_key,mode) VALUES($1,$2,$3,$4,'user:'||$4::uuid::text,'random')`, session, bankID, bankVersion, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_session_questions(session_id,bank_id,bank_version_id,question_id,question_version_id,position) VALUES($1,$2,$3,$4,$5,1)`, session, bankID, bankVersion, questionID, questionVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_attempts(id,session_id,bank_id,bank_version_id,question_id,question_version_id,user_id,submitted_answer,correct,expected_answer,response_body) VALUES($1,$2,$3,$4,$5,$6,$7,'0',false,'1','{}')`, uuid.New(), session, bankID, bankVersion, questionID, questionVersion, owner); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok || len(lease.Snapshot.Evidence.Evidence) == 0 {
		t.Fatalf("real evidence missing: %+v %t %v", lease, ok, err)
	}
	allowed := func(context.Context, uuid.UUID) (bool, error) { return true, nil }
	if _, err := service.PublishLearningReport(ctx, lease, nil, versions, allowed); err == nil {
		t.Fatal("nonempty evidence accepted without model decision")
	}
	invalid := []byte(`{"findings":[{"tag_id":"math","status":"supported","evidence_ids":["forged"]}],"primary_tag_id":"math"}`)
	if _, err := service.PublishLearningReport(ctx, lease, invalid, versions, allowed); err == nil {
		t.Fatal("fabricated citation was published")
	}
	decision := []byte(`{"findings":[],"primary_tag_id":""}`)
	report, err := service.PublishLearningReport(ctx, lease, decision, versions, allowed)
	if err != nil || len(report.Evidence) == 0 || len(report.Statistics) == 0 || report.Status != "insufficient_evidence" {
		t.Fatalf("real evidence report = %+v %v", report, err)
	}
}

func TestLearningPublishAndClearCannotLeaveResurrectedReport(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-publish-clear")
	allowed := func(context.Context, uuid.UUID) (bool, error) { return true, nil }
	for i := 0; i < 5; i++ {
		if i > 0 {
			owner = uuid.New()
			if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
			t.Fatal(err)
		}
		lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
		if err != nil || !ok || lease.UserID != owner {
			t.Fatalf("claim = %+v %t %v", lease, ok, err)
		}
		var wg sync.WaitGroup
		pubResult := make(chan error, 1)
		clearResult := make(chan error, 1)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := service.PublishLearningReport(ctx, lease, nil, versions, allowed)
			pubResult <- err
		}()
		go func() {
			defer wg.Done()
			_, err := service.ClearLearningReports(ctx, owner, bankID)
			clearResult <- err
		}()
		wg.Wait()
		if err := <-clearResult; err != nil {
			t.Fatalf("clear race: %v", err)
		}
		if err := <-pubResult; err != nil && !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
			t.Fatalf("unexpected publish failure: %v", err)
		}
		var reports, jobs int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1`, owner).Scan(&reports); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1`, owner).Scan(&jobs); err != nil || reports != 0 || jobs != 0 {
			t.Fatalf("late result survived cleanup: %d/%d %v", reports, jobs, err)
		}
	}
}

func TestLearningPublishRechecksAfterSlowEntitlementAndContentWithdrawal(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-publish-recheck")
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_catalogs SET enabled=false WHERE bank_id=$1`, bankID); err != nil {
		t.Fatal(err)
	}
	allowed := func(context.Context, uuid.UUID) (bool, error) { return true, nil }
	if _, err := service.PublishLearningReport(ctx, lease, nil, versions, allowed); !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("withdrawn content published: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_catalogs SET enabled=true WHERE bank_id=$1`, bankID); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	checker := func(ctx context.Context, _ uuid.UUID) (bool, error) {
		close(entered)
		select {
		case <-release:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go func() {
		_, err := service.PublishLearningReport(checkCtx, lease, nil, versions, checker)
		finished <- err
	}()
	<-entered
	// A slow external check must not hold the preference row. Clear can win;
	// once the checker returns, the old lease must not resurrect a report.
	clearDone := make(chan error, 1)
	go func() { _, err := service.ClearLearningReports(checkCtx, owner, bankID); clearDone <- err }()
	select {
	case err := <-clearDone:
		if err != nil {
			t.Fatalf("clear while entitlement pending: %v", err)
		}
	case <-checkCtx.Done():
		close(release)
		t.Fatal("entitlement callback held DB lock")
	}
	close(release)
	if err := <-finished; !errors.Is(err, quizcraft.ErrLearningLeaseLost) {
		t.Fatalf("late checked result survived clear: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1`, owner).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("late result = %d %v", remaining, err)
	}
}
