// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// ReleaseConvergenceLoopConfig configures the release convergence loop.
type ReleaseConvergenceLoopConfig struct {
	// Interval is how often the loop polls for pending releases.
	Interval time.Duration
	// SyncTimeout is how long to wait for ArgoCD sync before marking SYNC_FAILED.
	SyncTimeout time.Duration
	// MaxConcurrent bounds parallel release verifications.
	MaxConcurrent int
}

// ReleaseConvergenceStore defines the release store methods needed by the loop.
type ReleaseConvergenceStore interface {
	FindPendingReleases(ctx context.Context) ([]db.Release, error)
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
	GetActiveByEnvTx(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (*db.Release, error)
}

// ReleaseConvergenceLoop asynchronously verifies that PENDING releases
// converge (ArgoCD syncs and apps become healthy).
//
// MEET-2026-03-27-012 Phase 1.1: Decouples Phase D from the synchronous
// request path. The build reconcile loop no longer blocks on ArgoCD sync.
//
// Pattern: same as BuildReconcileLoop — semaphore-bounded concurrency,
// active map to prevent duplicates, ticker-based polling.
type ReleaseConvergenceLoop struct {
	releaseStore   ReleaseConvergenceStore
	envTargetStore db.EnvironmentTargetStore
	sync_          SyncClient
	locker         EnvironmentLocker
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger *slog.Logger
	cfg    ReleaseConvergenceLoopConfig

	sem    chan struct{}
	active sync.Map
}

// NewReleaseConvergenceLoop creates the convergence loop.
func NewReleaseConvergenceLoop(
	releaseStore ReleaseConvergenceStore,
	envTargetStore db.EnvironmentTargetStore,
	syncClient SyncClient,
	locker EnvironmentLocker,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	logger *slog.Logger,
	cfg ReleaseConvergenceLoopConfig,
) *ReleaseConvergenceLoop {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.SyncTimeout <= 0 {
		cfg.SyncTimeout = 180 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 10
	}
	return &ReleaseConvergenceLoop{
		releaseStore:   releaseStore,
		envTargetStore: envTargetStore,
		sync_:          syncClient,
		locker:         locker,
		eventStore:     eventStore,
		logger:         logger,
		cfg:            cfg,
		sem:            make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Start runs the convergence loop until context is cancelled.
func (r *ReleaseConvergenceLoop) Start(ctx context.Context) {
	r.logger.Info("release convergence loop started",
		"interval", r.cfg.Interval,
		"sync_timeout", r.cfg.SyncTimeout,
		"max_concurrent", r.cfg.MaxConcurrent)

	// Immediate check on boot
	r.tick(ctx)

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("release convergence loop stopped")
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *ReleaseConvergenceLoop) tick(ctx context.Context) {
	releases, err := r.releaseStore.FindPendingReleases(ctx)
	if err != nil {
		r.logger.Error("release convergence: failed to find pending releases", "error", err)
		return
	}

	for _, rel := range releases {
		if _, loaded := r.active.LoadOrStore(rel.ID, struct{}{}); loaded {
			continue
		}

		release := rel
		go func() {
			r.sem <- struct{}{}
			defer func() {
				<-r.sem
				r.active.Delete(release.ID)
			}()
			r.verifyRelease(ctx, &release)
		}()
	}
}

func (r *ReleaseConvergenceLoop) verifyRelease(ctx context.Context, release *db.Release) {
	// Check if release has exceeded sync timeout
	elapsed := time.Since(release.CreatedAt)
	if elapsed > r.cfg.SyncTimeout {
		r.logger.Warn("release convergence: sync timeout",
			"release_id", release.ID, "build_id", release.BuildID,
			"elapsed", elapsed, "timeout", r.cfg.SyncTimeout)
		r.failRelease(ctx, release, "Sync timeout: service did not become healthy within the time limit")
		return
	}

	// Check ArgoCD status
	argoAppName := fmt.Sprintf("%s-%s", release.TenantID, release.Environment)

	// Parse app specs to get deployed app names
	var apps []db.BuildAppSpec
	if release.Apps != nil {
		_ = json.Unmarshal(release.Apps, &apps)
	}
	var deployedAppNames []string
	for _, app := range apps {
		if app.Rendering == "static" {
			continue
		}
		deployedAppNames = append(deployedAppNames, app.Name)
	}

	synced, healthy, err := r.checkSync(ctx, argoAppName, deployedAppNames)
	if err != nil {
		r.logger.Warn("release convergence: sync check error",
			"release_id", release.ID, "error", err)
		return // Will retry on next tick
	}

	if !synced || !healthy {
		return // Not ready yet, will check on next tick
	}

	// Synced + healthy — activate the release
	r.activateRelease(ctx, release)
}

func (r *ReleaseConvergenceLoop) checkSync(ctx context.Context, appName string, deployedApps []string) (synced, healthy bool, err error) {
	// Use the SyncClient's WaitForSync with a very short timeout (just one check, no loop).
	// We poll externally via the convergence loop, so we don't want WaitForSync to block.
	// Instead, just check status once.

	// Type-assert to access getAppStatus if available, otherwise fall back to
	// a single-iteration WaitForSync with minimal timeout.
	type statusChecker interface {
		GetAppStatus(ctx context.Context, appName string, deployedApps []string) (bool, bool, error)
	}
	if checker, ok := r.sync_.(statusChecker); ok {
		return checker.GetAppStatus(ctx, appName, deployedApps)
	}

	// Fallback: use WaitForSync with 1-second timeout (one poll)
	err = r.sync_.WaitForSync(ctx, appName, 1*time.Second, deployedApps)
	if err != nil {
		return false, false, nil // timeout = not ready yet
	}
	return true, true, nil
}

func (r *ReleaseConvergenceLoop) activateRelease(ctx context.Context, release *db.Release) {
	err := r.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		// Transition PENDING → ACTIVE
		_, err := r.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncCompleted))
		if err != nil {
			return fmt.Errorf("activate release: %w", err)
		}

		// Supersede previous active release
		prev, _ := r.releaseStore.GetActiveByEnvTx(ctx, tx, release.TenantID, release.ProjectID, release.Environment)
		if prev != nil && prev.ID != release.ID {
			_, _ = r.releaseStore.UpdateStatus(ctx, tx, prev.ID, string(statemachine.ReleaseNewActivated))
		}

		// Update environment target
		if err := r.envTargetStore.SetActive(ctx, tx, release.TenantID, release.ProjectID, release.Environment, release.ID); err != nil {
			return fmt.Errorf("set active target: %w", err)
		}
		return nil
	})

	if err != nil {
		r.logger.Error("release convergence: activate failed",
			"release_id", release.ID, "error", err)
		return
	}

	r.logger.Info("release convergence: release activated",
		"release_id", release.ID, "build_id", release.BuildID)

	// Emit SSE events
	liveData, _ := json.Marshal(map[string]interface{}{
		"status":     "live",
		"release_id": release.ID,
	})
	if _, err := r.eventStore.Append(ctx, release.BuildID, "deploy.live", liveData); err != nil {
		r.logger.Error("failed to emit deploy.live event", "release_id", release.ID, "error", err)
	}
}

func (r *ReleaseConvergenceLoop) failRelease(ctx context.Context, release *db.Release, reason string) {
	err := r.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		_, err := r.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.ReleaseSyncFailed_))
		return err
	})

	if err != nil {
		r.logger.Error("release convergence: fail transition error",
			"release_id", release.ID, "error", err)
		return
	}

	r.logger.Warn("release convergence: release failed",
		"release_id", release.ID, "reason", reason)

	// Emit SSE event
	failedData, _ := json.Marshal(map[string]interface{}{
		"status": "failed",
		"reason": reason,
	})
	if _, err := r.eventStore.Append(ctx, release.BuildID, "deploy.sync_failed", failedData); err != nil {
		r.logger.Error("failed to emit deploy.sync_failed event", "release_id", release.ID, "error", err)
	}
}
