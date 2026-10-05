package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"henukit.dev/portal-gateway/internal/config"
	"henukit.dev/portal-gateway/internal/practice"
)

const (
	learningReportUserID = "9d1e6a5c-6b1a-4d5f-9f0e-91a0c1b2d3e4"
	learningReportBankID = "10ca9b18-c303-4b7a-ab14-1241e41b665a"
	learningReportID     = "3b7f2c44-9a5e-4f1b-8c2d-0e5a7b9c1d2e"
	learningReportTaskID = "c1d2e3f4-a5b6-4c7d-8e9f-0a1b2c3d4e5f"
)

func newLearningReportsHandler(t *testing.T, platformURL, coreURL string, learningReportsEnabled, v2ReadsEnabled bool) *Handler {
	t.Helper()
	handler, err := New(config.Config{
		SessionKey:                      []byte("0123456789abcdef0123456789abcdef"),
		PlatformCoreURL:                 platformURL,
		PlatformClientID:                "portal-gateway",
		PlatformSecret:                  "portal-client-secret-with-enough-entropy",
		PlatformKeyID:                   "platform-key-1",
		PortalRedirectURI:               "https://portal.test/api/v1/auth/callback",
		PortalOrigin:                    "https://portal.test",
		PortalAPIURL:                    "http://127.0.0.1:9",
		QuizCraftV2ReadsEnabled:         v2ReadsEnabled,
		QuizCraftCoreURL:                coreURL,
		QuizCraftLearningReportsEnabled: learningReportsEnabled,
		QuizCraftCoreAuth: config.ServiceAuth{
			ClientID: "portal-gateway", ClientSecret: "catalog-secret-with-enough-entropy", KeyID: "portal-catalog-key-1",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func learningReportsPath(suffix string) string {
	return "http://portal.test/api/v1/practice/banks/" + learningReportBankID + "/learning-reports" + suffix
}

func newLearningReportsCore(t *testing.T, calls *atomic.Int32, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		if request.Header.Get("X-Actor-User-Id") != learningReportUserID {
			t.Fatalf("Core actor header = %q", request.Header.Get("X-Actor-User-Id"))
		}
		assertActorBoundStatsSignature(t, request, learningReportUserID, "catalog-secret-with-enough-entropy")
		handler(writer, request)
	}))
}

func newLearningReportsPlatform(t *testing.T, calls *atomic.Int32, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/authorization/check" {
			t.Fatalf("Platform Core path = %q", request.URL.Path)
		}
		if calls != nil {
			calls.Add(1)
		}
		writer.WriteHeader(status)
	}))
}

func getLearningReport(t *testing.T, handler *Handler, cookie *http.Cookie, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("X-Request-Id", "req_gateway_learning")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	handler.Router().ServeHTTP(recorder, request)
	return recorder
}

const learningReportBody = `{"request_id":"req_gateway_learning","data":{` +
	`"report_id":"` + learningReportID + `",` +
	`"bank_id":"` + learningReportBankID + `",` +
	`"content_version_id":"8f7e6d5c-4b3a-4291-8877-665544332211",` +
	`"status":"ready","goal":"follow_course",` +
	`"evidence_until":"2026-10-01T00:00:00Z","created_at":"2026-10-02T00:00:00Z",` +
	`"user_id":"` + learningReportUserID + `",` +
	`"statistics":[{"tag_id":"math","tag_kind":"knowledge","label":"算术","attempt_count":4,"unique_question_count":3,"first_correct_count":1,"repeat_attempt_count":1,"repeat_correct_count":0,"latest_correct_count":2}],` +
	`"evidence":[{"evidence_id":"ev_1","question_id":"11111111-2222-4333-8444-555555555555","question_version_id":"66666666-7777-4888-8999-000000000000","submitted_at":"2026-10-01T00:00:00Z","correct":false,"question":"1+1=?","submitted_answer":2,"expected_answer":3}],` +
	`"findings":[{"tag_id":"math","status":"tentative","observation":"两次出现同类错误","possible_reason":"可能是进位不熟","evidence_ids":["ev_1"]}],` +
	`"next_step":{"kind":"practice","reason":"先补算术基础","tag_id":"math","question_ids":["11111111-2222-4333-8444-555555555555"]}},` +
	`"unmodelled_internal_field":"must not reach a browser"}`

func TestLearningReportsStayDarkUntilTheExplicitGateIsOn(t *testing.T) {
	var platformCalls, coreCalls atomic.Int32
	platform := newLearningReportsPlatform(t, &platformCalls, http.StatusOK)
	defer platform.Close()
	core := newLearningReportsCore(t, &coreCalls, func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	defer core.Close()

	paths := []string{learningReportsPath("/preferences"), learningReportsPath("/latest"), learningReportsPath("/tasks/" + learningReportTaskID)}
	for _, test := range []struct {
		name           string
		learning, v2   bool
		wantStatus     int
		wantCoreCalls  int32
		withoutSession bool
	}{
		{name: "gate off stays dark", learning: false, v2: true, wantStatus: http.StatusServiceUnavailable},
		{name: "gate on without the V2 read client stays dark", learning: true, v2: false, wantStatus: http.StatusServiceUnavailable},
		{name: "gate on requires a Portal session", learning: true, v2: true, wantStatus: http.StatusUnauthorized, withoutSession: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := newLearningReportsHandler(t, platform.URL, core.URL, test.learning, test.v2)
			var cookie *http.Cookie
			if !test.withoutSession {
				cookie = sessionCookie(t, handler, learningReportUserID)
			}
			for _, path := range paths {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				if cookie != nil {
					request.AddCookie(cookie)
				}
				recorder := httptest.NewRecorder()
				handler.Router().ServeHTTP(recorder, request)
				if recorder.Code != test.wantStatus {
					t.Fatalf("%s = %d, want %d: %s", path, recorder.Code, test.wantStatus, recorder.Body.String())
				}
				if strings.Contains(recorder.Body.String(), "unmodelled") || strings.Contains(recorder.Body.String(), learningReportID) {
					t.Fatalf("%s served report data while dark: %s", path, recorder.Body.String())
				}
			}
		})
	}
	if platformCalls.Load() != 0 || coreCalls.Load() != 0 {
		t.Fatalf("dark learning reports called dependencies: platform=%d core=%d", platformCalls.Load(), coreCalls.Load())
	}
}

func TestLatestLearningReportServesOnlyTheMirroredOwnerReport(t *testing.T) {
	var coreCalls atomic.Int32
	platform := newLearningReportsPlatform(t, nil, http.StatusOK)
	defer platform.Close()
	core := newLearningReportsCore(t, &coreCalls, func(writer http.ResponseWriter, request *http.Request) {
		expectedPath := strings.Replace(practice.GetPortalLatestLearningReportPath, "{bank_id}", learningReportBankID, 1)
		if request.URL.Path != expectedPath {
			t.Fatalf("Core path = %q, want %q", request.URL.Path, expectedPath)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(learningReportBody))
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	response := getLearningReport(t, handler, sessionCookie(t, handler, learningReportUserID), learningReportsPath("/latest"))
	if response.Code != http.StatusOK {
		t.Fatalf("latest report = %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, "unmodelled_internal_field") || strings.Contains(body, `"user_id"`) {
		t.Fatalf("response leaked an unmodelled Core field: %s", body)
	}
	var envelope practice.LearningReportEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.RequestID != "req_gateway_learning" || envelope.Data.ReportID != learningReportID || envelope.Data.Status != "ready" || envelope.Data.Goal != "follow_course" || len(envelope.Data.Statistics) != 1 || len(envelope.Data.Evidence) != 1 || len(envelope.Data.Findings) != 1 || envelope.Data.NextStep.Kind != "practice" || len(envelope.Data.NextStep.QuestionIDs) != 1 {
		t.Fatalf("latest report = %+v", envelope)
	}
	if coreCalls.Load() != 1 {
		t.Fatalf("core calls = %d", coreCalls.Load())
	}
}

func TestLearningReportReadsPassThroughAnHonestNotFound(t *testing.T) {
	platform := newLearningReportsPlatform(t, nil, http.StatusOK)
	defer platform.Close()
	core := newLearningReportsCore(t, nil, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"request_id":"req_gateway_learning","error":{"code":"learning_report_not_found","message":"no report"}}`))
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	cookie := sessionCookie(t, handler, learningReportUserID)
	for _, path := range []string{learningReportsPath("/latest"), learningReportsPath("/tasks/" + learningReportTaskID)} {
		response := getLearningReport(t, handler, cookie, path)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404: %s", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "learning_report_not_found") {
			t.Fatalf("%s echoed the Core error body: %s", path, response.Body.String())
		}
	}
}

func TestLearningReportTaskRejectsAMalformedTaskIDWithoutCallingCore(t *testing.T) {
	var coreCalls atomic.Int32
	platform := newLearningReportsPlatform(t, nil, http.StatusOK)
	defer platform.Close()
	core := newLearningReportsCore(t, &coreCalls, func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("Core must not be called for a malformed task id")
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	response := getLearningReport(t, handler, sessionCookie(t, handler, learningReportUserID), learningReportsPath("/tasks/not-a-uuid"))
	if response.Code != http.StatusNotFound {
		t.Fatalf("malformed task id = %d %s", response.Code, response.Body.String())
	}
	if coreCalls.Load() != 0 {
		t.Fatalf("core calls = %d", coreCalls.Load())
	}
}

func TestLearningReportReadsFailClosedOnAnInvalidCoreShape(t *testing.T) {
	platform := newLearningReportsPlatform(t, nil, http.StatusOK)
	defer platform.Close()
	// The finding cites evidence that is not in this report: an unverifiable
	// claim must never be rendered as the owner's own evidence.
	forged := strings.Replace(learningReportBody, `"evidence_ids":["ev_1"]`, `"evidence_ids":["ev_forged"]`, 1)
	core := newLearningReportsCore(t, nil, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(forged))
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	response := getLearningReport(t, handler, sessionCookie(t, handler, learningReportUserID), learningReportsPath("/latest"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("forged citation = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "ev_forged") || strings.Contains(response.Body.String(), "ev_1") {
		t.Fatalf("invalid Core body was partially served: %s", response.Body.String())
	}
}

func TestLearningReportReadsPassThroughThePlatformPermissionDecision(t *testing.T) {
	platform := newLearningReportsPlatform(t, nil, http.StatusForbidden)
	defer platform.Close()
	var coreCalls atomic.Int32
	core := newLearningReportsCore(t, &coreCalls, func(writer http.ResponseWriter, request *http.Request) {
		t.Fatal("Core must not be called without a live Portal permission")
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	response := getLearningReport(t, handler, sessionCookie(t, handler, learningReportUserID), learningReportsPath("/preferences"))
	if response.Code != http.StatusForbidden {
		t.Fatalf("forbidden preferences read = %d %s", response.Code, response.Body.String())
	}
	if coreCalls.Load() != 0 {
		t.Fatalf("core calls = %d", coreCalls.Load())
	}
}

func TestLearningReportPreferencesAndTaskReadTheirOwnPaths(t *testing.T) {
	platform := newLearningReportsPlatform(t, nil, http.StatusOK)
	defer platform.Close()
	core := newLearningReportsCore(t, nil, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/portal/practice/banks/" + learningReportBankID + "/learning-reports/preferences":
			_, _ = writer.Write([]byte(`{"request_id":"req_gateway_learning","data":{"enabled":true,"interval_days":7,"goal":"exam_review","chapter_ids":["ch01"],"external_analysis_consent":true,"bank_id":"` + learningReportBankID + `","revision":3,"next_due_at":"2026-10-09T00:00:00Z","updated_at":"2026-10-02T00:00:00Z"}}`))
		case "/api/v1/portal/practice/banks/" + learningReportBankID + "/learning-reports/tasks/" + learningReportTaskID:
			_, _ = writer.Write([]byte(`{"request_id":"req_gateway_learning","data":{"task_id":"` + learningReportTaskID + `","bank_id":"` + learningReportBankID + `","status":"queued","created_at":"2026-10-02T00:00:00Z","retry_after_seconds":30}}`))
		default:
			t.Fatalf("unexpected Core path %q", request.URL.Path)
		}
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	cookie := sessionCookie(t, handler, learningReportUserID)

	preferences := getLearningReport(t, handler, cookie, learningReportsPath("/preferences"))
	if preferences.Code != http.StatusOK {
		t.Fatalf("preferences = %d %s", preferences.Code, preferences.Body.String())
	}
	var preferencesEnvelope practice.LearningReportPreferencesEnvelope
	if err := json.Unmarshal(preferences.Body.Bytes(), &preferencesEnvelope); err != nil {
		t.Fatal(err)
	}
	if preferencesEnvelope.Data.Goal != "exam_review" || preferencesEnvelope.Data.Revision != 3 || !preferencesEnvelope.Data.ExternalAnalysisConsent || len(preferencesEnvelope.Data.ChapterIDs) != 1 {
		t.Fatalf("preferences = %+v", preferencesEnvelope)
	}

	task := getLearningReport(t, handler, cookie, learningReportsPath("/tasks/"+learningReportTaskID))
	if task.Code != http.StatusOK {
		t.Fatalf("task = %d %s", task.Code, task.Body.String())
	}
	var taskEnvelope practice.LearningReportTaskEnvelope
	if err := json.Unmarshal(task.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatal(err)
	}
	if taskEnvelope.Data.Status != "queued" || taskEnvelope.Data.TaskID != learningReportTaskID || taskEnvelope.Data.RetryAfterSeconds == nil || *taskEnvelope.Data.RetryAfterSeconds != 30 {
		t.Fatalf("task = %+v", taskEnvelope)
	}
}
