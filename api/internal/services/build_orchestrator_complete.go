// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/cfkv"
	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
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
func (o *BuildOrchestrator) OnBuildCompleted(ctx context.Context, buildID string, result BuildCallbackRequest) error {
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
			"build callback reported success but provided no apps",
			400,
		)
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

		// Transition to terminal state
		if result.Status == "failed" {
			if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventFailed)); err != nil {
				return fmt.Errorf("on build completed: transition to FAILED: %w", err)
			}

			// Persist error message (F-04: builds must have error_message when FAILED)
			if result.Error != "" {
				if err := o.buildStore.SetErrorMessage(ctx, tx, buildID, result.Error); err != nil {
					slog.Error("failed to set error message on build",
						"build_id", buildID,
						"error", err,
					)
					// Non-fatal: the build already transitioned to FAILED, which is the critical part
				}
			}

			// Emit SSE build.failed with error details
			failedPayload := map[string]string{
				"build_id": buildID,
				"status":   "failed",
			}
			if result.Error != "" {
				failedPayload["error"] = result.Error
			}
			eventData := safeJSONMarshal(failedPayload)
			if _, err := o.eventStore.Append(ctx, buildID, "build.failed", eventData); err != nil {
				return fmt.Errorf("on build completed: emit build.failed: %w", err)
			}

			return nil
		}

		// Transition to SUCCEEDED
		if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventSucceeded)); err != nil {
			return fmt.Errorf("on build completed: transition to SUCCEEDED: %w", err)
		}

		// Emit SSE build.succeeded
		eventData := safeJSONMarshal(map[string]string{
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

	// If build failed, no activation needed -- Phase 1 already emitted build.failed
	if result.Status == "failed" {
		return nil
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
	// Phase 2: Release activation via DeliveryActivator (MEET-2026-03-26-009).
	// Dispatches to the appropriate DeliveryActivator implementation
	// based on build plan. Each sub-phase acquires/releases its own lock.
	// Write CF KV routing entries for static apps BEFORE activation (Sprint 10).
	// The assets are already in S3 at this point (uploaded by Argo Workflow).
	// Writing KV early ensures the CF Worker serves the new version immediately,
	// even if the ArgoCD sync (container delivery) takes longer or fails.
	o.writeStaticRoutingEntries(ctx, build, result)

	activationErr := o.delivery.Activate(ctx, build, result)

	if activationErr != nil {
		// BRC-002 fix: if the delivery activator already handled the failure
		// (emitted SSE event, transitioned release state), skip our own emit
		// to prevent duplicate deploy.sync_failed events in the CLI stream.
		if !errors.Is(activationErr, ErrActivationAlreadyHandled) {
			o.emitActivationFailure(ctx, buildID, activationErr)
		}
		// Build stays in RELEASING so the reconcile loop retries (MEET-2026-03-26-008).
		return nil
	}

	// Phase 2 succeeded -- transition RELEASING -> SUCCEEDED (deploy complete).
	if err := o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock -- another pod's reconcile loop may have already transitioned
		current, err := o.buildStore.GetByID(ctx, buildID)
		if err != nil {
			return fmt.Errorf("on build completed: re-fetch for SUCCEEDED transition: %w", err)
		}
		if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(current.Status)) {
			return nil // Already finalized (race with reconcile loop) -- safe
		}
		if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventReleaseCompleted)); err != nil {
			return fmt.Errorf("on build completed: transition to SUCCEEDED: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("on build completed: failed to transition RELEASING -> SUCCEEDED",
			"build_id", buildID, "error", err)
		return fmt.Errorf("on build completed: RELEASING -> SUCCEEDED: %w", err)
	}

	return nil
}

// writeStaticRoutingEntries writes CF KV entries for static apps in the build.
// This enables the Cloudflare Worker to serve static assets from DO Spaces
// without any cluster involvement (Sprint 10).
func (o *BuildOrchestrator) writeStaticRoutingEntries(ctx context.Context, build *db.Build, result BuildCallbackRequest) {
	if o.kvClient == nil {
		return // CF KV not configured (local dev or not enabled)
	}

	for _, app := range result.Apps {
		// Only write KV for static apps
		if app.Rendering != "static" {
			continue
		}

		// ARCH-004 fix: Use cfkv.Subdomain() instead of inline generation to avoid
		// divergence with the canonical function (missing TrimRight for trailing hyphens).
		subdomain := cfkv.Subdomain(build.TenantID, build.ProjectID, app.Name, build.Environment)

		entry := map[string]string{
			"buildId":  build.ID,
			"s3Prefix": fmt.Sprintf("/%s/%s/%s/%s", build.TenantID, build.ProjectID, app.Name, build.ID),
		}

		if err := o.kvClient.Put(ctx, subdomain, entry); err != nil {
			slog.Error("static routing: failed to write KV entry",
				"subdomain", subdomain,
				"build_id", build.ID,
				"error", err,
			)
			// Non-fatal: assets are still accessible via nginx-s3-gateway in cluster
			continue
		}

		slog.Info("static routing: KV entry written",
			"subdomain", subdomain,
			"build_id", build.ID,
			"url", fmt.Sprintf("https://%s.%s", subdomain, o.baseDomain),
		)
	}
}
