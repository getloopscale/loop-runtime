// Package services implements business logic orchestration.
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// WorkflowSubmitter abstracts Argo Workflows submission (DIP — no direct coupling).
type WorkflowSubmitter interface {
	Submit(ctx context.Context, spec WorkflowSpec) (workflowID string, err error)
	Cancel(ctx context.Context, workflowID string) error
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
	Name string `json:"name"`
	Type string `json:"type"` // "server", "worker", "frontend"
	Port int    `json:"port"`
	Path string `json:"path,omitempty"`
}

// NOTE: GitOpsPusher was REMOVED from the API. The Argo Workflow's gitops-push
// step handles manifest generation and commit to Gitea (spec §10.1, step 13i).
// The API only transitions the release to PENDING after receiving the callback.

// AppRelease describes a built app with its image digest and URL.
type AppRelease struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"`
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"` // "server", "worker", "frontend"
	DurationMs  int    `json:"duration_ms"`
}

// HealthProber checks if a deployed app is accessible (DIP).
type HealthProber interface {
	Probe(ctx context.Context, url string, path string) error
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
	Status string       `json:"status"` // "succeeded" or "failed"
	Error  string       `json:"error,omitempty"`
	Apps   []AppRelease `json:"apps"`
}

// BuildOrchestratorDeps holds all dependencies for BuildOrchestrator.
type BuildOrchestratorDeps struct {
	BuildStore     db.BuildStore
	ReleaseStore   db.ReleaseStore
	EnvTargetStore db.EnvironmentTargetStore
	EventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Workflow WorkflowSubmitter
	Health   HealthProber
	Sync     SyncClient
	Locker   EnvironmentLocker
}

// BuildOrchestrator orchestrates the entire deploy flow:
// build submission, completion callback, release creation, and activation.
type BuildOrchestrator struct {
	buildStore     db.BuildStore
	releaseStore   db.ReleaseStore
	envTargetStore db.EnvironmentTargetStore
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	workflow WorkflowSubmitter
	health   HealthProber
	sync     SyncClient
	locker   EnvironmentLocker
}

// NewBuildOrchestrator creates a new BuildOrchestrator with all dependencies injected.
func NewBuildOrchestrator(deps BuildOrchestratorDeps) *BuildOrchestrator {
	return &BuildOrchestrator{
		buildStore:     deps.BuildStore,
		releaseStore:   deps.ReleaseStore,
		envTargetStore: deps.EnvTargetStore,
		eventStore:     deps.EventStore,
		workflow:       deps.Workflow,
		health:         deps.Health,
		sync:           deps.Sync,
		locker:         deps.Locker,
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
		if _, err := o.workflow.Submit(ctx, wfSpec); err != nil {
			return fmt.Errorf("submit build: submit workflow: %w", err)
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

	// Phase 2 (Lock 2): Release activation (create release, sync, health probe).
	// If this fails, we handle the error gracefully — the build is already SUCCEEDED.
	activationErr := o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		return o.activateRelease(ctx, tx, build, result)
	})

	if activationErr != nil {
		// Activation failed — emit deploy failure event so the CLI knows.
		// The build stays SUCCEEDED (Phase 1 already committed), which is correct:
		// the code was built successfully, the deployment just didn't complete.
		o.emitActivationFailure(ctx, buildID, activationErr)
	}

	return nil
}

// emitActivationFailure emits a deploy.sync_failed SSE event when release activation
// fails after a successful build. This ensures the CLI gets notified instead of
// waiting for a timeout.
func (o *BuildOrchestrator) emitActivationFailure(ctx context.Context, buildID string, activationErr error) {
	failedData, _ := json.Marshal(map[string]interface{}{
		"status": "sync_failed",
		"reason": "ACTIVATION_FAILED",
		"error":  activationErr.Error(),
	})
	if _, err := o.eventStore.Append(ctx, buildID, "deploy.sync_failed", failedData); err != nil {
		slog.Error("failed to emit activation failure event", "build_id", buildID, "error", err)
	}
}

// activateRelease runs inside Lock 2 and orchestrates release creation through activation.
//
// Steps (spec §10.1, steps 8a-8i):
//  1. Create release record (READY)
//  2. Push manifests to gitops store
//  3. Wait for sync and run health probes
//  4. Finalize activation or mark as failed
func (o *BuildOrchestrator) activateRelease(ctx context.Context, tx db.DBTX, build *db.Build, result BuildCallbackRequest) error {
	release, err := o.createRelease(ctx, tx, build, result)
	if err != nil {
		return err
	}

	if err := o.pushToGitOps(ctx, tx, build, release, result); err != nil {
		return err
	}

	probeFailed, err := o.waitForSync(ctx, tx, build, release, result)
	if err != nil {
		return err
	}

	return o.finalizeActivation(ctx, tx, build, release, result, probeFailed)
}

// createRelease creates a new release record in READY state (step 8a).
func (o *BuildOrchestrator) createRelease(ctx context.Context, tx db.DBTX, build *db.Build, result BuildCallbackRequest) (*db.Release, error) {
	appsJSON, err := json.Marshal(result.Apps)
	if err != nil {
		return nil, fmt.Errorf("activate release: marshal apps: %w", err)
	}

	release := &db.Release{
		BuildID:     build.ID,
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CreatedBy:   build.CreatedBy,
		Apps:        appsJSON,
	}
	if err := o.releaseStore.Create(ctx, tx, release); err != nil {
		return nil, fmt.Errorf("activate release: create release: %w", err)
	}

	return release, nil
}

// transitionToPending transitions the release to PENDING state and updates
// environment_targets. The gitops-push was already done by the Argo Workflow
// before calling the callback (spec §10.1, step 13i).
func (o *BuildOrchestrator) pushToGitOps(ctx context.Context, tx db.DBTX, build *db.Build, release *db.Release, result BuildCallbackRequest) error {
	// Gitops-push already completed by the workflow (step 13i).
	// Transition READY → PENDING via GitMirrorCompleted.
	if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorCompleted)); err != nil {
		return fmt.Errorf("activate release: transition to PENDING: %w", err)
	}

	// Emit SSE deploy.pending (user-facing: no "release" terminology per Rule 3)
	pendingData, _ := json.Marshal(map[string]string{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "pending",
	})
	if _, err := o.eventStore.Append(ctx, build.ID, "deploy.pending", pendingData); err != nil {
		return fmt.Errorf("activate release: emit deploy.pending: %w", err)
	}

	// Step 8d: update environment_targets.desired_release_id
	if err := o.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
		return fmt.Errorf("activate release: set desired: %w", err)
	}

	return nil
}

// waitForSync triggers ArgoCD sync and runs health probes against deployed apps
// (steps 8e-8f). Returns true if any health probe failed.
func (o *BuildOrchestrator) waitForSync(ctx context.Context, tx db.DBTX, build *db.Build, release *db.Release, result BuildCallbackRequest) (probeFailed bool, err error) {
	// Step 8e: trigger sync
	// ArgoCD Application name matches TenantController naming: {tenant}-{env}
	argoAppName := fmt.Sprintf("%s-%s", build.TenantID, build.Environment)
	if err := o.sync.TriggerSync(ctx, argoAppName); err != nil {
		// Any sync failure (circuit breaker, ArgoCD 400, network error) is treated
		// as a probe failure — the release will be marked SYNC_FAILED by finalizeActivation.
		// We MUST NOT propagate this as a hard error because that would rollback the
		// entire Phase 2 transaction, leaving no release record at all.
		return true, nil
	}

	// Step 8f: health probes (skip workers and frontends — they don't have /health endpoints)
	for _, app := range result.Apps {
		appType := appTypeFromName(app)
		if appType == "worker" || appType == "frontend" {
			continue
		}
		if app.URL == "" {
			continue
		}

		// Emit SSE deploy.health_check checking (user-facing: no "release" terminology per Rule 3)
		checkingData, _ := json.Marshal(map[string]interface{}{
			"status": "checking",
			"app":    app.Name,
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.health_check", checkingData); err != nil {
			return false, fmt.Errorf("activate release: emit health_check checking: %w", err)
		}

		probeStart := time.Now()
		probeErr := o.health.Probe(ctx, app.URL, "/health")
		probeDuration := time.Since(probeStart)

		if probeErr != nil {
			failedData, _ := json.Marshal(map[string]interface{}{
				"status":      "failed",
				"app":         app.Name,
				"duration_ms": probeDuration.Milliseconds(),
				"error":       probeErr.Error(),
			})
			if _, err := o.eventStore.Append(ctx, build.ID, "deploy.health_check", failedData); err != nil {
				return false, fmt.Errorf("activate release: emit health_check failed: %w", err)
			}
			return true, nil
		}

		okData, _ := json.Marshal(map[string]interface{}{
			"status":      "ok",
			"app":         app.Name,
			"duration_ms": probeDuration.Milliseconds(),
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.health_check", okData); err != nil {
			return false, fmt.Errorf("activate release: emit health_check ok: %w", err)
		}
	}

	return false, nil
}

// finalizeActivation marks the release as ACTIVE or SYNC_FAILED based on health
// probe results, updates environment targets, and supersedes previous releases
// (steps 8h-8i).
func (o *BuildOrchestrator) finalizeActivation(ctx context.Context, tx db.DBTX, build *db.Build, release *db.Release, result BuildCallbackRequest, probeFailed bool) error {
	if probeFailed {
		// Step 8i: Release → SYNC_FAILED
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncFailed_)); err != nil {
			return fmt.Errorf("activate release: transition to SYNC_FAILED: %w", err)
		}

		failedData, _ := json.Marshal(map[string]interface{}{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "sync_failed",
			"reason":    "HEALTH_PROBE_FAILED",
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.sync_failed", failedData); err != nil {
			return fmt.Errorf("activate release: emit deploy.sync_failed: %w", err)
		}

		return nil
	}

	// Step 8h: all probes passed — Release → ACTIVE
	if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted)); err != nil {
		return fmt.Errorf("activate release: transition to ACTIVE: %w", err)
	}

	// Emit SSE deploy.active (user-facing: no "release" terminology per Rule 3)
	activeData, _ := json.Marshal(map[string]string{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "active",
	})
	if _, err := o.eventStore.Append(ctx, build.ID, "deploy.active", activeData); err != nil {
		return fmt.Errorf("activate release: emit deploy.active: %w", err)
	}

	// Update environment_targets.active_release_id
	if err := o.envTargetStore.SetActive(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID); err != nil {
		return fmt.Errorf("activate release: set active: %w", err)
	}

	// Supersede previous active release
	prevRelease, err := o.releaseStore.GetActiveByEnv(ctx, build.TenantID, build.ProjectID, build.Environment)
	if err != nil {
		return fmt.Errorf("activate release: get previous active: %w", err)
	}
	if prevRelease != nil && prevRelease.ID != release.ID {
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, prevRelease.ID, string(statemachine.ReleaseNewActivated)); err != nil {
			return fmt.Errorf("activate release: supersede previous: %w", err)
		}
	}

	// Emit deploy.completed with full deploy response (DA-5)
	deployID := deriveDeployID(release.ID)
	// F-06: Calculate real duration from build creation to completion
	durationSecs := int(time.Since(build.CreatedAt).Seconds())

	var deployApps []map[string]interface{}
	for _, app := range result.Apps {
		appEntry := map[string]interface{}{
			"name":                   app.Name,
			"status":                 "deployed",
			"build_duration_seconds": durationSecs,
		}
		if app.URL != "" {
			appEntry["url"] = app.URL
		}
		deployApps = append(deployApps, appEntry)
	}

	deployData, _ := json.Marshal(map[string]interface{}{
		"deploy_id":        deployID,
		"status":           "success",
		"duration_seconds": durationSecs,
		"apps":             deployApps,
	})
	if _, err := o.eventStore.Append(ctx, build.ID, "deploy.completed", deployData); err != nil {
		return fmt.Errorf("activate release: emit deploy.completed: %w", err)
	}

	return nil
}

// deriveDeployID creates a deploy_id in the format d-{4hex} from a release ID.
// Example: rel-8f3a2b1c → d-8f3a
func deriveDeployID(releaseID string) string {
	// Strip the "rel-" prefix to get the hex portion
	hex := strings.TrimPrefix(releaseID, "rel-")
	if len(hex) >= 4 {
		return "d-" + hex[:4]
	}
	return "d-" + hex
}

// appTypeFromName determines the app type from the callback app.
// When the callback includes a Type field, use it directly.
// Otherwise, fall back to URL heuristic: empty URL = worker.
func appTypeFromName(app AppRelease) string {
	if app.Type != "" {
		return app.Type
	}
	// Fallback: workers have no URL
	if app.URL == "" {
		return "worker"
	}
	return "server"
}

