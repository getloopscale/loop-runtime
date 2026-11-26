// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package conditions

// Phase is the canonical lifecycle phase derived from a Condition slice.
// It replaces the deleted pkg/health.Phase enum (ADR-0028) with
// a kstatus-aware mapping that respects observedGeneration discipline.
//
// Per ADR-0032 (canonical replacement for pkg/health).
type Phase string

const (
	// PhasePending — controller has not yet observed the spec generation
	// (no conditions OR observedGeneration < currentGeneration on every
	// True condition).
	PhasePending Phase = "Pending"
	// PhaseProvisioning — controller is in progress (Reconciling=True,
	// Stalled=False).
	PhaseProvisioning Phase = "Provisioning"
	// PhaseReady — fully reconciled at the current generation
	// (Ready=True, Stalled=False).
	PhaseReady Phase = "Ready"
	// PhaseStalled — manual intervention required (Stalled=True).
	// Note: this maps to what pkg/health called PhaseDegraded; the
	// kstatus vocabulary uses Stalled to align with kpt convention.
	PhaseStalled Phase = "Stalled"
)

// AggregateToPhase maps a Condition slice + observedGeneration +
// currentGeneration to a single Phase. Designed for status reporting
// surfaces (e.g., `loopruntime status`, ArgoCD health, top-level CR phase
// fields) that want a single string instead of three booleans.
//
// Algorithm:
//
//   - observedGeneration < currentGeneration → PhasePending (the
//     controller has not yet acted on the latest spec; trumps any
//     stale True condition).
//   - Stalled=True → PhaseStalled.
//   - Ready=True → PhaseReady.
//   - Reconciling=True → PhaseProvisioning.
//   - Otherwise → PhasePending.
//
// Backward-compat note: callers migrating from the deleted pkg/health
// can map PhaseStalled → PhaseDegraded if they still emit the legacy
// vocabulary in user-facing surfaces.
func AggregateToPhase(conds []Condition, observedGeneration, currentGeneration int64) Phase {
	if observedGeneration < currentGeneration {
		return PhasePending
	}
	if IsConditionTrue(conds, TypeStalled) {
		return PhaseStalled
	}
	if IsConditionTrue(conds, TypeReady) {
		return PhaseReady
	}
	if IsConditionTrue(conds, TypeReconciling) {
		return PhaseProvisioning
	}
	return PhasePending
}
