// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/events"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// OnBuildCompleted handles the callback from the build system when a build finishes.
//
// Two-phase design (DA-17):
//   - Phase 1 (Lock): Transition build to terminal state (SUCCEEDED/FAILED).
//     This MUST commit independently so the build never stays stuck in RUNNING.
//   - Phase 2 (Lock): Activate release (create release, sync, health probe).
//     If this fails, the build is already SUCCEEDED and a failure event is emitted.
//
// Race condition guard (DA-15 EC-N): If build is QUEUED (Argo finished before
// SubmitBuild transitioned to RUNNING), transitions QUEUED->RUNNING first.
//
// Duplicate callbacks: If build is already in a terminal state, returns nil (no-op).
func (o *BuildOrchestrator) OnBuildCompleted(ctx context.Context, buildID string, result deploycontracts.BuildCallbackRequest) error {
	// Step 1: get build by ID (read-only, outside lock -- for lock coordinates)
	build, err := o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("on build completed: get build: %w", err)
	}

	// Fast path: duplicate callback -- build already terminal -> no-op (no lock needed)
	if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		return nil
	}

	// Validation before acquiring lock (stateless check)
	if result.Status == "succeeded" && len(result.Apps) == 0 {
		return loopzeroerrors.NewLoopZeroError(
			"INVALID_CALLBACK",
			"build callback reported success but provided no apps")
	}

	// A failed build is finalized by the shared failure path: it routes QUEUED
	// (the EC-N race, Argo finishing before SubmitBuild moved it to RUNNING)
	// through RUNNING, persists the error, stages deploy.failed and, after the
	// commit, emits build.failed. No activation follows.
	if result.Status == "failed" {
		if _, err := o.finalizer().fail(ctx, build, buildFailure{
			reason: result.Error, route: routeToFailed, stageDeployFailed: true,
		}); err != nil {
			return fmt.Errorf("on build completed: %w", err)
		}
		return nil
	}

	// Phase 1 (Lock 1): Transition build to terminal state.
	// This transaction MUST commit independently so the build never gets stuck in RUNNING.
	err = o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch build inside lock (state may have changed)
		build, err = o.buildStore.GetByID(ctx, buildID)
		if err != nil {
			return fmt.Errorf("on build completed: re-fetch build: %w", err)
		}

		// Re-check terminal state inside lock (concurrent cancel may have completed)
		if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
			return nil
		}

		// EC-N race condition guard: build still in QUEUED (Argo finished before SubmitBuild transitioned)
		if build.Status == string(statemachine.BPhQueued) {
			if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventStarted)); err != nil {
				return fmt.Errorf("on build completed: EC-N transition to RUNNING: %w", err)
			}
		}

		// Transition to SUCCEEDED
		if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventSucceeded)); err != nil {
			return fmt.Errorf("on build completed: transition to SUCCEEDED: %w", err)
		}

		// Emit SSE build.succeeded
		eventData := events.SafeJSONMarshal(map[string]string{
			"build_id": buildID,
			"status":   "succeeded",
		})
		if _, err := o.eventStore.Append(ctx, buildID, "build.succeeded", eventData); err != nil {
			return fmt.Errorf("on build completed: emit build.succeeded: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Re-fetch build after Phase 1 commit to get updated state
	build, err = o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("on build completed: re-fetch after phase 1: %w", err)
	}

	// T1.2 mitigation #3 of MEET-2026-05-12: emit per-sign cosign audit
	// for each app whose callback reported a digest + signature. Fires
	// AFTER the build is durably SUCCEEDED but BEFORE release activation
	// so the supply-chain trail is intact even if activation later fails.
	// Nil-safe + fail-open inside the helper.
	emitCosignSignAudits(ctx, o.auditLogger, &buildAuditContext{
		BuildID:   build.ID,
		TenantID:  build.TenantID,
		CreatedBy: build.CreatedBy,
	}, result.Apps, build.CorrelationID)

	// If build was cancelled concurrently during Phase 1, skip activation
	if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		return nil
	}

	// Build should be in RELEASING state after Phase 1 (MEET-2026-03-26-008).
	// Phase 2: Release activation via the DeliveryActivator wired at the
	// composition root (MEET-2026-03-26-009). Each sub-phase acquires and
	// releases its own lock.
	activationErr := o.activate(ctx, build, result)

	if activationErr != nil {
		if o.failOnUndeliveredSecrets(ctx, build, activationErr) {
			return nil
		}
		// BRC-002 fix: if the delivery activator already handled the failure
		// (emitted SSE event, transitioned release state), skip our own emit
		// to prevent duplicate deploy.sync_failed events in the CLI stream.
		if !errors.Is(activationErr, deploycontracts.ErrActivationAlreadyHandled) {
			o.emitActivationFailure(ctx, buildID, activationErr)
		}
		// Build stays in RELEASING so the reconcile loop retries (MEET-2026-03-26-008).
		return nil
	}

	// Phase 2 succeeded -- transition RELEASING -> SUCCEEDED (deploy complete).
	if err := o.finalizer().succeed(ctx, build); err != nil {
		slog.Error("on build completed: failed to transition RELEASING -> SUCCEEDED",
			"build_id", buildID, "error", err)
		return fmt.Errorf("on build completed: RELEASING -> SUCCEEDED: %w", err)
	}

	return nil
}
