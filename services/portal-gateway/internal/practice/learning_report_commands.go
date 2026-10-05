// Learning-report writes are the second Portal Gateway write surface after the
// practice commands. They reuse the same dedicated command credential and the
// same idempotency-key contract; nothing here accepts a client-supplied actor,
// question id or report body.
package practice

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// UpdateLearningReportPreferences replaces one owner's course-scoped feedback
// preferences and consent. Core decides whether the change requires a live
// entitlement; Gateway never relaxes that decision.
func (c *CommandClient) UpdateLearningReportPreferences(ctx context.Context, bankID, actorUserID, requestID, idempotencyKey string, raw []byte, anonymousCookie *http.Cookie) (CommandResult, error) {
	if !validPracticeCommandUUID(bankID) {
		return CommandResult{}, ErrPracticeCommandBadRequest
	}
	path := strings.Replace(UpdatePortalLearningReportPreferencesPath, "{bank_id}", url.PathEscape(bankID), 1)
	return c.command(ctx, http.MethodPut, path, actorUserID, requestID, idempotencyKey, raw, anonymousCookie, http.StatusOK, validateLearningReportPreferencesCommandEnvelope)
}

// RequestLearningReport queues one manual report request. Identical inputs
// reuse the existing task or report, so Core answers 200 with the task
// envelope instead of 202 when nothing new was queued.
func (c *CommandClient) RequestLearningReport(ctx context.Context, bankID, actorUserID, requestID, idempotencyKey string, anonymousCookie *http.Cookie) (CommandResult, error) {
	if !validPracticeCommandUUID(bankID) {
		return CommandResult{}, ErrPracticeCommandBadRequest
	}
	path := strings.Replace(RequestPortalLearningReportPath, "{bank_id}", url.PathEscape(bankID), 1)
	return c.command(ctx, http.MethodPost, path, actorUserID, requestID, idempotencyKey, []byte("{}"), anonymousCookie, http.StatusAccepted, validateLearningReportTaskCommandEnvelope(bankID), http.StatusOK)
}

// ClearLearningReports clears the owner's derived reports and queued work. It
// stays available after a revoked entitlement, so an owner can always withdraw.
func (c *CommandClient) ClearLearningReports(ctx context.Context, bankID, actorUserID, requestID, idempotencyKey string, anonymousCookie *http.Cookie) (CommandResult, error) {
	if !validPracticeCommandUUID(bankID) {
		return CommandResult{}, ErrPracticeCommandBadRequest
	}
	path := strings.Replace(ClearPortalLearningReportsPath, "{bank_id}", url.PathEscape(bankID), 1)
	return c.command(ctx, http.MethodDelete, path, actorUserID, requestID, idempotencyKey, []byte("{}"), anonymousCookie, http.StatusOK, validateLearningReportClearCommandEnvelope)
}

// CreateLearningReportPracticeSession pins a practice session to the verified
// report recommendation. Core re-selects and revalidates every question, so
// Gateway forwards no question ids and no report content.
func (c *CommandClient) CreateLearningReportPracticeSession(ctx context.Context, bankID, reportID, actorUserID, requestID, idempotencyKey string, anonymousCookie *http.Cookie) (CommandResult, error) {
	if !validPracticeCommandUUID(bankID) || !validPracticeCommandUUID(reportID) {
		return CommandResult{}, ErrPracticeCommandBadRequest
	}
	path := strings.Replace(CreatePortalLearningReportPracticeSessionPath, "{bank_id}", url.PathEscape(bankID), 1)
	path = strings.Replace(path, "{report_id}", url.PathEscape(reportID), 1)
	return c.command(ctx, http.MethodPost, path, actorUserID, requestID, idempotencyKey, []byte("{}"), anonymousCookie, http.StatusCreated, validatePracticeSessionEnvelope)
}

// learningReportCommandData unwraps a closed {request_id, data} write response.
// Unknown top-level or data members are rejected instead of relayed: the write
// path forwards Core bytes to the browser, so the Gateway must refuse a payload
// it does not fully model rather than pass an unmodelled field through.
func learningReportCommandData(raw []byte, data any) (string, error) {
	envelope := struct {
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || strings.TrimSpace(envelope.RequestID) == "" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return "", ErrPracticeCommandInvalid
	}
	dataDecoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	dataDecoder.DisallowUnknownFields()
	if err := dataDecoder.Decode(data); err != nil {
		return "", ErrPracticeCommandInvalid
	}
	return envelope.RequestID, nil
}

func validateLearningReportPreferencesCommandEnvelope(raw []byte) error {
	var preferences LearningReportPreferences
	requestID, err := learningReportCommandData(raw, &preferences)
	if err != nil {
		return err
	}
	// Reuse the read validator: a saved preference must satisfy exactly the
	// same shape the read surface will later serve for it.
	return validateLearningReportPreferences(LearningReportPreferencesEnvelope{RequestID: requestID, Data: preferences})
}

func validateLearningReportTaskCommandEnvelope(bankID string) commandEnvelopeValidator {
	return func(raw []byte) error {
		var task LearningReportTask
		requestID, err := learningReportCommandData(raw, &task)
		if err != nil {
			return err
		}
		return validateLearningReportTask(LearningReportTaskEnvelope{RequestID: requestID, Data: task}, bankID)
	}
}

func validateLearningReportClearCommandEnvelope(raw []byte) error {
	envelope := struct {
		RequestID string          `json:"request_id"`
		Data      json.RawMessage `json:"data"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || strings.TrimSpace(envelope.RequestID) == "" {
		return ErrPracticeCommandInvalid
	}
	data, valid := practiceRequiredObject(envelope.Data)
	if !valid || !practiceOnlyKeys(data, "cleared", "revision") {
		return ErrPracticeCommandInvalid
	}
	// cleared must be present and true: a response that did not clear anything
	// would let the browser claim the owner's derived data is gone.
	cleared, clearedOK := practiceRequiredBool(data, "cleared")
	if _, revisionOK := practiceRequiredInt(data, "revision"); !clearedOK || !cleared || !revisionOK {
		return ErrPracticeCommandInvalid
	}
	return nil
}
