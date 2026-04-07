// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/pkg/naming"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// BuildCompletionHandler abstracts the methods that BuildReconcileLoop calls on
// the BuildOrchestrator. Decoupling via interface enables testing the reconcile
// loop independently and follows ISP (arch-cp-004).
type BuildCompletionHandler interface {
	// OnBuildCompleted handles a successful build workflow completion.
	OnBuildCompleted(ctx context.Context, buildID string, result BuildCallbackRequest) error

	// RetryReleaseActivation retries Phase 2 (release activation) for a RELEASING build.
	RetryReleaseActivation(ctx context.Context, build *db.Build, result BuildCallbackRequest) error

	// FindReleaseForBuild returns the most recent release for a build, or nil if none exists.
	FindReleaseForBuild(ctx context.Context, buildID string) (*db.Release, error)
}

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
// CustomDomainQuerier queries active custom domains for manifest rendering.
// MEET-2026-04-06-034 Block 0: both deploy and domain provisioner must include custom domains.
type CustomDomainQuerier interface {
	ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]routes.CustomDomain, error)
}

type BuildReconcileLoop struct {
	buildStore   db.BuildStore
	workflow     WorkflowSubmitter
	orch         BuildCompletionHandler
	locker       EnvironmentLocker
	domainStore  CustomDomainQuerier // MEET-034: custom domains in manifests
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

	// BRC-003 fix: tracks activation attempt counts per build ID.
	// After maxActivationAttempts consecutive failures, the build is failed
	// immediately instead of waiting for the 10-minute reaper timeout.
	activationAttempts sync.Map // map[string]int

	// F-01 fix: tracks OnBuildCompleted failure count per build ID.
	// After maxCompletionAttempts failures, the build is failed to avoid
	// permanent RUNNING state when completion processing is broken.
	completionAttempts sync.Map // map[string]int
}

// NewBuildReconcileLoop creates a new BuildReconcileLoop.
func NewBuildReconcileLoop(
	buildStore db.BuildStore,
	workflow WorkflowSubmitter,
	orch BuildCompletionHandler,
	locker EnvironmentLocker,
	domainStore CustomDomainQuerier,
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
		buildStore:  buildStore,
		workflow:    workflow,
		orch:        orch,
		locker:      locker,
		domainStore: domainStore,
		eventStore:  eventStore,
		logger:      logger,
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

	// MEET-2026-03-31-023 T1-8: Record successful tick timestamp.
	metrics.ReconcileLoopLastSuccess.WithLabelValues("build").SetToCurrentTime()

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
				// BRC-004 fix: recover from panics in reconcileBuild. The call chain
				// reaches deep into BuildOrchestrator → DeliveryActivator → ActivateReleaseKernel.
				// State machine panics on invalid transitions (by design). Without recovery,
				// a single corrupted build record crashes the entire API process.
				defer func() {
					if p := recover(); p != nil {
						r.logger.Error("reconcile loop: panic in reconcileBuild — recovered",
							"build_id", b.ID, "panic", fmt.Sprintf("%v", p),
							"stack", string(debug.Stack()))
					}
				}()
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
//
// MEET-2026-03-26-008: Also handles builds in RELEASING state. These are builds
// where Phase 1 (image build) completed but Phase 2 (release activation) was
// interrupted (pod crash, rollout, Gitea/ArgoCD failure). The reconcile loop
// retries Phase 2 automatically.
func (r *BuildReconcileLoop) reconcileBuild(ctx context.Context, build *db.Build) {
	// RELEASING builds: Phase 1 done, Phase 2 needs retry (MEET-2026-03-26-008)
	if build.Status == string(statemachine.BuildReleasing) {
		r.retryReleaseActivation(ctx, build)
		return
	}

	// No workflow ID means the build was never submitted to Argo — OR the
	// SubmitBuild is still in progress on another pod (race condition between
	// lock release and SetWorkflowID). Grace period: skip builds created < 60s ago.
	// After 60s, the build is legitimately orphaned and should be failed.
	if build.WorkflowID == "" {
		age := time.Since(build.CreatedAt)
		if age < 60*time.Second {
			r.logger.Debug("reconcile loop: build has no workflow ID yet, skipping (created recently)",
				"build_id", build.ID, "age", age.String())
			return
		}
		r.failBuild(ctx, build, "Build had no workflow ID (API crashed before submission)")
		return
	}

	// Check external workflow state in the build cluster
	status, err := r.workflow.GetStatus(ctx, "argo", build.WorkflowID)
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

	for _, app := range apps {
		// REVIEW-MOD-4: Per-app image tag: {registry}/{tenant}/{project}/{app}:{sha}
		// Each app gets its own BuildKit build and independent image.
		// ARCH-001 fix: single source of truth for image tag format
		imageRef := naming.ImageTag(r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name, build.CommitSHA)

		url := ""
		if app.Type == "server" || app.Type == "frontend" {
			url = fmt.Sprintf("https://%s-%s-%s-%s.%s",
				build.TenantID, build.ProjectID, app.Name, build.Environment, r.cfg.BaseDomain)
		}

		buildApps = append(buildApps, AppRelease{
			Name:        app.Name,
			ImageDigest: imageRef,
			URL:         url,
			Type:        app.Type,
			Rendering:   app.Rendering,
		})

		// MEET-2026-04-06-034 Block 0: include custom domains in manifest rendering.
		// Without this, every deploy silently removes custom domain IngressRoutes.
		var customDomains []manifests.CustomDomainInput
		// CODE-002 fix: ListActiveByApp errors are fatal — continuing without
		// custom domains would silently erase IngressRoutes from the cluster.
		if r.domainStore != nil {
			domains, err := r.domainStore.ListActiveByApp(ctx, build.TenantID, build.ProjectID, app.Name, build.Environment)
			if err != nil {
				r.logger.Error("reconcile loop: custom domain query failed — aborting to prevent IngressRoute loss",
					"app", app.Name, "error", err)
				r.failBuild(ctx, build, "Failed to read custom domain configuration")
				return
			}
			for _, d := range domains {
				customDomains = append(customDomains, manifests.CustomDomainInput{Domain: d.Domain, ID: d.ID})
			}
		}

		manifestApps = append(manifestApps, manifests.AppManifestInput{
			Name:          app.Name,
			Type:          app.Type,
			Rendering:     app.Rendering,
			Port:          app.Port,
			ImageRef:      imageRef,
			Secrets:       app.Secrets,
			CustomDomains: customDomains,
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

// retryReleaseActivation handles builds in RELEASING state (MEET-2026-03-26-008).
// These are builds where Phase 1 (image build) completed but Phase 2 (release
// activation) was interrupted. The method reconstructs the BuildCallbackRequest
// from stored data and delegates to the orchestrator.
//
// Guard: if a release already exists for this build, do NOT create another one.
// Check the existing release's state and decide whether to retry or fail.
const maxActivationAttempts = 3

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
	var manifestApps []manifests.AppManifestInput

	for _, app := range apps {
		// ARCH-001 fix: single source of truth for image tag format
		imageRef := naming.ImageTag(r.cfg.RegistryURL, build.TenantID, build.ProjectID, app.Name, build.CommitSHA)

		url := ""
		if app.Type == "server" || app.Type == "frontend" {
			url = fmt.Sprintf("https://%s-%s-%s-%s.%s",
				build.TenantID, build.ProjectID, app.Name, build.Environment, r.cfg.BaseDomain)
		}

		buildApps = append(buildApps, AppRelease{
			Name:        app.Name,
			ImageDigest: imageRef,
			URL:         url,
			Type:        app.Type,
			Rendering:   app.Rendering,
		})
		// MEET-2026-04-06-034 Block 0: include custom domains (same as onWorkflowSucceeded path)
		var customDomains []manifests.CustomDomainInput
		if r.domainStore != nil {
			domains, err := r.domainStore.ListActiveByApp(ctx, build.TenantID, build.ProjectID, app.Name, build.Environment)
			if err != nil {
				r.logger.Warn("reconcile loop: failed to query custom domains for retry, continuing without",
					"app", app.Name, "error", err)
			} else {
				for _, d := range domains {
					customDomains = append(customDomains, manifests.CustomDomainInput{Domain: d.Domain, ID: d.ID})
				}
			}
		}

		manifestApps = append(manifestApps, manifests.AppManifestInput{
			Name:          app.Name,
			Type:          app.Type,
			Rendering:     app.Rendering,
			Port:          app.Port,
			ImageRef:      imageRef,
			Secrets:       app.Secrets,
			CustomDomains: customDomains,
		})
	}

	renderedManifests := manifests.Render(manifests.RenderInput{
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		BuildID:     build.ID,
		ReleaseID:   "",
		BaseDomain:  r.cfg.BaseDomain,
		Apps:        manifestApps,
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
