package tests

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

func newLearningLimitedWriteHandler(t *testing.T, pool *pgxpool.Pool, client *quizcraft.LearningEntitlementClient, versions quizcraft.LearningJobVersions, limit int) *httptest.Server {
	t.Helper()
	handler, err := quizcraft.NewPracticeHTTP(quizcraft.PracticeHTTPConfig{
		Database:              pool,
		AuthHMACSecret:        []byte(practiceAuthSecret),
		LearningEntitlement:   client,
		LearningVersions:      versions,
		LearningManualLimit:   limit,
		CatalogClientID:       portalCatalogClientID,
		CatalogKeys:           map[string]string{portalCatalogKeyID: portalCatalogSecret},
		PortalCommandsEnabled: true,
		PortalCommandClientID: portalCommandClientID,
		PortalCommandKeys:     map[string]string{portalCommandKeyID: portalCommandSecret},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// backdateLearningJob writes one stored generation that is older than the
// guard window. created_at is immutable once written, so a fixture is the only
// way to place history behind the window.
func backdateLearningJob(t *testing.T, pool *pgxpool.Pool, owner, bankID uuid.UUID, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	var contentVersion uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT active_content_version_id FROM quizcraft_learning_catalogs WHERE bank_id=$1`, bankID).Scan(&contentVersion); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_report_jobs
        (id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot,status,created_at)
        SELECT $1,$2,$3,revision,$4,$5,'{}'::jsonb,'queued',now()-make_interval(secs=>$6)
        FROM quizcraft_learning_report_preferences WHERE user_id=$2 AND bank_id=$3`,
		uuid.New(), owner, bankID, contentVersion, digest, age.Seconds()); err != nil {
		t.Fatal(err)
	}
}

// The manual-entry guard is abuse protection, not a quota: it must cap how much
// model work one member can buy for one course, never break replay of an
// existing request, never count history outside its window, and never touch
// scheduled generation.
func TestLearningManualGenerationGuardCapsCostWithoutBreakingReplay(t *testing.T) {
	ctx := context.Background()
	pool, _, owner, bankID, versions := newLearningLeaseTest(t, "learning-limit-guard")
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningLimitedWriteHandler(t, pool, allowedClient, versions, 1)
	path := learningReportPath("", bankID.String())
	preferencesPath := path + "/preferences"

	// A generation from two hours ago is outside the window: it must not consume
	// the budget of a fresh request.
	backdateLearningJob(t, pool, owner, bankID, 2*time.Hour)

	first := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-limit-first-0001")
	firstStatus, firstBody, _ := sendPortalPracticeCommand(t, first)
	if firstStatus != http.StatusAccepted {
		t.Fatalf("first request inside a clear window = %d %s", firstStatus, firstBody)
	}
	var task contract.LearningReportTask
	decodeLearningEnvelope(t, firstBody, &task)

	// Replaying the stored key must return the stored answer even though the
	// member is already at the cap: the retry reuses its own job for free.
	replay := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-limit-first-0001")
	if status, body, _ := sendPortalPracticeCommand(t, replay); status != http.StatusAccepted || !bytes.Equal(firstBody, body) {
		t.Fatalf("replay at the cap = %d %s", status, body)
	}

	// A new revision is new model work, so it is what the guard must refuse.
	change := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, preferencesPath,
		mustJSON(map[string]any{"enabled": true, "external_analysis_consent": true, "interval_days": 7, "goal": "follow_course", "chapter_ids": []string{"ch01"}}),
		owner.String(), "learning-limit-change-0001")
	if status, body, _ := sendPortalPracticeCommand(t, change); status != http.StatusOK {
		t.Fatalf("scope change = %d %s", status, body)
	}
	second := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-limit-second-0001")
	secondStatus, secondBody, _ := sendPortalPracticeCommand(t, second)
	if secondStatus != http.StatusTooManyRequests || !bytes.Contains(secondBody, []byte(`"code":"rate_limited"`)) {
		t.Fatalf("second generation over the cap = %d %s", secondStatus, secondBody)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("a refused request must not store a job: %d %v", jobs, err)
	}

	// The guard is configuration: an operator can turn it off explicitly.
	open := newLearningLimitedWriteHandler(t, pool, allowedClient, versions, 0)
	third := newPortalPracticeCommandRequest(t, http.MethodPost, open.URL, path, nil, owner.String(), "learning-limit-open-0001")
	if status, body, _ := sendPortalPracticeCommand(t, third); status != http.StatusAccepted {
		t.Fatalf("disabled guard = %d %s", status, body)
	}

	// Scheduled work is not the member's abuse surface: it must keep running
	// with the same budget already spent.
	limited, err := quizcraft.New(quizcraft.Config{Database: pool, LearningManualLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=now()-interval '1 minute' WHERE user_id=$1 AND bank_id=$2`, owner, bankID); err != nil {
		t.Fatal(err)
	}
	automatic := versions
	automatic.Prompt = "prompt-limit-scheduler"
	if _, _, err := limited.QueueLearningReport(ctx, owner, bankID, time.Now(), "automatic", automatic); err != nil {
		t.Fatalf("automatic generation must not be rate limited: %v", err)
	}
	// A different (never seen) input is genuine new model work, so the guard
	// must refuse it even though the scheduler just queued its own job.
	manual := automatic
	manual.Model = "synthetic-model-limit"
	if _, _, err := limited.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", manual); err == nil {
		t.Fatal("manual generation above the cap was allowed")
	}
}

// Concurrent identical requests are retries, not extra work: they must all be
// answered from one stored job and none of them may be refused by the guard.
func TestLearningManualGenerationGuardDoesNotRefuseConcurrentRetries(t *testing.T) {
	pool, _, owner, bankID, versions := newLearningLeaseTest(t, "learning-limit-concurrent")
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningLimitedWriteHandler(t, pool, allowedClient, versions, 1)
	path := learningReportPath("", bankID.String())

	const workers = 5
	requests := make([]*http.Request, workers)
	for index := 0; index < workers; index++ {
		// Build every request on the test goroutine: the helper reports failures
		// through t.Fatal, which must not run inside a worker.
		requests[index] = newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-limit-race-000"+string(rune('1'+index)))
	}
	statuses := make([]int, workers)
	bodies := make([][]byte, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			statuses[index], bodies[index], _ = sendPortalPracticeCommand(t, requests[index])
		}(index)
	}
	group.Wait()

	var accepted int
	for index, status := range statuses {
		switch status {
		case http.StatusAccepted, http.StatusOK:
			accepted++
		default:
			t.Fatalf("concurrent retry %d = %d %s", index, status, bodies[index])
		}
	}
	if accepted != workers {
		t.Fatalf("accepted %d of %d concurrent retries", accepted, workers)
	}
	var jobs, kinds int
	if err := pool.QueryRow(context.Background(), `SELECT count(*),count(DISTINCT input_sha256) FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&jobs, &kinds); err != nil || jobs != 1 || kinds != 1 {
		t.Fatalf("concurrent retries stored %d job(s) with %d input(s): %v", jobs, kinds, err)
	}
}
