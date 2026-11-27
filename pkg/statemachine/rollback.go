// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Rollback states as defined in spec section 5.5.
const (
	RollbackValidating   State = "VALIDATING"
	RollbackReverting    State = "REVERTING"
	RollbackSyncing      State = "SYNCING"
	RollbackCompleted    State = "COMPLETED"
	RollbackRejected     State = "REJECTED"
	RollbackBlocked      State = "BLOCKED"
	RollbackRevertFailed State = "REVERT_FAILED"
	RollbackSyncFailed   State = "SYNC_FAILED"
)

// Rollback events as defined in spec section 5.5.
const (
	RollbackValidationPassed      Event = "ValidationPassed"
	RollbackUnacknowledgedEffects Event = "UnacknowledgedEffects"
	RollbackNoPreviousRelease     Event = "NoPreviousRelease"
	RollbackRevertCompleted       Event = "RevertCompleted"
	RollbackRevertFailed_         Event = "RevertFailed"
	RollbackSyncCompleted         Event = "SyncCompleted"
	RollbackSyncFailed_           Event = "SyncFailed"
)

// NewRollbackMachine returns the rollback state machine per spec section 5.5.
//
// States: VALIDATING -> REVERTING -> SYNCING -> COMPLETED
// Terminal: COMPLETED, REJECTED, BLOCKED, REVERT_FAILED, SYNC_FAILED
func NewRollbackMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Rollback",
		Transitions: []transition{
			{From: RollbackValidating, Event: RollbackValidationPassed, To: RollbackReverting},
			{From: RollbackValidating, Event: RollbackUnacknowledgedEffects, To: RollbackRejected},
			{From: RollbackValidating, Event: RollbackNoPreviousRelease, To: RollbackBlocked},
			{From: RollbackReverting, Event: RollbackRevertCompleted, To: RollbackSyncing},
			{From: RollbackReverting, Event: RollbackRevertFailed_, To: RollbackRevertFailed},
			{From: RollbackSyncing, Event: RollbackSyncCompleted, To: RollbackCompleted},
			{From: RollbackSyncing, Event: RollbackSyncFailed_, To: RollbackSyncFailed},
		},
		Terminal: []State{RollbackCompleted, RollbackRejected, RollbackBlocked, RollbackRevertFailed, RollbackSyncFailed},
		Timeouts: nil, // Sync timeout is handled by the controller.
	})
}
