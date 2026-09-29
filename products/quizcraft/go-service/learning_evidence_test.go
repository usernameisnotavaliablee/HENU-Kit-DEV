package quizcraft

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"henukit.dev/quizcraft/internal/contract"
)

func TestLearningEvidenceSamplesAreBoundedDeduplicatedAndStable(t *testing.T) {
	facts := []learningQuestionEvidence{}
	for i := 0; i < 40; i++ {
		evidence := contract.LearningReportEvidence{EvidenceId: fmt.Sprintf("sample-%02d", i), SubmittedAt: time.Unix(int64(i), 0), Correct: i%2 == 0}
		facts = append(facts, learningQuestionEvidence{Attempts: 1, TagIDs: []string{"large", "overlap"}, First: evidence, Latest: evidence})
	}
	rare := contract.LearningReportEvidence{EvidenceId: "rare", SubmittedAt: time.Unix(1, 0), Correct: false}
	facts = append(facts, learningQuestionEvidence{Attempts: 1, TagIDs: []string{"small"}, First: rare, Latest: rare})
	stats := []contract.LearningReportStatistic{{TagId: "large", UniqueQuestionCount: 40}, {TagId: "overlap", UniqueQuestionCount: 40}, {TagId: "small", UniqueQuestionCount: 1}, {TagId: "uncovered"}}
	samples := selectLearningEvidence(facts, stats)
	if len(samples) != 24 {
		t.Fatalf("sample count %d, want 24", len(samples))
	}
	seen := map[string]bool{}
	for _, sample := range samples {
		if seen[sample.EvidenceId] {
			t.Fatal("multi-tag duplicate sample")
		}
		seen[sample.EvidenceId] = true
	}
	if !seen["rare"] {
		t.Fatal("small tag lost every representative sample")
	}
	for i, j := 0, len(facts)-1; i < j; i, j = i+1, j-1 {
		facts[i], facts[j] = facts[j], facts[i]
	}
	want, _ := json.Marshal(samples)
	got, _ := json.Marshal(selectLearningEvidence(facts, stats))
	if string(want) != string(got) {
		t.Fatal("input ordering changed deterministic sample")
	}
	if len(selectLearningEvidence(nil, stats)) != 0 {
		t.Fatal("zero evidence fabricated samples")
	}
}
