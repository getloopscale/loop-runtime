// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getloopscale/loopzero/api/internal/processing"
	"github.com/getloopscale/loopzero/pkg/naming"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/events"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// ReleaseConvergenceLoopConfig configures the release convergence loop.
type ReleaseConvergenceLoopConfig struct {
	// Interval is how often the loop polls for pending releases.
	Interval time.Duration
	// HealthInterval is how often the loop checks ACTIVE releases against ArgoCD health.
	// MEET-2026-03-31-023 T1-4: Post-activation health monitoring.
	// Defaults to 60 seconds if not set.
	HealthInterval time.Duration
	// SyncTimeout is how long to wait for ArgoCD sync before marking SYNC_FAILED.
	SyncTimeout time.Duration
	// MaxConcurrent bounds parallel release verifications.
	MaxConcurrent int
	// CellID scopes the loop to a single cell (roadmap 03). Empty means every
	// cell — how the API runs its loops (#447).
	CellID string
}

// ReleaseConvergenceStore defines the release store methods needed by the loop.
type ReleaseConvergenceStore interface {
	// FindPendingReleases is scoped to the loop's cell (roadmap 03).
	FindPendingReleases(ctx context.Context, cellID string) ([]db.Release, error)
	// FindActiveReleases returns releases in ACTIVE state for post-activation health monitoring,
	// scoped to the loop's cell (roadmap 03).
	// MEET-2026-03-31-023 T1-4: Used by the health check ticker to detect divergence.
	FindActiveReleases(ctx context.Context, cellID string) ([]db.Release, error)
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
	GetActiveByEnvTx(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (*db.Release, error)
	GetByIDTx(ctx context.Context, tx db.DBTX, id string) (*db.Release, error)
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
	envTargetStore store.EnvironmentTargetStore
	sync_          deploycontracts.SyncClient
	locker         deploycontracts.EnvironmentLocker
	eventStore     interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	}
	// J-16 slice 3: transactional-outbox writer (set via WithDeployEventStore).
	// Nil-safe: ActivationKernel.Activate skips the deploy.succeeded emit when nil.
	deployEventStore store.DeployEventStore
	baseDomain       string
	publicDomain     string // release-blockers-2026-06-25-set v1.1 #61 — canonical wildcard for deploy.completed URL (mirrors SyncCallbackActivator.publicDomain).
	logger           *slog.Logger
	cfg              ReleaseConvergenceLoopConfig

	sem    chan struct{}
	active sync.Map

	// pgDedup is the optional PG-backed dedup layer (Phase 0.2,
	// scale-50k-plan). Same pattern as BuildReconcileLoop.
	pgDedup *processing.PGClaimDedup

	// MEET-2026-03-31-023 T1-5: track sync retrigger attempts per release.
	// Prevents infinite retry loops (max 3 retriggers per release).
	syncRetriggers sync.Map // releaseID → int
}

// NewReleaseConvergenceLoop creates the convergence loop.
func NewReleaseConvergenceLoop(
	releaseStore ReleaseConvergenceStore,
	envTargetStore store.EnvironmentTargetStore,
	syncClient deploycontracts.SyncClient,
	locker deploycontracts.EnvironmentLocker,
	eventStore interface {
		Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	},
	baseDomain, publicDomain string,
	logger *slog.Logger,
	cfg ReleaseConvergenceLoopConfig,
) *ReleaseConvergenceLoop {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.SyncTimeout <= 0 {
		cfg.SyncTimeout = 180 * time.Second
	}
	if cfg.HealthInterval <= 0 {
		cfg.HealthInterval = 60 * time.Second
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
		baseDomain:     baseDomain,
		publicDomain:   publicDomain,
		logger:         logger,
		cfg:            cfg,
		sem:            make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Quality-Gate Phase 2: WithPGClaims removed — fluent setter was never
// invoked; PG dedup is wired via constructor when needed.

// WithDeployEventStore wires the transactional-outbox writer (J-16 slice 3).
// Fluent setter (not a constructor param) so existing call sites/tests are
// unaffected; the composition root sets it in production. Nil = outbox skipped.
func (r *ReleaseConvergenceLoop) WithDeployEventStore(s store.DeployEventStore) *ReleaseConvergenceLoop {
	r.deployEventStore = s
	return r
}

// Start runs the convergence loop until context is cancelled.
func (r *ReleaseConvergenceLoop) Start(ctx context.Context) {
	r.logger.Info("release convergence loop started",
		"interval", r.cfg.Interval,
		"health_interval", r.cfg.HealthInterval,
		"sync_timeout", r.cfg.SyncTimeout,
		"max_concurrent", r.cfg.MaxConcurrent)

	// Immediate check on boot
	r.tick(ctx)

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	// MEET-2026-03-31-023 T1-4: Separate ticker for ACTIVE release health monitoring.
	healthTicker := time.NewTicker(r.cfg.HealthInterval)
	defer healthTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("release convergence loop stopped")
			return
		case <-ticker.C:
			r.tick(ctx)
		case <-healthTicker.C:
			r.healthCheckActiveReleases(ctx)
		}
	}
}

// cellID is the cell scope passed to the store: empty means every cell, as
// backgroundloops.Deps documents. It used to become cell-0, and the API — which runs
// the loops with no cell — never activated a release on any other cell (#447).
func (r *ReleaseConvergenceLoop) cellID() string {
	return r.cfg.CellID
}

func (r *ReleaseConvergenceLoop) tick(ctx context.Context) {
	// T4.3 of deep-review-2026-05-10-remediation-plan (find_obs_metric_premature MEDIUM):
	// `ReconcileLoopLastSuccess` was being set BEFORE the DB query — so
	// the "last successful reconcile" gauge updated even when the query
	// failed seconds later. Now we only set it AFTER the query returns
	// successfully (real evidence the loop reached the reconcile pass).
	releases, err := r.releaseStore.FindPendingReleases(ctx, r.cellID())
	if err != nil {
		r.logger.Error("release convergence: failed to find pending releases", "error", err)
		return
	}

	metrics.ReconcileLoopLastSuccess.WithLabelValues("release").SetToCurrentTime()
	metrics.ReleasesPending.Set(float64(len(releases)))

	for _, rel := range releases {
		if _, loaded := r.active.LoadOrStore(rel.ID, struct{}{}); loaded {
			continue
		}

		// Phase 0.2: cross-replica dedup via PG claims.
		if r.pgDedup != nil {
			claimed, err := r.pgDedup.TryClaim(ctx, "releases", rel.ID)
			if err != nil {
				r.logger.Warn("release convergence: release claim failed; retrying next tick",
					"release_id", rel.ID, "error", err)
			}
			if !claimed {
				r.active.Delete(rel.ID)
				continue
			}
		}

		release := rel
		// Acquire semaphore BEFORE spawning goroutine to prevent unbounded
		// goroutine accumulation when all slots are busy.
		select {
		case r.sem <- struct{}{}:
			go func() {
				defer func() {
					<-r.sem
					r.active.Delete(release.ID)
					if r.pgDedup != nil {
						// T0.2 of deep-review-2026-05-10-remediation-plan
						// (CRITICAL): use independent context for cleanup so
						// graceful shutdown (parent ctx cancelled) cannot
						// orphan the pgDedup claim. 5s is generous for a
						// single DELETE against PG.
						cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cleanupCancel()
						r.pgDedup.Release(cleanupCtx, "releases", release.ID)
					}
				}()
				r.verifyRelease(ctx, &release)
			}()
		default:
			// All semaphore slots busy — skip this release, next tick will retry.
			r.active.Delete(release.ID)
		}
	}
}

// verifyRelease advances one PENDING release: fail it when the sync timed out,
// nudge a sync that stalled, and activate it once ArgoCD reports it synced,
// healthy and at the release's own revision.
func (r *ReleaseConvergenceLoop) verifyRelease(ctx context.Context, release *db.Release) {
	elapsed := time.Since(release.CreatedAt)
	argoAppName, nameErr := naming.ArgoAppName(release.TenantID, release.ProjectID, release.Environment)
	if nameErr != nil {
		// Syncing, terminating or failing the wrong Application is worse than
		// doing nothing: fail loud and let the next pass retry once the release
		// carries a project (rules/error-handling.md § 2).
		r.logger.Error("release convergence: argocd application name",
			"tenant", release.TenantID, "project", release.ProjectID,
			"environment", release.Environment, "error", nameErr)
		return
	}
	if elapsed > r.cfg.SyncTimeout {
		r.handleSyncTimeout(ctx, release, argoAppName, elapsed)
		return
	}

	synced, healthy, err := r.checkSync(ctx, argoAppName, r.deployedAppNames(release))
	if err != nil {
		r.logger.Warn("release convergence: sync check error",
			"release_id", release.ID, "error", err)
		return // Will retry on next tick
	}
	if !synced || !healthy {
		if !synced {
			r.recoverStalledSync(ctx, release, argoAppName, elapsed)
		}
		return // Not ready yet, will check on next tick
	}
	if !r.revisionMatches(ctx, release, argoAppName) {
		return
	}
	r.activateRelease(ctx, release)
}

// handleSyncTimeout fails a release whose sync did not converge in time, after
// terminating the stuck ArgoCD operation so the next deploy is not blocked by
// it (MEET-2026-03-29-019).
func (r *ReleaseConvergenceLoop) handleSyncTimeout(ctx context.Context, release *db.Release, argoAppName string, elapsed time.Duration) {
	r.logger.Warn("release convergence: sync timeout",
		"release_id", release.ID, "build_id", release.BuildID,
		"elapsed", elapsed, "timeout", r.cfg.SyncTimeout)
	deploycontracts.TerminateStuckSync(ctx, r.sync_, argoAppName, r.logger.With("release_id", release.ID, "reason", "sync_timeout"))
	r.failRelease(ctx, release, "Sync timeout: service did not become healthy within the time limit")
}

// deployedAppNames lists the Kubernetes workload names ArgoCD health is filtered
// on: the release's non-static apps, as `<project>-<app>` — the resource name
// the renderer emits (Achado #17/#20). Passing the bare app name made the
// health filter never match, so a healthy release stayed SYNC_FAILED. An
// unparseable spec list is logged and yields no filter (global health).
func (r *ReleaseConvergenceLoop) deployedAppNames(release *db.Release) []string {
	if release.Apps == nil {
		return nil
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(release.Apps, &apps); err != nil {
		r.logger.Error("release convergence: failed to parse app specs (skipping health filter)",
			"release_id", release.ID, "error", err)
		return nil
	}
	var names []string
	for _, app := range apps {
		if app.Rendering == "static" {
			continue
		}
		names = append(names, fmt.Sprintf("%s-%s", release.ProjectID, app.Name))
	}
	return names
}

// stalledSyncGrace is how long a PENDING release may stay unsynced before the
// loop retriggers its sync (MEET-2026-03-31-023 T1-5): under the manual sync
// policy ArgoCD never retries a failed initial trigger on its own.
const stalledSyncGrace = 30 * time.Second

// stuckControllerAfter is how long an unsynced release waits, with its
// retriggers exhausted, before the loop suspects the controller's workqueue.
const stuckControllerAfter = 2 * time.Minute

// recoverStalledSync handles an unsynced release: retrigger the sync after the
// grace period, and escalate to ForceReconcile once retriggers are exhausted.
func (r *ReleaseConvergenceLoop) recoverStalledSync(ctx context.Context, release *db.Release, argoAppName string, elapsed time.Duration) {
	if elapsed > stalledSyncGrace {
		r.retriggerSyncIfNeeded(ctx, release, argoAppName)
	}
	if elapsed > stuckControllerAfter {
		r.escalateStuckController(ctx, release, argoAppName)
	}
}

// escalateStuckController is the V9 L22 recovery: the release has been PENDING
// for over two minutes, every TriggerSync returned 200, and the Application's
// operationState never advanced — the controller's workqueue stalled (seen in
// the 2026-05-30 cleanroom dogfood, where L21 did not fire because the lock was
// not stuck). ForceReconcile sets the refresh=hard annotation, which ArgoCD acts
// on regardless of the workqueue; it is idempotent. It fires once per exhausted
// retrigger budget, and a success resets the counter so TriggerSync is retried
// after the controller picks the refresh up.
func (r *ReleaseConvergenceLoop) escalateStuckController(ctx context.Context, release *db.Release, argoAppName string) {
	loaded, ok := r.syncRetriggers.Load(release.ID)
	if !ok {
		return
	}
	counter := loaded.(*atomic.Int32)
	if counter.Load() < int32(maxSyncRetriggers) {
		return
	}
	if err := r.sync_.ForceReconcile(ctx, argoAppName); err != nil {
		r.logger.Error("release convergence: ForceReconcile failed (L22 recovery)",
			"release_id", release.ID, "app", argoAppName, "error", err)
		return
	}
	r.logger.Info("release convergence: triggered ForceReconcile (L22 — controller queue-stuck recovery)",
		"release_id", release.ID, "app", argoAppName, "retriggers_exhausted", counter.Load())
	counter.Store(0)
}

// revisionMatches reports whether the release may be activated as far as its
// revision goes (MEET-2026-04-04-029 P4): ArgoCD must have synced the release's
// own GitOps commit, or rapid sequential deploys could activate the wrong
// release. A mismatch retriggers the sync; a read error waits for the next tick.
// A release without a commit, or an unknown synced revision, is not blocked.
func (r *ReleaseConvergenceLoop) revisionMatches(ctx context.Context, release *db.Release, argoAppName string) bool {
	if release.GitCommit == "" {
		return true
	}
	revision, err := r.sync_.GetSyncRevision(ctx, argoAppName)
	if err != nil {
		r.logger.Warn("release convergence: revision check failed",
			"release_id", release.ID, "error", err)
		return false
	}
	if revision != "" && revision != release.GitCommit {
		metrics.RevisionMismatchTotal.Inc()
		r.logger.Warn("release convergence: revision mismatch — retriggering sync",
			"release_id", release.ID, "expected", release.GitCommit, "actual", revision)
		r.retriggerSyncIfNeeded(ctx, release, argoAppName)
		return false
	}
	return true
}

func (r *ReleaseConvergenceLoop) checkSync(ctx context.Context, appName string, deployedApps []string) (synced, healthy bool, err error) {
	// Use the SyncClient's WaitForSync with a very short timeout (just one check, no loop).
	// We poll externally via the convergence loop, so we don't want WaitForSync to block.
	// Instead, just check status once.

	// Sprint B T1.1: GetAppStatus is now part of the SyncClient interface
	// (was a duck-typed assertion pre-ADR D1). The WaitForSync fallback is no
	// longer reachable because every implementer must satisfy GetAppStatus.
	return r.sync_.GetAppStatus(ctx, appName, deployedApps)
}

func (r *ReleaseConvergenceLoop) activateRelease(ctx context.Context, release *db.Release) {
	err := r.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		apps := releaseAppsToActivationApps(release, r.baseDomain, r.publicDomain)
		duration := int(time.Since(release.CreatedAt).Seconds())
		return ActivationKernel{
			Releases: r.releaseStore, EnvTargets: r.envTargetStore, Events: r.eventStore,
			DeployEvents: r.deployEventStore, Logger: r.logger,
		}.Activate(ctx, tx, release, apps, duration)
	})
	if err != nil {
		r.logger.Error("release convergence: activate failed",
			"release_id", release.ID, "error", err)
		return
	}
	// MEET-2026-03-31-024: Clean up syncRetriggers entry to prevent memory leak.
	r.syncRetriggers.Delete(release.ID)

	r.logger.Info("release convergence: release activated",
		"release_id", release.ID, "build_id", release.BuildID)
}

func (r *ReleaseConvergenceLoop) failRelease(ctx context.Context, release *db.Release, reason string) {
	err := r.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		_, err := r.releaseStore.UpdateStatus(ctx, tx, release.ID, string(statemachine.RPEventSyncFailed))
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
	failedData := events.SafeJSONMarshal(map[string]interface{}{
		"status": "failed",
		"reason": reason,
	})
	if _, err := r.eventStore.Append(ctx, release.BuildID, "deploy.sync_failed", failedData); err != nil {
		r.logger.Error("failed to emit deploy.sync_failed event", "release_id", release.ID, "error", err)
	}

	// T0.3 of deep-review-remediation-plan (closes find_flow_001 CRITICAL).
	// Per CLAUDE.md, Argo Rollouts is the default workload primitive when
	// LOOPZERO_PROGRESSIVE_DELIVERY=true (the default). For Rollout-backed
	// releases, the canonical terminal SSE event is rollout.aborted (not
	// deploy.sync_failed). The CLI deploy command handles both as terminal
	// (cli/src/commands/deploy.ts:603), so emitting both is safe — but the
	// canary-specific event surfaces reason + weight + step for Pro/Team
	// canary visibility.
	if isRolloutBackedRelease(release) {
		abortReason := classifyAbortReason(reason)
		abortedEv := RolloutAbortedEvent{
			Reason:  abortReason,
			Message: reason,
		}
		if emitErr := EmitRolloutAborted(ctx, r.logger, r.eventStore, release.BuildID, abortedEv); emitErr != nil {
			// Logged inside EmitRolloutAborted; intentionally not failing
			// the failRelease path because deploy.sync_failed was already
			// emitted above as backwards-compat terminal.
			r.logger.Warn("failed to emit rollout.aborted (deploy.sync_failed already emitted)",
				"release_id", release.ID, "error", emitErr)
		}
	}

	// MEET-2026-03-31-024: Clean up syncRetriggers entry to prevent memory leak.
	r.syncRetriggers.Delete(release.ID)
}

// isRolloutBackedRelease — true if the release was rendered with progressive
// delivery (Argo Rollouts), as opposed to a static-only release. Heuristic:
// any app in the release whose Rendering is not "static" is assumed Rollout-backed
// when LOOPZERO_PROGRESSIVE_DELIVERY is enabled (default per CLAUDE.md).
//
// Per ADR T0.3: emitting an extra rollout.aborted on edge cases (e.g.,
// progressive delivery disabled but app non-static) is benign — CLI treats
// either terminal event as exit 3.
func isRolloutBackedRelease(release *db.Release) bool {
	if release == nil || release.Apps == nil {
		return false
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(release.Apps, &apps); err != nil {
		// On parse failure, assume Rollout-backed so the canary event still
		// fires. Failing closed here keeps T0.3 closed even if Apps JSON is
		// malformed (which would be a separate bug).
		return true
	}
	for _, app := range apps {
		if app.Rendering != "static" {
			return true
		}
	}
	return false
}

// classifyAbortReason — maps a free-text reason from the convergence loop to
// the documented rollout.aborted reason enum (analysis-failed | pod-unhealthy |
// user-aborted | progress-deadline-exceeded). Best-effort; default is
// pod-unhealthy because the convergence loop's primary failure path is
// "release did not become healthy within sync timeout".
func classifyAbortReason(reason string) string {
	r := strings.ToLower(reason)
	switch {
	case strings.Contains(r, "analysis"):
		return "analysis-failed"
	case strings.Contains(r, "deadline") || strings.Contains(r, "progress"):
		return "progress-deadline-exceeded"
	case strings.Contains(r, "user") || strings.Contains(r, "cancel") || strings.Contains(r, "abort"):
		return "user-aborted"
	default:
		return "pod-unhealthy"
	}
}

// convergenceSyncPolicy is the stuck-operation rule the convergence loop applies
// across ticks: it retries once per tick rather than inside one call, so only the
// termination threshold of the shared policy applies here.
var convergenceSyncPolicy = deploycontracts.SyncRecoveryPolicy{TerminateAfterStuck: 2}

// maxSyncRetriggers bounds the number of times the convergence loop will
// retrigger TriggerSync for a single release. Prevents infinite retry loops.
const maxSyncRetriggers = 3

// retriggerSyncIfNeeded calls TriggerSync when a PENDING release has no active
// sync operation in ArgoCD. With manual sync policy, a failed initial TriggerSync
// means ArgoCD will never auto-sync from Gitea, leaving the release stuck.
// MEET-2026-03-31-023 T1-5.
//
// T5.5 of deep-review-2026-05-10-remediation-plan (find_arch_sync_retrigger_race MEDIUM):
// pre-T5.5 the counter pattern was LoadOrStore(0) → read → Store(count+1).
// Two concurrent reconciliations of the same release could both read 0
// then both Store(1), losing one increment. Now we use atomic.Int32 via
// LoadOrStore (returning the SAME pointer for repeat lookups) and
// `(*atomic.Int32).Add(1)` so concurrent increments are serialized.
func (r *ReleaseConvergenceLoop) retriggerSyncIfNeeded(ctx context.Context, release *db.Release, argoAppName string) {
	// Atomic counter: LoadOrStore returns the existing *atomic.Int32 (or
	// the freshly-stored one) so subsequent goroutines hit the same cell.
	loaded, _ := r.syncRetriggers.LoadOrStore(release.ID, new(atomic.Int32))
	counter := loaded.(*atomic.Int32)
	if counter.Load() >= int32(maxSyncRetriggers) {
		r.logger.Warn("release convergence: max sync retriggers reached",
			"release_id", release.ID, "retriggers", counter.Load())
		return
	}

	// Attempt to trigger sync
	if err := r.sync_.TriggerSync(ctx, argoAppName); err != nil {
		r.logger.Warn("release convergence: sync retrigger failed",
			"release_id", release.ID, "app", argoAppName, "error", err)

		// V9 L21: when TriggerSync fails with SYNC_OPERATION_IN_PROGRESS
		// repeatedly, the ArgoCD app has a stale operation lock — typically
		// caused by a controller restart mid-sync. Without intervention,
		// every subsequent retrigger fails until the release sync-timeouts
		// (5min) at which point the release is failed. We can short-circuit
		// this by calling TerminateOperation after 2 consecutive failures
		// to clear the stale lock, then the NEXT retrigger succeeds.
		//
		// Detection: errors.Is against ErrSyncOperationInProgress matches
		// both the wrapped and unwrapped forms produced by sync_client.go.
		//
		// Discovered LIVE via cleanroom-fresh dogfood 2026-05-30 (audit
		// 2026-05-30-V9-cleanroom-fresh): after a manual controller restart
		// during initial TLS handshake recovery, 4 journeys cascaded into
		// release ACTIVE timeout because their syncs were never re-tried
		// successfully — the loop logged "sync retrigger failed" every poll
		// for the full 5min sync_timeout window without ever calling
		// TerminateOperation. The timeout-side termination in verifyRelease
		// fires only AFTER failure; this pre-failure path
		// recovers without losing the release.
		if deploycontracts.IsSyncStuck(err) {
			// Count the stuck failure in the same counter as a retrigger, so the
			// policy's threshold is reached after two consecutive stuck ticks. One
			// is a normal race (the operation completes between the status check and
			// the retrigger); two means the lock is stuck.
			counter.Add(1)
			if convergenceSyncPolicy.ShouldTerminate(int(counter.Load())) {
				deploycontracts.TerminateStuckSync(ctx, r.sync_, argoAppName,
					r.logger.With("release_id", release.ID, "reason", "pre_timeout_recovery"))
			}
		}
		return
	}

	attempt := counter.Add(1)
	r.logger.Info("release convergence: retriggered sync for stalled release",
		"release_id", release.ID, "app", argoAppName, "attempt", attempt)
}

// healthCheckActiveReleases checks ACTIVE releases against ArgoCD health.
// MEET-2026-03-31-023 T1-4: Post-activation health monitoring.
//
// This is observability-only — it does NOT change release state. It emits
// metrics (ReleaseStateDivergence, DriftDetected) so Prometheus alerts can
// fire when an ACTIVE release becomes unhealthy in ArgoCD.
func (r *ReleaseConvergenceLoop) healthCheckActiveReleases(ctx context.Context) {
	releases, err := r.releaseStore.FindActiveReleases(ctx, r.cellID())
	if err != nil {
		r.logger.Error("health check: failed to find active releases", "error", err)
		return
	}

	for _, rel := range releases {
		argoAppName, nameErr := naming.ArgoAppName(rel.TenantID, rel.ProjectID, rel.Environment)
		if nameErr != nil {
			r.logger.Error("release convergence: argocd application name",
				"tenant", rel.TenantID, "project", rel.ProjectID,
				"environment", rel.Environment, "error", nameErr)
			continue
		}

		synced, healthy, checkErr := r.checkSync(ctx, argoAppName, r.deployedAppNames(&rel))
		if checkErr != nil {
			r.logger.Warn("health check: ArgoCD status check error",
				"release_id", rel.ID, "app", argoAppName, "error", checkErr)
			continue
		}

		// loopzero-cloud#242 (Fix A): only evaluate post-activation health when the
		// ArgoCD status is DETERMINATE (synced). During the controller's
		// reconcile-rewrite window the app status is transiently empty/Unknown →
		// GetAppStatus returns synced=false, healthy=false (no workload resources
		// visible yet). That is a STILL-CONVERGING state, not a post-activation
		// health regression. The pre-fix code flagged it as a false-success,
		// making a live, healthy promotion look failed ("sem sucesso") in the dashboard  // english-only: verbatim dashboard label, meaning without success
		// and firing spurious FalseSuccess/divergence Prometheus signals. A
		// non-Synced app is converging or drifting — neither is a "healthy
		// workload degraded" verdict — so skip and let a later synced poll
		// set/clear the gauges. Genuine unhealth is synced==true, healthy==false
		// (a matched workload reported Degraded), handled below.
		if !synced {
			continue
		}

		if !healthy {
			metrics.ReleaseStateDivergence.WithLabelValues(rel.TenantID, rel.ProjectID, rel.Environment).Set(1)
			metrics.DriftDetected.WithLabelValues(rel.TenantID, rel.Environment).Set(1)
			// Phase 4A: False success — release was ACTIVE but is now unhealthy.
			metrics.FalseSuccessTotal.WithLabelValues(rel.TenantID, rel.Environment).Inc()
			r.logger.Warn("health check: ACTIVE release is unhealthy in ArgoCD",
				"release_id", rel.ID, "tenant", rel.TenantID,
				"project", rel.ProjectID, "env", rel.Environment)
		} else {
			metrics.ReleaseStateDivergence.WithLabelValues(rel.TenantID, rel.ProjectID, rel.Environment).Set(0)
			metrics.DriftDetected.WithLabelValues(rel.TenantID, rel.Environment).Set(0)
		}
	}
}
