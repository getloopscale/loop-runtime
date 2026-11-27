// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// T3.6 / ARCH-002: ExposedRoute lifecycle state machine.
//
// String values match operators/api/v1.ExposedRoutePhase constants
// (Pending, CertIssued, Reachable, Failed) so a controller can cast
// the result of Transition directly to ExposedRoutePhase, matching
// the TenantMachine pattern.
//
// States:
//
//	Pending       — route created, certificate not yet issued
//	CertIssued    — TLS certificate is Ready
//	Reachable     — route probe succeeded; user-facing
//	Failed        — terminal failure on probe or cert (recoverable via Retry)
const (
	ExposedRouteEmpty      State = ""
	ExposedRoutePending    State = "Pending"
	ExposedRouteCertIssued State = "CertIssued"
	ExposedRouteReachable  State = "Reachable"
	ExposedRouteFailed     State = "Failed"
)

// ExposedRoute events.
const (
	ExposedRouteCreate    Event = "Create"    // initial reconcile
	ExposedRouteCertReady Event = "CertReady" // cert-manager Certificate ready
	ExposedRouteProbeOK   Event = "ProbeOK"   // route reachable check succeeded
	ExposedRouteProbeFail Event = "ProbeFail" // route reachable check failed
	ExposedRouteRetry     Event = "Retry"     // recover from Failed
)

// NewExposedRouteMachine returns the ExposedRoute state machine.
//
// Lifecycle: Empty → Pending → CertIssued → Reachable
// Recovery:  Failed → Pending (Retry)
// No terminal: ExposedRoute lives until parent CR (LoopRuntimeApplication) is deleted.
func NewExposedRouteMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "ExposedRoute",
		Transitions: []transition{
			// Initial creation
			{From: ExposedRouteEmpty, Event: ExposedRouteCreate, To: ExposedRoutePending},

			// Pending → CertIssued or Failed
			{From: ExposedRoutePending, Event: ExposedRouteCertReady, To: ExposedRouteCertIssued},
			{From: ExposedRoutePending, Event: ExposedRouteProbeFail, To: ExposedRouteFailed},

			// CertIssued → Reachable or Failed
			{From: ExposedRouteCertIssued, Event: ExposedRouteProbeOK, To: ExposedRouteReachable},
			{From: ExposedRouteCertIssued, Event: ExposedRouteProbeFail, To: ExposedRouteFailed},

			// Reachable → Failed (probe drift)
			{From: ExposedRouteReachable, Event: ExposedRouteProbeFail, To: ExposedRouteFailed},

			// Failed → Pending (retry on next reconcile)
			{From: ExposedRouteFailed, Event: ExposedRouteRetry, To: ExposedRoutePending},
		},
		// Audit gap M11 #26 fix: explicit empty Terminal set (instead of
		// omitting the field). ExposedRoute is intentionally non-terminal —
		// the CR is garbage-collected via ownerRef when its parent
		// LoopRuntimeApplication is deleted, so the lifecycle has no symbolic
		// "Cleaned" state. An explicit `[]State{}` makes the design intent
		// visible to readers and to anyone running parity checks against
		// the operator status reconcile, distinguishing "no terminal by
		// design" from "Terminal field forgotten".
		Terminal: []State{},
	})
}
