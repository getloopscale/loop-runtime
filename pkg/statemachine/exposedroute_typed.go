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
	XRStateEmpty      = ExposedRouteState(ExposedRouteEmpty)
	XRStatePending    = ExposedRouteState(ExposedRoutePending)
	XRStateCertIssued = ExposedRouteState(ExposedRouteCertIssued)
	XRStateReachable  = ExposedRouteState(ExposedRouteReachable)
	XRStateFailed     = ExposedRouteState(ExposedRouteFailed)
)

// Typed event constants.
const (
	XREventCreate    = ExposedRouteEvent(ExposedRouteCreate)
	XREventCertReady = ExposedRouteEvent(ExposedRouteCertReady)
	XREventProbeOK   = ExposedRouteEvent(ExposedRouteProbeOK)
	XREventProbeFail = ExposedRouteEvent(ExposedRouteProbeFail)
	XREventRetry     = ExposedRouteEvent(ExposedRouteRetry)
)

// ExposedRouteMachine is the typed ExposedRouteState machine.
type ExposedRouteMachine = Typed[ExposedRouteState, ExposedRouteEvent]

// NewExposedRouteTypedMachine returns the typed machine over NewExposedRouteMachine(), which stays the
// single source of the transition table and the terminal set.
func NewExposedRouteTypedMachine() *ExposedRouteMachine {
	return newTyped[ExposedRouteState, ExposedRouteEvent](NewExposedRouteMachine())
}
