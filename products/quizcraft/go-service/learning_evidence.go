package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"henukit.dev/quizcraft/internal/contract"
)

const (
	learningEvidenceLimit       = 24
	learningAnswerMaxBytes      = 4 << 10
	learningSnapshotMaxBytes    = 1 << 20
	learningQuestionSetMaxBytes = 16 << 20
)

var ErrLearningUnavailable = errors.New("learning feedback is not enabled for the owner and published course")

// LearningEvidenceSnapshot is internal derived data, NOT a provider request.
// The model adapter must construct its own minimized, identity-free allowlist.
type LearningEvidenceSnapshot struct {
	SchemaVersion      string                             `json:"schema_version"`
	BankID             uuid.UUID                          `json:"bank_id"`
	BankVersionID      uuid.UUID                          `json:"bank_version_id"`
	ContentVersionID   uuid.UUID                          `json:"content_version_id"`
	ContentSHA256      string                             `json:"content_sha256"`
	PreferenceRevision int64                              `json:"preference_revision"`
	Goal               string                             `json:"goal"`
	ChapterIDs         []string                           `json:"chapter_ids"`
	Cutoff             time.Time                          `json:"cutoff"`
	Statistics         []contract.LearningReportStatistic `json:"statistics"`
	Evidence           []contract.LearningReportEvidence  `json:"evidence"`
	QuestionContexts   []LearningQuestionContext          `json:"question_contexts"`
	PracticeCandidates map[string][]uuid.UUID             `json:"practice_candidates"`
	InputSHA256        string                             `json:"input_sha256"`
}

type LearningQuestionContext struct {
	QuestionID        uuid.UUID `json:"question_id"`
	QuestionVersionID uuid.UUID `json:"question_version_id"`
	Kind              string    `json:"kind"`
	Options           []string  `json:"options"`
}

// Compact identities and aggregates bind the whole scoped history without
// persisting unsampled raw answers, question text or option sets in the hash.
type learningFactFingerprint struct {
	QuestionID    uuid.UUID
	VersionID     uuid.UUID
	Attempts      int64
	Correct       int64
	FirstID       string
	LatestID      string
	FirstAt       time.Time
	LatestAt      time.Time
	FirstCorrect  bool
	LatestCorrect bool
}

type learningQuestionEvidence struct {
	QuestionID uuid.UUID
	VersionID  uuid.UUID
	ChapterID  string
	Question   string
	Kind       string
	Options    []string
	TagIDs     []string
	Attempts   int64
	Correct    int64
	First      contract.LearningReportEvidence
	Latest     contract.LearningReportEvidence
	FirstID    string
	LatestID   string
}

// BuildLearningEvidence reads immutable answer facts for one opted-in owner.
// Authentication and live lifetime entitlement remain caller responsibilities.
// No model call is made here and no transaction may outlive this method.
func (s *Service) BuildLearningEvidence(ctx context.Context, userID, bankID uuid.UUID, cutoff time.Time) (LearningEvidenceSnapshot, error) {
	result := LearningEvidenceSnapshot{SchemaVersion: "learning-evidence-v2", BankID: bankID, Cutoff: cutoff.UTC(), ChapterIDs: []string{}, Statistics: []contract.LearningReportStatistic{}, Evidence: []contract.LearningReportEvidence{}}
	result.QuestionContexts = []LearningQuestionContext{}
	result.PracticeCandidates = map[string][]uuid.UUID{}
	if userID == uuid.Nil || bankID == uuid.Nil || cutoff.IsZero() {
		return LearningEvidenceSnapshot{}, ErrLearningUnavailable
	}
	tx, err := s.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw, chaptersJSON []byte
	err = tx.QueryRow(ctx, `
        SELECT c.id,c.bank_version_id,c.content_sha256,c.document,p.goal,p.chapter_ids,p.revision
        FROM quizcraft_learning_catalogs l
        JOIN quizcraft_learning_content_versions c ON c.bank_id=l.bank_id AND c.id=l.active_content_version_id
        JOIN quizcraft_banks b ON b.id=c.bank_id AND b.active_version_id=c.bank_version_id
        JOIN quizcraft_bank_versions bv ON bv.bank_id=b.id AND bv.id=b.active_version_id AND bv.sealed_at IS NOT NULL
        JOIN quizcraft_learning_report_preferences p ON p.bank_id=b.id AND p.user_id=$2
        WHERE l.bank_id=$1 AND l.enabled AND c.status='approved' AND p.enabled AND p.external_analysis_consent AND p.consent_version=$3`,
		bankID, userID, learningConsentVersion).Scan(&result.ContentVersionID, &result.BankVersionID, &result.ContentSHA256, &raw, &result.Goal, &chaptersJSON, &result.PreferenceRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return LearningEvidenceSnapshot{}, ErrLearningUnavailable
	}
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	if err := json.Unmarshal(chaptersJSON, &result.ChapterIDs); err != nil {
		return LearningEvidenceSnapshot{}, fmt.Errorf("decode learning scope: %w", err)
	}
	if result.ChapterIDs == nil {
		result.ChapterIDs = []string{}
	}
	if len(result.ChapterIDs) > 50 {
		return LearningEvidenceSnapshot{}, errors.New("learning scope exceeds 50 chapters")
	}
	sort.Strings(result.ChapterIDs)

	rows, err := tx.Query(ctx, `SELECT q.question_id,q.id,q.chapter_id,q.content,q.type,COALESCE(q.options,'[]'::jsonb) FROM quizcraft_bank_version_questions m JOIN quizcraft_question_versions q ON q.bank_id=m.bank_id AND q.question_id=m.question_id AND q.id=m.question_version_id WHERE m.bank_id=$1 AND m.bank_version_id=$2 ORDER BY q.question_id`, bankID, result.BankVersionID)
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	allQuestions := map[uuid.UUID]learningQuestionEvidence{}
	questionBytes := 0
	members, knownChapters := map[uuid.UUID]uuid.UUID{}, map[string]bool{}
	for rows.Next() {
		var q learningQuestionEvidence
		if err := rows.Scan(&q.QuestionID, &q.VersionID, &q.ChapterID, &q.Question, &q.Kind, &q.Options); err != nil {
			rows.Close()
			return LearningEvidenceSnapshot{}, err
		}
		questionBytes += len(q.Question)
		for _, option := range q.Options {
			questionBytes += len(option)
		}
		if len(allQuestions) >= 10000 || questionBytes > learningQuestionSetMaxBytes {
			rows.Close()
			return LearningEvidenceSnapshot{}, errors.New("learning question set exceeds resource bounds")
		}
		allQuestions[q.QuestionID], members[q.QuestionID], knownChapters[q.ChapterID] = q, q.VersionID, true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	chapters := map[string]bool{}
	for _, chapter := range result.ChapterIDs {
		if !knownChapters[chapter] || chapters[chapter] {
			return LearningEvidenceSnapshot{}, errors.New("unknown or duplicate learning chapter")
		}
		chapters[chapter] = true
	}
	document, digest, err := ParseLearningContent(raw, members)
	if err != nil {
		return LearningEvidenceSnapshot{}, fmt.Errorf("invalid stored learning content: %w", err)
	}
	if digest != result.ContentSHA256 {
		return LearningEvidenceSnapshot{}, errors.New("learning content checksum mismatch")
	}
	questions := map[uuid.UUID]learningQuestionEvidence{}
	ids, scopedTags := []uuid.UUID{}, map[string]bool{}
	for _, binding := range document.Questions {
		q := allQuestions[binding.QuestionID]
		if len(chapters) != 0 && !chapters[q.ChapterID] {
			continue
		}
		q.TagIDs = append([]string{}, binding.TagIDs...)
		sort.Strings(q.TagIDs)
		questions[q.QuestionID] = q
		ids = append(ids, q.QuestionID)
		for _, id := range q.TagIDs {
			scopedTags[id] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	rows, err = tx.Query(ctx, learningEvidenceFactsSQL, userID, bankID, result.BankVersionID, result.Cutoff, ids, learningAnswerMaxBytes)
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	for rows.Next() {
		var id uuid.UUID
		var attempts, correct int64
		var firstID, latestID string
		var first, latest contract.LearningReportEvidence
		var oversized bool
		if err := rows.Scan(&id, &attempts, &correct, &firstID, &first.SubmittedAt, &first.Correct, &latestID, &latest.SubmittedAt, &latest.Correct, &oversized); err != nil {
			rows.Close()
			return LearningEvidenceSnapshot{}, err
		}
		if oversized {
			rows.Close()
			return LearningEvidenceSnapshot{}, errors.New("learning answer exceeds 4096 bytes")
		}
		q, ok := questions[id]
		if !ok {
			rows.Close()
			return LearningEvidenceSnapshot{}, errors.New("unexpected evidence question")
		}
		first.EvidenceId, latest.EvidenceId = "e_"+hash([]byte(firstID))[:32], "e_"+hash([]byte(latestID))[:32]
		first.QuestionId, latest.QuestionId = q.QuestionID, q.QuestionID
		first.QuestionVersionId, latest.QuestionVersionId = q.VersionID, q.VersionID
		first.Question, latest.Question = q.Question, q.Question
		q.Attempts, q.Correct, q.First, q.Latest = attempts, correct, first, latest
		q.FirstID, q.LatestID = firstID, latestID
		questions[id] = q
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	stats := map[string]contract.LearningReportStatistic{}
	for _, tag := range document.Tags {
		if len(chapters) != 0 && !scopedTags[tag.ID] {
			continue
		}
		stats[tag.ID] = contract.LearningReportStatistic{TagId: tag.ID, TagKind: contract.LearningReportStatisticTagKind(tag.Kind), Label: tag.Name}
	}
	facts := make([]learningQuestionEvidence, 0, len(ids))
	for _, id := range ids {
		q := questions[id]
		facts = append(facts, q)
		if q.Attempts == 0 {
			continue
		}
		for _, tag := range q.TagIDs {
			stat := stats[tag]
			stat.AttemptCount += q.Attempts
			stat.UniqueQuestionCount++
			stat.RepeatAttemptCount += q.Attempts - 1
			stat.RepeatCorrectCount += q.Correct
			if q.First.Correct {
				stat.FirstCorrectCount++
				stat.RepeatCorrectCount--
			}
			if q.Latest.Correct {
				stat.LatestCorrectCount++
			}
			stats[tag] = stat
		}
	}
	for _, stat := range stats {
		result.Statistics = append(result.Statistics, stat)
	}
	sort.Slice(result.Statistics, func(i, j int) bool { return result.Statistics[i].TagId < result.Statistics[j].TagId })
	result.Evidence = selectLearningEvidence(facts, result.Statistics)
	addLearningPracticeContext(&result, facts)
	if err := loadLearningSampleAnswers(ctx, tx, userID, bankID, &result, facts); err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	if err := validateLearningSnapshotSize(result); err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	// Wall-clock cutoff alone must not create new work. Include every scoped
	// question's aggregate, not just the limited displayed sample. The final
	// job key must additionally bind the configured model/prompt/policy versions.
	fingerprint := result
	fingerprint.Cutoff = time.Time{}
	// Only immutable ids, timestamps, correctness and aggregates bind unsampled
	// history. Never hash or serialize every user's raw answer into the job key.
	compact := make([]learningFactFingerprint, 0, len(facts))
	for _, q := range facts {
		compact = append(compact, learningFactFingerprint{q.QuestionID, q.VersionID, q.Attempts, q.Correct, q.FirstID, q.LatestID, q.First.SubmittedAt, q.Latest.SubmittedAt, q.First.Correct, q.Latest.Correct})
	}
	canonical, err := json.Marshal(struct {
		Snapshot LearningEvidenceSnapshot
		Facts    []learningFactFingerprint
	}{fingerprint, compact})
	if err != nil {
		return LearningEvidenceSnapshot{}, err
	}
	result.InputSHA256 = hash(canonical)
	return result, nil
}

func selectLearningEvidence(facts []learningQuestionEvidence, stats []contract.LearningReportStatistic) []contract.LearningReportEvidence {
	samples := []contract.LearningReportEvidence{}
	byTag := map[string][]contract.LearningReportEvidence{}
	for _, q := range facts {
		if q.Attempts == 0 {
			continue
		}
		for _, tag := range q.TagIDs {
			byTag[tag] = append(byTag[tag], q.First)
			if q.Latest.EvidenceId != q.First.EvidenceId {
				byTag[tag] = append(byTag[tag], q.Latest)
			}
		}
	}
	for _, items := range byTag {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Correct != items[j].Correct {
				return !items[i].Correct
			}
			if !items[i].SubmittedAt.Equal(items[j].SubmittedAt) {
				return items[i].SubmittedAt.After(items[j].SubmittedAt)
			}
			return items[i].EvidenceId < items[j].EvidenceId
		})
	}
	priority := append([]contract.LearningReportStatistic{}, stats...)
	sort.Slice(priority, func(i, j int) bool {
		left, right := priority[i], priority[j]
		// This orders examples, not a diagnostic ability score.
		a, b := left.UniqueQuestionCount-left.FirstCorrectCount, right.UniqueQuestionCount-right.FirstCorrectCount
		if a != b {
			return a > b
		}
		return left.TagId < right.TagId
	})
	seen := map[string]bool{}
	for len(samples) < learningEvidenceLimit {
		progress := false
		for _, stat := range priority {
			items := byTag[stat.TagId]
			for len(items) > 0 && seen[items[0].EvidenceId] {
				items = items[1:]
			}
			byTag[stat.TagId] = items
			if len(items) == 0 {
				continue
			}
			samples = append(samples, items[0])
			seen[items[0].EvidenceId] = true
			byTag[stat.TagId] = items[1:]
			progress = true
			if len(samples) == learningEvidenceLimit {
				break
			}
		}
		if !progress {
			break
		}
	}
	return samples
}

// Current membership is matched by QUESTION version: an unrelated bank revision
// must not erase valid history for an unchanged question. Ties use immutable ID.
const learningEvidenceFactsSQL = `
WITH ranked AS (
    SELECT a.question_id,a.id,a.correct,a.submitted_at,
      octet_length(a.submitted_answer::text) AS submitted_bytes,
      octet_length(a.expected_answer::text) AS expected_bytes,
      row_number() OVER (PARTITION BY a.question_id ORDER BY a.submitted_at,a.id) AS first_no,
      row_number() OVER (PARTITION BY a.question_id ORDER BY a.submitted_at DESC,a.id DESC) AS latest_no
    FROM quizcraft_practice_attempts a
    JOIN quizcraft_bank_version_questions m ON m.bank_id=a.bank_id AND m.question_id=a.question_id
      AND m.question_version_id=a.question_version_id AND m.bank_version_id=$3
    WHERE a.user_id=$1 AND a.bank_id=$2 AND a.submitted_at<=$4 AND a.question_id=ANY($5::uuid[]) AND a.expected_answer<>'null'::jsonb
)
SELECT question_id,count(*),count(*) FILTER (WHERE correct),
    max(id::text) FILTER (WHERE first_no=1),max(submitted_at) FILTER (WHERE first_no=1),bool_or(correct) FILTER (WHERE first_no=1),
    max(id::text) FILTER (WHERE latest_no=1),max(submitted_at) FILTER (WHERE latest_no=1),bool_or(correct) FILTER (WHERE latest_no=1),
    bool_or(submitted_bytes>$6 OR expected_bytes>$6) FILTER (WHERE first_no=1 OR latest_no=1)
FROM ranked GROUP BY question_id ORDER BY question_id`

// Fetch only the <=24 sampled answers. CASE keeps oversized values server-side;
// NULL results fail closed rather than silently turning truncated text into evidence.
func loadLearningSampleAnswers(ctx context.Context, tx pgx.Tx, userID, bankID uuid.UUID, result *LearningEvidenceSnapshot, facts []learningQuestionEvidence) error {
	if len(result.Evidence) == 0 {
		return nil
	}
	requested := map[uuid.UUID]*contract.LearningReportEvidence{}
	sampleByEvidence := map[string]*contract.LearningReportEvidence{}
	for i := range result.Evidence {
		sampleByEvidence[result.Evidence[i].EvidenceId] = &result.Evidence[i]
	}
	for _, q := range facts {
		if sample := sampleByEvidence[q.First.EvidenceId]; sample != nil {
			id, err := uuid.Parse(q.FirstID)
			if err != nil {
				return err
			}
			requested[id] = sample
		}
		if sample := sampleByEvidence[q.Latest.EvidenceId]; sample != nil {
			id, err := uuid.Parse(q.LatestID)
			if err != nil {
				return err
			}
			requested[id] = sample
		}
	}
	ids := make([]uuid.UUID, 0, len(requested))
	for id := range requested {
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `SELECT id,
        CASE WHEN octet_length(submitted_answer::text)<=$2 THEN submitted_answer END,
        CASE WHEN octet_length(expected_answer::text)<=$2 THEN expected_answer END
        FROM quizcraft_practice_attempts WHERE id=ANY($1::uuid[]) AND user_id=$3 AND bank_id=$4`, ids, learningAnswerMaxBytes, userID, bankID)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var id uuid.UUID
		var submitted, expected json.RawMessage
		if err := rows.Scan(&id, &submitted, &expected); err != nil {
			rows.Close()
			return err
		}
		sample := requested[id]
		if sample == nil || len(submitted) == 0 || len(expected) == 0 || len(submitted) > learningAnswerMaxBytes || len(expected) > learningAnswerMaxBytes {
			rows.Close()
			return errors.New("learning answer exceeds 4096 bytes or is unavailable")
		}
		sample.SubmittedAnswer, sample.ExpectedAnswer = submitted, expected
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != len(requested) {
		return errors.New("learning sampled answer missing")
	}
	return nil
}

func validateLearningSnapshotSize(snapshot LearningEvidenceSnapshot) error {
	for _, evidence := range snapshot.Evidence {
		for _, answer := range []any{evidence.SubmittedAnswer, evidence.ExpectedAnswer} {
			encoded, err := json.Marshal(answer)
			if err != nil {
				return err
			}
			if len(encoded) > learningAnswerMaxBytes {
				return errors.New("learning answer exceeds 4096 bytes")
			}
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(raw) > learningSnapshotMaxBytes {
		return errors.New("learning snapshot exceeds 1 MiB")
	}
	return nil
}

// Candidates remain internal: the model cannot choose arbitrary question IDs.
func addLearningPracticeContext(snapshot *LearningEvidenceSnapshot, facts []learningQuestionEvidence) {
	sampled := map[uuid.UUID]bool{}
	for _, sample := range snapshot.Evidence {
		sampled[sample.QuestionId] = true
	}
	for _, q := range facts {
		if sampled[q.QuestionID] {
			snapshot.QuestionContexts = append(snapshot.QuestionContexts, LearningQuestionContext{QuestionID: q.QuestionID, QuestionVersionID: q.VersionID, Kind: q.Kind, Options: q.Options})
		}
	}
	candidates := append([]learningQuestionEvidence{}, facts...)
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.Attempts == 0) != (b.Attempts == 0) {
			return a.Attempts == 0
		}
		if a.Latest.Correct != b.Latest.Correct {
			return !a.Latest.Correct
		}
		if a.Attempts != b.Attempts {
			return a.Attempts < b.Attempts
		}
		return a.QuestionID.String() < b.QuestionID.String()
	})
	for _, q := range candidates {
		for _, tag := range q.TagIDs {
			if len(snapshot.PracticeCandidates[tag]) < 5 {
				snapshot.PracticeCandidates[tag] = append(snapshot.PracticeCandidates[tag], q.QuestionID)
			}
		}
	}
}
