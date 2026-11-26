// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// PreviewEnvironment states as defined in spec section 5.3.
//
// T2.1 / COMP-001: string values aligned with operators/api/v1
// PreviewPhase constants ("Creating", "Active", etc.) so the cast
// loopruntimev1.PreviewPhase(sm.Transition(...)) used by PreviewController
// works without a translation layer — matching the TenantMachine
// pattern that already aligns with loopruntimev1.TenantPhase.
const (
	PreviewEmpty         State = ""
	PreviewCreating      State = "Creating"
	PreviewActive        State = "Active"
	PreviewExpiring      State = "Expiring"
	PreviewDeleted       State = "Deleted"
	PreviewFailed        State = "Failed"
	PreviewDeleting      State = "Deleting"
	PreviewCleanupFailed State = "CleanupFailed"
)

// PreviewEnvironment events as defined in spec section 5.3.
const (
	PreviewCreate              Event = "Create"
	PreviewCreationCompleted   Event = "CreationCompleted"
	PreviewCreationFailed      Event = "CreationFailed"
	PreviewExpirationTriggered Event = "ExpirationTriggered"
	PreviewDeletionTriggered   Event = "DeletionTriggered"
	PreviewCleanupCompleted    Event = "CleanupCompleted"
	PreviewCleanupFailed_      Event = "CleanupFailed"
	PreviewGarbageCollected    Event = "GarbageCollected"
)

// NewPreviewMachine returns the preview environment state machine per spec section 5.3.
//
// States: (empty) -> CREATING -> ACTIVE -> EXPIRING/DELETING -> DELETED
// Terminal: DELETED
func NewPreviewMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "PreviewEnvironment",
		Transitions: []transition{
			// A new CR enters the lifecycle
			{From: PreviewEmpty, Event: PreviewCreate, To: PreviewCreating},

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
