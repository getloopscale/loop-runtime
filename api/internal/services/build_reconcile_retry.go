// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// maxActivationAttempts is the maximum number of times the reconcile loop will
// retry release activation before failing the build outright.
// BRC-003 fix: prevents a RELEASING build with persistent failures (e.g., Gitea down)
// from blocking the environment for the full 10-minute reaper timeout.
const maxActivationAttempts = 3

// retryReleaseActivation handles builds in RELEASING state (MEET-2026-03-26-008).
// These are builds where Phase 1 (image build) completed but Phase 2 (release
// activation) was interrupted. The method reconstructs the BuildCallbackRequest
// from stored data and delegates to the orchestrator.
//
// Guard: if a release already exists for this build, do NOT create another one.
// Check the existing release's state and decide whether to retry or fail.
func (r *BuildReconcileLoop) retryReleaseActivation(ctx context.Context, build *db.Build) {
	// BRC-003 fix: track and limit activation attempts. Without this, a RELEASING
	// build with persistent failures (e.g., Gitea down) blocks the environment for
	// the full 10-minute reaper timeout. After maxActivationAttempts failures, fail
	// the build immediately so the user can re-deploy.
	attemptsVal, _ := r.activationAttempts.LoadOrStore(build.ID, 0)
	attempts := attemptsVal.(int) + 1
	r.activationAttempts.Store(build.ID, attempts)

	if attempts > maxActivationAttempts {
		r.logger.Error("reconcile loop: activation exceeded max attempts, failing build",
			"build_id", build.ID, "attempts", attempts, "max", maxActivationAttempts)
		r.activationAttempts.Delete(build.ID)
		r.failBuild(ctx, build, fmt.Sprintf("Release activation failed after %d attempts", maxActivationAttempts))
		return
	}

	r.logger.Info("reconcile loop: retrying release activation for RELEASING build",
		"build_id", build.ID, "age", time.Since(build.CreatedAt).String(), "attempt", attempts)

	// Guard: check if a release already exists for this build.
	// Activate() creates a new release on each call. If we call it again after a
	// previous attempt created a release that failed (SYNC_FAILED, GIT_MIRROR_FAILED),
	// we'd create duplicates in an infinite loop. Instead, check existing state and decide.
	existingRelease, _ := r.orch.FindReleaseForBuild(ctx, build.ID)
	if existingRelease != nil {
		switch existingRelease.Status {
		case "ACTIVE":
			// Release already active — just transition build to SUCCEEDED
			r.logger.Info("reconcile loop: release already ACTIVE, completing build",
				"build_id", build.ID, "release_id", existingRelease.ID)
			r.completeBuild(ctx, build)
			return
		case "SYNC_FAILED", "GIT_MIRROR_FAILED":
			// Release permanently failed — fail the build
			r.logger.Warn("reconcile loop: release in failed state, failing build",
				"build_id", build.ID, "release_id", existingRelease.ID,
				"release_status", existingRelease.Status)
			r.failBuild(ctx, build, fmt.Sprintf("Release %s failed: %s", existingRelease.ID, existingRelease.Status))
			return
		default:
			// Release in transient state (READY, PENDING) — wait for it to resolve
			r.logger.Debug("reconcile loop: release in transient state, skipping",
				"build_id", build.ID, "release_id", existingRelease.ID,
				"release_status", existingRelease.Status)
			return
		}
	}

	// Reconstruct BuildCallbackRequest from stored app specs (same pattern as onWorkflowSucceeded)
	apps, err := r.buildStore.GetBuildApps(ctx, build.ID)
	if err != nil {
		r.logger.Error("reconcile loop: failed to get build apps for RELEASING build",
			"build_id", build.ID, "error", err)
		return
	}
	if len(apps) == 0 {
		r.logger.Error("reconcile loop: RELEASING build has no stored apps, failing",
			"build_id", build.ID)
		r.failBuild(ctx, build, "Release activation failed: no apps spec stored")
		return
	}

	var buildApps []AppRelease
	for _, app := range apps {
		buildApps = append(buildApps, BuildAppRelease(
			r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name,
			build.CommitSHA, build.Environment, r.cfg.BaseDomain, app.Type, app.Rendering,
		))
	}

	// CHG-2026-04-29-DOGFOOD iter 8 — re-enrich attestation outputs from
	// the workflow status before manifest building. The retry path was
	// previously rendering manifests with empty digests; with the
	// supply-chain VAP active that produces pods Forbidden at admission.
	// Re-fetching status is cheap (Argo Workflow CR read) and idempotent.
	if r.workflow != nil && build.WorkflowID != "" {
		if status, sErr := r.workflow.GetStatus(ctx, "argo", build.WorkflowID); sErr == nil && status != nil {
			enrichAppReleasesWithAttestation(buildApps, status)
		}
	}
	digests := make(map[string]string, len(buildApps))
	for _, ar := range buildApps {
		if ar.ImageDigestSHA256 != "" {
			digests[ar.Name] = ar.ImageDigestSHA256
		}
	}

	// F-010: shared manifest building (retry path)
	manifestApps, err := BuildManifestApps(ctx, BuildManifestAppsInput{
		RegistryURL: r.cfg.RegistryURL,
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CommitSHA:   build.CommitSHA,
		Apps:        apps,
		AppDigests:  digests,
		DomainStore: r.domainStore,
	})
	if err != nil {
		r.logger.Error("reconcile loop: manifest building failed on retry", "build_id", build.ID, "error", err)
		r.failBuild(ctx, build, "Failed to build manifests with custom domains")
		return
	}

	renderedManifests := manifests.Render(manifests.RenderInput{
		TenantID:          build.TenantID,
		ProjectID:         build.ProjectID,
		Environment:       build.Environment,
		BuildID:           build.ID,
		ReleaseID:         "",
		BaseDomain:        r.cfg.BaseDomain,
		ClusterIssuerName: r.cfg.ClusterIssuer, // B14
		Apps:              manifestApps,
		// argo-rollouts-progressive-delivery-plan T1.2.
		PrometheusAddress:   r.cfg.PrometheusAddress,
		ProgressiveDelivery: manifests.ProgressiveDeliveryEnabled(),
	})

	result := BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      buildApps,
		Manifests: renderedManifests,
	}

	if err := r.orch.RetryReleaseActivation(ctx, build, result); err != nil {
		r.logger.Error("reconcile loop: release activation retry failed",
			"build_id", build.ID, "error", err)
	}
}

// failBuild transitions a build to FAILED inside the environment lock.
func (r *BuildReconcileLoop) failBuild(ctx context.Context, build *db.Build, reason string) {
	metrics.BuildsTotal.WithLabelValues(build.TenantID, build.Environment, "failed").Inc()
	metrics.BuildDuration.WithLabelValues(build.TenantID, build.Environment, "failed").Observe(time.Since(build.CreatedAt).Seconds())
	metrics.BuildQueueDepth.Dec()

	err := r.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock — state may have changed
		current, err := r.buildStore.GetByID(ctx, build.ID)
		if err != nil {
			return err
		}
		if statemachine.IsBuildTerminal(statemachine.State(current.Status)) {
			return nil // Already finalized
		}

		// Route to the correct failure event based on current state
		switch statemachine.State(current.Status) {
		case statemachine.BuildQueued:
			// QUEUED → RUNNING first (state machine requires it), then RUNNING → FAILED
			if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildStarted)); err != nil {
				return fmt.Errorf("transition to RUNNING: %w", err)
			}
			if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildFailedEvent)); err != nil {
				return fmt.Errorf("transition to FAILED: %w", err)
			}
		case statemachine.BuildRunning:
			if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildFailedEvent)); err != nil {
				return fmt.Errorf("transition to FAILED: %w", err)
			}
		case statemachine.BuildReleasing:
			// MEET-008: RELEASING → FAILED via release-specific event
			if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildReleaseFailedEvent)); err != nil {
				return fmt.Errorf("transition RELEASING to FAILED: %w", err)
			}
		default:
			return fmt.Errorf("unexpected build state %q for failBuild", current.Status)
		}

		// Set error message
		if err := r.buildStore.SetErrorMessage(ctx, tx, build.ID, reason); err != nil {
			return fmt.Errorf("set error message: %w", err)
		}

		return nil
	})

	if err != nil {
		r.logger.Error("reconcile loop: failed to transition build",
			"build_id", build.ID, "error", err)
		return
	}

	// Emit SSE build.failed event so the CLI gets notified
	eventData := safeJSONMarshal(map[string]string{
		"build_id": build.ID,
		"status":   "failed",
		"error":    reason,
	})
	if _, err := r.eventStore.Append(ctx, build.ID, "build.failed", eventData); err != nil {
		r.logger.Error("reconcile loop: failed to emit SSE event",
			"build_id", build.ID, "error", err)
	}

	r.logger.Warn("reconcile loop: build failed",
		"build_id", build.ID, "reason", reason)

	if onBuildFailed != nil {
		// T6.4 of deep-review-remediation-plan (closes find_code_004 / CODE-004 MEDIUM).
		// Use context.WithoutCancel(ctx) instead of context.Background():
		// preserves request-scoped values (correlation ID, OTel trace span)
		// across the goroutine boundary, but does NOT propagate cancellation
		// from the inbound request (the callback must survive request close).
		// Then add a 30s timeout cap. Pre-fix: trace context lost, OTel span
		// for the callback was orphaned.
		parentCtx := context.WithoutCancel(ctx)
		go func() {
			cbCtx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
			defer cancel()
			onBuildFailed(cbCtx, build.TenantID, build.ProjectID, build.CommitSHA, build.ID)
		}()
	}
}
