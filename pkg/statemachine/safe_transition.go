// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 5 preparation work for ADR
// `2026-05-15-T6-1-typed-state-machines.md`:
//
// Adds typed `SafeTransition(current, event) (newState, error)`
// methods to every typed wrapper. SafeTransition catches the panic
// raised by the underlying Machine.Transition on invalid transitions
// and converts it to a sentinel `ErrInvalidTransition` for callers
// that prefer error-returning semantics over the strict panic-on-bug
// design (e.g., callers that read state from external sources like
// DB rows or CR status fields that may have been manually edited).
//
// Why a single file (vs one method per wrapper file): the 11 methods
// are mechanically identical — same recover-and-convert pattern with
// only the typed parameter signature differing. Co-locating keeps
// the maintenance surface minimal; adding a 12th wrapper in the
// future is a 4-line append here, not a new method scattered across
// a separate file.
//
// Callers that previously hand-rolled a `safeTransition` local
// helper around `*Machine` (e.g., `api/internal/db/build_store.go`)
// can now switch to the typed wrapper + use this method directly —
// the migration is mechanical and preserves wire format.

import "errors"

// ErrInvalidTransition is returned by every typed wrapper's
// SafeTransition method when the underlying Machine.Transition would
// have panicked (unknown source state OR event not valid from source
// state OR source state is terminal). Callers can wrap this error
// with context-specific information.
var ErrInvalidTransition = errors.New("statemachine: invalid transition")

// safeRecover converts a Machine.Transition panic into a typed
// error. Internal helper used by every typed wrapper's
// SafeTransition method.
func safeRecover(perr *error) {
	if r := recover(); r != nil {
		*perr = ErrInvalidTransition
	}
}

// --- ExposedRoute ---

// SafeTransition is the panic-safe variant of Transition. Returns
// ErrInvalidTransition on invalid input instead of panicking.
func (m *ExposedRouteMachine) SafeTransition(current ExposedRouteState, event ExposedRouteEvent) (next ExposedRouteState, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Build ---

// SafeTransition is the panic-safe variant of Transition.
func (m *BuildMachine) SafeTransition(current BuildPhase, event BuildPhaseEvent) (next BuildPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Release ---

// SafeTransition is the panic-safe variant of Transition.
func (m *ReleaseMachine) SafeTransition(current ReleasePhase, event ReleasePhaseEvent) (next ReleasePhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Tenant ---

// SafeTransition is the panic-safe variant of Transition.
func (m *TenantTypedMachine) SafeTransition(current TenantPhase, event TenantPhaseEvent) (next TenantPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Environment ---

// SafeTransition is the panic-safe variant of Transition.
func (m *EnvironmentTypedMachine) SafeTransition(current EnvironmentPhase, event EnvironmentPhaseEvent) (next EnvironmentPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- LoopZeroApplication ---

// SafeTransition is the panic-safe variant of Transition.
func (m *ApplicationTypedMachine) SafeTransition(current ApplicationPhase, event ApplicationPhaseEvent) (next ApplicationPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Domain ---

// SafeTransition is the panic-safe variant of Transition.
func (m *DomainTypedMachine) SafeTransition(current DomainPhase, event DomainPhaseEvent) (next DomainPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- PreviewEnvironment ---

// SafeTransition is the panic-safe variant of Transition.
func (m *PreviewTypedMachine) SafeTransition(current PreviewPhase, event PreviewPhaseEvent) (next PreviewPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Promotion ---

// SafeTransition is the panic-safe variant of Transition.
func (m *PromotionTypedMachine) SafeTransition(current PromotionPhase, event PromotionPhaseEvent) (next PromotionPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- Rollback ---

// SafeTransition is the panic-safe variant of Transition.
func (m *RollbackTypedMachine) SafeTransition(current RollbackPhase, event RollbackPhaseEvent) (next RollbackPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}

// --- PlatformBootstrap ---

// SafeTransition is the panic-safe variant of Transition.
func (m *PlatformBootstrapTypedMachine) SafeTransition(current PlatformBootstrapPhase, event PlatformBootstrapPhaseEvent) (next PlatformBootstrapPhase, err error) {
	defer safeRecover(&err)
	next = m.Transition(current, event)
	return next, nil
}
