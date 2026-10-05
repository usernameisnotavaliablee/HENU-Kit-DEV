package quizcraft

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"henukit.dev/quizcraft/internal/contract"
	"henukit.dev/quizcraft/internal/store"
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
	return &Service{database: service.database, learningManualLimit: service.learningManualLimit}
}

// learningPublishedBank resolves the course in the path and requires it to be
// published. An unknown or withdrawn bank is a 404, not an empty result.
func (service *practiceHTTP) learningPublishedBank(writer http.ResponseWriter, request *http.Request) (uuid.UUID, bool) {
	bankID, err := uuid.Parse(chi.URLParam(request, "bank_id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_bank_id", "bank_id must be a UUID")
		return uuid.Nil, false
	}
	if _, err := service.queries.GetActiveBankVersion(request.Context(), bankID); errors.Is(err, pgx.ErrNoRows) {
		writeError(writer, http.StatusNotFound, "bank_not_found", "this course is not published")
		return uuid.Nil, false
	} else if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return uuid.Nil, false
	}
	return bankID, true
}

// learningReadScope authenticates the signed Portal actor and resolves a
// published course.
func (service *practiceHTTP) learningReadScope(writer http.ResponseWriter, request *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, err := portalActorUserID(request)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "authentication_required", "sign in to read course feedback")
		return uuid.Nil, uuid.Nil, false
	}
	bankID, ok := service.learningPublishedBank(writer, request)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, bankID, true
}

// learningCommandOwner requires a verified signed-in owner on the Portal
// command boundary. Guest actors have no reports to manage.
func (service *practiceHTTP) learningCommandOwner(writer http.ResponseWriter, request *http.Request) (practiceActor, bool) {
	actor, status, err := service.actor(writer, request)
	if err != nil {
		writeError(writer, status, "invalid_session", err.Error())
		return practiceActor{}, false
	}
	if actor.userID == nil {
		writeError(writer, http.StatusUnauthorized, "authentication_required", "sign in to manage course feedback")
		return practiceActor{}, false
	}
	return actor, true
}

func (service *practiceHTTP) writeLearningWriteError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLearningInvalidPreferences), errors.Is(err, ErrLearningInvalidJob):
		writeError(writer, http.StatusBadRequest, "invalid_learning_request", "the course feedback request is invalid")
	case errors.Is(err, ErrLearningReportNotFound):
		writeError(writer, http.StatusNotFound, "learning_report_not_found", "no course feedback report is available yet")
	case errors.Is(err, ErrLearningNoPracticeRecommendation):
		writeError(writer, http.StatusConflict, "learning_no_practice", "this report has no available practice questions")
	case errors.Is(err, ErrLearningUnavailable):
		writeError(writer, http.StatusConflict, "learning_conflict", "course feedback is not available in its current state")
	case errors.Is(err, ErrLearningRateLimited):
		// Abuse protection, not a quota: nothing was charged and the same
		// request replays for free once the window moves on.
		writeError(writer, http.StatusTooManyRequests, "rate_limited", "too many course feedback requests; retry later")
	default:
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
	}
}

// learningWriteRequest is one idempotent Portal command. Every domain write here
// is idempotent by input, so the idempotency record is written after the write:
// a replayed key re-runs an equivalent write and then returns the first stored
// response instead of a second one. That keeps the domain write and the record
// in one transaction each instead of holding a transaction across the domain
// call.
type learningWriteRequest struct {
	actorKey string
	kind     string
	key      string
	hash     string
}

func (service *practiceHTTP) beginLearningWrite(writer http.ResponseWriter, request *http.Request, actorKey, kind, hash string) (learningWriteRequest, bool) {
	key, ok := requiredIdempotencyKey(writer, request)
	if !ok {
		return learningWriteRequest{}, false
	}
	return learningWriteRequest{actorKey: actorKey, kind: kind, key: key, hash: hash}, true
}

// finishLearningWrite records the response, or replays the stored one. A key
// reused with another request body is a conflict and never overwrites the first
// record.
func (service *practiceHTTP) finishLearningWrite(writer http.ResponseWriter, request *http.Request, write learningWriteRequest, status int, payload any, resourceID uuid.UUID) {
	body, err := json.Marshal(responseEnvelope{RequestID: requestID(), Data: payload})
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	ctx := request.Context()
	tx, err := service.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := store.New(tx)
	if err := lockIdempotency(ctx, queries, write.actorKey, write.kind, write.key); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	storedStatus, storedBody, found, conflict, err := loadIdempotency(ctx, queries, write.actorKey, write.kind, write.key, write.hash)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	if conflict {
		writeError(writer, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with another request")
		return
	}
	if found {
		writeRawJSON(writer, storedStatus, storedBody)
		return
	}
	if err := storeIdempotency(ctx, queries, write.actorKey, write.kind, write.key, write.hash, status, body, resourceID); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	writeRawJSON(writer, status, body)
}

// decodeLearningPreferencesUpdate rejects unknown fields so the Portal cannot
// smuggle fields the contract does not define.
func decodeLearningPreferencesUpdate(request *http.Request) (contract.LearningReportPreferencesUpdate, []byte, error) {
	var input contract.LearningReportPreferencesUpdate
	raw, err := io.ReadAll(io.LimitReader(request.Body, 64<<10))
	if err != nil {
		return input, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, raw, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return input, raw, err
	}
	return input, raw, nil
}

// portalUpdateLearningReportPreferences stores the owner's settings. Enabling
// generation needs a live membership; disabling or narrowing scope is owner
// cleanup and stays available after revocation, which is why the entitlement
// check is inside this handler rather than on the route.
func (service *practiceHTTP) portalUpdateLearningReportPreferences(writer http.ResponseWriter, request *http.Request) {
	actor, ok := service.learningCommandOwner(writer, request)
	if !ok {
		return
	}
	bankID, ok := service.learningPublishedBank(writer, request)
	if !ok {
		return
	}
	input, raw, err := decodeLearningPreferencesUpdate(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_learning_request", "the preferences payload is invalid")
		return
	}
	write, ok := service.beginLearningWrite(writer, request, actor.key, "update_learning_report_preferences", hashCanonical(raw))
	if !ok {
		return
	}
	if input.Enabled && !service.requireLearningLifetime(writer, request, *actor.userID) {
		return
	}
	preferences, err := service.learning().UpdateLearningReportPreferences(request.Context(), *actor.userID, bankID, input)
	if err != nil {
		service.writeLearningWriteError(writer, err)
		return
	}
	service.finishLearningWrite(writer, request, write, http.StatusOK, preferences, bankID)
}

// portalRequestLearningReport queues one manual report. It never runs the model
// in the request path: the worker picks the job up. Requests with the same
// content revision and scope reuse the existing task or report.
func (service *practiceHTTP) portalRequestLearningReport(writer http.ResponseWriter, request *http.Request) {
	actor, ok := service.learningCommandOwner(writer, request)
	if !ok {
		return
	}
	bankID, ok := service.learningPublishedBank(writer, request)
	if !ok {
		return
	}
	write, ok := service.beginLearningWrite(writer, request, actor.key, "request_learning_report", hashCanonical([]byte(http.MethodPost+":"+bankID.String())))
	if !ok {
		return
	}
	if !service.requireLearningLifetime(writer, request, *actor.userID) {
		return
	}
	if !validLearningJobVersions(service.learningVersions) {
		writeError(writer, http.StatusServiceUnavailable, "learning_unavailable", "course feedback generation is not configured")
		return
	}
	task, reused, err := service.learning().QueueLearningReport(request.Context(), *actor.userID, bankID, time.Now().UTC(), "manual", service.learningVersions)
	if err != nil {
		service.writeLearningWriteError(writer, err)
		return
	}
	status := http.StatusAccepted
	if reused || task.Status == "ready" {
		status = http.StatusOK
	}
	service.finishLearningWrite(writer, request, write, status, task, task.TaskId)
}

// portalClearLearningReports discards derived feedback and cancels outstanding
// work for this owner and course. It needs no live membership: cleanup must
// work after revocation, and it never reads a report or starts model work.
func (service *practiceHTTP) portalClearLearningReports(writer http.ResponseWriter, request *http.Request) {
	actor, ok := service.learningCommandOwner(writer, request)
	if !ok {
		return
	}
	bankID, ok := service.learningPublishedBank(writer, request)
	if !ok {
		return
	}
	write, ok := service.beginLearningWrite(writer, request, actor.key, "clear_learning_reports", hashCanonical([]byte(http.MethodDelete+":"+bankID.String())))
	if !ok {
		return
	}
	result, err := service.learning().ClearLearningReports(request.Context(), *actor.userID, bankID)
	if err != nil {
		service.writeLearningWriteError(writer, err)
		return
	}
	service.finishLearningWrite(writer, request, write, http.StatusOK, result, bankID)
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

// portalCreateLearningReportPracticeSession pins one practice session to the
// questions the published report recommends. The client sends no question ids;
// questions that are no longer published are dropped and counted, and the
// report is revalidated against the approved active content version in the same
// transaction as the session.
func (service *practiceHTTP) portalCreateLearningReportPracticeSession(writer http.ResponseWriter, request *http.Request) {
	actor, ok := service.learningCommandOwner(writer, request)
	if !ok {
		return
	}
	bankID, ok := service.learningPublishedBank(writer, request)
	if !ok {
		return
	}
	reportID, err := uuid.Parse(chi.URLParam(request, "report_id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_report_id", "report_id must be a UUID")
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(writer, request)
	if !ok {
		return
	}
	if !service.requireLearningLifetime(writer, request, *actor.userID) {
		return
	}
	write := learningWriteRequest{actorKey: actor.key, kind: "create_learning_report_session", key: idempotencyKey, hash: hashCanonical([]byte(http.MethodPost + ":" + bankID.String() + ":" + reportID.String()))}
	ctx := request.Context()
	tx, err := service.database.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := store.New(tx)
	if err := lockIdempotency(ctx, queries, write.actorKey, write.kind, write.key); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	storedStatus, storedBody, found, conflict, err := loadIdempotency(ctx, queries, write.actorKey, write.kind, write.key, write.hash)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	if conflict {
		writeError(writer, http.StatusConflict, "idempotency_conflict", "idempotency key was already used with another request")
		return
	}
	if found {
		writeRawJSON(writer, storedStatus, storedBody)
		return
	}
	session, err := createLearningReportPracticeSession(ctx, tx, *actor.userID, bankID, reportID)
	if err != nil {
		service.writeLearningWriteError(writer, err)
		return
	}
	body, err := json.Marshal(responseEnvelope{RequestID: requestID(), Data: session})
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	if err := storeIdempotency(ctx, queries, write.actorKey, write.kind, write.key, write.hash, http.StatusCreated, body, session.SessionID); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft is temporarily unavailable")
		return
	}
	writeRawJSON(writer, http.StatusCreated, body)
}
