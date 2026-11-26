// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// tenant_lister_test.go — the metering enumeration source (plan
// metering-compute-loop T1.3 / T3.1). Unit tests over the controller-runtime
// fake client; NO live cluster.

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// meteringScheme registers the runtime.getloopscale.com Tenant types on a fresh scheme for the
// fake client (mirrors k8s.NewScheme, kept local to avoid an out-of-scope import).
func meteringScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := apiv1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return s
}

// tenantCRWithPhase builds a Tenant CR in the given namespace with the given
// observed status.phase. Status is seeded via WithObjects + WithStatusSubresource
// (the reaper pattern) so the fake client persists status.phase and returns it on
// List; otherwise the phase filter would pass for the wrong reason (empty phase).
func tenantCRWithPhase(id, namespace string, phase apiv1.TenantPhase) *apiv1.Tenant {
	return &apiv1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: namespace},
		Status:     apiv1.TenantStatus{Phase: phase},
	}
}

// TestCRTenantLister_FiltersOnPhaseActiveConstant — T1.3 + C2: the enumeration
// returns ONLY the tenant IDs whose observed status.phase is Active (the
// TenantPhaseActive CONSTANT, never a "Active" literal) AND that live in the
// canonical loopruntime-system namespace. Provisioning / Suspended are excluded by phase
// (T1.3 — they reserve no billable compute, ADR-5); an Active CR in a NON-canonical
// namespace is excluded by scope (C2 — proves client.InNamespace constrains List).
func TestCRTenantLister_FiltersOnPhaseActiveConstant(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(meteringScheme(t)).
		WithObjects(
			tenantCRWithPhase("tn-active", tenantCRNamespace, apiv1.TenantPhaseActive),
			tenantCRWithPhase("tn-provisioning", tenantCRNamespace, apiv1.TenantPhaseProvisioning),
			tenantCRWithPhase("tn-suspended", tenantCRNamespace, apiv1.TenantPhaseSuspended),
			tenantCRWithPhase("tn-pending-deletion", tenantCRNamespace, apiv1.TenantPhasePendingDeletion),
			tenantCRWithPhase("tn-active-wrong-ns", "default", apiv1.TenantPhaseActive),
		).
		WithStatusSubresource(&apiv1.Tenant{}).
		Build()
	lister := &CRTenantLister{Client: c}

	got, err := lister.ListActiveTenants(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenants: %v", err)
	}
	if len(got) != 1 || got[0] != "tn-active" {
		t.Fatalf("ListActiveTenants = %v, want [tn-active] — only the Active CR in "+
			"loopruntime-system (T1.3 phase-filter on TenantPhaseActive constant + C2 namespace scope)", got)
	}
}

// TestCRTenantLister_EmptyWhenNoneActive — a namespace with zero Active tenants
// returns an empty (non-nil) slice, never an error. Guards the worker's "nothing
// to meter this tick" path (empty MUST NOT be treated as a failure that HOLDS
// every cursor).
func TestCRTenantLister_EmptyWhenNoneActive(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(meteringScheme(t)).
		WithObjects(
			tenantCRWithPhase("tn-prov", tenantCRNamespace, apiv1.TenantPhaseProvisioning),
			tenantCRWithPhase("tn-del", tenantCRNamespace, apiv1.TenantPhaseDeleting),
		).
		WithStatusSubresource(&apiv1.Tenant{}).
		Build()
	lister := &CRTenantLister{Client: c}

	got, err := lister.ListActiveTenants(context.Background())
	if err != nil {
		t.Fatalf("ListActiveTenants: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListActiveTenants = %v, want [] (no Active tenants)", got)
	}
}

// TestCRTenantLister_ImplementsTenantLister — compile-time proof the concrete
// adapter satisfies the narrow TenantLister interface the worker depends on (DIP).
func TestCRTenantLister_ImplementsTenantLister(t *testing.T) {
	var _ TenantLister = (*CRTenantLister)(nil)
}
