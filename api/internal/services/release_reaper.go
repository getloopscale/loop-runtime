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
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// ReleaseReaperConfig configures the release reaper thresholds.
type ReleaseReaperConfig struct {
	// ReadyTimeout is how long a release can stay READY before being reaped (spec §5.2: 5min).
	ReadyTimeout time.Duration
	// PendingTimeout is how long a release can stay PENDING before being reaped.
	// MEET-2026-04-04-029: Must be strictly > convergence SyncTimeout (300s).
	PendingTimeout time.Duration
	// Interval is how often the reaper runs.
	Interval time.Duration
	// CellID scopes the reaper to a single cell (roadmap 03). Empty or
	// "cell-default" means single-cell / backward-compatible behavior.
	CellID string
}

// DefaultReleaseReaperConfig returns the spec-defined timeouts.
func DefaultReleaseReaperConfig() ReleaseReaperConfig {
	return ReleaseReaperConfig{
		ReadyTimeout:   statemachine.ReleaseReadyTimeout,
		PendingTimeout: statemachine.ReleasePendingTimeout,
		Interval:       30 * time.Second,
	}
}

// StaleRelease represents a release that has exceeded its timeout.
type StaleRelease struct {
	ID          string
	State       statemachine.State
	Event       statemachine.Event
	TenantID    string
	ProjectID   string
	Environment string
	BuildID     string
}

// ReleaseStaleChecker finds releases that have exceeded their state timeout.
type ReleaseStaleChecker interface {
	// FindStaleReleases returns stale releases belonging to the given cell.
	// Roadmap 03: pass db.CellDefault for single-cell / backward-compatible
	// behavior.
	FindStaleReleases(ctx context.Context, cellID string, readyTimeout, pendingTimeout time.Duration) ([]StaleRelease, error)
}

// ReleaseReaper periodically checks for stale releases and transitions them to failure states.
// This implements the timeout enforcement from spec §5.2.
//
// - Releases stuck in READY past ReadyTimeout are transitioned via GitPushTimeout → GIT_MIRROR_FAILED
// - Releases stuck in PENDING past PendingTimeout are transitioned via SyncTimeout → SYNC_FAILED
//
// Unlike BuildReaper, the ReleaseReaper does not need to check external workflow status —
// it only checks database state.
type ReleaseReaper struct {
	releaseStore db.ReleaseStore
	checker      ReleaseStaleChecker
	locker       EnvironmentLocker
	eventStore   interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger *slog.Logger
	cfg    ReleaseReaperConfig
}

// NewReleaseReaper creates a new ReleaseReaper.
func NewReleaseReaper(
	releaseStore db.ReleaseStore,
	checker ReleaseStaleChecker,
	locker EnvironmentLocker,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	logger *slog.Logger,
	cfg ReleaseReaperConfig,
) *ReleaseReaper {
	return &ReleaseReaper{
		releaseStore: releaseStore,
		checker:      checker,
		locker:       locker,
		eventStore:   eventStore,
		logger:       logger,
		cfg:          cfg,
	}
}

// Start runs the reaper loop until the context is cancelled.
func (r *ReleaseReaper) Start(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	r.logger.Info("release reaper started",
		"ready_timeout", r.cfg.ReadyTimeout,
		"pending_timeout", r.cfg.PendingTimeout,
		"interval", r.cfg.Interval)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("release reaper stopped")
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

// ReapOnce runs a single reaper cycle. Exported for testing.
func (r *ReleaseReaper) ReapOnce(ctx context.Context) int {
	return r.reap(ctx)
}

// releaseReaperReason returns a human-readable reason for a reaper timeout event.
func releaseReaperReason(event statemachine.Event) string {
	switch event {
	case statemachine.ReleaseGitPushTimeout:
		return "Deploy timed out preparing deployment (5 min limit). Run `loopzero deploy` to retry."
	case statemachine.ReleaseSyncTimeout:
		return "Deploy timed out waiting for service to become healthy (10 min limit). Run `loopzero deploy` to retry."
	default:
		return "Release reaped due to timeout"
	}
}

func (r *ReleaseReaper) reap(ctx context.Context) int {
	cellID := r.cfg.CellID
	if cellID == "" {
		cellID = db.CellDefault
	}
	stale, err := r.checker.FindStaleReleases(ctx, cellID, r.cfg.ReadyTimeout, r.cfg.PendingTimeout)
	if err != nil {
		r.logger.Error("release reaper: failed to find stale releases", "error", err)
		return 0
	}

	reaped := 0
	for _, s := range stale {
		release, err := r.releaseStore.GetByID(ctx, s.ID)
		if err != nil {
			r.logger.Error("release reaper: failed to get release", "release_id", s.ID, "error", err)
			continue
		}

		// Re-check: release may have transitioned since the query
		if !statemachine.IsReleaseReapable(statemachine.State(release.Status)) {
			continue
		}

		reason := releaseReaperReason(s.Event)

		err = r.locker.WithLock(ctx, s.TenantID, s.ProjectID, s.Environment, func(tx db.DBTX) error {
			// Re-fetch inside lock
			release, err = r.releaseStore.GetByID(ctx, s.ID)
			if err != nil {
				return err
			}
			if !statemachine.IsReleaseReapable(statemachine.State(release.Status)) {
				return nil
			}

			_, err = r.releaseStore.UpdateStatus(ctx, tx, s.ID, string(s.Event))
			return err
		})

		if err != nil {
			r.logger.Error("release reaper: failed to transition release",
				"release_id", s.ID, "from", s.State, "event", s.Event, "error", err)
			continue
		}

		// Emit SSE event so the CLI gets notified immediately
		if r.eventStore != nil {
			var sseEventType string
			switch s.Event {
			case statemachine.ReleaseGitPushTimeout:
				sseEventType = "deploy.git_push_failed"
			case statemachine.ReleaseSyncTimeout:
				sseEventType = "deploy.sync_failed"
			default:
				sseEventType = "deploy.failed"
			}

			eventData := safeJSONMarshal(map[string]string{
				"deploy_id":  deriveDeployIDFromRelease(s.ID),
				"release_id": s.ID,
				"status":     "failed",
				"error":      reason,
			})
			if _, err := r.eventStore.Append(ctx, s.BuildID, sseEventType, eventData); err != nil {
				r.logger.Error("release reaper: failed to emit SSE event",
					"release_id", s.ID, "error", err)
			}
		}

		r.logger.Warn("release reaper: reaped stale release",
			"release_id", s.ID, "from_state", s.State, "event", s.Event)
		reaped++
	}

	if reaped > 0 {
		r.logger.Info("release reaper: cycle complete", "reaped", reaped)
	}
	return reaped
}

// deriveDeployIDFromRelease creates a deploy_id from a release ID.
// Uses the same logic as deriveDeployID in release_activator.go.
func deriveDeployIDFromRelease(releaseID string) string {
	hex := strings.TrimPrefix(releaseID, "rel-")
	if len(hex) >= 8 {
		return "d-" + hex[:8]
	}
	return "d-" + hex
}

// PostgresStaleReleaseChecker implements ReleaseStaleChecker using PostgreSQL.
type PostgresStaleReleaseChecker struct {
	DB db.DBTX
}

// FindStaleReleases queries for releases that have exceeded their state timeout,
// scoped to the given cell (roadmap 03). Pass db.CellDefault for backward
// compatibility with single-cell deployments.
func (c *PostgresStaleReleaseChecker) FindStaleReleases(ctx context.Context, cellID string, readyTimeout, pendingTimeout time.Duration) ([]StaleRelease, error) {
	if cellID == "" {
		cellID = db.CellDefault
	}
	// cell_id filter uses idx_releases_cell_status_created (migration 000029).
	query := `
		SELECT id, status, tenant_id, project_id, environment, build_id FROM releases
		WHERE cell_id = $3
		  AND ((status = 'READY' AND created_at < $1)
		    OR (status = 'PENDING' AND created_at < $2))
		ORDER BY created_at ASC
		LIMIT 100
	`

	now := time.Now()
	rows, err := c.DB.Query(ctx, query,
		now.Add(-readyTimeout),
		now.Add(-pendingTimeout),
		cellID,
	)
	if err != nil {
		return nil, fmt.Errorf("find stale releases: %w", err)
	}
	defer rows.Close()

	var result []StaleRelease
	for rows.Next() {
		var id, status, tenantID, projectID, environment, buildID string
		if err := rows.Scan(&id, &status, &tenantID, &projectID, &environment, &buildID); err != nil {
			return nil, fmt.Errorf("scan stale release: %w", err)
		}

		var event statemachine.Event
		switch statemachine.State(status) {
		case statemachine.ReleaseReady:
			event = statemachine.ReleaseGitPushTimeout
		case statemachine.ReleasePending:
			event = statemachine.ReleaseSyncTimeout
		default:
			continue
		}

		result = append(result, StaleRelease{
			ID:          id,
			State:       statemachine.State(status),
			Event:       event,
			TenantID:    tenantID,
			ProjectID:   projectID,
			Environment: environment,
			BuildID:     buildID,
		})
	}

	return result, rows.Err()
}
