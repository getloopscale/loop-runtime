// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// RetryReleaseActivation retries Phase 2 (release activation) for a build stuck
// in RELEASING state. Called by the reconcile loop when it detects a RELEASING
// build whose Phase 2 was interrupted (pod crash, rollout, Gitea/ArgoCD failure).
//
// MEET-2026-03-26-008: This method does NOT check IsBuildTerminal because
// RELEASING is explicitly non-terminal. It directly runs activateRelease and
// transitions to SUCCEEDED on success or leaves in RELEASING for retry.
func (o *BuildOrchestrator) RetryReleaseActivation(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
	if build.Status != string(statemachine.BPhReleasing) {
		return fmt.Errorf("retry release activation: build %s is in %s state, expected RELEASING", build.ID, build.Status)
	}

	// Phase 4A: Track retry amplification for release activation retries.
	metrics.RetryAmplificationTotal.WithLabelValues("release_activation").Inc()

	activationErr := o.delivery.Activate(ctx, build, result)
	if activationErr != nil {
		slog.Warn("retry release activation: Phase 2 failed, will retry on next tick",
			"build_id", build.ID, "error", activationErr)
		o.emitActivationFailure(ctx, build.ID, activationErr)
		return activationErr
	}

	// Phase 2 succeeded -- transition RELEASING -> SUCCEEDED (inside lock to handle races)
	return o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock -- another pod may have already completed this
		current, err := o.buildStore.GetByID(ctx, build.ID)
		if err != nil {
			return fmt.Errorf("retry release activation: re-fetch build: %w", err)
		}
		if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(current.Status)) {
			return nil // Already finalized by another pod -- safe to skip
		}
		if _, err := o.buildStore.UpdateStatus(ctx, tx, current.ID, string(statemachine.BPEventReleaseCompleted)); err != nil {
			return fmt.Errorf("retry release activation: transition to SUCCEEDED: %w", err)
		}
		return nil
	})
}
