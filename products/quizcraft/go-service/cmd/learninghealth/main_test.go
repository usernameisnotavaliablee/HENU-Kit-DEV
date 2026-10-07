package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

// The monitoring command must fail closed and loudly: without a target database
// it refuses to run, and out-of-range thresholds are rejected rather than
// silently monitoring nothing.
func TestLearningHealthCommandRejectsMissingTargetAndBadThresholds(t *testing.T) {
	t.Setenv("QUIZCRAFT_V2_DATABASE_URL", "")
	err := run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "QUIZCRAFT_V2_DATABASE_URL") {
		t.Fatalf("missing target = %v", err)
	}

	t.Setenv("QUIZCRAFT_V2_DATABASE_URL", "postgres://user:secret@127.0.0.1:5432/not_quizcraft_v2?sslmode=disable")
	err = run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "quizcraft_v2") {
		t.Fatalf("wrong target database = %v", err)
	}

	t.Setenv("QUIZCRAFT_V2_DATABASE_URL", "postgres://user:secret@127.0.0.1:5432/quizcraft_v2?sslmode=disable")
	for _, args := range [][]string{
		{"-queued-behind", "0s"},
		{"-queued-behind", "25h"},
		{"-failure-budget", "-1"},
		{"-failure-budget", "100001"},
	} {
		if err := run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "queued-behind must be") {
			t.Fatalf("%v = %v", args, err)
		}
	}
	if err := run(context.Background(), []string{"-unknown"}); err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("unknown flag = %v", err)
	}
	if err := run(context.Background(), []string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help = %v", err)
	}
}
