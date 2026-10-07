package quizcraft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"henukit.dev/quizcraft/internal/contract"
	"henukit.dev/quizcraft/internal/store"
)

// learningReportPracticeMode is the pinned session mode for questions selected
// by a published report. It is deliberately distinct from random/difficult/
// chapter/favorites so the client can describe the set honestly.
const learningReportPracticeMode = "report"

// A report may only re-serve questions that are still part of the published
// bank version; a shrunken set is reported through the excluded count.
const learningReportPracticeMaxQuestions = 200

// ErrLearningNoPracticeRecommendation means the report recommends a lesson,
// nothing, or only questions that are no longer published.
var ErrLearningNoPracticeRecommendation = errors.New("learning report has no practice recommendation")

const learningReportQuestionColumns = `m.question_id,m.question_version_id,qv.type,qv.chapter_id,qv.chapter_name,qv.content,COALESCE(qv.options,'null'::jsonb) AS options`

// learningPracticeQuestionRow is one still-published question of a report's
// recommendation, in the column order used by learningReportQuestionColumns.
type learningPracticeQuestionRow struct {
	QuestionID        uuid.UUID
	QuestionVersionID uuid.UUID
	Type              string
	ChapterID         string
	ChapterName       string
	Content           string
	Options           []byte
}

// learningReportSessionSource revalidates the report and returns the questions
// that are still available in the published bank version, in report order.
func learningReportSessionSource(ctx context.Context, tx pgx.Tx, bankID, reportID, userID uuid.UUID) ([]learningPracticeQuestionRow, uuid.UUID, int, error) {
	var status string
	var contentVersionID, bankVersionID uuid.UUID
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT r.status,r.content_version_id,r.body,b.active_version_id FROM quizcraft_learning_reports r
        JOIN quizcraft_learning_content_versions c ON c.bank_id=r.bank_id AND c.id=r.content_version_id
        JOIN quizcraft_learning_catalogs l ON l.bank_id=c.bank_id AND l.active_content_version_id=c.id
        JOIN quizcraft_banks b ON b.id=c.bank_id AND b.active_version_id=c.bank_version_id
        WHERE r.id=$1 AND r.user_id=$2 AND r.bank_id=$3 AND r.status<>'stale' AND c.status='approved' AND l.enabled
        FOR UPDATE OF r`, reportID, userID, bankID).Scan(&status, &contentVersionID, &raw, &bankVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, uuid.Nil, 0, ErrLearningReportNotFound
	}
	if err != nil {
		return nil, uuid.Nil, 0, err
	}
	if len(raw) > learningReportMaxBytes {
		return nil, uuid.Nil, 0, ErrLearningUnavailable
	}
	var report contract.LearningReport
	if json.Unmarshal(raw, &report) != nil || report.ReportId != reportID || report.BankId != bankID || report.ContentVersionId != contentVersionID {
		return nil, uuid.Nil, 0, ErrLearningUnavailable
	}
	// Only a practice or diagnostic recommendation names questions. A lesson-only
	// or no-action report must not silently become a session.
	if report.NextStep.Kind != contract.Practice && report.NextStep.Kind != contract.Diagnostic {
		return nil, uuid.Nil, 0, ErrLearningNoPracticeRecommendation
	}
	if report.NextStep.QuestionIds == nil || len(*report.NextStep.QuestionIds) == 0 || len(*report.NextStep.QuestionIds) > learningReportPracticeMaxQuestions {
		return nil, uuid.Nil, 0, ErrLearningNoPracticeRecommendation
	}
	questionIDs := make([]uuid.UUID, 0, len(*report.NextStep.QuestionIds))
	seen := map[uuid.UUID]bool{}
	for _, id := range *report.NextStep.QuestionIds {
		if id == uuid.Nil || seen[id] {
			return nil, uuid.Nil, 0, ErrLearningUnavailable
		}
		seen[id] = true
		questionIDs = append(questionIDs, id)
	}
	rows, err := tx.Query(ctx, `SELECT `+learningReportQuestionColumns+`
        FROM quizcraft_bank_version_questions m
        JOIN quizcraft_question_versions qv
          ON qv.id=m.question_version_id AND qv.question_id=m.question_id AND qv.bank_id=m.bank_id
        WHERE m.bank_id=$1 AND m.bank_version_id=$2 AND m.question_id = ANY($3::uuid[])
        ORDER BY array_position($3::uuid[], m.question_id)`, bankID, bankVersionID, questionIDs)
	if err != nil {
		return nil, uuid.Nil, 0, err
	}
	available, err := pgx.CollectRows(rows, pgx.RowToStructByPos[learningPracticeQuestionRow])
	if err != nil {
		return nil, uuid.Nil, 0, err
	}
	return available, bankVersionID, len(questionIDs), nil
}

// createLearningReportPracticeSession pins one session to the report's
// recommendation. It runs on the caller's transaction so the idempotency record
// and the session commit together; it never accepts client question ids, never
// generates questions and never changes scoring.
func createLearningReportPracticeSession(ctx context.Context, tx pgx.Tx, userID, bankID, reportID uuid.UUID) (practiceSession, error) {
	empty := practiceSession{}
	if userID == uuid.Nil || bankID == uuid.Nil || reportID == uuid.Nil {
		return empty, ErrLearningReportNotFound
	}
	rows, bankVersionID, requested, err := learningReportSessionSource(ctx, tx, bankID, reportID, userID)
	if err != nil {
		return empty, err
	}
	if len(rows) == 0 {
		return empty, ErrLearningNoPracticeRecommendation
	}
	queries := store.New(tx)
	sessionID := uuid.New()
	if err := queries.CreatePracticeSession(ctx, store.CreatePracticeSessionParams{ID: sessionID, BankID: bankID, BankVersionID: bankVersionID, UserID: uuid.NullUUID{UUID: userID, Valid: true}, ActorKey: "user:" + userID.String(), Mode: learningReportPracticeMode}); err != nil {
		return empty, err
	}
	questions := make([]practiceQuestion, 0, len(rows))
	for index, row := range rows {
		question := practiceQuestion{QuestionID: row.QuestionID, QuestionVersionID: row.QuestionVersionID, Type: row.Type, ChapterID: row.ChapterID, Chapter: row.ChapterName, Content: row.Content}
		if question.Type == "single" || question.Type == "multi" {
			if err := json.Unmarshal(row.Options, &question.Options); err != nil {
				return empty, ErrLearningUnavailable
			}
		}
		questions = append(questions, question)
		if err := queries.AddPracticeSessionQuestion(ctx, store.AddPracticeSessionQuestionParams{SessionID: sessionID, BankID: bankID, BankVersionID: bankVersionID, QuestionID: row.QuestionID, QuestionVersionID: row.QuestionVersionID, Position: int32(index + 1)}); err != nil {
			return empty, err
		}
	}
	return practiceSession{SessionID: sessionID, BankID: bankID, BankVersionID: bankVersionID, Mode: learningReportPracticeMode, ExcludedUnavailableCount: requested - len(questions), Questions: questions}, nil
}
