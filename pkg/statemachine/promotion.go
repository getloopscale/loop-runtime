// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Promotion states as defined in spec section 5.4.
const (
	PromotionValidating   State = "VALIDATING"
	PromotionCommitting   State = "COMMITTING"
	PromotionSyncing      State = "SYNCING"
	PromotionCompleted    State = "COMPLETED"
	PromotionRejected     State = "REJECTED"
	PromotionCommitFailed State = "COMMIT_FAILED"
	PromotionSyncFailed   State = "SYNC_FAILED"
)

// Promotion events as defined in spec section 5.4.
const (
	PromotionValidationPassed Event = "ValidationPassed"
	PromotionValidationFailed Event = "ValidationFailed"
	PromotionCommitCompleted  Event = "CommitCompleted"
	PromotionCommitFailed_    Event = "CommitFailed"
	PromotionSyncCompleted    Event = "SyncCompleted"
	PromotionSyncFailed_      Event = "SyncFailed"
)

// NewPromotionMachine returns the promotion state machine per spec section 5.4.
//
// States: VALIDATING -> COMMITTING -> SYNCING -> COMPLETED
// Terminal: COMPLETED, REJECTED, COMMIT_FAILED, SYNC_FAILED
func NewPromotionMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Promotion",
		Transitions: []transition{
			{From: PromotionValidating, Event: PromotionValidationPassed, To: PromotionCommitting},
			{From: PromotionValidating, Event: PromotionValidationFailed, To: PromotionRejected},
			{From: PromotionCommitting, Event: PromotionCommitCompleted, To: PromotionSyncing},
			{From: PromotionCommitting, Event: PromotionCommitFailed_, To: PromotionCommitFailed},
			{From: PromotionSyncing, Event: PromotionSyncCompleted, To: PromotionCompleted},
			{From: PromotionSyncing, Event: PromotionSyncFailed_, To: PromotionSyncFailed},
		},
		Terminal: []State{PromotionCompleted, PromotionRejected, PromotionCommitFailed, PromotionSyncFailed},
		Timeouts: nil, // Sync timeout (2 min) is handled by the controller, not the machine.
	})
}
