package quizcraft

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// learningScheduleMaxBatch bounds one automatic sweep. Operators choose the
// batch; the repository only refuses values that would let a single sweep run
// unbounded.
const learningScheduleMaxBatch = 200

// QueueDueLearningReports queues automatic work for preferences whose next
// automatic time has passed. It never runs a model and never bypasses the
// queue repository: every row goes through QueueLearningReport, which re-reads
// consent, current reviewed content and the due time under the preference lock.
// A row the repository rejects (consent withdrawn, content retired, another
// replica already claimed it) is counted as skipped and keeps its due time, so
// the next sweep retries it; only the candidate read itself fails the sweep.
func (s *Service) QueueDueLearningReports(ctx context.Context, versions LearningJobVersions, limit int) (int, int, error) {
	if s == nil || s.database == nil || !validLearningJobVersions(versions) || limit <= 0 || limit > learningScheduleMaxBatch {
		return 0, 0, ErrLearningInvalidJob
	}
	type candidate struct{ userID, bankID uuid.UUID }
	var candidates []candidate
	rows, err := s.database.Query(ctx, `SELECT user_id,bank_id
        FROM quizcraft_learning_report_preferences
        WHERE enabled AND external_analysis_consent AND consent_version=$1
          AND next_due_at IS NOT NULL AND next_due_at <= clock_timestamp()
        ORDER BY next_due_at,user_id,bank_id LIMIT $2`, learningConsentVersion, limit)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.userID, &item.bankID); err != nil {
			rows.Close()
			return 0, 0, err
		}
		candidates = append(candidates, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, 0, err
	}
	queued, skipped := 0, 0
	for _, item := range candidates {
		if ctx.Err() != nil {
			return queued, skipped, ctx.Err()
		}
		if _, _, err := s.QueueLearningReport(ctx, item.userID, item.bankID, time.Now(), "automatic", versions); err != nil {
			if ctx.Err() != nil {
				return queued, skipped, ctx.Err()
			}
			skipped++
			continue
		}
		queued++
	}
	return queued, skipped, nil
}

// RunLearningScheduler sweeps the automatic schedule until the context is
// cancelled, then returns nil. The first sweep runs immediately; a failed
// sweep is not fatal (the next one re-reads whatever is still due), and the
// loop never runs a sweep faster than the interval.
func RunLearningScheduler(ctx context.Context, interval time.Duration, sweep func(context.Context) error) error {
	if ctx == nil || sweep == nil || interval <= 0 {
		return ErrLearningWorkerConfig
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return nil
		}
		_ = sweep(ctx)
		timer.Reset(interval)
	}
}
