package tests

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

// newPortalCatalogGET signs a five-part catalog read; a non-empty actor adds
// the sixth actor-bound line used by the Portal personal boundary. It is the
// same signature scheme as the personal-stats read.
func newPortalCatalogGET(t *testing.T, rawURL, actor string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	nonceText := base64.RawURLEncoding.EncodeToString(nonce)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	digest := sha256.Sum256(nil)
	canonical := strings.Join([]string{http.MethodGet, request.URL.RequestURI(), timestamp, nonceText, hex.EncodeToString(digest[:]), actor}, "\n")
	mac := hmac.New(sha256.New, []byte(portalCatalogSecret))
	_, _ = mac.Write([]byte(canonical))
	request.SetBasicAuth(portalCatalogClientID, portalCatalogSecret)
	request.Header.Set("X-Service-Id", portalCatalogClientID)
	request.Header.Set("X-Key-Id", portalCatalogKeyID)
	request.Header.Set("X-Timestamp", timestamp)
	request.Header.Set("X-Nonce", nonceText)
	request.Header.Set("X-Signature", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set("X-Permission-Code", "portal.practice.read")
	request.Header.Set("X-Scope-Kind", "product")
	request.Header.Set("X-Product-Code", "quizcraft")
	request.Header.Set("X-Actor-User-Id", actor)
	request.Header.Set("X-Request-Id", "req_learning_reads_test")
	return request
}

func learningReadPaths(bankID string) []string {
	return []string{
		"/api/v1/portal/practice/banks/" + bankID + "/learning-reports/preferences",
		"/api/v1/portal/practice/banks/" + bankID + "/learning-reports/latest",
		"/api/v1/portal/practice/banks/" + bankID + "/learning-reports/tasks/" + uuid.NewString(),
	}
}

func newLearningReadsHandler(t *testing.T, pool *pgxpool.Pool, client *quizcraft.LearningEntitlementClient, withCatalog bool) *httptest.Server {
	t.Helper()
	config := quizcraft.PracticeHTTPConfig{Database: pool, AuthHMACSecret: []byte(practiceAuthSecret), LearningEntitlement: client}
	if withCatalog {
		config.CatalogClientID = portalCatalogClientID
		config.CatalogKeys = map[string]string{portalCatalogKeyID: portalCatalogSecret}
	}
	handler, err := quizcraft.NewPracticeHTTP(config)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func decodeLearningEnvelope(t *testing.T, raw []byte, target any) {
	t.Helper()
	var envelope struct {
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.RequestID == "" {
		t.Fatalf("learning read returned a malformed envelope: %s", raw)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("learning read data is malformed: %v (%s)", err, raw)
	}
}

func TestLearningReportReadsStayDarkWithoutBothSignedBoundaries(t *testing.T) {
	pool := practicePool(t)
	bankID := uuid.NewString()
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	for name, server := range map[string]*httptest.Server{
		"no entitlement client": newLearningReadsHandler(t, pool, nil, true),
		"no catalog boundary":   newLearningReadsHandler(t, pool, allowedClient, false),
	} {
		t.Run(name, func(t *testing.T) {
			for _, path := range learningReadPaths(bankID) {
				status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, server.URL+path, uuid.NewString()))
				if status != http.StatusNotFound {
					t.Fatalf("%s must stay unregistered, got %d", path, status)
				}
			}
		})
	}
}

func TestLearningReportReadsRequireSignedActorAndLiveLifetime(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, _ := newLearningLeaseTest(t, "learning-http-scope")
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningReadsHandler(t, pool, allowedClient, true)
	latestPath := "/api/v1/portal/practice/banks/" + bankID.String() + "/learning-reports/latest"
	preferencesPath := "/api/v1/portal/practice/banks/" + bankID.String() + "/learning-reports/preferences"
	latest := server.URL + latestPath

	unsigned := newPortalCatalogGET(t, latest, owner.String())
	unsigned.Header.Del("X-Actor-User-Id")
	if status, _ := sendCatalogRequest(t, unsigned); status != http.StatusUnauthorized {
		t.Fatalf("unsigned report read = %d, want 401", status)
	}
	tampered := newPortalCatalogGET(t, latest, owner.String())
	tampered.Header.Set("X-Actor-User-Id", uuid.NewString())
	if status, _ := sendCatalogRequest(t, tampered); status != http.StatusUnauthorized {
		t.Fatalf("tampered actor = %d, want 401", status)
	}

	revokedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return false, http.StatusOK })
	revoked := newLearningReadsHandler(t, pool, revokedClient, true)
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, revoked.URL+latestPath, owner.String())); status != http.StatusForbidden {
		t.Fatalf("revoked member report read = %d, want 403", status)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, revoked.URL+preferencesPath, owner.String())); status != http.StatusOK {
		t.Fatalf("owner preferences must stay readable after revocation, got %d", status)
	}

	failingClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return false, http.StatusServiceUnavailable })
	failing := newLearningReadsHandler(t, pool, failingClient, true)
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, failing.URL+latestPath, owner.String())); status != http.StatusServiceUnavailable {
		t.Fatalf("entitlement dependency error = %d, want 503", status)
	}

	// A live member still gets 404, not 403, when nothing was generated yet.
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: false, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, latest, owner.String())); status != http.StatusNotFound {
		t.Fatalf("member without a report = %d, want 404", status)
	}
}

func TestLearningReportReadsServeOnlyTheOwnersContractShape(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-http-owner")
	task, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions)
	if err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	report, err := service.PublishLearningReport(ctx, lease, nil, versions, func(_ context.Context, id uuid.UUID) (bool, error) { return id == owner, nil })
	if err != nil {
		t.Fatal(err)
	}
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningReadsHandler(t, pool, allowedClient, true)
	base := server.URL + "/api/v1/portal/practice/banks/" + bankID.String() + "/learning-reports"

	status, raw := sendCatalogRequest(t, newPortalCatalogGET(t, base+"/latest", owner.String()))
	if status != http.StatusOK {
		t.Fatalf("owner latest = %d %s", status, raw)
	}
	envelope := map[string]any{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	schema, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Components.Schemas["LearningReportEnvelope"].Value.VisitJSON(envelope); err != nil {
		t.Fatalf("latest report envelope violates contract: %v", err)
	}
	var served contract.LearningReport
	decodeLearningEnvelope(t, raw, &served)
	if served.ReportId != report.ReportId || served.BankId != bankID {
		t.Fatalf("served report is not the published one: %+v", served)
	}

	status, raw = sendCatalogRequest(t, newPortalCatalogGET(t, base+"/tasks/"+task.TaskId.String(), owner.String()))
	if status != http.StatusOK {
		t.Fatalf("owner task = %d %s", status, raw)
	}
	var servedTask contract.LearningReportTask
	decodeLearningEnvelope(t, raw, &servedTask)
	if servedTask.TaskId != task.TaskId || servedTask.Status != "ready" || servedTask.ReportId == nil || *servedTask.ReportId != report.ReportId {
		t.Fatalf("served task = %+v", servedTask)
	}

	status, raw = sendCatalogRequest(t, newPortalCatalogGET(t, base+"/preferences", owner.String()))
	if status != http.StatusOK {
		t.Fatalf("owner preferences = %d %s", status, raw)
	}
	var stored contract.LearningReportPreferences
	decodeLearningEnvelope(t, raw, &stored)
	if !stored.Enabled || !stored.ExternalAnalysisConsent || stored.BankId != bankID {
		t.Fatalf("served preferences = %+v", stored)
	}

	other := uuid.NewString()
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, base+"/latest", other)); status != http.StatusNotFound {
		t.Fatalf("another owner read a report: %d", status)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, base+"/tasks/"+task.TaskId.String(), other)); status != http.StatusNotFound {
		t.Fatalf("another owner read a task: %d", status)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, base+"/tasks/not-a-uuid", owner.String())); status != http.StatusBadRequest {
		t.Fatalf("malformed task id = %d, want 400", status)
	}
	unknown := server.URL + "/api/v1/portal/practice/banks/" + uuid.NewString() + "/learning-reports/latest"
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, unknown, owner.String())); status != http.StatusNotFound {
		t.Fatalf("unknown bank = %d, want 404", status)
	}
}

func TestLearningLatestReportNeverSubstitutesAStaleReport(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-http-stale")
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	if _, err := service.PublishLearningReport(ctx, lease, nil, versions, func(_ context.Context, id uuid.UUID) (bool, error) { return id == owner, nil }); err != nil {
		t.Fatal(err)
	}
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningReadsHandler(t, pool, allowedClient, true)
	latest := server.URL + "/api/v1/portal/practice/banks/" + bankID.String() + "/learning-reports/latest"
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, latest, owner.String())); status != http.StatusOK {
		t.Fatalf("published report should be readable, got %d", status)
	}
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: false, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&stored); err != nil || stored != "stale" {
		t.Fatalf("preference change must stale the report: %q %v", stored, err)
	}
	status, raw := sendCatalogRequest(t, newPortalCatalogGET(t, latest, owner.String()))
	if status != http.StatusNotFound {
		t.Fatalf("stale report served as latest: %d %s", status, raw)
	}
}

func TestLearningLatestReportRequiresStillApprovedContent(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-http-content")
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	if _, err := service.PublishLearningReport(ctx, lease, nil, versions, func(_ context.Context, id uuid.UUID) (bool, error) { return id == owner, nil }); err != nil {
		t.Fatal(err)
	}
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningReadsHandler(t, pool, allowedClient, true)
	latest := server.URL + "/api/v1/portal/practice/banks/" + bankID.String() + "/learning-reports/latest"
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, latest, owner.String())); status != http.StatusOK {
		t.Fatalf("approved content report = %d, want 200", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_content_versions SET status='retired' WHERE bank_id=$1`, bankID); err != nil {
		t.Fatal(err)
	}
	var reportStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&reportStatus); err != nil || reportStatus == "stale" {
		t.Fatalf("test premise lost: report row is %q %v", reportStatus, err)
	}
	status, raw := sendCatalogRequest(t, newPortalCatalogGET(t, latest, owner.String()))
	if status != http.StatusNotFound {
		t.Fatalf("report for retired content served: %d %s", status, raw)
	}
}
