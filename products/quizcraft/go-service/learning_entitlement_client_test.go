package quizcraft

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const learningEntitlementTestSecret = "quizcraft-learning-entitlement-secret-at-least-32bytes"

func TestLearningEntitlementClientSignsBoundOwnerAndAlwaysChecksLive(t *testing.T) {
	owner := uuid.New()
	var count atomic.Int32
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.URL.Path != learningEntitlementPath+owner.String() || r.Method != http.MethodGet {
			t.Errorf("wrong entitlement target: %s %s", r.Method, r.URL.Path)
		}
		id, secret, ok := r.BasicAuth()
		if !ok || id != "quizcraft-learning" || secret != learningEntitlementTestSecret || r.Header.Get("X-Service-Id") != id || r.Header.Get("X-Key-Id") != "key-1" || r.Header.Get("X-Actor-User-Id") != owner.String() {
			t.Error("caller identity not bound to signed job owner")
		}
		nonce := r.Header.Get("X-Nonce")
		if seen[nonce] || nonce == "" {
			t.Error("nonce reused or omitted")
		}
		seen[nonce] = true
		decoded, err := base64.RawURLEncoding.DecodeString(nonce)
		if err != nil || len(decoded) != 24 {
			t.Error("nonce must contain 24 random bytes")
		}
		empty := sha256.Sum256(nil)
		canonical := strings.Join([]string{http.MethodGet, r.URL.RequestURI(), r.Header.Get("X-Timestamp"), nonce, hex.EncodeToString(empty[:]), owner.String()}, "\n")
		mac := hmac.New(sha256.New, []byte(learningEntitlementTestSecret))
		_, _ = mac.Write([]byte(canonical))
		if !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))) {
			t.Error("actor-bound signature invalid")
		}
		if r.Header.Get("X-Request-Id") == "" {
			t.Error("missing request correlation")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"lifetime":%t,"version":%d},"request_id":"req_owner"}`, count.Load() == 2, int(count.Load()))
	}))
	defer server.Close()
	client, err := NewLearningEntitlementClient(LearningEntitlementClientConfig{BaseURL: server.URL, ClientID: "quizcraft-learning", KeyID: "key-1", Secret: learningEntitlementTestSecret})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{false, true, false} {
		got, err := client.CheckLifetime(context.Background(), owner)
		if err != nil || got != want {
			t.Fatalf("live call %d = %t %v; want %t", i, got, err, want)
		}
	}
	if count.Load() != 3 {
		t.Fatalf("entitlement result was cached: %d", count.Load())
	}
	if _, err := client.CheckLifetime(context.Background(), uuid.Nil); err == nil {
		t.Fatal("empty owner was checked")
	}
}

func TestLearningEntitlementClientRejectsUntrustedOriginsAndResponses(t *testing.T) {
	config := LearningEntitlementClientConfig{BaseURL: "https://account-portfolio.internal", ClientID: "quizcraft-learning", KeyID: "key-1", Secret: learningEntitlementTestSecret}
	for _, bad := range []string{"", "http://public.example", "https://user@account-portfolio.internal", "https://account-portfolio.internal/unsafe", "https://account-portfolio.internal?x=1", "file:///etc/passwd"} {
		changed := config
		changed.BaseURL = bad
		if _, err := NewLearningEntitlementClient(changed); err == nil {
			t.Fatalf("untrusted origin accepted: %q", bad)
		}
	}
	changed := config
	changed.Secret = "short"
	if _, err := NewLearningEntitlementClient(changed); err == nil {
		t.Fatal("weak secret accepted")
	}
	targetCalls := atomic.Int32{}
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetCalls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer redirectTarget.Close()
	response := `{"data":{"lifetime":true,"version":3},"request_id":"req_ok"}`
	contentType := "application/json"
	status := http.StatusOK
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusFound {
			http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	defer source.Close()
	config.BaseURL = source.URL
	client, err := NewLearningEntitlementClient(config)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable, http.StatusFound} {
		status = code
		if got, err := client.CheckLifetime(context.Background(), owner); err == nil || got {
			t.Fatalf("non-200 %d granted entitlement: %t %v", code, got, err)
		}
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect forwarded signed credential to different origin")
	}
	status = http.StatusOK
	contentType = "application/json-forged"
	if got, err := client.CheckLifetime(context.Background(), owner); err == nil || got {
		t.Fatalf("wrong media type granted entitlement: %t %v", got, err)
	}
	contentType = "application/json; charset=utf-8"
	if got, err := client.CheckLifetime(context.Background(), owner); err != nil || !got {
		t.Fatalf("valid JSON media type denied entitlement: %t %v", got, err)
	}
	contentType = "application/json"
	for _, raw := range []string{`{}`, `{"data":{"lifetime":true},"request_id":"req_ok"}`, `{"data":{"lifetime":true,"version":0},"request_id":"req_ok"}`, `{"data":{"lifetime":true,"version":2,"secret":"leak"},"request_id":"req_ok"}`, `{"data":{"lifetime":true,"version":2},"request_id":""}`, strings.Repeat("x", 5000)} {
		response = raw
		if got, err := client.CheckLifetime(context.Background(), owner); err == nil || got {
			t.Fatalf("bad payload granted entitlement: %t %v", got, err)
		}
	}
	response = `{"data":{"lifetime":true,"version":2},"request_id":"req_ok"}`
	status = http.StatusOK
	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if got, err := client.CheckLifetime(timeoutCtx, owner); err == nil || got {
		t.Fatalf("timed out request granted entitlement: %t %v", got, err)
	}
}
