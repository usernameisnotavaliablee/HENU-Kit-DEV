package tests

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

func newLearningWriteHandler(t *testing.T, pool *pgxpool.Pool, client *quizcraft.LearningEntitlementClient, versions quizcraft.LearningJobVersions, writesDisabled bool) *httptest.Server {
	t.Helper()
	handler, err := quizcraft.NewPracticeHTTP(quizcraft.PracticeHTTPConfig{
		Database:              pool,
		AuthHMACSecret:        []byte(practiceAuthSecret),
		LearningEntitlement:   client,
		LearningVersions:      versions,
		CatalogClientID:       portalCatalogClientID,
		CatalogKeys:           map[string]string{portalCatalogKeyID: portalCatalogSecret},
		PortalCommandsEnabled: true,
		PortalCommandClientID: portalCommandClientID,
		PortalCommandKeys:     map[string]string{portalCommandKeyID: portalCommandSecret},
		WritesDisabled:        writesDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func learningPreferencesBody(enabled, consent bool) []byte {
	return mustJSON(map[string]any{
		"enabled":                   enabled,
		"external_analysis_consent": consent,
		"interval_days":             7,
		"goal":                      "follow_course",
		"chapter_ids":               []string{},
	})
}

func learningReportPath(baseURL, bankID string) string {
	return baseURL + "/api/v1/portal/practice/banks/" + bankID + "/learning-reports"
}

func TestLearningReportWritesStayDarkWithoutEntitlementBoundary(t *testing.T) {
	pool, _, owner, bankID, versions := newLearningLeaseTest(t, "learning-write-dark")
	server := newLearningWriteHandler(t, pool, nil, versions, false)
	base := learningReportPath("", bankID.String())

	cases := []struct {
		method string
		path   string
		body   []byte
		key    string
	}{
		{http.MethodPut, base + "/preferences", learningPreferencesBody(false, false), "learning-dark-prefs-0001"},
		{http.MethodPost, base, nil, "learning-dark-request-0001"},
		{http.MethodDelete, base, nil, "learning-dark-clear-0001"},
		{http.MethodPost, learningPracticeSessionPath(bankID.String(), uuid.NewString()), nil, "learning-dark-session-0001"},
	}
	for _, item := range cases {
		request := newPortalPracticeCommandRequest(t, item.method, server.URL, item.path, item.body, owner.String(), item.key)
		status, body, _ := sendPortalPracticeCommand(t, request)
		if status != http.StatusNotFound {
			t.Fatalf("%s %s must stay unregistered, got %d %s", item.method, item.path, status, body)
		}
	}
}

func TestLearningReportWritesRespectCutoverAndLiveMembership(t *testing.T) {
	pool, _, owner, bankID, versions := newLearningLeaseTest(t, "learning-write-scope")
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	revokedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return false, http.StatusOK })
	base := learningReportPath("", bankID.String())

	disabled := newLearningWriteHandler(t, pool, allowedClient, versions, true)
	request := newPortalPracticeCommandRequest(t, http.MethodPut, disabled.URL, base+"/preferences", learningPreferencesBody(false, false), owner.String(), "learning-writes-disabled-0001")
	if status, body, _ := sendPortalPracticeCommand(t, request); status != http.StatusServiceUnavailable || !bytes.Contains(body, []byte(`"code":"writes_disabled"`)) {
		t.Fatalf("writes disabled = %d %s", status, body)
	}

	revoked := newLearningWriteHandler(t, pool, revokedClient, versions, false)
	enable := newPortalPracticeCommandRequest(t, http.MethodPut, revoked.URL, base+"/preferences", learningPreferencesBody(true, true), owner.String(), "learning-revoked-enable-0001")
	if status, body, _ := sendPortalPracticeCommand(t, enable); status != http.StatusForbidden {
		t.Fatalf("revoked enable = %d %s", status, body)
	}
	generate := newPortalPracticeCommandRequest(t, http.MethodPost, revoked.URL, base, nil, owner.String(), "learning-revoked-request-0001")
	if status, body, _ := sendPortalPracticeCommand(t, generate); status != http.StatusForbidden {
		t.Fatalf("revoked generation = %d %s", status, body)
	}
	disable := newPortalPracticeCommandRequest(t, http.MethodPut, revoked.URL, base+"/preferences", learningPreferencesBody(false, false), owner.String(), "learning-revoked-disable-0001")
	if status, body, _ := sendPortalPracticeCommand(t, disable); status != http.StatusOK {
		t.Fatalf("owner must still be able to opt out after revocation: %d %s", status, body)
	}
	clear := newPortalPracticeCommandRequest(t, http.MethodDelete, revoked.URL, base, nil, owner.String(), "learning-revoked-clear-0001")
	if status, body, _ := sendPortalPracticeCommand(t, clear); status != http.StatusOK {
		t.Fatalf("owner must still be able to clear after revocation: %d %s", status, body)
	}
}

func TestLearningReportPreferencesWriteIsValidatedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	pool, _, owner, bankID, versions := newLearningLeaseTest(t, "learning-write-prefs")
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningWriteHandler(t, pool, allowedClient, versions, false)
	path := learningReportPath("", bankID.String()) + "/preferences"

	unknownBody := []byte(`{"enabled":false,"external_analysis_consent":false,"interval_days":7,"goal":"follow_course","chapter_ids":[],"surprise":1}`)
	unknown := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, path, unknownBody, owner.String(), "learning-prefs-unknown-0001")
	if status, body, _ := sendPortalPracticeCommand(t, unknown); status != http.StatusBadRequest {
		t.Fatalf("unknown field = %d %s", status, body)
	}
	noConsent := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, path, learningPreferencesBody(true, false), owner.String(), "learning-prefs-consent-0001")
	if status, body, _ := sendPortalPracticeCommand(t, noConsent); status != http.StatusBadRequest {
		t.Fatalf("enabling without consent = %d %s", status, body)
	}

	var before int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM quizcraft_learning_report_preferences WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	body := learningPreferencesBody(true, true)
	first := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, path, body, owner.String(), "learning-prefs-enable-0001")
	firstStatus, firstBody, _ := sendPortalPracticeCommand(t, first)
	if firstStatus != http.StatusOK {
		t.Fatalf("enable = %d %s", firstStatus, firstBody)
	}
	var stored contract.LearningReportPreferences
	decodeLearningEnvelope(t, firstBody, &stored)
	// Re-applying the stored settings is a no-op: the same values with the same
	// interval must not bump the invalidation revision.
	if !stored.Enabled || !stored.ExternalAnalysisConsent || stored.IntervalDays != 7 || stored.Revision != before || stored.BankId != bankID {
		t.Fatalf("stored preferences = %+v", stored)
	}

	replay := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, path, body, owner.String(), "learning-prefs-enable-0001")
	replayStatus, replayBody, _ := sendPortalPracticeCommand(t, replay)
	if replayStatus != http.StatusOK || !bytes.Equal(firstBody, replayBody) {
		t.Fatalf("replay must return the stored response: %d %s", replayStatus, replayBody)
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM quizcraft_learning_report_preferences WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&revision); err != nil || revision != before {
		t.Fatalf("replay must not bump the revision: %d %v", revision, err)
	}

	conflict := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, path, learningPreferencesBody(false, false), owner.String(), "learning-prefs-enable-0001")
	if status, respBody, _ := sendPortalPracticeCommand(t, conflict); status != http.StatusConflict || !bytes.Contains(respBody, []byte(`"code":"idempotency_conflict"`)) {
		t.Fatalf("same key with another body = %d %s", status, respBody)
	}
	missingKey := newPortalPracticeCommandRequest(t, http.MethodPut, server.URL, path, body, owner.String(), "")
	if status, respBody, _ := sendPortalPracticeCommand(t, missingKey); status != http.StatusBadRequest {
		t.Fatalf("missing idempotency key = %d %s", status, respBody)
	}
}

func TestLearningReportManualRequestQueuesOnceAndReusesWork(t *testing.T) {
	ctx := context.Background()
	pool, _, owner, bankID, versions := newLearningLeaseTest(t, "learning-write-request")
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningWriteHandler(t, pool, allowedClient, versions, false)
	path := learningReportPath("", bankID.String())

	first := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-request-first-0001")
	firstStatus, firstBody, _ := sendPortalPracticeCommand(t, first)
	if firstStatus != http.StatusAccepted {
		t.Fatalf("new manual request = %d %s", firstStatus, firstBody)
	}
	var task contract.LearningReportTask
	decodeLearningEnvelope(t, firstBody, &task)
	if task.TaskId == uuid.Nil || task.Status != "queued" || task.BankId != bankID {
		t.Fatalf("queued task = %+v", task)
	}

	second := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-request-second-0001")
	secondStatus, secondBody, _ := sendPortalPracticeCommand(t, second)
	if secondStatus != http.StatusOK {
		t.Fatalf("identical input must reuse the task = %d %s", secondStatus, secondBody)
	}
	var reused contract.LearningReportTask
	decodeLearningEnvelope(t, secondBody, &reused)
	if reused.TaskId != task.TaskId {
		t.Fatalf("reused task id = %s, want %s", reused.TaskId, task.TaskId)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("manual requests must not pile up jobs: %d %v", jobs, err)
	}

	unconfigured := newLearningWriteHandler(t, pool, allowedClient, quizcraft.LearningJobVersions{}, false)
	request := newPortalPracticeCommandRequest(t, http.MethodPost, unconfigured.URL, path, nil, owner.String(), "learning-request-unconfigured-0001")
	if status, body, _ := sendPortalPracticeCommand(t, request); status != http.StatusServiceUnavailable {
		t.Fatalf("generation without a worker model = %d %s", status, body)
	}
}

func TestLearningReportClearIsOwnerScopedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-write-clear")
	other := uuid.NewSHA1(uuid.NameSpaceOID, []byte("learning-write-clear-other:"+bankID.String()))
	if _, err := service.UpdateLearningReportPreferences(ctx, other, bankID, contract.LearningReportPreferencesUpdate{Enabled: true, ExternalAnalysisConsent: true, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	publishLearningTestReport(t, service, other, bankID, versions)
	publishLearningTestReport(t, service, owner, bankID, versions)

	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningWriteHandler(t, pool, allowedClient, versions, false)
	path := learningReportPath("", bankID.String())

	clear := newPortalPracticeCommandRequest(t, http.MethodDelete, server.URL, path, nil, owner.String(), "learning-clear-owner-0001")
	clearStatus, clearBody, _ := sendPortalPracticeCommand(t, clear)
	if clearStatus != http.StatusOK {
		t.Fatalf("clear = %d %s", clearStatus, clearBody)
	}
	var result contract.LearningReportClearResult
	decodeLearningEnvelope(t, clearBody, &result)
	if !result.Cleared || result.Revision < 2 {
		t.Fatalf("clear result = %+v", result)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("owner reports must be gone: %d %v", remaining, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, other, bankID).Scan(&remaining); err != nil || remaining == 0 {
		t.Fatalf("another owner's report must survive: %d %v", remaining, err)
	}
	replay := newPortalPracticeCommandRequest(t, http.MethodDelete, server.URL, path, nil, owner.String(), "learning-clear-owner-0001")
	replayStatus, replayBody, _ := sendPortalPracticeCommand(t, replay)
	if replayStatus != http.StatusOK || !bytes.Equal(clearBody, replayBody) {
		t.Fatalf("clear replay must return the stored response: %d %s", replayStatus, replayBody)
	}
	// The owner's derived data stays gone after a replay.
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("replay must not resurrect derived data: %d %v", remaining, err)
	}
}

// publishLearningTestReport gives one owner a published report so clear and read
// behaviour can be checked against real rows.
func publishLearningTestReport(t *testing.T, service *quizcraft.Service, owner, bankID uuid.UUID, versions quizcraft.LearningJobVersions) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	if lease.UserID != owner {
		t.Fatalf("claimed another owner's job: %s", lease.UserID)
	}
	if _, err := service.PublishLearningReport(ctx, lease, nil, versions, func(_ context.Context, id uuid.UUID) (bool, error) { return id == owner, nil }); err != nil {
		t.Fatal(err)
	}
}
