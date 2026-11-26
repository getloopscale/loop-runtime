// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package statemachine — Deploy Timeline 5-phase canonical state machine.
//
// Plan ADR D1: single linear top-level FSM (Build → Sign → Release →
// Deploy → Live) with per-phase substate refinement only in Deploy
// (sync_status + health_status — handled at the aggregator layer, NOT
// here). The state machine enforces phase ordering monotonically:
// invalid transitions panic per `loopruntime-design.md § State machines are
// strict`. The aggregator owns its instance — concurrent access is a
// programming bug.
package statemachine

import "fmt"

// DeployTimelinePhase enumerates the 5 canonical phases.
type DeployTimelinePhase string

const (
	DTPhaseBuild   DeployTimelinePhase = "build"
	DTPhaseSign    DeployTimelinePhase = "sign"
	DTPhaseRelease DeployTimelinePhase = "release"
	DTPhaseDeploy  DeployTimelinePhase = "deploy"
	DTPhaseLive    DeployTimelinePhase = "live"
)

// DeployTimelinePhaseStatus enumerates per-phase status values.
type DeployTimelinePhaseStatus string

const (
	DTStatusReady      DeployTimelinePhaseStatus = "ready"
	DTStatusInProgress DeployTimelinePhaseStatus = "in_progress"
	DTStatusCompleted  DeployTimelinePhaseStatus = "completed"
	DTStatusFailed     DeployTimelinePhaseStatus = "failed"
	DTStatusCanceled   DeployTimelinePhaseStatus = "canceled"
	DTStatusDegraded   DeployTimelinePhaseStatus = "degraded" // Deploy phase only (recoverable)
)

// canonicalPhaseOrder is the immutable ladder. Each phase MUST reach a
// terminal status (completed/failed/canceled) before its successor may
// enter in_progress. DEGRADED is recoverable in Deploy only.
var canonicalPhaseOrder = []DeployTimelinePhase{
	DTPhaseBuild, DTPhaseSign, DTPhaseRelease, DTPhaseDeploy, DTPhaseLive,
}

// CanonicalPhaseOrder returns a copy of the immutable phase ladder.
func CanonicalPhaseOrder() []DeployTimelinePhase {
	out := make([]DeployTimelinePhase, len(canonicalPhaseOrder))
	copy(out, canonicalPhaseOrder)
	return out
}

// phaseIndex returns the position of a phase in the canonical ladder.
// Unknown phase returns -1.
func phaseIndex(p DeployTimelinePhase) int {
	for i, x := range canonicalPhaseOrder {
		if x == p {
			return i
		}
	}
	return -1
}

// DeployTimelineState is the per-deploy state machine instance owned by
// one aggregator goroutine. NOT thread-safe by design — concurrent
// access is a programming bug per plan T2.1 documented invariant.
type DeployTimelineState struct {
	phase    DeployTimelinePhase
	status   DeployTimelinePhaseStatus
	terminal bool
}

// NewDeployTimelineState returns a fresh state at Build/ready.
func NewDeployTimelineState() *DeployTimelineState {
	return &DeployTimelineState{
		phase:  DTPhaseBuild,
		status: DTStatusReady,
	}
}

// Phase returns the current phase.
func (s *DeployTimelineState) Phase() DeployTimelinePhase { return s.phase }

// Status returns the current status.
func (s *DeployTimelineState) Status() DeployTimelinePhaseStatus { return s.status }

// IsTerminal reports whether the state machine has reached a final state.
// Terminal cases: live/completed (happy), any phase /failed, any
// non-Live phase /canceled.
func (s *DeployTimelineState) IsTerminal() bool { return s.terminal }

// Transition advances the state machine. Per `loopruntime-design.md` state
// machines are strict: invalid transitions PANIC (programming error,
// NOT runtime error). The aggregator goroutine wraps Transition() calls
// in `defer recover()` so a panic converts to an ERROR log + DEGRADED
// emission + goroutine exit (plan § Drawbacks & Risks).
func (s *DeployTimelineState) Transition(target DeployTimelinePhase, status DeployTimelinePhaseStatus) error {
	if s.terminal {
		panic(fmt.Sprintf("deploy_timeline: transition from terminal state phase=%s status=%s",
			s.phase, s.status))
	}
	targetIdx := phaseIndex(target)
	if targetIdx < 0 {
		panic(fmt.Sprintf("deploy_timeline: unknown phase %q", target))
	}
	currentIdx := phaseIndex(s.phase)
	// Cross-phase transition rules: target must be either the current
	// phase (substate change) or the immediate successor (after current
	// reached a terminal status).
	switch targetIdx {
	case currentIdx:
		// Substate transition within current phase.
		if !validStatusTransition(s.status, status, target) {
			panic(fmt.Sprintf("deploy_timeline: invalid status transition %s/%s → %s",
				s.phase, s.status, status))
		}
	case currentIdx + 1:
		// Advance to next phase — current must have completed.
		if s.status != DTStatusCompleted {
			panic(fmt.Sprintf("deploy_timeline: cannot advance %s → %s while current status=%s",
				s.phase, target, s.status))
		}
		if status != DTStatusInProgress && status != DTStatusReady {
			panic(fmt.Sprintf("deploy_timeline: phase %s must enter as ready/in_progress, not %s",
				target, status))
		}
	default:
		// Out-of-order phase jump — invalid.
		panic(fmt.Sprintf("deploy_timeline: out-of-order phase jump %s → %s",
			s.phase, target))
	}
	s.phase = target
	s.status = status
	// Compute terminality.
	switch status {
	case DTStatusFailed, DTStatusCanceled:
		s.terminal = true
	case DTStatusCompleted:
		// live/completed = happy terminal; other completed advances to next.
		if target == DTPhaseLive {
			s.terminal = true
		}
	}
	return nil
}

// validStatusTransition encodes the allowed within-phase status
// transitions. Deploy phase additionally permits ↔ DEGRADED for
// recovery (ADR D5).
func validStatusTransition(from, to DeployTimelinePhaseStatus, phase DeployTimelinePhase) bool {
	switch from {
	case DTStatusReady:
		return to == DTStatusInProgress || to == DTStatusFailed || to == DTStatusCanceled
	case DTStatusInProgress:
		switch to {
		case DTStatusCompleted, DTStatusFailed, DTStatusCanceled:
			return true
		case DTStatusDegraded:
			return phase == DTPhaseDeploy
		}
	case DTStatusDegraded:
		// Recovery to in_progress, or terminal failure.
		if phase != DTPhaseDeploy {
			return false
		}
		return to == DTStatusInProgress || to == DTStatusFailed || to == DTStatusCanceled
	}
	return false
}
