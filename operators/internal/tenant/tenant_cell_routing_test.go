// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"context"
	"fmt"
	"testing"

	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

// cellRoutes resolves each cellID to its own client, as the production factory
// resolves each cell to its own cluster.
type cellRoutes map[string]client.Client

func (c cellRoutes) For(_ context.Context, cellID string) (client.Client, error) {
	cl, ok := c[cellID]
	if !ok {
		return nil, fmt.Errorf("no client for cell %q", cellID)
	}
	return cl, nil
}

func cellScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := loopruntimev1.AddToScheme(s); err != nil {
		t.Fatalf("loop-runtime scheme: %v", err)
	}
	return s
}

func pinnedTenant(cellID string) *loopruntimev1.Tenant {
	return &loopruntimev1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-pinned", Namespace: "default"},
		Spec: loopruntimev1.TenantSpec{
			CellID: cellID,
			Plan:   loopruntimev1.PlanStarter,
			Environments: []loopruntimev1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: "shop"},
			},
		},
	}
}

func TestReconcilePriorityClass_WritesToTheTenantCell(t *testing.T) {
	s := cellScheme(t)
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).Build()
	r := &TenantReconciler{RuntimeFactory: cellRoutes{"": cell0, "cell-1": cell1}}

	if err := r.reconcilePriorityClass(context.Background(), pinnedTenant("cell-1")); err != nil {
		t.Fatalf("reconcilePriorityClass: %v", err)
	}

	key := client.ObjectKey{Name: constants.PriorityClassStarter}
	if err := cell1.Get(context.Background(), key, &schedulingv1.PriorityClass{}); err != nil {
		t.Fatalf("PriorityClass must exist on the tenant's cell: %v", err)
	}
	if err := cell0.Get(context.Background(), key, &schedulingv1.PriorityClass{}); !apierrors.IsNotFound(err) {
		t.Fatalf("PriorityClass must not be written to the default cell, Get err = %v", err)
	}
}
