// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package conditions

import "testing"

func TestAggregateToPhase_PendingWhenObservedLess(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusTrue},
	}
	got := AggregateToPhase(conds, 1, 2)
	if got != PhasePending {
		t.Errorf("expected PhasePending when observedGen < currentGen; got %q", got)
	}
}

func TestAggregateToPhase_StalledTrueDominates(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusTrue},
		{Type: TypeStalled, Status: StatusTrue},
	}
	got := AggregateToPhase(conds, 2, 2)
	if got != PhaseStalled {
		t.Errorf("Stalled=True must dominate Ready=True; got %q", got)
	}
}

func TestAggregateToPhase_ReadyMapsToReady(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusTrue},
		{Type: TypeReconciling, Status: StatusFalse},
		{Type: TypeStalled, Status: StatusFalse},
	}
	got := AggregateToPhase(conds, 2, 2)
	if got != PhaseReady {
		t.Errorf("Ready=True with no Stalled must map to PhaseReady; got %q", got)
	}
}

func TestAggregateToPhase_ReconcilingMapsToProvisioning(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusFalse},
		{Type: TypeReconciling, Status: StatusTrue},
		{Type: TypeStalled, Status: StatusFalse},
	}
	got := AggregateToPhase(conds, 2, 2)
	if got != PhaseProvisioning {
		t.Errorf("Reconciling=True must map to PhaseProvisioning; got %q", got)
	}
}

func TestAggregateToPhase_EmptyConditionsReturnsPending(t *testing.T) {
	got := AggregateToPhase(nil, 0, 0)
	if got != PhasePending {
		t.Errorf("empty conditions must return PhasePending; got %q", got)
	}
}
