// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
	"time"
)

// TestBuildMachine_TransitionSequences exercises every valid BuildMachine
// transition chain starting from BuildQueued. Phase 3 helper: collapses 12
// near-clone test functions (HappyPath, BuildFails, QueuedBuildFails,
// CancelWhileQueued/Running, CancelTimeout, QueueTimeout, BuildTimeout,
// 4× Releasing*) each running the same Create+Transition+AssertState
// scaffold with different event sequences.
func TestBuildMachine_TransitionSequences(t *testing.T) {
	type step struct {
		event    Event
		expected State
	}
	cases := []struct {
		name  string
		steps []step // applied in order, starting from BuildQueued
	}{
		{
			name: "HappyPath_QueuedToSucceeded",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildSucceededEvent, BuildReleasing},
				{BuildReleaseCompleted, BuildSucceeded},
			},
		},
		{
			name: "BuildFails_RunningToFailed",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildFailedEvent, BuildFailed},
			},
		},
		{
			// REVIEW-BLOCKER-1: QUEUED → FAILED on submit error (no 5min wait).
			name:  "QueuedBuildFails_SubmitError",
			steps: []step{{BuildFailedEvent, BuildFailed}},
		},
		{
			name:  "CancelWhileQueued",
			steps: []step{{BuildCancelRequested, BuildCancelled}},
		},
		{
			name: "CancelWhileRunning",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildCancelRequested, BuildCancelling},
				{BuildCancelConfirmed, BuildCancelled},
			},
		},
		{
			name: "CancelTimeout",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildCancelRequested, BuildCancelling},
				{BuildCancelTimeout, BuildCancelled},
			},
		},
		{
			name:  "QueueTimeout",
			steps: []step{{BuildQueueTimeout, BuildFailed}},
		},
		{
			name: "BuildTimeout",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildTimeout, BuildFailed},
			},
		},
		{
			name: "ReleasingToSucceeded",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildSucceededEvent, BuildReleasing},
				{BuildReleaseCompleted, BuildSucceeded},
			},
		},
		{
			name: "ReleasingToFailed_ActivationError",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildSucceededEvent, BuildReleasing},
				{BuildReleaseFailedEvent, BuildFailed},
			},
		},
		{
			name: "ReleasingTimeout",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildSucceededEvent, BuildReleasing},
				{BuildReleaseTimeoutEvent, BuildFailed},
			},
		},
		{
			name: "ReleasingCancel",
			steps: []step{
				{BuildStarted, BuildRunning},
				{BuildSucceededEvent, BuildReleasing},
				{BuildCancelRequested, BuildCancelled},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewBuildMachine()
			state := BuildQueued
			for _, s := range c.steps {
				state = m.Transition(state, s.event)
				assertState(t, state, s.expected)
			}
		})
	}
}

func TestBuildMachine_TerminalStatesRejectAllEvents(t *testing.T) {
	m := NewBuildMachine()

	terminalStates := []State{BuildSucceeded, BuildFailed, BuildCancelled}
	allEvents := []Event{
		BuildStarted, BuildSucceededEvent, BuildFailedEvent,
		BuildCancelRequested, BuildCancelConfirmed,
		BuildQueueTimeout, BuildTimeout, BuildCancelTimeout,
		BuildReleaseCompleted, BuildReleaseFailedEvent, BuildReleaseTimeoutEvent,
	}

	for _, terminal := range terminalStates {
		for _, event := range allEvents {
			assertPanics(t, func() {
				m.Transition(terminal, event)
			}, "expected panic for transition from terminal state %q on event %q", terminal, event)
		}
	}
}

func TestBuildMachine_InvalidTransition_RunningToQueued(t *testing.T) {
	m := NewBuildMachine()

	assertPanics(t, func() {
		m.Transition(BuildRunning, BuildStarted)
	}, "RUNNING -> BuildStarted should panic (no RUNNING->QUEUED path)")
}

func TestBuildMachine_IsTerminal(t *testing.T) {
	m := NewBuildMachine()

	if m.IsTerminal(BuildQueued) {
		t.Error("QUEUED should not be terminal")
	}
	if m.IsTerminal(BuildRunning) {
		t.Error("RUNNING should not be terminal")
	}
	if m.IsTerminal(BuildReleasing) {
		t.Error("RELEASING should not be terminal")
	}
	if m.IsTerminal(BuildCancelling) {
		t.Error("CANCELLING should not be terminal")
	}
	if !m.IsTerminal(BuildSucceeded) {
		t.Error("SUCCEEDED should be terminal")
	}
	if !m.IsTerminal(BuildFailed) {
		t.Error("FAILED should be terminal")
	}
	if !m.IsTerminal(BuildCancelled) {
		t.Error("CANCELLED should be terminal")
	}
}

func TestBuildMachine_Timeouts(t *testing.T) {
	m := NewBuildMachine()
	timeouts := m.Timeouts()

	expected := map[State]time.Duration{
		BuildQueued:     5 * time.Minute,
		BuildRunning:    30 * time.Minute,
		BuildReleasing:  10 * time.Minute,
		BuildCancelling: 60 * time.Second,
	}

	if len(timeouts) != len(expected) {
		t.Fatalf("expected %d timeouts, got %d", len(expected), len(timeouts))
	}

	for _, tc := range timeouts {
		exp, ok := expected[tc.State]
		if !ok {
			t.Errorf("unexpected timeout for state %q", tc.State)
			continue
		}
		if tc.Timeout != exp {
			t.Errorf("timeout for %q: expected %v, got %v", tc.State, exp, tc.Timeout)
		}
	}
}

func TestBuildMachine_Name(t *testing.T) {
	m := NewBuildMachine()
	if m.Name() != "Build" {
		t.Errorf("expected name %q, got %q", "Build", m.Name())
	}
}

func TestIsBuildTerminal(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{BuildSucceeded, true},
		{BuildFailed, true},
		{BuildCancelled, true},
		{BuildQueued, false},
		{BuildRunning, false},
		{BuildReleasing, false},
		{BuildCancelling, false},
		{State("UNKNOWN"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := IsBuildTerminal(tt.state); got != tt.want {
				t.Errorf("IsBuildTerminal(%q) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
