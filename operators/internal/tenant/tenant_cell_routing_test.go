// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/harbor"
	"github.com/getloopscale/loopzero/pkg/naming"
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

// harborStub hands out fresh robot credentials and records nothing else.
type harborStub struct{}

func (harborStub) EnsureProject(context.Context, string, harbor.ProjectOpts) error { return nil }
func (harborStub) DeleteProject(context.Context, string) error                     { return nil }
func (harborStub) EnsureRobotAccount(_ context.Context, _ string, opts harbor.RobotAccountOpts) (*harbor.RobotCredentials, error) {
	return &harbor.RobotCredentials{Username: "robot$" + opts.Name, Secret: "s3cr3t", ExpiresAt: time.Unix(0, 0)}, nil
}
func (harborStub) DeleteRobotAccount(context.Context, string, string) error { return nil }
func (harborStub) Healthy(context.Context) error                            { return nil }

// namespacelessCell behaves like a real apiserver whose workload namespace does
// not exist: creating a namespaced Secret there returns NotFound. The fake
// client does not model namespace existence on its own.
func namespacelessCell(s *runtime.Scheme) client.Client {
	return interceptor.NewClient(fake.NewClientBuilder().WithScheme(s).Build(), interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*corev1.Secret); ok {
				return apierrors.NewNotFound(corev1.Resource("namespaces"), obj.GetNamespace())
			}
			return c.Create(ctx, obj, opts...)
		},
	})
}

func cellScheme(t *testing.T) *runtime.Scheme {
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

func pinnedTenant(cellID string) *loopzerov1.Tenant {
	return &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-pinned", Namespace: "default"},
		Spec: loopzerov1.TenantSpec{
			CellID: cellID,
			Plan:   loopzerov1.PlanStarter,
			Environments: []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload", Project: "shop"},
			},
		},
	}
}

func workloadNamespace(t *testing.T) string {
	t.Helper()
	ns, err := naming.Namespace("tn-pinned", "shop", "production")
	if err != nil {
		t.Fatalf("namespace: %v", err)
	}
	return ns
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

func TestEnsureHarborRobotAccounts_WritesPullSecretToTheTenantCell(t *testing.T) {
	s := cellScheme(t)
	ns := workloadNamespace(t)
	build := fake.NewClientBuilder().WithScheme(s).Build()
	cell0 := fake.NewClientBuilder().WithScheme(s).Build()
	cell1 := fake.NewClientBuilder().WithScheme(s).
		WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}).Build()
	r := &TenantReconciler{
		Client:             fake.NewClientBuilder().WithScheme(s).Build(),
		BuildClient:        build,
		RuntimeFactory:     cellRoutes{"": cell0, "cell-1": cell1},
		HarborClient:       harborStub{},
		HarborRegistryHost: "harbor.test",
	}

	if err := r.ensureHarborRobotAccounts(context.Background(), pinnedTenant("cell-1")); err != nil {
		t.Fatalf("ensureHarborRobotAccounts: %v", err)
	}

	key := client.ObjectKey{Namespace: ns, Name: constants.SecretHarborPullCredentials}
	if err := cell1.Get(context.Background(), key, &corev1.Secret{}); err != nil {
		t.Fatalf("pull secret must be written to the tenant's cell: %v", err)
	}
}

// A missing workload namespace is only tolerated while its Environment has not
// reported the namespace as provisioned. After that, NotFound is a real miss —
// a wrong cluster or a deleted namespace — and must fail loudly.
func TestEnsureHarborRobotAccounts_FailsWhenAProvisionedNamespaceIsMissing(t *testing.T) {
	s := cellScheme(t)
	envCRName, err := naming.EnvironmentCRName("tn-pinned", "shop", "production")
	if err != nil {
		t.Fatalf("env CR name: %v", err)
	}
	env := &loopzerov1.Environment{ObjectMeta: metav1.ObjectMeta{Name: envCRName, Namespace: "default"}}
	conditions.SetCondition(&env.Status.Conditions, conditions.TypeEnvNamespaceReady,
		metav1.ConditionTrue, "NamespaceReady", "namespace provisioned", 1)
	mgmt := fake.NewClientBuilder().WithScheme(s).WithObjects(env).WithStatusSubresource(env).Build()
	if err := mgmt.Status().Update(context.Background(), env); err != nil {
		t.Fatalf("seed env status: %v", err)
	}
	r := &TenantReconciler{
		Client:             mgmt,
		BuildClient:        fake.NewClientBuilder().WithScheme(s).Build(),
		RuntimeFactory:     cellRoutes{"cell-1": namespacelessCell(s)},
		HarborClient:       harborStub{},
		HarborRegistryHost: "harbor.test",
	}

	if err := r.ensureHarborRobotAccounts(context.Background(), pinnedTenant("cell-1")); err == nil {
		t.Fatal("a namespace its Environment reports as provisioned must exist; NotFound is not 'still pending'")
	}
}

func TestEnsureHarborRobotAccounts_ToleratesANamespaceStillBeingProvisioned(t *testing.T) {
	s := cellScheme(t)
	r := &TenantReconciler{
		Client:             fake.NewClientBuilder().WithScheme(s).Build(),
		BuildClient:        fake.NewClientBuilder().WithScheme(s).Build(),
		RuntimeFactory:     cellRoutes{"cell-1": namespacelessCell(s)},
		HarborClient:       harborStub{},
		HarborRegistryHost: "harbor.test",
	}

	if err := r.ensureHarborRobotAccounts(context.Background(), pinnedTenant("cell-1")); err != nil {
		t.Fatalf("a namespace whose Environment has not provisioned it yet is pending, not an error: %v", err)
	}
}
