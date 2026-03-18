package statemachine

import "time"

// Release states as defined in spec section 5.2.
// Release state timeout defaults (spec §5.2).
const (
	ReleaseReadyTimeout       = 5 * time.Minute
	ReleasePendingTimeout     = 2 * time.Minute
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
)

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

			// Sync failures and retry
			{From: ReleasePending, Event: ReleaseSyncFailed_, To: ReleaseSyncFailed},
			{From: ReleasePending, Event: ReleaseSyncTimeout, To: ReleaseSyncFailed},
			{From: ReleasePending, Event: ReleaseCancelledEvent, To: ReleaseCancelled},
			{From: ReleaseSyncFailed, Event: ReleaseSyncRetry, To: ReleasePending},
			{From: ReleaseSyncFailed, Event: ReleaseSyncExhausted, To: ReleaseDriftAlert},

			// Active state transitions
			{From: ReleaseActive, Event: ReleaseNewActivated, To: ReleaseSuperseded},
			{From: ReleaseActive, Event: ReleaseRollbackStarted, To: ReleaseRollingBack},

			// Rollback outcomes
			{From: ReleaseRollingBack, Event: ReleaseRollbackCompleted, To: ReleaseRolledBack},
			{From: ReleaseRollingBack, Event: ReleaseRollbackFailed_, To: ReleaseRollbackFailed},
			{From: ReleaseRollingBack, Event: ReleaseRollbackTimeout, To: ReleaseRollbackFailed},
		},
		Terminal: []State{ReleaseSuperseded, ReleaseRolledBack, ReleaseCancelled},
		Timeouts: []TimeoutConfig{
			{State: ReleaseReady, Timeout: ReleaseReadyTimeout, Reason: "GIT_PUSH_TIMEOUT"},
			{State: ReleasePending, Timeout: ReleasePendingTimeout, Reason: "SYNC_TIMEOUT"},
			{State: ReleaseRollingBack, Timeout: ReleaseRollingBackTimeout, Reason: "ROLLBACK_TIMEOUT"},
		},
	})
}
