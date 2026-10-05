package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"henukit.dev/quizcraft/internal/contract"
)

func testLearningProviderVersions(model string) LearningJobVersions {
	return LearningJobVersions{Model: model, Prompt: LearningPromptVersion, Policy: LearningAnalysisPolicyVersion}
}

func testLearningProviderInput() LearningModelInput {
	return LearningModelInput{
		PolicyVersion: LearningAnalysisPolicyVersion,
		Goal:          "巩固基础",
		Tags:          []LearningContentTag{},
		Statistics:    []contract.LearningReportStatistic{},
		Evidence:      []LearningModelEvidence{},
	}
}

func TestLearningProviderRejectsUnsafeConfiguration(t *testing.T) {
	cases := map[string]LearningProviderConfig{
		"missing base url":  {APIKey: "k", Model: "m"},
		"missing api key":   {BaseURL: "https://provider.test/v1", Model: "m"},
		"missing model":     {BaseURL: "https://provider.test/v1", APIKey: "k"},
		"unsupported":       {BaseURL: "ftp://provider.test", APIKey: "k", Model: "m"},
		"public http":       {BaseURL: "http://provider.test/v1", APIKey: "k", Model: "m"},
		"embedded userinfo": {BaseURL: "https://u:p@provider.test/v1", APIKey: "k", Model: "m"},
		"query string":      {BaseURL: "https://provider.test/v1?x=1", APIKey: "k", Model: "m"},
		"newline model":     {BaseURL: "https://provider.test/v1", APIKey: "k", Model: "m\nm"},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewOpenAICompatibleLearningModel(config); !errors.Is(err, ErrLearningProviderUnavailable) {
				t.Fatalf("expected unsafe provider configuration to be rejected, got %v", err)
			}
		})
	}
}

func TestLearningProviderSendsOnlyTheBoundedPolicyPayload(t *testing.T) {
	var captured map[string]json.RawMessage
	var contentType, authorization string
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL.Path)
		}
		contentType = r.Header.Get("Content-Type")
		authorization = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &captured); err != nil {
			t.Errorf("provider request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"findings\":[],\"primary_tag_id\":\"\"}"}}],"usage":{"total_tokens":12}}`)
	}))
	defer server.Close()

	model, err := NewOpenAICompatibleLearningModel(LearningProviderConfig{BaseURL: server.URL + "/v1/", APIKey: " test-key ", Model: "model-a"})
	if err != nil {
		t.Fatalf("configure provider: %v", err)
	}
	input := testLearningProviderInput()
	decision, err := model(context.Background(), input, testLearningProviderVersions("model-a"))
	if err != nil {
		t.Fatalf("provider call: %v", err)
	}
	if got := strings.TrimSpace(string(decision)); got != `{"findings":[],"primary_tag_id":""}` {
		t.Fatalf("unexpected decision payload %q", got)
	}
	if contentType != "application/json" || authorization != "Bearer test-key" {
		t.Fatalf("unexpected provider headers content-type=%q authorization=%q", contentType, authorization)
	}
	allowed := map[string]bool{"model": true, "stream": true, "temperature": true, "messages": true}
	for key := range captured {
		if !allowed[key] {
			t.Fatalf("provider request leaked a non-policy field %q", key)
		}
	}
	var messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if json.Unmarshal(captured["messages"], &messages) != nil || len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("provider messages are not the expected system+user pair: %s", captured["messages"])
	}
	var sentInput LearningModelInput
	if err := json.Unmarshal([]byte(messages[1].Content), &sentInput); err != nil || sentInput.PolicyVersion != LearningAnalysisPolicyVersion {
		t.Fatalf("provider user content is not the bounded policy input: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly one provider call, got %d", calls)
	}
}

func TestLearningProviderNeverAnswersAnotherModelOrPolicy(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	}))
	defer server.Close()
	model, err := NewOpenAICompatibleLearningModel(LearningProviderConfig{BaseURL: server.URL, APIKey: "k", Model: "model-a"})
	if err != nil {
		t.Fatalf("configure provider: %v", err)
	}
	other := testLearningProviderInput()
	other.PolicyVersion = "learning-analysis-v9"
	cases := map[string]struct {
		input    LearningModelInput
		versions LearningJobVersions
	}{
		"foreign model":  {testLearningProviderInput(), testLearningProviderVersions("model-b")},
		"foreign policy": {other, testLearningProviderVersions("model-a")},
		"empty versions": {testLearningProviderInput(), LearningJobVersions{}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := model(context.Background(), testCase.input, testCase.versions); !errors.Is(err, ErrLearningProviderUnavailable) {
				t.Fatalf("expected a mismatched job to be refused, got %v", err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("mismatched jobs must not reach the provider, got %d calls", calls)
	}
}

func TestLearningProviderFailsClosedOnUnusableResponse(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"server error":          {http.StatusBadGateway, `{"error":{"code":"upstream"}}`},
		"not json":              {http.StatusOK, `not-json`},
		"no choices":            {http.StatusOK, `{"choices":[]}`},
		"empty content":         {http.StatusOK, `{"choices":[{"message":{"content":"   "}}]}`},
		"unknown shape":         {http.StatusOK, `{"data":{"text":"hello"}}`},
		"oversized content":     {http.StatusOK, `{"choices":[{"message":{"content":"` + strings.Repeat("a", learningModelOutputMaxBytes+1) + `"}}]}`},
		"oversized whole reply": {http.StatusOK, `{"padding":"` + strings.Repeat("a", learningProviderResponseMaxBytes+1) + `"}`},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = io.WriteString(w, testCase.body)
			}))
			defer server.Close()
			model, err := NewOpenAICompatibleLearningModel(LearningProviderConfig{BaseURL: server.URL, APIKey: "k", Model: "model-a"})
			if err != nil {
				t.Fatalf("configure provider: %v", err)
			}
			if _, err := model(context.Background(), testLearningProviderInput(), testLearningProviderVersions("model-a")); !errors.Is(err, ErrLearningProviderUnavailable) {
				t.Fatalf("expected an unusable provider response to fail closed, got %v", err)
			}
		})
	}
}

func TestLearningProviderDoesNotFollowRedirects(t *testing.T) {
	var followed int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&followed, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"findings\":[]}"}}]}`)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	model, err := NewOpenAICompatibleLearningModel(LearningProviderConfig{BaseURL: redirect.URL, APIKey: "k", Model: "model-a"})
	if err != nil {
		t.Fatalf("configure provider: %v", err)
	}
	if _, err := model(context.Background(), testLearningProviderInput(), testLearningProviderVersions("model-a")); !errors.Is(err, ErrLearningProviderUnavailable) {
		t.Fatalf("expected a redirect to fail closed, got %v", err)
	}
	if atomic.LoadInt32(&followed) != 0 {
		t.Fatal("provider credential must never be replayed to a redirect target")
	}
}

func TestLearningProviderUnwrapsASingleMarkdownFence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"findings\\\":[],\\\"primary_tag_id\\\":\\\"\\\"}\\n```\"}}]}")
	}))
	defer server.Close()
	model, err := NewOpenAICompatibleLearningModel(LearningProviderConfig{BaseURL: server.URL, APIKey: "k", Model: "model-a"})
	if err != nil {
		t.Fatalf("configure provider: %v", err)
	}
	decision, err := model(context.Background(), testLearningProviderInput(), testLearningProviderVersions("model-a"))
	if err != nil {
		t.Fatalf("fenced decision should be accepted: %v", err)
	}
	if got := strings.TrimSpace(string(decision)); got != `{"findings":[],"primary_tag_id":""}` {
		t.Fatalf("fence was not unwrapped cleanly: %q", got)
	}
}
