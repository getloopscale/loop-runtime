// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// ContainerDeliveryActivator implements DeliveryActivator for container-based
// deployments (Kubernetes pods via Gitea + ArgoCD).
//
// This is the existing delivery model extracted into the interface pattern
// (MEET-2026-03-26-009 Fase 0). The 5-phase activation and rollback logic
// are identical to the pre-refactoring code — zero behavior change.
// GitManifestReader abstracts reading manifests from git history (DIP for rollback).
type GitManifestReader interface {
	GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error)
}

type ContainerDeliveryActivator struct {
	releaseStore   db.ReleaseStore
	envTargetStore db.EnvironmentTargetStore
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	sync              SyncClient
	locker            EnvironmentLocker
	gitops            GitOpsPusher
	gitReader         GitManifestReader
	argocdSyncTimeout time.Duration
}

// ContainerDeliveryDeps holds dependencies for ContainerDeliveryActivator.
type ContainerDeliveryDeps struct {
	ReleaseStore      db.ReleaseStore
	EnvTargetStore    db.EnvironmentTargetStore
	EventStore        interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	Sync              SyncClient
	Locker            EnvironmentLocker
	GitOps            GitOpsPusher
	GitReader         GitManifestReader
	ArgocdSyncTimeout time.Duration
}

// NewContainerDeliveryActivator creates a new ContainerDeliveryActivator.
func NewContainerDeliveryActivator(deps ContainerDeliveryDeps) *ContainerDeliveryActivator {
	syncTimeout := deps.ArgocdSyncTimeout
	if syncTimeout == 0 {
		syncTimeout = 600 * time.Second
	}
	return &ContainerDeliveryActivator{
		releaseStore:      deps.ReleaseStore,
		envTargetStore:    deps.EnvTargetStore,
		eventStore:        deps.EventStore,
		sync:              deps.Sync,
		locker:            deps.Locker,
		gitops:            deps.GitOps,
		gitReader:         deps.GitReader,
		argocdSyncTimeout: syncTimeout,
	}
}

// Activate runs the 5-phase container release activation.
// Phases A-E are semantically identical to the pre-refactoring activateRelease.
func (c *ContainerDeliveryActivator) Activate(ctx context.Context, build *db.Build, result BuildCallbackRequest) error {
	// Phase A (Lock): Create release record (READY)
	release, err := c.phaseACreateRelease(ctx, build, result)
	if err != nil {
		return err
	}

	// Phase B (no lock): Push manifests to GitOps repo
	commitSHA, pushErr := c.phaseBDeliver(ctx, build, release, result)

	// Phase C (Lock): Record delivery result (PENDING or GIT_MIRROR_FAILED)
	deliveryFailed, err := c.phaseCRecordResult(ctx, build, release, commitSHA, pushErr)
	if err != nil {
		return err
	}
	if deliveryFailed {
		// Return error so BuildOrchestrator keeps build in RELEASING for retry
		return fmt.Errorf("delivery failed: gitops push error")
	}

	// Phase D is now ASYNC (MEET-2026-03-28-015).
	// Release activation is handled by:
	//   1. ArgoCD Notification callback (fast path, seconds)
	//   2. ReleaseConvergenceLoop (safety net, polls every 10s)
	//   3. ReleaseReaper (ultimate timeout, 10m)
	//
	// We trigger ArgoCD sync once here, then return immediately.
	// The release stays in PENDING state until async activation.
	argoAppName := fmt.Sprintf("%s-%s", build.TenantID, build.Environment)
	if err := c.triggerSyncWithRetry(ctx, argoAppName); err != nil {
		slog.Error("argocd trigger sync failed after retries",
			"build_id", build.ID, "app", argoAppName, "error", err)
		// Sync trigger failed — fail the release immediately
		return c.phaseEFinalize(ctx, build, release, result, true)
	}

	// Emit deploy.verifying to show async verification started
	verifyingData, _ := json.Marshal(map[string]interface{}{
		"deploy_id": deriveDeployID(release.ID),
		"status":    "verifying",
		"app":       argoAppName,
	})
	if _, emitErr := c.eventStore.Append(ctx, build.ID, "deploy.verifying", verifyingData); emitErr != nil {
		slog.Warn("failed to emit deploy.verifying event", "build_id", build.ID, "error", emitErr)
	}

	// Release is PENDING — async paths will activate and emit deploy.completed
	return nil
}

// Rollback delivers a previous release's manifests to the runtime.
// Fetches old manifests from Gitea git history, pushes them, and triggers ArgoCD sync.
// State machine transitions are handled by the RollbackOrchestrator, not here.
func (c *ContainerDeliveryActivator) Rollback(ctx context.Context, req RollbackDeliveryRequest) error {
	if req.TargetRelease.GitCommit == "" {
		return fmt.Errorf("container rollback: target release has no git commit")
	}

	// Fetch old manifests from git
	manifests, err := c.gitReader.GetManifestsAtCommit(ctx, req.TenantID, req.ProjectID, req.Environment, req.TargetRelease.GitCommit)
	if err != nil {
		return fmt.Errorf("container rollback: fetch manifests: %w", err)
	}

	// Push old manifests to gitops repo
	_, err = c.gitops.Push(ctx, GitOpsPushOpts{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		Manifests:     string(manifests),
		ReleaseID:     req.TargetRelease.ID,
		CorrelationID: req.CorrelationID,
	})
	if err != nil {
		return fmt.Errorf("container rollback: push manifests: %w", err)
	}

	// Trigger ArgoCD sync (best-effort).
	// ArgoCD Application name is {tenantID}-{environment}, matching what the
	// TenantController creates via EnsureApplication. NOT ProjectName.
	// See MEET-2026-03-27-012 for the bug report.
	argoAppName := fmt.Sprintf("%s-%s", req.TenantID, req.Environment)
	if syncErr := c.sync.TriggerSync(ctx, argoAppName); syncErr != nil {
		slog.Warn("container rollback: argocd sync trigger failed, continuing", "error", syncErr)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Phase implementations (extracted from release_activator.go, same logic)
// ---------------------------------------------------------------------------

func (c *ContainerDeliveryActivator) phaseACreateRelease(ctx context.Context, build *db.Build, result BuildCallbackRequest) (*db.Release, error) {
	var release *db.Release

	err := c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		appsJSON, err := json.Marshal(result.Apps)
		if err != nil {
			return fmt.Errorf("activate release: marshal apps: %w", err)
		}

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
		if err := c.releaseStore.Create(ctx, tx, release); err != nil {
			return fmt.Errorf("activate release: create release: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return release, nil
}

func (c *ContainerDeliveryActivator) phaseBDeliver(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest) (string, error) {
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	return c.gitops.Push(pushCtx, GitOpsPushOpts{
		TenantID:      build.TenantID,
		ProjectID:     build.ProjectID,
		Environment:   build.Environment,
		Manifests:     result.Manifests,
		BuildID:       build.ID,
		ReleaseID:     release.ID,
		CorrelationID: build.CorrelationID,
	})
}

func (c *ContainerDeliveryActivator) phaseCRecordResult(ctx context.Context, build *db.Build, release *db.Release, commitSHA string, pushErr error) (deliveryFailed bool, err error) {
	return deliveryFailed, c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if pushErr != nil {
			slog.Error("gitops push failed",
				"build_id", build.ID,
				"release_id", release.ID,
				"error", pushErr,
			)

			if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorFailed_)); err != nil {
				return fmt.Errorf("activate release: transition to GIT_MIRROR_FAILED: %w", err)
			}

			failedData, _ := json.Marshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "delivery_failed",
				"error":     pushErr.Error(),
			})
			if _, err := c.eventStore.Append(ctx, build.ID, "deploy.delivery_failed", failedData); err != nil {
				return fmt.Errorf("activate release: emit deploy.delivery_failed: %w", err)
			}

			deliveryFailed = true
			return nil
		}

		if commitSHA != "" {
			if err := c.releaseStore.SetGitCommit(ctx, tx, release.ID, commitSHA); err != nil {
				slog.Warn("failed to persist git_commit on release (non-fatal)",
					"release_id", release.ID, "commit_sha", commitSHA, "error", err)
			}
		}

		if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseGitMirrorCompleted)); err != nil {
			return fmt.Errorf("activate release: transition to PENDING: %w", err)
		}

		// SSE: deploy.delivering (delivery-agnostic name)
		pendingData, _ := json.Marshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "delivering",
		})
		if _, err := c.eventStore.Append(ctx, build.ID, "deploy.delivering", pendingData); err != nil {
			return fmt.Errorf("activate release: emit deploy.delivering: %w", err)
		}

		if err := c.envTargetStore.SetDesired(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID, build.CreatedBy, build.CorrelationID); err != nil {
			return fmt.Errorf("activate release: set desired: %w", err)
		}

		return nil
	})
}

// phaseDVerify REMOVED (MEET-2026-03-28-015) — 81 lines of synchronous ArgoCD polling deleted.
// Replaced by async: ReleaseConvergenceLoop + SyncCallbackActivator.
// Historical reference: git show f1bf264:api/internal/services/container_delivery.go

func (c *ContainerDeliveryActivator) phaseEFinalize(ctx context.Context, build *db.Build, release *db.Release, result BuildCallbackRequest, verifyFailed bool) error {
	return c.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		if verifyFailed {
			if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncFailed_)); err != nil {
				return fmt.Errorf("activate release: transition to SYNC_FAILED: %w", err)
			}

			failedData, _ := json.Marshal(map[string]interface{}{
				"deploy_id": deriveDeployID(release.ID),
				"status":    "sync_failed",
				"reason":    "HEALTH_PROBE_FAILED",
			})
			if _, err := c.eventStore.Append(ctx, build.ID, "deploy.sync_failed", failedData); err != nil {
				return fmt.Errorf("activate release: emit deploy.sync_failed: %w", err)
			}

			return nil
		}

		// Release → ACTIVE
		if _, err := c.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted)); err != nil {
			return fmt.Errorf("activate release: transition to ACTIVE: %w", err)
		}

		// SSE: deploy.live (delivery-agnostic name)
		activeData, _ := json.Marshal(map[string]string{
			"deploy_id": deriveDeployID(release.ID),
			"status":    "live",
		})
		if _, err := c.eventStore.Append(ctx, build.ID, "deploy.live", activeData); err != nil {
			return fmt.Errorf("activate release: emit deploy.live: %w", err)
		}

		if err := c.envTargetStore.SetActive(ctx, tx, build.TenantID, build.ProjectID, build.Environment, release.ID); err != nil {
			return fmt.Errorf("activate release: set active: %w", err)
		}

		// Supersede previous active release
		prevRelease, err := c.releaseStore.GetActiveByEnvTx(ctx, tx, build.TenantID, build.ProjectID, build.Environment)
		if err != nil {
			return fmt.Errorf("activate release: get previous active: %w", err)
		}
		if prevRelease != nil && prevRelease.ID != release.ID {
			if _, err := c.releaseStore.UpdateStatus(ctx, tx, prevRelease.ID, string(statemachine.ReleaseNewActivated)); err != nil {
				return fmt.Errorf("activate release: supersede previous: %w", err)
			}
		}

		// SSE: deploy.completed
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
		if _, err := c.eventStore.Append(ctx, build.ID, "deploy.completed", deployData); err != nil {
			return fmt.Errorf("activate release: emit deploy.completed: %w", err)
		}

		return nil
	})
}

func (c *ContainerDeliveryActivator) triggerSyncWithRetry(ctx context.Context, appName string) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := c.sync.TriggerSync(ctx, appName); err == nil {
			return nil
		} else {
			lastErr = err
			slog.Warn("argocd trigger sync failed, retrying",
				"app", appName, "attempt", attempt+1, "error", err)
		}

		backoff := time.Duration(1<<uint(attempt)) * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return lastErr
}
