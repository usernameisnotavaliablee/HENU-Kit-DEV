package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const learningMaxAutomaticAttempts = 3

var ErrLearningLeaseLost = errors.New("learning report lease expired or revoked")

type LearningJobLease struct {
	JobID      uuid.UUID
	UserID     uuid.UUID
	BankID     uuid.UUID
	LeaseToken uuid.UUID
	LeaseUntil time.Time
	Attempts   int
	Snapshot   LearningJobSnapshot
}

func validLearningLeaseDuration(duration time.Duration) bool {
	return duration >= 15*time.Second && duration <= 10*time.Minute && duration%time.Second == 0
}

// ClaimNextLearningReport is repository-only. Workers MUST verify live lifetime
// entitlement before model work and again at publication; no provider call
// happens under these short preference->job transactions.
func (s *Service) ClaimNextLearningReport(ctx context.Context, duration time.Duration) (LearningJobLease, bool, error) {
	if !validLearningLeaseDuration(duration) {
		return LearningJobLease{}, false, ErrLearningInvalidJob
	}
	rows, err := s.database.Query(ctx, `SELECT id,user_id,bank_id FROM quizcraft_learning_report_jobs
        WHERE (status='queued' AND run_after<=clock_timestamp()) OR (status='running' AND lease_until<=clock_timestamp())
        ORDER BY run_after,created_at,id LIMIT 32`)
	if err != nil {
		return LearningJobLease{}, false, err
	}
	type candidate struct{ id, user, bank uuid.UUID }
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.user, &c.bank); err != nil {
			rows.Close()
			return LearningJobLease{}, false, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return LearningJobLease{}, false, err
	}
	for _, c := range candidates {
		lease, ok, err := s.claimLearningJob(ctx, c.id, c.user, c.bank, duration)
		if err != nil || ok {
			return lease, ok, err
		}
	}
	return LearningJobLease{}, false, nil
}

func (s *Service) claimLearningJob(ctx context.Context, jobID, userID, bankID uuid.UUID, duration time.Duration) (LearningJobLease, bool, error) {
	empty := LearningJobLease{}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return empty, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := lockLearningPreferences(ctx, tx, userID, bankID)
	if err != nil {
		return empty, false, err
	}
	var status, key string
	var attempts int
	var until *time.Time
	var runAfter time.Time
	var revision int64
	var contentID uuid.UUID
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT status,attempts,lease_until,run_after,preference_revision,content_version_id,input_sha256,snapshot
        FROM quizcraft_learning_report_jobs WHERE id=$1 AND user_id=$2 AND bank_id=$3 FOR UPDATE`, jobID, userID, bankID).
		Scan(&status, &attempts, &until, &runAfter, &revision, &contentID, &key, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	// The candidate list was read without a lock. Recheck eligibility after
	// acquiring the preference and job rows: another worker may have started
	// a new lease or moved an expired one into its retry cooldown.
	if status == "running" && until != nil && until.After(time.Now()) {
		return empty, false, nil
	}
	if status == "queued" && runAfter.After(time.Now()) {
		return empty, false, nil
	}
	if status != "queued" && status != "running" {
		return empty, false, nil
	}
	// Expiry releases the old token before any other worker can claim work.
	// Attempts count actual lease grants, never successful HTTP retries.
	if attempts >= learningMaxAutomaticAttempts || status == "running" {
		nextStatus, reason, delay := "queued", "lease_expired", attempts
		if attempts >= learningMaxAutomaticAttempts {
			nextStatus, reason, delay = "failed", "lease_exhausted", 5
		}
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs
            SET status=$2,lease_token=NULL,lease_until=NULL,run_after=clock_timestamp()+make_interval(mins=>$3),reason_code=$4,updated_at=clock_timestamp() WHERE id=$1`, jobID, nextStatus, delay, reason); err != nil {
			return empty, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, false, err
		}
		return empty, false, nil
	}
	var snapshot LearningJobSnapshot
	invalid := len(raw) > learningSnapshotMaxBytes || json.Unmarshal(raw, &snapshot) != nil || !validLearningJobVersions(snapshot.Versions) ||
		snapshot.Evidence.SchemaVersion != "learning-evidence-v2" || snapshot.Evidence.Cutoff.IsZero() ||
		snapshot.Evidence.BankID != bankID || snapshot.Evidence.ContentVersionID != contentID || snapshot.Evidence.PreferenceRevision != revision ||
		!learningHex(snapshot.Evidence.InputSHA256, 64)
	if !invalid {
		var calculated string
		calculated, err = learningJobInputSHA(snapshot)
		invalid = err != nil || calculated != key || validateLearningSnapshotSize(snapshot.Evidence) != nil
	}
	if invalid {
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='failed',lease_token=NULL,lease_until=NULL,reason_code='invalid_snapshot',run_after=clock_timestamp()+interval '5 minutes',updated_at=clock_timestamp() WHERE id=$1`, jobID); err != nil {
			return empty, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, false, err
		}
		return empty, false, nil
	}
	if !learningJobCurrent(stored, snapshot) {
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='cancelled',lease_token=NULL,lease_until=NULL,reason_code='consent_revoked',updated_at=clock_timestamp() WHERE id=$1`, jobID); err != nil {
			return empty, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, false, err
		}
		return empty, false, nil
	}
	err = learningCurrentContent(ctx, tx, bankID, snapshot.Evidence)
	if errors.Is(err, ErrLearningUnavailable) {
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='cancelled',lease_token=NULL,lease_until=NULL,reason_code='content_changed',updated_at=clock_timestamp() WHERE id=$1`, jobID); err != nil {
			return empty, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, false, err
		}
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	result := LearningJobLease{JobID: jobID, UserID: userID, BankID: bankID, LeaseToken: uuid.New(), Snapshot: snapshot}
	err = tx.QueryRow(ctx, `UPDATE quizcraft_learning_report_jobs SET status='running',attempts=attempts+1,lease_token=$2,
        lease_until=clock_timestamp()+make_interval(secs=>$3),reason_code='',updated_at=clock_timestamp()
        WHERE id=$1 RETURNING attempts,lease_until`, jobID, result.LeaseToken, int(duration/time.Second)).Scan(&result.Attempts, &result.LeaseUntil)
	if err != nil {
		return empty, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, false, err
	}
	return result, true, nil
}

// RenewLearningLease does not revive an expired/cleared/superseded token.
func (s *Service) RenewLearningLease(ctx context.Context, lease LearningJobLease, duration time.Duration) (time.Time, error) {
	if !validLearningLeaseDuration(duration) || lease.JobID == uuid.Nil || lease.LeaseToken == uuid.Nil || lease.UserID == uuid.Nil || lease.BankID == uuid.Nil {
		return time.Time{}, ErrLearningInvalidJob
	}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := lockLearningPreferences(ctx, tx, lease.UserID, lease.BankID)
	if err != nil {
		return time.Time{}, err
	}
	var raw []byte
	var contentID uuid.UUID
	var revision int64
	var key string
	err = tx.QueryRow(ctx, `SELECT snapshot,content_version_id,preference_revision,input_sha256 FROM quizcraft_learning_report_jobs WHERE id=$1 AND user_id=$2 AND bank_id=$3 AND status='running' AND lease_token=$4 AND lease_until>clock_timestamp() FOR UPDATE`, lease.JobID, lease.UserID, lease.BankID, lease.LeaseToken).
		Scan(&raw, &contentID, &revision, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrLearningLeaseLost
	}
	if err != nil {
		return time.Time{}, err
	}
	var snapshot LearningJobSnapshot
	if len(raw) > learningSnapshotMaxBytes || json.Unmarshal(raw, &snapshot) != nil || snapshot.Evidence.ContentVersionID != contentID || snapshot.Evidence.PreferenceRevision != revision || !learningJobCurrent(stored, snapshot) {
		return time.Time{}, ErrLearningLeaseLost
	}
	digest, err := learningJobInputSHA(snapshot)
	if err != nil || digest != key || validateLearningSnapshotSize(snapshot.Evidence) != nil {
		return time.Time{}, ErrLearningLeaseLost
	}
	if err := learningCurrentContent(ctx, tx, lease.BankID, snapshot.Evidence); err != nil {
		if errors.Is(err, ErrLearningUnavailable) {
			return time.Time{}, ErrLearningLeaseLost
		}
		return time.Time{}, err
	}
	var until time.Time
	err = tx.QueryRow(ctx, `UPDATE quizcraft_learning_report_jobs SET lease_until=GREATEST(lease_until,clock_timestamp()+make_interval(secs=>$2)),updated_at=clock_timestamp() WHERE id=$1 RETURNING lease_until`, lease.JobID, int(duration/time.Second)).Scan(&until)
	if err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return until, nil
}

// FailLearningLease handles bounded provider retries and permanent revocation
// or policy/content changes under the current lease token. Paused work may be
// explicitly requeued after its cooldown; cancelled work is never revived.
func (s *Service) FailLearningLease(ctx context.Context, lease LearningJobLease, reason string) (string, error) {
	switch reason {
	case "provider_error", "provider_timeout", "invalid_model_result", "entitlement_unavailable", "entitlement_revoked", "policy_changed", "content_changed", "worker_error":
	default:
		return "", ErrLearningInvalidJob
	}
	if lease.JobID == uuid.Nil || lease.LeaseToken == uuid.Nil || lease.UserID == uuid.Nil || lease.BankID == uuid.Nil {
		return "", ErrLearningInvalidJob
	}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockLearningPreferences(ctx, tx, lease.UserID, lease.BankID); err != nil {
		return "", err
	}
	var attempts int
	err = tx.QueryRow(ctx, `SELECT attempts FROM quizcraft_learning_report_jobs
        WHERE id=$1 AND user_id=$2 AND bank_id=$3 AND status='running' AND lease_token=$4 AND lease_until>clock_timestamp() FOR UPDATE`,
		lease.JobID, lease.UserID, lease.BankID, lease.LeaseToken).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLearningLeaseLost
	}
	if err != nil {
		return "", err
	}
	next, delay := "queued", attempts
	if reason == "entitlement_unavailable" {
		next, delay = "paused", 5
	}
	if reason == "entitlement_revoked" || reason == "policy_changed" || reason == "content_changed" {
		next, delay = "cancelled", 0
	} else if attempts >= learningMaxAutomaticAttempts {
		next, delay = "failed", 5
	}
	_, err = tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status=$2,lease_token=NULL,lease_until=NULL,
        run_after=clock_timestamp()+make_interval(mins=>$3),reason_code=$4,updated_at=clock_timestamp() WHERE id=$1`, lease.JobID, next, delay, reason)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return next, nil
}
