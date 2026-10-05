package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"henukit.dev/quizcraft/internal/contract"
)

var (
	ErrLearningInvalidJob = errors.New("invalid learning report job")
	// ErrLearningRateLimited is abuse protection for member-requested
	// generation: it is not a quota, no credits are consumed, and scheduled
	// (automatic) generation is never limited by it.
	ErrLearningRateLimited = errors.New("too many learning report requests for this owner and course")
)

// LearningManualWindow is the fixed window of the member-requested generation
// guard. Counting stored jobs (not a separate counter) keeps the guard honest
// after a restart and gives it an immutable, auditable clock.
const LearningManualWindow = time.Hour

func learningRecentGenerations(ctx context.Context, query learningQuerier, userID, bankID uuid.UUID, since time.Time) (int, error) {
	var used int
	// Counts every generation for this member and course in the window,
	// automatic ones included: the point is the cost of model work, not who
	// asked for it.
	err := query.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs
        WHERE user_id=$1 AND bank_id=$2 AND created_at > $3`, userID, bankID, since).Scan(&used)
	if err != nil {
		return 0, fmt.Errorf("count learning generations: %w", err)
	}
	return used, nil
}

// Versions are server configuration, not caller-provided HTTP fields. The
// policy is compiled into the evaluator; changing model/prompt invalidates work.
type LearningJobVersions struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Policy string `json:"policy"`
}

type LearningJobSnapshot struct {
	Evidence LearningEvidenceSnapshot `json:"evidence"`
	Versions LearningJobVersions      `json:"versions"`
}

func validLearningJobVersions(v LearningJobVersions) bool {
	return v.Policy == LearningAnalysisPolicyVersion &&
		learningText(v.Model, 160) && learningText(v.Prompt, 160) &&
		strings.TrimSpace(v.Model) == v.Model && strings.TrimSpace(v.Prompt) == v.Prompt &&
		!strings.ContainsAny(v.Model+v.Prompt, "\r\n\x00")
}

func learningJobInputSHA(snapshot LearningJobSnapshot) (string, error) {
	evidence, versions := snapshot.Evidence, snapshot.Versions
	key, err := json.Marshal(struct {
		Evidence string
		Content  uuid.UUID
		Model    string
		Prompt   string
		Policy   string
	}{evidence.InputSHA256, evidence.ContentVersionID, versions.Model, versions.Prompt, versions.Policy})
	if err != nil {
		return "", err
	}
	return hash(key), nil
}

// Apply under the owner's preference lock; the content may change between
// evidence capture and enqueue, or during an external model call.
func learningCurrentContent(ctx context.Context, tx pgx.Tx, bankID uuid.UUID, evidence LearningEvidenceSnapshot) error {
	var contentVersion, bankVersion uuid.UUID
	var digest string
	err := tx.QueryRow(ctx, `SELECT c.id,c.bank_version_id,c.content_sha256
        FROM quizcraft_learning_catalogs l
        JOIN quizcraft_learning_content_versions c ON c.bank_id=l.bank_id AND c.id=l.active_content_version_id
        JOIN quizcraft_banks b ON b.id=c.bank_id AND b.active_version_id=c.bank_version_id
        JOIN quizcraft_bank_versions bv ON bv.bank_id=b.id AND bv.id=b.active_version_id AND bv.sealed_at IS NOT NULL
        WHERE l.bank_id=$1 AND l.enabled AND c.status='approved'
        FOR SHARE OF l,c,b`, bankID).Scan(&contentVersion, &bankVersion, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLearningUnavailable
	}
	if err != nil {
		return err
	}
	if contentVersion != evidence.ContentVersionID || bankVersion != evidence.BankVersionID || digest != evidence.ContentSHA256 {
		return ErrLearningUnavailable
	}
	return nil
}

func learningJobCurrent(p learningStoredPreferences, snapshot LearningJobSnapshot) bool {
	e := snapshot.Evidence
	return p.Value.Enabled && p.Value.ExternalAnalysisConsent && p.ConsentVersion == learningConsentVersion &&
		p.Value.Revision == e.PreferenceRevision && p.Value.BankId == e.BankID &&
		p.Value.Goal == contract.LearningReportPreferencesGoal(e.Goal) && slices.Equal(p.Value.ChapterIds, e.ChapterIDs)
}

// QueueLearningReport is an internal repository operation. The HTTP/scheduler
// caller MUST verify the owner and live lifetime entitlement before entry.
// Model invocation, if any, happens later and never within this transaction.
// reused=true means this immutable effective input already has a job.
func (s *Service) QueueLearningReport(ctx context.Context, userID, bankID uuid.UUID, cutoff time.Time, source string, versions LearningJobVersions) (contract.LearningReportTask, bool, error) {
	empty := contract.LearningReportTask{}
	if (source != "manual" && source != "automatic") || !validLearningJobVersions(versions) || cutoff.IsZero() || cutoff.After(time.Now().Add(time.Minute)) {
		return empty, false, ErrLearningInvalidJob
	}
	evidence, err := s.BuildLearningEvidence(ctx, userID, bankID, cutoff)
	if err != nil {
		return empty, false, err
	}
	snapshot := LearningJobSnapshot{Evidence: evidence, Versions: versions}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return empty, false, err
	}
	if len(raw) > learningSnapshotMaxBytes {
		return empty, false, ErrLearningInvalidJob
	}
	inputSHA, err := learningJobInputSHA(snapshot)
	if err != nil {
		return empty, false, err
	}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return empty, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := lockLearningPreferences(ctx, tx, userID, bankID)
	if err != nil {
		return empty, false, err
	}
	p := stored.Value
	if !learningJobCurrent(stored, snapshot) {
		return empty, false, ErrLearningUnavailable
	}
	if source == "automatic" && (p.NextDueAt == nil || p.NextDueAt.After(time.Now())) {
		return empty, false, ErrLearningUnavailable
	}
	if err := learningCurrentContent(ctx, tx, bankID, evidence); err != nil {
		return empty, false, err
	}
	task := contract.LearningReportTask{BankId: bankID}
	var reason string
	var runAfter time.Time
	reused := true
	err = tx.QueryRow(ctx, `SELECT id,status,created_at,reason_code,run_after FROM quizcraft_learning_report_jobs
        WHERE user_id=$1 AND bank_id=$2 AND preference_revision=$3 AND input_sha256=$4 FOR UPDATE`,
		userID, bankID, p.Revision, inputSHA).Scan(&task.TaskId, &task.Status, &task.CreatedAt, &reason, &runAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		reused = false
		// The guard runs inside the owner's preference lock and only when a new
		// job would be written, so a replay of an existing request still reuses
		// its job, and two concurrent requests cannot both pass the limit.
		if source == "manual" && s.learningManualLimit > 0 {
			used, err := learningRecentGenerations(ctx, tx, userID, bankID, time.Now().Add(-LearningManualWindow))
			if err != nil {
				return empty, false, err
			}
			if used >= s.learningManualLimit {
				return empty, false, ErrLearningRateLimited
			}
		}
		task.TaskId = uuid.New()
		err = tx.QueryRow(ctx, `INSERT INTO quizcraft_learning_report_jobs(id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot)
            VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING status,created_at`,
			task.TaskId, userID, bankID, p.Revision, evidence.ContentVersionID, inputSHA, raw).Scan(&task.Status, &task.CreatedAt)
	}
	if err != nil {
		return empty, false, fmt.Errorf("queue learning report: %w", err)
	}
	switch task.Status {
	case "failed", "paused":
		if runAfter.After(time.Now()) {
			retry := int(time.Until(runAfter).Seconds()) + 1
			task.RetryAfterSeconds = &retry
		} else {
			if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='queued',attempts=0,reason_code='',run_after=clock_timestamp(),updated_at=clock_timestamp()
                WHERE id=$1`, task.TaskId); err != nil {
				return empty, false, err
			}
			task.Status, reason, reused = "queued", "", false
		}
	case "cancelled":
		return empty, false, ErrLearningUnavailable
	}
	if !reused {
		// New effective input supersedes in-flight work. Keep existing ready
		// reports until replacement succeeds; do not serve them as latest then.
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs
            SET status='cancelled',lease_token=NULL,lease_until=NULL,reason_code='superseded',updated_at=clock_timestamp()
            WHERE user_id=$1 AND bank_id=$2 AND preference_revision=$3 AND id<>$4 AND status IN ('queued','running')`,
			userID, bankID, p.Revision, task.TaskId); err != nil {
			return empty, false, err
		}
	}
	if reason != "" {
		task.ReasonCode = &reason
	}
	if task.Status == "ready" {
		var reportID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM quizcraft_learning_reports WHERE job_id=$1 AND status<>'stale'`, task.TaskId).Scan(&reportID); err != nil {
			return empty, false, fmt.Errorf("ready learning report missing: %w", err)
		}
		task.ReportId = &reportID
	}
	if source == "automatic" && task.Status != "failed" && task.Status != "paused" {
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET next_due_at=clock_timestamp()+make_interval(days=>interval_days),updated_at=clock_timestamp() WHERE user_id=$1 AND bank_id=$2`, userID, bankID); err != nil {
			return empty, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, false, err
	}
	return task, reused, nil
}
