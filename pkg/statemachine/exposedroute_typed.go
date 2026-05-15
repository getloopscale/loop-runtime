// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 1 POC of ADR `2026-05-15-T6-1-typed-state-machines.md`
// (sponsor-ratified 2026-05-16):
//
// Per-machine typed wrappers around the shared *Machine substrate.
// Compile-time blocks `BuildMachine.Transition(ExposedRouteReachable, ...)`
// style cross-machine confusion that the shared `type State string`
// alone cannot prevent.
//
// Phase 1 scope: ExposedRoute ONLY (smallest blast radius — 5 states,
// no terminal, ~10 call sites in operators/internal/app/exposure_controller.go).
// Other 10 state machines (Build/Release/Tenant/Preview/Promotion/
// Rollback/Domain/Environment/Application/PlatformBootstrap) keep the
// untyped Machine signature until Phases 2-4 ship — they continue to
// import + use the package without any breakage.
//
// Coexistence design:
//   - Untyped `NewExposedRouteMachine() *Machine` is preserved for
//     back-compat. Existing callers continue to work.
//   - New `NewExposedRouteTypedMachine() *ExposedRouteMachine` wraps
//     the same machine spec but exposes typed Transition / TryTransition
//     / IsTerminal / IsKnownState / IsKnownEvent / MustParseState /
//     MustParseEvent / ValidEvents methods.
//   - The wrapper internally casts to/from the untyped State/Event so
//     the wire format (string values persisted to DB / CR status / SSE
//     events) is identical — no migration on existing data.
//
// After all 11 machines have typed wrappers (Phases 2-4), the untyped
// `Machine` type may be deleted in Phase 5, but that decision belongs
// to the future plan that ships the remaining 10 typed wrappers.

// ExposedRouteState is the typed state for the ExposedRoute lifecycle.
// The underlying string values are identical to the untyped State
// constants — DB persistence + CR status round-trip without
// translation.
type ExposedRouteState string

// ExposedRouteEvent is the typed event for the ExposedRoute lifecycle.
type ExposedRouteEvent string

// Typed state constants (mirror the untyped const block above).
const (
	XRStateEmpty      ExposedRouteState = ""
	XRStatePending    ExposedRouteState = "Pending"
	XRStateCertIssued ExposedRouteState = "CertIssued"
	XRStateReachable  ExposedRouteState = "Reachable"
	XRStateFailed     ExposedRouteState = "Failed"
)

// Typed event constants.
const (
	XREventCreate    ExposedRouteEvent = "Create"
	XREventCertReady ExposedRouteEvent = "CertReady"
	XREventProbeOK   ExposedRouteEvent = "ProbeOK"
	XREventProbeFail ExposedRouteEvent = "ProbeFail"
	XREventRetry     ExposedRouteEvent = "Retry"
)

// ExposedRouteMachine is the typed wrapper around the shared *Machine
// substrate. All operations are delegated to the underlying machine
// after a same-bit-pattern cast to/from the untyped State/Event types.
type ExposedRouteMachine struct {
	inner *Machine
}

// NewExposedRouteTypedMachine constructs the typed ExposedRoute state
// machine. Internally reuses NewExposedRouteMachine() so the transition
// table + terminal set stay single-sourced — there is exactly ONE spec
// for ExposedRoute, exposed via two surfaces (typed + untyped).
func NewExposedRouteTypedMachine() *ExposedRouteMachine {
	return &ExposedRouteMachine{inner: NewExposedRouteMachine()}
}

// Transition runs a typed transition. Panics on invalid input,
// matching the untyped Machine.Transition contract.
func (m *ExposedRouteMachine) Transition(current ExposedRouteState, event ExposedRouteEvent) ExposedRouteState {
	return ExposedRouteState(m.inner.Transition(State(current), Event(event)))
}

// TryTransition is the non-panicking variant — same contract as
// Machine.TryTransition but typed.
func (m *ExposedRouteMachine) TryTransition(current ExposedRouteState, event ExposedRouteEvent) (ExposedRouteState, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return ExposedRouteState(next), nil
}

// IsTerminal returns whether the given state is terminal. ExposedRoute
// has no terminal states by design (lifecycle bound to parent CR
// ownerRef), so this always returns false — but exposed for API
// uniformity with other typed machines.
func (m *ExposedRouteMachine) IsTerminal(s ExposedRouteState) bool {
	return m.inner.IsTerminal(State(s))
}

// Name returns "ExposedRoute".
func (m *ExposedRouteMachine) Name() string {
	return m.inner.Name()
}

// IsKnownState mirrors Machine.IsKnownState in the typed surface.
func (m *ExposedRouteMachine) IsKnownState(s ExposedRouteState) bool {
	return m.inner.IsKnownState(State(s))
}

// IsKnownEvent mirrors Machine.IsKnownEvent in the typed surface.
func (m *ExposedRouteMachine) IsKnownEvent(e ExposedRouteEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

// MustParseState wraps a raw string into an ExposedRouteState after
// validating membership. Panics on unknown values — the typed
// equivalent of Machine.MustParseState for callers reading from DB /
// CR status fields.
func (m *ExposedRouteMachine) MustParseState(s string) ExposedRouteState {
	return ExposedRouteState(m.inner.MustParseState(s))
}

// MustParseEvent wraps a raw string into an ExposedRouteEvent after
// validating membership.
func (m *ExposedRouteMachine) MustParseEvent(e string) ExposedRouteEvent {
	return ExposedRouteEvent(m.inner.MustParseEvent(e))
}

// ValidEvents returns the typed events valid from the given state.
func (m *ExposedRouteMachine) ValidEvents(state ExposedRouteState) []ExposedRouteEvent {
	untyped := m.inner.ValidEvents(State(state))
	if untyped == nil {
		return nil
	}
	out := make([]ExposedRouteEvent, len(untyped))
	for i, e := range untyped {
		out[i] = ExposedRouteEvent(e)
	}
	return out
}
