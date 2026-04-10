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

// PendingReleaseByTenantEnvFinder is an optional interface for efficient tenant+env lookup.
// Falls back to FindPendingReleases + Go-side filtering if not implemented (CODE-SCAN-001).
type PendingReleaseByTenantEnvFinder interface {
	FindPendingReleasesByTenantEnv(ctx context.Context, tenantID, env string) ([]db.Release, error)
}

// OnSyncCompleted is called when ArgoCD reports an Application as Synced + Healthy.
// The appName is in format "{tenantID}-{environment}" (e.g., "tn-loopzero-production").
// It finds all PENDING releases matching that tenant+environment and activates them.
func (a *SyncCallbackActivator) OnSyncCompleted(appName, revision string) error {
	// ARCH-006 fix: Use a timeout context derived from the server shutdown context
	// would be ideal, but since this is called from an HTTP handler that already
	// has a request context with timeout (argocd_callback.go), we use a bounded
	// background context as fallback.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse tenantID and environment from app name: "{tenant}-{env}"
	tenantID, env, ok := ParseArgoAppName(appName)
	if !ok {
		return fmt.Errorf("sync callback: invalid app name %q (expected {tenant}-{env})", appName)
	}

	// CODE-SCAN-001 fix: Use tenant+env-filtered query when available,
	// avoiding full table scan of all PENDING releases.
	var releases []db.Release
	var err error
	if finder, ok := a.releaseStore.(PendingReleaseByTenantEnvFinder); ok {
		releases, err = finder.FindPendingReleasesByTenantEnv(ctx, tenantID, env)
	} else {
		// Fallback path: scan PENDING releases in the default cell.
		// Real production always hits the efficient tenant+env path above;
		// this branch only fires in tests with simple store fakes. We pass
		// db.CellDefault because the callback itself does not carry cell
		// information — if a caller needs cross-cell callback routing, the
		// efficient finder must be implemented on the store.
		var allReleases []db.Release
		allReleases, err = a.releaseStore.FindPendingReleases(ctx, db.CellDefault)
		if err == nil {
			for _, r := range allReleases {
				if r.TenantID == tenantID && r.Environment == env {
					releases = append(releases, r)
				}
			}
		}
	}
	if err != nil {
		return fmt.Errorf("sync callback: find pending releases: %w", err)
	}

	activated := 0
	for _, release := range releases {
		// MEET-2026-03-31-024 BUG-03 defense-in-depth: if GitCommit is empty,
		// do NOT activate via callback — let the convergence loop handle it.
		// Empty GitCommit means SetGitCommit failed (now fatal) or a race occurred.
		// Activating with empty GitCommit bypasses revision verification.
		if release.GitCommit == "" {
			a.logger.Warn("sync callback: release has empty GitCommit, deferring to convergence loop",
				"release_id", release.ID)
			continue
		}

		// Verify revision matches
		if revision != "" && release.GitCommit != revision {
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
		apps := releaseAppsToActivationApps(release)
		duration := int(time.Since(release.CreatedAt).Seconds())
		return ActivateReleaseKernel(ctx, tx, release, a.releaseStore, a.envTargetStore, a.eventStore, apps, duration, a.logger)
	})
}

// ParseArgoAppName extracts tenantID and environment from an ArgoCD Application name.
// Format: "{tenantID}-{environment}" (e.g., "tn-loopzero-production").
//
// ARCH-002 fix: Uses LastIndex("-") to split, which means the environment name
// MUST NOT contain hyphens. This is enforced because:
//   - Tenant IDs contain hyphens (e.g., "tn-loopzero-e2e")
//   - Environment is always the last segment after the last hyphen
//   - Sprint 5 preview environments MUST use underscore or single-word names
//     (e.g., "preview_pr42" not "preview-pr-42")
//
// TODO(sprint-5): Consider switching to ArgoCD labels in the webhook payload
// instead of parsing the app name. Labels contain tenant/env as separate fields.
func ParseArgoAppName(appName string) (tenantID, env string, ok bool) {
	idx := strings.LastIndex(appName, "-")
	if idx <= 0 || idx >= len(appName)-1 {
		return "", "", false
	}
	return appName[:idx], appName[idx+1:], true
}
