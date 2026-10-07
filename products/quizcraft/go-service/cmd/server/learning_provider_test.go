package main

import (
	"testing"
	"time"

	quizcraft "henukit.dev/quizcraft"
)

var learningWorkerEnvNames = []string{
	"QUIZCRAFT_LEARNING_WORKER_ENABLED",
	"QUIZCRAFT_LEARNING_WORKER_POLL",
	"QUIZCRAFT_LEARNING_WORKER_LEASE",
	"QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL",
	"QUIZCRAFT_LEARNING_PROVIDER_URL",
	"QUIZCRAFT_LEARNING_PROVIDER_API_KEY",
	"QUIZCRAFT_LEARNING_PROVIDER_MODEL",
	"QUIZCRAFT_AUTH_HMAC_SECRET",
	"QUIZCRAFT_CUTOVER_EVIDENCE_SECRET",
	"QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET",
	"QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET",
	"QUIZCRAFT_SUMMARY_CLIENT_SECRET",
	"QUIZCRAFT_PLATFORM_CLIENT_SECRET",
	"QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET",
}

func clearLearningWorkerEnv(t *testing.T) {
	t.Helper()
	for _, name := range learningWorkerEnvNames {
		t.Setenv(name, "")
	}
}

func TestLearningWorkerStaysDarkWithoutConfiguration(t *testing.T) {
	clearLearningWorkerEnv(t)
	settings, err := learningWorkerFromEnv()
	if err != nil || settings != nil {
		t.Fatalf("an unconfigured worker must stay dark, got %+v err=%v", settings, err)
	}
}

func TestLearningWorkerRefusesAPartialOrUnrequestedConfiguration(t *testing.T) {
	cases := map[string]map[string]string{
		"enabled without provider": {"QUIZCRAFT_LEARNING_WORKER_ENABLED": "1"},
		"missing api key": {
			"QUIZCRAFT_LEARNING_PROVIDER_URL":   "https://provider.test/v1",
			"QUIZCRAFT_LEARNING_PROVIDER_MODEL": "model-a",
		},
		"missing model": {
			"QUIZCRAFT_LEARNING_PROVIDER_URL":     "https://provider.test/v1",
			"QUIZCRAFT_LEARNING_PROVIDER_API_KEY": "sk-live-9f3a2b7c4d5e6f70",
		},
		"bad enabled flag": {
			"QUIZCRAFT_LEARNING_WORKER_ENABLED": "yes",
		},
		"public http provider": {
			"QUIZCRAFT_LEARNING_WORKER_ENABLED":   "1",
			"QUIZCRAFT_LEARNING_PROVIDER_URL":     "http://provider.test/v1",
			"QUIZCRAFT_LEARNING_PROVIDER_API_KEY": "sk-live-9f3a2b7c4d5e6f70",
			"QUIZCRAFT_LEARNING_PROVIDER_MODEL":   "model-a",
		},
		"placeholder key": {
			"QUIZCRAFT_LEARNING_WORKER_ENABLED":   "1",
			"QUIZCRAFT_LEARNING_PROVIDER_URL":     "https://provider.test/v1",
			"QUIZCRAFT_LEARNING_PROVIDER_API_KEY": "replace-provider-api-key",
			"QUIZCRAFT_LEARNING_PROVIDER_MODEL":   "model-a",
		},
		"short key": {
			"QUIZCRAFT_LEARNING_WORKER_ENABLED":   "1",
			"QUIZCRAFT_LEARNING_PROVIDER_URL":     "https://provider.test/v1",
			"QUIZCRAFT_LEARNING_PROVIDER_API_KEY": "short",
			"QUIZCRAFT_LEARNING_PROVIDER_MODEL":   "model-a",
		},
		"reused service secret": {
			"QUIZCRAFT_LEARNING_WORKER_ENABLED":   "1",
			"QUIZCRAFT_LEARNING_PROVIDER_URL":     "https://provider.test/v1",
			"QUIZCRAFT_LEARNING_PROVIDER_API_KEY": "sk-live-9f3a2b7c4d5e6f70",
			"QUIZCRAFT_LEARNING_PROVIDER_MODEL":   "model-a",
			"QUIZCRAFT_AUTH_HMAC_SECRET":          "sk-live-9f3a2b7c4d5e6f70",
		},
	}
	for name, environment := range cases {
		t.Run(name, func(t *testing.T) {
			clearLearningWorkerEnv(t)
			for key, value := range environment {
				t.Setenv(key, value)
			}
			if settings, err := learningWorkerFromEnv(); err == nil || settings != nil {
				t.Fatalf("expected an unsafe worker configuration to be refused, got %+v err=%v", settings, err)
			}
		})
	}
}

func TestLearningWorkerConfiguredButDisabledStaysDark(t *testing.T) {
	clearLearningWorkerEnv(t)
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_URL", "https://provider.test/v1")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY", "sk-live-9f3a2b7c4d5e6f70")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL", "model-a")
	settings, err := learningWorkerFromEnv()
	if err != nil || settings != nil {
		t.Fatalf("a configured but disabled worker must not start, got %+v err=%v", settings, err)
	}
}

func TestLearningWorkerBuildsPinnedVersionsAndBoundedIntervals(t *testing.T) {
	clearLearningWorkerEnv(t)
	t.Setenv("QUIZCRAFT_LEARNING_WORKER_ENABLED", "1")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_URL", "https://provider.test/v1/")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY", "sk-live-9f3a2b7c4d5e6f70")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL", "model-a")
	settings, err := learningWorkerFromEnv()
	if err != nil {
		t.Fatalf("configure worker: %v", err)
	}
	if settings == nil || settings.Provider == nil {
		t.Fatal("an enabled, fully configured worker must expose a provider")
	}
	want := quizcraft.LearningJobVersions{Model: "model-a", Prompt: quizcraft.LearningPromptVersion, Policy: quizcraft.LearningAnalysisPolicyVersion}
	if settings.Versions != want {
		t.Fatalf("worker versions must be pinned by the server, got %+v", settings.Versions)
	}
	if settings.Poll != learningWorkerDefaultPoll || settings.Lease != learningWorkerDefaultLease {
		t.Fatalf("unexpected default intervals poll=%s lease=%s", settings.Poll, settings.Lease)
	}
	if settings.Schedule != learningWorkerDefaultSchedule {
		t.Fatalf("unexpected default schedule %s", settings.Schedule)
	}
}

func TestLearningSchedulerIntervalIsBoundedAndCanBeTurnedOff(t *testing.T) {
	clearLearningWorkerEnv(t)
	t.Setenv("QUIZCRAFT_LEARNING_WORKER_ENABLED", "1")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_URL", "https://provider.test/v1")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY", "sk-live-9f3a2b7c4d5e6f70")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL", "model-a")
	t.Setenv("QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL", "0")
	settings, err := learningWorkerFromEnv()
	if err != nil || settings == nil || settings.Schedule != 0 {
		t.Fatalf("explicit 0 must run the worker without automatic scheduling, got %+v err=%v", settings, err)
	}
	for name, value := range map[string]string{
		"unparseable": "every hour",
		"too small":   "500ms",
		"too large":   "2h",
		"sub-second":  "10500ms",
	} {
		t.Run(name, func(t *testing.T) {
			clearLearningWorkerEnv(t)
			t.Setenv("QUIZCRAFT_LEARNING_WORKER_ENABLED", "1")
			t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_URL", "https://provider.test/v1")
			t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY", "sk-live-9f3a2b7c4d5e6f70")
			t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL", "model-a")
			t.Setenv("QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL", value)
			if settings, err := learningWorkerFromEnv(); err == nil || settings != nil {
				t.Fatalf("expected %s to be refused, got %+v err=%v", value, settings, err)
			}
		})
	}
}

func TestLearningWorkerIntervalBoundsAreEnforced(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		env   string
		value string
	}{
		{"unparseable poll", "QUIZCRAFT_LEARNING_WORKER_POLL", "soon"},
		{"poll too small", "QUIZCRAFT_LEARNING_WORKER_POLL", "500ms"},
		{"poll too large", "QUIZCRAFT_LEARNING_WORKER_POLL", "1h"},
		{"lease too small", "QUIZCRAFT_LEARNING_WORKER_LEASE", "5s"},
		{"sub-second lease", "QUIZCRAFT_LEARNING_WORKER_LEASE", "90500ms"},
		{"lease too large", "QUIZCRAFT_LEARNING_WORKER_LEASE", "30m"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			clearLearningWorkerEnv(t)
			t.Setenv("QUIZCRAFT_LEARNING_WORKER_ENABLED", "1")
			t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_URL", "https://provider.test/v1")
			t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY", "sk-live-9f3a2b7c4d5e6f70")
			t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL", "model-a")
			t.Setenv(testCase.env, testCase.value)
			if settings, err := learningWorkerFromEnv(); err == nil || settings != nil {
				t.Fatalf("expected %s=%s to be refused, got %+v err=%v", testCase.env, testCase.value, settings, err)
			}
		})
	}
}

func TestLearningWorkerAcceptsExplicitIntervals(t *testing.T) {
	clearLearningWorkerEnv(t)
	t.Setenv("QUIZCRAFT_LEARNING_WORKER_ENABLED", "1")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_URL", "https://provider.test/v1")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_API_KEY", "sk-live-9f3a2b7c4d5e6f70")
	t.Setenv("QUIZCRAFT_LEARNING_PROVIDER_MODEL", "model-a")
	t.Setenv("QUIZCRAFT_LEARNING_WORKER_POLL", "30s")
	t.Setenv("QUIZCRAFT_LEARNING_WORKER_LEASE", "3m")
	t.Setenv("QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL", "45m")
	settings, err := learningWorkerFromEnv()
	if err != nil || settings == nil {
		t.Fatalf("explicit in-range intervals must be accepted, got %+v err=%v", settings, err)
	}
	if settings.Poll != 30*time.Second || settings.Lease != 3*time.Minute || settings.Schedule != 45*time.Minute {
		t.Fatalf("explicit intervals were not honoured: %+v", settings)
	}
}
