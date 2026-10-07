package quizcraft

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	learningContentMaxBytes  = 4 << 20
	learningReviewRepository = "https://github.com/jry21223/HENU-Final-Review"
)

var learningContentIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,159}$`)

// LearningContentDocument is an importable draft, never an approval receipt.
// Reviewer identity, state and timestamps are server-owned database fields.
type LearningContentDocument struct {
	SchemaVersion int                       `json:"schema_version"`
	Tags          []LearningContentTag      `json:"tags"`
	Questions     []LearningContentQuestion `json:"questions"`
	Sources       []LearningContentSource   `json:"sources"`
	Lessons       []LearningContentLesson   `json:"lessons"`
}

type LearningContentTag struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

type LearningContentQuestion struct {
	QuestionID        uuid.UUID `json:"question_id"`
	QuestionVersionID uuid.UUID `json:"question_version_id"`
	TagIDs            []string  `json:"tag_ids"`
}

type LearningContentSource struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Locator    string `json:"locator"`
	UsageBasis string `json:"usage_basis"`
}

type LearningContentLesson struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	TagIDs    []string `json:"tag_ids"`
	SourceIDs []string `json:"source_ids"`
}

// ParseLearningContent validates a bounded draft against an authoritative
// published bank-version membership map. Callers must load this map themselves,
// not accept it from import JSON. A successful parse grants no usage rights or
// publication permission; those require the separate authenticated review flow.
func ParseLearningContent(raw []byte, published map[uuid.UUID]uuid.UUID) (LearningContentDocument, string, error) {
	var document LearningContentDocument
	if len(raw) == 0 || len(raw) > learningContentMaxBytes {
		return document, "", errors.New("learning content must be between 1 byte and 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return LearningContentDocument{}, "", fmt.Errorf("decode learning content: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return LearningContentDocument{}, "", err
	}
	if err := validateLearningContent(document, published); err != nil {
		return LearningContentDocument{}, "", err
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return LearningContentDocument{}, "", err
	}
	return document, hash(canonical), nil
}

func validateLearningContent(document LearningContentDocument, published map[uuid.UUID]uuid.UUID) error {
	if document.SchemaVersion != 1 || len(document.Tags) < 1 || len(document.Tags) > 300 || len(document.Questions) < 1 || len(document.Questions) > 10000 || len(document.Sources) > 1000 || len(document.Lessons) > 300 {
		return errors.New("unsupported learning schema or content collection size")
	}
	tags := make(map[string]bool, len(document.Tags))
	for i, tag := range document.Tags {
		if !learningContentIDPattern.MatchString(tag.ID) || tags[tag.ID] || (tag.Kind != "knowledge" && tag.Kind != "ability") || !learningText(tag.Name, 240) || !learningText(tag.Definition, 1000) {
			return fmt.Errorf("tags[%d]: invalid or duplicate learning tag", i)
		}
		tags[tag.ID] = true
	}
	questions := make(map[uuid.UUID]bool, len(document.Questions))
	for i, question := range document.Questions {
		version, member := published[question.QuestionID]
		if !member || question.QuestionID == uuid.Nil || question.QuestionVersionID == uuid.Nil || version != question.QuestionVersionID || questions[question.QuestionID] || !learningReferences(question.TagIDs, tags) {
			return fmt.Errorf("questions[%d]: invalid version, membership or tags", i)
		}
		questions[question.QuestionID] = true
	}
	sources := make(map[string]bool, len(document.Sources))
	for i, source := range document.Sources {
		if !learningContentIDPattern.MatchString(source.ID) || sources[source.ID] || !learningText(source.Title, 240) || source.Repository != learningReviewRepository || !learningHex(source.Commit, 40) || !learningHex(source.SHA256, 64) || !learningSourcePath(source.Path) || !learningText(source.Locator, 1000) || !learningText(source.Path+" · "+source.Locator, 500) || !learningText(source.UsageBasis, 2000) {
			return fmt.Errorf("sources[%d]: missing immutable provenance or usage basis", i)
		}
		sources[source.ID] = true
	}
	lessons := make(map[string]bool, len(document.Lessons))
	for i, lesson := range document.Lessons {
		if !learningContentIDPattern.MatchString(lesson.ID) || lessons[lesson.ID] || !learningText(lesson.Title, 240) || !learningText(lesson.Body, 10000) || !learningReferences(lesson.TagIDs, tags) || !learningReferences(lesson.SourceIDs, sources) {
			return fmt.Errorf("lessons[%d]: invalid reviewed-lesson candidate or references", i)
		}
		lessons[lesson.ID] = true
	}
	return nil
}

func learningText(value string, max int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= max
}

func learningHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func learningSourcePath(value string) bool {
	return learningText(value, 2000) && !path.IsAbs(value) && path.Clean(value) == value && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsRune(value, 92) && !strings.ContainsAny(value, "%?#")
}

func learningReferences(ids []string, known map[string]bool) bool {
	if len(ids) < 1 || len(ids) > 16 {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !known[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
