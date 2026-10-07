package quizcraft

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestLearningSchedulerLoopRepeatsAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int64
	started := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunLearningScheduler(ctx, 5*time.Millisecond, func(context.Context) error {
			if calls.Add(1) == 1 {
				started <- struct{}{}
			}
			return errors.New("sweep unavailable")
		})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not sweep immediately")
	}
	deadline := time.After(2 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("scheduler stopped after %d sweeps", calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled scheduler returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop on cancel")
	}
}

func TestLearningSchedulerLoopRejectsBadConfiguration(t *testing.T) {
	ctx := context.Background()
	// Held in a variable so the row genuinely passes a nil context without the
	// literal that staticcheck rejects (SA1012).
	var noContext context.Context
	sweep := func(context.Context) error { return nil }
	for name, run := range map[string]func() error{
		"nil context":   func() error { return RunLearningScheduler(noContext, time.Second, sweep) },
		"nil sweep":     func() error { return RunLearningScheduler(ctx, time.Second, nil) },
		"zero interval": func() error { return RunLearningScheduler(ctx, 0, sweep) },
		"negative":      func() error { return RunLearningScheduler(ctx, -time.Second, sweep) },
	} {
		if err := run(); !errors.Is(err, ErrLearningWorkerConfig) {
			t.Fatalf("%s = %v", name, err)
		}
	}
}

func TestLearningSchedulerSweepValidatesInput(t *testing.T) {
	service := &Service{}
	valid := LearningJobVersions{Model: "m", Prompt: "p", Policy: LearningAnalysisPolicyVersion}
	if _, _, err := service.QueueDueLearningReports(context.Background(), valid, 10); !errors.Is(err, ErrLearningInvalidJob) {
		t.Fatalf("nil database = %v", err)
	}
}
