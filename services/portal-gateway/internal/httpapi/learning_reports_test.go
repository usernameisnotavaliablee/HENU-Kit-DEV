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

func newLearningReportWriteHandler(t *testing.T, coreURL string, learningReportsEnabled bool) *Handler {
	t.Helper()
	handler, err := New(config.Config{
		SessionKey:                      []byte("0123456789abcdef0123456789abcdef"),
		PortalOrigin:                    "https://portal.test",
		PracticeURL:                     coreURL,
		PracticeCommandAuth:             config.ServiceAuth{ClientID: "portal-gateway", ClientSecret: practiceCommandSecret, KeyID: "portal-practice-command-key"},
		PracticeCommandsEnabled:         true,
		QuizCraftLearningReportsEnabled: learningReportsEnabled,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestLearningReportWritesStayDarkUntilTheGateIsOn(t *testing.T) {
	var coreCalls atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		coreCalls.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer core.Close()

	handler := newLearningReportWriteHandler(t, core.URL, false)
	writes := []struct {
		method, path, body string
	}{
		{http.MethodPut, "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports/preferences", `{"enabled":true}`},
		{http.MethodPost, "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports", ""},
		{http.MethodDelete, "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports", ""},
		{http.MethodPost, "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports/results/" + learningReportID + "/practice-sessions", ""},
	}
	for _, write := range writes {
		recorder := httptest.NewRecorder()
		handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, write.method, write.path, write.body, "learning-report-idempotency-key"))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s = %d, want 503: %s", write.method, write.path, recorder.Code, recorder.Body.String())
		}
	}
	if coreCalls.Load() != 0 {
		t.Fatalf("dark learning-report writes reached Core %d times", coreCalls.Load())
	}
}

func TestLearningReportWritesRequireASessionAndAnIdempotencyKey(t *testing.T) {
	var coreCalls atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		coreCalls.Add(1)
	}))
	defer core.Close()

	handler := newLearningReportWriteHandler(t, core.URL, true)
	path := "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports"

	anonymous := httptest.NewRequest(http.MethodDelete, "https://portal.test"+path, nil)
	anonymous.Header.Set("Idempotency-Key", "learning-report-idempotency-key")
	recorder := httptest.NewRecorder()
	handler.Router().ServeHTTP(recorder, anonymous)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous clear = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}

	for _, key := range []string{"", "short"} {
		recorder := httptest.NewRecorder()
		handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, http.MethodDelete, path, "", key))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("clear with key %q = %d, want 400: %s", key, recorder.Code, recorder.Body.String())
		}
	}
	if coreCalls.Load() != 0 {
		t.Fatalf("unauthenticated or keyless writes reached Core %d times", coreCalls.Load())
	}
}

func TestLearningReportPreferencesWriteForwardsOneSignedCommand(t *testing.T) {
	body := `{"enabled":true,"interval_days":7,"goal":"exam_review","chapter_ids":["ch01"],"external_analysis_consent":true}`
	var calls atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		expectedPath := strings.Replace(practice.UpdatePortalLearningReportPreferencesPath, "{bank_id}", learningReportBankID, 1)
		if request.Method != http.MethodPut || request.URL.Path != expectedPath {
			t.Fatalf("Core request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("X-Actor-User-Id") != practiceCommandUserID {
			t.Fatalf("actor header = %q", request.Header.Get("X-Actor-User-Id"))
		}
		if request.Header.Get("Idempotency-Key") != "learning-report-idempotency-key" {
			t.Fatalf("idempotency key = %q", request.Header.Get("Idempotency-Key"))
		}
		assertSignedPracticeCommand(t, request, readPracticeCommandBody(t, request), practiceCommandUserID)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"request_id":"req_core_preferences","data":{"enabled":true,"interval_days":7,"goal":"exam_review","chapter_ids":["ch01"],"external_analysis_consent":true,"bank_id":"` + learningReportBankID + `","revision":3}}`))
	}))
	defer core.Close()

	handler := newLearningReportWriteHandler(t, core.URL, true)
	path := "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports/preferences"
	recorder := httptest.NewRecorder()
	handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, http.MethodPut, path, body, "learning-report-idempotency-key"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("preferences write = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"revision":3`) || !strings.Contains(recorder.Body.String(), `"request_id":"req_core_preferences"`) {
		t.Fatalf("preferences write did not relay the Core envelope: %s", recorder.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("core calls = %d", calls.Load())
	}
}

func TestRequestLearningReportRelaysTheTaskEnvelope(t *testing.T) {
	for _, coreStatus := range []int{http.StatusAccepted, http.StatusOK} {
		t.Run(http.StatusText(coreStatus), func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				expectedPath := strings.Replace(practice.RequestPortalLearningReportPath, "{bank_id}", learningReportBankID, 1)
				if request.Method != http.MethodPost || request.URL.Path != expectedPath {
					t.Fatalf("Core request = %s %s", request.Method, request.URL.Path)
				}
				assertSignedPracticeCommand(t, request, readPracticeCommandBody(t, request), practiceCommandUserID)
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(coreStatus)
				_, _ = writer.Write([]byte(`{"request_id":"req_core_task","data":{"task_id":"` + learningReportTaskID + `","bank_id":"` + learningReportBankID + `","status":"queued","created_at":"2026-10-02T00:00:00Z"}}`))
			}))
			defer core.Close()

			handler := newLearningReportWriteHandler(t, core.URL, true)
			path := "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports"
			recorder := httptest.NewRecorder()
			handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, http.MethodPost, path, "", "learning-report-idempotency-key"))
			// Core answers 202 for a new request and 200 for a reused one; the
			// browser gets 202 either way and reads the task status from the body.
			if recorder.Code != http.StatusAccepted {
				t.Fatalf("request write (core %d) = %d: %s", coreStatus, recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"status":"queued"`) {
				t.Fatalf("request write did not relay the task envelope: %s", recorder.Body.String())
			}
		})
	}
}

func TestClearLearningReportsRelaysTheClearedResult(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		expectedPath := strings.Replace(practice.ClearPortalLearningReportsPath, "{bank_id}", learningReportBankID, 1)
		if request.Method != http.MethodDelete || request.URL.Path != expectedPath {
			t.Fatalf("Core request = %s %s", request.Method, request.URL.Path)
		}
		assertSignedPracticeCommand(t, request, readPracticeCommandBody(t, request), practiceCommandUserID)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"request_id":"req_core_clear","data":{"cleared":true,"revision":4}}`))
	}))
	defer core.Close()

	handler := newLearningReportWriteHandler(t, core.URL, true)
	path := "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports"
	recorder := httptest.NewRecorder()
	handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, http.MethodDelete, path, "", "learning-report-idempotency-key"))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"cleared":true`) {
		t.Fatalf("clear = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateLearningReportPracticeSessionPinsTheReportPath(t *testing.T) {
	var calls atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		expectedPath := strings.Replace(strings.Replace(practice.CreatePortalLearningReportPracticeSessionPath, "{bank_id}", learningReportBankID, 1), "{report_id}", learningReportID, 1)
		if request.Method != http.MethodPost || request.URL.Path != expectedPath {
			t.Fatalf("Core request = %s %s, want %s", request.Method, request.URL.Path, expectedPath)
		}
		assertSignedPracticeCommand(t, request, readPracticeCommandBody(t, request), practiceCommandUserID)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"request_id":"req_core_session","data":{"session_id":"22222222-2222-4222-8222-222222222222","bank_id":"` + learningReportBankID + `","bank_version_id":"44444444-4444-4444-8444-444444444444","mode":"report","excluded_unavailable_count":0,"questions":[{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","type":"single","chapter_id":"ch01","chapter":"基础","content":"服务端选择的题目","options":["甲","乙"]}]}}`))
	}))
	defer core.Close()

	handler := newLearningReportWriteHandler(t, core.URL, true)
	path := "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports/results/" + learningReportID + "/practice-sessions"
	recorder := httptest.NewRecorder()
	handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, http.MethodPost, path, "", "learning-report-idempotency-key"))
	if recorder.Code != http.StatusCreated || !strings.Contains(recorder.Body.String(), `"mode":"report"`) {
		t.Fatalf("report session = %d: %s", recorder.Code, recorder.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("core calls = %d", calls.Load())
	}

	// A report id that is not a UUID is rejected before Core is contacted.
	malformed := httptest.NewRecorder()
	handler.Router().ServeHTTP(malformed, authenticatedPracticeCommandRequest(t, handler, http.MethodPost, "/api/v1/practice/banks/"+learningReportBankID+"/learning-reports/results/not-a-uuid/practice-sessions", "", "learning-report-idempotency-key"))
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed report id = %d: %s", malformed.Code, malformed.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("malformed report id reached Core: calls = %d", calls.Load())
	}
}

func TestLearningReportWriteFailuresMapToHonestBrowserErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		coreStatus int
		wantStatus int
		wantError  string
	}{
		{name: "invalid payload", coreStatus: http.StatusBadRequest, wantStatus: http.StatusBadRequest, wantError: "practice_command_invalid"},
		{name: "revoked entitlement", coreStatus: http.StatusForbidden, wantStatus: http.StatusForbidden, wantError: "practice_session_forbidden"},
		{name: "unknown bank", coreStatus: http.StatusNotFound, wantStatus: http.StatusNotFound, wantError: "practice_session_not_found"},
		{name: "state conflict", coreStatus: http.StatusConflict, wantStatus: http.StatusConflict, wantError: "practice_command_conflict"},
		{name: "abuse guard", coreStatus: http.StatusTooManyRequests, wantStatus: http.StatusTooManyRequests, wantError: "practice_command_rate_limited"},
		{name: "core fault", coreStatus: http.StatusInternalServerError, wantStatus: http.StatusServiceUnavailable, wantError: "practice_commands_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(test.coreStatus)
			}))
			defer core.Close()

			handler := newLearningReportWriteHandler(t, core.URL, true)
			path := "/api/v1/practice/banks/" + learningReportBankID + "/learning-reports"
			recorder := httptest.NewRecorder()
			handler.Router().ServeHTTP(recorder, authenticatedPracticeCommandRequest(t, handler, http.MethodDelete, path, "", "learning-report-idempotency-key"))
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), test.wantError) {
				t.Fatalf("clear (core %d) = %d: %s", test.coreStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestLearningReportDependencyFailureNeverLeaksUpstreamDetail(t *testing.T) {
	platform := newLearningReportsPlatform(t, nil, http.StatusOK)
	defer platform.Close()
	// The failing Core answers with a body only a test can recognise. Portal
	// shows the Gateway's own message to members verbatim, so neither that body
	// nor any Go error text may reach the browser.
	const marker = "upstream_marker_9f3"
	core := newLearningReportsCore(t, nil, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":"` + marker + `","message":"` + marker + `"}`))
	})
	defer core.Close()

	handler := newLearningReportsHandler(t, platform.URL, core.URL, true, true)
	response := getLearningReport(t, handler, sessionCookie(t, handler, learningReportUserID), learningReportsPath("/latest"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("core fault = %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("dependency failure is not an error envelope: %v (%s)", err, response.Body.String())
	}
	if envelope.Error != "practice learning reports are temporarily unavailable" {
		t.Fatalf("dependency failure code = %q", envelope.Error)
	}
	if envelope.Message != "学习报告暂时不可用，请稍后再试" {
		t.Fatalf("dependency failure message = %q", envelope.Message)
	}
	if strings.Contains(response.Body.String(), marker) {
		t.Fatalf("upstream detail reached the browser: %s", response.Body.String())
	}
}
