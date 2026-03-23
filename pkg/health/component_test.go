package health

import (
	"testing"
	"time"
)

func TestAggregatePlatform_NoComponents_PendingOverall(t *testing.T) {
	result := AggregatePlatform(nil)

	if result.Overall != PhasePending {
		t.Errorf("Overall = %q, want %q", result.Overall, PhasePending)
	}
	if len(result.Components) != 0 {
		t.Errorf("Components length = %d, want 0", len(result.Components))
	}
}

func TestAggregatePlatform_AllReady_ReadyOverall(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseReady, Detail: "all resources created"},
		{Name: "gitops", Phase: PhaseReady, Detail: "synced"},
		{Name: "runtime", Phase: PhaseReady, Detail: "workloads running"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseReady {
		t.Errorf("Overall = %q, want %q", result.Overall, PhaseReady)
	}
	if len(result.Components) != 3 {
		t.Errorf("Components length = %d, want 3", len(result.Components))
	}
}

func TestAggregatePlatform_AnyFailed_FailedOverall(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseReady, Detail: "ok"},
		{Name: "gitops", Phase: PhaseFailed, Detail: "irrecoverable error"},
		{Name: "runtime", Phase: PhaseReady, Detail: "ok"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseFailed {
		t.Errorf("Overall = %q, want %q", result.Overall, PhaseFailed)
	}
}

func TestAggregatePlatform_AnyDeleting_DeletingOverall(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseReady, Detail: "ok"},
		{Name: "gitops", Phase: PhaseDeleting, Detail: "finalizers running"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseDeleting {
		t.Errorf("Overall = %q, want %q", result.Overall, PhaseDeleting)
	}
}

func TestAggregatePlatform_AnyDegraded_DegradedOverall(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseReady, Detail: "ok"},
		{Name: "gitops", Phase: PhaseDegraded, Detail: "sync lag"},
		{Name: "runtime", Phase: PhaseReady, Detail: "ok"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseDegraded {
		t.Errorf("Overall = %q, want %q", result.Overall, PhaseDegraded)
	}
}

func TestAggregatePlatform_AnyProvisioning_ProvisioningOverall(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseProvisioning, Detail: "creating resources"},
		{Name: "gitops", Phase: PhaseReady, Detail: "ok"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseProvisioning {
		t.Errorf("Overall = %q, want %q", result.Overall, PhaseProvisioning)
	}
}

func TestAggregatePlatform_AnyPending_PendingOverall(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhasePending, Detail: "not started"},
		{Name: "gitops", Phase: PhaseReady, Detail: "ok"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhasePending {
		t.Errorf("Overall = %q, want %q", result.Overall, PhasePending)
	}
}

func TestAggregatePlatform_FailedTakesPrecedenceOverDegraded(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseDegraded, Detail: "slow"},
		{Name: "gitops", Phase: PhaseFailed, Detail: "broken"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseFailed {
		t.Errorf("Overall = %q, want %q (Failed takes precedence)", result.Overall, PhaseFailed)
	}
}

func TestAggregatePlatform_DeletingTakesPrecedenceOverDegraded(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseDegraded, Detail: "slow"},
		{Name: "gitops", Phase: PhaseDeleting, Detail: "removing"},
	}

	result := AggregatePlatform(components)

	if result.Overall != PhaseDeleting {
		t.Errorf("Overall = %q, want %q (Deleting takes precedence over Degraded)", result.Overall, PhaseDeleting)
	}
}

func TestAggregatePlatform_UpdatedAtIsSet(t *testing.T) {
	components := []ComponentHealth{
		{Name: "bootstrap", Phase: PhaseReady, Detail: "ok"},
	}

	before := time.Now()
	result := AggregatePlatform(components)
	after := time.Now()

	if result.UpdatedAt.Before(before) || result.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt = %v, expected between %v and %v", result.UpdatedAt, before, after)
	}
}

func TestAggregatePlatform_PreservesComponentOrder(t *testing.T) {
	components := []ComponentHealth{
		{Name: "alpha", Phase: PhaseReady, Detail: "a"},
		{Name: "beta", Phase: PhaseReady, Detail: "b"},
		{Name: "gamma", Phase: PhaseReady, Detail: "c"},
	}

	result := AggregatePlatform(components)

	for i, c := range result.Components {
		if c.Name != components[i].Name {
			t.Errorf("Components[%d].Name = %q, want %q", i, c.Name, components[i].Name)
		}
	}
}
