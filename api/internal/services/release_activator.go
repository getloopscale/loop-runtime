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
	App     AppRelease
	Healthy bool
	Skipped bool
	Duration time.Duration
	Err     error
}

// activateRelease orchestrates release creation through activation.
//
// BLOCKER-2 fix: I/O external operations (gitops push, ArgoCD sync, health probes)
// now happen OUTSIDE the advisory lock. The lock is only held for database state
// mutations (milliseconds), preventing lock contention when Gitea or ArgoCD are slow.
//
// Phases:
//   - Phase 2a (Lock): Create release record (READY)
//   - Phase 2b (no lock): Push manifests to GitOps repo
//   - Phase 2c (Lock): Update release state based on push result (PENDING or GIT_MIRROR_FAILED)
//   - Phase 2d (no lock): Trigger ArgoCD sync + health probes
//   - Phase 2e (Lock): Finalize (ACTIVE or SYNC_FAILED)
func (o *BuildOrchestrator) activateRelease(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
	// Phase 2a (Lock): Create release record
	release, err := o.phase2aCreateRelease(ctx, build, result)
	if err != nil {
		return err
	}

	// Phase 2b (no lock): Push manifests to GitOps repo
	commitSHA, pushErr := o.phase2bPushToGitOps(ctx, build, release, result)

	// Phase 2c (Lock): Update state based on push result
	gitPushFailed, err := o.phase2cRecordPushResult(ctx, build, release, result, commitSHA, pushErr)
	if err != nil {
		return err
	}
	if gitPushFailed {
		return nil // Release is in GIT_MIRROR_FAILED — can be retried later
	}

	// Phase 2d (no lock): ArgoCD sync + health probes
	probeFailed := o.phase2dSyncAndProbe(ctx, build, release, result)

	// Phase 2e (Lock): Finalize activation
	return o.phase2eFinalizeActivation(ctx, build, release, result, probeFailed)
}

// phase2aCreateRelease creates a release record in READY state inside the lock.
func (o *BuildOrchestrator) phase2aCreateRelease(ctx context.Context, build *db.Build, result BuildCallbackRequest) (*db.Release, error) {
	var release *db.Release

	err := o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		appsJSON, err := json.Marshal(result.Apps)
		if err != nil {
			return fmt.Errorf("activate release: marshal apps: %w", err)
		}

		// Store manifests in artifacts for retry-without-rebuild support (Step 6)
		var artifactsJSON json.RawMessage
		if result.Manifests != "" {
			artifactsJSON, err = json.Marshal(map[string]string{
				"manifests": result.Manifests,
			})
			if err != nil {
				return fmt.Errorf("activate release: marshal artifacts: %w", err)
			}
		}

		release = &db.Release{
			BuildID:     build.ID,
			TenantID:    build.TenantID,
			ProjectID:   build.ProjectID,
			Environment: build.Environment,
			CreatedBy:   build.CreatedBy,
			Apps:        appsJSON,
			Artifacts:   artifactsJSON,
		}
		if err := o.releaseStore.Create(ctx, tx, release); err != nil {
			return fmt.Errorf("activate release: create release: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return release, nil
}

// phase2bPushToGitOps pushes manifests to Gitea OUTSIDE the lock.
// Returns the commit SHA on success, or an error on failure.
func (o *BuildOrchestrator) phase2bPushToGitOps(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest) (string, error) {
	// Use a context with timeout to prevent indefinite hangs on Gitea
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	return o.gitops.Push(pushCtx, GitOpsPushOpts{
		TenantID:      build.TenantID,
		ProjectID:     build.ProjectID,
		Environment:   build.Environment,
		Manifests:     result.Manifests,
		BuildID:       build.ID,
		ReleaseID:     release.ID,
		CorrelationID: build.CorrelationID,
	})
}

// phase2cRecordPushResult records the GitOps push outcome inside the lock.
// On success: READY → PENDING, updates environment_targets.
// On failure: READY → GIT_MIRROR_FAILED.
func (o *BuildOrchestrator) phase2cRecordPushResult(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest, commitSHA string, pushErr error) (gitPushFailed bool, err error) {
	return gitPushFailed, o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if pushErr != nil {
			slog.Error("gitops push failed",
				"build_id", build.ID,
				"release_id", release.ID,
				"error", pushErr,
			)

			// Transition READY → GIT_MIRROR_FAILED
			if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorFailed_)); err != nil {
				return fmt.Errorf("activate release: transition to GIT_MIRROR_FAILED: %w", err)
			}

			failedData, _ := json.Marshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "git_push_failed",
				"error":     pushErr.Error(),
			})
			if _, err := o.eventStore.Append(ctx, build.ID, "deploy.git_push_failed", failedData); err != nil {
				return fmt.Errorf("activate release: emit deploy.git_push_failed: %w", err)
			}

			gitPushFailed = true
			return nil
		}

		// Store the git commit SHA on the release (for audit trail)
		_ = commitSHA // TODO: persist git_commit on release when UpdateGitCommit method is added

		// Transition READY → PENDING via GitMirrorCompleted
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorCompleted)); err != nil {
			return fmt.Errorf("activate release: transition to PENDING: %w", err)
		}

		// Emit SSE deploy.pending
		pendingData, _ := json.Marshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "pending",
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.pending", pendingData); err != nil {
			return fmt.Errorf("activate release: emit deploy.pending: %w", err)
		}

		// Update environment_targets.desired_release_id
		if err := o.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
			return fmt.Errorf("activate release: set desired: %w", err)
		}

		return nil
	})
}

// phase2dSyncAndProbe triggers ArgoCD sync and runs health probes OUTSIDE the lock.
// Returns true if any probe failed.
func (o *BuildOrchestrator) phase2dSyncAndProbe(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest) bool {
	// Trigger ArgoCD sync
	argoAppName := fmt.Sprintf("%s-%s", build.TenantID, build.Environment)
	if err := o.sync.TriggerSync(ctx, argoAppName); err != nil {
		// Sync failure → treated as probe failure, release will be SYNC_FAILED
		return true
	}

	// Health probes — run in parallel with semaphore (A-09)
	results := o.probeAppsParallel(ctx, result.Apps)

	// Emit SSE events for each probe result
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
				slog.Error("failed to emit health_check failed event", "build_id", build.ID, "error", err)
			}
			return true // At least one probe failed
		}

		okData, _ := json.Marshal(map[string]interface{}{
			"status":      "ok",
			"app":         pr.App.Name,
			"duration_ms": pr.Duration.Milliseconds(),
		})
		if _, err := o.eventStore.Append(ctx, build.ID, "deploy.health_check", okData); err != nil {
			slog.Error("failed to emit health_check ok event", "build_id", build.ID, "error", err)
		}
	}

	return false
}

// phase2eFinalizeActivation marks the release as ACTIVE or SYNC_FAILED inside the lock.
func (o *BuildOrchestrator) phase2eFinalizeActivation(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest, probeFailed bool) error {
	return o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if probeFailed {
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

		// All probes passed — Release → ACTIVE
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted)); err != nil {
			return fmt.Errorf("activate release: transition to ACTIVE: %w", err)
		}

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

		// Supersede previous active release (MOD-4: use tx for consistency)
		prevRelease, err := o.releaseStore.GetActiveByEnvTx(ctx, tx, build.TenantID, build.ProjectID, build.Environment)
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
	})
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
func (o *BuildOrchestrator) RetryRelease(ctx context.Context, releaseID string) error {
	release, err := o.releaseStore.GetByID(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("retry release: get release: %w", err)
	}

	if release.Status != string(statemachine.ReleaseGitMirrorFailed) {
		return fmt.Errorf("retry release: release %s is in %s state, not GIT_MIRROR_FAILED — cannot retry", releaseID, release.Status)
	}

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

	// Push outside lock (consistent with BLOCKER-2 fix)
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, pushErr := o.gitops.Push(pushCtx, GitOpsPushOpts{
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

	// Record success inside lock
	return o.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, releaseID, string(statemachine.ReleaseGitMirrorRetried)); err != nil {
			return fmt.Errorf("retry release: transition to PENDING: %w", err)
		}

		if err := o.envTargetStore.SetDesired(ctx, tx, release.TenantID, release.ProjectID, release.Environment, releaseID, release.CreatedBy, release.CorrelationID); err != nil {
			return fmt.Errorf("retry release: set desired: %w", err)
		}

		return nil
	})
}

// appTypeFromName determines the app type from the callback app.
func appTypeFromName(app AppRelease) string {
	if app.Type != "" {
		return app.Type
	}
	if app.URL == "" {
		return "worker"
	}
	return "server"
}
