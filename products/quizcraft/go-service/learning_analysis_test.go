package quizcraft

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"henukit.dev/quizcraft/internal/contract"
)

func learningAnalysisFixture(t *testing.T) (LearningContentDocument, LearningEvidenceSnapshot, LearningModelDecision) {
	t.Helper()
	doc, members := learningContentFixture()
	snapshot := LearningEvidenceSnapshot{SchemaVersion: "learning-evidence-v2", BankID: uuid.New(), BankVersionID: uuid.New(), ContentVersionID: uuid.New(), PreferenceRevision: 1, Goal: "follow_course", ChapterIDs: []string{}, Cutoff: time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC), PracticeCandidates: map[string][]uuid.UUID{}}
	for i := 0; i < 3; i++ {
		var q LearningContentQuestion
		if i == 0 {
			q = doc.Questions[0]
		} else {
			q = LearningContentQuestion{QuestionID: uuid.New(), QuestionVersionID: uuid.New(), TagIDs: []string{"loops"}}
			doc.Questions = append(doc.Questions, q)
			members[q.QuestionID] = q.QuestionVersionID
		}
		answer := 0
		if i == 2 {
			answer = 1
		}
		snapshot.Evidence = append(snapshot.Evidence, contract.LearningReportEvidence{EvidenceId: []string{"ev-a", "ev-b", "ev-c"}[i], QuestionId: q.QuestionID, QuestionVersionId: q.QuestionVersionID, Question: "判断循环边界", SubmittedAnswer: answer, ExpectedAnswer: 1, Correct: i == 2, SubmittedAt: snapshot.Cutoff.Add(-time.Duration(i+1) * time.Minute)})
		snapshot.QuestionContexts = append(snapshot.QuestionContexts, LearningQuestionContext{QuestionID: q.QuestionID, QuestionVersionID: q.QuestionVersionID, Kind: "single", Options: []string{"边界未处理", "边界已处理"}})
		snapshot.PracticeCandidates["loops"] = append(snapshot.PracticeCandidates["loops"], q.QuestionID)
	}
	snapshot.Statistics = []contract.LearningReportStatistic{{TagId: "loops", TagKind: "knowledge", Label: "循环", AttemptCount: 3, UniqueQuestionCount: 3, FirstCorrectCount: 1, LatestCorrectCount: 1}}
	raw, _ := json.Marshal(doc)
	_, digest, err := ParseLearningContent(raw, members)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ContentSHA256 = digest
	decision := LearningModelDecision{PrimaryTagID: "loops", Findings: []LearningModelFinding{{TagID: "loops", Status: "supported", EvidenceIDs: []string{"ev-a", "ev-b"}, PossibleReason: "可能对循环边界的判断还不稳定。"}}}
	return doc, snapshot, decision
}

func TestLearningModelInputMinimizesIdentityAndFreeText(t *testing.T) {
	doc, snapshot, _ := learningAnalysisFixture(t)
	snapshot.Evidence[0].SubmittedAnswer = "sensitive-free-answer user@example.invalid"
	input, err := BuildLearningModelInput(snapshot, doc)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(input)
	for _, secret := range []string{snapshot.BankID.String(), snapshot.ContentVersionID.String(), snapshot.Evidence[0].QuestionId.String(), "sensitive-free-answer", "user@example.invalid", "submitted_at", "practice_candidates", "usage_basis"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("unnecessary sensitive field exported: %s", secret)
		}
	}
	if !input.Evidence[0].AnswerWithheld || input.Evidence[1].AnswerWithheld {
		t.Fatal("free text must be withheld; closed choice remains useful")
	}
	if len(input.Evidence) != 3 || !strings.Contains(string(raw), "判断循环边界") {
		t.Fatal("necessary controlled evidence missing")
	}
}

func TestLearningAnalysisRejectsFabricatedAndUnsupportedConclusions(t *testing.T) {
	for name, mutate := range map[string]func(*LearningEvidenceSnapshot, *LearningModelDecision){
		"unknown tag": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) { d.Findings[0].TagID = "foreign" },
		"foreign evidence": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) {
			d.Findings[0].EvidenceIDs = []string{"another-user"}
		},
		"duplicate evidence": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) {
			d.Findings[0].EvidenceIDs = []string{"ev-a", "ev-a"}
		},
		"no supporting evidence": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) { d.Findings[0].EvidenceIDs = nil },
		"no independent error support": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) {
			d.Findings[0].EvidenceIDs = []string{"ev-c"}
		},
		"one question is not strong evidence": func(s *LearningEvidenceSnapshot, _ *LearningModelDecision) { s.Statistics[0].UniqueQuestionCount = 1 },
		"invented coverage state":             func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) { d.Findings[0].Status = "uncovered" },
		"invented numeric claim": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) {
			d.Findings[0].PossibleReason = "可能错了99次。"
		},
		"unselected primary": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) { d.PrimaryTagID = "other" },
		"duplicate finding": func(_ *LearningEvidenceSnapshot, d *LearningModelDecision) {
			d.Findings = append(d.Findings, d.Findings[0])
		},
		"changed question version": func(s *LearningEvidenceSnapshot, _ *LearningModelDecision) {
			s.Evidence[0].QuestionVersionId = uuid.New()
		},
		"tampered content hash": func(s *LearningEvidenceSnapshot, _ *LearningModelDecision) { s.ContentSHA256 = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			doc, snapshot, decision := learningAnalysisFixture(t)
			mutate(&snapshot, &decision)
			raw, _ := json.Marshal(decision)
			if _, err := ValidateLearningModelDecision(raw, snapshot, doc); err == nil {
				t.Fatal("unsupported model output accepted")
			}
		})
	}
	doc, snapshot, decision := learningAnalysisFixture(t)
	raw, _ := json.Marshal(decision)
	forged := append([]byte(`{"statistics":{"attempt_count":999},`), raw[1:]...)
	if _, err := ValidateLearningModelDecision(forged, snapshot, doc); err == nil {
		t.Fatal("model-owned numbers accepted")
	}
}

func TestLearningReportCombinesOnlyApprovedContentAndExistingQuestions(t *testing.T) {
	doc, snapshot, decision := learningAnalysisFixture(t)
	raw, _ := json.Marshal(decision)
	report, err := ComposeLearningReport(snapshot, doc, raw, uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ready" || len(report.Findings) != 1 || !strings.Contains(report.Findings[0].Observation, "3") {
		t.Fatalf("report summary = %+v", report)
	}
	if report.NextStep.Kind != "practice" || report.NextStep.Lesson == nil || report.NextStep.Lesson.Body != doc.Lessons[0].Body || report.NextStep.QuestionIds == nil || len(*report.NextStep.QuestionIds) != 3 {
		t.Fatalf("next step = %+v", report.NextStep)
	}
	snapshot.PracticeCandidates["loops"] = []uuid.UUID{uuid.New()}
	if _, err := ComposeLearningReport(snapshot, doc, raw, uuid.New(), time.Now()); err == nil {
		t.Fatal("invented practice question accepted")
	}
	snapshot.PracticeCandidates = map[string][]uuid.UUID{}
	report, err = ComposeLearningReport(snapshot, doc, raw, uuid.New(), time.Now())
	if err != nil || report.NextStep.Kind != "content_unavailable" {
		t.Fatalf("missing practice gap: %+v %v", report.NextStep, err)
	}
}

func TestLearningReportColdStartNeedsNoModelAndDoesNotInventWeakness(t *testing.T) {
	doc, snapshot, _ := learningAnalysisFixture(t)
	snapshot.Evidence = nil
	snapshot.QuestionContexts = nil
	snapshot.Statistics = []contract.LearningReportStatistic{{TagId: "loops", TagKind: "knowledge", Label: "循环"}}
	report, err := ComposeLearningReport(snapshot, doc, nil, uuid.New(), time.Now())
	if err != nil || report.Status != "insufficient_evidence" || len(report.Findings) != 0 || report.NextStep.Kind != "diagnostic" {
		t.Fatalf("cold start = %+v %v", report, err)
	}
}

func TestLearningClosedAnswersReuseScoringNormalization(t *testing.T) {
	for _, test := range []struct {
		kind          string
		answer        any
		want          string
		reference, ok bool
	}{
		{kind: "single", answer: "second", want: "1", ok: true},
		{kind: "single", answer: json.RawMessage(`1.0`), want: "1", ok: true},
		{kind: "multi", answer: []string{"second", "first"}, want: "[0,1]", ok: true},
		{kind: "judge", answer: "正确", want: "true", ok: true},
		{kind: "judge", answer: 0, want: "false", ok: true},
		{kind: "blank", answer: "private-name", want: "null", ok: false},
		{kind: "single", answer: map[string]any{"token": "private"}, want: "null", ok: false},
		{kind: "blank", answer: "main", want: `"main"`, reference: true, ok: true},
	} {
		got, ok := safeLearningAnswer(test.kind, test.answer, []string{"first", "second"}, test.reference)
		raw, _ := json.Marshal(got)
		if ok != test.ok || string(raw) != test.want {
			t.Fatalf("%s: %s %v, want %s %v", test.kind, raw, ok, test.want, test.ok)
		}
	}
}
