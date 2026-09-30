package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"henukit.dev/quizcraft/internal/contract"
)

// LearningEntitlementCheck must make a fresh, fail-closed Account Portfolio
// lifetime check. A synthetic callback belongs only in tests, never in a
// production worker. Network access happens before the short DB transaction.
type LearningEntitlementCheck func(context.Context, uuid.UUID) (bool, error)

// PublishLearningReport accepts only a current lease, model/prompt policy and
// entitlement. It composes from the immutable DB snapshot and reviewed content,
// not worker-supplied JSON. Duplicate publication with the same valid token
// returns the existing report. Model/provider calls never run inside this tx.
func (s *Service) PublishLearningReport(ctx context.Context, lease LearningJobLease, modelResult []byte, current LearningJobVersions, entitled LearningEntitlementCheck) (contract.LearningReport, error) {
	empty := contract.LearningReport{}
	if lease.JobID == uuid.Nil || lease.UserID == uuid.Nil || lease.BankID == uuid.Nil || lease.LeaseToken == uuid.Nil || !validLearningJobVersions(current) {
		return empty, ErrLearningInvalidJob
	}
	if entitled == nil {
		return empty, ErrLearningUnavailable
	}
	hasLifetime, err := entitled(ctx, lease.UserID)
	if err != nil || !hasLifetime {
		return empty, ErrLearningUnavailable
	}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	pref, err := lockLearningPreferences(ctx, tx, lease.UserID, lease.BankID)
	if err != nil {
		return empty, err
	}
	var status, key string
	var leaseToken *uuid.UUID
	var until *time.Time
	var revision int64
	var contentID uuid.UUID
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT status,lease_token,lease_until,preference_revision,content_version_id,input_sha256,snapshot
        FROM quizcraft_learning_report_jobs WHERE id=$1 AND user_id=$2 AND bank_id=$3 FOR UPDATE`, lease.JobID, lease.UserID, lease.BankID).
		Scan(&status, &leaseToken, &until, &revision, &contentID, &key, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrLearningLeaseLost
	}
	if err != nil {
		return empty, err
	}
	var job LearningJobSnapshot
	if len(raw) > learningSnapshotMaxBytes || json.Unmarshal(raw, &job) != nil || job.Versions != current ||
		job.Evidence.SchemaVersion != "learning-evidence-v2" || job.Evidence.BankID != lease.BankID || job.Evidence.ContentVersionID != contentID || job.Evidence.PreferenceRevision != revision ||
		!learningJobCurrent(pref, job) || validateLearningSnapshotSize(job.Evidence) != nil {
		return empty, ErrLearningLeaseLost
	}
	calculated, err := learningJobInputSHA(job)
	if err != nil || calculated != key {
		return empty, ErrLearningLeaseLost
	}
	if err := learningCurrentContent(ctx, tx, lease.BankID, job.Evidence); err != nil {
		if errors.Is(err, ErrLearningUnavailable) {
			return empty, ErrLearningLeaseLost
		}
		return empty, err
	}
	if status == "ready" {
		var body []byte
		err = tx.QueryRow(ctx, `SELECT body FROM quizcraft_learning_reports
            WHERE job_id=$1 AND lease_token=$2 AND status<>'stale'`, lease.JobID, lease.LeaseToken).Scan(&body)
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrLearningLeaseLost
		}
		if err != nil {
			return empty, err
		}
		var report contract.LearningReport
		if err := json.Unmarshal(body, &report); err != nil {
			return empty, err
		}
		if report.BankId != lease.BankID || report.ContentVersionId != contentID || report.EvidenceUntil != job.Evidence.Cutoff {
			return empty, ErrLearningLeaseLost
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return report, nil
	}
	if status != "running" || leaseToken == nil || *leaseToken != lease.LeaseToken || until == nil || !until.After(time.Now()) {
		return empty, ErrLearningLeaseLost
	}
	var documentRaw []byte
	err = tx.QueryRow(ctx, `SELECT document FROM quizcraft_learning_content_versions WHERE bank_id=$1 AND id=$2 AND status='approved'`, lease.BankID, contentID).Scan(&documentRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrLearningLeaseLost
	}
	if err != nil {
		return empty, err
	}
	if len(documentRaw) > learningContentMaxBytes {
		return empty, ErrLearningUnavailable
	}
	var document LearningContentDocument
	if err := json.Unmarshal(documentRaw, &document); err != nil {
		return empty, err
	}
	report, err := ComposeLearningReport(job.Evidence, document, modelResult, uuid.New(), time.Now().UTC())
	if err != nil {
		return empty, err
	}
	body, err := json.Marshal(report)
	if err != nil {
		return empty, err
	}
	if len(body) > 2<<20 {
		return empty, ErrLearningUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO quizcraft_learning_reports(id,job_id,user_id,bank_id,content_version_id,lease_token,evidence_until,status,body,created_at)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, report.ReportId, lease.JobID, lease.UserID, lease.BankID, contentID, lease.LeaseToken, report.EvidenceUntil, string(report.Status), body, report.CreatedAt)
	if err != nil {
		return empty, err
	}
	if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='ready',lease_token=NULL,lease_until=NULL,reason_code='',updated_at=clock_timestamp() WHERE id=$1`, lease.JobID); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return report, nil
}
