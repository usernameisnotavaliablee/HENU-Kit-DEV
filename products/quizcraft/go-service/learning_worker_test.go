package quizcraft

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestLearningWorkerRejectsInvalidConfiguration(t *testing.T) {
	step := func(context.Context) (bool, error) { return false, nil }
	if err := RunLearningWorker(context.Background(), 0, step); err == nil {
		t.Fatal("a zero poll interval must be rejected")
	}
	if err := RunLearningWorker(context.Background(), time.Second, nil); err == nil {
		t.Fatal("a nil step must be rejected")
	}
}

func TestLearningWorkerDrainsAvailableWorkWithoutSleeping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int32
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := RunLearningWorker(ctx, time.Hour, func(context.Context) (bool, error) {
		atomic.AddInt32(&calls, 1)
		return true, nil
	})
	if err != nil {
		t.Fatalf("worker returned %v on a graceful stop", err)
	}
	if got := atomic.LoadInt32(&calls); got < 10 {
		t.Fatalf("available work must be drained immediately, only %d call(s) in 50ms", got)
	}
}

func TestLearningWorkerPollsIdleQueuesWithoutBusySpinning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int32
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	err := RunLearningWorker(ctx, 20*time.Millisecond, func(context.Context) (bool, error) {
		atomic.AddInt32(&calls, 1)
		return false, nil
	})
	if err != nil {
		t.Fatalf("worker returned %v on a graceful stop", err)
	}
	got := atomic.LoadInt32(&calls)
	if got < 2 || got > 8 {
		t.Fatalf("idle worker should poll about every interval, got %d call(s) in 100ms", got)
	}
}

func TestLearningWorkerSurvivesStepFailuresAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int32
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	err := RunLearningWorker(ctx, 10*time.Millisecond, func(context.Context) (bool, error) {
		atomic.AddInt32(&calls, 1)
		return true, errors.New("transient provider failure")
	})
	if err != nil {
		t.Fatalf("worker must not exit on a step error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("worker stopped after a step failure (%d calls)", got)
	}
}

func TestLearningWorkerHonoursAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int32
	start := time.Now()
	if err := RunLearningWorker(ctx, time.Millisecond, func(context.Context) (bool, error) {
		atomic.AddInt32(&calls, 1)
		return true, nil
	}); err != nil {
		t.Fatalf("worker returned %v for a cancelled context", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a cancelled context must not start work, waited %s", elapsed)
	}
}
