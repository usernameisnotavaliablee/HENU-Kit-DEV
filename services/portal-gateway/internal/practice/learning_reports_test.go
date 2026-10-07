package practice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testLearningReportBankID = "10ca9b18-c303-4b7a-ab14-1241e41b665a"
	testLearningReportTaskID = "c1d2e3f4-a5b6-4c7d-8e9f-0a1b2c3d4e5f"
	testLearningReportID     = "3b7f2c44-9a5e-4f1b-8c2d-0e5a7b9c1d2e"
)

const testLearningReportData = `{"report_id":"` + testLearningReportID + `",` +
	`"bank_id":"` + testLearningReportBankID + `",` +
	`"content_version_id":"8f7e6d5c-4b3a-4291-8877-665544332211",` +
	`"status":"ready","goal":"follow_course",` +
	`"evidence_until":"2026-10-01T00:00:00Z","created_at":"2026-10-02T00:00:00Z",` +
	`"statistics":[{"tag_id":"math","tag_kind":"knowledge","label":"算术","attempt_count":4,"unique_question_count":3,"first_correct_count":1,"repeat_attempt_count":1,"repeat_correct_count":0,"latest_correct_count":2}],` +
	`"evidence":[{"evidence_id":"ev_1","question_id":"11111111-2222-4333-8444-555555555555","question_version_id":"66666666-7777-4888-8999-000000000000","submitted_at":"2026-10-01T00:00:00Z","correct":false,"question":"1+1=?","submitted_answer":2,"expected_answer":3}],` +
	`"findings":[{"tag_id":"math","status":"tentative","observation":"两次出现同类错误","possible_reason":"可能是进位不熟","evidence_ids":["ev_1"]}],` +
	`"next_step":{"kind":"practice","reason":"先补算术基础","tag_id":"math","question_ids":["11111111-2222-4333-8444-555555555555"]}}`

func TestLearningReportReadsUseTheGeneratedContractPaths(t *testing.T) {
	var calls int
	// The generated constants carry the {bank_id}/{task_id} placeholders; the
	// client must substitute the owner-scoped ids into the real Core path.
	preferencesPath := strings.Replace(GetPortalLearningReportPreferencesPath, "{bank_id}", testLearningReportBankID, 1)
	latestPath := strings.Replace(GetPortalLatestLearningReportPath, "{bank_id}", testLearningReportBankID, 1)
	taskPath := strings.Replace(strings.Replace(GetPortalLearningReportTaskPath, "{bank_id}", testLearningReportBankID, 1), "{task_id}", testLearningReportTaskID, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		switch request.URL.Path {
		case "/api/v1/portal/practice/banks/" + testLearningReportBankID + "/learning-reports/preferences":
			assertPortalReadRequest(t, request, testStatsUserID, "req_learning", preferencesPath, true)
			_, _ = writer.Write([]byte(`{"request_id":"req_core_learning","data":{"enabled":true,"interval_days":7,"goal":"follow_course","chapter_ids":[],"external_analysis_consent":false,"bank_id":"` + testLearningReportBankID + `","revision":2}}`))
		case "/api/v1/portal/practice/banks/" + testLearningReportBankID + "/learning-reports/latest":
			assertPortalReadRequest(t, request, testStatsUserID, "req_learning", latestPath, true)
			_, _ = writer.Write([]byte(`{"request_id":"req_core_learning","data":` + testLearningReportData + `}`))
		case "/api/v1/portal/practice/banks/" + testLearningReportBankID + "/learning-reports/tasks/" + testLearningReportTaskID:
			assertPortalReadRequest(t, request, testStatsUserID, "req_learning", taskPath, true)
			_, _ = writer.Write([]byte(`{"request_id":"req_core_learning","data":{"task_id":"` + testLearningReportTaskID + `","bank_id":"` + testLearningReportBankID + `","status":"running","created_at":"2026-10-02T00:00:00Z"}}`))
		default:
			t.Fatalf("unexpected Core path %q", request.URL.Path)
		}
	}))
	defer server.Close()

	client := testCatalogClient(t, server)
	ctx := context.Background()
	preferences, err := client.LearningReportPreferences(ctx, testStatsUserID, "req_learning", testLearningReportBankID)
	if err != nil || preferences.RequestID != "req_core_learning" || !preferences.Data.Enabled || preferences.Data.IntervalDays != 7 || preferences.Data.Revision != 2 || preferences.Data.ChapterIDs == nil {
		t.Fatalf("preferences = %+v / %v", preferences, err)
	}
	report, err := client.LatestLearningReport(ctx, testStatsUserID, "req_learning", testLearningReportBankID)
	if err != nil || report.RequestID != "req_core_learning" || report.Data.ReportID != testLearningReportID || report.Data.Status != "ready" || len(report.Data.Statistics) != 1 || len(report.Data.Findings) != 1 || report.Data.NextStep.Kind != "practice" {
		t.Fatalf("latest report = %+v / %v", report, err)
	}
	task, err := client.LearningReportTask(ctx, testStatsUserID, "req_learning", testLearningReportBankID, testLearningReportTaskID)
	if err != nil || task.RequestID != "req_core_learning" || task.Data.TaskID != testLearningReportTaskID || task.Data.Status != "running" || task.Data.ReportID != nil {
		t.Fatalf("task = %+v / %v", task, err)
	}
	if calls != 3 {
		t.Fatalf("core requests = %d", calls)
	}
}

func TestLearningReportReadsKeepAnHonestNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"request_id":"req_learning","error":{"code":"learning_report_not_found","message":"none"}}`))
	}))
	defer server.Close()

	client := testCatalogClient(t, server)
	if _, err := client.LatestLearningReport(context.Background(), testStatsUserID, "req_learning", testLearningReportBankID); !errors.Is(err, ErrLearningReportNotFound) {
		t.Fatalf("latest report error = %v, want ErrLearningReportNotFound", err)
	}
	if _, err := client.LearningReportTask(context.Background(), testStatsUserID, "req_learning", testLearningReportBankID, testLearningReportTaskID); !errors.Is(err, ErrLearningReportNotFound) {
		t.Fatalf("task error = %v, want ErrLearningReportNotFound", err)
	}
}

func TestLearningReportReadsRejectAMalformedTaskIDWithoutCallingCore(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()

	if _, err := testCatalogClient(t, server).LearningReportTask(context.Background(), testStatsUserID, "req_learning", testLearningReportBankID, "not-a-uuid"); !errors.Is(err, ErrLearningReportNotFound) {
		t.Fatalf("malformed task id error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("core calls = %d", calls)
	}
}

func TestLearningReportReadsRejectUnmodelledOrForgedCoreShapes(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{
			name: "finding cites evidence outside the report",
			body: `{"request_id":"req_learning","data":` + strings.Replace(testLearningReportData, `"evidence_ids":["ev_1"]`, `"evidence_ids":["ev_forged"]`, 1) + `}`,
		},
		{
			name: "practice next step without questions",
			body: `{"request_id":"req_learning","data":` + strings.Replace(testLearningReportData, `"question_ids":["11111111-2222-4333-8444-555555555555"]`, `"question_ids":[]`, 1) + `}`,
		},
		{
			name: "statistics claim more unique questions than attempts",
			body: `{"request_id":"req_learning","data":` + strings.Replace(testLearningReportData, `"attempt_count":4`, `"attempt_count":1`, 1) + `}`,
		},
		{
			name: "envelope without a request id",
			body: `{"data":` + testLearningReportData + `}`,
		},
		{
			name: "legacy mock shape",
			body: `{"request_id":"req_learning","data":{"stats":{"total":486}}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			if _, err := testCatalogClient(t, server).LatestLearningReport(context.Background(), testStatsUserID, "req_learning", testLearningReportBankID); !errors.Is(err, ErrInvalidStats) {
				t.Fatalf("error = %v, want ErrInvalidStats", err)
			}
		})
	}
}

// TestLearningReportReadClassifiesCoreDenialAsForbidden locks the read boundary:
// Core gates the newest report and task progress on live membership, so its 403
// must not be flattened into "the dependency is down". A member who can renew
// needs a denial, and only a code matching the machine shape may travel further.
func TestLearningReportReadClassifiesCoreDenialAsForbidden(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantForbid bool
		wantCode   string
	}{
		{name: "membership denial", status: http.StatusForbidden, body: `{"request_id":"req_core","error":{"code":"learning_entitlement_required","message":"course feedback requires an active membership"}}`, wantForbid: true, wantCode: "learning_entitlement_required"},
		{name: "denial without a usable code", status: http.StatusForbidden, body: `{"request_id":"req_core","error":{"code":"<b>denied</b>"}}`, wantForbid: true},
		{name: "denial with an empty body", status: http.StatusForbidden, body: ``, wantForbid: true},
		{name: "dependency fault is not a denial", status: http.StatusInternalServerError, body: `{"error":"boom"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(testCase.body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, testCatalogClientID, testCatalogSecret, testCatalogKeyID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.LatestLearningReport(context.Background(), testStatsUserID, "req_learning_read_1", testLearningReportBankID)
			if got := errors.Is(err, ErrPortalReadForbidden); got != testCase.wantForbid {
				t.Fatalf("forbidden = %t, want %t (%v)", got, testCase.wantForbid, err)
			}
			if testCase.wantForbid {
				if code := RejectedCode(err); code != testCase.wantCode {
					t.Fatalf("forwarded code = %q, want %q", code, testCase.wantCode)
				}
				return
			}
			if !errors.Is(err, ErrStatsUnavailable) {
				t.Fatalf("dependency fault must stay unavailable: %v", err)
			}
		})
	}
}
