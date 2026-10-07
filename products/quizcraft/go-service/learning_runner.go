package quizcraft

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// LearningModelCall receives ONLY the bounded, validated provider payload;
// production must supply a separately reviewed, configured provider adapter.
// No worker loop or provider is enabled merely by compiling this function.
type LearningModelCall func(context.Context, LearningModelInput, LearningJobVersions) ([]byte, error)

// ProcessNextLearningReport performs one bounded lease without keeping a DB
// transaction open across membership or provider calls. A signed, uncached
// Account Portfolio client is mandatory; a synthetic callback is not accepted.
func (s *Service) ProcessNextLearningReport(ctx context.Context, client *LearningEntitlementClient, model LearningModelCall, current LearningJobVersions, duration time.Duration) (bool, error) {
	if s == nil || s.database == nil || client == nil || model == nil || !validLearningJobVersions(current) || !validLearningLeaseDuration(duration) {
		return false, ErrLearningInvalidJob
	}
	lease, claimed, err := s.ClaimNextLearningReport(ctx, duration)
	if err != nil || !claimed {
		return claimed, err
	}
	// Failure writes use the lease token and the normal preference -> job lock
	// order. A caller cancellation should not leave a live lease stranded.
	finish := func(reason string) (bool, error) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, failed := s.FailLearningLease(cleanupCtx, lease, reason)
		if failed != nil && !errors.Is(failed, ErrLearningLeaseLost) {
			return true, failed
		}
		return true, ErrLearningUnavailable
	}
	if lease.Snapshot.Versions != current {
		return finish("policy_changed")
	}
	// Reserve time for the second check and the publication transaction.
	callCtx, cancel := context.WithDeadline(ctx, lease.LeaseUntil.Add(-5*time.Second))
	defer cancel()
	allowed, err := client.CheckLifetime(callCtx, lease.UserID)
	if err != nil {
		return finish("entitlement_unavailable")
	}
	if !allowed {
		return finish("entitlement_revoked")
	}
	var decision []byte
	hasAttempts := false
	for _, stat := range lease.Snapshot.Evidence.Statistics {
		hasAttempts = hasAttempts || stat.AttemptCount > 0
	}
	if hasAttempts {
		var raw []byte
		err = s.database.QueryRow(callCtx, `SELECT document FROM quizcraft_learning_content_versions
            WHERE bank_id=$1 AND id=$2 AND status='approved'`, lease.BankID, lease.Snapshot.Evidence.ContentVersionID).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return finish("content_changed")
		}
		if err != nil || len(raw) > learningContentMaxBytes {
			return finish("worker_error")
		}
		var document LearningContentDocument
		if json.Unmarshal(raw, &document) != nil {
			return finish("worker_error")
		}
		input, err := BuildLearningModelInput(lease.Snapshot.Evidence, document)
		if err != nil {
			return finish("worker_error")
		}
		decision, err = model(callCtx, input, current)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
				return finish("provider_timeout")
			}
			return finish("provider_error")
		}
		if _, err := ValidateLearningModelDecision(decision, lease.Snapshot.Evidence, document); err != nil {
			return finish("invalid_model_result")
		}
		// The member may have been revoked during a slow model call. Nothing
		// past this point may publish on the basis of the earlier decision.
		allowed, err = client.CheckLifetime(callCtx, lease.UserID)
		if err != nil {
			return finish("entitlement_unavailable")
		}
		if !allowed {
			return finish("entitlement_revoked")
		}
	}
	_, err = s.PublishLearningReport(callCtx, lease, decision, current, client.CheckLifetime)
	if err != nil {
		if errors.Is(err, ErrLearningLeaseLost) {
			return true, ErrLearningUnavailable
		}
		if errors.Is(err, ErrLearningUnavailable) {
			return finish("entitlement_unavailable")
		}
		return finish("worker_error")
	}
	return true, nil
}
