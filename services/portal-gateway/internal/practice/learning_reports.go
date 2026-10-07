package practice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrLearningReportNotFound means the owner has no such published report (or
// task): the Core answers 404 rather than substituting another report, and the
// Portal surface must show an honest empty state instead of a fake report.
var ErrLearningReportNotFound = errors.New("QuizCraft course report is not available to this owner")

// The contract caps collections per report; a Core response beyond them is a
// contract violation, not partial data.
const (
	learningReportMaxStatistics    = 300
	learningReportMaxEvidence      = 24
	learningReportMaxFindings      = 3
	learningReportMaxEvidenceIDs   = 24
	learningReportMaxLessonSources = 20
	learningReportMaxQuestionIDs   = 10
)

// LearningReportPreferences reads one signed-in owner's course-scoped feedback
// preferences. A user without stored preferences receives the Core's disabled
// seven-day defaults, never a mock.
func (c *Client) LearningReportPreferences(ctx context.Context, actorUserID, requestID, bankID string) (LearningReportPreferencesEnvelope, error) {
	path := strings.Replace(GetPortalLearningReportPreferencesPath, "{bank_id}", url.PathEscape(bankID), 1)
	resp, err := c.learningReportRead(ctx, path, actorUserID, requestID)
	if err != nil {
		return LearningReportPreferencesEnvelope{}, err
	}
	defer resp.Body.Close()
	var envelope LearningReportPreferencesEnvelope
	envelopeRequestID, err := decodeLearningReportEnvelope(resp.Body, &envelope.Data)
	if err != nil {
		return LearningReportPreferencesEnvelope{}, err
	}
	envelope.RequestID = envelopeRequestID
	if err := validateLearningReportPreferences(envelope); err != nil {
		return LearningReportPreferencesEnvelope{}, err
	}
	return envelope, nil
}

// LatestLearningReport reads the newest non-stale report of one owner and bank.
// A superseded or absent report is a 404: the Gateway never serves a stale
// report in place of a current one.
func (c *Client) LatestLearningReport(ctx context.Context, actorUserID, requestID, bankID string) (LearningReportEnvelope, error) {
	path := strings.Replace(GetPortalLatestLearningReportPath, "{bank_id}", url.PathEscape(bankID), 1)
	resp, err := c.learningReportRead(ctx, path, actorUserID, requestID)
	if err != nil {
		return LearningReportEnvelope{}, err
	}
	defer resp.Body.Close()
	var envelope LearningReportEnvelope
	envelopeRequestID, err := decodeLearningReportEnvelope(resp.Body, &envelope.Data)
	if err != nil {
		return LearningReportEnvelope{}, err
	}
	envelope.RequestID = envelopeRequestID
	if err := validateLearningReport(envelope, bankID); err != nil {
		return LearningReportEnvelope{}, err
	}
	return envelope, nil
}

// LearningReportTask reads one report task's progress. Task ids are owner
// scoped in the Core, so another user's task is indistinguishable from a
// missing one.
func (c *Client) LearningReportTask(ctx context.Context, actorUserID, requestID, bankID, taskID string) (LearningReportTaskEnvelope, error) {
	if !validUUID(taskID) {
		return LearningReportTaskEnvelope{}, ErrLearningReportNotFound
	}
	path := strings.Replace(GetPortalLearningReportTaskPath, "{bank_id}", url.PathEscape(bankID), 1)
	path = strings.Replace(path, "{task_id}", url.PathEscape(taskID), 1)
	resp, err := c.learningReportRead(ctx, path, actorUserID, requestID)
	if err != nil {
		return LearningReportTaskEnvelope{}, err
	}
	defer resp.Body.Close()
	var envelope LearningReportTaskEnvelope
	envelopeRequestID, err := decodeLearningReportEnvelope(resp.Body, &envelope.Data)
	if err != nil {
		return LearningReportTaskEnvelope{}, err
	}
	envelope.RequestID = envelopeRequestID
	if err := validateLearningReportTask(envelope, bankID); err != nil {
		return LearningReportTaskEnvelope{}, err
	}
	return envelope, nil
}

// learningReportRead is the actor-bound read for learning reports. It is the
// same six-part signed GET as personal statistics, with one deliberate
// difference: a Core 404 stays a 404 so the member surface can distinguish "no
// report yet" from an outage.
func (c *Client) learningReportRead(ctx context.Context, path, actorUserID, requestID string) (*http.Response, error) {
	if c == nil || c.signer == nil || c.httpClient == nil || !validUUID(actorUserID) || strings.TrimSpace(requestID) == "" {
		return nil, ErrStatsUnauthorized
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, ErrStatsUnavailable
	}
	req.Header.Set("X-Request-Id", requestID)
	req.Header.Set("X-Permission-Code", PortalReadPermission)
	req.Header.Set("X-Scope-Kind", "product")
	req.Header.Set("X-Product-Code", "quizcraft")
	if err := c.signer.SignWithActor(req, actorUserID); err != nil {
		return nil, fmt.Errorf("learning report read %s sign: %w", path, ErrStatsUnavailable)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("learning report read %s request: %w", path, ErrStatsUnavailable)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		return nil, ErrLearningReportNotFound
	case http.StatusForbidden:
		// Core gates the newest report and task progress on live membership, so
		// this is a member state, not a dependency outage. Report it as such and
		// keep Core's own code when it named one.
		err := coreRejection(resp, ErrPortalReadForbidden)
		_ = resp.Body.Close()
		return nil, err
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("learning report read %s status %d: %w", path, resp.StatusCode, ErrStatsUnavailable)
	}
}

// decodeLearningReportEnvelope unwraps the closed {request_id, data} envelope
// and decodes only the data payload into the mirrored type, so an unmodelled
// Core member can never be re-serialized to a browser.
func decodeLearningReportEnvelope(body io.Reader, data any) (string, error) {
	raw := struct {
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
	}{}
	if err := json.NewDecoder(io.LimitReader(body, 4<<20)).Decode(&raw); err != nil {
		return "", fmt.Errorf("learning report decode: %w", ErrInvalidStats)
	}
	if strings.TrimSpace(raw.RequestID) == "" || len(raw.Data) == 0 || string(raw.Data) == "null" {
		return "", ErrInvalidStats
	}
	if err := json.Unmarshal(raw.Data, data); err != nil {
		return "", ErrInvalidStats
	}
	return raw.RequestID, nil
}

func validateLearningReportPreferences(result LearningReportPreferencesEnvelope) error {
	preferences := result.Data
	if strings.TrimSpace(result.RequestID) == "" || !validUUID(preferences.BankID) || preferences.IntervalDays < 1 || preferences.IntervalDays > 30 || !validLearningGoal(preferences.Goal) || !validLearningChapterIDs(preferences.ChapterIDs) || preferences.Revision < 0 {
		return ErrInvalidStats
	}
	for _, timestamp := range []*string{preferences.NextDueAt, preferences.UpdatedAt} {
		if timestamp != nil && strings.TrimSpace(*timestamp) == "" {
			return ErrInvalidStats
		}
	}
	return nil
}

func validLearningGoal(value string) bool {
	return value == "follow_course" || value == "exam_review"
}

func validLearningChapterIDs(values []string) bool {
	if values == nil || len(values) > 50 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(value) > 160 {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validateLearningReportTask(result LearningReportTaskEnvelope, expectedBankID string) error {
	task := result.Data
	if strings.TrimSpace(result.RequestID) == "" || !validUUID(task.TaskID) || !validUUID(task.BankID) || task.BankID != expectedBankID || !validLearningTaskStatus(task.Status) || strings.TrimSpace(task.CreatedAt) == "" {
		return ErrInvalidStats
	}
	if task.ReportID != nil && !validUUID(*task.ReportID) {
		return ErrInvalidStats
	}
	if task.ReasonCode != nil && strings.TrimSpace(*task.ReasonCode) == "" {
		return ErrInvalidStats
	}
	if task.RetryAfterSeconds != nil && *task.RetryAfterSeconds < 0 {
		return ErrInvalidStats
	}
	return nil
}

func validLearningTaskStatus(value string) bool {
	switch value {
	case "queued", "running", "ready", "failed", "paused", "cancelled":
		return true
	default:
		return false
	}
}

func validateLearningReport(result LearningReportEnvelope, expectedBankID string) error {
	report := result.Data
	if strings.TrimSpace(result.RequestID) == "" || !validUUID(report.ReportID) || !validUUID(report.BankID) || report.BankID != expectedBankID || !validUUID(report.ContentVersionID) || !validLearningGoal(report.Goal) {
		return ErrInvalidStats
	}
	switch report.Status {
	case "ready", "insufficient_evidence", "stale":
	default:
		return ErrInvalidStats
	}
	if strings.TrimSpace(report.EvidenceUntil) == "" || strings.TrimSpace(report.CreatedAt) == "" {
		return ErrInvalidStats
	}
	if report.Statistics == nil || len(report.Statistics) > learningReportMaxStatistics || report.Evidence == nil || len(report.Evidence) > learningReportMaxEvidence || report.Findings == nil || len(report.Findings) > learningReportMaxFindings {
		return ErrInvalidStats
	}
	evidenceIDs := make(map[string]struct{}, len(report.Evidence))
	for _, evidence := range report.Evidence {
		if strings.TrimSpace(evidence.EvidenceID) == "" || len(evidence.EvidenceID) > 80 || !validUUID(evidence.QuestionID) || !validUUID(evidence.QuestionVersionID) || strings.TrimSpace(evidence.SubmittedAt) == "" || strings.TrimSpace(evidence.Question) == "" {
			return ErrInvalidStats
		}
		if _, exists := evidenceIDs[evidence.EvidenceID]; exists {
			return ErrInvalidStats
		}
		evidenceIDs[evidence.EvidenceID] = struct{}{}
	}
	for _, statistic := range report.Statistics {
		if strings.TrimSpace(statistic.TagID) == "" || (statistic.TagKind != "knowledge" && statistic.TagKind != "ability") || strings.TrimSpace(statistic.Label) == "" {
			return ErrInvalidStats
		}
		if statistic.AttemptCount < 0 || statistic.UniqueQuestionCount < 0 || statistic.FirstCorrectCount < 0 || statistic.RepeatAttemptCount < 0 || statistic.RepeatCorrectCount < 0 || statistic.LatestCorrectCount < 0 {
			return ErrInvalidStats
		}
		if statistic.FirstCorrectCount > statistic.UniqueQuestionCount || statistic.UniqueQuestionCount > statistic.AttemptCount {
			return ErrInvalidStats
		}
	}
	for _, finding := range report.Findings {
		switch finding.Status {
		case "supported", "tentative", "uncovered":
		default:
			return ErrInvalidStats
		}
		if strings.TrimSpace(finding.TagID) == "" || strings.TrimSpace(finding.Observation) == "" || finding.EvidenceIDs == nil || len(finding.EvidenceIDs) > learningReportMaxEvidenceIDs {
			return ErrInvalidStats
		}
		// A finding may only cite the owner's own evidence in this report; a
		// citation outside it would be an unverifiable claim rendered as fact.
		seen := make(map[string]struct{}, len(finding.EvidenceIDs))
		for _, id := range finding.EvidenceIDs {
			if _, ok := evidenceIDs[id]; !ok {
				return ErrInvalidStats
			}
			if _, exists := seen[id]; exists {
				return ErrInvalidStats
			}
			seen[id] = struct{}{}
		}
	}
	switch report.NextStep.Kind {
	case "practice", "diagnostic":
		if len(report.NextStep.QuestionIDs) == 0 || len(report.NextStep.QuestionIDs) > learningReportMaxQuestionIDs {
			return ErrInvalidStats
		}
		seen := make(map[string]struct{}, len(report.NextStep.QuestionIDs))
		for _, id := range report.NextStep.QuestionIDs {
			if !validUUID(id) {
				return ErrInvalidStats
			}
			if _, exists := seen[id]; exists {
				return ErrInvalidStats
			}
			seen[id] = struct{}{}
		}
	case "content_unavailable", "no_action":
		if len(report.NextStep.QuestionIDs) != 0 {
			return ErrInvalidStats
		}
	default:
		return ErrInvalidStats
	}
	if strings.TrimSpace(report.NextStep.Reason) == "" {
		return ErrInvalidStats
	}
	if report.NextStep.Lesson != nil {
		lesson := report.NextStep.Lesson
		if strings.TrimSpace(lesson.LessonID) == "" || strings.TrimSpace(lesson.Title) == "" || strings.TrimSpace(lesson.Body) == "" || lesson.Sources == nil || len(lesson.Sources) > learningReportMaxLessonSources {
			return ErrInvalidStats
		}
		seen := make(map[string]struct{}, len(lesson.Sources))
		for _, source := range lesson.Sources {
			if strings.TrimSpace(source.SourceID) == "" || strings.TrimSpace(source.Title) == "" || strings.TrimSpace(source.Version) == "" || strings.TrimSpace(source.Locator) == "" {
				return ErrInvalidStats
			}
			if _, exists := seen[source.SourceID]; exists {
				return ErrInvalidStats
			}
			seen[source.SourceID] = struct{}{}
		}
	}
	return nil
}
