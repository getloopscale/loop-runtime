// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
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
	// domain.DefaultCellID means single-cell / backward-compatible behavior.
	CellID string
}

// DefaultBuildReaperConfig returns the per-state timeouts the build state
// machine declares (pkg/statemachine/build.go), so the machine stays the one
// owner of those budgets; deployments override them through BUILD_*_TIMEOUT.
func DefaultBuildReaperConfig() BuildReaperConfig {
	cfg := BuildReaperConfig{Interval: 30 * time.Second}
	for _, tc := range statemachine.NewBuildTypedMachine().Timeouts() {
		switch statemachine.BuildPhase(tc.State) {
		case statemachine.BPhQueued:
			cfg.QueueTimeout = tc.Timeout
		case statemachine.BPhRunning:
			cfg.RunningTimeout = tc.Timeout
		case statemachine.BPhReleasing:
			cfg.ReleasingTimeout = tc.Timeout
		case statemachine.BPhCancelling:
			cfg.CancelTimeout = tc.Timeout
		}
	}
	return cfg
}

// buildTimeoutEvents maps each state the build machine times out to the event
// the reaper fires when the budget is exceeded. Each pair is a legal machine
// edge (TestBuildTimeoutEvents_MatchTheMachineTimeouts), and this table is the
// only place the reaper encodes it.
var buildTimeoutEvents = map[statemachine.BuildPhase]statemachine.BuildPhaseEvent{
	statemachine.BPhQueued:     statemachine.BPEventQueueTimeout,
	statemachine.BPhRunning:    statemachine.BPEventTimeout,
	statemachine.BPhReleasing:  statemachine.BPEventReleaseTimeout,
	statemachine.BPhCancelling: statemachine.BPEventCancelTimeout,
}

// StaleBuild represents a build that has exceeded its timeout.
type StaleBuild struct {
	ID    string
	State statemachine.BuildPhase
	Event statemachine.BuildPhaseEvent
}

// BuildStaleChecker finds builds that have exceeded their state timeout.
type BuildStaleChecker interface {
	// FindStaleBuilds returns builds whose state has exceeded the given
	// timeout, scoped to the given cell. Roadmap 03: pass domain.DefaultCellID for
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
	locker     deploycontracts.EnvironmentLocker
	workflow   deploycontracts.WorkflowSubmitter
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	logger     *slog.Logger
	cfg        BuildReaperConfig
	reconciler BuildReconcileChecker // MOD-6: coordinate with reconcile loop
	// J-16 slice 3 (MEET-2026-07-04-001): deploy.failed outbox writer
	// (set via WithDeployEventStore). Nil-safe: emit skipped.
	deployEventStore store.DeployEventStore
}

// NewBuildReaper creates a new BuildReaper.
func NewBuildReaper(buildStore db.BuildStore, checker BuildStaleChecker, locker deploycontracts.EnvironmentLocker, workflow deploycontracts.WorkflowSubmitter, eventStore interface {
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

// WithDeployEventStore wires the deploy.failed outbox writer (J-16 slice 3,
// MEET-2026-07-04-001). Fluent setter so existing call sites/tests are
// unaffected; the composition root sets it in production. Nil = emit skipped.
func (r *BuildReaper) WithDeployEventStore(s store.DeployEventStore) *BuildReaper {
	r.deployEventStore = s
	return r
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

// reaperStuckPodReason returns the actionable operator message for a build
// that was reaped because its workflow pods were wedged in ImagePullBackOff
// / ErrImagePull / CreateContainerError past the RunningTimeout window.
// Red Team loopzero-cloud#133. The message MUST name the container image and
// point at the fix path (registry auth / image existence) rather than the
// generic RUNNING-timeout copy which mis-blames slow builds.
func reaperStuckPodReason(stuckPodReason string) string {
	return fmt.Sprintf("Build was stuck at container image pull for more than 30 minutes (%s). Likely cause: the container image does not exist OR the workflow does not have credentials to pull it (private registry auth). Next: check the workflow overlay `LOOPZERO_COSIGN_IMAGE` / `LOOPZERO_SYFT_IMAGE` / build-image tags point at a real, pullable image; verify the build cluster has registry credentials.", stuckPodReason)
}

// reaperReason returns a human-readable reason for a reaper timeout event.
// LEVE-1: Single source of truth for reaper reason strings — eliminates DRY violation.
//
// Beta Logs Gate Criterion 6 — when a state-machine timeout fires, the
// CLI exit message MUST include a likely cause so the operator has a
// starting point for diagnosis. Each branch returns:
//   - WHAT timed out (state + the budget cfg actually applied — the budgets
//     are configurable, so a hard-coded default would misreport)
//   - LIKELY CAUSE (top 1-2 hypotheses observed in dogfood)
//   - NEXT STEP (specific CLI command to run)
func reaperReason(event statemachine.BuildPhaseEvent, cfg BuildReaperConfig) string {
	switch event {
	case statemachine.BPEventQueueTimeout:
		return "Build was QUEUED for more than " + humanBudget(cfg.QueueTimeout) + ". Likely cause: build cluster is at capacity OR a previous build is still using the per-tenant concurrency slot. Next: run `loopzero status` to see pending builds; consider canceling a stuck build with `loopzero cancel`."
	case statemachine.BPEventTimeout:
		return "Build was RUNNING for more than " + humanBudget(cfg.RunningTimeout) + ". Likely cause: image build step is slow (large layers, npm install resolving many packages) OR the build container is hung on a network call. Next: run `loopzero logs --build <id>` and check the LAST log line — that's the step where the build stalled."
	case statemachine.BPEventReleaseTimeout:
		return "Build was RELEASING (image built, release activation in flight) for more than " + humanBudget(cfg.ReleasingTimeout) + ". Likely cause: deployment sync is slow OR the workload cluster is unhealthy. Next: run `loopzero status` to see if the release became active; if not, retry with `loopzero deploy`."
	case statemachine.BPEventCancelTimeout:
		return "Cancellation did not complete within " + humanBudget(cfg.CancelTimeout) + ". The build pod may need manual cleanup. Next: run `loopzero status` to confirm the build moved to CANCELLED; if still CANCELLING, contact support."
	default:
		return "Build reaped due to timeout. Run `loopzero logs --build <id>` for context."
	}
}

// humanBudget renders a timeout for the user: "60s" below two minutes, "N
// minutes" for whole minutes, Go's duration syntax otherwise.
func humanBudget(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d%time.Minute == 0:
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	default:
		return d.String()
	}
}

func (r *BuildReaper) reap(ctx context.Context) int {
	// Empty means every cell (backgroundloops.Deps) — how the API runs its loops (#450).
	stale, err := r.checker.FindStaleBuilds(ctx, r.cfg.CellID, r.cfg.QueueTimeout, r.cfg.RunningTimeout, r.cfg.ReleasingTimeout, r.cfg.CancelTimeout)
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
		if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
			continue
		}

		// M-03: Verify actual Argo Workflow state before reaping.
		// If the workflow is still actively running, do NOT reap — the build is
		// legitimately slow, not stuck. Only reap when the workflow is gone,
		// failed, or we can't determine its state.
		//
		// Red Team loopzero-cloud#133 override: when the workflow reports Phase=Running
		// but the underlying pod is wedged in ImagePullBackOff / ErrImagePull /
		// CreateContainerError past the RunningTimeout window, the workflow will
		// never make progress. Reap with a stuck-pod-shaped reason so the operator
		// sees "container image can't be pulled" instead of the generic timeout copy.
		var stuckPodReason string
		if build.WorkflowID != "" && r.workflow != nil {
			wfStatus, wfErr := r.workflow.GetStatus(ctx, "argo", build.WorkflowID)
			if wfErr != nil {
				r.logger.Warn("build reaper: cannot verify workflow state, proceeding with reap",
					"build_id", s.ID, "workflow_id", build.WorkflowID, "error", wfErr)
			} else if wfStatus != nil && (wfStatus.Phase == "Running" || wfStatus.Phase == "Pending") {
				if wfStatus.StuckPodReason != "" {
					// #133: pod-level stuck-state override — reap with a pod-shaped reason.
					stuckPodReason = wfStatus.StuckPodReason
					r.logger.Warn("build reaper: workflow pod stuck in image-pull failure — reaping",
						"build_id", s.ID, "workflow_id", build.WorkflowID, "phase", wfStatus.Phase,
						"stuck_pod_reason", stuckPodReason)
				} else {
					r.logger.Warn("build reaper: workflow still active despite timeout — skipping reap",
						"build_id", s.ID, "workflow_id", build.WorkflowID, "phase", wfStatus.Phase,
						"state", s.State, "timeout_event", s.Event)
					continue
				}
			}
		}

		var reason string
		if stuckPodReason != "" {
			reason = reaperStuckPodReason(stuckPodReason)
		} else {
			reason = reaperReason(s.Event, r.cfg)
		}

		timeoutEvent := s.Event
		transitioned, err := r.finalizer().fail(ctx, build, buildFailure{
			reason: reason,
			route: func(statemachine.BuildPhase) ([]statemachine.BuildPhaseEvent, error) {
				return []statemachine.BuildPhaseEvent{timeoutEvent}, nil
			},
			// A cancel-timeout reaps to CANCELLED, which is not a failed deploy
			// (the customer cancelled), so deploy.failed is not staged.
			stageDeployFailed: timeoutEvent != statemachine.BPEventCancelTimeout,
		})
		if err != nil {
			r.logger.Error("build reaper: failed to transition build",
				"build_id", s.ID, "from", s.State, "event", s.Event, "error", err)
			continue
		}
		if !transitioned {
			continue // finalized by another worker since the stale query
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

// StaleBuildRowReader is the persistence query behind BuildStaleChecker;
// *store.PostgresStaleScanner implements it.
type StaleBuildRowReader interface {
	StaleBuildRows(ctx context.Context, cellID string, cutoffs store.StaleBuildCutoffs) ([]store.StaleBuildRow, error)
}

// NewStaleBuildChecker adapts the stale-row query to BuildStaleChecker: it
// turns the phase timeouts into cutoffs and keeps only phases that time out.
func NewStaleBuildChecker(rows StaleBuildRowReader) BuildStaleChecker {
	return &staleBuildChecker{rows: rows, now: time.Now}
}

type staleBuildChecker struct {
	rows StaleBuildRowReader
	now  func() time.Time
}

// FindStaleBuilds returns the builds of one cell (roadmap 03; empty means
// domain.DefaultCellID) that exceeded their phase timeout, with the event each
// phase times out through (buildTimeoutEvents).
func (c *staleBuildChecker) FindStaleBuilds(ctx context.Context, cellID string, queueTimeout, runningTimeout, releasingTimeout, cancelTimeout time.Duration) ([]StaleBuild, error) {
	now := c.now()
	rows, err := c.rows.StaleBuildRows(ctx, cellID, store.StaleBuildCutoffs{
		QueuedBefore:     now.Add(-queueTimeout),
		RunningBefore:    now.Add(-runningTimeout),
		ReleasingBefore:  now.Add(-releasingTimeout),
		CancellingBefore: now.Add(-cancelTimeout),
	})
	if err != nil {
		return nil, err
	}
	var result []StaleBuild
	for _, row := range rows {
		phase := statemachine.BuildPhase(row.Status)
		event, timesOut := buildTimeoutEvents[phase]
		if !timesOut {
			continue
		}
		result = append(result, StaleBuild{ID: row.ID, State: phase, Event: event})
	}
	return result, nil
}

// finalizer takes the reaper's builds to a terminal phase.
func (r *BuildReaper) finalizer() buildFinalizer {
	return buildFinalizer{builds: r.buildStore, locker: r.locker, sse: r.eventStore,
		deployEvents: r.deployEventStore, logger: r.logger}
}
