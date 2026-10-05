package main

import (
	"errors"
	"os"
	"strings"
	"time"

	quizcraft "henukit.dev/quizcraft"
)

// learningWorkerSettings is the fully validated, opt-in report worker
// configuration. A nil result means the feature stays dark.
type learningWorkerSettings struct {
	Provider quizcraft.LearningModelCall
	Versions quizcraft.LearningJobVersions
	Poll     time.Duration
	Lease    time.Duration
}

const (
	learningWorkerDefaultPoll  = 15 * time.Second
	learningWorkerDefaultLease = 90 * time.Second
)

// The provider is operator configuration: never a browser field, and never the
// Portal, Console, session or entitlement credential. It stays disabled unless
// QUIZCRAFT_LEARNING_WORKER_ENABLED=1 and all three provider values are set.
func learningWorkerFromEnv() (*learningWorkerSettings, error) {
	invalid := errors.New("QuizCraft learning provider/worker configuration is incomplete or unsafe")
	baseURL := os.Getenv("QUIZCRAFT_LEARNING_PROVIDER_URL")
	apiKey := os.Getenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY")
	model := os.Getenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL")
	enabled := os.Getenv("QUIZCRAFT_LEARNING_WORKER_ENABLED")
	if enabled != "" && enabled != "0" && enabled != "1" {
		return nil, invalid
	}
	if baseURL == "" && apiKey == "" && model == "" {
		if enabled == "1" {
			return nil, invalid
		}
		return nil, nil
	}
	if baseURL == "" || apiKey == "" || model == "" || strings.TrimSpace(apiKey) != apiKey || len(apiKey) < 16 {
		return nil, invalid
	}
	lower := strings.ToLower(apiKey)
	if strings.HasPrefix(lower, "replace-") || strings.HasPrefix(lower, "change-me") || strings.HasPrefix(lower, "example-") || strings.Contains(lower, "placeholder") {
		return nil, invalid
	}
	for _, name := range []string{"QUIZCRAFT_AUTH_HMAC_SECRET", "QUIZCRAFT_CUTOVER_EVIDENCE_SECRET", "QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET", "QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET", "QUIZCRAFT_SUMMARY_CLIENT_SECRET", "QUIZCRAFT_PLATFORM_CLIENT_SECRET", "QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET"} {
		if apiKey == os.Getenv(name) {
			return nil, invalid
		}
	}
	provider, err := quizcraft.NewOpenAICompatibleLearningModel(quizcraft.LearningProviderConfig{BaseURL: baseURL, APIKey: apiKey, Model: model})
	if err != nil {
		return nil, invalid
	}
	poll, err := learningWorkerDuration("QUIZCRAFT_LEARNING_WORKER_POLL", learningWorkerDefaultPoll, time.Second, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	lease, err := learningWorkerDuration("QUIZCRAFT_LEARNING_WORKER_LEASE", learningWorkerDefaultLease, 15*time.Second, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	if enabled != "1" {
		return nil, nil
	}
	return &learningWorkerSettings{
		Provider: provider,
		Versions: quizcraft.LearningJobVersions{Model: model, Prompt: quizcraft.LearningPromptVersion, Policy: quizcraft.LearningAnalysisPolicyVersion},
		Poll:     poll,
		Lease:    lease,
	}, nil
}

func learningWorkerDuration(name string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < minimum || value > maximum || value%time.Second != 0 {
		return 0, errors.New(name + " is invalid")
	}
	return value, nil
}
