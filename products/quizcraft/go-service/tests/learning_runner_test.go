package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
)

func learningRunnerClient(t *testing.T, lifetime func(int32) (bool, int)) (*quizcraft.LearningEntitlementClient, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		_, secret, ok := r.BasicAuth()
		if !ok || secret != "independent-learning-runner-secret-32bytes" || r.Header.Get("X-Signature") == "" {
			t.Error("runner skipped signed live owner check")
		}
		allowed, code := lifetime(count)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"data":{"lifetime":%t,"version":%d},"request_id":"req_runner"}`, allowed, 1)
	}))
	t.Cleanup(server.Close)
	client, err := quizcraft.NewLearningEntitlementClient(quizcraft.LearningEntitlementClientConfig{BaseURL: server.URL, ClientID: "quizcraft-learning", KeyID: "runner-key", Secret: "independent-learning-runner-secret-32bytes"})
	if err != nil {
		t.Fatal(err)
	}
	return client, calls
}

func TestLearningRunnerChecksOwnerBeforeAnyModelWork(t *testing.T) {
	for _, trial := range []struct {
		name, wantStatus string
		code             int
	}{
		{name: "revoked", code: http.StatusOK, wantStatus: "cancelled"},
		{name: "dependency down", code: http.StatusServiceUnavailable, wantStatus: "paused"},
	} {
		t.Run(trial.name, func(t *testing.T) {
			ctx := context.Background()
			pool, service, owner, bankID, versions := newLearningLeaseTest(t, "runner-"+trial.wantStatus)
			task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
			if err != nil {
				t.Fatal(err)
			}
			client, calls := learningRunnerClient(t, func(int32) (bool, int) { return false, trial.code })
			modelCalls := 0
			provider := func(context.Context, quizcraft.LearningModelInput, quizcraft.LearningJobVersions) ([]byte, error) {
				modelCalls++
				return nil, errors.New("provider must never be called")
			}
			processed, err := service.ProcessNextLearningReport(ctx, client, provider, versions, time.Minute)
			if !processed || !errors.Is(err, quizcraft.ErrLearningUnavailable) || modelCalls != 0 || calls.Load() != 1 {
				t.Fatalf("runner result %t %v, model=%d, checks=%d", processed, err, modelCalls, calls.Load())
			}
			var status, reason string
			if err := pool.QueryRow(ctx, `SELECT status,reason_code FROM quizcraft_learning_report_jobs WHERE id=$1`, task.TaskId).Scan(&status, &reason); err != nil || status != trial.wantStatus || reason == "" {
				t.Fatalf("job status/reason = %s/%s %v", status, reason, err)
			}
			var reports int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE job_id=$1`, task.TaskId).Scan(&reports); err != nil || reports != 0 {
				t.Fatalf("unauthorized report count = %d %v", reports, err)
			}
		})
	}
}

func TestLearningRunnerColdStartUsesNoProviderAndChecksAgainToPublish(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "runner-cold")
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	client, calls := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	if done, err := service.ProcessNextLearningReport(ctx, client, nil, versions, time.Minute); done || !errors.Is(err, quizcraft.ErrLearningInvalidJob) || calls.Load() != 0 {
		t.Fatalf("missing provider claimed work: %t %v checks=%d", done, err, calls.Load())
	}
	provider := func(context.Context, quizcraft.LearningModelInput, quizcraft.LearningJobVersions) ([]byte, error) {
		t.Error("model called for cold start")
		return nil, nil
	}
	processed, err := service.ProcessNextLearningReport(ctx, client, provider, versions, time.Minute)
	if err != nil || !processed || calls.Load() != 2 {
		t.Fatalf("cold result %t %v; entitlement checks %d", processed, err, calls.Load())
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_report_jobs WHERE id=$1`, task.TaskId).Scan(&status); err != nil || status != "ready" {
		t.Fatalf("cold job status = %s %v", status, err)
	}
}

func queueRunnerEvidence(t *testing.T, pool *pgxpool.Pool, service *quizcraft.Service, owner, bankID uuid.UUID, versions quizcraft.LearningJobVersions) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	// Give the job real answer evidence to force the provider path.
	var bankVersion, questionID, questionVersion uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT b.active_version_id,q.question_id,q.id FROM quizcraft_banks b
        JOIN quizcraft_bank_version_questions m ON m.bank_id=b.id AND m.bank_version_id=b.active_version_id
        JOIN quizcraft_question_versions q ON q.bank_id=b.id AND q.id=m.question_version_id
        WHERE b.id=$1 AND q.type='single' ORDER BY q.question_id LIMIT 1`, bankID).Scan(&bankVersion, &questionID, &questionVersion); err != nil {
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
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	return task.TaskId
}

func TestLearningRunnerFailsClosedWhenMemberRevokedDuringProvider(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "runner-revoke")
	taskID := queueRunnerEvidence(t, pool, service, owner, bankID, versions)
	client, calls := learningRunnerClient(t, func(count int32) (bool, int) { return count == 1, http.StatusOK })
	modelCalls := 0
	provider := func(_ context.Context, input quizcraft.LearningModelInput, current quizcraft.LearningJobVersions) ([]byte, error) {
		modelCalls++
		if current != versions || len(input.Evidence) != 1 || input.Evidence[0].EvidenceID == "" {
			t.Errorf("unscoped provider payload: %+v", input)
		}
		return []byte(`{"findings":[],"primary_tag_id":""}`), nil
	}
	processed, err := service.ProcessNextLearningReport(ctx, client, provider, versions, time.Minute)
	if !processed || !errors.Is(err, quizcraft.ErrLearningUnavailable) || modelCalls != 1 || calls.Load() != 2 {
		t.Fatalf("revoked during model: %t %v, provider=%d, checks=%d", processed, err, modelCalls, calls.Load())
	}
	var status, reason string
	if err := pool.QueryRow(ctx, `SELECT status,reason_code FROM quizcraft_learning_report_jobs WHERE id=$1`, taskID).Scan(&status, &reason); err != nil || status != "cancelled" || reason != "entitlement_revoked" {
		t.Fatalf("late revoked job = %s/%s %v", status, reason, err)
	}
}

func TestLearningRunnerPublishesOnlyValidatedModelDecision(t *testing.T) {
	for _, trial := range []struct {
		name, decision, wantStatus string
	}{
		{name: "valid", decision: `{"findings":[],"primary_tag_id":""}`, wantStatus: "ready"},
		{name: "forged", decision: `{"findings":[{"tag_id":"math","status":"supported","evidence_ids":["forged"]}],"primary_tag_id":"math"}`, wantStatus: "queued"},
	} {
		t.Run(trial.name, func(t *testing.T) {
			ctx := context.Background()
			pool, service, owner, bankID, versions := newLearningLeaseTest(t, "runner-model-"+trial.name)
			taskID := queueRunnerEvidence(t, pool, service, owner, bankID, versions)
			client, checks := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
			modelCalls := 0
			provider := func(_ context.Context, input quizcraft.LearningModelInput, current quizcraft.LearningJobVersions) ([]byte, error) {
				modelCalls++
				if len(input.Evidence) != 1 || input.Evidence[0].EvidenceID == "" || current != versions {
					t.Errorf("unscoped model input: %+v", input)
				}
				return []byte(trial.decision), nil
			}
			processed, err := service.ProcessNextLearningReport(ctx, client, provider, versions, time.Minute)
			if !processed || modelCalls != 1 {
				t.Fatalf("provider work: %t %v, calls=%d", processed, err, modelCalls)
			}
			var status, reason string
			if dbErr := pool.QueryRow(ctx, `SELECT status,reason_code FROM quizcraft_learning_report_jobs WHERE id=$1`, taskID).Scan(&status, &reason); dbErr != nil || status != trial.wantStatus {
				t.Fatalf("job = %s/%s %v", status, reason, dbErr)
			}
			var count int
			if dbErr := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE job_id=$1`, taskID).Scan(&count); dbErr != nil {
				t.Fatal(dbErr)
			}
			if trial.name == "valid" {
				if err != nil || checks.Load() != 3 || count != 1 {
					t.Fatalf("valid decision not published: %v checks=%d reports=%d", err, checks.Load(), count)
				}
			} else if !errors.Is(err, quizcraft.ErrLearningUnavailable) || checks.Load() != 1 || count != 0 || reason != "invalid_model_result" {
				t.Fatalf("forged decision published: %v checks=%d reports=%d reason=%s", err, checks.Load(), count, reason)
			}
		})
	}
}
