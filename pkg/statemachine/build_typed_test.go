// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
)

// Phase 2 tests for ADR `2026-05-15-T6-1-typed-state-machines.md`.
// Validates that the Build typed wrapper exposes the same lifecycle
// as the untyped Machine while blocking cross-machine type confusion
// at compile time.

func TestBuildTypedMachine_HappyPath(t *testing.T) {
	m := NewBuildTypedMachine()

	// QUEUED → RUNNING
	next := m.Transition(BPhQueued, BPEventStarted)
	if next != BPhRunning {
		t.Fatalf("BuildStarted from QUEUED: want RUNNING, got %q", next)
	}

	// RUNNING → RELEASING
	next = m.Transition(BPhRunning, BPEventSucceeded)
	if next != BPhReleasing {
		t.Fatalf("BuildSucceeded from RUNNING: want RELEASING, got %q", next)
	}

	// RELEASING → SUCCEEDED
	next = m.Transition(BPhReleasing, BPEventReleaseCompleted)
	if next != BPhSucceeded {
		t.Fatalf("BuildReleaseCompleted from RELEASING: want SUCCEEDED, got %q", next)
	}
}

func TestBuildTypedMachine_Cancel(t *testing.T) {
	m := NewBuildTypedMachine()

	// QUEUED → CANCELLED (cancel before start)
	next := m.Transition(BPhQueued, BPEventCancelRequested)
	if next != BPhCancelled {
		t.Fatalf("CancelRequested from QUEUED: want CANCELLED, got %q", next)
	}

	// RUNNING → CANCELLING → CANCELLED
	next = m.Transition(BPhRunning, BPEventCancelRequested)
	if next != BPhCancelling {
		t.Fatalf("CancelRequested from RUNNING: want CANCELLING, got %q", next)
	}
	next = m.Transition(BPhCancelling, BPEventCancelConfirmed)
	if next != BPhCancelled {
		t.Fatalf("CancelConfirmed from CANCELLING: want CANCELLED, got %q", next)
	}
}

func TestBuildTypedMachine_Failures(t *testing.T) {
	m := NewBuildTypedMachine()

	// QUEUED → FAILED via timeout
	next := m.Transition(BPhQueued, BPEventQueueTimeout)
	if next != BPhFailed {
		t.Errorf("QueueTimeout: want FAILED, got %q", next)
	}

	// RUNNING → FAILED via timeout
	next = m.Transition(BPhRunning, BPEventTimeout)
	if next != BPhFailed {
		t.Errorf("Timeout from RUNNING: want FAILED, got %q", next)
	}

	// RELEASING → FAILED via release failure
	next = m.Transition(BPhReleasing, BPEventReleaseFailed)
	if next != BPhFailed {
		t.Errorf("ReleaseFailed: want FAILED, got %q", next)
	}

	// RELEASING → FAILED via release timeout
	next = m.Transition(BPhReleasing, BPEventReleaseTimeout)
	if next != BPhFailed {
		t.Errorf("ReleaseTimeout: want FAILED, got %q", next)
	}
}

func TestBuildTypedMachine_TryTransition_Invalid(t *testing.T) {
	m := NewBuildTypedMachine()
	// BuildStarted is not valid from SUCCEEDED (terminal).
	_, err := m.TryTransition(BPhSucceeded, BPEventStarted)
	if err == nil {
		t.Fatal("TryTransition SUCCEEDED+Started: expected error, got nil")
	}
}

func TestBuildTypedMachine_IsTerminal(t *testing.T) {
	m := NewBuildTypedMachine()
	terminalPhases := []BuildPhase{BPhSucceeded, BPhFailed, BPhCancelled}
	for _, p := range terminalPhases {
		if !m.IsTerminal(p) {
			t.Errorf("IsTerminal(%q) = false, want true", p)
		}
		if !IsBuildPhaseTerminal(p) {
			t.Errorf("IsBuildPhaseTerminal(%q) = false, want true", p)
		}
	}
	nonTerminalPhases := []BuildPhase{BPhQueued, BPhRunning, BPhReleasing, BPhCancelling}
	for _, p := range nonTerminalPhases {
		if m.IsTerminal(p) {
			t.Errorf("IsTerminal(%q) = true, want false", p)
		}
		if IsBuildPhaseTerminal(p) {
			t.Errorf("IsBuildPhaseTerminal(%q) = true, want false", p)
		}
	}
}

func TestBuildTypedMachine_Name(t *testing.T) {
	m := NewBuildTypedMachine()
	if m.Name() != "Build" {
		t.Errorf("Name() = %q, want %q", m.Name(), "Build")
	}
}

func TestBuildTypedMachine_MustParseState_PanicsOnUnknown(t *testing.T) {
	m := NewBuildTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustParseState(Bogus): expected panic, got none")
		}
	}()
	_ = m.MustParseState("Bogus")
}

func TestBuildTypedMachine_Timeouts(t *testing.T) {
	m := NewBuildTypedMachine()
	timeouts := m.Timeouts()
	if len(timeouts) != 4 {
		t.Fatalf("Timeouts: want 4 (Queued/Running/Releasing/Cancelling), got %d", len(timeouts))
	}
	// Verify typed timeout constants match the untyped originals.
	if BPTimeoutQueued != BuildQueuedTimeout {
		t.Errorf("BPTimeoutQueued != BuildQueuedTimeout")
	}
	if BPTimeoutRunning != BuildRunningTimeout {
		t.Errorf("BPTimeoutRunning != BuildRunningTimeout")
	}
	if BPTimeoutReleasing != BuildReleasingTimeout {
		t.Errorf("BPTimeoutReleasing != BuildReleasingTimeout")
	}
	if BPTimeoutCancelling != BuildCancellingTimeout {
		t.Errorf("BPTimeoutCancelling != BuildCancellingTimeout")
	}
}

func TestBuildTypedMachine_WireFormatCompatibility(t *testing.T) {
	// Critical contract: typed wire format MUST match untyped byte-
	// for-byte. DB rows persisted under untyped path round-trip
	// through typed path without translation.
	statePairs := []struct {
		typed   BuildPhase
		untyped State
	}{
		{BPhQueued, BuildQueued},
		{BPhRunning, BuildRunning},
		{BPhReleasing, BuildReleasing},
		{BPhSucceeded, BuildSucceeded},
		{BPhFailed, BuildFailed},
		{BPhCancelling, BuildCancelling},
		{BPhCancelled, BuildCancelled},
	}
	for _, p := range statePairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("state wire format drift: typed %q != untyped %q", p.typed, p.untyped)
		}
	}

	eventPairs := []struct {
		typed   BuildPhaseEvent
		untyped Event
	}{
		{BPEventStarted, BuildStarted},
		{BPEventSucceeded, BuildSucceededEvent},
		{BPEventReleaseCompleted, BuildReleaseCompleted},
		{BPEventReleaseFailed, BuildReleaseFailedEvent},
		{BPEventReleaseTimeout, BuildReleaseTimeoutEvent},
		{BPEventFailed, BuildFailedEvent},
		{BPEventCancelRequested, BuildCancelRequested},
		{BPEventCancelConfirmed, BuildCancelConfirmed},
		{BPEventQueueTimeout, BuildQueueTimeout},
		{BPEventTimeout, BuildTimeout},
		{BPEventCancelTimeout, BuildCancelTimeout},
	}
	for _, p := range eventPairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("event wire format drift: typed %q != untyped %q", p.typed, p.untyped)
		}
	}
}
