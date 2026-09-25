// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
)

// execOnlyKubeconfig authenticates through an exec plugin, which an ArgoCD
// cluster Secret cannot carry: the sync to ArgoCD fails deterministically.
const execOnlyKubeconfig = `apiVersion: v1
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
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: /bin/false
`

// A ClusterCredential whose ArgoCD cluster Secret cannot be written must not
// read as healthy: ArgoCD cannot deploy to the cell, which is the purpose of
// Scope.ArgoCD.
func TestClusterCredential_ArgoCDSecretSyncFailureIsVisibleInStatus(t *testing.T) {
	s := bootstrapScheme(t)
	cr := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1", Namespace: "loopzero-system", Generation: 2,
			Finalizers: []string{clusterCredentialFinalizer}},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1", APIServer: "https://127.0.0.1:1", CellID: "cell-1",
			SecretRef: loopzerov1.SecretReference{Name: "cell-1-kubeconfig", Namespace: "loopzero-system"},
			Scope:     loopzerov1.CredentialScope{ArgoCD: true},
		},
	}
	kubeconfig := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1-kubeconfig", Namespace: "loopzero-system"},
		Data:       map[string][]byte{"kubeconfig": []byte(execOnlyKubeconfig)},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cr, kubeconfig).
		WithStatusSubresource(&loopzerov1.ClusterCredential{}).Build()
	r := &ClusterCredentialReconciler{Client: c, ArgoCDNamespace: "argocd"}

	_, _ = r.Reconcile(context.Background(),
		ctrl.Request{NamespacedName: types.NamespacedName{Name: "cell-1", Namespace: "loopzero-system"}})

	var got loopzerov1.ClusterCredential
	if err := c.Get(context.Background(), types.NamespacedName{Name: "cell-1", Namespace: "loopzero-system"}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	synced := conditions.GetCondition(got.Status.Conditions, conditions.TypeArgoCDSecretSynced)
	if synced == nil || synced.Status != metav1.ConditionFalse {
		t.Fatalf("ArgoCDSecretSynced = %+v, want False after a failed sync", synced)
	}
	if conditions.IsConditionTrue(got.Status.Conditions, "Ready") {
		t.Fatal("Ready must not be True while the ArgoCD cluster Secret is out of sync")
	}
}

func TestClusterCredential_DetailTypesIncludeTheArgoCDSyncOnlyForArgoCDScope(t *testing.T) {
	withArgo := clusterCredentialDetailTypes(&loopzerov1.ClusterCredential{Spec: loopzerov1.ClusterCredentialSpec{Scope: loopzerov1.CredentialScope{ArgoCD: true}}})
	without := clusterCredentialDetailTypes(&loopzerov1.ClusterCredential{})

	if len(withArgo) != 2 || withArgo[1] != conditions.TypeArgoCDSecretSynced {
		t.Fatalf("detail types with ArgoCD scope = %v, want Connected + ArgoCDSecretSynced", withArgo)
	}
	if len(without) != 1 || without[0] != conditions.TypeConnected {
		t.Fatalf("detail types without ArgoCD scope = %v, want Connected only", without)
	}
}
