package statemachine

import "testing"

func TestPreviewMachine_HappyPath(t *testing.T) {
	m := NewPreviewMachine()

	// CREATING -> ACTIVE -> EXPIRING -> DELETED
	state := m.Transition(PreviewCreating, PreviewCreationCompleted)
	assertState(t, state, PreviewActive)

	state = m.Transition(state, PreviewExpirationTriggered)
	assertState(t, state, PreviewExpiring)

	state = m.Transition(state, PreviewCleanupCompleted)
	assertState(t, state, PreviewDeleted)
}

func TestPreviewMachine_ExplicitDelete(t *testing.T) {
	m := NewPreviewMachine()

	state := m.Transition(PreviewCreating, PreviewCreationCompleted)
	state = m.Transition(state, PreviewDeletionTriggered)
	assertState(t, state, PreviewDeleting)

	state = m.Transition(state, PreviewCleanupCompleted)
	assertState(t, state, PreviewDeleted)
}

func TestPreviewMachine_CreationFailed(t *testing.T) {
	m := NewPreviewMachine()

	state := m.Transition(PreviewCreating, PreviewCreationFailed)
	assertState(t, state, PreviewFailed)

	// Failed previews are cleaned up automatically.
	state = m.Transition(state, PreviewDeletionTriggered)
	assertState(t, state, PreviewDeleting)

	state = m.Transition(state, PreviewCleanupCompleted)
	assertState(t, state, PreviewDeleted)
}

func TestPreviewMachine_CleanupFailedDuringExpiry(t *testing.T) {
	m := NewPreviewMachine()

	state := m.Transition(PreviewCreating, PreviewCreationCompleted)
	state = m.Transition(state, PreviewExpirationTriggered)
	state = m.Transition(state, PreviewCleanupFailed_)
	assertState(t, state, PreviewCleanupFailed)

	// Garbage collector catches it.
	state = m.Transition(state, PreviewGarbageCollected)
	assertState(t, state, PreviewDeleted)
}

func TestPreviewMachine_CleanupFailedDuringDelete(t *testing.T) {
	m := NewPreviewMachine()

	state := m.Transition(PreviewCreating, PreviewCreationCompleted)
	state = m.Transition(state, PreviewDeletionTriggered)
	state = m.Transition(state, PreviewCleanupFailed_)
	assertState(t, state, PreviewCleanupFailed)

	state = m.Transition(state, PreviewGarbageCollected)
	assertState(t, state, PreviewDeleted)
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
