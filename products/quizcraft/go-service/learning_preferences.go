package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"henukit.dev/quizcraft/internal/contract"
)

const learningConsentVersion = "v1"

var ErrLearningInvalidPreferences = errors.New("invalid learning report preferences")

const learningPreferenceColumns = `bank_id,enabled,interval_days,goal,chapter_ids,external_analysis_consent,revision,next_due_at,updated_at,consent_version`

type learningStoredPreferences struct {
	Value          contract.LearningReportPreferences
	ConsentVersion string
}

func scanLearningPreferences(row pgx.Row) (learningStoredPreferences, error) {
	var stored learningStoredPreferences
	var chapters []byte
	var goal string
	v := &stored.Value
	err := row.Scan(&v.BankId, &v.Enabled, &v.IntervalDays, &goal, &chapters, &v.ExternalAnalysisConsent, &v.Revision, &v.NextDueAt, &v.UpdatedAt, &stored.ConsentVersion)
	if err != nil {
		return stored, err
	}
	if err := json.Unmarshal(chapters, &v.ChapterIds); err != nil {
		return stored, err
	}
	if v.ChapterIds == nil {
		v.ChapterIds = []string{}
	}
	v.Goal = contract.LearningReportPreferencesGoal(goal)
	return stored, nil
}

// GetLearningReportPreferences does not create a row just to show defaults.
// The HTTP boundary must authenticate the owner and enforce read entitlement.
func (s *Service) GetLearningReportPreferences(ctx context.Context, userID, bankID uuid.UUID) (contract.LearningReportPreferences, error) {
	if userID == uuid.Nil || bankID == uuid.Nil {
		return contract.LearningReportPreferences{}, ErrLearningUnavailable
	}
	stored, err := scanLearningPreferences(s.database.QueryRow(ctx, `SELECT `+learningPreferenceColumns+` FROM quizcraft_learning_report_preferences WHERE user_id=$1 AND bank_id=$2`, userID, bankID))
	if err == nil {
		return stored.Value, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return contract.LearningReportPreferences{}, err
	}
	var exists bool
	if err := s.database.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quizcraft_banks WHERE id=$1)`, bankID).Scan(&exists); err != nil {
		return contract.LearningReportPreferences{}, err
	}
	if !exists {
		return contract.LearningReportPreferences{}, ErrLearningUnavailable
	}
	return contract.LearningReportPreferences{BankId: bankID, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}, nil
}

// lockLearningPreferences is the first lock for every consent/job mutation.
// Clear, enqueue and publication must use the same owner-before-job order.
func lockLearningPreferences(ctx context.Context, tx pgx.Tx, userID, bankID uuid.UUID) (learningStoredPreferences, error) {
	if userID == uuid.Nil || bankID == uuid.Nil {
		return learningStoredPreferences{}, ErrLearningUnavailable
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quizcraft_banks WHERE id=$1)`, bankID).Scan(&exists); err != nil {
		return learningStoredPreferences{}, err
	}
	if !exists {
		return learningStoredPreferences{}, ErrLearningUnavailable
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quizcraft_learning_report_preferences(user_id,bank_id) VALUES($1,$2) ON CONFLICT(user_id,bank_id) DO NOTHING`, userID, bankID); err != nil {
		return learningStoredPreferences{}, err
	}
	return scanLearningPreferences(tx.QueryRow(ctx, `SELECT `+learningPreferenceColumns+` FROM quizcraft_learning_report_preferences WHERE user_id=$1 AND bank_id=$2 FOR UPDATE`, userID, bankID))
}

// UpdateLearningReportPreferences requires live membership at the caller when
// enabling. Disabling remains available to the verified owner after revocation.
func (s *Service) UpdateLearningReportPreferences(ctx context.Context, userID, bankID uuid.UUID, input contract.LearningReportPreferencesUpdate) (contract.LearningReportPreferences, error) {
	if input.IntervalDays < 1 || input.IntervalDays > 30 || !input.Goal.Valid() || len(input.ChapterIds) > 50 || (input.Enabled && !input.ExternalAnalysisConsent) {
		return contract.LearningReportPreferences{}, ErrLearningInvalidPreferences
	}
	chapters := append([]string{}, input.ChapterIds...)
	sort.Strings(chapters)
	for i, id := range chapters {
		if !learningText(id, 160) || strings.TrimSpace(id) != id || (i > 0 && chapters[i-1] == id) {
			return contract.LearningReportPreferences{}, ErrLearningInvalidPreferences
		}
	}
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return contract.LearningReportPreferences{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockLearningPreferences(ctx, tx, userID, bankID)
	if err != nil {
		return contract.LearningReportPreferences{}, err
	}
	consent, consentVersion := input.Enabled && input.ExternalAnalysisConsent, ""
	if input.Enabled {
		if old.Value.Enabled && old.ConsentVersion != learningConsentVersion {
			return contract.LearningReportPreferences{}, fmt.Errorf("%w: explicitly renew outdated consent", ErrLearningInvalidPreferences)
		}
		if err := validateLearningEnabledScope(ctx, tx, bankID, chapters); err != nil {
			return contract.LearningReportPreferences{}, err
		}
		consentVersion = learningConsentVersion
	}
	previousChapters := append([]string{}, old.Value.ChapterIds...)
	sort.Strings(previousChapters)
	invalidate := old.Value.Enabled != input.Enabled || old.Value.ExternalAnalysisConsent != consent || old.Value.Goal != contract.LearningReportPreferencesGoal(input.Goal) || !slices.Equal(previousChapters, chapters) || old.ConsentVersion != consentVersion
	if !invalidate && old.Value.IntervalDays == input.IntervalDays {
		if err := tx.Commit(ctx); err != nil {
			return contract.LearningReportPreferences{}, err
		}
		return old.Value, nil
	}
	raw, err := json.Marshal(chapters)
	if err != nil {
		return contract.LearningReportPreferences{}, err
	}
	stored, err := scanLearningPreferences(tx.QueryRow(ctx, `
        UPDATE quizcraft_learning_report_preferences
        SET enabled=$3,interval_days=$4,goal=$5,chapter_ids=$6,external_analysis_consent=$7,consent_version=$8,
          revision=revision+CASE WHEN $9 THEN 1 ELSE 0 END,
          next_due_at=CASE WHEN NOT $3 THEN NULL WHEN NOT enabled OR interval_days<>$4 THEN clock_timestamp()+make_interval(days=>$4) ELSE next_due_at END,
          updated_at=clock_timestamp()
        WHERE user_id=$1 AND bank_id=$2 RETURNING `+learningPreferenceColumns, userID, bankID, input.Enabled, input.IntervalDays, string(input.Goal), raw, consent, consentVersion, invalidate))
	if err != nil {
		return contract.LearningReportPreferences{}, err
	}
	if invalidate {
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_report_jobs SET status='cancelled',lease_token=NULL,lease_until=NULL,reason_code='preferences_changed',updated_at=clock_timestamp() WHERE user_id=$1 AND bank_id=$2 AND status IN ('queued','running','failed','paused')`, userID, bankID); err != nil {
			return contract.LearningReportPreferences{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE quizcraft_learning_reports SET status='stale' WHERE user_id=$1 AND bank_id=$2 AND status<>'stale'`, userID, bankID); err != nil {
			return contract.LearningReportPreferences{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return contract.LearningReportPreferences{}, err
	}
	return stored.Value, nil
}

func validateLearningEnabledScope(ctx context.Context, tx pgx.Tx, bankID uuid.UUID, chapters []string) error {
	rows, err := tx.Query(ctx, `
        SELECT DISTINCT q.chapter_id FROM quizcraft_banks b
        JOIN quizcraft_bank_versions bv ON bv.bank_id=b.id AND bv.id=b.active_version_id AND bv.sealed_at IS NOT NULL
        JOIN quizcraft_learning_catalogs l ON l.bank_id=b.id AND l.enabled
        JOIN quizcraft_learning_content_versions c ON c.bank_id=b.id AND c.id=l.active_content_version_id AND c.bank_version_id=bv.id AND c.status='approved'
        JOIN quizcraft_bank_version_questions m ON m.bank_id=b.id AND m.bank_version_id=bv.id
        JOIN quizcraft_question_versions q ON q.bank_id=b.id AND q.id=m.question_version_id
        WHERE b.id=$1`, bankID)
	if err != nil {
		return err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		known[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(known) == 0 {
		return ErrLearningUnavailable
	}
	for _, id := range chapters {
		if !known[id] {
			return fmt.Errorf("%w: unknown chapter", ErrLearningInvalidPreferences)
		}
	}
	return nil
}

// ClearLearningReports revokes consent before deleting only derived rows.
// Keeping the preference tombstone makes earlier generations permanently stale.
// This operation must not depend on live membership or content availability.
func (s *Service) ClearLearningReports(ctx context.Context, userID, bankID uuid.UUID) (contract.LearningReportClearResult, error) {
	tx, err := s.database.Begin(ctx)
	if err != nil {
		return contract.LearningReportClearResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := lockLearningPreferences(ctx, tx, userID, bankID); err != nil {
		return contract.LearningReportClearResult{}, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE quizcraft_learning_report_preferences SET enabled=false,external_analysis_consent=false,consent_version='',revision=revision+1,next_due_at=NULL,updated_at=clock_timestamp() WHERE user_id=$1 AND bank_id=$2 RETURNING revision`, userID, bankID).Scan(&revision); err != nil {
		return contract.LearningReportClearResult{}, err
	}
	// Reports cascade from their owning job. Original sessions/attempts are not touched.
	if _, err := tx.Exec(ctx, `DELETE FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, userID, bankID); err != nil {
		return contract.LearningReportClearResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return contract.LearningReportClearResult{}, err
	}
	return contract.LearningReportClearResult{Cleared: true, Revision: revision}, nil
}
