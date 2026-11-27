// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "time"

// Release states as defined in spec section 5.2.
// Release state timeout defaults (spec §5.2).
const (
	ReleaseReadyTimeout = 5 * time.Minute
	// MEET-2026-04-04-029 P2: Reaper PendingTimeout MUST be strictly greater than
	// convergence loop SyncTimeout (300s) to prevent race conditions.
	// Hierarchy: convergence gives up at 300s → reaper cleans up at 600s.
	ReleasePendingTimeout     = 10 * time.Minute
	ReleaseRollingBackTimeout = 2 * time.Minute
)

const (
	ReleaseReady           State = "READY"
	ReleasePending         State = "PENDING"
	ReleaseActive          State = "ACTIVE"
	ReleaseGitMirrorFailed State = "GIT_MIRROR_FAILED"
	ReleaseSyncFailed      State = "SYNC_FAILED"
	ReleaseDriftAlert      State = "DRIFT_ALERT"
	ReleaseSuperseded      State = "SUPERSEDED"
	ReleaseRollingBack     State = "ROLLING_BACK"
	ReleaseRolledBack      State = "ROLLED_BACK"
	ReleaseRollbackFailed  State = "ROLLBACK_FAILED"
	ReleaseCancelled       State = "CANCELLED"
	// ReleaseArchived was introduced by migration 000008
	// (2026-05-02 — archive stale failed/cancelled releases > 7 days).
	// T4.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap formalizes it
	// in the state machine so a future archiver service can use Transition()
	// instead of a raw SQL UPDATE.
	ReleaseArchived State = "ARCHIVED"
)

// Release events as defined in spec section 5.2.
const (
	ReleaseGitMirrorCompleted Event = "GitMirrorCompleted"
	ReleaseGitMirrorFailed_   Event = "GitMirrorFailed"
	ReleaseGitMirrorRetried   Event = "GitMirrorRetried"
	ReleaseSyncCompleted      Event = "SyncCompleted"
	ReleaseSyncFailed_        Event = "SyncFailed"
	ReleaseSyncExhausted      Event = "SyncExhausted"
	ReleaseSyncRetry          Event = "SyncRetry"
	ReleaseNewActivated       Event = "NewReleaseActivated"
	ReleaseRollbackStarted    Event = "RollbackStarted"
	ReleaseRollbackCompleted  Event = "RollbackCompleted"
	ReleaseRollbackFailed_    Event = "RollbackFailed"
	ReleaseCancelledEvent     Event = "ReleaseCancelled"
	ReleaseGitPushTimeout     Event = "GitPushTimeout"
	ReleaseSyncTimeout        Event = "SyncTimeout"
	ReleaseRollbackTimeout    Event = "RollbackTimeout"
	ReleaseManualResolve      Event = "ManualResolve"
	ReleaseReactivated        Event = "Reactivated"    // Rollback reactivates a SUPERSEDED or ROLLED_BACK release
	ReleaseProjectDeleted     Event = "ProjectDeleted" // Cascade from `loopscale project delete`. Transitions any non-terminal state to CANCELLED so future GetActiveByEnv lookups don't return zombie rows.
	// ReleaseArchiveEvent is fired by the (future) ReleaseArchiver service
	// when a CANCELLED, SUPERSEDED, ROLLED_BACK, GIT_MIRROR_FAILED, or
	// SYNC_FAILED release is older than the archive window (currently 7 days
	// per migration 000008). Transitions to ARCHIVED — terminal, excluded
	// from active queries.
	ReleaseArchiveEvent Event = "ReleaseArchived"
)

// IsReleaseTerminal returns true if the given state is a truly terminal release state.
// CANCELLED and ARCHIVED are terminal — SUPERSEDED and ROLLED_BACK can be
// reactivated via the ReleaseReactivated event (rollback flow). ARCHIVED added
// by T4.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap (migration 000008
// formalized into state machine). Consistent with NewReleaseMachine().Terminal.
func IsReleaseTerminal(state State) bool {
	return releaseLifecycle.IsTerminal(state)
}

// releaseLifecycle answers the package-level Release helpers from the one
// transition table. A Machine is immutable, so sharing it is safe.
var releaseLifecycle = NewReleaseMachine()

// IsReleaseReapable reports whether the ReleaseReaper may reap a release in
// this state: only READY and PENDING, the states a release can be stuck in
// before it lands. Every other state (active, failed, terminal) is forward
// progress the reaper must leave alone.
func IsReleaseReapable(state State) bool {
	switch state {
	case ReleaseReady, ReleasePending:
		return true
	}
	return false
}

// NewReleaseMachine returns the release state machine per spec section 5.2.
//
// States: READY -> PENDING -> ACTIVE -> SUPERSEDED|ROLLING_BACK
// Terminal: CANCELLED, ARCHIVED (SUPERSEDED and ROLLED_BACK can be
// reactivated by a rollback)
// Manual intervention: DRIFT_ALERT, ROLLBACK_FAILED, GIT_MIRROR_FAILED
func NewReleaseMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Release",
		Transitions: []transition{
			// Happy path
			{From: ReleaseReady, Event: ReleaseGitMirrorCompleted, To: ReleasePending},
			{From: ReleasePending, Event: ReleaseSyncCompleted, To: ReleaseActive},

			// Git mirror failures
			{From: ReleaseReady, Event: ReleaseGitMirrorFailed_, To: ReleaseGitMirrorFailed},
			{From: ReleaseReady, Event: ReleaseGitPushTimeout, To: ReleaseGitMirrorFailed},
			{From: ReleaseGitMirrorFailed, Event: ReleaseGitMirrorRetried, To: ReleasePending},
			{From: ReleaseGitMirrorFailed, Event: ReleaseCancelledEvent, To: ReleaseCancelled},

			// Sync failures and retry
			{From: ReleasePending, Event: ReleaseSyncFailed_, To: ReleaseSyncFailed},
			{From: ReleasePending, Event: ReleaseSyncTimeout, To: ReleaseSyncFailed},
			{From: ReleasePending, Event: ReleaseCancelledEvent, To: ReleaseCancelled},
			{From: ReleaseSyncFailed, Event: ReleaseSyncRetry, To: ReleasePending},
			{From: ReleaseSyncFailed, Event: ReleaseSyncExhausted, To: ReleaseDriftAlert},
			{From: ReleaseSyncFailed, Event: ReleaseCancelledEvent, To: ReleaseCancelled},

			// Active state transitions
			{From: ReleaseActive, Event: ReleaseNewActivated, To: ReleaseSuperseded},
			{From: ReleaseActive, Event: ReleaseRollbackStarted, To: ReleaseRollingBack},

			// Rollback outcomes
			{From: ReleaseRollingBack, Event: ReleaseRollbackCompleted, To: ReleaseRolledBack},
			{From: ReleaseRollingBack, Event: ReleaseRollbackFailed_, To: ReleaseRollbackFailed},
			{From: ReleaseRollingBack, Event: ReleaseRollbackTimeout, To: ReleaseRollbackFailed},

			// Manual intervention recovery (spec §5.2 — sink state exits)
			{From: ReleaseDriftAlert, Event: ReleaseManualResolve, To: ReleaseActive},
			{From: ReleaseDriftAlert, Event: ReleaseCancelledEvent, To: ReleaseCancelled},
			{From: ReleaseRollbackFailed, Event: ReleaseManualResolve, To: ReleaseActive},
			{From: ReleaseRollbackFailed, Event: ReleaseCancelledEvent, To: ReleaseCancelled},

			// Rollback reactivation — reverts a SUPERSEDED or ROLLED_BACK release back to ACTIVE
			{From: ReleaseSuperseded, Event: ReleaseReactivated, To: ReleaseActive},
			{From: ReleaseRolledBack, Event: ReleaseReactivated, To: ReleaseActive},

			// Project delete cascade — every non-terminal state transitions to CANCELLED.
			// Required so `loopscale project delete` doesn't leave zombie ACTIVE/PENDING rows
			// that would shadow fresh deploys via GetActiveByEnv.
			{From: ReleaseReady, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleasePending, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseActive, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseSuperseded, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseRolledBack, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseRollingBack, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseGitMirrorFailed, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseSyncFailed, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseRollbackFailed, Event: ReleaseProjectDeleted, To: ReleaseCancelled},
			{From: ReleaseDriftAlert, Event: ReleaseProjectDeleted, To: ReleaseCancelled},

			// Archive cascade — stale non-terminal rows transition to ARCHIVED.
			// Terminal CANCELLED is archived via raw SQL UPDATE (per migration
			// 000008) — state machine invariant forbids outgoing transitions
			// from terminal states. T4.5 of plan
			// 2026-05-26-maturity-3-62-to-4-0-roadmap formalizes transitions
			// for the reactivatable + failure states so the future archiver
			// service can use Transition() instead of raw UPDATE for them.
			{From: ReleaseSuperseded, Event: ReleaseArchiveEvent, To: ReleaseArchived},
			{From: ReleaseRolledBack, Event: ReleaseArchiveEvent, To: ReleaseArchived},
			{From: ReleaseGitMirrorFailed, Event: ReleaseArchiveEvent, To: ReleaseArchived},
			{From: ReleaseSyncFailed, Event: ReleaseArchiveEvent, To: ReleaseArchived},
			{From: ReleaseRollbackFailed, Event: ReleaseArchiveEvent, To: ReleaseArchived},
			{From: ReleaseDriftAlert, Event: ReleaseArchiveEvent, To: ReleaseArchived},
		},
		// CANCELLED + ARCHIVED are truly terminal. SUPERSEDED and ROLLED_BACK can be
		// reactivated via rollback (ReleaseReactivated event); ARCHIVED cannot.
		Terminal: []State{ReleaseCancelled, ReleaseArchived},
		Timeouts: []TimeoutConfig{
			{State: ReleaseReady, Timeout: ReleaseReadyTimeout, Reason: "GIT_PUSH_TIMEOUT"},
			{State: ReleasePending, Timeout: ReleasePendingTimeout, Reason: "SYNC_TIMEOUT"},
			{State: ReleaseRollingBack, Timeout: ReleaseRollingBackTimeout, Reason: "ROLLBACK_TIMEOUT"},
		},
	})
}
