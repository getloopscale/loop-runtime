// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// WorkflowStatus represents the external state of a build workflow.
type WorkflowStatus struct {
	Phase string // "Succeeded", "Failed", "Running", "Pending", ""
}

// WorkflowSubmitter abstracts Argo Workflows submission (DIP — no direct coupling).
type WorkflowSubmitter interface {
	Submit(ctx context.Context, spec WorkflowSpec) (workflowID string, err error)
	Cancel(ctx context.Context, namespace, workflowID string) error
	GetStatus(ctx context.Context, namespace, workflowID string) (*WorkflowStatus, error)
}

// StaticRoutingClient writes routing entries for static frontends (CF Workers KV).
// The key is the flat subdomain (e.g., "tn-loopzero-e2e-project-web-production"),
// the value is a JSON object with buildId and s3Prefix.
// Nil is valid — it means CF KV is not configured (local dev).
type StaticRoutingClient interface {
	Put(ctx context.Context, subdomain string, entry interface{}) error
}

// WorkflowSpec contains everything needed to submit a build workflow.
type WorkflowSpec struct {
	BuildID       string
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	NoCache       bool // Skip BuildKit layer cache (full rebuild via --no-cache)
	Apps          []AppSpec
	Plan          *BuildPlan // v2: resolved build plan (when present, WorkflowBuilder uses this instead of Apps)
	CorrelationID string
	Namespace     string // Target namespace for the workflow (e.g., "build-acme")
}

// AppSpec describes a single app from loopzero.yaml as received from the CLI.
// This is the raw input — use NormalizeApp() to resolve into a fully populated AppUnit.
type AppSpec struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`                        // "server", "worker", "frontend"
	Port       int               `json:"port"`
	Path       string            `json:"path,omitempty"`
	Framework  string            `json:"framework,omitempty"`          // "nextjs", "express", "gin", "fastapi", "custom", etc.
	Build      string            `json:"build,omitempty"`              // v2: "dockerfile", "buildpack", "custom"
	Dockerfile string            `json:"dockerfile,omitempty"`         // v2: Dockerfile path relative to app path
	Rendering  string            `json:"rendering,omitempty"`          // v2: "static" or "server" (MEET-009)
	Env        map[string]string `json:"env,omitempty"`                // v2: build-time environment variables
	Secrets    []string          `json:"secrets,omitempty"`            // declared secret names from loopzero.yaml
}

// GitOpsPusher abstracts the commit-and-push of manifests to the tenant GitOps
// repo (Gitea). Step 6 architectural fix: the build pipeline no longer touches
// GitOps — the API owns the entire release flow after a build succeeds.
type GitOpsPusher interface {
	// Push commits manifests to the tenant GitOps repo and returns the git commit SHA.
	// Path inside repo: {projectID}/{environment}/manifests.yaml
	Push(ctx context.Context, opts GitOpsPushOpts) (commitSHA string, err error)

	// Remove deletes a project's manifests from the tenant GitOps repo.
	// The operation is idempotent: if the directory doesn't exist, returns success.
	// ArgoCD with prune: true will automatically remove the K8s resources.
	Remove(ctx context.Context, opts GitOpsRemoveOpts) (commitSHA string, err error)
}

// GitOpsPushOpts contains everything needed to push manifests to the GitOps repo.
type GitOpsPushOpts struct {
	TenantID      string
	ProjectID     string
	Environment   string
	Manifests     string // raw YAML content
	BuildID       string
	ReleaseID     string
	CorrelationID string
}

// GitOpsRemoveOpts contains everything needed to remove a project from the GitOps repo.
type GitOpsRemoveOpts struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CorrelationID string
}

// AppRelease describes a built app with its image digest and URL.
type AppRelease struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"`
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"`      // "server", "worker", "frontend"
	Rendering   string `json:"rendering,omitempty"` // "static" or "server" (Sprint 10)
	DurationMs  int    `json:"duration_ms"`
}

// SyncClient abstracts ArgoCD sync operations (DIP).
type SyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
	// WaitForSync polls until the ArgoCD Application is synced and the specified
	// deployedApps are healthy. If deployedApps is empty, checks global Application health.
	WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error
}

// EnvironmentLocker abstracts the advisory lock acquisition (DIP for testability).
// In production, this wraps lock.WithEnvironmentLock with a real pgxpool.Pool.
// In tests, a fake locker executes fn immediately with a nil tx.
type EnvironmentLocker interface {
	WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error
}

// SubmitBuildRequest contains the inputs for submitting a new build.
type SubmitBuildRequest struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	NoCache       bool
	Apps          []AppSpec
	CreatedBy     string
	CorrelationID string
}

// SubmitBuildResponse is returned after a build is successfully submitted.
type SubmitBuildResponse struct {
	BuildID   string `json:"build_id"`
	StreamURL string `json:"stream_url"`
}

// BuildCallbackRequest is sent by the build system when a build completes.
type BuildCallbackRequest struct {
	Status    string       `json:"status"` // "succeeded" or "failed"
	Error     string       `json:"error,omitempty"`
	Apps      []AppRelease `json:"apps"`
	Manifests string       `json:"manifests,omitempty"` // raw K8s YAML from generate-manifests step
}

// BuildOrchestratorDeps holds all dependencies for BuildOrchestrator.
type BuildOrchestratorDeps struct {
	BuildStore     db.BuildStore
	ReleaseStore   db.ReleaseStore
	EnvTargetStore db.EnvironmentTargetStore
	EventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Workflow           WorkflowSubmitter
	Sync               SyncClient
	Locker             EnvironmentLocker
	GitOps             GitOpsPusher
	Delivery           DeliveryActivator // MEET-009: polymorphic release activation
	RegistryURL        string        // Container registry URL for BuildPlan image tag resolution
	ArgocdSyncTimeout  time.Duration // 0 = default 120s
	KVClient           StaticRoutingClient // CF KV client for static frontend routing (nil = disabled)
	BaseDomain         string              // Base domain for app hostnames
}

// BuildOrchestrator orchestrates the entire deploy flow:
// build submission, completion callback, release creation, and activation.
//
// Build lifecycle methods (SubmitBuild, CancelBuild, OnBuildCompleted) live here.
// Release activation methods live in release_activator.go.
type BuildOrchestrator struct {
	buildStore     db.BuildStore
	releaseStore   db.ReleaseStore
	envTargetStore db.EnvironmentTargetStore
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	workflow          WorkflowSubmitter
	sync              SyncClient
	locker            EnvironmentLocker
	gitops            GitOpsPusher
	delivery          DeliveryActivator // MEET-009: polymorphic release activation
	registryURL       string // Container registry URL for BuildPlan image tag resolution
	argocdSyncTimeout time.Duration
	kvClient          StaticRoutingClient // CF KV client for static frontend routing
	baseDomain        string
}

// defaultArgocdSyncTimeout is used when no timeout is configured.
// 600s accommodates monorepo deploys with multiple apps (3+ Deployments rolling
// out simultaneously, each with image pull + rollout + cert issuance + health probes).
// At 300s, 3-app monorepos consistently hit SYNC_FAILED during ArgoCD WaitForSync.
const defaultArgocdSyncTimeout = 600 * time.Second

// NewBuildOrchestrator creates a new BuildOrchestrator with all dependencies injected.
func NewBuildOrchestrator(deps BuildOrchestratorDeps) *BuildOrchestrator {
	syncTimeout := deps.ArgocdSyncTimeout
	if syncTimeout == 0 {
		syncTimeout = defaultArgocdSyncTimeout
	}
	registryURL := deps.RegistryURL
	if registryURL == "" {
		registryURL = "k3d-loopzero-registry:5000" // local dev default
	}
	// MEET-009: If no DeliveryActivator provided, create default ContainerDeliveryActivator.
	// This ensures backward compatibility — existing call sites don't need to change.
	delivery := deps.Delivery
	if delivery == nil {
		delivery = NewContainerDeliveryActivator(ContainerDeliveryDeps{
			ReleaseStore:      deps.ReleaseStore,
			EnvTargetStore:    deps.EnvTargetStore,
			EventStore:        deps.EventStore,
			Sync:              deps.Sync,
			Locker:            deps.Locker,
			GitOps:            deps.GitOps,
			ArgocdSyncTimeout: syncTimeout,
		})
	}

	return &BuildOrchestrator{
		buildStore:        deps.BuildStore,
		releaseStore:      deps.ReleaseStore,
		envTargetStore:    deps.EnvTargetStore,
		eventStore:        deps.EventStore,
		workflow:          deps.Workflow,
		sync:              deps.Sync,
		locker:            deps.Locker,
		gitops:            deps.GitOps,
		delivery:          delivery,
		registryURL:       registryURL,
		argocdSyncTimeout: syncTimeout,
		kvClient:          deps.KVClient,
		baseDomain:        deps.BaseDomain,
	}
}

// SubmitBuild acquires the environment lock to reserve a build slot, then
// submits the workflow OUTSIDE the lock to avoid holding it during slow
// Argo API calls (REVIEW-BLOCKER-1 fix).
//
// Steps (spec §10.1, steps 4-11):
//  1. Acquire environment lock
//  2. Check concurrent build (HasActiveBuild) — INSIDE the lock
//  3. Create Build record (QUEUED) + persist apps spec — INSIDE the lock
//  4. Release lock (transaction commits) — build exists as QUEUED
//  5. Submit Argo Workflow (OUTSIDE the lock)
//  6. Persist workflow ID, transition QUEUED → RUNNING, emit SSE
//
// If workflow submission fails (step 5), the build is immediately transitioned
// to FAILED so the environment is unblocked. The build reaper also covers this
// case (5min QUEUED timeout) as a safety net.
//
// Returns ErrDeployInProgress if another build is active.
// Returns ErrLockTimeout if the lock cannot be acquired within 5s.
func (o *BuildOrchestrator) SubmitBuild(ctx context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error) {
	// Phase 1 (INSIDE lock): reserve build slot as QUEUED
	var buildID string

	err := o.locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		// Step 2: concurrent build check INSIDE the lock (Rule 6)
		hasActive, err := o.buildStore.HasActiveBuild(ctx, tx, req.TenantID, req.ProjectID, req.Environment)
		if err != nil {
			return fmt.Errorf("submit build: check active builds: %w", err)
		}
		if hasActive {
			return loopzeroerrors.ErrDeployInProgress
		}

		// Step 3: create Build (QUEUED)
		build := &db.Build{
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			Environment:   req.Environment,
			CommitSHA:     req.CommitSHA,
			CreatedBy:     req.CreatedBy,
			CorrelationID: req.CorrelationID,
		}
		if err := o.buildStore.Create(ctx, tx, build); err != nil {
			return fmt.Errorf("submit build: create build: %w", err)
		}
		buildID = build.ID

		// C-01 fix: Persist app specs so the reconcile loop can generate
		// manifests and compute image references when the workflow completes.
		var appsSpec []db.BuildAppSpec
		for _, a := range req.Apps {
			appsSpec = append(appsSpec, db.BuildAppSpec{
				Name:      a.Name,
				Type:      a.Type,
				Rendering: a.Rendering,
				Port:      a.Port,
				Secrets:   a.Secrets,
			})
		}
		if err := o.buildStore.SetAppsSpec(ctx, tx, build.ID, appsSpec); err != nil {
			return fmt.Errorf("submit build: set apps spec: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Phase 2 (OUTSIDE lock): submit workflow and activate build.
	// Lock is released — Argo API latency no longer blocks other deploys.
	// If anything below fails, the build stays QUEUED briefly, then we
	// immediately transition it to FAILED so the env is unblocked.

	// Normalize AppSpecs into AppUnits and create a BuildPlan.
	// The BuildPlan resolves all paths, image tags, and strategies before
	// reaching the WorkflowBuilder — no inference happens in the pipeline.
	var units []AppUnit
	for _, a := range req.Apps {
		unit, normErr := NormalizeApp(a.Name, a)
		if normErr != nil {
			return nil, fmt.Errorf("normalize app %q: %w", a.Name, normErr)
		}
		units = append(units, unit)
	}
	plan := CreateBuildPlan(units, req.TenantID, req.ProjectID, req.CommitSHA, o.registryURL)

	wfSpec := WorkflowSpec{
		BuildID:       buildID,
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		NoCache:       req.NoCache,
		Apps:          req.Apps,
		Plan:          &plan,
		CorrelationID: req.CorrelationID,
		Namespace:     "build-" + req.TenantID,
	}
	workflowID, submitErr := o.workflow.Submit(ctx, wfSpec)
	if submitErr != nil {
		// Workflow submit failed — immediately fail the build so the environment
		// is unblocked. The build reaper is a safety net (5min QUEUED timeout)
		// but immediate failure gives better UX.
		o.failQueuedBuild(ctx, buildID, fmt.Sprintf("Workflow submission failed: %v", submitErr))
		return nil, fmt.Errorf("submit build: submit workflow: %w", submitErr)
	}

	// Persist workflow ID (outside lock, tx=nil → uses pool directly)
	if err := o.buildStore.SetWorkflowID(ctx, nil, buildID, workflowID); err != nil {
		slog.Error("submit build: failed to set workflow ID after submit",
			"build_id", buildID, "workflow_id", workflowID, "error", err)
		// Non-fatal: reconcile loop will find the workflow by label matching.
		// The build will still work, just cancel won't correlate by workflow ID.
	}

	// Transition QUEUED → RUNNING
	if _, err := o.buildStore.UpdateStatus(ctx, nil, buildID, string(statemachine.BuildStarted)); err != nil {
		slog.Error("submit build: failed to transition to RUNNING",
			"build_id", buildID, "error", err)
		// Non-fatal: reconcile loop handles EC-N (QUEUED build with completed workflow).
	}

	// Emit SSE build.started — include app names for CLI per-app rendering (F-01)
	appNames := make([]string, len(req.Apps))
	for i, a := range req.Apps {
		appNames[i] = a.Name
	}
	eventData, _ := json.Marshal(map[string]interface{}{
		"build_id": buildID,
		"status":   "running",
		"apps":     appNames,
	})
	if _, err := o.eventStore.Append(ctx, buildID, "build.started", eventData); err != nil {
		slog.Error("submit build: failed to emit SSE event",
			"build_id", buildID, "error", err)
	}

	metrics.DeploysTotal.WithLabelValues(req.TenantID, req.Environment, "submitted").Inc()
	metrics.BuildQueueDepth.Inc()

	streamURL := fmt.Sprintf("/api/v1/builds/%s/stream", buildID)
	return &SubmitBuildResponse{
		BuildID:   buildID,
		StreamURL: streamURL,
	}, nil
}

// failQueuedBuild transitions a QUEUED build to FAILED when workflow submission
// fails. This runs OUTSIDE the advisory lock using the connection pool directly.
// REVIEW-BLOCKER-1: ensures the environment is immediately unblocked on submit failure.
func (o *BuildOrchestrator) failQueuedBuild(ctx context.Context, buildID string, reason string) {
	if _, err := o.buildStore.UpdateStatus(ctx, nil, buildID, string(statemachine.BuildFailedEvent)); err != nil {
		slog.Error("submit build: failed to mark QUEUED build as FAILED",
			"build_id", buildID, "error", err)
		return
	}
	if err := o.buildStore.SetErrorMessage(ctx, nil, buildID, reason); err != nil {
		slog.Error("submit build: failed to set error message on failed build",
			"build_id", buildID, "error", err)
	}

	// Emit SSE build.failed so CLI gets notified
	eventData, _ := json.Marshal(map[string]string{
		"build_id": buildID,
		"status":   "failed",
		"error":    reason,
	})
	if _, err := o.eventStore.Append(ctx, buildID, "build.failed", eventData); err != nil {
		slog.Error("submit build: failed to emit build.failed SSE event",
			"build_id", buildID, "error", err)
	}
}

// RetryReleaseActivation retries Phase 2 (release activation) for a build stuck
// in RELEASING state. Called by the reconcile loop when it detects a RELEASING
// build whose Phase 2 was interrupted (pod crash, rollout, Gitea/ArgoCD failure).
//
// MEET-2026-03-26-008: This method does NOT check IsBuildTerminal because
// RELEASING is explicitly non-terminal. It directly runs activateRelease and
// transitions to SUCCEEDED on success or leaves in RELEASING for retry.
func (o *BuildOrchestrator) RetryReleaseActivation(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
	if build.Status != string(statemachine.BuildReleasing) {
		return fmt.Errorf("retry release activation: build %s is in %s state, expected RELEASING", build.ID, build.Status)
	}

	activationErr := o.delivery.Activate(ctx, build, result)
	if activationErr != nil {
		slog.Warn("retry release activation: Phase 2 failed, will retry on next tick",
			"build_id", build.ID, "error", activationErr)
		o.emitActivationFailure(ctx, build.ID, activationErr)
		return activationErr
	}

	// Phase 2 succeeded — transition RELEASING → SUCCEEDED (inside lock to handle races)
	return o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock — another pod may have already completed this
		current, err := o.buildStore.GetByID(ctx, build.ID)
		if err != nil {
			return fmt.Errorf("retry release activation: re-fetch build: %w", err)
		}
		if statemachine.IsBuildTerminal(statemachine.State(current.Status)) {
			return nil // Already finalized by another pod — safe to skip
		}
		if _, err := o.buildStore.UpdateStatus(ctx, tx, current.ID, string(statemachine.BuildReleaseCompleted)); err != nil {
			return fmt.Errorf("retry release activation: transition to SUCCEEDED: %w", err)
		}
		return nil
	})
}

// FindReleaseForBuild returns the most recent release for a build, or nil if none exists.
// Used by the reconcile loop to determine whether Activate() has already been called.
func (o *BuildOrchestrator) FindReleaseForBuild(ctx context.Context, buildID string) (*db.Release, error) {
	return o.releaseStore.GetLatestByBuildID(ctx, buildID)
}

// CancelBuild cancels a running or queued build.
//
// All state mutations happen INSIDE the environment lock (Rule 6).
//
// Steps:
//  1. Get build by ID — verify it exists and get lock coordinates
//  2. Acquire environment lock on (tenant, project, environment)
//  3. Re-fetch build inside lock (state may have changed)
//  4. If build is terminal → return ErrBuildAlreadyCompleted
//  5. Transition to CANCELLING (if RUNNING) or CANCELLED (if QUEUED) via state machine
//  6. Cancel the Argo Workflow via WorkflowSubmitter.Cancel
//  7. If was RUNNING (now CANCELLING), confirm cancellation → CANCELLED
//  8. Emit SSE build.cancelled event
func (o *BuildOrchestrator) CancelBuild(ctx context.Context, buildID string) error {
	// Step 1: get build to determine lock coordinates (tenant, project, env)
	build, err := o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("cancel build: get build: %w", err)
	}

	// Check if already terminal before acquiring lock (fast path, no race risk)
	if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
		return loopzeroerrors.ErrBuildAlreadyCompleted
	}

	// Step 2: acquire environment lock — all state mutations inside (Rule 6)
	return o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Step 3: re-fetch inside lock (state may have changed between pre-check and lock acquisition)
		build, err = o.buildStore.GetByID(ctx, buildID)
		if err != nil {
			return fmt.Errorf("cancel build: re-fetch build: %w", err)
		}

		// Step 4: re-check terminal state inside lock
		if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			return loopzeroerrors.ErrBuildAlreadyCompleted
		}

		// Step 5: transition via state machine: BuildCancelRequested
		// QUEUED → CANCELLED (direct), RUNNING → CANCELLING
		updatedBuild, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BuildCancelRequested))
		if err != nil {
			return fmt.Errorf("cancel build: transition cancel requested: %w", err)
		}

		// Step 6: cancel the workflow (best-effort — workflow may already be done)
		if build.WorkflowID != "" {
			if cancelErr := o.workflow.Cancel(ctx, "build-"+build.TenantID, build.WorkflowID); cancelErr != nil {
				slog.Warn("workflow cancel failed (best-effort)", "workflow_id", build.WorkflowID, "error", cancelErr)
			}
		}

		// Step 7: if transitioned to CANCELLING (was RUNNING), confirm cancellation
		if statemachine.State(updatedBuild.Status) == statemachine.BuildCancelling {
			if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BuildCancelConfirmed)); err != nil {
				return fmt.Errorf("cancel build: confirm cancellation: %w", err)
			}
		}

		// Step 8: emit SSE build.cancelled
		eventData, _ := json.Marshal(map[string]string{
			"build_id": buildID,
			"status":   "cancelled",
		})
		if _, err := o.eventStore.Append(ctx, buildID, "build.cancelled", eventData); err != nil {
			return fmt.Errorf("cancel build: emit SSE event: %w", err)
		}

		return nil
	})
}

// OnBuildCompleted handles the callback from the build system when a build finishes.
//
// Two-phase design (DA-17):
//   - Phase 1 (Lock): Transition build to terminal state (SUCCEEDED/FAILED).
//     This MUST commit independently so the build never stays stuck in RUNNING.
//   - Phase 2 (Lock): Activate release (create release, sync, health probe).
//     If this fails, the build is already SUCCEEDED and a failure event is emitted.
//
// Race condition guard (DA-15 EC-N): If build is QUEUED (Argo finished before
// SubmitBuild transitioned to RUNNING), transitions QUEUED→RUNNING first.
//
// Duplicate callbacks: If build is already in a terminal state, returns nil (no-op).
func (o *BuildOrchestrator) OnBuildCompleted(ctx context.Context, buildID string, result BuildCallbackRequest) error {
	// Step 1: get build by ID (read-only, outside lock — for lock coordinates)
	build, err := o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("on build completed: get build: %w", err)
	}

	// Fast path: duplicate callback — build already terminal → no-op (no lock needed)
	if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
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
		if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			return nil
		}

		// EC-N race condition guard: build still in QUEUED (Argo finished before SubmitBuild transitioned)
		if build.Status == string(statemachine.BuildQueued) {
			if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BuildStarted)); err != nil {
				return fmt.Errorf("on build completed: EC-N transition to RUNNING: %w", err)
			}
		}

		// Transition to terminal state
		if result.Status == "failed" {
			if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BuildFailedEvent)); err != nil {
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
			eventData, _ := json.Marshal(failedPayload)
			if _, err := o.eventStore.Append(ctx, buildID, "build.failed", eventData); err != nil {
				return fmt.Errorf("on build completed: emit build.failed: %w", err)
			}

			return nil
		}

		// Transition to SUCCEEDED
		if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BuildSucceededEvent)); err != nil {
			return fmt.Errorf("on build completed: transition to SUCCEEDED: %w", err)
		}

		// Emit SSE build.succeeded
		eventData, _ := json.Marshal(map[string]string{
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

	// If build failed, no activation needed — Phase 1 already emitted build.failed
	if result.Status == "failed" {
		return nil
	}

	// Re-fetch build after Phase 1 commit to get updated state
	build, err = o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("on build completed: re-fetch after phase 1: %w", err)
	}

	// If build was cancelled concurrently during Phase 1, skip activation
	if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
		return nil
	}

	// Build should be in RELEASING state after Phase 1 (MEET-2026-03-26-008).
	// Phase 2: Release activation via DeliveryActivator (MEET-2026-03-26-009).
	// Dispatches to ContainerDeliveryActivator or StaticDeliveryActivator
	// based on build plan. Each sub-phase acquires/releases its own lock.
	activationErr := o.delivery.Activate(ctx, build, result)

	if activationErr != nil {
		// Activation failed — emit deploy failure event so the CLI knows.
		// Build stays in RELEASING so the reconcile loop retries (MEET-2026-03-26-008).
		o.emitActivationFailure(ctx, buildID, activationErr)
		return nil
	}

	// Write CF KV routing entries for static apps (Sprint 10).
	// This happens after delivery activation so the release is already ACTIVE.
	// KV write failure is non-fatal — the nginx-s3-gateway fallback in the cluster
	// still serves assets via IngressRoute. The KV write is an optimization.
	o.writeStaticRoutingEntries(ctx, build, result)

	// Phase 2 succeeded — transition RELEASING → SUCCEEDED (deploy complete).
	if err := o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock — another pod's reconcile loop may have already transitioned
		current, err := o.buildStore.GetByID(ctx, buildID)
		if err != nil {
			return fmt.Errorf("on build completed: re-fetch for SUCCEEDED transition: %w", err)
		}
		if statemachine.IsBuildTerminal(statemachine.State(current.Status)) {
			return nil // Already finalized (race with reconcile loop) — safe
		}
		if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BuildReleaseCompleted)); err != nil {
			return fmt.Errorf("on build completed: transition to SUCCEEDED: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("on build completed: failed to transition RELEASING → SUCCEEDED",
			"build_id", buildID, "error", err)
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

		subdomain := fmt.Sprintf("%s-%s-%s-%s", build.TenantID, build.ProjectID, app.Name, build.Environment)
		if len(subdomain) > 63 {
			subdomain = subdomain[:63]
		}

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
