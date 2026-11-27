// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
)

// Phase 2 tests for ADR `2026-05-15-T6-1-typed-state-machines.md`
// (Release sibling to build_typed_test.go).

func TestReleaseTypedMachine_HappyPath(t *testing.T) {
	m := NewReleaseTypedMachine()

	// READY → PENDING (git mirror completed)
	next := m.Transition(RPhReady, RPEventGitMirrorCompleted)
	if next != RPhPending {
		t.Fatalf("GitMirrorCompleted from READY: want PENDING, got %q", next)
	}

	// PENDING → ACTIVE (sync completed)
	next = m.Transition(RPhPending, RPEventSyncCompleted)
	if next != RPhActive {
		t.Fatalf("SyncCompleted from PENDING: want ACTIVE, got %q", next)
	}

	// ACTIVE → SUPERSEDED (new release activated)
	next = m.Transition(RPhActive, RPEventNewActivated)
	if next != RPhSuperseded {
		t.Fatalf("NewActivated from ACTIVE: want SUPERSEDED, got %q", next)
	}
}

func TestReleaseTypedMachine_RollbackFlow(t *testing.T) {
	m := NewReleaseTypedMachine()

	// ACTIVE → ROLLING_BACK
	next := m.Transition(RPhActive, RPEventRollbackStarted)
	if next != RPhRollingBack {
		t.Fatalf("RollbackStarted: want ROLLING_BACK, got %q", next)
	}

	// ROLLING_BACK → ROLLED_BACK
	next = m.Transition(RPhRollingBack, RPEventRollbackCompleted)
	if next != RPhRolledBack {
		t.Fatalf("RollbackCompleted: want ROLLED_BACK, got %q", next)
	}

	// ROLLED_BACK → ACTIVE (reactivated)
	next = m.Transition(RPhRolledBack, RPEventReactivated)
	if next != RPhActive {
		t.Fatalf("Reactivated: want ACTIVE, got %q", next)
	}
}

func TestReleaseTypedMachine_ProjectDeleted_AllStates(t *testing.T) {
	m := NewReleaseTypedMachine()
	// Project-delete cascade: every non-terminal state goes to CANCELLED.
	nonTerminalStates := []ReleasePhase{
		RPhReady, RPhPending, RPhActive, RPhSuperseded, RPhRolledBack,
		RPhRollingBack, RPhGitMirrorFailed, RPhSyncFailed,
		RPhRollbackFailed, RPhDriftAlert,
	}
	for _, s := range nonTerminalStates {
		next := m.Transition(s, RPEventProjectDeleted)
		if next != RPhCancelled {
			t.Errorf("ProjectDeleted from %q: want CANCELLED, got %q", s, next)
		}
	}
}

func TestReleaseTypedMachine_IsTerminal_OnlyCancelled(t *testing.T) {
	m := NewReleaseTypedMachine()
	if !m.IsTerminal(RPhCancelled) {
		t.Error("IsTerminal(CANCELLED) = false, want true")
	}
	if !IsReleasePhaseTerminal(RPhCancelled) {
		t.Error("IsReleasePhaseTerminal(CANCELLED) = false, want true")
	}
	// SUPERSEDED and ROLLED_BACK are NOT terminal (can reactivate).
	if m.IsTerminal(RPhSuperseded) {
		t.Error("IsTerminal(SUPERSEDED) = true, want false")
	}
	if m.IsTerminal(RPhRolledBack) {
		t.Error("IsTerminal(ROLLED_BACK) = true, want false")
	}
}

func TestReleaseTypedMachine_IsReapable(t *testing.T) {
	if !IsReleasePhaseReapable(RPhReady) {
		t.Error("READY should be reapable")
	}
	if !IsReleasePhaseReapable(RPhPending) {
		t.Error("PENDING should be reapable")
	}
	for _, s := range []ReleasePhase{RPhActive, RPhSuperseded, RPhRolledBack, RPhCancelled} {
		if IsReleasePhaseReapable(s) {
			t.Errorf("%q should NOT be reapable", s)
		}
	}
}

func TestReleaseTypedMachine_Name(t *testing.T) {
	m := NewReleaseTypedMachine()
	if m.Name() != "Release" {
		t.Errorf("Name() = %q, want %q", m.Name(), "Release")
	}
}

func TestReleaseTypedMachine_Timeouts(t *testing.T) {
	m := NewReleaseTypedMachine()
	timeouts := m.Timeouts()
	if len(timeouts) != 3 {
		t.Fatalf("Timeouts: want 3, got %d", len(timeouts))
	}
	if RPTimeoutReady != ReleaseReadyTimeout {
		t.Errorf("RPTimeoutReady != ReleaseReadyTimeout")
	}
	if RPTimeoutPending != ReleasePendingTimeout {
		t.Errorf("RPTimeoutPending != ReleasePendingTimeout")
	}
	if RPTimeoutRollingBack != ReleaseRollingBackTimeout {
		t.Errorf("RPTimeoutRollingBack != ReleaseRollingBackTimeout")
	}
}

func TestReleaseTypedMachine_WireFormatCompatibility(t *testing.T) {
	statePairs := []struct {
		typed   ReleasePhase
		untyped State
	}{
		{RPhReady, ReleaseReady},
		{RPhPending, ReleasePending},
		{RPhActive, ReleaseActive},
		{RPhGitMirrorFailed, ReleaseGitMirrorFailed},
		{RPhSyncFailed, ReleaseSyncFailed},
		{RPhDriftAlert, ReleaseDriftAlert},
		{RPhSuperseded, ReleaseSuperseded},
		{RPhRollingBack, ReleaseRollingBack},
		{RPhRolledBack, ReleaseRolledBack},
		{RPhRollbackFailed, ReleaseRollbackFailed},
		{RPhCancelled, ReleaseCancelled},
	}
	for _, p := range statePairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("state wire format drift: typed %q != untyped %q", p.typed, p.untyped)
		}
	}

	eventPairs := []struct {
		typed   ReleasePhaseEvent
		untyped Event
	}{
		{RPEventGitMirrorCompleted, ReleaseGitMirrorCompleted},
		{RPEventGitMirrorFailed, ReleaseGitMirrorFailed_},
		{RPEventGitMirrorRetried, ReleaseGitMirrorRetried},
		{RPEventSyncCompleted, ReleaseSyncCompleted},
		{RPEventSyncFailed, ReleaseSyncFailed_},
		{RPEventSyncExhausted, ReleaseSyncExhausted},
		{RPEventSyncRetry, ReleaseSyncRetry},
		{RPEventNewActivated, ReleaseNewActivated},
		{RPEventRollbackStarted, ReleaseRollbackStarted},
		{RPEventRollbackCompleted, ReleaseRollbackCompleted},
		{RPEventRollbackFailed, ReleaseRollbackFailed_},
		{RPEventCancelled, ReleaseCancelledEvent},
		{RPEventGitPushTimeout, ReleaseGitPushTimeout},
		{RPEventSyncTimeout, ReleaseSyncTimeout},
		{RPEventRollbackTimeout, ReleaseRollbackTimeout},
		{RPEventManualResolve, ReleaseManualResolve},
		{RPEventReactivated, ReleaseReactivated},
		{RPEventProjectDeleted, ReleaseProjectDeleted},
	}
	for _, p := range eventPairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("event wire format drift: typed %q != untyped %q", p.typed, p.untyped)
		}
	}
}
