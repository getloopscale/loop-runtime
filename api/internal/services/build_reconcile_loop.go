// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// BuildReconcileLoopConfig configures the reconcile loop.
type BuildReconcileLoopConfig struct {
	// Interval is how often the loop polls for non-terminal builds.
	Interval time.Duration
	// RegistryURL is used to compute image references (e.g., "k3d-loopzero-registry:5000").
	RegistryURL string
	// BaseDomain is the base domain for app hostnames (e.g., "loopzero.local").
	BaseDomain string
	// MaxConcurrent is the maximum number of builds reconciled concurrently.
	// Bounds pressure on the Kubernetes API and Argo Workflow queries.
	// Default: 10.
	MaxConcurrent int
}

// BuildReconcileLoop continuously reconciles non-terminal builds against
// the actual Argo Workflow CRD state in the build cluster.
//
// C-01/C-03 fix: This replaces the HTTP callback and the StartupReconciler.
// Instead of the build cluster calling the API (violating the network boundary),
// the API polls the build cluster's Workflow CRDs.
//
// The control plane OBSERVES the build plane — the build plane is write-only
// for its own CRDs and artifacts. No cross-cluster HTTP from build to control.
//
// On workflow completion:
//   - Succeeded: generate manifests, push to GitOps, create Release, trigger ArgoCD sync
//   - Failed: transition build to FAILED with error from workflow status
//
// This loop also subsumes the StartupReconciler: on boot, it immediately catches up
// with any workflows that completed while the API was down.
type BuildReconcileLoop struct {
	buildStore db.BuildStore
	workflow   WorkflowSubmitter
	orch       *BuildOrchestrator
	locker     EnvironmentLocker
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger *slog.Logger
	cfg    BuildReconcileLoopConfig

	// sem bounds concurrent build reconciliations. Initialized once in constructor.
	// REVIEW-MOD-2: moved from per-reconcile allocation to struct field so
	// goroutines from previous ticks continue to hold semaphore slots.
	sem chan struct{}

	// active tracks build IDs currently being processed to avoid
	// duplicate processing and coordinate with BuildReaper (MOD-6 fix).
	active sync.Map
}

// NewBuildReconcileLoop creates a new BuildReconcileLoop.
func NewBuildReconcileLoop(
	buildStore db.BuildStore,
	workflow WorkflowSubmitter,
	orch *BuildOrchestrator,
	locker EnvironmentLocker,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	logger *slog.Logger,
	cfg BuildReconcileLoopConfig,
) *BuildReconcileLoop {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	return &BuildReconcileLoop{
		buildStore: buildStore,
		workflow:   workflow,
		orch:       orch,
		locker:     locker,
		eventStore: eventStore,
		logger:     logger,
		cfg:        cfg,
		sem:        make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Start runs the reconcile loop until the context is cancelled.
// It runs one cycle immediately on start (catching up on missed workflows from
// API downtime) and then polls at the configured interval.
func (r *BuildReconcileLoop) Start(ctx context.Context) {
	r.logger.Info("build reconcile loop started",
		"interval", r.cfg.Interval,
		"registry_url", r.cfg.RegistryURL,
		"base_domain", r.cfg.BaseDomain)

	// Immediate reconciliation on boot (replaces StartupReconciler)
	r.reconcile(ctx)

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("build reconcile loop stopped")
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

// reconcile checks all non-terminal builds and reconciles their state
// against the Argo Workflow CRDs in the build cluster.
//
// BLOCKER-3 fix: Builds are processed concurrently using a semaphore pattern
// (bounded to MaxConcurrent). Previously, serial iteration meant build #20
// could wait ~40 minutes while earlier builds blocked on ArgoCD WaitForSync.
//
// REVIEW-MOD-2: Fire-and-forget — reconcile dispatches goroutines and returns
// immediately. The next ticker tick can dispatch new builds even if previous
// ones are still waiting for ArgoCD sync. The semaphore (r.sem) bounds total
// concurrent processing across ticks. The r.active map prevents duplicate
// processing of the same build across ticks.
//
// MOD-6 fix: Each build is tracked in r.active while being processed.
// BuildReaper checks this to avoid competing on the same build.
func (r *BuildReconcileLoop) reconcile(ctx context.Context) {
	builds, err := r.buildStore.FindActiveBuilds(ctx)
	if err != nil {
		r.logger.Error("reconcile loop: failed to find active builds", "error", err)
		return
	}

	if len(builds) == 0 {
		return
	}

	for _, build := range builds {
		// Skip builds already being processed (MOD-6: avoid reaper competition)
		if _, loaded := r.active.LoadOrStore(build.ID, struct{}{}); loaded {
			continue
		}

		b := build // capture loop variable

		// Try to acquire semaphore slot without blocking the dispatch loop.
		// If all slots are full, skip this build — it will be retried next tick.
		select {
		case r.sem <- struct{}{}:
			go func() {
				defer func() { <-r.sem }()
				defer r.active.Delete(b.ID)
				r.reconcileBuild(ctx, &b)
			}()
		default:
			// All semaphore slots occupied — release active tracking so the
			// build can be retried on the next tick.
			r.active.Delete(b.ID)
			r.logger.Debug("reconcile loop: all slots busy, deferring build",
				"build_id", b.ID, "max_concurrent", r.cfg.MaxConcurrent)
		}
	}
}

// IsProcessing returns true if the reconcile loop is currently processing a build.
// Used by BuildReaper to avoid competing on the same build (MOD-6).
func (r *BuildReconcileLoop) IsProcessing(buildID string) bool {
	_, ok := r.active.Load(buildID)
	return ok
}

// reconcileBuild reconciles a single build against its Argo Workflow state.
func (r *BuildReconcileLoop) reconcileBuild(ctx context.Context, build *db.Build) {
	// No workflow ID means the build was never submitted to Argo
	if build.WorkflowID == "" {
		r.failBuild(ctx, build, "Build had no workflow ID (API crashed before submission)")
		return
	}

	// Check external workflow state in the build cluster
	status, err := r.workflow.GetStatus(ctx, "build-"+build.TenantID, build.WorkflowID)
	if err != nil {
		r.logger.Error("reconcile loop: failed to check workflow status",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "error", err)
		return
	}

	if status == nil {
		// Workflow not found — cleaned up by TTL or never existed
		r.failBuild(ctx, build, "Build not found (expired or never started). Redeploy to retry.")
		return
	}

	switch status.Phase {
	case "Succeeded":
		r.onWorkflowSucceeded(ctx, build)
	case "Failed", "Error":
		r.failBuild(ctx, build, fmt.Sprintf("Build %s", strings.ToLower(status.Phase)))
	case "Running", "Pending":
		// Workflow is still active — nothing to do
	default:
		r.logger.Warn("reconcile loop: unknown workflow phase",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "phase", status.Phase)
	}
}

// onWorkflowSucceeded handles a completed build workflow by creating a release
// and activating it through the standard flow (gitops push → ArgoCD sync).
func (r *BuildReconcileLoop) onWorkflowSucceeded(ctx context.Context, build *db.Build) {
	r.logger.Info("reconcile loop: workflow succeeded, processing build completion",
		"build_id", build.ID, "workflow_id", build.WorkflowID)

	metrics.BuildsTotal.WithLabelValues(build.TenantID, build.Environment, "succeeded").Inc()
	metrics.BuildDuration.WithLabelValues(build.TenantID, build.Environment, "succeeded").Observe(time.Since(build.CreatedAt).Seconds())

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

	for _, app := range apps {
		// REVIEW-MOD-4: Per-app image tag: {registry}/{tenant}/{project}/{app}:{sha}
		// Each app gets its own BuildKit build and independent image.
		imageRef := fmt.Sprintf("%s/%s/%s/%s:%s",
			r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name, build.CommitSHA)

		url := ""
		if app.Type == "server" || app.Type == "frontend" {
			url = fmt.Sprintf("https://%s-%s.%s.%s.%s",
				app.Name, build.Environment, build.ProjectID, build.TenantID, r.cfg.BaseDomain)
		}

		buildApps = append(buildApps, AppRelease{
			Name:        app.Name,
			ImageDigest: imageRef,
			URL:         url,
			Type:        app.Type,
		})

		manifestApps = append(manifestApps, manifests.AppManifestInput{
			Name:     app.Name,
			Type:     app.Type,
			Port:     app.Port,
			ImageRef: imageRef,
			Secrets:  app.Secrets,
		})
	}

	// Generate manifests in the API (C-02 fix: control plane generates manifests)
	renderedManifests := manifests.Render(manifests.RenderInput{
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		BuildID:     build.ID,
		ReleaseID:   "", // Will be set after release creation
		BaseDomain:  r.cfg.BaseDomain,
		Apps:        manifestApps,
	})

	// Construct the completion result and delegate to the orchestrator
	result := BuildCallbackRequest{
		Status:    "succeeded",
		Apps:      buildApps,
		Manifests: renderedManifests,
	}

	if err := r.orch.OnBuildCompleted(ctx, build.ID, result); err != nil {
		r.logger.Error("reconcile loop: build completion processing failed",
			"build_id", build.ID, "error", err)
	}
}

// failBuild transitions a build to FAILED inside the environment lock.
func (r *BuildReconcileLoop) failBuild(ctx context.Context, build *db.Build, reason string) {
	metrics.BuildsTotal.WithLabelValues(build.TenantID, build.Environment, "failed").Inc()
	metrics.BuildDuration.WithLabelValues(build.TenantID, build.Environment, "failed").Observe(time.Since(build.CreatedAt).Seconds())

	err := r.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock — state may have changed
		current, err := r.buildStore.GetByID(ctx, build.ID)
		if err != nil {
			return err
		}
		if statemachine.IsBuildTerminal(statemachine.State(current.Status)) {
			return nil // Already finalized
		}

		// Transition QUEUED → RUNNING first if needed (state machine requires it)
		if current.Status == string(statemachine.BuildQueued) {
			if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildStarted)); err != nil {
				return fmt.Errorf("transition to RUNNING: %w", err)
			}
		}

		// Transition RUNNING → FAILED
		if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildFailedEvent)); err != nil {
			return fmt.Errorf("transition to FAILED: %w", err)
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
	eventData, _ := json.Marshal(map[string]string{
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
}
