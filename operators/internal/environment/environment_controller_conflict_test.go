// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Deterministic unit proof for MEET-2026-07-02-002 (t31 flake fix).
//
// These tests use controller-runtime's fake client + interceptor to prove the
// status-write concurrency contract WITHOUT envtest or timing — the flake the
// finding `2026-07-02-tenant-envtest-red-baseline.md` tracked was fundamentally
// a status-write conflict-handling bug, so it is provable deterministically:
//
//   - MA-2: an injected status-write conflict must surface as a requeue
//     (returned error), never a silent `return ctrl.Result{}, nil`.
//   - MA-4: the NameTooLong branch must report ObservedGeneration.
//   - MA-3: a delete that arrives while the phase is still Empty must flow
//     through the additive SM edge (Empty→Deleting) and remove the finalizer,
//     not error on an "invalid transition".
//
// The interceptor injects the conflict on BOTH SubResourceUpdate (the pre-fix
// idiom) and SubResourcePatch (the post-fix idiom) so each test is anchored to
// the behavioral contract, not the write mechanism.
package environment

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
)

func environmentTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("clientgoscheme.AddToScheme: %v", err)
	}
	if err := loopruntimev1.AddToScheme(s); err != nil {
		t.Fatalf("loopruntimev1.AddToScheme: %v", err)
	}
	return s
}

// overflowEnv builds an Environment whose canonical M2 namespace
// (`loopruntime-<tenant>-<project>-<env>`) exceeds the 63-char limit (85 chars),
// forcing the reconcile down the NameTooLong branch. The finalizer is
// pre-seeded so EnsureFinalizer is a no-op and Generation stays stable.
func overflowEnv() *loopruntimev1.Environment {
	return &loopruntimev1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "tn-overflow-conflict",
			Namespace:  "default",
			Finalizers: []string{environmentFinalizer},
			Generation: 7,
		},
		Spec: loopruntimev1.EnvironmentCRSpec{
			TenantRef:   loopruntimev1.TenantRef{Name: "tn-very-long-tenant-name-h"},
			Project:     "production-data-pipeline-v2",
			Environment: "staging-canary-experiment",
			Namespace:   loopruntimev1.NamespaceSpec{Name: "unused-when-project-set"},
			Plan:        loopruntimev1.PlanStarter,
		},
	}
}

func assertNameTooLong(t *testing.T, env *loopruntimev1.Environment) {
	t.Helper()
	for _, c := range env.Status.Conditions {
		if c.Type == "NamespaceReady" && c.Status == metav1.ConditionFalse && c.Reason == "NameTooLong" {
			return
		}
	}
	t.Fatalf("expected NamespaceReady=False Reason=NameTooLong; got conditions: %+v", env.Status.Conditions)
}

// MA-4: the NameTooLong branch must set ObservedGeneration to the observed
// spec generation. Deterministic — no conflict injection.
func TestEnvironmentReconciler_NameTooLong_SetsObservedGeneration(t *testing.T) {
	s := environmentTestScheme(t)
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(overflowEnv()).
		WithStatusSubresource(&loopruntimev1.Environment{}).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	key := types.NamespacedName{Name: "tn-overflow-conflict", Namespace: "default"}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	var got loopruntimev1.Environment
	if err := cl.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	if got.Generation == 0 {
		t.Fatal("precondition: Generation must be > 0")
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Fatalf("ObservedGeneration=%d, want Generation=%d (MA-4)",
			got.Status.ObservedGeneration, got.Generation)
	}
	assertNameTooLong(t, &got)
}

// MA-2: an injected status-write conflict must surface as a requeue (returned
// error), never a silent nil. Second pass (no conflict) converges via
// idempotent re-derivation.
func TestEnvironmentReconciler_NameTooLong_RequeuesOnStatusConflict(t *testing.T) {
	s := environmentTestScheme(t)

	conflictArmed := true
	conflict := func(name string) error {
		conflictArmed = false
		return apierrors.NewConflict(
			schema.GroupResource{Group: "runtime.getloopscale.com", Resource: "environments"},
			name, fmt.Errorf("injected optimistic-concurrency conflict"))
	}
	ic := interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
			if conflictArmed {
				return conflict(obj.GetName())
			}
			return cl.Status().Patch(ctx, obj, patch, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, cl client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if conflictArmed {
				return conflict(obj.GetName())
			}
			return cl.Status().Update(ctx, obj, opts...)
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(overflowEnv()).
		WithStatusSubresource(&loopruntimev1.Environment{}).
		WithInterceptorFuncs(ic).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	key := types.NamespacedName{Name: "tn-overflow-conflict", Namespace: "default"}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err == nil {
		t.Fatal("expected requeue error on injected status-write conflict, got nil (MA-2)")
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("second reconcile (no conflict) returned error: %v", err)
	}
	var got loopruntimev1.Environment
	if err := cl.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	assertNameTooLong(t, &got)
}

// MA-3: a delete that arrives while the phase is still Empty must flow through
// the additive SM edge (Empty→Deleting) and remove the finalizer without error.
func TestEnvironmentReconciler_DeleteFromEmpty_RemovesFinalizer(t *testing.T) {
	s := environmentTestScheme(t)
	now := metav1.Now()
	env := &loopruntimev1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "tn-del-empty",
			Namespace:         "default",
			Finalizers:        []string{environmentFinalizer},
			DeletionTimestamp: &now,
			// Phase intentionally left "" (Empty): the delete arrived before
			// the first successful reconcile ever persisted a phase.
		},
		Spec: loopruntimev1.EnvironmentCRSpec{
			TenantRef: loopruntimev1.TenantRef{Name: "tn-x"},
			Namespace: loopruntimev1.NamespaceSpec{Name: "loopruntime-tn-x-default-project-production"},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(env).
		WithStatusSubresource(&loopruntimev1.Environment{}).
		Build()

	r := &EnvironmentReconciler{Client: cl, RuntimeFactory: cellrouting.NewStaticRuntimeClientFactory(cl), Scheme: s}
	key := types.NamespacedName{Name: "tn-del-empty", Namespace: "default"}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("delete-from-Empty reconcile returned error: %v (MA-3 edge missing?)", err)
	}

	var got loopruntimev1.Environment
	err := cl.Get(context.Background(), key, &got)
	if err == nil && len(got.Finalizers) > 0 {
		t.Fatalf("expected finalizer removed (object GC'd); still present: %v", got.Finalizers)
	}
}
