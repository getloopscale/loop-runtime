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
	"github.com/loopzero/loopzero/pkg/statemachine"
)

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
