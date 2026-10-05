package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"henukit.dev/portal-gateway/internal/practice"
)

// learningReportPreferences reads the signed-in owner's course-scoped feedback
// preferences for one bank. Missing preferences are the Core's disabled
// defaults, so this read never invents a local default.
func (h *Handler) learningReportPreferences(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	h.learningReportRead(w, r, func(userID, requestID string) (any, error) {
		return h.quizCraft.LearningReportPreferences(r.Context(), userID, requestID, bankID)
	})
}

// latestLearningReport reads the newest published report of the signed-in
// owner. A 404 from the Core is passed through as 404: the Gateway never
// substitutes a stale report or another user's report.
func (h *Handler) latestLearningReport(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	h.learningReportRead(w, r, func(userID, requestID string) (any, error) {
		return h.quizCraft.LatestLearningReport(r.Context(), userID, requestID, bankID)
	})
}

// learningReportTask reads one report task's progress for the signed-in owner.
func (h *Handler) learningReportTask(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	taskID := chi.URLParam(r, "task_id")
	h.learningReportRead(w, r, func(userID, requestID string) (any, error) {
		return h.quizCraft.LearningReportTask(r.Context(), userID, requestID, bankID, taskID)
	})
}

// learningReportRead is the shared actor-bound read path for learning reports.
// It mirrors the favorites read: no guest downgrade, Platform Core permission
// checked per request, dependency failures fail closed. Unlike the other
// actor-bound reads it preserves a Core 404, because "no report yet" is a real
// member state the surface has to show.
func (h *Handler) learningReportRead(w http.ResponseWriter, r *http.Request, read func(userID, requestID string) (any, error)) {
	setPrivateResponseHeaders(w)
	if !h.learningReportsEnabled || h.quizCraft == nil {
		writeError(w, r, http.StatusServiceUnavailable, "practice learning reports are not enabled", "学习报告暂时不可用，请稍后再试")
		return
	}
	value, err := h.readSession(r)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "not authenticated", "登录已过期，请重新登录")
		return
	}
	if err := h.platform.CheckPermission(r.Context(), value.ExchangeToken, practice.CatalogReadPermission); err != nil {
		h.writePracticeReadPermissionError(w, r, err)
		return
	}
	envelope, err := read(value.UserID, requestIDOf(w, r))
	if err != nil {
		if errors.Is(err, practice.ErrLearningReportNotFound) {
			writeError(w, r, http.StatusNotFound, "learning report not found", "暂时没有可查看的学习报告")
			return
		}
		writeError(w, r, http.StatusServiceUnavailable, "practice learning reports are temporarily unavailable", "学习报告暂时不可用，请稍后再试: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, envelope)
}
