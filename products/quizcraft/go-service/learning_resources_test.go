package quizcraft

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"henukit.dev/quizcraft/internal/contract"
)

func TestLearningReportLargeCourseColdStart(t *testing.T) {
	doc, snapshot, _ := learningAnalysisFixture(t)
	snapshot.Evidence = nil
	snapshot.QuestionContexts = nil
	snapshot.Statistics = []contract.LearningReportStatistic{{TagId: "loops", TagKind: "knowledge", Label: "循环"}}
	for i := len(doc.Tags); i < 300; i++ {
		tag := LearningContentTag{ID: fmt.Sprintf("tag-%03d", i), Kind: "knowledge", Name: fmt.Sprintf("概念-%03d", i), Definition: strings.Repeat("定义", 400)}
		doc.Tags = append(doc.Tags, tag)
		snapshot.Statistics = append(snapshot.Statistics, contract.LearningReportStatistic{TagId: tag.ID, TagKind: "knowledge", Label: tag.Name})
	}
	members := map[uuid.UUID]uuid.UUID{}
	for _, q := range doc.Questions {
		members[q.QuestionID] = q.QuestionVersionID
	}
	raw, _ := json.Marshal(doc)
	_, digest, err := ParseLearningContent(raw, members)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ContentSHA256 = digest
	if _, err := BuildLearningModelInput(snapshot, doc); err == nil {
		t.Fatal("provider payload must still respect the 128 KiB limit")
	}
	report, err := ComposeLearningReport(snapshot, doc, nil, uuid.New(), time.Now())
	if err != nil {
		t.Fatalf("valid cold start must not require a model payload: %v", err)
	}
	if report.Status != "insufficient_evidence" || report.NextStep.Kind != "diagnostic" || len(report.Findings) != 0 {
		t.Fatalf("cold start invented a weakness or lost its existing exercise: %+v", report)
	}
	snapshot.ContentSHA256 = strings.Repeat("0", 64)
	if _, err := ComposeLearningReport(snapshot, doc, nil, uuid.New(), time.Now()); err == nil {
		t.Fatal("cold start must still validate content integrity")
	}
}

func TestLearningSnapshotRejectsOversizedAnswerBeforeWithholding(t *testing.T) {
	doc, snapshot, decision := learningAnalysisFixture(t)
	snapshot.Evidence[0].SubmittedAnswer = strings.Repeat("x", 8193)
	if _, err := BuildLearningModelInput(snapshot, doc); err == nil {
		t.Fatal("withholding from the provider must not permit an oversized stored answer")
	}
	raw, _ := json.Marshal(decision)
	if _, err := ComposeLearningReport(snapshot, doc, raw, uuid.New(), time.Now()); err == nil {
		t.Fatal("report must not retain the oversized raw answer")
	}
}
