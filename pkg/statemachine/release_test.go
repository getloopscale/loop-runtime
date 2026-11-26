// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

// TestReleaseMachine_TransitionSequences exercises every valid ReleaseMachine
// transition chain starting from ReleaseReady. Phase 3 helper: collapses 10
// near-clone test functions into one table-driven test (same pattern as
// TestBuildMachine_TransitionSequences).
func TestReleaseMachine_TransitionSequences(t *testing.T) {
	type step struct {
		event    Event
		expected State
	}
	cases := []struct {
		name  string
		steps []step // applied in order, starting from ReleaseReady
	}{
		{
			name: "HappyPath_ReadyToSuperseded",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncCompleted, ReleaseActive},
				{ReleaseNewActivated, ReleaseSuperseded},
			},
		},
		{
			name: "GitMirrorFailedThenRetry",
			steps: []step{
				{ReleaseGitMirrorFailed_, ReleaseGitMirrorFailed},
				{ReleaseGitMirrorRetried, ReleasePending},
			},
		},
		{
			name:  "GitPushTimeout",
			steps: []step{{ReleaseGitPushTimeout, ReleaseGitMirrorFailed}},
		},
		{
			name: "SyncFailedThenRetry",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncFailed_, ReleaseSyncFailed},
				{ReleaseSyncRetry, ReleasePending},
			},
		},
		{
			name: "SyncExhaustedToDriftAlert",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncFailed_, ReleaseSyncFailed},
				{ReleaseSyncExhausted, ReleaseDriftAlert},
			},
		},
		{
			name: "SyncTimeout",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncTimeout, ReleaseSyncFailed},
			},
		},
		{
			name: "PendingCancelled",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseCancelledEvent, ReleaseCancelled},
			},
		},
		{
			name: "RollbackHappyPath",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncCompleted, ReleaseActive},
				{ReleaseRollbackStarted, ReleaseRollingBack},
				{ReleaseRollbackCompleted, ReleaseRolledBack},
			},
		},
		{
			name: "RollbackFailed",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncCompleted, ReleaseActive},
				{ReleaseRollbackStarted, ReleaseRollingBack},
				{ReleaseRollbackFailed_, ReleaseRollbackFailed},
			},
		},
		{
			name: "RollbackTimeout",
			steps: []step{
				{ReleaseGitMirrorCompleted, ReleasePending},
				{ReleaseSyncCompleted, ReleaseActive},
				{ReleaseRollbackStarted, ReleaseRollingBack},
				{ReleaseRollbackTimeout, ReleaseRollbackFailed},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewReleaseMachine()
			state := ReleaseReady
			for _, s := range c.steps {
				state = m.Transition(state, s.event)
				assertState(t, state, s.expected)
			}
		})
	}
}

func TestReleaseMachine_InvalidEventsRejectFromNonActiveStates(t *testing.T) {
	m := NewReleaseMachine()

	// CANCELLED is the only truly terminal state.
	// SUPERSEDED and ROLLED_BACK accept Reactivated but reject other events.
	statesWithLimitedTransitions := []State{ReleaseSuperseded, ReleaseRolledBack, ReleaseCancelled}
	invalidEvents := []Event{
		ReleaseGitMirrorCompleted, ReleaseSyncCompleted,
		ReleaseNewActivated, ReleaseRollbackStarted,
	}

	for _, state := range statesWithLimitedTransitions {
		for _, event := range invalidEvents {
			assertPanics(t, func() {
				m.Transition(state, event)
			}, "expected panic for invalid event %q from state %q", event, state)
		}
	}
}

func TestReleaseMachine_IsTerminal(t *testing.T) {
	m := NewReleaseMachine()

	// SUPERSEDED and ROLLED_BACK are NOT terminal — they can be reactivated via rollback.
	nonTerminal := []State{
		ReleaseReady, ReleasePending, ReleaseActive,
		ReleaseGitMirrorFailed, ReleaseSyncFailed, ReleaseDriftAlert,
		ReleaseRollingBack, ReleaseRollbackFailed,
		ReleaseSuperseded, ReleaseRolledBack,
	}
	for _, s := range nonTerminal {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal", s)
		}
	}

	// CANCELLED + ARCHIVED are truly terminal (cannot be recovered).
	// ARCHIVED added by T4.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
	terminal := []State{ReleaseCancelled, ReleaseArchived}
	for _, s := range terminal {
		if !m.IsTerminal(s) {
			t.Errorf("state %q should be terminal", s)
		}
	}
}

func TestReleaseMachine_DriftAlertIsNotTerminal(t *testing.T) {
	// DRIFT_ALERT requires manual intervention but is NOT terminal per spec.
	// The spec says it requires manual intervention, but does not list it as terminal.
	// It has no outgoing transitions defined, so it is a sink state.
	m := NewReleaseMachine()
	if m.IsTerminal(ReleaseDriftAlert) {
		t.Error("DRIFT_ALERT should not be terminal (requires manual intervention, not terminal)")
	}
}

func TestReleaseMachine_RollbackFailedIsNotTerminal(t *testing.T) {
	// ROLLBACK_FAILED requires manual intervention but is NOT terminal per spec.
	m := NewReleaseMachine()
	if m.IsTerminal(ReleaseRollbackFailed) {
		t.Error("ROLLBACK_FAILED should not be terminal (requires manual intervention)")
	}
}

func TestReleaseMachine_Reactivation(t *testing.T) {
	m := NewReleaseMachine()

	t.Run("SUPERSEDED can be reactivated to ACTIVE", func(t *testing.T) {
		next := m.Transition(ReleaseSuperseded, ReleaseReactivated)
		if next != ReleaseActive {
			t.Errorf("expected ACTIVE, got %s", next)
		}
	})

	t.Run("ROLLED_BACK can be reactivated to ACTIVE", func(t *testing.T) {
		next := m.Transition(ReleaseRolledBack, ReleaseReactivated)
		if next != ReleaseActive {
			t.Errorf("expected ACTIVE, got %s", next)
		}
	})

	t.Run("CANCELLED cannot be reactivated", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for reactivating CANCELLED release")
			}
		}()
		m.Transition(ReleaseCancelled, ReleaseReactivated)
	})
}

func TestReleaseMachine_ManualResolve(t *testing.T) {
	m := NewReleaseMachine()

	t.Run("DRIFT_ALERT can be manually resolved to ACTIVE", func(t *testing.T) {
		next := m.Transition(ReleaseDriftAlert, ReleaseManualResolve)
		if next != ReleaseActive {
			t.Errorf("expected ACTIVE, got %s", next)
		}
	})

	t.Run("ROLLBACK_FAILED can be manually resolved to ACTIVE", func(t *testing.T) {
		next := m.Transition(ReleaseRollbackFailed, ReleaseManualResolve)
		if next != ReleaseActive {
			t.Errorf("expected ACTIVE, got %s", next)
		}
	})
}

func TestIsReleaseTerminal(t *testing.T) {
	m := NewReleaseMachine()

	// IsReleaseTerminal must be consistent with Machine.IsTerminal.
	// ARCHIVED added 2026-05-27 per T4.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
	allStates := []State{
		ReleaseReady, ReleasePending, ReleaseActive,
		ReleaseGitMirrorFailed, ReleaseSyncFailed, ReleaseDriftAlert,
		ReleaseRollingBack, ReleaseRollbackFailed,
		ReleaseSuperseded, ReleaseRolledBack, ReleaseCancelled,
		ReleaseArchived,
	}

	for _, s := range allStates {
		got := IsReleaseTerminal(s)
		want := m.IsTerminal(s)
		if got != want {
			t.Errorf("IsReleaseTerminal(%s) = %v, but Machine.IsTerminal = %v", s, got, want)
		}
	}

	// Unknown state should not be terminal.
	if IsReleaseTerminal("UNKNOWN") {
		t.Error("IsReleaseTerminal(UNKNOWN) should return false")
	}
}

func TestIsReleaseReapable(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{ReleaseReady, true},
		{ReleasePending, true},
		{ReleaseActive, false},
		{ReleaseSuperseded, false},
		{ReleaseRolledBack, false},
		{ReleaseCancelled, false},
		{ReleaseGitMirrorFailed, false},
		{ReleaseSyncFailed, false},
		{ReleaseDriftAlert, false},
		{ReleaseRollingBack, false},
		{ReleaseRollbackFailed, false},
		{ReleaseArchived, false}, // T4.5: terminal, not reapable
		{"UNKNOWN", false},
	}

	for _, tt := range tests {
		if got := IsReleaseReapable(tt.state); got != tt.want {
			t.Errorf("IsReleaseReapable(%s) = %v, want %v", tt.state, got, tt.want)
		}
	}
}

// TestReleaseMachine_ArchiveTransitions exercises T4.5 of plan
// 2026-05-26-maturity-3-62-to-4-0-roadmap — every non-terminal state that
// migration 000008 archives (SUPERSEDED/ROLLED_BACK/failed) must accept
// ReleaseArchiveEvent and land in ARCHIVED. CANCELLED is excluded because
// it is already terminal — its archive happens via raw SQL UPDATE.
func TestReleaseMachine_ArchiveTransitions(t *testing.T) {
	m := NewReleaseMachine()
	archivable := []State{
		ReleaseSuperseded,
		ReleaseRolledBack,
		ReleaseGitMirrorFailed,
		ReleaseSyncFailed,
		ReleaseRollbackFailed,
		ReleaseDriftAlert,
	}
	for _, from := range archivable {
		t.Run(string(from), func(t *testing.T) {
			next := m.Transition(from, ReleaseArchiveEvent)
			if next != ReleaseArchived {
				t.Errorf("Transition(%s, ReleaseArchiveEvent) = %s, want ARCHIVED", from, next)
			}
		})
	}
}

// TestReleaseMachine_ArchiveFromCancelled_Rejected — CANCELLED is terminal;
// archiver service must use raw UPDATE for it (per migration 000008
// approach). This test guards the spec invariant.
func TestReleaseMachine_ArchiveFromCancelled_Rejected(t *testing.T) {
	m := NewReleaseMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic archiving CANCELLED via Transition (terminal sink)")
		}
	}()
	m.Transition(ReleaseCancelled, ReleaseArchiveEvent)
}

// TestReleaseMachine_ArchiveFromActive_Rejected guarantees ACTIVE releases
// cannot be archived without first cycling through CANCELLED/SUPERSEDED.
func TestReleaseMachine_ArchiveFromActive_Rejected(t *testing.T) {
	m := NewReleaseMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic archiving ACTIVE release directly")
		}
	}()
	m.Transition(ReleaseActive, ReleaseArchiveEvent)
}

// TestReleaseMachine_ArchivedIsTerminal_NoReactivation enforces that
// ARCHIVED is a sink — no event recovers it.
func TestReleaseMachine_ArchivedIsTerminal_NoReactivation(t *testing.T) {
	m := NewReleaseMachine()
	if !m.IsTerminal(ReleaseArchived) {
		t.Fatal("ARCHIVED should be terminal")
	}
	// Try to reactivate — should panic (no transition out of ARCHIVED).
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on ARCHIVED -> Reactivated")
		}
	}()
	m.Transition(ReleaseArchived, ReleaseReactivated)
}

// TestReleaseMachine_LoadArchivedFromDB ensures that loading an ARCHIVED row
// from PG (via migration 000008 backfill) parses cleanly into the typed
// machine. EC-14 of edge-case-review.
func TestReleaseMachine_LoadArchivedFromDB(t *testing.T) {
	m := NewReleaseTypedMachine()
	// DB stores raw string "ARCHIVED" — MustParseState must not panic.
	phase := m.MustParseState("ARCHIVED")
	if phase != RPhArchived {
		t.Errorf("MustParseState(\"ARCHIVED\") = %s, want %s", phase, RPhArchived)
	}
	if !m.IsTerminal(phase) {
		t.Error("loaded ARCHIVED should be terminal")
	}
	if !m.IsKnownState(phase) {
		t.Error("loaded ARCHIVED should be known")
	}
}
