// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cellrouting

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// kubeconfigFor builds a syntactically valid kubeconfig. The server is never
// contacted: building a controller-runtime client does not dial.
func kubeconfigFor(server, token string) []byte {
	return []byte(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: ` + server + `
    insecure-skip-tls-verify: true
contexts:
- name: c
  context:
    cluster: c
    user: u
current-context: c
users:
- name: u
  user:
    token: ` + token + `
`)
}

func cellFixture(t *testing.T, kubeconfig []byte) (client.Client, *corev1.Secret) {
	t.Helper()
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1",
			APIServer:   "https://127.0.0.1:6443",
			CellID:      "cell-1",
			SecretRef:   loopzerov1.SecretReference{Name: "cell-1-kubeconfig", Namespace: "loopzero-system"},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-1-kubeconfig", Namespace: "loopzero-system"},
		Data:       map[string][]byte{"kubeconfig": kubeconfig},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc, secret).Build()
	return mgmt, secret
}

func TestCellAwareFactory_ReusesClientWhileKubeconfigSecretIsUnchanged(t *testing.T) {
	mgmt, _ := cellFixture(t, kubeconfigFor("https://127.0.0.1:6443", "token-a"))
	f := NewCellAwareRuntimeClientFactory(mgmt, nil, "loopzero-system")

	first, err := f.For(context.Background(), "cell-1")
	if err != nil {
		t.Fatalf("first For: %v", err)
	}
	second, err := f.For(context.Background(), "cell-1")
	if err != nil {
		t.Fatalf("second For: %v", err)
	}

	if first != second {
		t.Fatal("an unchanged kubeconfig Secret must reuse the cached cell client")
	}
}

// A rotated SA token (or a re-issued kubeconfig) lands in the ClusterCredential's
// Secret. A client cached forever keeps presenting the old credential, and every
// reconcile for that cell fails until the operator restarts.
func TestCellAwareFactory_RebuildsClientWhenKubeconfigSecretRotates(t *testing.T) {
	mgmt, secret := cellFixture(t, kubeconfigFor("https://127.0.0.1:6443", "token-a"))
	f := NewCellAwareRuntimeClientFactory(mgmt, nil, "loopzero-system")
	before, err := f.For(context.Background(), "cell-1")
	if err != nil {
		t.Fatalf("For before rotation: %v", err)
	}

	secret.Data["kubeconfig"] = kubeconfigFor("https://127.0.0.1:6443", "token-b")
	if err := mgmt.Update(context.Background(), secret); err != nil {
		t.Fatalf("rotate kubeconfig Secret: %v", err)
	}
	after, err := f.For(context.Background(), "cell-1")
	if err != nil {
		t.Fatalf("For after rotation: %v", err)
	}

	if before == after {
		t.Fatal("a rotated kubeconfig Secret must produce a new cell client, not the cached one")
	}
}
