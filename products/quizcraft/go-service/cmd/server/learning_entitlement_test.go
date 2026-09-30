package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLearningEntitlementFromEnvRequiresSeparateCompleteConfiguration(t *testing.T) {
	for _, name := range []string{"QUIZCRAFT_LEARNING_ENTITLEMENT_URL", "QUIZCRAFT_LEARNING_ENTITLEMENT_CLIENT_ID", "QUIZCRAFT_LEARNING_ENTITLEMENT_KEY_ID", "QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET", "QUIZCRAFT_AUTH_HMAC_SECRET", "QUIZCRAFT_PORTAL_COMMAND_CLIENT_ID", "QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET", "QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID", "QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET", "QUIZCRAFT_SUMMARY_CLIENT_ID", "QUIZCRAFT_SUMMARY_CLIENT_SECRET", "QUIZCRAFT_CUTOVER_EVIDENCE_SECRET", "QUIZCRAFT_PLATFORM_CLIENT_ID", "QUIZCRAFT_PLATFORM_CLIENT_SECRET"} {
		t.Setenv(name, "")
	}
	client, err := learningEntitlementFromEnv()
	if err != nil || client != nil {
		t.Fatalf("default-off caller = %v, %v", client, err)
	}
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_CLIENT_ID", "quizcraft-learning")
	if client, err = learningEntitlementFromEnv(); err == nil || client != nil {
		t.Fatal("partial caller accepted")
	}
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_URL", "https://account-portfolio.internal")
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_KEY_ID", "learning-key")
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET", "replace-quizcraft-learning-secret-32bytes-min!!")
	if client, err = learningEntitlementFromEnv(); err == nil || client != nil {
		t.Fatal("placeholder caller credential accepted")
	}
	const secret = "independent-quizcraft-learning-secret-32bytes-min"
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET", secret)
	t.Setenv("QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET", secret)
	if client, err = learningEntitlementFromEnv(); err == nil || client != nil {
		t.Fatal("Portal command credential reused")
	}
	t.Setenv("QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET", "different-portal-command-secret-32bytes-min")
	t.Setenv("QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID", "quizcraft-learning")
	if client, err = learningEntitlementFromEnv(); err == nil || client != nil {
		t.Fatal("Portal catalog identity reused")
	}
	t.Setenv("QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID", "")
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_URL", "http://public.example")
	if client, err = learningEntitlementFromEnv(); err == nil || client != nil {
		t.Fatal("non-private plain HTTP target accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, actualSecret, _ := r.BasicAuth()
		if id != "quizcraft-learning" || actualSecret != secret || !strings.HasPrefix(r.URL.Path, "/api/v1/internal/quizcraft/entitlements/") {
			t.Errorf("caller did not send scoped credential")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"lifetime":true,"version":2},"request_id":"req_learning"}`)
	}))
	defer server.Close()
	t.Setenv("QUIZCRAFT_LEARNING_ENTITLEMENT_URL", server.URL)
	client, err = learningEntitlementFromEnv()
	if err != nil || client == nil {
		t.Fatalf("valid caller = %v, %v", client, err)
	}
	if granted, err := client.CheckLifetime(context.Background(), uuid.New()); !granted || err != nil {
		t.Fatalf("current member denied: %t %v", granted, err)
	}
}
