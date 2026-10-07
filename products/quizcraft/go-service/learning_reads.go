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

const learningReportMaxBytes = 2 << 20

// ErrLearningReportNotFound covers both "no report was generated yet" and
// "the newest report is stale". Callers must not distinguish the two to the
// owner: a superseded report is not served as the latest one.
var ErrLearningReportNotFound = errors.New("learning report not found")

// GetLatestLearningReport returns the newest published report for this owner
// and course. Only a report whose content version is still the approved,
// active catalog version is served: a retired, withdrawn or superseded content
// version makes the report unreadable rather than serving stale advice. The
// identity fields are re-checked so a foreign or truncated row cannot be served
// as this bank's report. A newest row marked stale means the derived data is no
// longer valid; older reports are never substituted.
func (s *Service) GetLatestLearningReport(ctx context.Context, userID, bankID uuid.UUID) (contract.LearningReport, error) {
	empty := contract.LearningReport{}
	if s == nil || s.database == nil || userID == uuid.Nil || bankID == uuid.Nil {
		return empty, ErrLearningInvalidJob
	}
	var status string
	var raw []byte
	err := s.database.QueryRow(ctx, `SELECT r.status,r.body FROM quizcraft_learning_reports r
        JOIN quizcraft_learning_content_versions c ON c.bank_id=r.bank_id AND c.id=r.content_version_id
        JOIN quizcraft_learning_catalogs l ON l.bank_id=c.bank_id AND l.active_content_version_id=c.id
        JOIN quizcraft_banks b ON b.id=c.bank_id AND b.active_version_id=c.bank_version_id
        WHERE r.user_id=$1 AND r.bank_id=$2 AND c.status='approved' AND l.enabled
        ORDER BY r.created_at DESC, r.id DESC LIMIT 1`, userID, bankID).Scan(&status, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrLearningReportNotFound
	}
	if err != nil {
		return empty, err
	}
	if status == "stale" {
		return empty, ErrLearningReportNotFound
	}
	if len(raw) > learningReportMaxBytes {
		return empty, ErrLearningUnavailable
	}
	var report contract.LearningReport
	if json.Unmarshal(raw, &report) != nil || report.ReportId == uuid.Nil || report.BankId != bankID {
		return empty, ErrLearningUnavailable
	}
	return report, nil
}

// GetLearningReportTask returns one task owned by this user and course. It is
// the owner-facing progress read: it exposes no snapshot, evidence, model
// input or other user's row, and never resurrects a cancelled task.
func (s *Service) GetLearningReportTask(ctx context.Context, userID, bankID, taskID uuid.UUID) (contract.LearningReportTask, error) {
	empty := contract.LearningReportTask{}
	if s == nil || s.database == nil || userID == uuid.Nil || bankID == uuid.Nil || taskID == uuid.Nil {
		return empty, ErrLearningInvalidJob
	}
	task := contract.LearningReportTask{BankId: bankID, TaskId: taskID}
	var reason string
	var runAfter time.Time
	err := s.database.QueryRow(ctx, `SELECT status,created_at,reason_code,run_after FROM quizcraft_learning_report_jobs
        WHERE id=$1 AND user_id=$2 AND bank_id=$3`, taskID, userID, bankID).
		Scan(&task.Status, &task.CreatedAt, &reason, &runAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, ErrLearningReportNotFound
	}
	if err != nil {
		return empty, err
	}
	if reason != "" {
		task.ReasonCode = &reason
	}
	if (task.Status == "failed" || task.Status == "paused") && runAfter.After(time.Now()) {
		retry := int(time.Until(runAfter).Seconds()) + 1
		task.RetryAfterSeconds = &retry
	}
	if task.Status == "ready" {
		var reportID uuid.UUID
		if err := s.database.QueryRow(ctx, `SELECT id FROM quizcraft_learning_reports
            WHERE job_id=$1 AND status<>'stale'`, taskID).Scan(&reportID); err != nil {
			return empty, err
		}
		task.ReportId = &reportID
	}
	return task, nil
}
