// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

import "testing"

// TestPreviewMachine_TransitionSequences exercises every valid PreviewMachine
// transition chain starting from PreviewCreating. Phase 3 helper: collapses
// 5 near-clone test functions into one table-driven test.
func TestPreviewMachine_TransitionSequences(t *testing.T) {
	type step struct {
		event    Event
		expected State
	}
	cases := []struct {
		name  string
		steps []step // applied in order, starting from PreviewCreating
	}{
		{
			name: "HappyPath_CreatingToDeleted",
			steps: []step{
				{PreviewCreationCompleted, PreviewActive},
				{PreviewExpirationTriggered, PreviewExpiring},
				{PreviewCleanupCompleted, PreviewDeleted},
			},
		},
		{
			name: "ExplicitDelete",
			steps: []step{
				{PreviewCreationCompleted, PreviewActive},
				{PreviewDeletionTriggered, PreviewDeleting},
				{PreviewCleanupCompleted, PreviewDeleted},
			},
		},
		{
			name: "CreationFailed_AutomaticCleanup",
			steps: []step{
				{PreviewCreationFailed, PreviewFailed},
				{PreviewDeletionTriggered, PreviewDeleting},
				{PreviewCleanupCompleted, PreviewDeleted},
			},
		},
		{
			name: "CleanupFailedDuringExpiry_GarbageCollected",
			steps: []step{
				{PreviewCreationCompleted, PreviewActive},
				{PreviewExpirationTriggered, PreviewExpiring},
				{PreviewCleanupFailed_, PreviewCleanupFailed},
				{PreviewGarbageCollected, PreviewDeleted},
			},
		},
		{
			name: "CleanupFailedDuringDelete_GarbageCollected",
			steps: []step{
				{PreviewCreationCompleted, PreviewActive},
				{PreviewDeletionTriggered, PreviewDeleting},
				{PreviewCleanupFailed_, PreviewCleanupFailed},
				{PreviewGarbageCollected, PreviewDeleted},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewPreviewMachine()
			state := PreviewCreating
			for _, s := range c.steps {
				state = m.Transition(state, s.event)
				assertState(t, state, s.expected)
			}
		})
	}
}

func TestPreviewMachine_TerminalDeletedRejectsAllEvents(t *testing.T) {
	m := NewPreviewMachine()

	allEvents := []Event{
		PreviewCreationCompleted, PreviewCreationFailed,
		PreviewExpirationTriggered, PreviewDeletionTriggered,
		PreviewCleanupCompleted, PreviewCleanupFailed_,
		PreviewGarbageCollected,
	}

	for _, event := range allEvents {
		assertPanics(t, func() {
			m.Transition(PreviewDeleted, event)
		}, "expected panic for transition from DELETED on event %q", event)
	}
}

func TestPreviewMachine_IsTerminal(t *testing.T) {
	m := NewPreviewMachine()

	if !m.IsTerminal(PreviewDeleted) {
		t.Error("DELETED should be terminal")
	}

	nonTerminal := []State{
		PreviewCreating, PreviewActive, PreviewExpiring,
		PreviewFailed, PreviewDeleting, PreviewCleanupFailed,
	}
	for _, s := range nonTerminal {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal", s)
		}
	}
}

// A new PreviewEnvironment enters the lifecycle through the machine, like every
// other LoopZero CRD, instead of the controller assigning Creating directly.
func TestPreviewMachine_CreateMovesANewPreviewToCreating(t *testing.T) {
	m := NewPreviewTypedMachine()

	got, err := m.TryTransition(PPhEmpty, PPEventCreate)

	if err != nil {
		t.Fatalf("TryTransition(Empty, Create): %v", err)
	}
	if got != PPhCreating {
		t.Fatalf("TryTransition(Empty, Create) = %q, want %q", got, PPhCreating)
	}
}
