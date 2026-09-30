package quizcraft

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"henukit.dev/quizcraft/internal/contract"
)

// ComposeLearningReport never publishes. The repository must recheck consent,
// lease, current content and live entitlement immediately before publication.
func ComposeLearningReport(snapshot LearningEvidenceSnapshot, document LearningContentDocument, modelResult []byte, reportID uuid.UUID, createdAt time.Time) (contract.LearningReport, error) {
	if reportID == uuid.Nil || createdAt.IsZero() {
		return contract.LearningReport{}, errors.New("report identity and creation time required")
	}
	if _, err := buildLearningModelInput(snapshot, document, len(snapshot.Evidence) != 0); err != nil {
		return contract.LearningReport{}, err
	}
	report := contract.LearningReport{ReportId: reportID, BankId: snapshot.BankID, ContentVersionId: snapshot.ContentVersionID, Goal: contract.LearningReportGoal(snapshot.Goal), Status: "insufficient_evidence", EvidenceUntil: snapshot.Cutoff, CreatedAt: createdAt.UTC(), Statistics: append([]contract.LearningReportStatistic{}, snapshot.Statistics...), Evidence: append([]contract.LearningReportEvidence{}, snapshot.Evidence...), Findings: []contract.LearningReportFinding{}, NextStep: contract.LearningReportAction{Kind: "no_action", Reason: "当前作答证据不足，暂不判断薄弱点。"}}
	stats := map[string]contract.LearningReportStatistic{}
	hasEvidence, hasCoverage := false, false
	for _, stat := range snapshot.Statistics {
		stats[stat.TagId] = stat
		hasEvidence = hasEvidence || stat.AttemptCount > 0
		hasCoverage = hasCoverage || stat.UniqueQuestionCount >= 3
	}
	if !hasEvidence {
		report.NextStep.Kind = "content_unavailable"
		report.NextStep.Reason = "尚无作答证据，当前范围也暂缺合格诊断题；不生成题目补齐。"
		if len(modelResult) != 0 {
			return contract.LearningReport{}, errors.New("cold start must not use model-generated weaknesses")
		}
		for _, tag := range document.Tags {
			if len(snapshot.PracticeCandidates[tag.ID]) > 0 {
				action, err := learningReportAction(snapshot, document, tag.ID, true)
				if err != nil {
					return contract.LearningReport{}, err
				}
				action.Lesson = nil
				action.Reason = fmt.Sprintf("尚无作答证据。可先完成这 %d 道诊断题，再判断需要加强的内容；也可跳过。", len(*action.QuestionIds))
				report.NextStep = action
				break
			}
		}
		return report, nil
	}
	decision, err := ValidateLearningModelDecision(modelResult, snapshot, document)
	if err != nil {
		return contract.LearningReport{}, err
	}
	if len(decision.Findings) == 0 {
		if hasCoverage {
			report.Status = "ready"
			report.NextStep.Reason = "当前证据未识别出需要优先加强的项目；这不代表已经全面掌握。"
		}
		return report, nil
	}
	primaryStatus := "tentative"
	for _, finding := range decision.Findings {
		stat := stats[finding.TagID]
		observation := fmt.Sprintf("已作答 %d 道独立题，首答正确 %d 道；重答 %d 次，其中正确 %d 次，最近作答正确 %d 道。仅反映当前范围的作答表现。", stat.UniqueQuestionCount, stat.FirstCorrectCount, stat.RepeatAttemptCount, stat.RepeatCorrectCount, stat.LatestCorrectCount)
		if finding.Status == "uncovered" {
			observation = "当前范围尚无该标签的作答证据，不能据此认定薄弱。"
		}
		item := contract.LearningReportFinding{TagId: finding.TagID, Status: contract.LearningReportFindingStatus(finding.Status), EvidenceIds: append([]string{}, finding.EvidenceIDs...), Observation: observation}
		if finding.PossibleReason != "" {
			reason := finding.PossibleReason
			item.PossibleReason = &reason
		}
		report.Findings = append(report.Findings, item)
		if finding.Status == "supported" {
			report.Status = "ready"
		}
		if finding.TagID == decision.PrimaryTagID {
			primaryStatus = finding.Status
		}
	}
	report.NextStep, err = learningReportAction(snapshot, document, decision.PrimaryTagID, primaryStatus != "supported")
	if err != nil {
		return contract.LearningReport{}, err
	}
	return report, nil
}

func learningReportAction(snapshot LearningEvidenceSnapshot, document LearningContentDocument, tag string, diagnostic bool) (contract.LearningReportAction, error) {
	action := contract.LearningReportAction{TagId: &tag, Kind: "content_unavailable", Reason: "该范围暂缺合格练习题，不能生成题目补齐。"}
	sources := map[string]LearningContentSource{}
	for _, source := range document.Sources {
		sources[source.ID] = source
	}
	for _, lesson := range document.Lessons {
		matches := false
		for _, id := range lesson.TagIDs {
			if id == tag {
				matches = true
			}
		}
		if !matches {
			continue
		}
		ready := contract.LearningReportLesson{LessonId: lesson.ID, Title: lesson.Title, Body: lesson.Body, Sources: []contract.LearningReportSource{}}
		for _, id := range lesson.SourceIDs {
			source, ok := sources[id]
			locator := source.Path + " · " + source.Locator
			if !ok || !learningText(locator, 500) {
				return contract.LearningReportAction{}, errors.New("reviewed lesson source is not reportable")
			}
			ready.Sources = append(ready.Sources, contract.LearningReportSource{SourceId: source.ID, Title: source.Title, Version: source.Commit, Locator: locator})
		}
		action.Lesson = &ready
		break
	}
	questions := append([]uuid.UUID{}, snapshot.PracticeCandidates[tag]...)
	if len(questions) == 0 {
		if action.Lesson != nil {
			action.Reason = "暂缺合格练习题，可先阅读这份已审核讲解；不自动生成新题。"
		}
		return action, nil
	}
	action.QuestionIds = &questions
	action.Kind = "practice"
	action.Reason = fmt.Sprintf("先完成这 %d 道已有针对练习，再结合新作答更新判断。", len(questions))
	if action.Lesson != nil {
		action.Reason = fmt.Sprintf("先阅读已审核讲解，再完成这 %d 道针对练习。", len(questions))
	} else {
		action.Reason = fmt.Sprintf("暂缺已审核讲解，可先完成这 %d 道已有针对练习。", len(questions))
	}
	if diagnostic {
		action.Kind = "diagnostic"
		action.Lesson = nil
		action.Reason = fmt.Sprintf("当前判断仍不确定。建议先做这 %d 道诊断题补充证据；可以跳过。", len(questions))
		if len(questions) == 1 {
			for _, e := range snapshot.Evidence {
				if e.QuestionId == questions[0] {
					action.Reason = "当前仅有这道已作答的合格题；重做不能增加独立样本，可以跳过。"
					break
				}
			}
		}
	}
	return action, nil
}
