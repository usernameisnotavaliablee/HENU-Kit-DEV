package quizcraft

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Synthetic data exercises validation only; it is not reviewed course content.
func learningContentFixture() (LearningContentDocument, map[uuid.UUID]uuid.UUID) {
	question, version := uuid.New(), uuid.New()
	return LearningContentDocument{
		SchemaVersion: 1,
		Tags:          []LearningContentTag{{ID: "loops", Kind: "knowledge", Name: "循环", Definition: "判断循环边界"}},
		Questions:     []LearningContentQuestion{{QuestionID: question, QuestionVersionID: version, TagIDs: []string{"loops"}}},
		Sources:       []LearningContentSource{{ID: "source-1", Title: "测试资料", Repository: learningReviewRepository, Commit: strings.Repeat("a", 40), Path: "test/loops.md", SHA256: strings.Repeat("b", 64), Locator: "循环边界章节", UsageBasis: "合成测试声明，非真实授权"}},
		Lessons:       []LearningContentLesson{{ID: "lesson-1", Title: "循环边界", Body: "合成测试讲解", TagIDs: []string{"loops"}, SourceIDs: []string{"source-1"}}},
	}, map[uuid.UUID]uuid.UUID{question: version}
}

func TestLearningContentAcceptsScopedDraftWithoutGrantingApproval(t *testing.T) {
	doc, members := learningContentFixture()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, digest, err := ParseLearningContent(raw, members)
	if err != nil || len(digest) != 64 || got.SchemaVersion != 1 {
		t.Fatalf("parse draft: %+v %s %v", got, digest, err)
	}
	pretty, _ := json.MarshalIndent(doc, "", "  ")
	_, prettyDigest, err := ParseLearningContent(pretty, members)
	if err != nil || digest != prettyDigest {
		t.Fatal("whitespace changed content digest")
	}
	doc.Sources, doc.Lessons = nil, nil
	raw, _ = json.Marshal(doc)
	if _, _, err := ParseLearningContent(raw, members); err != nil {
		t.Fatalf("honest lesson gap rejected: %v", err)
	}
}

func TestLearningContentRejectsInvalidBindingsAndSources(t *testing.T) {
	for name, mutate := range map[string]func(*LearningContentDocument){
		"unsupported schema":           func(d *LearningContentDocument) { d.SchemaVersion = 2 },
		"no tags":                      func(d *LearningContentDocument) { d.Tags = nil },
		"duplicate tag":                func(d *LearningContentDocument) { d.Tags = append(d.Tags, d.Tags[0]) },
		"answer format is not ability": func(d *LearningContentDocument) { d.Tags[0].Kind = "single" },
		"empty definition":             func(d *LearningContentDocument) { d.Tags[0].Definition = " " },
		"foreign question":             func(d *LearningContentDocument) { d.Questions[0].QuestionID = uuid.New() },
		"stale question version":       func(d *LearningContentDocument) { d.Questions[0].QuestionVersionID = uuid.New() },
		"duplicate question":           func(d *LearningContentDocument) { d.Questions = append(d.Questions, d.Questions[0]) },
		"unknown tag":                  func(d *LearningContentDocument) { d.Questions[0].TagIDs = []string{"not-in-course"} },
		"duplicate binding":            func(d *LearningContentDocument) { d.Questions[0].TagIDs = []string{"loops", "loops"} },
		"unapproved repository":        func(d *LearningContentDocument) { d.Sources[0].Repository = "https://github.com/example/other" },
		"mutable source reference":     func(d *LearningContentDocument) { d.Sources[0].Commit = "main" },
		"missing source hash":          func(d *LearningContentDocument) { d.Sources[0].SHA256 = "" },
		"path traversal":               func(d *LearningContentDocument) { d.Sources[0].Path = "course/../private.md" },
		"unreportable source locator":  func(d *LearningContentDocument) { d.Sources[0].Locator = strings.Repeat("界", 500) },
		"missing rights basis":         func(d *LearningContentDocument) { d.Sources[0].UsageBasis = "" },
		"missing lesson source":        func(d *LearningContentDocument) { d.Lessons[0].SourceIDs = nil },
		"unknown lesson source":        func(d *LearningContentDocument) { d.Lessons[0].SourceIDs = []string{"invented"} },
		"duplicate lesson":             func(d *LearningContentDocument) { d.Lessons = append(d.Lessons, d.Lessons[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			doc, members := learningContentFixture()
			mutate(&doc)
			raw, _ := json.Marshal(doc)
			if _, _, err := ParseLearningContent(raw, members); err == nil {
				t.Fatal("invalid content accepted")
			}
		})
	}
}

func TestLearningContentRejectsUntrustedApprovalAndOversizedInput(t *testing.T) {
	doc, members := learningContentFixture()
	raw, _ := json.Marshal(doc)
	for _, bad := range [][]byte{
		append(append([]byte{}, raw...), []byte(" {}")...),
		append([]byte(`{"status":"approved","reviewed_by":"forged",`), raw[1:]...),
		bytes.Repeat([]byte(" "), learningContentMaxBytes+1),
	} {
		if _, _, err := ParseLearningContent(bad, members); err == nil {
			t.Fatal("untrusted import accepted")
		}
	}
}
