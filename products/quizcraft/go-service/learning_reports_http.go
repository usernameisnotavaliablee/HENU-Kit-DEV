package quizcraft

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The learning report read boundary is the Portal Gateway's signed personal
// boundary (six-part HMAC over X-Actor-User-Id). It is registered only when a
// verified Account Portfolio client and the catalog caller are both configured,
// so an unconfigured deployment has no such route at all. Writes stay on the
// separate Portal command boundary.
//
// Reading a published report or a task requires a fresh lifetime entitlement.
// Reading preferences does not: an owner whose membership lapsed must still see
// their settings, and the contract explicitly lets an owner disable or clear
// derived data after revocation. No read here ever exposes another owner's rows,
// the stored evidence snapshot or provider input.

func (service *practiceHTTP) learning() *Service {
	return &Service{database: service.database}
}

// learningReadScope authenticates the signed Portal actor and resolves a
// published course. A missing or unknown bank is a 404, not an empty result.
func (service *practiceHTTP) learningReadScope(writer http.ResponseWriter, request *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, err := portalActorUserID(request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication_required", "sign in to read course feedback")
		return uuid.Nil, uuid.Nil, false
	}
	bankID, err := uuid.Parse(chi.URLParam(request, "bank_id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_bank_id", "bank_id must be a UUID")
		return uuid.Nil, uuid.Nil, false
	}
	if _, err := service.queries.GetActiveBankVersion(request.Context(), bankID); errors.Is(err, pgx.ErrNoRows) {
		writeError(writer, http.StatusNotFound, "bank_not_found", "this course is not published")
		return uuid.Nil, uuid.Nil, false
	} else if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, bankID, true
}

// requireLearningLifetime fails closed: a dependency error is a 503, never an
// implicit allowance.
func (service *practiceHTTP) requireLearningLifetime(writer http.ResponseWriter, request *http.Request, userID uuid.UUID) bool {
	if service.learningEntitlement == nil {
		writeError(writer, http.StatusServiceUnavailable, "learning_unavailable", "course feedback is temporarily unavailable")
		return false
	}
	allowed, err := service.learningEntitlement.CheckLifetime(request.Context(), userID)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "learning_unavailable", "course feedback is temporarily unavailable")
		return false
	}
	if !allowed {
		writeError(writer, http.StatusForbidden, "learning_entitlement_required", "course feedback requires an active membership")
		return false
	}
	return true
}

func (service *practiceHTTP) writeLearningReadError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLearningReportNotFound):
		writeError(writer, http.StatusNotFound, "learning_report_not_found", "no course feedback report is available yet")
	case errors.Is(err, ErrLearningInvalidJob):
		writeError(writer, http.StatusBadRequest, "invalid_learning_request", "the course feedback request is invalid")
	case errors.Is(err, ErrLearningUnavailable):
		writeError(writer, http.StatusServiceUnavailable, "learning_unavailable", "course feedback is temporarily unavailable")
	default:
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
	}
}

func (service *practiceHTTP) portalLearningReportPreferences(writer http.ResponseWriter, request *http.Request) {
	userID, bankID, ok := service.learningReadScope(writer, request)
	if !ok {
		return
	}
	preferences, err := service.learning().GetLearningReportPreferences(request.Context(), userID, bankID)
	if err != nil {
		service.writeLearningReadError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, responseEnvelope{RequestID: requestID(), Data: preferences})
}

func (service *practiceHTTP) portalLatestLearningReport(writer http.ResponseWriter, request *http.Request) {
	userID, bankID, ok := service.learningReadScope(writer, request)
	if !ok || !service.requireLearningLifetime(writer, request, userID) {
		return
	}
	report, err := service.learning().GetLatestLearningReport(request.Context(), userID, bankID)
	if err != nil {
		service.writeLearningReadError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, responseEnvelope{RequestID: requestID(), Data: report})
}

func (service *practiceHTTP) portalLearningReportTask(writer http.ResponseWriter, request *http.Request) {
	userID, bankID, ok := service.learningReadScope(writer, request)
	if !ok || !service.requireLearningLifetime(writer, request, userID) {
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(request, "task_id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_task_id", "task_id must be a UUID")
		return
	}
	task, err := service.learning().GetLearningReportTask(request.Context(), userID, bankID, taskID)
	if err != nil {
		service.writeLearningReadError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, responseEnvelope{RequestID: requestID(), Data: task})
}
