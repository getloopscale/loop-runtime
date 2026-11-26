// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environment

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
)

// B-074. Deleting the Environment CR directly left the Application orphaned:
// the reaper `pruneOrphanPreviewApps` only deletes Applications OUTSIDE
// `expectedApplicationNames`, and that set comes from `Tenant.Spec.Environments`
// by the (Project, Name) tuple. With the tuple still in the spec, the
// Application stays EXPECTED and the reaper never reaches it — while the
// Namespace it points at is already deleted.
//
// T2 record: knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-19-environment-delete-orphans-application.md
func tenantWithTwoEnvironments() *loopruntimev1.Tenant {
	return &loopruntimev1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-shop", Namespace: "default"},
		Spec: loopruntimev1.TenantSpec{
			Environments: []loopruntimev1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "cell-0", Project: "shop"},
				{Name: "staging", Cluster: "cell-0", Project: "shop"},
			},
		},
	}
}

func environmentBeingDeleted() *loopruntimev1.Environment {
	now := metav1.NewTime(time.Now())
	return &loopruntimev1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "env-shop-staging",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{environmentFinalizer},
		},
		Spec: loopruntimev1.EnvironmentCRSpec{
			TenantRef:   loopruntimev1.TenantRef{Name: "tn-shop"},
			Environment: "staging",
			Project:     "shop",
			Namespace:   loopruntimev1.NamespaceSpec{Name: "loopruntime-tn-shop-shop-staging"},
		},
		Status: loopruntimev1.EnvironmentCRStatus{Phase: loopruntimev1.EnvironmentPhaseDeleting},
	}
}

func TestDeletingAnEnvironmentRemovesItsTupleSoTheReaperReachesTheApplication(t *testing.T) {
	s := environmentTestScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(tenantWithTwoEnvironments(), environmentBeingDeleted()).
		WithStatusSubresource(&loopruntimev1.Environment{}, &loopruntimev1.Tenant{}).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "env-shop-staging", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var tn loopruntimev1.Tenant
	if err := cl.Get(context.Background(),
		types.NamespacedName{Name: "tn-shop", Namespace: "default"}, &tn); err != nil {
		t.Fatalf("reading the Tenant: %v", err)
	}

	for _, e := range tn.Spec.Environments {
		if e.Project == "shop" && e.Name == "staging" {
			t.Fatalf("the (shop, staging) tuple is still in Spec.Environments after the delete — "+
				"the reaper keeps the Application EXPECTED and it is orphaned, pointing at a Namespace "+
				"that no longer exists. Spec.Environments = %+v", tn.Spec.Environments)
		}
	}
	// And the counterpart: deleting one environment must not take the others.
	if len(tn.Spec.Environments) != 1 || tn.Spec.Environments[0].Name != "production" {
		t.Errorf("Spec.Environments = %+v; the production entry was not to be touched",
			tn.Spec.Environments)
	}
}

// Negative case (`rules/testing.md` § 4.1): the Tenant no longer exists. There
// is no tuple to remove, and holding the finalizer here would block the
// Environment's deletion FOREVER — a fix that closes a leak by opening a
// deadlock is no fix.
func TestDeletingAnEnvironmentWithoutItsTenantDoesNotBlockTheDeletion(t *testing.T) {
	s := environmentTestScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(environmentBeingDeleted()). // no Tenant
		WithStatusSubresource(&loopruntimev1.Environment{}).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	if _, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "env-shop-staging", Namespace: "default"}}); err != nil {
		t.Fatalf("Reconcile returned an error with no Tenant: %v — the finalizer never leaves and the CR is stuck", err)
	}

	var env loopruntimev1.Environment
	err := cl.Get(context.Background(),
		types.NamespacedName{Name: "env-shop-staging", Namespace: "default"}, &env)
	if err == nil && len(env.Finalizers) > 0 {
		t.Errorf("finalizer %v is still on the Environment; with no Tenant there is nothing to prune and "+
			"the deletion had to complete", env.Finalizers)
	}
}
