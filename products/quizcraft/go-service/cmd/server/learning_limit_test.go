package main

import (
	"strings"
	"testing"
)

func TestLearningManualLimitDefaultsAndRejectsUnsafeValues(t *testing.T) {
	t.Setenv("QUIZCRAFT_LEARNING_MANUAL_LIMIT", "")
	value, err := learningManualLimitFromEnv()
	if err != nil || value != learningManualLimitDefault {
		t.Fatalf("default = %d, %v", value, err)
	}

	t.Setenv("QUIZCRAFT_LEARNING_MANUAL_LIMIT", " 0 ")
	value, err = learningManualLimitFromEnv()
	if err != nil || value != 0 {
		t.Fatalf("explicit disable = %d, %v", value, err)
	}

	t.Setenv("QUIZCRAFT_LEARNING_MANUAL_LIMIT", "3")
	if value, err = learningManualLimitFromEnv(); err != nil || value != 3 {
		t.Fatalf("configured = %d, %v", value, err)
	}

	for _, invalid := range []string{"-1", "abc", "1.5", "1001", "1e3"} {
		t.Setenv("QUIZCRAFT_LEARNING_MANUAL_LIMIT", invalid)
		if _, err := learningManualLimitFromEnv(); err == nil || !strings.Contains(err.Error(), "QUIZCRAFT_LEARNING_MANUAL_LIMIT") {
			t.Fatalf("%q = %v", invalid, err)
		}
	}
}
