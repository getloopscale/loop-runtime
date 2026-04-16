// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// PreviewEnvironment states as defined in spec section 5.3.
const (
	PreviewCreating      State = "CREATING"
	PreviewActive        State = "ACTIVE"
	PreviewExpiring      State = "EXPIRING"
	PreviewDeleted       State = "DELETED"
	PreviewFailed        State = "FAILED"
	PreviewDeleting      State = "DELETING"
	PreviewCleanupFailed State = "CLEANUP_FAILED"
)

// PreviewEnvironment events as defined in spec section 5.3.
const (
	PreviewCreationCompleted  Event = "CreationCompleted"
	PreviewCreationFailed     Event = "CreationFailed"
	PreviewExpirationTriggered Event = "ExpirationTriggered"
	PreviewDeletionTriggered  Event = "DeletionTriggered"
	PreviewCleanupCompleted   Event = "CleanupCompleted"
	PreviewCleanupFailed_     Event = "CleanupFailed"
	PreviewGarbageCollected   Event = "GarbageCollected"
)

// NewPreviewMachine returns the preview environment state machine per spec section 5.3.
//
// States: CREATING -> ACTIVE -> EXPIRING/DELETING -> DELETED
// Terminal: DELETED
func NewPreviewMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "PreviewEnvironment",
		Transitions: []transition{
			// Creation outcomes
			{From: PreviewCreating, Event: PreviewCreationCompleted, To: PreviewActive},
			{From: PreviewCreating, Event: PreviewCreationFailed, To: PreviewFailed},

			// Active -> expiry or explicit delete
			{From: PreviewActive, Event: PreviewExpirationTriggered, To: PreviewExpiring},
			{From: PreviewActive, Event: PreviewDeletionTriggered, To: PreviewDeleting},

			// Expiring -> cleanup
			{From: PreviewExpiring, Event: PreviewCleanupCompleted, To: PreviewDeleted},
			{From: PreviewExpiring, Event: PreviewCleanupFailed_, To: PreviewCleanupFailed},

			// Explicit delete -> cleanup
			{From: PreviewDeleting, Event: PreviewCleanupCompleted, To: PreviewDeleted},
			{From: PreviewDeleting, Event: PreviewCleanupFailed_, To: PreviewCleanupFailed},

			// Failed -> cleanup
			{From: PreviewFailed, Event: PreviewDeletionTriggered, To: PreviewDeleting},

			// Garbage collection catches cleanup failures
			{From: PreviewCleanupFailed, Event: PreviewGarbageCollected, To: PreviewDeleted},
		},
		Terminal: []State{PreviewDeleted},
		Timeouts: nil, // Preview TTL is handled by ExpiresAt field, not state machine timeouts.
	})
}
