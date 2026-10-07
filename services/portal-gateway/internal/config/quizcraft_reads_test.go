package config

import "testing"

func TestQuizCraftV2ReadsRequireExplicitCompleteEnablement(t *testing.T) {
	valid := map[string]string{
		"PORTAL_ENABLE_QUIZCRAFT_V2_READS":       "1",
		"QUIZCRAFT_CORE_URL":                     "http://quizcraft:8080",
		"QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID":     "portal-gateway",
		"QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET": "catalog-secret-with-enough-entropy",
		"QUIZCRAFT_PORTAL_CATALOG_KEY_ID":        "portal-catalog-key-1",
	}

	for _, test := range []struct {
		name        string
		environment map[string]string
		wantEnabled bool
		wantError   bool
	}{
		{name: "unset is safely dark", environment: map[string]string{}},
		{name: "zero is safely dark even if credentials exist", environment: validWith(valid, "PORTAL_ENABLE_QUIZCRAFT_V2_READS", "0")},
		{name: "one requires every Core credential", environment: map[string]string{"PORTAL_ENABLE_QUIZCRAFT_V2_READS": "1"}, wantError: true},
		{name: "invalid flag fails closed", environment: map[string]string{"PORTAL_ENABLE_QUIZCRAFT_V2_READS": "true"}, wantError: true},
		{name: "one enables only a complete config", environment: valid, wantEnabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			enabled, coreURL, auth, err := quizCraftV2ReadsFromEnv(func(key string) string { return test.environment[key] })
			if test.wantError {
				if err == nil {
					t.Fatal("quizCraftV2ReadsFromEnv() succeeded, want error")
				}
				return
			}
			if err != nil || enabled != test.wantEnabled {
				t.Fatalf("quizCraftV2ReadsFromEnv() = enabled:%v url:%q auth:%+v err:%v", enabled, coreURL, auth, err)
			}
			if enabled && (coreURL == "" || auth.ClientID == "" || auth.ClientSecret == "" || auth.KeyID == "") {
				t.Fatalf("enabled QuizCraft read config was incomplete: url:%q auth:%+v", coreURL, auth)
			}
		})
	}
}

func validWith(values map[string]string, key, value string) map[string]string {
	copy := make(map[string]string, len(values))
	for existingKey, existingValue := range values {
		copy[existingKey] = existingValue
	}
	copy[key] = value
	return copy
}

func TestQuizCraftLearningReportsGateIsExplicitAndRequiresTheV2ReadClient(t *testing.T) {
	for _, test := range []struct {
		name        string
		value       string
		v2Reads     bool
		wantEnabled bool
		wantError   bool
	}{
		{name: "unset is safely dark", value: "", v2Reads: true},
		{name: "zero is safely dark", value: "0", v2Reads: true},
		{name: "invalid flag fails closed", value: "on", v2Reads: true, wantError: true},
		{name: "one without the V2 read client fails closed", value: "1", v2Reads: false, wantError: true},
		{name: "one with the V2 read client enables the surface", value: "1", v2Reads: true, wantEnabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			enabled, err := quizCraftLearningReportsFromEnv(func(key string) string {
				if key == "PORTAL_ENABLE_QUIZCRAFT_LEARNING_REPORTS" {
					return test.value
				}
				return ""
			}, test.v2Reads)
			if test.wantError {
				if err == nil {
					t.Fatal("quizCraftLearningReportsFromEnv() succeeded, want error")
				}
				return
			}
			if err != nil || enabled != test.wantEnabled {
				t.Fatalf("quizCraftLearningReportsFromEnv() = %v, %v", enabled, err)
			}
		})
	}
}
