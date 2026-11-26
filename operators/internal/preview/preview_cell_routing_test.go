// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
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
	if err := loopruntimev1.AddToScheme(s); err != nil {
		t.Fatalf("loopruntime scheme: %v", err)
	}
	return s
}

func cellPreview(cellID string) *loopruntimev1.PreviewEnvironment {
	return &loopruntimev1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pr-7",
			Namespace:  "loopruntime-system",
			Finalizers: []string{previewFinalizer},
		},
		Spec: loopruntimev1.PreviewEnvironmentSpec{TenantID: "tn-acme", ProjectID: "shop", PRNumber: 7, CellID: cellID},
	}
}

const cellPreviewNamespace = "loopruntime-tn-acme-shop-preview-pr-7"

func reconcileCellPreview(t *testing.T, r *PreviewReconciler) error {
	t.Helper()
	_, err := r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "pr-7", Namespace: "loopruntime-system"}})
	return err
}

func TestPreviewReconcile_ProvisionsEveryResourceInThePreviewCell(t *testing.T) {
	s := previewCellScheme(t)
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(cellPreview("cell-1")).
		WithStatusSubresource(&loopruntimev1.PreviewEnvironment{}).Build()
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &PreviewReconciler{Client: mgmt, Scheme: s,
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
	if err := cell1.Get(ctx, client.ObjectKey{Namespace: cellPreviewNamespace, Name: "loopruntime-app"}, &corev1.ServiceAccount{}); err != nil {
		t.Fatalf("loopruntime-app ServiceAccount must be in the preview's cell: %v", err)
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
		WithStatusSubresource(&loopruntimev1.PreviewEnvironment{}).Build()
	factoryErr := errors.New("cell-1 kubeconfig Secret unreadable")
	r := &PreviewReconciler{Client: mgmt, Scheme: s,
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
		WithStatusSubresource(&loopruntimev1.PreviewEnvironment{}).Build()
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

// The URL a preview reports is the one the API computed, never a formula of
// the controller's own.
func TestPreviewReconcile_ReportsTheURLTheAPIComputed(t *testing.T) {
	s := previewCellScheme(t)
	pv := cellPreview("cell-1")
	pv.Spec.PublicURL = "https://tn-acme-shop-web-preview-pr-7.preview.example.test"
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(pv).
		WithStatusSubresource(&loopruntimev1.PreviewEnvironment{}).Build()
	r := &PreviewReconciler{Client: mgmt, Scheme: s,
		RuntimeFactory: previewCells{cells: map[string]client.Client{"cell-1": fake.NewClientBuilder().WithScheme(s).Build()}}}

	if err := reconcileCellPreview(t, r); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got loopruntimev1.PreviewEnvironment
	if err := mgmt.Get(context.Background(), types.NamespacedName{Name: "pr-7", Namespace: "loopruntime-system"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.URL != pv.Spec.PublicURL {
		t.Fatalf("status.url = %q, want the API's %q", got.Status.URL, pv.Spec.PublicURL)
	}
}
