// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

const tokenKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:1
    insecure-skip-tls-verify: true
contexts:
- name: c
  context: {cluster: c, user: u}
current-context: c
users:
- name: u
  user:
    token: t0ken
`

// The ArgoCD cluster Secret has another writer; losing one optimistic
// concurrency race must not fail the sync.
func TestClusterCredential_ArgoCDSecretWriteSurvivesAConflict(t *testing.T) {
	s := bootstrapScheme(t)
	cr := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1", APIServer: "https://127.0.0.1:1", CellID: "cell-1",
			Scope: loopzerov1.CredentialScope{ArgoCD: true},
		},
	}
	existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cluster-loopzero-cell-1", Namespace: "argocd"}}
	conflicts := 0
	c := interceptor.NewClient(fake.NewClientBuilder().WithScheme(s).WithObjects(existing).Build(), interceptor.Funcs{
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if conflicts == 0 {
				conflicts++
				return apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, obj.GetName(), nil)
			}
			return cl.Update(ctx, obj, opts...)
		},
	})
	r := &ClusterCredentialReconciler{Client: c, ArgoCDNamespace: "argocd"}

	if err := r.reconcileArgoCDClusterSecret(context.Background(), cr, []byte(tokenKubeconfig)); err != nil {
		t.Fatalf("sync after one conflict: %v", err)
	}

	var got corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cluster-loopzero-cell-1", Namespace: "argocd"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StringData["server"] != "https://127.0.0.1:1" && string(got.Data["server"]) != "https://127.0.0.1:1" {
		t.Fatalf("secret not written after the retry: %+v", got)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want the injected one", conflicts)
	}
}
