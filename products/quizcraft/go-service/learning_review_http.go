package quizcraft

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The Workshop learning-content surface is the only way a content package
// becomes usable. Import stores a draft, approval names the reviewing operator,
// and every member-facing read still revalidates entitlement, consent and the
// approved-version join.

type learningContentReviewCommand struct {
	Note     string `json:"note"`
	Activate *bool  `json:"activate"`
	Enable   bool   `json:"enable"`
}

func learningContentVersionJSON(info LearningContentVersionInfo) map[string]any {
	return map[string]any{
		"content_version_id": info.ContentVersionID,
		"bank_id":            info.BankID,
		"bank_version_id":    info.BankVersionID,
		"content_sha256":     info.ContentSHA256,
		"status":             info.Status,
		"created_at":         info.CreatedAt,
		"reviewed_by":        info.ReviewedBy,
		"reviewed_at":        info.ReviewedAt,
		"active":             info.Active,
		"catalog_enabled":    info.CatalogEnabled,
		"question_count":     info.QuestionCount,
		"lesson_count":       info.LessonCount,
	}
}

// learningReviewProblem maps review outcomes onto the Workshop error surface.
// Anything unrecognised stays an error so the caller reports 503, never 400.
func learningReviewProblem(err error) error {
	switch {
	case errors.Is(err, ErrLearningContentInvalid):
		return workshopHTTPError{http.StatusBadRequest, "invalid_learning_content", err.Error()}
	case errors.Is(err, ErrLearningContentMissing):
		return workshopHTTPError{http.StatusNotFound, "learning_content_not_found", err.Error()}
	case errors.Is(err, ErrLearningContentConflict), errors.Is(err, ErrLearningUnavailable):
		return workshopHTTPError{http.StatusConflict, "learning_content_conflict", err.Error()}
	default:
		return err
	}
}

func writeLearningReviewError(writer http.ResponseWriter, err error) {
	var failure workshopHTTPError
	if errors.As(learningReviewProblem(err), &failure) {
		writeError(writer, failure.status, failure.code, failure.message)
		return
	}
	writeError(writer, http.StatusServiceUnavailable, "database_unavailable", "QuizCraft Workshop is temporarily unavailable")
}

func learningContentVersionID(writer http.ResponseWriter, request *http.Request) (uuid.UUID, bool) {
	contentID, err := uuid.Parse(chi.URLParam(request, "content_version_id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_content_version_id", "content_version_id must be a UUID")
		return uuid.Nil, false
	}
	return contentID, true
}

func (service *practiceHTTP) listWorkshopLearningContent(writer http.ResponseWriter, request *http.Request) {
	bankID, _, ok := workshopIDs(writer, request, false)
	if !ok {
		return
	}
	if _, _, allowed := service.workshopActor(writer, request, "quizcraft.workshop.read", &bankID, false); !allowed {
		return
	}
	versions, err := ListLearningContentVersions(request.Context(), service.database, bankID)
	if err != nil {
		writeLearningReviewError(writer, err)
		return
	}
	data := make([]map[string]any, 0, len(versions))
	for _, info := range versions {
		data = append(data, learningContentVersionJSON(info))
	}
	writeJSON(writer, http.StatusOK, responseEnvelope{RequestID: requestID(), Data: data})
}

func (service *practiceHTTP) importWorkshopLearningContent(writer http.ResponseWriter, request *http.Request) {
	bankID, _, ok := workshopIDs(writer, request, false)
	if !ok {
		return
	}
	actor, _, allowed := service.workshopActor(writer, request, "quizcraft.workshop.write", &bankID, false)
	if !allowed {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, learningContentMaxBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > learningContentMaxBytes {
		writeError(writer, http.StatusBadRequest, "invalid_request", "learning content must be between 1 byte and 4 MiB")
		return
	}
	contentID := uuid.New()
	storedID := contentID
	// An identical package resolves to the draft that already exists, so the
	// response and the idempotency record name the stored version.
	service.runWorkshopMutationResource(writer, request, actor, "import_learning_content", http.StatusCreated, raw, func() uuid.UUID { return storedID }, func(ctx context.Context, tx pgx.Tx, requestID string) error {
		info, err := ImportLearningContentDraft(ctx, tx, bankID, contentID, raw)
		if err != nil {
			return learningReviewProblem(err)
		}
		storedID = info.ContentVersionID
		return nil
	})
}

func (service *practiceHTTP) approveWorkshopLearningContent(writer http.ResponseWriter, request *http.Request) {
	service.learningContentReview(writer, request, "approve_learning_content", "quizcraft.workshop.publish", func(ctx context.Context, tx pgx.Tx, bankID, contentID, reviewer uuid.UUID, activate, enable bool, note string) error {
		_, err := ApproveLearningContent(ctx, tx, bankID, contentID, reviewer, activate, enable, note)
		return err
	})
}

func (service *practiceHTTP) retireWorkshopLearningContent(writer http.ResponseWriter, request *http.Request) {
	service.learningContentReview(writer, request, "retire_learning_content", "quizcraft.workshop.publish", func(ctx context.Context, tx pgx.Tx, bankID, contentID, reviewer uuid.UUID, _ bool, _ bool, note string) error {
		_, err := RetireLearningContent(ctx, tx, bankID, contentID, reviewer, note)
		return err
	})
}

func (service *practiceHTTP) learningContentReview(writer http.ResponseWriter, request *http.Request, operationKind, permission string, review func(context.Context, pgx.Tx, uuid.UUID, uuid.UUID, uuid.UUID, bool, bool, string) error) {
	bankID, _, ok := workshopIDs(writer, request, false)
	if !ok {
		return
	}
	contentID, ok := learningContentVersionID(writer, request)
	if !ok {
		return
	}
	actor, _, allowed := service.workshopActor(writer, request, permission, &bankID, false)
	if !allowed {
		return
	}
	var input learningContentReviewCommand
	raw, err := decodeBody(request, &input)
	activate := true
	if input.Activate != nil {
		activate = *input.Activate
	}
	if err != nil || !learningReviewNote(input.Note) || (!activate && input.Enable) {
		writeError(writer, http.StatusBadRequest, "invalid_request", "learning content review command is invalid")
		return
	}
	service.runWorkshopMutation(writer, request, actor, operationKind, http.StatusOK, raw, contentID, func(ctx context.Context, tx pgx.Tx, requestID string) error {
		if err := review(ctx, tx, bankID, contentID, *actor.userID, activate, input.Enable, input.Note); err != nil {
			return learningReviewProblem(err)
		}
		return nil
	})
}
