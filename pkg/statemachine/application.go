// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// T3.6 / ARCH-002: LoopZeroApplication lifecycle state machine.
//
// String values match operators/api/v1.ApplicationPhase constants
// (Progressing, Serving, Reachable, Degraded, Down, Cleaned) so the
// controller can cast Transition results directly to ApplicationPhase.
//
// Unlike most LoopZero state machines (Build, Tenant, Preview), the
// Application machine is CYCLIC — apps can move Reachable → Degraded
// → Reachable across many reconciles in their lifetime. The Cleaned
// state IS terminal (added 2026-05-03 by fix-all-platform-issues-plan
// T2.2 reaper); it represents an orphan CR garbage-collected by the
// application_reaper after grace period when the GitOps source no
// longer carries the manifest.
//
// States:
//
//	Progressing — newly created or rolling update in progress
//	Serving     — endpoints ready, ingress configured, route NOT yet reachable
//	Reachable   — route probe succeeded; the app is user-facing
//	Degraded    — was Reachable, now route probe fails; recoverable
//	Down        — was Serving/Reachable, endpoints dropped to zero
//	Cleaned     — terminal: GitOps source removed manifest; CR scheduled
//	               for finalizer removal by reaper (no further reconcile).
const (
	ApplicationEmpty       State = ""
	ApplicationProgressing State = "Progressing"
	ApplicationServing     State = "Serving"
	ApplicationReachable   State = "Reachable"
	ApplicationDegraded    State = "Degraded"
	ApplicationDown        State = "Down"
	ApplicationCleaned     State = "Cleaned"
)

// LoopZeroApplication events.
const (
	ApplicationCreate         Event = "Create"         // initial reconcile
	ApplicationEndpointsReady Event = "EndpointsReady" // Service has ≥1 ready endpoint
	ApplicationEndpointsLost  Event = "EndpointsLost"  // Service has 0 ready endpoints
	ApplicationRouteOK        Event = "RouteOK"        // ExposedRoute is Reachable
	ApplicationRouteFail      Event = "RouteFail"      // ExposedRoute probe failed
	ApplicationUpdate         Event = "Update"         // Spec changed, rolling update
	ApplicationOrphaned       Event = "Orphaned"       // The manifest left the GitOps source; no code emits it since the reaper was deleted
)

// NewApplicationMachine returns the LoopZeroApplication state machine.
//
// Lifecycle (typical):
//
//	Empty → Progressing → Serving → Reachable
//
// Cycles:
//
//	Reachable ↔ Degraded (probe flapping)
//	Reachable/Serving → Down (all endpoints lost)
//	Down → Progressing (recovery via reconcile)
//	Reachable/Degraded → Progressing (Update event, rolling deploy)
//
// Terminal: Cleaned — reached on ApplicationOrphaned; nothing transitions out
// of it. The reaper that emitted Orphaned was deleted because nothing ever
// started it, so today no code reaches Cleaned; the edges stay declared until
// deciding who owns orphan cleanup replaces or removes them.
func NewApplicationMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "LoopZeroApplication",
		Transitions: []transition{
			// Initial creation
			{From: ApplicationEmpty, Event: ApplicationCreate, To: ApplicationProgressing},

			// Progressing → Serving (endpoints ready)
			{From: ApplicationProgressing, Event: ApplicationEndpointsReady, To: ApplicationServing},
			{From: ApplicationProgressing, Event: ApplicationEndpointsLost, To: ApplicationDown},

			// Serving → Reachable (route probe ok)
			{From: ApplicationServing, Event: ApplicationRouteOK, To: ApplicationReachable},
			{From: ApplicationServing, Event: ApplicationEndpointsLost, To: ApplicationDown},
			{From: ApplicationServing, Event: ApplicationUpdate, To: ApplicationProgressing},

			// Reachable: probe flapping → Degraded; endpoints loss → Down
			{From: ApplicationReachable, Event: ApplicationRouteFail, To: ApplicationDegraded},
			{From: ApplicationReachable, Event: ApplicationEndpointsLost, To: ApplicationDown},
			{From: ApplicationReachable, Event: ApplicationUpdate, To: ApplicationProgressing},

			// Degraded → Reachable (probe recovered) or Down (endpoints lost)
			{From: ApplicationDegraded, Event: ApplicationRouteOK, To: ApplicationReachable},
			{From: ApplicationDegraded, Event: ApplicationEndpointsLost, To: ApplicationDown},
			{From: ApplicationDegraded, Event: ApplicationUpdate, To: ApplicationProgressing},

			// Down → Progressing (recovery — endpoints came back)
			{From: ApplicationDown, Event: ApplicationEndpointsReady, To: ApplicationProgressing},
			{From: ApplicationDown, Event: ApplicationUpdate, To: ApplicationProgressing},

			// Orphan cleanup (fix-all-platform-issues-plan T2.2): allowed from
			// EVERY non-terminal state because an app can be orphaned at any
			// point in its lifecycle (e.g., removed from the gitops repo while
			// Reachable). No emitter today — see the Terminal note above.
			{From: ApplicationProgressing, Event: ApplicationOrphaned, To: ApplicationCleaned},
			{From: ApplicationServing, Event: ApplicationOrphaned, To: ApplicationCleaned},
			{From: ApplicationReachable, Event: ApplicationOrphaned, To: ApplicationCleaned},
			{From: ApplicationDegraded, Event: ApplicationOrphaned, To: ApplicationCleaned},
			{From: ApplicationDown, Event: ApplicationOrphaned, To: ApplicationCleaned},
		},
		Terminal: []State{ApplicationCleaned},
	})
}
