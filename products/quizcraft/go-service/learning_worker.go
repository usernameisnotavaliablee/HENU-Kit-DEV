package quizcraft

import (
	"context"
	"errors"
	"time"
)

var ErrLearningWorkerConfig = errors.New("invalid learning report worker configuration")

// LearningWorkerStep performs at most one bounded unit of work and reports
// whether it did anything useful. ProcessNextLearningReport is the production
// step; tests inject a plain function instead of a database.
type LearningWorkerStep func(context.Context) (bool, error)

// RunLearningWorker keeps the report queue moving until the context is
// cancelled, then returns nil. Available work is drained without sleeping; an
// empty queue or a failed step waits one poll interval. Step failures are not
// fatal here: the lease repository already persisted the retry/backoff state,
// so the loop must not exit (and must not retry faster than the interval).
func RunLearningWorker(ctx context.Context, poll time.Duration, step LearningWorkerStep) error {
	if ctx == nil || step == nil || poll <= 0 {
		return ErrLearningWorkerConfig
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		processed, err := step(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil && processed {
			continue
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
