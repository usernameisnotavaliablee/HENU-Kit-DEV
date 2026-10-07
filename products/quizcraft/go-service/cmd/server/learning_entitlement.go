package main

import (
	"errors"
	"os"
	"strings"

	quizcraft "henukit.dev/quizcraft"
)

// All fields must be provided together; absence keeps the feature dark. These
// credentials are never reused for Portal, Console or the QuizCraft session.
func learningEntitlementFromEnv() (*quizcraft.LearningEntitlementClient, error) {
	baseURL := os.Getenv("QUIZCRAFT_LEARNING_ENTITLEMENT_URL")
	clientID := os.Getenv("QUIZCRAFT_LEARNING_ENTITLEMENT_CLIENT_ID")
	keyID := os.Getenv("QUIZCRAFT_LEARNING_ENTITLEMENT_KEY_ID")
	secret := os.Getenv("QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET")
	if baseURL == "" && clientID == "" && keyID == "" && secret == "" {
		return nil, nil
	}
	invalid := errors.New("QuizCraft learning entitlement caller configuration is incomplete or unsafe")
	if baseURL == "" || clientID == "" || keyID == "" || secret == "" || strings.TrimSpace(secret) != secret {
		return nil, invalid
	}
	lower := strings.ToLower(secret)
	if strings.HasPrefix(lower, "replace-") || strings.HasPrefix(lower, "change-me") || strings.HasPrefix(lower, "example-") || strings.Contains(lower, "placeholder") {
		return nil, invalid
	}
	for _, name := range []string{"QUIZCRAFT_AUTH_HMAC_SECRET", "QUIZCRAFT_CUTOVER_EVIDENCE_SECRET", "QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET", "QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET", "QUIZCRAFT_SUMMARY_CLIENT_SECRET", "QUIZCRAFT_PLATFORM_CLIENT_SECRET"} {
		if secret == os.Getenv(name) {
			return nil, invalid
		}
	}
	for _, name := range []string{"QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID", "QUIZCRAFT_PORTAL_COMMAND_CLIENT_ID", "QUIZCRAFT_SUMMARY_CLIENT_ID", "QUIZCRAFT_PLATFORM_CLIENT_ID"} {
		if clientID == os.Getenv(name) {
			return nil, invalid
		}
	}
	client, err := quizcraft.NewLearningEntitlementClient(quizcraft.LearningEntitlementClientConfig{BaseURL: baseURL, ClientID: clientID, KeyID: keyID, Secret: secret})
	if err != nil {
		return nil, invalid
	}
	return client, nil
}
