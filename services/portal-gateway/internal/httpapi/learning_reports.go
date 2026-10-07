package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"henukit.dev/portal-gateway/internal/practice"
)

// learningEntitlementRequiredCode is the one Core denial the read path forwards
// by name: it is the code Portal renders as the membership entry. Any other 403
// keeps the shared practice mapping, because a code Portal cannot look up costs
// the member the actionable message and leaves only a generic denial.
//
// Only the comparison below uses it: every write site spells the literal on
// purpose, because the Portal scan in gateway-errors.test.ts looks for
// `writeError(..., "code")` literals and would stop guarding this code.
const learningEntitlementRequiredCode = "learning_entitlement_required"

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
		if errors.Is(err, practice.ErrPortalReadForbidden) {
			// Core denies these reads when the membership lapsed, and that is the
			// only reason this path can name. Saying so is the difference between a
			// member who can renew and a member who is told the feature is broken.
			// A 403 Core did not explain — or explained with a code Portal does not
			// render — stays a denial but falls back to the shared practice wording:
			// asserting a membership problem we did not verify would send the member
			// to check something that is fine.
			if practice.RejectedCode(err) == learningEntitlementRequiredCode {
				writeError(w, r, http.StatusForbidden, "learning_entitlement_required", "学习报告需要有效的会员权益，请确认会员状态后再试")
				return
			}
			writeError(w, r, http.StatusForbidden, "practice access denied", "暂无练习权限。如有疑问，请到账户中心提交工单。")
			return
		}
		// The dependency error stays in the log-only path: the browser message is
		// shown to members verbatim by Portal, so it must never carry transport or
		// upstream detail. The request_id in the envelope is enough to correlate.
		writeError(w, r, http.StatusServiceUnavailable, "practice learning reports are temporarily unavailable", "学习报告暂时不可用，请稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, envelope)
}

// learningReportWrite routes one learning-report write. The cutover flag is
// checked first because the shared command skeleton only knows about the
// command client: with the command client present but the learning surface
// still dark, an unwired write must answer an honest 503 rather than reach Core.
func (h *Handler) learningReportWrite(w http.ResponseWriter, r *http.Request, successStatus int, readBody bool, command practiceCommand) {
	if !h.learningReportsEnabled {
		writeError(w, r, http.StatusServiceUnavailable, "practice learning reports are not enabled", "学习报告暂时不可用，请稍后再试")
		return
	}
	h.practiceCommand(w, r, successStatus, false, readBody, "请先登录后再使用学习报告", command)
}

// updateLearningReportPreferences saves preferences and consent. Core decides
// whether the change needs a live entitlement (enabling/narrowing does, turning
// generation off does not); Gateway forwards the browser body unchanged.
func (h *Handler) updateLearningReportPreferences(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	h.learningReportWrite(w, r, http.StatusOK, true, func(ctx context.Context, actorUserID, requestID, idempotencyKey string, raw []byte, anonymousCookie *http.Cookie) (practice.CommandResult, error) {
		return h.practiceCommands.UpdateLearningReportPreferences(ctx, bankID, actorUserID, requestID, idempotencyKey, raw, anonymousCookie)
	})
}

// requestLearningReport asks Core to queue one manual report request. Core
// answers with the task envelope either way; the envelope's task status tells
// the browser whether work was queued or an identical request was reused.
func (h *Handler) requestLearningReport(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	h.learningReportWrite(w, r, http.StatusAccepted, false, func(ctx context.Context, actorUserID, requestID, idempotencyKey string, raw []byte, anonymousCookie *http.Cookie) (practice.CommandResult, error) {
		return h.practiceCommands.RequestLearningReport(ctx, bankID, actorUserID, requestID, idempotencyKey, anonymousCookie)
	})
}

// clearLearningReports withdraws the owner's derived reports and queued work.
//
// It is deliberately the one learning write that ignores the cutover gate.
// Withdrawing data and consent has to stay possible after the feature is turned
// off in a rollback, and clearing already revokes both: Core's clear route sets
// enabled=false and external_analysis_consent=false without checking membership
// or content review; it still requires the course to have a published version.
// Everything else stays 503 while dark.
func (h *Handler) clearLearningReports(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	h.practiceCommand(w, r, http.StatusOK, false, false, "请先登录后再使用学习报告", func(ctx context.Context, actorUserID, requestID, idempotencyKey string, raw []byte, anonymousCookie *http.Cookie) (practice.CommandResult, error) {
		return h.practiceCommands.ClearLearningReports(ctx, bankID, actorUserID, requestID, idempotencyKey, anonymousCookie)
	})
}

// createLearningReportSession starts practice from the verified report
// recommendation. The browser sends no question ids.
func (h *Handler) createLearningReportSession(w http.ResponseWriter, r *http.Request) {
	bankID := chi.URLParam(r, "bank_id")
	reportID := chi.URLParam(r, "report_id")
	h.learningReportWrite(w, r, http.StatusCreated, false, func(ctx context.Context, actorUserID, requestID, idempotencyKey string, raw []byte, anonymousCookie *http.Cookie) (practice.CommandResult, error) {
		return h.practiceCommands.CreateLearningReportPracticeSession(ctx, bankID, reportID, actorUserID, requestID, idempotencyKey, anonymousCookie)
	})
}
