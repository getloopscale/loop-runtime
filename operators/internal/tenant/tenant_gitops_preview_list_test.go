// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// An unknown preview set must never feed the prune.
//
// pruneOrphanPreviewApps deletes every tenant-labelled Application outside the
// expected set, and the ArgoCD resources finalizer cascades that delete into the
// workload. If the PreviewEnvironment List fails and the reconcile carries on
// with an empty preview set, every live preview of the tenant is torn down by a
// transient API error. The reconcile must fail instead, and delete nothing.
func TestReconcileArgoCDApp_FailsWithoutPruningWhenPreviewListFails(t *testing.T) {
	sc := runtime.NewScheme()
	if err := loopruntimev1.AddToScheme(sc); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	listErr := errors.New("apiserver timeout")
	c := interceptor.NewClient(fake.NewClientBuilder().WithScheme(sc).Build(), interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*loopruntimev1.PreviewEnvironmentList); ok {
				return listErr
			}
			return cl.List(ctx, list, opts...)
		},
	})
	argo := &fakeArgoListDelete{existing: []string{
		"tn-acme-default-project-production",
		"tn-acme-default-project-preview-pr-7",
	}}
	r := &TenantReconciler{Client: c, ArgoCDProvisioner: argo}
	tenant := &loopruntimev1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "tn-acme"},
		Spec: loopruntimev1.TenantSpec{Environments: []loopruntimev1.TenantEnvironmentEntry{
			{Name: "production", Cluster: "workload", Project: "default-project"},
		}},
	}

	err := r.reconcileArgoCDApp(context.Background(), tenant)

	if !errors.Is(err, listErr) {
		t.Fatalf("reconcileArgoCDApp error = %v, want it to wrap the preview list error", err)
	}
	if len(argo.deleted) != 0 {
		t.Fatalf("no Application may be deleted while the preview set is unknown, deleted %v", argo.deleted)
	}
}
