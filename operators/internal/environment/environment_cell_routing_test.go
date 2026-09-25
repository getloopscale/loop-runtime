// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environment

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/constants"
)

// cellFactory routes each cellID to its own client, the way the production
// factory routes each cell to its own cluster. An unknown cell is an error.
type cellFactory struct {
	cells map[string]client.Client
	err   error
}

func (f cellFactory) For(_ context.Context, cellID string) (client.Client, error) {
	if f.err != nil {
		return nil, f.err
	}
	c, ok := f.cells[cellID]
	if !ok {
		return nil, fmt.Errorf("no client for cell %q", cellID)
	}
	return c, nil
}

const routedNamespace = "loopzero-tn-cell-loja-production"

func routedTenant(cellID string) *loopzerov1.Tenant {
	return &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-cell", Namespace: "default"},
		Spec:       loopzerov1.TenantSpec{CellID: cellID},
	}
}

func routedEnvironment() *loopzerov1.Environment {
	return &loopzerov1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "env-cell",
			Namespace:  "default",
			Finalizers: []string{environmentFinalizer},
		},
		Spec: loopzerov1.EnvironmentCRSpec{
			TenantRef:   loopzerov1.TenantRef{Name: "tn-cell"},
			Environment: "production",
			Project:     "loja",
			Namespace:   loopzerov1.NamespaceSpec{Name: routedNamespace},
		},
	}
}

func deletingRoutedEnvironment(recordedCell, provisionedNamespace string) *loopzerov1.Environment {
	env := routedEnvironment()
	now := metav1.NewTime(time.Now())
	env.DeletionTimestamp = &now
	env.Status = loopzerov1.EnvironmentCRStatus{
		Phase:     loopzerov1.EnvironmentPhaseDeleting,
		CellID:    recordedCell,
		Namespace: provisionedNamespace,
	}
	return env
}

func tenantNamespace(labelTenant string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   routedNamespace,
		Labels: map[string]string{constants.LabelTenant: labelTenant},
	}}
}

func reconcileRouted(t *testing.T, r *EnvironmentReconciler) error {
	t.Helper()
	_, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "env-cell", Namespace: "default"}})
	return err
}

func TestEnvironmentReconcile_ProvisionsNamespaceInTheTenantCell(t *testing.T) {
	s := environmentTestScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routedTenant("cell-1"), routedEnvironment()).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{cells: map[string]client.Client{"": cell0, "cell-1": cell1}}}

	if err := reconcileRouted(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if err := cell1.Get(context.Background(), client.ObjectKey{Name: routedNamespace}, &corev1.Namespace{}); err != nil {
		t.Fatalf("namespace must exist in the tenant's cell: %v", err)
	}
	if err := cell0.Get(context.Background(), client.ObjectKey{Name: routedNamespace}, &corev1.Namespace{}); !apierrors.IsNotFound(err) {
		t.Fatalf("namespace must not be created on the default cell, Get err = %v", err)
	}
}

func TestEnvironmentReconcile_RecordsTheCellItProvisionedIn(t *testing.T) {
	s := environmentTestScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routedTenant("cell-1"), routedEnvironment()).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{cells: map[string]client.Client{"cell-1": cell1}}}

	if err := reconcileRouted(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var env loopzerov1.Environment
	if err := mgmt.Get(context.Background(), types.NamespacedName{Name: "env-cell", Namespace: "default"}, &env); err != nil {
		t.Fatalf("get env: %v", err)
	}
	if env.Status.CellID != "cell-1" {
		t.Fatalf("Status.CellID = %q, want cell-1", env.Status.CellID)
	}
}

func TestEnvironmentReconcile_FailsWhenTheCellClientCannotBeResolved(t *testing.T) {
	s := environmentTestScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routedTenant("cell-1"), routedEnvironment()).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	factoryErr := errors.New("ClusterCredential secret unreadable")
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{err: factoryErr}}

	err := reconcileRouted(t, r)

	if !errors.Is(err, factoryErr) {
		t.Fatalf("Reconcile error = %v, want the factory error", err)
	}
	if gerr := cell0.Get(context.Background(), client.ObjectKey{Name: routedNamespace}, &corev1.Namespace{}); !apierrors.IsNotFound(gerr) {
		t.Fatalf("no namespace may be created anywhere while the cell is unresolved, Get err = %v", gerr)
	}
}

func TestEnvironmentReconcile_FailsWhenTheTenantIsUnreadable(t *testing.T) {
	s := environmentTestScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routedEnvironment()).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{cells: map[string]client.Client{"": cell0}}}

	if err := reconcileRouted(t, r); err == nil {
		t.Fatal("Reconcile must fail while the tenant (and so its cell) is unknown")
	}
	if gerr := cell0.Get(context.Background(), client.ObjectKey{Name: routedNamespace}, &corev1.Namespace{}); !apierrors.IsNotFound(gerr) {
		t.Fatalf("no namespace may be created on the default cell as a fallback, Get err = %v", gerr)
	}
}

func TestEnvironmentDelete_DeletesTheNamespaceInTheRecordedCell(t *testing.T) {
	s := environmentTestScheme(t)
	// The tenant was re-pinned to cell-2 after provisioning; the namespace
	// still lives where it was created.
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routedTenant("cell-2"), deletingRoutedEnvironment("cell-1", routedNamespace)).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).WithObjects(tenantNamespace("tn-cell")).Build()
	cell2 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{cells: map[string]client.Client{"cell-1": cell1, "cell-2": cell2}}}

	if err := reconcileRouted(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	err := cell1.Get(context.Background(), client.ObjectKey{Name: routedNamespace}, &corev1.Namespace{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("the namespace in the recorded cell must be deleted, Get err = %v", err)
	}
}

func TestEnvironmentDelete_KeepsTheFinalizerWhenTheNamespaceLookupFails(t *testing.T) {
	s := environmentTestScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(routedTenant("cell-1"), deletingRoutedEnvironment("cell-1", routedNamespace)).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	lookupErr := errors.New("runtime apiserver timeout")
	cell1 := interceptor.NewClient(fake.NewClientBuilder().WithScheme(s).WithObjects(tenantNamespace("tn-cell")).Build(),
		interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				return lookupErr
			}
			return c.Get(ctx, key, obj, opts...)
		}})
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{cells: map[string]client.Client{"cell-1": cell1}}}

	err := reconcileRouted(t, r)

	if !errors.Is(err, lookupErr) {
		t.Fatalf("Reconcile error = %v, want the namespace lookup error", err)
	}
	var env loopzerov1.Environment
	if gerr := mgmt.Get(context.Background(), types.NamespacedName{Name: "env-cell", Namespace: "default"}, &env); gerr != nil {
		t.Fatalf("the Environment must still exist: %v", gerr)
	}
	if len(env.Finalizers) == 0 {
		t.Fatal("the finalizer must stay while the namespace's existence is unknown")
	}
}

func TestEnvironmentDelete_FailsWhenAProvisionedNamespaceHasNoKnownCell(t *testing.T) {
	s := environmentTestScheme(t)
	// No tenant and no recorded cell, but the environment did provision a
	// namespace: it exists somewhere this controller can no longer locate.
	mgmt := fake.NewClientBuilder().WithScheme(s).
		WithObjects(deletingRoutedEnvironment("", routedNamespace)).
		WithStatusSubresource(&loopzerov1.Environment{}).Build()
	r := &EnvironmentReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: cellFactory{cells: map[string]client.Client{}}}

	if err := reconcileRouted(t, r); err == nil {
		t.Fatal("deletion must not release the finalizer over a namespace it cannot locate")
	}
}
