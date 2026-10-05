package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Learning content is imported as a draft and only becomes usable after a named
// operator reviews it. These errors are the only outcomes the Workshop maps to
// 400/404/409; everything else is an infrastructure failure.
var (
	ErrLearningContentInvalid  = errors.New("invalid learning content draft")
	ErrLearningContentMissing  = errors.New("learning content was not found for this bank")
	ErrLearningContentConflict = errors.New("learning content state conflict")
)

// LearningContentVersionInfo is the operator-facing review state of one content
// version. It deliberately carries no document body: reviewers read the imported
// package, members read the published report.
type LearningContentVersionInfo struct {
	ContentVersionID uuid.UUID
	BankID           uuid.UUID
	BankVersionID    uuid.UUID
	ContentSHA256    string
	Status           string
	CreatedAt        time.Time
	ReviewedBy       *uuid.UUID
	ReviewedAt       *time.Time
	Active           bool
	CatalogEnabled   bool
	QuestionCount    int
	LessonCount      int
}

// learningQuerier is satisfied by both a pool and a transaction, so the
// review flow can run inside the Workshop idempotency transaction.
type learningQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const learningContentVersionColumns = `c.id,c.bank_id,c.bank_version_id,c.content_sha256,c.status,c.created_at,c.reviewed_by,c.reviewed_at,
        COALESCE(jsonb_array_length(NULLIF(c.document->'questions','null'::jsonb)),0),
        COALESCE(jsonb_array_length(NULLIF(c.document->'lessons','null'::jsonb)),0),
        COALESCE(l.active_content_version_id=c.id,false),COALESCE(l.enabled,false)`

const learningContentVersionFrom = `FROM quizcraft_learning_content_versions c
        LEFT JOIN quizcraft_learning_catalogs l ON l.bank_id=c.bank_id`

func scanLearningContentVersion(row pgx.Row) (LearningContentVersionInfo, error) {
	var info LearningContentVersionInfo
	err := row.Scan(&info.ContentVersionID, &info.BankID, &info.BankVersionID, &info.ContentSHA256, &info.Status, &info.CreatedAt, &info.ReviewedBy, &info.ReviewedAt, &info.QuestionCount, &info.LessonCount, &info.Active, &info.CatalogEnabled)
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	return info, nil
}

func learningContentVersion(ctx context.Context, query learningQuerier, bankID, contentID uuid.UUID) (LearningContentVersionInfo, error) {
	info, err := scanLearningContentVersion(query.QueryRow(ctx, `SELECT `+learningContentVersionColumns+` `+learningContentVersionFrom+` WHERE c.bank_id=$1 AND c.id=$2`, bankID, contentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return LearningContentVersionInfo{}, ErrLearningContentMissing
	}
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	return info, nil
}

func learningContentVersionByDigest(ctx context.Context, query learningQuerier, bankID uuid.UUID, digest string) (LearningContentVersionInfo, error) {
	info, err := scanLearningContentVersion(query.QueryRow(ctx, `SELECT `+learningContentVersionColumns+` `+learningContentVersionFrom+` WHERE c.bank_id=$1 AND c.content_sha256=$2`, bankID, digest))
	if errors.Is(err, pgx.ErrNoRows) {
		return LearningContentVersionInfo{}, ErrLearningContentMissing
	}
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	return info, nil
}

// ListLearningContentVersions returns every content version of a bank, newest
// first, with the review and activation state an operator needs to decide.
func ListLearningContentVersions(ctx context.Context, query learningQuerier, bankID uuid.UUID) ([]LearningContentVersionInfo, error) {
	if bankID == uuid.Nil {
		return nil, ErrLearningContentInvalid
	}
	var exists bool
	err := query.QueryRow(ctx, `SELECT true FROM quizcraft_banks WHERE id=$1`, bankID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLearningContentMissing
	}
	if err != nil {
		return nil, err
	}
	rows, err := query.Query(ctx, `SELECT `+learningContentVersionColumns+` `+learningContentVersionFrom+` WHERE c.bank_id=$1 ORDER BY c.created_at DESC,c.id`, bankID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []LearningContentVersionInfo{}
	for rows.Next() {
		info, err := scanLearningContentVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

// ImportLearningContentDraft stores a candidate package as a draft. The
// membership map is loaded from the bank's published version, never from the
// import payload, and the reviewer fields stay empty until an operator reviews.
func ImportLearningContentDraft(ctx context.Context, query learningQuerier, bankID, contentID uuid.UUID, raw []byte) (LearningContentVersionInfo, error) {
	if bankID == uuid.Nil || contentID == uuid.Nil || len(raw) == 0 || len(raw) > learningContentMaxBytes {
		return LearningContentVersionInfo{}, ErrLearningContentInvalid
	}
	bankVersionID, members, err := learningPublishedMembership(ctx, query, bankID)
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	document, digest, err := ParseLearningContent(raw, members)
	if err != nil {
		return LearningContentVersionInfo{}, fmt.Errorf("%w: %v", ErrLearningContentInvalid, err)
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	// The same package for the same bank is the same draft, not a second one.
	// This lookup replaces relying on the unique violation: an aborted statement
	// would poison the surrounding Workshop idempotency transaction.
	existing, err := learningContentVersionByDigest(ctx, query, bankID, digest)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrLearningContentMissing) {
		return LearningContentVersionInfo{}, err
	}
	if _, err := query.Exec(ctx, `INSERT INTO quizcraft_learning_content_versions(id,bank_id,bank_version_id,content_sha256,document,status)
        VALUES($1,$2,$3,$4,$5,'draft')`, contentID, bankID, bankVersionID, digest, canonical); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return LearningContentVersionInfo{}, fmt.Errorf("%w: the same package is being imported concurrently", ErrLearningContentConflict)
		}
		return LearningContentVersionInfo{}, err
	}
	return learningContentVersion(ctx, query, bankID, contentID)
}

// ApproveLearningContent records the human review of a draft. Activation is a
// separate, explicit choice: only an activation guard checked against the bank's
// current published version keeps a superseded package out of the catalog.
func ApproveLearningContent(ctx context.Context, query learningQuerier, bankID, contentID, reviewer uuid.UUID, activate, enable bool, note string) (LearningContentVersionInfo, error) {
	if bankID == uuid.Nil || contentID == uuid.Nil || reviewer == uuid.Nil || !learningReviewNote(note) {
		return LearningContentVersionInfo{}, ErrLearningContentInvalid
	}
	var bankVersionID uuid.UUID
	var status string
	err := query.QueryRow(ctx, `SELECT bank_version_id,status FROM quizcraft_learning_content_versions WHERE bank_id=$1 AND id=$2 FOR UPDATE`, bankID, contentID).Scan(&bankVersionID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return LearningContentVersionInfo{}, ErrLearningContentMissing
	}
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	if status != "draft" {
		return LearningContentVersionInfo{}, fmt.Errorf("%w: learning content is already %s", ErrLearningContentConflict, status)
	}
	if activate {
		activeVersionID, err := learningBankActiveVersion(ctx, query, bankID)
		if err != nil {
			return LearningContentVersionInfo{}, err
		}
		if activeVersionID != bankVersionID {
			return LearningContentVersionInfo{}, fmt.Errorf("%w: learning content was drafted against a superseded bank version", ErrLearningContentConflict)
		}
	}
	result, err := query.Exec(ctx, `UPDATE quizcraft_learning_content_versions SET status='approved',reviewed_by=$3,reviewed_at=now()
        WHERE bank_id=$1 AND id=$2 AND status='draft'`, bankID, contentID, reviewer)
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	if result.RowsAffected() != 1 {
		return LearningContentVersionInfo{}, fmt.Errorf("%w: learning content was reviewed concurrently", ErrLearningContentConflict)
	}
	if activate {
		// An already enabled course stays enabled when a newer version is activated.
		if _, err := query.Exec(ctx, `INSERT INTO quizcraft_learning_catalogs(bank_id,active_content_version_id,enabled,updated_at)
            VALUES($1,$2,$3,now())
            ON CONFLICT(bank_id) DO UPDATE SET active_content_version_id=excluded.active_content_version_id,enabled=quizcraft_learning_catalogs.enabled OR excluded.enabled,updated_at=now()`, bankID, contentID, enable); err != nil {
			return LearningContentVersionInfo{}, err
		}
	}
	if err := insertLearningContentReview(ctx, query, bankID, contentID, reviewer, "approve", note); err != nil {
		return LearningContentVersionInfo{}, err
	}
	return learningContentVersion(ctx, query, bankID, contentID)
}

// RetireLearningContent retires approved content. Content that is still the
// active catalog version must be replaced first: retiring it in place would keep
// the course enabled while every read silently fails the approved-version join.
func RetireLearningContent(ctx context.Context, query learningQuerier, bankID, contentID, reviewer uuid.UUID, note string) (LearningContentVersionInfo, error) {
	if bankID == uuid.Nil || contentID == uuid.Nil || reviewer == uuid.Nil || !learningReviewNote(note) {
		return LearningContentVersionInfo{}, ErrLearningContentInvalid
	}
	var status string
	err := query.QueryRow(ctx, `SELECT status FROM quizcraft_learning_content_versions WHERE bank_id=$1 AND id=$2 FOR UPDATE`, bankID, contentID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return LearningContentVersionInfo{}, ErrLearningContentMissing
	}
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	if status != "approved" {
		return LearningContentVersionInfo{}, fmt.Errorf("%w: learning content is %s", ErrLearningContentConflict, status)
	}
	var activeContentID *uuid.UUID
	err = query.QueryRow(ctx, `SELECT active_content_version_id FROM quizcraft_learning_catalogs WHERE bank_id=$1 FOR UPDATE`, bankID).Scan(&activeContentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LearningContentVersionInfo{}, err
	}
	if activeContentID != nil && *activeContentID == contentID {
		return LearningContentVersionInfo{}, fmt.Errorf("%w: activate a replacement version before retiring this content", ErrLearningContentConflict)
	}
	result, err := query.Exec(ctx, `UPDATE quizcraft_learning_content_versions SET status='retired' WHERE bank_id=$1 AND id=$2 AND status='approved'`, bankID, contentID)
	if err != nil {
		return LearningContentVersionInfo{}, err
	}
	if result.RowsAffected() != 1 {
		return LearningContentVersionInfo{}, fmt.Errorf("%w: learning content was reviewed concurrently", ErrLearningContentConflict)
	}
	if err := insertLearningContentReview(ctx, query, bankID, contentID, reviewer, "retire", note); err != nil {
		return LearningContentVersionInfo{}, err
	}
	return learningContentVersion(ctx, query, bankID, contentID)
}

func insertLearningContentReview(ctx context.Context, query learningQuerier, bankID, contentID, reviewer uuid.UUID, action, note string) error {
	_, err := query.Exec(ctx, `INSERT INTO quizcraft_learning_content_reviews(id,bank_id,content_version_id,actor_user_id,action,note)
        VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), bankID, contentID, reviewer, action, strings.TrimSpace(note))
	return err
}

// learningPublishedMembership is the authoritative question-version membership
// of the bank's published version. Import validation must never trust the
// payload for this.
func learningPublishedMembership(ctx context.Context, query learningQuerier, bankID uuid.UUID) (uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	bankVersionID, err := learningBankActiveVersion(ctx, query, bankID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	rows, err := query.Query(ctx, `SELECT m.question_id,m.question_version_id FROM quizcraft_bank_version_questions m
        WHERE m.bank_id=$1 AND m.bank_version_id=$2 ORDER BY m.question_id`, bankID, bankVersionID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	defer rows.Close()
	members := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var questionID, versionID uuid.UUID
		if err := rows.Scan(&questionID, &versionID); err != nil {
			return uuid.Nil, nil, err
		}
		members[questionID] = versionID
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, nil, err
	}
	if len(members) == 0 {
		return uuid.Nil, nil, fmt.Errorf("%w: bank has no published question membership", ErrLearningUnavailable)
	}
	return bankVersionID, members, nil
}

func learningBankActiveVersion(ctx context.Context, query learningQuerier, bankID uuid.UUID) (uuid.UUID, error) {
	var bankVersionID uuid.UUID
	err := query.QueryRow(ctx, `SELECT b.active_version_id FROM quizcraft_banks b
        JOIN quizcraft_bank_versions bv ON bv.bank_id=b.id AND bv.id=b.active_version_id AND bv.sealed_at IS NOT NULL
        WHERE b.id=$1`, bankID).Scan(&bankVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		var known bool
		if lookupErr := query.QueryRow(ctx, `SELECT true FROM quizcraft_banks WHERE id=$1`, bankID).Scan(&known); lookupErr != nil {
			if errors.Is(lookupErr, pgx.ErrNoRows) {
				return uuid.Nil, ErrLearningContentMissing
			}
			return uuid.Nil, lookupErr
		}
		return uuid.Nil, fmt.Errorf("%w: publish the bank version before reviewing learning content", ErrLearningUnavailable)
	}
	if err != nil {
		return uuid.Nil, err
	}
	return bankVersionID, nil
}

func learningReviewNote(note string) bool {
	return len([]rune(strings.TrimSpace(note))) <= 1000
}
