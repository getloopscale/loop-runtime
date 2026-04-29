// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

import "time"

// Release states as defined in spec section 5.2.
// Release state timeout defaults (spec §5.2).
const (
	ReleaseReadyTimeout       = 5 * time.Minute
	// MEET-2026-04-04-029 P2: Reaper PendingTimeout MUST be strictly greater than
	// convergence loop SyncTimeout (300s) to prevent race conditions.
	// Hierarchy: convergence gives up at 300s → reaper cleans up at 600s.
	ReleasePendingTimeout = 10 * time.Minute
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
	ReleaseReactivated        Event = "Reactivated" // Rollback reactivates a SUPERSEDED or ROLLED_BACK release
	ReleaseProjectDeleted     Event = "ProjectDeleted" // Cascade from `loopzero project delete`. Transitions any non-terminal state to CANCELLED so future GetActiveByEnv lookups don't return zombie rows.
)

// IsReleaseTerminal returns true if the given state is a truly terminal release state.
// Only CANCELLED is terminal — SUPERSEDED and ROLLED_BACK can be reactivated
// via the ReleaseReactivated event (rollback flow). This is consistent with
// NewReleaseMachine().Terminal which only lists ReleaseCancelled.
func IsReleaseTerminal(state State) bool {
	return state == ReleaseCancelled
}

// IsReleaseReapable returns true if the state is one the ReleaseReaper should skip
// because the release has already left the reapable states (READY or PENDING).
// This includes terminal states AND failure/active states that represent forward progress.
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
// Terminal: SUPERSEDED, ROLLED_BACK, CANCELLED
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
			// Required so `loopzero project delete` doesn't leave zombie ACTIVE/PENDING rows
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
		},
		// Only CANCELLED is truly terminal. SUPERSEDED and ROLLED_BACK can be
		// reactivated via rollback (ReleaseReactivated event).
		Terminal: []State{ReleaseCancelled},
		Timeouts: []TimeoutConfig{
			{State: ReleaseReady, Timeout: ReleaseReadyTimeout, Reason: "GIT_PUSH_TIMEOUT"},
			{State: ReleasePending, Timeout: ReleasePendingTimeout, Reason: "SYNC_TIMEOUT"},
			{State: ReleaseRollingBack, Timeout: ReleaseRollingBackTimeout, Reason: "ROLLBACK_TIMEOUT"},
		},
	})
}
