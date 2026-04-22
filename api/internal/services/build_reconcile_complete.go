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

// onWorkflowSucceeded handles a completed build workflow by creating a release
// and activating it through the standard flow (gitops push → ArgoCD sync).
func (r *BuildReconcileLoop) onWorkflowSucceeded(ctx context.Context, build *db.Build) {
	r.logger.Info("reconcile loop: workflow succeeded, processing build completion",
		"build_id", build.ID, "workflow_id", build.WorkflowID)

	metrics.BuildsTotal.WithLabelValues(build.TenantID, build.Environment, "succeeded").Inc()
	metrics.BuildDuration.WithLabelValues(build.TenantID, build.Environment, "succeeded").Observe(time.Since(build.CreatedAt).Seconds())
	metrics.BuildQueueDepth.Dec()

	// Compute image references from deterministic pattern
	var buildApps []AppRelease
	var manifestApps []manifests.AppManifestInput

	// Parse the apps from the build record (stored at submission time)
	apps, err := r.buildStore.GetBuildApps(ctx, build.ID)
	if err != nil {
		r.logger.Error("reconcile loop: failed to get build apps",
			"build_id", build.ID, "error", err)
		r.failBuild(ctx, build, "Failed to read build apps from database")
		return
	}

	// Build AppRelease list for the build callback (URL computation, etc.)
	for _, app := range apps {
		buildApps = append(buildApps, BuildAppRelease(
			r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name,
			build.CommitSHA, build.Environment, r.cfg.BaseDomain, app.Type, app.Rendering,
		))
	}

	// F-010: shared manifest building function (includes custom domains).
	// CODE-002: errors are fatal — prevents silent IngressRoute erasure.
	manifestApps, err = BuildManifestApps(ctx, BuildManifestAppsInput{
		RegistryURL: r.cfg.RegistryURL,
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CommitSHA:   build.CommitSHA,
		Apps:        apps,
		DomainStore: r.domainStore,
	})
	if err != nil {
		r.logger.Error("reconcile loop: manifest building failed", "build_id", build.ID, "error", err)
		r.failBuild(ctx, build, "Failed to build manifests with custom domains")
		return
	}

	// Generate manifests in the API (C-02 fix: control plane generates manifests)
	renderedManifests := manifests.Render(manifests.RenderInput{
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		BuildID:     build.ID,
		ReleaseID:   "",
		BaseDomain:  r.cfg.BaseDomain,
		Apps:        manifestApps,
	})

	// Ensure LoopZeroApplication CRs exist on mgmt cluster (non-fatal).
	// Enables hostname assignment, ExposedRoute creation, TLS monitoring, reachability probes.
	r.ensureApplicationCRs(ctx, build, apps)

	// Construct the completion result and delegate to the orchestrator
	result := BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      buildApps,
		Manifests: renderedManifests,
	}

	if err := r.orch.OnBuildCompleted(ctx, build.ID, result); err != nil {
		// F-01 fix: track completion failures. After 3 attempts, fail the build
		// to prevent permanent RUNNING state when completion processing is broken.
		attemptsVal, _ := r.completionAttempts.LoadOrStore(build.ID, 0)
		attempts := attemptsVal.(int) + 1
		r.completionAttempts.Store(build.ID, attempts)

		if attempts >= maxActivationAttempts {
			r.logger.Error("reconcile loop: build completion failed after max attempts, failing build",
				"build_id", build.ID, "attempts", attempts, "error", err)
			r.completionAttempts.Delete(build.ID)
			r.failBuild(ctx, build, fmt.Sprintf("Build completion processing failed after %d attempts: %v", attempts, err))
			return
		}

		r.logger.Error("reconcile loop: build completion processing failed, will retry",
			"build_id", build.ID, "attempt", attempts, "error", err)
	} else {
		r.completionAttempts.Delete(build.ID)
	}
}

// completeBuild transitions a RELEASING build to SUCCEEDED (MEET-2026-03-26-008).
// Called when the reconcile loop detects that the release is already ACTIVE.
func (r *BuildReconcileLoop) completeBuild(ctx context.Context, build *db.Build) {
	r.activationAttempts.Delete(build.ID) // BRC-003: cleanup on success
	err := r.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		current, err := r.buildStore.GetByID(ctx, build.ID)
		if err != nil {
			return err
		}
		if statemachine.IsBuildTerminal(statemachine.State(current.Status)) {
			return nil // Already finalized
		}
		if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildReleaseCompleted)); err != nil {
			return fmt.Errorf("complete build: transition to SUCCEEDED: %w", err)
		}
		return nil
	})
	if err != nil {
		r.logger.Error("reconcile loop: failed to complete RELEASING build",
			"build_id", build.ID, "error", err)
	}
}

// ensureApplicationCRs creates LoopZeroApplication CRs on the mgmt cluster for each app
// in the build. This wires the ApplicationController and ExposureController to provide
// hostname assignment, ExposedRoute creation, TLS monitoring, and reachability probes.
// CHG-2026-04-08-001: Non-fatal — failure is logged but does not block the deploy.
func (r *BuildReconcileLoop) ensureApplicationCRs(ctx context.Context, build *db.Build, apps []db.BuildAppSpec) {
	if r.appCRMgr == nil {
		r.logger.Debug("reconcile loop: appCRMgr not configured, skipping LoopZeroApplication CR creation")
		return
	}
	for _, app := range apps {
		port := int32(app.Port)
		if port == 0 {
			port = 80
		}
		input := ApplicationCRInput{
			TenantID:    build.TenantID,
			Project:     build.ProjectID,
			Environment: build.Environment,
			AppName:     app.Name,
			ServiceName: app.Name,
			ServicePort: port,
		}
		if err := r.appCRMgr.EnsureLoopZeroApplication(ctx, input); err != nil {
			metrics.ApplicationCREnsureTotal.WithLabelValues(build.TenantID, app.Name, "failure").Inc()
			r.logger.Warn("reconcile loop: failed to ensure LoopZeroApplication CR (non-fatal)",
				"build_id", build.ID, "app", app.Name, "error", err)
		} else {
			metrics.ApplicationCREnsureTotal.WithLabelValues(build.TenantID, app.Name, "success").Inc()
			r.logger.Info("reconcile loop: LoopZeroApplication CR ensured",
				"build_id", build.ID, "app", app.Name,
				"tenant", build.TenantID, "environment", build.Environment)
		}
	}
}
