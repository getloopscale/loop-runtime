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
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// OnReleaseActivated is an optional callback invoked AFTER a release transitions to ACTIVE.
// Used by the GitHub Status Notifier to update commit status to "success".
// Registered once at startup via RegisterReleaseActivatedCallback().
// Non-blocking: runs in a goroutine. Errors are logged, not propagated.
// OnReleaseActivated callback receives buildID + environment after a release goes ACTIVE.
// The callback is responsible for looking up the build's source commit SHA (not the gitops SHA).
var onReleaseActivated func(ctx context.Context, tenantID, projectName, buildID, environment string)

// RegisterReleaseActivatedCallback sets the global callback for release activation.
func RegisterReleaseActivatedCallback(fn func(ctx context.Context, tenantID, projectName, buildID, environment string)) {
	onReleaseActivated = fn
}

// OnBuildFailed is an optional callback invoked when a build transitions to FAILED.
// Used by the GitHub Status Notifier to update commit status to "failure".
var onBuildFailed func(ctx context.Context, tenantID, projectName, commitSHA, buildID string)

// RegisterBuildFailedCallback sets the global callback for build failure.
func RegisterBuildFailedCallback(fn func(ctx context.Context, tenantID, projectName, commitSHA, buildID string)) {
	onBuildFailed = fn
}

// emitActivationFailure emits a deploy.sync_failed SSE event when release activation
// fails after a successful build. This ensures the CLI gets notified instead of
// waiting for a timeout.
func (o *BuildOrchestrator) emitActivationFailure(ctx context.Context, buildID string, activationErr error) {
	failedData := safeJSONMarshal(map[string]interface{}{
		"status": "sync_failed",
		"reason": "ACTIVATION_FAILED",
		"error":  activationErr.Error(),
	})
	if _, err := o.eventStore.Append(ctx, buildID, "deploy.sync_failed", failedData); err != nil {
		slog.Error("failed to emit activation failure event", "build_id", buildID, "error", err)
	}
}

// ReleaseActivationStore defines the minimal store interface needed by the
// release activation kernel. Both db.ReleaseStore and ReleaseConvergenceStore
// satisfy this interface.
type ReleaseActivationStore interface {
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
	GetActiveByEnvTx(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (*db.Release, error)
	// GetByIDTx re-fetches a release inside the lock transaction.
	// MEET-2026-03-31-023 T0-1: prevents duplicate activation race.
	GetByIDTx(ctx context.Context, tx db.DBTX, id string) (*db.Release, error)
}

// ReleaseActivationEventStore is the subset of EventStore used by activation.
type ReleaseActivationEventStore interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
}

// ActivationAppInfo describes one app in the deploy.completed SSE event.
type ActivationAppInfo struct {
	Name   string
	Status string
	URL    string // optional; set by container delivery (has build callback URLs)
}

// ActivateReleaseKernel implements the correct release activation sequence.
// CODE-0001: Queries the previous active release BEFORE transitioning the new
// release to ACTIVE, preventing the race where GetActiveByEnvTx returns the
// just-activated release instead of the previous one.
//
// This is the single source of truth for release activation. All three activation
// paths (ContainerDeliveryActivator, ReleaseConvergenceLoop, SyncCallbackActivator)
// MUST call this function inside a withEnvironmentLock transaction.
//
// Sequence: (1) get prev release, (2) transition ACTIVE, (3) emit deploy.live,
// (4) SetActive, (5) supersede prev, (6) emit deploy.completed.
func ActivateReleaseKernel(
	ctx context.Context,
	tx db.DBTX,
	release *db.Release,
	releaseStore ReleaseActivationStore,
	envTargetStore db.EnvironmentTargetStore,
	eventStore ReleaseActivationEventStore,
	apps []ActivationAppInfo,
	durationSecs int,
	logger *slog.Logger,
) error {
	// MEET-2026-03-31-023 T0-1: Idempotency guard — re-fetch release inside the lock
	// to prevent duplicate activation when SyncCallbackActivator and
	// ReleaseConvergenceLoop race on the same release.
	current, err := releaseStore.GetByIDTx(ctx, tx, release.ID)
	if err != nil {
		return fmt.Errorf("activate release: re-fetch inside lock: %w", err)
	}
	if current.Status != string(statemachine.ReleasePending) {
		logger.Info("activate release: already transitioned, skipping",
			"release_id", release.ID, "current_status", current.Status)
		return nil
	}

	// Step 1: CODE-0001 — query previous ACTIVE release BEFORE activating new one.
	prevRelease, err := releaseStore.GetActiveByEnvTx(ctx, tx, release.TenantID, release.ProjectID, release.Environment)
	if err != nil {
		return fmt.Errorf("activate release: get previous active: %w", err)
	}

	// Step 2: Transition PENDING → ACTIVE.
	if _, err := releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted)); err != nil {
		return fmt.Errorf("activate release: transition to ACTIVE: %w", err)
	}

	// MEET-2026-03-31-023 T1-8: Record convergence duration (PENDING → ACTIVE).
	convergenceDuration := time.Since(release.CreatedAt).Seconds()
	metrics.ReleaseConvergenceDuration.WithLabelValues(release.TenantID, release.Environment).Observe(convergenceDuration)

	// End-to-end deploy duration: build creation → release ACTIVE.
	// durationSecs is computed by callers as time.Since(build.CreatedAt).
	metrics.DeployDuration.WithLabelValues(release.TenantID, release.Environment).Observe(float64(durationSecs))

	// Step 3: Emit deploy.live SSE event.
	liveData := safeJSONMarshal(map[string]interface{}{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "live",
	})
	if _, err := eventStore.Append(ctx, release.BuildID, "deploy.live", liveData); err != nil {
		logger.Error("activate release: failed to emit deploy.live", "release_id", release.ID, "error", err)
	}

	// Step 4: Update environment target pointer.
	if err := envTargetStore.SetActive(ctx, tx, release.TenantID, release.ProjectID, release.Environment, release.ID); err != nil {
		return fmt.Errorf("activate release: set active: %w", err)
	}

	// Step 5: Supersede previous active release (queried in Step 1, before activation).
	if prevRelease != nil && prevRelease.ID != release.ID {
		if _, supersedeErr := releaseStore.UpdateStatus(ctx, tx, prevRelease.ID, string(statemachine.ReleaseNewActivated)); supersedeErr != nil {
			logger.Error("activate release: failed to supersede previous",
				"prev_id", prevRelease.ID, "new_id", release.ID, "error", supersedeErr)
		} else {
			logger.Info("activate release: superseded previous",
				"prev_id", prevRelease.ID, "new_id", release.ID)
		}
	}

	// Step 6: Emit deploy.completed SSE event — CLI waits for this to exit.
	deployID := deriveDeployID(release.ID)
	appResults := make([]map[string]interface{}, 0, len(apps))
	for _, app := range apps {
		entry := map[string]interface{}{
			"name":   app.Name,
			"status": app.Status,
		}
		if app.URL != "" {
			entry["url"] = app.URL
		}
		appResults = append(appResults, entry)
	}
	completedData := safeJSONMarshal(map[string]interface{}{
		"deploy_id":        deployID,
		"status":           "success",
		"duration_seconds": durationSecs,
		"apps":             appResults,
	})
	if _, err := eventStore.Append(ctx, release.BuildID, "deploy.completed", completedData); err != nil {
		logger.Error("activate release: failed to emit deploy.completed", "release_id", release.ID, "error", err)
	}

	// Step 7: Notify external systems (GitHub commit status, etc.)
	if onReleaseActivated != nil {
		go func() {
			cbCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			onReleaseActivated(cbCtx, release.TenantID, release.ProjectID, release.BuildID, release.Environment)
		}()
	}

	return nil
}

// releaseAppsToActivationApps converts the JSON-encoded apps from a release
// into ActivationAppInfo structs for the activation kernel.
// F-03 fix: computes URL for server/frontend apps so deploy.completed SSE event includes it.
func releaseAppsToActivationApps(release *db.Release, baseDomain string) []ActivationAppInfo {
	var specs []db.BuildAppSpec
	if release.Apps != nil {
		if err := json.Unmarshal(release.Apps, &specs); err != nil {
			slog.Warn("failed to unmarshal release apps for activation",
				"release_id", release.ID, "error", err)
		}
	}
	apps := make([]ActivationAppInfo, 0, len(specs))
	for _, s := range specs {
		url := ""
		if (s.Type == "server" || s.Type == "frontend") && baseDomain != "" {
			url = fmt.Sprintf("https://%s-%s-%s-%s.%s",
				release.TenantID, release.ProjectID, s.Name, release.Environment, baseDomain)
		}
		apps = append(apps, ActivationAppInfo{Name: s.Name, Status: "deployed", URL: url})
	}
	return apps
}

// safeJSONMarshal marshals v to JSON. If marshalling fails, it logs the error
// and returns a fallback empty JSON object so SSE events are still emitted.
func safeJSONMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("json.Marshal failed for SSE event payload", "error", err)
		return json.RawMessage("{}")
	}
	return data
}

// deriveDeployID creates a deploy_id in the format d-{8hex} from a release ID.
// MOD-5: Increased from 4 to 8 hex chars (32 bits) to reduce collision probability.
// With 4 hex chars (16 bits), collisions were guaranteed after ~256 deploys.
// With 8 hex chars (32 bits), birthday paradox gives 50% collision at ~65K deploys.
// Example: rel-8f3a2b1c → d-8f3a2b1c
func deriveDeployID(releaseID string) string {
	hex := strings.TrimPrefix(releaseID, "rel-")
	if len(hex) >= 8 {
		return "d-" + hex[:8]
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
