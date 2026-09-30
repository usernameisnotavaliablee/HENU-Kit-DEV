package quizcraft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"henukit.dev/quizcraft/internal/contract"
)

const LearningAnalysisPolicyVersion = "learning-analysis-v1"
const learningModelInputMaxBytes = 128 << 10
const learningModelOutputMaxBytes = 32 << 10

// LearningModelInput is the only allowed provider payload. Never marshal the
// internal snapshot into a provider request. Question text is untrusted data.
type LearningModelInput struct {
	PolicyVersion string                             `json:"policy_version"`
	Goal          string                             `json:"goal"`
	Tags          []LearningContentTag               `json:"tags"`
	Statistics    []contract.LearningReportStatistic `json:"statistics"`
	Evidence      []LearningModelEvidence            `json:"evidence"`
}

type LearningModelEvidence struct {
	EvidenceID      string   `json:"evidence_id"`
	QuestionKey     string   `json:"question_key"`
	TagIDs          []string `json:"tag_ids"`
	Question        string   `json:"question"`
	Kind            string   `json:"kind"`
	Options         []string `json:"options,omitempty"`
	SubmittedAnswer any      `json:"submitted_answer,omitempty"`
	ExpectedAnswer  any      `json:"expected_answer"`
	AnswerWithheld  bool     `json:"answer_withheld"`
	Correct         bool     `json:"correct"`
}

// The model may infer and prioritize, but cannot supply statistics, lessons,
// question IDs, URLs or actions. Those are assembled from trusted state.
type LearningModelDecision struct {
	Findings     []LearningModelFinding `json:"findings"`
	PrimaryTagID string                 `json:"primary_tag_id"`
}

type LearningModelFinding struct {
	TagID          string   `json:"tag_id"`
	Status         string   `json:"status"`
	EvidenceIDs    []string `json:"evidence_ids"`
	PossibleReason string   `json:"possible_reason"`
}

func BuildLearningModelInput(snapshot LearningEvidenceSnapshot, document LearningContentDocument) (LearningModelInput, error) {
	return buildLearningModelInput(snapshot, document, true)
}

func buildLearningModelInput(snapshot LearningEvidenceSnapshot, document LearningContentDocument, providerPayload bool) (LearningModelInput, error) {
	if err := validateLearningSnapshotSize(snapshot); err != nil {
		return LearningModelInput{}, err
	}
	fail := func(message string) (LearningModelInput, error) { return LearningModelInput{}, errors.New(message) }
	if snapshot.SchemaVersion != "learning-evidence-v2" || snapshot.BankID == uuid.Nil || snapshot.BankVersionID == uuid.Nil || snapshot.ContentVersionID == uuid.Nil || snapshot.PreferenceRevision < 1 || snapshot.Cutoff.IsZero() {
		return fail("invalid or obsolete learning snapshot")
	}
	if snapshot.Goal != "follow_course" && snapshot.Goal != "exam_review" {
		return fail("invalid learning goal")
	}
	if len(snapshot.Evidence) > learningEvidenceLimit || len(snapshot.Statistics) > 300 || len(snapshot.QuestionContexts) > learningEvidenceLimit {
		return fail("learning snapshot exceeds model bounds")
	}
	members, bindings, tags := map[uuid.UUID]uuid.UUID{}, map[uuid.UUID]LearningContentQuestion{}, map[string]LearningContentTag{}
	for _, q := range document.Questions {
		members[q.QuestionID] = q.QuestionVersionID
		bindings[q.QuestionID] = q
	}
	if err := validateLearningContent(document, members); err != nil {
		return LearningModelInput{}, err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return LearningModelInput{}, err
	}
	if hash(raw) != snapshot.ContentSHA256 {
		return fail("learning content fingerprint mismatch")
	}
	for _, tag := range document.Tags {
		tags[tag.ID] = tag
	}
	input := LearningModelInput{PolicyVersion: LearningAnalysisPolicyVersion, Goal: snapshot.Goal, Tags: []LearningContentTag{}, Statistics: append([]contract.LearningReportStatistic{}, snapshot.Statistics...), Evidence: []LearningModelEvidence{}}
	stats := map[string]contract.LearningReportStatistic{}
	for _, stat := range input.Statistics {
		tag, ok := tags[stat.TagId]
		_, duplicate := stats[stat.TagId]
		if !ok || duplicate || tag.Name != stat.Label || tag.Kind != string(stat.TagKind) || !validLearningStatistic(stat) {
			return fail("invalid authoritative learning statistic")
		}
		stats[stat.TagId] = stat
		input.Tags = append(input.Tags, tag)
	}
	contexts := map[uuid.UUID]LearningQuestionContext{}
	for _, q := range snapshot.QuestionContexts {
		_, duplicate := contexts[q.QuestionID]
		if duplicate || q.QuestionID == uuid.Nil || members[q.QuestionID] != q.QuestionVersionID || !validQuestionType(q.Kind) || len(q.Options) > 100 {
			return fail("invalid question context")
		}
		contexts[q.QuestionID] = q
	}
	seen, questionKeys := map[string]bool{}, map[uuid.UUID]string{}
	independent := map[string]map[uuid.UUID]bool{}
	for _, evidence := range snapshot.Evidence {
		binding, ok := bindings[evidence.QuestionId]
		q, hasContext := contexts[evidence.QuestionId]
		if !ok || !hasContext || binding.QuestionVersionID != evidence.QuestionVersionId || !learningText(evidence.EvidenceId, 80) || seen[evidence.EvidenceId] || evidence.SubmittedAt.After(snapshot.Cutoff) || !learningText(evidence.Question, 10000) {
			return fail("invalid scoped learning evidence")
		}
		seen[evidence.EvidenceId] = true
		if questionKeys[evidence.QuestionId] == "" {
			questionKeys[evidence.QuestionId] = fmt.Sprintf("q%d", len(questionKeys)+1)
		}
		for _, id := range binding.TagIDs {
			if _, ok := stats[id]; !ok {
				return fail("evidence tag outside statistical scope")
			}
			if independent[id] == nil {
				independent[id] = map[uuid.UUID]bool{}
			}
			independent[id][evidence.QuestionId] = true
			if int64(len(independent[id])) > stats[id].UniqueQuestionCount {
				return fail("sample exceeds authoritative independent question count")
			}
		}
		expected, valid := safeLearningAnswer(q.Kind, evidence.ExpectedAnswer, q.Options, true)
		if !valid {
			return fail("invalid authoritative expected answer")
		}
		answer, available := safeLearningAnswer(q.Kind, evidence.SubmittedAnswer, q.Options, false)
		input.Evidence = append(input.Evidence, LearningModelEvidence{EvidenceID: evidence.EvidenceId, QuestionKey: questionKeys[evidence.QuestionId], TagIDs: append([]string{}, binding.TagIDs...), Question: evidence.Question, Kind: q.Kind, Options: append([]string{}, q.Options...), SubmittedAnswer: answer, ExpectedAnswer: expected, AnswerWithheld: !available, Correct: evidence.Correct})
	}
	for tag, ids := range snapshot.PracticeCandidates {
		if _, ok := stats[tag]; !ok || len(ids) > 5 {
			return fail("practice candidates outside learning scope")
		}
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			binding, ok := bindings[id]
			found := false
			for _, bound := range binding.TagIDs {
				if bound == tag {
					found = true
				}
			}
			if !ok || !found || seen[id] {
				return fail("invalid existing practice candidate")
			}
			seen[id] = true
		}
	}
	raw, err = json.Marshal(input)
	if err != nil {
		return LearningModelInput{}, err
	}
	if providerPayload && len(raw) > learningModelInputMaxBytes {
		return fail("learning model input exceeds 128 KiB; do not silently truncate evidence")
	}
	return input, nil
}

func validLearningStatistic(s contract.LearningReportStatistic) bool {
	return s.AttemptCount >= 0 && s.UniqueQuestionCount >= 0 && s.UniqueQuestionCount <= s.AttemptCount && s.FirstCorrectCount >= 0 && s.FirstCorrectCount <= s.UniqueQuestionCount && s.LatestCorrectCount >= 0 && s.LatestCorrectCount <= s.UniqueQuestionCount && s.RepeatAttemptCount == s.AttemptCount-s.UniqueQuestionCount && s.RepeatCorrectCount >= 0 && s.RepeatCorrectCount <= s.RepeatAttemptCount
}

func safeLearningAnswer(kind string, value any, options []string, reference bool) (any, bool) {
	if kind == "blank" && !reference {
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 4096 {
		return nil, false
	}
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return nil, false
	}
	optionValues := make([]any, len(options))
	for i, option := range options {
		optionValues[i] = option
	}
	switch kind {
	case "single":
		index, ok := canonicalChoiceValue(decoded, optionValues)
		if !ok {
			return nil, false
		}
		n, err := strconv.Atoi(index)
		if err == nil {
			return n, true
		}
	case "multi":
		items, ok := decoded.([]any)
		if !ok || len(items) > 100 {
			return nil, false
		}
		canonical, ok := canonicalChoiceValues(items, optionValues)
		if !ok {
			return nil, false
		}
		result := make([]int, 0, len(canonical))
		for _, value := range canonical {
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, false
			}
			result = append(result, n)
		}
		return result, true
	case "judge":
		normalized, ok := normalizeJudge(decoded)
		if ok {
			return normalized, true
		}
	case "blank":
		if text, ok := decoded.(string); ok && learningText(text, 1000) {
			return text, true
		}
		if items, ok := decoded.([]any); ok && len(items) > 0 && len(items) <= 16 {
			for _, item := range items {
				text, ok := item.(string)
				if !ok || !learningText(text, 1000) {
					return nil, false
				}
			}
			return items, true
		}
	}
	return nil, false
}

func ValidateLearningModelDecision(raw []byte, snapshot LearningEvidenceSnapshot, document LearningContentDocument) (LearningModelDecision, error) {
	input, err := BuildLearningModelInput(snapshot, document)
	if err != nil {
		return LearningModelDecision{}, err
	}
	if len(raw) == 0 || len(raw) > learningModelOutputMaxBytes {
		return LearningModelDecision{}, errors.New("invalid model decision size")
	}
	var decision LearningModelDecision
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return LearningModelDecision{}, fmt.Errorf("decode model decision: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return LearningModelDecision{}, err
	}
	if decision.Findings == nil || len(decision.Findings) > 3 {
		return LearningModelDecision{}, errors.New("model must return zero to three explicit findings")
	}
	stats := map[string]contract.LearningReportStatistic{}
	evidence := map[string]LearningModelEvidence{}
	for _, stat := range input.Statistics {
		stats[stat.TagId] = stat
	}
	for _, e := range input.Evidence {
		evidence[e.EvidenceID] = e
	}
	seen := map[string]bool{}
	for _, finding := range decision.Findings {
		stat, ok := stats[finding.TagID]
		if !ok || seen[finding.TagID] || len(finding.EvidenceIDs) > learningEvidenceLimit {
			return LearningModelDecision{}, errors.New("invalid or duplicate model tag")
		}
		seen[finding.TagID] = true
		cited, wrongQuestions := map[string]bool{}, map[string]bool{}
		for _, id := range finding.EvidenceIDs {
			e, ok := evidence[id]
			matches := false
			for _, tag := range e.TagIDs {
				if tag == finding.TagID {
					matches = true
				}
			}
			if !ok || !matches || cited[id] {
				return LearningModelDecision{}, errors.New("model evidence is foreign, unrelated or duplicated")
			}
			cited[id] = true
			if !e.Correct {
				wrongQuestions[e.QuestionKey] = true
			}
		}
		switch finding.Status {
		case "supported":
			// Conservative support for an observed error pattern, not an
			// ability probability. Human calibration remains a release gate.
			if stat.UniqueQuestionCount < 3 || len(wrongQuestions) < 2 {
				return LearningModelDecision{}, errors.New("insufficient independent evidence for a supported weakness")
			}
		case "tentative":
			if stat.UniqueQuestionCount == 0 || len(cited) == 0 {
				return LearningModelDecision{}, errors.New("tentative inference needs observed evidence")
			}
		case "uncovered":
			if stat.UniqueQuestionCount != 0 || len(cited) != 0 || finding.PossibleReason != "" {
				return LearningModelDecision{}, errors.New("uncovered is not an inferred weakness")
			}
		default:
			return LearningModelDecision{}, errors.New("invalid finding support status")
		}
		if finding.PossibleReason != "" {
			if !learningText(finding.PossibleReason, 1000) || !strings.HasPrefix(finding.PossibleReason, "可能") {
				return LearningModelDecision{}, errors.New("model explanations must be explicitly hypothetical")
			}
			for _, r := range finding.PossibleReason {
				if unicode.IsNumber(r) || r == '%' {
					return LearningModelDecision{}, errors.New("model may not invent numeric narrative; server renders statistics")
				}
			}
		}
	}
	if (len(decision.Findings) == 0 && decision.PrimaryTagID != "") || (len(decision.Findings) > 0 && !seen[decision.PrimaryTagID]) {
		return LearningModelDecision{}, errors.New("primary action must refer to a selected finding")
	}
	return decision, nil
}
