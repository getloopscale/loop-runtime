// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
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
	Cancel(ctx context.Context, workflowID string) error
	GetStatus(ctx context.Context, workflowID string) (*WorkflowStatus, error)
}

// WorkflowSpec contains everything needed to submit a build workflow.
type WorkflowSpec struct {
	BuildID       string
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	Apps          []AppSpec
	CorrelationID string
}

// AppSpec describes a single app from loopzero.yaml.
type AppSpec struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`                        // "server", "worker", "frontend"
	Port      int      `json:"port"`
	Path      string   `json:"path,omitempty"`
	Framework string   `json:"framework,omitempty"`          // "nextjs", "express", "gin", "fastapi", "custom", etc.
	Secrets   []string `json:"secrets,omitempty"`            // declared secret names from loopzero.yaml
}

// GitOpsPusher abstracts the commit-and-push of manifests to the tenant GitOps
// repo (Gitea). Step 6 architectural fix: the build pipeline no longer touches
// GitOps — the API owns the entire release flow after a build succeeds.
type GitOpsPusher interface {
	// Push commits manifests to the tenant GitOps repo and returns the git commit SHA.
	// Path inside repo: {projectID}/{environment}/manifests.yaml
	Push(ctx context.Context, opts GitOpsPushOpts) (commitSHA string, err error)
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

// AppRelease describes a built app with its image digest and URL.
type AppRelease struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"`
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"` // "server", "worker", "frontend"
	DurationMs  int    `json:"duration_ms"`
}

// SyncClient abstracts ArgoCD sync operations (DIP).
type SyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
	WaitForSync(ctx context.Context, appName string, timeout time.Duration) error
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
	ArgocdSyncTimeout  time.Duration // 0 = default 120s
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
	argocdSyncTimeout time.Duration
}

// defaultArgocdSyncTimeout is used when no timeout is configured.
const defaultArgocdSyncTimeout = 120 * time.Second

// NewBuildOrchestrator creates a new BuildOrchestrator with all dependencies injected.
func NewBuildOrchestrator(deps BuildOrchestratorDeps) *BuildOrchestrator {
	syncTimeout := deps.ArgocdSyncTimeout
	if syncTimeout == 0 {
		syncTimeout = defaultArgocdSyncTimeout
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
		argocdSyncTimeout: syncTimeout,
	}
}

// SubmitBuild acquires the environment lock (Lock 1) and submits a new build.
//
// Steps (spec §10.1, steps 4-11):
//  1. Acquire environment lock
//  2. Check concurrent build (HasActiveBuild) — INSIDE the lock
//  3. Create Build record (QUEUED)
//  4. Submit Argo Workflow (via WorkflowSubmitter)
//  5. Transition Build to RUNNING
//  6. Emit SSE event (build.started)
//  7. Release lock (transaction commits)
//
// Returns ErrDeployInProgress if another build is active.
// Returns ErrLockTimeout if the lock cannot be acquired within 5s.
func (o *BuildOrchestrator) SubmitBuild(ctx context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error) {
	var resp *SubmitBuildResponse

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

		// Step 4: submit workflow
		wfSpec := WorkflowSpec{
			BuildID:       build.ID,
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			Environment:   req.Environment,
			CommitSHA:     req.CommitSHA,
			Apps:          req.Apps,
			CorrelationID: req.CorrelationID,
		}
		workflowID, err := o.workflow.Submit(ctx, wfSpec)
		if err != nil {
			return fmt.Errorf("submit build: submit workflow: %w", err)
		}

		// Persist workflow ID for reconcile loop and cancel (BLOCKER-1 fix)
		if err := o.buildStore.SetWorkflowID(ctx, tx, build.ID, workflowID); err != nil {
			return fmt.Errorf("submit build: set workflow id: %w", err)
		}

		// C-01 fix: Persist app specs so the reconcile loop can generate
		// manifests and compute image references when the workflow completes.
		var appsSpec []db.BuildAppSpec
		for _, a := range req.Apps {
			appsSpec = append(appsSpec, db.BuildAppSpec{
				Name:    a.Name,
				Type:    a.Type,
				Port:    a.Port,
				Secrets: a.Secrets,
			})
		}
		if err := o.buildStore.SetAppsSpec(ctx, tx, build.ID, appsSpec); err != nil {
			return fmt.Errorf("submit build: set apps spec: %w", err)
		}

		// Step 5: transition QUEUED → RUNNING
		if _, err := o.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildStarted)); err != nil {
			return fmt.Errorf("submit build: transition to RUNNING: %w", err)
		}

		// Step 6: emit SSE build.started — include app names for CLI per-app rendering (F-01)
		appNames := make([]string, len(req.Apps))
		for i, a := range req.Apps {
			appNames[i] = a.Name
		}
		eventData, _ := json.Marshal(map[string]interface{}{
			"build_id": build.ID,
			"status":   "running",
			"apps":     appNames,
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "build.started", eventData); err != nil {
			return fmt.Errorf("submit build: emit SSE event: %w", err)
		}

		// Build response
		streamURL := fmt.Sprintf("/api/v1/builds/%s/stream", build.ID)
		resp = &SubmitBuildResponse{
			BuildID:   build.ID,
			StreamURL: streamURL,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return resp, nil
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
			if cancelErr := o.workflow.Cancel(ctx, build.WorkflowID); cancelErr != nil {
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
	if statemachine.IsBuildTerminal(statemachine.State(build.Status)) && build.Status != string(statemachine.BuildSucceeded) {
		return nil
	}

	// Phase 2: Release activation (create release, gitops push, sync, health probe).
	// BLOCKER-2 fix: each sub-phase acquires/releases its own lock. External I/O
	// (gitops push, ArgoCD sync, health probes) runs OUTSIDE locks.
	activationErr := o.activateRelease(ctx, build, result)

	if activationErr != nil {
		// Activation failed — emit deploy failure event so the CLI knows.
		// The build stays SUCCEEDED (Phase 1 already committed), which is correct:
		// the code was built successfully, the deployment just didn't complete.
		o.emitActivationFailure(ctx, buildID, activationErr)
	}

	return nil
}
