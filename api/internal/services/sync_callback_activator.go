// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

// SyncCallbackActivator processes ArgoCD sync completion callbacks.
// When ArgoCD notifies that an Application is Synced + Healthy, this
// finds PENDING releases for that application and activates them.
//
// This is the push-based replacement for Phase D polling.
// MEET-2026-03-27-012: ArgoCD notifies instead of API polling.
type SyncCallbackActivator struct {
	releaseStore   ReleaseConvergenceStore
	envTargetStore db.EnvironmentTargetStore
	locker         EnvironmentLocker
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger *slog.Logger
}

// NewSyncCallbackActivator creates a new SyncCallbackActivator.
func NewSyncCallbackActivator(
	releaseStore ReleaseConvergenceStore,
	envTargetStore db.EnvironmentTargetStore,
	locker EnvironmentLocker,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	logger *slog.Logger,
) *SyncCallbackActivator {
	return &SyncCallbackActivator{
		releaseStore:   releaseStore,
		envTargetStore: envTargetStore,
		locker:         locker,
		eventStore:     eventStore,
		logger:         logger,
	}
}

// OnSyncCompleted is called when ArgoCD reports an Application as Synced + Healthy.
// The appName is in format "{tenantID}-{environment}" (e.g., "tn-loopzero-production").
// It finds all PENDING releases matching that tenant+environment and activates them.
func (a *SyncCallbackActivator) OnSyncCompleted(appName, revision string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse tenantID and environment from app name: "{tenant}-{env}"
	tenantID, env, ok := ParseArgoAppName(appName)
	if !ok {
		return fmt.Errorf("sync callback: invalid app name %q (expected {tenant}-{env})", appName)
	}

	// Find PENDING releases for this tenant+environment
	releases, err := a.releaseStore.FindPendingReleases(ctx)
	if err != nil {
		return fmt.Errorf("sync callback: find pending releases: %w", err)
	}

	activated := 0
	for _, release := range releases {
		if release.TenantID != tenantID || release.Environment != env {
			continue
		}

		// Verify revision matches if available
		if revision != "" && release.GitCommit != "" && release.GitCommit != revision {
			a.logger.Warn("sync callback: revision mismatch, skipping",
				"release_id", release.ID, "expected", release.GitCommit, "actual", revision)
			continue
		}

		if err := a.activateRelease(ctx, &release); err != nil {
			a.logger.Error("sync callback: activation failed",
				"release_id", release.ID, "error", err)
			continue
		}
		activated++
	}

	if activated > 0 {
		a.logger.Info("sync callback: releases activated",
			"app", appName, "count", activated, "revision", revision)
	}

	return nil
}

func (a *SyncCallbackActivator) activateRelease(ctx context.Context, release *db.Release) error {
	return a.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		_, err := a.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted))
		if err != nil {
			return fmt.Errorf("activate release: %w", err)
		}

		prev, prevErr := a.releaseStore.GetActiveByEnvTx(ctx, tx, release.TenantID, release.ProjectID, release.Environment)
		if prevErr != nil {
			a.logger.Warn("sync callback: failed to get previous active release",
				"release_id", release.ID, "project_id", release.ProjectID, "error", prevErr)
		}
		if prev != nil && prev.ID != release.ID {
			if _, supersedeErr := a.releaseStore.UpdateStatus(ctx, tx, prev.ID, string(statemachine.ReleaseNewActivated)); supersedeErr != nil {
				a.logger.Error("sync callback: failed to supersede previous release",
					"prev_id", prev.ID, "new_id", release.ID, "error", supersedeErr)
			} else {
				a.logger.Info("sync callback: superseded previous release",
					"prev_id", prev.ID, "new_id", release.ID, "project_id", release.ProjectID)
			}
		} else if prev == nil {
			a.logger.Info("sync callback: no previous active release to supersede",
				"release_id", release.ID, "project_id", release.ProjectID)
		}

		if err := a.envTargetStore.SetActive(ctx, tx, release.TenantID, release.ProjectID, release.Environment, release.ID); err != nil {
			return fmt.Errorf("set active target: %w", err)
		}

		liveData, _ := json.Marshal(map[string]interface{}{
			"status":     "live",
			"release_id": release.ID,
		})
		if _, emitErr := a.eventStore.Append(ctx, release.BuildID, "deploy.live", liveData); emitErr != nil {
			a.logger.Error("sync callback: failed to emit deploy.live", "release_id", release.ID, "error", emitErr)
		}

		// deploy.completed — CLI waits for this to exit (MEET-2026-03-28-015).
		var apps []db.BuildAppSpec
		if release.Apps != nil {
			if err := json.Unmarshal(release.Apps, &apps); err != nil {
				slog.Warn("sync callback: failed to unmarshal release apps",
					"release_id", release.ID, "error", err)
			}
		}
		duration := int(time.Since(release.CreatedAt).Seconds())
		appResults := make([]map[string]interface{}, 0, len(apps))
		for _, app := range apps {
			appResults = append(appResults, map[string]interface{}{
				"name":   app.Name,
				"status": "deployed",
			})
		}
		completedData, _ := json.Marshal(map[string]interface{}{
			"deploy_id":        deriveDeployID(release.ID),
			"status":           "success",
			"duration_seconds": duration,
			"apps":             appResults,
		})
		if _, emitErr := a.eventStore.Append(ctx, release.BuildID, "deploy.completed", completedData); emitErr != nil {
			a.logger.Error("sync callback: failed to emit deploy.completed", "release_id", release.ID, "error", emitErr)
		}

		return nil
	})
}

// ParseArgoAppName extracts tenantID and environment from "{tenant}-{env}".
// Returns false if the format is invalid.
func ParseArgoAppName(appName string) (tenantID, env string, ok bool) {
	// Find the last hyphen — environment is always the last segment
	idx := strings.LastIndex(appName, "-")
	if idx <= 0 || idx >= len(appName)-1 {
		return "", "", false
	}
	return appName[:idx], appName[idx+1:], true
}
