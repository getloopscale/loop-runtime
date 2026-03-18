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
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// maxConcurrentProbes limits the number of parallel health probes during
// release activation. This prevents overwhelming the network or target
// services when a deploy contains many apps.
const maxConcurrentProbes = 5

// probeResult holds the outcome of a single health probe.
type probeResult struct {
	App        AppRelease
	Healthy    bool
	Skipped    bool
	Duration   time.Duration
	Err        error
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

	gitPushFailed, err := o.pushToGitOps(ctx, tx, build, release, result)
	if err != nil {
		return err
	}
	if gitPushFailed {
		// Release is in GIT_MIRROR_FAILED — transaction commits, retry without rebuild later
		return nil
	}

	probeFailed, err := o.waitForSync(ctx, tx, build, release, result)
	if err != nil {
		return err
	}

	return o.finalizeActivation(ctx, tx, build, release, result, probeFailed)
}

// createRelease creates a new release record in READY state (step 8a).
// Manifests are stored in the Artifacts field for retry support — if the
// gitops push fails, the API can retry from the stored manifests without
// triggering a new build.
func (o *BuildOrchestrator) createRelease(ctx context.Context, tx db.DBTX, build *db.Build, result BuildCallbackRequest) (*db.Release, error) {
	appsJSON, err := json.Marshal(result.Apps)
	if err != nil {
		return nil, fmt.Errorf("activate release: marshal apps: %w", err)
	}

	// Store manifests in artifacts for retry-without-rebuild support (Step 6)
	var artifactsJSON json.RawMessage
	if result.Manifests != "" {
		artifactsJSON, err = json.Marshal(map[string]string{
			"manifests": result.Manifests,
		})
		if err != nil {
			return nil, fmt.Errorf("activate release: marshal artifacts: %w", err)
		}
	}

	release := &db.Release{
		BuildID:     build.ID,
		TenantID:    build.TenantID,
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CreatedBy:   build.CreatedBy,
		Apps:        appsJSON,
		Artifacts:   artifactsJSON,
	}
	if err := o.releaseStore.Create(ctx, tx, release); err != nil {
		return nil, fmt.Errorf("activate release: create release: %w", err)
	}

	return release, nil
}

// pushToGitOps commits manifests to the tenant GitOps repo via GitOpsPusher,
// then transitions the release to PENDING and updates environment_targets.
//
// Step 6 architectural fix: the build pipeline no longer touches GitOps.
// The API owns the entire release flow after a build succeeds.
//
// On git push failure: transitions release to GIT_MIRROR_FAILED and returns
// (true, nil) so the transaction commits and the release can be retried later.
func (o *BuildOrchestrator) pushToGitOps(ctx context.Context, tx db.DBTX, build *db.Build, release *db.Release, result BuildCallbackRequest) (gitPushFailed bool, err error) {
	// Push manifests to GitOps repo
	commitSHA, pushErr := o.gitops.Push(ctx, GitOpsPushOpts{
		TenantID:      build.TenantID,
		ProjectID:     build.ProjectID,
		Environment:   build.Environment,
		Manifests:     result.Manifests,
		BuildID:       build.ID,
		ReleaseID:     release.ID,
		CorrelationID: build.CorrelationID,
	})

	if pushErr != nil {
		slog.Error("gitops push failed",
			"build_id", build.ID,
			"release_id", release.ID,
			"error", pushErr,
		)

		// Transition READY → GIT_MIRROR_FAILED — transaction commits so release persists
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorFailed_)); err != nil {
			return false, fmt.Errorf("activate release: transition to GIT_MIRROR_FAILED: %w", err)
		}

		// Emit SSE deploy.git_mirror_failed
		failedData, _ := json.Marshal(map[string]interface{}{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "git_push_failed",
			"error":     pushErr.Error(),
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.git_push_failed", failedData); err != nil {
			return false, fmt.Errorf("activate release: emit deploy.git_push_failed: %w", err)
		}

		return true, nil
	}

	// Store the git commit SHA on the release (for audit trail)
	_ = commitSHA // TODO: persist git_commit on release when UpdateGitCommit method is added

	// Transition READY → PENDING via GitMirrorCompleted
	if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorCompleted)); err != nil {
		return false, fmt.Errorf("activate release: transition to PENDING: %w", err)
	}

	// Emit SSE deploy.pending (user-facing: no "release" terminology per Rule 3)
	pendingData, _ := json.Marshal(map[string]string{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "pending",
	})
	if _, err := o.eventStore.Append(ctx, build.ID, "deploy.pending", pendingData); err != nil {
		return false, fmt.Errorf("activate release: emit deploy.pending: %w", err)
	}

	// Step 8d: update environment_targets.desired_release_id
	if err := o.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
		return false, fmt.Errorf("activate release: set desired: %w", err)
	}

	return false, nil
}

// waitForSync triggers ArgoCD sync and runs health probes against deployed apps
// (steps 8e-8f). Returns true if any health probe failed.
//
// Health probes run in parallel (A-09) with a concurrency limit of
// maxConcurrentProbes to avoid overwhelming target services during
// multi-app deploys.
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

	// Step 8f: health probes — run in parallel with semaphore (A-09).
	// Workers and frontends are skipped (they don't have /health endpoints).
	results := o.probeAppsParallel(ctx, result.Apps)

	// Emit SSE events for each probe result and check for failures.
	// SSE events are emitted sequentially to maintain a predictable event order.
	for _, pr := range results {
		if pr.Skipped {
			continue
		}

		if pr.Err != nil {
			failedData, _ := json.Marshal(map[string]interface{}{
				"status":      "failed",
				"app":         pr.App.Name,
				"duration_ms": pr.Duration.Milliseconds(),
				"error":       pr.Err.Error(),
			})
			if _, err := o.eventStore.Append(ctx, build.ID, "deploy.health_check", failedData); err != nil {
				return false, fmt.Errorf("activate release: emit health_check failed: %w", err)
			}
			probeFailed = true
			continue
		}

		okData, _ := json.Marshal(map[string]interface{}{
			"status":      "ok",
			"app":         pr.App.Name,
			"duration_ms": pr.Duration.Milliseconds(),
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.health_check", okData); err != nil {
			return false, fmt.Errorf("activate release: emit health_check ok: %w", err)
		}
	}

	return probeFailed, nil
}

// probeAppsParallel runs health probes for all probeable apps concurrently,
// limited by maxConcurrentProbes. Workers, frontends, and apps without URLs
// are skipped. Results are returned in the same order as the input slice.
func (o *BuildOrchestrator) probeAppsParallel(ctx context.Context, apps []AppRelease) []probeResult {
	results := make([]probeResult, len(apps))
	sem := make(chan struct{}, maxConcurrentProbes)
	var wg sync.WaitGroup

	for i, app := range apps {
		appType := appTypeFromName(app)
		if appType == "worker" || appType == "frontend" || app.URL == "" {
			results[i] = probeResult{App: app, Healthy: true, Skipped: true}
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // acquire semaphore slot
		go func(idx int, a AppRelease) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore slot

			start := time.Now()
			probeErr := o.health.Probe(ctx, a.URL, "/health")
			duration := time.Since(start)

			results[idx] = probeResult{
				App:      a,
				Healthy:  probeErr == nil,
				Skipped:  false,
				Duration: duration,
				Err:      probeErr,
			}
		}(i, app)
	}

	wg.Wait()
	return results
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

// RetryRelease retries the GitOps push for a release stuck in GIT_MIRROR_FAILED.
//
// This is the "retry without rebuild" mechanism (Step 6): the API reads the
// manifests stored in release.Artifacts and re-pushes them to the GitOps repo.
// No new build is triggered.
//
// On success: transitions GIT_MIRROR_FAILED → PENDING via GitMirrorRetried,
// updates environment_targets, and triggers ArgoCD sync.
//
// On failure: returns error, release stays in GIT_MIRROR_FAILED (safe to retry again).
func (o *BuildOrchestrator) RetryRelease(ctx context.Context, releaseID string) error {
	// Step 1: fetch release
	release, err := o.releaseStore.GetByID(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("retry release: get release: %w", err)
	}

	// Step 2: validate state — only GIT_MIRROR_FAILED is retryable
	if release.Status != string(statemachine.ReleaseGitMirrorFailed) {
		return fmt.Errorf("retry release: release %s is in %s state, not GIT_MIRROR_FAILED — cannot retry", releaseID, release.Status)
	}

	// Step 3: extract manifests from stored artifacts
	var artifacts map[string]string
	if release.Artifacts == nil {
		return fmt.Errorf("retry release: release %s has no stored artifacts", releaseID)
	}
	if err := json.Unmarshal(release.Artifacts, &artifacts); err != nil {
		return fmt.Errorf("retry release: unmarshal artifacts: %w", err)
	}
	manifests, ok := artifacts["manifests"]
	if !ok || manifests == "" {
		return fmt.Errorf("retry release: release %s has no manifests in artifacts", releaseID)
	}

	// Step 4: re-push to GitOps
	_, pushErr := o.gitops.Push(ctx, GitOpsPushOpts{
		TenantID:      release.TenantID,
		ProjectID:     release.ProjectID,
		Environment:   release.Environment,
		Manifests:     manifests,
		BuildID:       release.BuildID,
		ReleaseID:     release.ID,
		CorrelationID: release.CorrelationID,
	})
	if pushErr != nil {
		return fmt.Errorf("retry release: gitops push failed: %w", pushErr)
	}

	// Step 5: transition GIT_MIRROR_FAILED → PENDING inside lock
	return o.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		// Transition via state machine: GitMirrorRetried
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, releaseID, string(statemachine.ReleaseGitMirrorRetried)); err != nil {
			return fmt.Errorf("retry release: transition to PENDING: %w", err)
		}

		// Update environment_targets.desired_release_id
		if err := o.envTargetStore.SetDesired(ctx, tx, release.TenantID, release.ProjectID, release.Environment, releaseID, release.CreatedBy, release.CorrelationID); err != nil {
			return fmt.Errorf("retry release: set desired: %w", err)
		}

		return nil
	})
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
