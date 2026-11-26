// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/events"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
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
	// CellID scopes the reaper to a single cell (roadmap 03). Empty means every
	// cell — how the API runs its loops (#447).
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
	State       statemachine.ReleasePhase
	Event       statemachine.ReleasePhaseEvent
	TenantID    string
	ProjectID   string
	Environment string
	BuildID     string
}

// ReleaseStaleChecker finds releases that have exceeded their state timeout.
type ReleaseStaleChecker interface {
	// FindStaleReleases returns stale releases belonging to the given cell, or to
	// every cell when cellID is empty.
	FindStaleReleases(ctx context.Context, cellID string, readyTimeout, pendingTimeout time.Duration) ([]StaleRelease, error)
}

// ReleaseReaperStore is the part of the release store the reaper uses:
// re-reading a stale release, transitioning it, and recording why (ISP —
// the full db.ReleaseStore has fifteen methods).
type ReleaseReaperStore interface {
	GetByID(ctx context.Context, id string) (*db.Release, error)
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
	AppendSideEffect(ctx context.Context, tx db.DBTX, id string, patch map[string]any) error
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
	releaseStore ReleaseReaperStore
	checker      ReleaseStaleChecker
	locker       deploycontracts.EnvironmentLocker
	eventStore   interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger *slog.Logger
	cfg    ReleaseReaperConfig
}

// NewReleaseReaper creates a new ReleaseReaper.
func NewReleaseReaper(
	releaseStore ReleaseReaperStore,
	checker ReleaseStaleChecker,
	locker deploycontracts.EnvironmentLocker,
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
//
// Beta Logs Gate Criterion 6 — same pattern as reaperReason in
// build_reaper.go: WHAT timed out + LIKELY CAUSE + NEXT STEP.
func releaseReaperReason(event statemachine.ReleasePhaseEvent) string {
	switch event {
	case statemachine.RPEventGitPushTimeout:
		return "Release was READY (deployment manifests prepared but not yet pushed) for more than 5 minutes. Likely cause: repository write is slow OR temporary network issue between API and the deployment repo. Next: run `loopscale deploy` to retry — the operation is idempotent."
	case statemachine.RPEventSyncTimeout:
		return "Release was PENDING (manifests pushed, waiting for service to become healthy) for more than 10 minutes. Likely cause: your app's /health endpoint is not responding 200 OR the pod is crash-looping. Next: run `loopscale logs --runtime --env <env> --app <name>` for runtime pod context; if health probe is correct, retry with `loopscale deploy`."
	default:
		return "Release reaped due to timeout. Run `loopscale status` and `loopscale logs --build <id>` for context."
	}
}

func (r *ReleaseReaper) reap(ctx context.Context) int {
	// Empty means every cell (backgroundloops.Deps); see ReleaseConvergenceLoop.cellID.
	stale, err := r.checker.FindStaleReleases(ctx, r.cfg.CellID, r.cfg.ReadyTimeout, r.cfg.PendingTimeout)
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
		if !statemachine.IsReleasePhaseReapable(statemachine.ReleasePhase(release.Status)) {
			continue
		}

		reason := releaseReaperReason(s.Event)

		err = r.locker.WithLock(ctx, s.TenantID, s.ProjectID, s.Environment, func(tx db.DBTX) error {
			// Re-fetch inside lock
			release, err = r.releaseStore.GetByID(ctx, s.ID)
			if err != nil {
				return err
			}
			if !statemachine.IsReleasePhaseReapable(statemachine.ReleasePhase(release.Status)) {
				return nil
			}

			_, err = r.releaseStore.UpdateStatus(ctx, tx, s.ID, string(s.Event))
			if err != nil {
				return err
			}

			// fix-all-platform-issues-plan T4.1: capture reaper transition reason
			// in side_effects for actionable RCA on future audits. Without this,
			// failed releases show only `status=GIT_MIRROR_FAILED` with no
			// indication of WHY (timeout? network? credentials?). The reason
			// string + timestamp + previous state form the audit trail.
			patch := map[string]any{
				"reaper_event":     string(s.Event),
				"reaper_reason":    reason,
				"reaper_from":      string(s.State),
				"reaper_at":        time.Now().UTC().Format(time.RFC3339),
				"reaper_timeout_s": int(r.cfg.ReadyTimeout.Seconds()),
			}
			if err := r.releaseStore.AppendSideEffect(ctx, tx, s.ID, patch); err != nil {
				// Non-fatal: status transition succeeded; side_effects audit
				// is supplementary. Log and continue.
				r.logger.Error("release reaper: failed to append side_effects audit",
					"release_id", s.ID, "error", err)
			}
			return nil
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
			case statemachine.RPEventGitPushTimeout:
				sseEventType = "deploy.git_push_failed"
			case statemachine.RPEventSyncTimeout:
				sseEventType = "deploy.sync_failed"
			default:
				sseEventType = "deploy.failed"
			}

			eventData := events.SafeJSONMarshal(map[string]string{
				"deploy_id":  deriveDeployID(s.ID),
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

// StaleReleaseRowReader is the persistence query behind ReleaseStaleChecker;
// *store.PostgresStaleScanner implements it.
type StaleReleaseRowReader interface {
	StaleReleaseRows(ctx context.Context, cellID string, readyBefore, pendingBefore time.Time) ([]store.StaleReleaseRow, error)
}

// NewStaleReleaseChecker adapts the stale-row query to ReleaseStaleChecker:
// it turns the timeouts into cutoffs and decides the timeout event per phase.
func NewStaleReleaseChecker(rows StaleReleaseRowReader) ReleaseStaleChecker {
	return &staleReleaseChecker{rows: rows, now: time.Now}
}

type staleReleaseChecker struct {
	rows StaleReleaseRowReader
	now  func() time.Time
}

// FindStaleReleases returns the releases of one cell (roadmap 03; empty means
// every cell) that exceeded their state timeout. READY times out with
// GitPushTimeout and PENDING with SyncTimeout; any other phase is skipped.
func (c *staleReleaseChecker) FindStaleReleases(ctx context.Context, cellID string, readyTimeout, pendingTimeout time.Duration) ([]StaleRelease, error) {
	now := c.now()
	rows, err := c.rows.StaleReleaseRows(ctx, cellID, now.Add(-readyTimeout), now.Add(-pendingTimeout))
	if err != nil {
		return nil, err
	}
	var result []StaleRelease
	for _, row := range rows {
		phase := statemachine.ReleasePhase(row.Status)
		var event statemachine.ReleasePhaseEvent
		switch phase {
		case statemachine.RPhReady:
			event = statemachine.RPEventGitPushTimeout
		case statemachine.RPhPending:
			event = statemachine.RPEventSyncTimeout
		default:
			continue
		}
		result = append(result, StaleRelease{
			ID: row.ID, State: phase, Event: event,
			TenantID: row.TenantID, ProjectID: row.ProjectID, Environment: row.Environment, BuildID: row.BuildID,
		})
	}
	return result, nil
}
