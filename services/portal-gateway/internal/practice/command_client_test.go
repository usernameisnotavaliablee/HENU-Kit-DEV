package practice

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validPracticeSessionEnvelope = `{"request_id":"req_core_session","data":{"session_id":"22222222-2222-4222-8222-222222222222","bank_id":"33333333-3333-4333-8333-333333333333","bank_version_id":"44444444-4444-4444-8444-444444444444","mode":"random","excluded_unavailable_count":0,"questions":[{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","type":"single","chapter_id":"ch01","chapter":"基础","content":"服务端选择的题目","options":["甲","乙"]}]}}`

const validPracticeAnswerEnvelope = `{"request_id":"req_core_answer","data":{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","correct":false,"replayed":false,"expected_answer":1,"analysis":"服务端讲解"}}`

const validPracticeFeedbackEnvelope = `{"request_id":"req_core_feedback","data":{"operation_id":"77777777-7777-4777-8777-777777777777","state":"succeeded","idempotency_key":"feedback-retry-key-000001","request_id":"req_core_feedback","resource_id":"88888888-8888-4888-8888-888888888888"}}`

func TestValidatePracticeSessionEnvelopeRejectsIncompleteOrUnclosedCoreData(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "missing excluded unavailable count",
			raw:  `{"request_id":"req_core_session","data":{"session_id":"22222222-2222-4222-8222-222222222222","bank_id":"33333333-3333-4333-8333-333333333333","bank_version_id":"44444444-4444-8444-8444-444444444444","mode":"random","questions":[]}}`,
		},
		{
			name: "missing required question chapter",
			raw:  `{"request_id":"req_core_session","data":{"session_id":"22222222-2222-4222-8222-222222222222","bank_id":"33333333-3333-4333-8333-333333333333","bank_version_id":"44444444-4444-8444-8444-444444444444","mode":"random","excluded_unavailable_count":0,"questions":[{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","type":"single","chapter_id":"ch01","content":"服务端选择的题目"}]}}`,
		},
		{
			name: "question answer leaks before submission",
			raw:  `{"request_id":"req_core_session","data":{"session_id":"22222222-2222-4222-8222-222222222222","bank_id":"33333333-3333-4333-8333-333333333333","bank_version_id":"44444444-4444-8444-8444-444444444444","mode":"random","excluded_unavailable_count":0,"questions":[{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","type":"single","chapter_id":"ch01","chapter":"基础","content":"服务端选择的题目","answer":1}]}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePracticeSessionEnvelope([]byte(test.raw)); err == nil {
				t.Fatal("accepted an incomplete or unclosed Core session response")
			}
		})
	}
	if err := validatePracticeSessionEnvelope([]byte(validPracticeSessionEnvelope)); err != nil {
		t.Fatalf("rejected valid Core session response: %v", err)
	}
}

func TestValidatePracticeFeedbackEnvelopeRejectsIncompleteOrUnclosedCoreData(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "missing accepted resource id",
			raw:  `{"request_id":"req_core_feedback","data":{"operation_id":"77777777-7777-4777-8777-777777777777","state":"succeeded","idempotency_key":"feedback-retry-key-000001","request_id":"req_core_feedback"}}`,
		},
		{
			name: "operation did not succeed",
			raw:  `{"request_id":"req_core_feedback","data":{"operation_id":"77777777-7777-4777-8777-777777777777","state":"failed","idempotency_key":"feedback-retry-key-000001","request_id":"req_core_feedback","resource_id":"88888888-8888-4888-8888-888888888888"}}`,
		},
		{
			name: "unknown feedback write field",
			raw:  `{"request_id":"req_core_feedback","data":{"operation_id":"77777777-7777-4777-8777-777777777777","state":"succeeded","idempotency_key":"feedback-retry-key-000001","request_id":"req_core_feedback","resource_id":"88888888-8888-4888-8888-888888888888","score":1}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePracticeFeedbackEnvelope([]byte(test.raw)); err == nil {
				t.Fatal("accepted an incomplete or unclosed Core feedback response")
			}
		})
	}
	if err := validatePracticeFeedbackEnvelope([]byte(validPracticeFeedbackEnvelope)); err != nil {
		t.Fatalf("rejected valid Core feedback response: %v", err)
	}
}

func TestValidatePracticeAnswerEnvelopeRejectsIncompleteOrUnclosedCoreData(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "missing required analysis",
			raw:  `{"request_id":"req_core_answer","data":{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","correct":false,"replayed":false,"expected_answer":1}}`,
		},
		{
			name: "unknown answer result field",
			raw:  `{"request_id":"req_core_answer","data":{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","correct":false,"replayed":false,"expected_answer":1,"analysis":"服务端讲解","score":1}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePracticeAnswerEnvelope([]byte(test.raw)); err == nil {
				t.Fatal("accepted an incomplete or unclosed Core answer response")
			}
		})
	}
	if err := validatePracticeAnswerEnvelope([]byte(validPracticeAnswerEnvelope)); err != nil {
		t.Fatalf("rejected valid Core answer response: %v", err)
	}
}

func TestValidatePracticeSessionEnvelopeAcceptsEveryContractMode(t *testing.T) {
	template := `{"request_id":"req_core_session","data":{"session_id":"22222222-2222-4222-8222-222222222222","bank_id":"33333333-3333-4333-8333-333333333333","bank_version_id":"44444444-4444-4444-8444-444444444444","mode":"%s","excluded_unavailable_count":0,"questions":[{"question_id":"55555555-5555-4555-8555-555555555555","question_version_id":"66666666-6666-4666-8666-666666666666","type":"single","chapter_id":"ch01","chapter":"基础","content":"服务端选择的题目","options":["甲","乙"]}]}}`
	for _, mode := range []string{"random", "difficult", "chapter", "favorites", "report"} {
		if err := validatePracticeSessionEnvelope([]byte(fmt.Sprintf(template, mode))); err != nil {
			t.Fatalf("mode %s was rejected: %v", mode, err)
		}
	}
	if err := validatePracticeSessionEnvelope([]byte(fmt.Sprintf(template, "invented"))); err == nil {
		t.Fatal("unknown mode was accepted")
	}
}

// TestCommandRejectionCarriesOnlyCoreCodesTheBrowserContractCanHold locks the
// boundary between Core's error body and the browser contract: the status
// sentinel must keep classifying every rejection, and only a code matching the
// documented machine shape may travel further.
func TestCommandRejectionCarriesOnlyCoreCodesTheBrowserContractCanHold(t *testing.T) {
	const bankID = "33333333-3333-4333-8333-333333333333"
	cases := []struct {
		name     string
		status   int
		body     string
		sentinel error
		wantCode string
	}{
		{name: "named reason", status: http.StatusBadRequest, body: `{"request_id":"req_core","error":{"code":"learning_consent_outdated","message":"the stored analysis consent is out of date"}}`, sentinel: ErrPracticeCommandBadRequest, wantCode: "learning_consent_outdated"},
		{name: "membership reason", status: http.StatusForbidden, body: `{"request_id":"req_core","error":{"code":"learning_entitlement_required","message":"course feedback requires an active membership"}}`, sentinel: ErrPracticeCommandForbidden, wantCode: "learning_entitlement_required"},
		{name: "rate limited keeps its sentinel", status: http.StatusTooManyRequests, body: `{"request_id":"req_core","error":{"code":"rate_limited","message":"too many requests"}}`, sentinel: ErrPracticeCommandRateLimited, wantCode: "rate_limited"},
		{name: "markup is refused", status: http.StatusBadRequest, body: `{"request_id":"req_core","error":{"code":"<script>alert(1)</script>"}}`, sentinel: ErrPracticeCommandBadRequest},
		{name: "uppercase is refused", status: http.StatusBadRequest, body: `{"request_id":"req_core","error":{"code":"Learning_Consent_Outdated"}}`, sentinel: ErrPracticeCommandBadRequest},
		{name: "leading digit is refused", status: http.StatusBadRequest, body: `{"request_id":"req_core","error":{"code":"1_leading_digit"}}`, sentinel: ErrPracticeCommandBadRequest},
		{name: "overlong is refused", status: http.StatusBadRequest, body: `{"request_id":"req_core","error":{"code":"` + strings.Repeat("a", 65) + `"}}`, sentinel: ErrPracticeCommandBadRequest},
		{name: "plain text body is refused", status: http.StatusConflict, body: `not json at all`, sentinel: ErrPracticeCommandConflict},
		{name: "empty body is refused", status: http.StatusNotFound, body: ``, sentinel: ErrPracticeCommandNotFound},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(testCase.body))
			}))
			defer core.Close()
			client, err := NewCommandClient(core.URL, "portal-gateway", strings.Repeat("s", 32), "portal-practice-command-key")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateFavoritesSession(t.Context(), bankID, "11111111-1111-4111-8111-111111111111", "req_portal_gateway_1", "favorites-session-key-0001", nil)
			if !errors.Is(err, testCase.sentinel) {
				t.Fatalf("sentinel = %v, want %v", err, testCase.sentinel)
			}
			if code := RejectedCode(err); code != testCase.wantCode {
				t.Fatalf("forwarded code = %q, want %q", code, testCase.wantCode)
			}
		})
	}
}
