// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

// previewCells routes each cellID to its own client, as the production factory
// routes each cell to its own cluster. err, when set, fails every lookup.
type previewCells struct {
	cells map[string]client.Client
	err   error
}

func (p previewCells) For(_ context.Context, cellID string) (client.Client, error) {
	if p.err != nil {
		return nil, p.err
	}
	c, ok := p.cells[cellID]
	if !ok {
		return nil, fmt.Errorf("no client for cell %q", cellID)
	}
	return c, nil
}

func previewCellScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(s); err != nil {
		t.Fatalf("loopzero scheme: %v", err)
	}
	return s
}

func cellPreview(cellID string) *loopzerov1.PreviewEnvironment {
	return &loopzerov1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pr-7",
			Namespace:  "loopzero-system",
			Finalizers: []string{previewFinalizer},
		},
		Spec: loopzerov1.PreviewEnvironmentSpec{TenantID: "tn-acme", ProjectID: "shop", PRNumber: 7, CellID: cellID},
	}
}

const cellPreviewNamespace = "loopzero-tn-acme-shop-preview-pr-7"

func reconcileCellPreview(t *testing.T, r *PreviewReconciler) error {
	t.Helper()
	_, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "pr-7", Namespace: "loopzero-system"}})
	return err
}

func TestPreviewReconcile_ProvisionsEveryResourceInThePreviewCell(t *testing.T) {
	s := previewCellScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(cellPreview("cell-1")).
		WithStatusSubresource(&loopzerov1.PreviewEnvironment{}).Build()
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &PreviewReconciler{Client: mgmt, Scheme: s, BaseDomain: "example.test",
		RuntimeFactory: previewCells{cells: map[string]client.Client{"": cell0, "cell-1": cell1}}}

	if err := reconcileCellPreview(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	ctx := context.Background()
	if err := cell1.Get(ctx, client.ObjectKey{Name: cellPreviewNamespace}, &corev1.Namespace{}); err != nil {
		t.Fatalf("namespace must exist in the preview's cell: %v", err)
	}
	var policies networkingv1.NetworkPolicyList
	if err := cell1.List(ctx, &policies, client.InNamespace(cellPreviewNamespace)); err != nil || len(policies.Items) == 0 {
		t.Fatalf("isolation NetworkPolicies must be in the preview's cell, got %d (err %v)", len(policies.Items), err)
	}
	if err := cell1.Get(ctx, client.ObjectKey{Namespace: cellPreviewNamespace, Name: "loopzero-app"}, &corev1.ServiceAccount{}); err != nil {
		t.Fatalf("loopzero-app ServiceAccount must be in the preview's cell: %v", err)
	}
	var quotas corev1.ResourceQuotaList
	if err := cell1.List(ctx, &quotas, client.InNamespace(cellPreviewNamespace)); err != nil || len(quotas.Items) == 0 {
		t.Fatalf("ResourceQuota must be in the preview's cell, got %d (err %v)", len(quotas.Items), err)
	}

	var strays networkingv1.NetworkPolicyList
	if err := cell0.List(ctx, &strays); err != nil {
		t.Fatalf("list default cell: %v", err)
	}
	if len(strays.Items) != 0 {
		t.Fatalf("no preview object may land on the default cell, found %d NetworkPolicies", len(strays.Items))
	}
}

func TestPreviewReconcile_FailsWhenThePreviewCellCannotBeResolved(t *testing.T) {
	s := previewCellScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(cellPreview("cell-1")).
		WithStatusSubresource(&loopzerov1.PreviewEnvironment{}).Build()
	factoryErr := errors.New("cell-1 kubeconfig Secret unreadable")
	r := &PreviewReconciler{Client: mgmt, Scheme: s, BaseDomain: "example.test",
		RuntimeFactory: previewCells{err: factoryErr}}

	if err := reconcileCellPreview(t, r); !errors.Is(err, factoryErr) {
		t.Fatalf("Reconcile error = %v, want the factory error", err)
	}
}

func TestPreviewDelete_DeletesTheNamespaceInThePreviewCell(t *testing.T) {
	s := previewCellScheme(t)
	deleting := cellPreview("cell-1")
	now := metav1.NewTime(time.Now())
	deleting.DeletionTimestamp = &now
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(deleting).
		WithStatusSubresource(&loopzerov1.PreviewEnvironment{}).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).
		WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cellPreviewNamespace}}).Build()
	r := &PreviewReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: previewCells{cells: map[string]client.Client{"cell-1": cell1}}}

	if err := reconcileCellPreview(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if err := cell1.Get(context.Background(), client.ObjectKey{Name: cellPreviewNamespace}, &corev1.Namespace{}); !apierrors.IsNotFound(err) {
		t.Fatalf("the preview namespace in its cell must be deleted, Get err = %v", err)
	}
}
