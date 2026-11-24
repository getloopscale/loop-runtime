// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/release"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/manifests"
	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// maxActivationAttempts is the maximum number of times the reconcile loop will
// retry release activation before failing the build outright.
//
// BRC-003 fix (original): prevents a RELEASING build with persistent failures
// (e.g., Gitea down) from blocking the environment for the full 10-minute
// reaper timeout. Original value: 3.
//
// V9 dogfood 2026-05-30 final tuning: increased 3 → 10. Pre-fix
// 3 retries × reaper Interval=30s = 90s budget exhausted under
// dogfood concurrency (5+ journeys racing on shared
// `e2e-tenant-production` ArgoCD app); ArgoCD app-level lock
// queue legitimately takes 3-5 minutes to drain. 10 retries =
// 5min budget — covers worst-case queue drain while keeping
// fast-fail on real infra failure (Gitea down etc still fails
// within 5min, not 90s).
const maxActivationAttempts = 10

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
	// we'd create duplicates up to the attempt cap. Instead, check existing state and decide.
	existingRelease, err := r.orch.FindReleaseForBuild(ctx, build.ID)
	if err != nil {
		// The store separates "no release exists for this build" ((nil, nil))
		// from "the lookup failed" ((nil, err)). Discarding the error collapsed
		// both into nil, skipped the guard below, and reached Activate() —
		// creating the duplicate release the comment above exists to prevent.
		//
		// A failed read is recoverable, so skip the tick rather than fail the
		// user's deploy on one transient error. The attempt counter was already
		// incremented above, so a lookup that fails on every tick still
		// terminates the build through maxActivationAttempts.
		r.logger.Error("reconcile loop: failed to look up existing release for RELEASING build",
			"build_id", build.ID, "attempt", attempts, "error", err)
		return
	}
	if existingRelease != nil {
		switch existingRelease.Status {
		case "ACTIVE":
			// Release already active — just transition build to SUCCEEDED
			r.logger.Info("reconcile loop: release already ACTIVE, completing build",
				"build_id", build.ID, "release_id", existingRelease.ID)
			r.completeBuild(ctx, build)
			return
		case "SYNC_FAILED", "GIT_MIRROR_FAILED":
			// Release permanently failed — fail the build.
			// Plan T2.1 D4: propagate reaper_reason (WHAT/CAUSE/NEXT) into build
			// error_message instead of the legacy technical "Release X failed: Y"
			// string. CLI surfaces this to user with actionable next-step text.
			r.logger.Warn("reconcile loop: release in failed state, failing build",
				"build_id", build.ID, "release_id", existingRelease.ID,
				"release_status", existingRelease.Status)
			r.failBuild(ctx, build, release.FormatBuildFailureReason(existingRelease))
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

	var buildApps []deploycontracts.AppRelease
	for _, app := range apps {
		buildApps = append(buildApps, deploycontracts.BuildAppRelease(
			r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name,
			build.CommitSHA, build.Environment, r.cfg.BaseDomain, app.Type, app.Rendering,
		))
	}

	// CHG-2026-04-29-DOGFOOD iter 8 — re-enrich attestation outputs from
	// the workflow status before manifest building. The retry path was
	// previously rendering manifests with empty digests; with the
	// supply-chain VAP active that produces pods Forbidden at admission.
	// Re-fetching status is cheap (Argo Workflow CR read) and idempotent.
	// Apps whose image was reused ran no image step: the stored reuse fills
	// them first, and the enrichment leaves them alone.
	applyReusedImages(buildApps, apps)
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
	manifestApps, err := deploycontracts.BuildManifestApps(ctx, deploycontracts.BuildManifestAppsInput{
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
		r.failBuild(ctx, build, manifestFailureReason(err))
		return
	}

	// Plan v1.2 T3.1 — propagate Render error instead of panicking the retry loop.
	renderedManifests, renderErr := manifests.Render(manifests.RenderInput{
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
		ProgressiveDelivery: r.cfg.ProgressiveDelivery,
		LeanResources:       r.cfg.LeanResources,
		// T6.2 of deep-review-remediation-plan (closes find_code_006 / CODE-006 MEDIUM):
		// populate tenant plan so canary steps match subscription tier.
		// Pre-T6.2 every Pro/Team/Enterprise tenant silently received
		// Starter canary (50/100 setWeight, no AnalysisRun).
		Plan: r.resolvePlan(ctx, build.TenantID),
	})
	if renderErr != nil {
		r.logger.Error("reconcile loop: manifests.Render failed on retry", "build_id", build.ID, "error", renderErr)
		r.failBuild(ctx, build, "Failed to render manifests on retry — platform configuration error")
		return
	}

	result := deploycontracts.BuildCallbackRequest{
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

	if _, err := r.finalizer().fail(ctx, build, buildFailure{
		reason: reason, route: routeToFailed, stageDeployFailed: true,
	}); err != nil {
		r.logger.Error("reconcile loop: failed to transition build",
			"build_id", build.ID, "error", err)
	}
}
