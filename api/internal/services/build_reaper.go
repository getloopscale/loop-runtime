// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
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

// BuildReaperConfig configures the build reaper thresholds.
type BuildReaperConfig struct {
	// QueueTimeout is how long a build can stay in QUEUED before being reaped (spec §5.1: 5min).
	QueueTimeout time.Duration
	// RunningTimeout is how long a build can stay in RUNNING before being reaped (spec §5.1: 30min).
	RunningTimeout time.Duration
	// ReleasingTimeout is how long a build can stay in RELEASING before being reaped (MEET-008: 10min).
	ReleasingTimeout time.Duration
	// CancelTimeout is how long a build can stay in CANCELLING before being reaped (spec §5.1: 60s).
	CancelTimeout time.Duration
	// Interval is how often the reaper runs.
	Interval time.Duration
	// CellID scopes the reaper to a single cell (roadmap 03). Empty or
	// "cell-default" means single-cell / backward-compatible behavior.
	CellID string
}

// DefaultBuildReaperConfig returns the spec-defined timeouts.
func DefaultBuildReaperConfig() BuildReaperConfig {
	return BuildReaperConfig{
		QueueTimeout:     5 * time.Minute,
		RunningTimeout:   30 * time.Minute,
		ReleasingTimeout: 10 * time.Minute,
		CancelTimeout:    60 * time.Second,
		Interval:         30 * time.Second,
	}
}

// StaleBuild represents a build that has exceeded its timeout.
type StaleBuild struct {
	ID    string
	State statemachine.State
	Event statemachine.Event
}

// BuildStaleChecker finds builds that have exceeded their state timeout.
type BuildStaleChecker interface {
	// FindStaleBuilds returns builds whose state has exceeded the given
	// timeout, scoped to the given cell. Roadmap 03: pass db.CellDefault for
	// single-cell / backward-compatible behavior.
	FindStaleBuilds(ctx context.Context, cellID string, queueTimeout, runningTimeout, releasingTimeout, cancelTimeout time.Duration) ([]StaleBuild, error)
}

// BuildReconcileChecker allows the reaper to check if a build is being reconciled.
// MOD-6: Coordinates BuildReaper with BuildReconcileLoop to avoid competing on the same build.
type BuildReconcileChecker interface {
	IsProcessing(buildID string) bool
}

// BuildReaper periodically checks for stale builds and transitions them to FAILED/CANCELLED.
// This implements the timeout enforcement from spec §5.1.
//
// M-03 fix: Before marking a build as FAILED, the reaper verifies the actual
// Argo Workflow status. If the workflow is still Running, the build is NOT reaped —
// only a warning is logged. This prevents false positives where a legitimately slow
// build is incorrectly marked FAILED while it's still producing artifacts.
type BuildReaper struct {
	buildStore db.BuildStore
	checker    BuildStaleChecker
	locker     EnvironmentLocker
	workflow   WorkflowSubmitter
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger     *slog.Logger
	cfg        BuildReaperConfig
	reconciler BuildReconcileChecker // MOD-6: coordinate with reconcile loop
}

// NewBuildReaper creates a new BuildReaper.
func NewBuildReaper(buildStore db.BuildStore, checker BuildStaleChecker, locker EnvironmentLocker, workflow WorkflowSubmitter, eventStore interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
}, logger *slog.Logger, cfg BuildReaperConfig, reconciler BuildReconcileChecker) *BuildReaper {
	return &BuildReaper{
		buildStore: buildStore,
		checker:    checker,
		locker:     locker,
		workflow:   workflow,
		eventStore: eventStore,
		logger:     logger,
		cfg:        cfg,
		reconciler: reconciler,
	}
}

// Start runs the reaper loop until the context is cancelled.
func (r *BuildReaper) Start(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	r.logger.Info("build reaper started",
		"queue_timeout", r.cfg.QueueTimeout,
		"running_timeout", r.cfg.RunningTimeout,
		"releasing_timeout", r.cfg.ReleasingTimeout,
		"cancel_timeout", r.cfg.CancelTimeout,
		"interval", r.cfg.Interval)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("build reaper stopped")
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

// ReapOnce runs a single reaper cycle. Exported for testing.
func (r *BuildReaper) ReapOnce(ctx context.Context) int {
	return r.reap(ctx)
}

// reaperReason returns a human-readable reason for a reaper timeout event.
// LEVE-1: Single source of truth for reaper reason strings — eliminates DRY violation.
func reaperReason(event statemachine.Event) string {
	switch event {
	case statemachine.BuildQueueTimeout:
		return "Build timed out while waiting in queue (5 min limit)"
	case statemachine.BuildTimeout:
		return "Build timed out while running (30 min limit)"
	case statemachine.BuildCancelTimeout:
		return "Build cancellation timed out (60s limit)"
	default:
		return "Build reaped due to timeout"
	}
}

func (r *BuildReaper) reap(ctx context.Context) int {
	cellID := r.cfg.CellID
	if cellID == "" {
		cellID = db.CellDefault
	}
	stale, err := r.checker.FindStaleBuilds(ctx, cellID, r.cfg.QueueTimeout, r.cfg.RunningTimeout, r.cfg.ReleasingTimeout, r.cfg.CancelTimeout)
	if err != nil {
		r.logger.Error("build reaper: failed to find stale builds", "error", err)
		return 0
	}

	reaped := 0
	for _, s := range stale {
		// MOD-6: Skip builds currently being processed by reconcile loop
		if r.reconciler != nil && r.reconciler.IsProcessing(s.ID) {
			r.logger.Debug("build reaper: skipping build being reconciled", "build_id", s.ID)
			continue
		}

		build, err := r.buildStore.GetByID(ctx, s.ID)
		if err != nil {
			r.logger.Error("build reaper: failed to get build", "build_id", s.ID, "error", err)
			continue
		}

		// Re-check: build may have transitioned since the query
		if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			continue
		}

		// M-03: Verify actual Argo Workflow state before reaping.
		// If the workflow is still actively running, do NOT reap — the build is
		// legitimately slow, not stuck. Only reap when the workflow is gone,
		// failed, or we can't determine its state.
		if build.WorkflowID != "" && r.workflow != nil {
			wfStatus, wfErr := r.workflow.GetStatus(ctx, "argo", build.WorkflowID)
			if wfErr != nil {
				r.logger.Warn("build reaper: cannot verify workflow state, proceeding with reap",
					"build_id", s.ID, "workflow_id", build.WorkflowID, "error", wfErr)
			} else if wfStatus != nil && (wfStatus.Phase == "Running" || wfStatus.Phase == "Pending") {
				r.logger.Warn("build reaper: workflow still active despite timeout — skipping reap",
					"build_id", s.ID, "workflow_id", build.WorkflowID, "phase", wfStatus.Phase,
					"state", s.State, "timeout_event", s.Event)
				continue
			}
		}

		reason := reaperReason(s.Event)

		err = r.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
			// Re-fetch inside lock
			build, err = r.buildStore.GetByID(ctx, s.ID)
			if err != nil {
				return err
			}
			if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
				return nil
			}

			_, err = r.buildStore.UpdateStatus(ctx, tx, s.ID, string(s.Event))
			if err != nil {
				return err
			}

			// Set descriptive error message (F-04: reaped builds must explain why)
			return r.buildStore.SetErrorMessage(ctx, tx, s.ID, reason)
		})

		if err != nil {
			r.logger.Error("build reaper: failed to transition build",
				"build_id", s.ID, "from", s.State, "event", s.Event, "error", err)
			continue
		}

		// MOD-5: Emit SSE build.failed so the CLI gets notified immediately
		// instead of waiting for the deploy timeout (10 minutes).
		if r.eventStore != nil {
			eventData := safeJSONMarshal(map[string]string{
				"build_id": s.ID,
				"status":   "failed",
				"error":    reason,
			})
			if _, err := r.eventStore.Append(ctx, s.ID, "build.failed", eventData); err != nil {
				r.logger.Error("build reaper: failed to emit SSE event",
					"build_id", s.ID, "error", err)
			}
		}

		r.logger.Warn("build reaper: reaped stale build",
			"build_id", s.ID, "from_state", s.State, "event", s.Event)
		reaped++
	}

	if reaped > 0 {
		r.logger.Info("build reaper: cycle complete", "reaped", reaped)
	}
	return reaped
}

// PostgresStaleBuildChecker implements BuildStaleChecker using PostgreSQL.
type PostgresStaleBuildChecker struct {
	DB db.DBTX
}

// FindStaleBuilds queries for builds that have exceeded their state timeout,
// scoped to the given cell (roadmap 03). Pass db.CellDefault for backward
// compatibility with single-cell deployments.
func (c *PostgresStaleBuildChecker) FindStaleBuilds(ctx context.Context, cellID string, queueTimeout, runningTimeout, releasingTimeout, cancelTimeout time.Duration) ([]StaleBuild, error) {
	if cellID == "" {
		cellID = db.CellDefault
	}
	// MEET-2026-03-26-008: RELEASING added — builds stuck in release activation
	// are reaped after 10min (default) to prevent infinite retry.
	// cell_id filter (migration 000029) uses idx_builds_cell_status_created.
	query := `
		SELECT id, status FROM builds
		WHERE cell_id = $5
		  AND ((status = 'QUEUED' AND created_at < $1)
		    OR (status = 'RUNNING' AND started_at < $2)
		    OR (status = 'RELEASING' AND started_at < $3)
		    OR (status = 'CANCELLING' AND started_at < $4))
		ORDER BY created_at ASC
		LIMIT 100
	`

	now := time.Now()
	rows, err := c.DB.Query(ctx, query,
		now.Add(-queueTimeout),
		now.Add(-runningTimeout),
		now.Add(-releasingTimeout),
		now.Add(-cancelTimeout),
		cellID,
	)
	if err != nil {
		return nil, fmt.Errorf("find stale builds: %w", err)
	}
	defer rows.Close()

	var result []StaleBuild
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("scan stale build: %w", err)
		}

		var event statemachine.Event
		switch statemachine.State(status) {
		case statemachine.BuildQueued:
			event = statemachine.BuildQueueTimeout
		case statemachine.BuildRunning:
			event = statemachine.BuildTimeout
		case statemachine.BuildReleasing:
			event = statemachine.BuildReleaseTimeoutEvent
		case statemachine.BuildCancelling:
			event = statemachine.BuildCancelTimeout
		default:
			continue
		}

		result = append(result, StaleBuild{
			ID:    id,
			State: statemachine.State(status),
			Event: event,
		})
	}

	return result, rows.Err()
}
