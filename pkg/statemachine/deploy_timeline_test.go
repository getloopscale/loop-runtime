// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
)

// TestDeployTimeline_HappyPath_AllPhasesComplete drives the canonical
// build → sign → release → deploy → live ladder.
func TestDeployTimeline_HappyPath_AllPhasesComplete(t *testing.T) {
	s := NewDeployTimelineState()
	steps := []struct {
		phase  DeployTimelinePhase
		status DeployTimelinePhaseStatus
	}{
		{DTPhaseBuild, DTStatusInProgress},
		{DTPhaseBuild, DTStatusCompleted},
		{DTPhaseSign, DTStatusInProgress},
		{DTPhaseSign, DTStatusCompleted},
		{DTPhaseRelease, DTStatusInProgress},
		{DTPhaseRelease, DTStatusCompleted},
		{DTPhaseDeploy, DTStatusInProgress},
		{DTPhaseDeploy, DTStatusCompleted},
		{DTPhaseLive, DTStatusInProgress},
		{DTPhaseLive, DTStatusCompleted},
	}
	for _, step := range steps {
		if err := s.Transition(step.phase, step.status); err != nil {
			t.Fatalf("transition %s/%s failed: %v", step.phase, step.status, err)
		}
	}
	if !s.IsTerminal() {
		t.Fatalf("expected terminal state after live/completed; got phase=%s status=%s", s.Phase(), s.Status())
	}
}

// TestDeployTimeline_PanicsOnOutOfOrderPhase confirms invariant:
// skipping a phase is a programming error and MUST panic per loop-runtime-design.md.
func TestDeployTimeline_PanicsOnOutOfOrderPhase(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on out-of-order transition")
		}
	}()
	s := NewDeployTimelineState()
	// Skip Build → jump straight to Sign — invalid.
	_ = s.Transition(DTPhaseSign, DTStatusInProgress)
}

// TestDeployTimeline_AllowsDegradedInDeploy validates the recoverable
// DEGRADED substate per ADR D5: Deploy can flip degraded ↔ in_progress
// before reaching completed.
func TestDeployTimeline_AllowsDegradedInDeploy(t *testing.T) {
	s := NewDeployTimelineState()
	advanceTo(t, s, DTPhaseDeploy, DTStatusInProgress)
	if err := s.Transition(DTPhaseDeploy, DTStatusDegraded); err != nil {
		t.Fatalf("degraded should be allowed in deploy in_progress: %v", err)
	}
	if s.IsTerminal() {
		t.Fatalf("degraded is NOT terminal in deploy")
	}
	// Recover.
	if err := s.Transition(DTPhaseDeploy, DTStatusInProgress); err != nil {
		t.Fatalf("recovery from degraded should be allowed: %v", err)
	}
	if err := s.Transition(DTPhaseDeploy, DTStatusCompleted); err != nil {
		t.Fatalf("complete after recovery should be allowed: %v", err)
	}
}

// TestDeployTimeline_FailedAtAnyPhaseIsTerminal — failed status terminates
// regardless of which phase fires it.
func TestDeployTimeline_FailedAtAnyPhaseIsTerminal(t *testing.T) {
	phases := []DeployTimelinePhase{
		DTPhaseBuild, DTPhaseSign, DTPhaseRelease, DTPhaseDeploy, DTPhaseLive,
	}
	for _, ph := range phases {
		s := NewDeployTimelineState()
		advanceTo(t, s, ph, DTStatusInProgress)
		if err := s.Transition(ph, DTStatusFailed); err != nil {
			t.Fatalf("phase %s failed transition: %v", ph, err)
		}
		if !s.IsTerminal() {
			t.Fatalf("failed should be terminal for phase %s", ph)
		}
	}
}

// TestDeployTimeline_CanceledAtAnyPhaseIsTerminal mirrors failed semantics.
func TestDeployTimeline_CanceledAtAnyPhaseIsTerminal(t *testing.T) {
	phases := []DeployTimelinePhase{
		DTPhaseBuild, DTPhaseSign, DTPhaseRelease, DTPhaseDeploy,
	}
	for _, ph := range phases {
		s := NewDeployTimelineState()
		advanceTo(t, s, ph, DTStatusInProgress)
		if err := s.Transition(ph, DTStatusCanceled); err != nil {
			t.Fatalf("phase %s canceled transition: %v", ph, err)
		}
		if !s.IsTerminal() {
			t.Fatalf("canceled should be terminal for phase %s", ph)
		}
	}
}

// TestDeployTimeline_PhaseOrder ensures the canonical ordering.
func TestDeployTimeline_PhaseOrder(t *testing.T) {
	expect := []DeployTimelinePhase{
		DTPhaseBuild, DTPhaseSign, DTPhaseRelease, DTPhaseDeploy, DTPhaseLive,
	}
	got := CanonicalPhaseOrder()
	if len(got) != len(expect) {
		t.Fatalf("phase order length mismatch: got %d want %d", len(got), len(expect))
	}
	for i, p := range expect {
		if got[i] != p {
			t.Errorf("phase order[%d] = %s; want %s", i, got[i], p)
		}
	}
}

// advanceTo walks the canonical ladder to the requested phase+status.
func advanceTo(t *testing.T, s *DeployTimelineState, target DeployTimelinePhase, status DeployTimelinePhaseStatus) {
	t.Helper()
	for _, ph := range CanonicalPhaseOrder() {
		if ph == target {
			if err := s.Transition(ph, status); err != nil {
				t.Fatalf("advanceTo target %s/%s: %v", ph, status, err)
			}
			return
		}
		if err := s.Transition(ph, DTStatusInProgress); err != nil {
			t.Fatalf("advanceTo step %s in_progress: %v", ph, err)
		}
		if err := s.Transition(ph, DTStatusCompleted); err != nil {
			t.Fatalf("advanceTo step %s completed: %v", ph, err)
		}
	}
}
