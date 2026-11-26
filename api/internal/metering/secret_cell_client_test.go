// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// secret_cell_client_test.go — the SecretRef-based cell client provider (plan
// metering-compute-loop T3.2, slice S5 enabler). Unit-tested with a fake mgmt
// controller-runtime client seeded with a ClusterCredential CR + its kubeconfig
// Secret; the built kubernetes.Interface is lazy (no live connection), so the
// happy path is verifiable without a cluster.

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/getloopscale/loop-runtime/api/internal/placement"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// minimalKubeconfig is a syntactically valid kubeconfig that RESTConfigFromKubeConfig
// parses into a rest.Config (kubernetes.NewForConfig then builds a lazy client — no
// connection until a request).
const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://cell.example:6443
  name: cell
contexts:
- context:
    cluster: cell
    user: cell-user
  name: cell-ctx
current-context: cell-ctx
users:
- name: cell-user
  user:
    token: fake-token
`

func secretCellScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := apiv1.AddToScheme(s); err != nil {
		t.Fatalf("apiv1.AddToScheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("corev1.AddToScheme: %v", err)
	}
	return s
}

// fakeCellResolver stands in for placement.DynamicRouter: it knows only the
// connected cells it was given.
type fakeCellResolver map[string]placement.CellConfig

func (f fakeCellResolver) Resolve(_ context.Context, cellID string) (*placement.CellConfig, error) {
	c, ok := f[cellID]
	if !ok {
		return nil, fmt.Errorf("cell %q not connected", cellID)
	}
	return &c, nil
}

func resolverWith(cellID, secretName string) fakeCellResolver {
	return fakeCellResolver{cellID: {CellID: cellID, SecretRefName: secretName, SecretRefNamespace: "loopruntime-system"}}
}

func kubeconfigSecret(name string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "loopruntime-system"},
		Data:       data,
	}
}

func TestSecretCellClientProvider_SatisfiesInterface(t *testing.T) {
	var _ CellClientProvider = (*SecretCellClientProvider)(nil)
}

// TestSecretCellClientProvider_ClientFor_ResolvesFromSecret — the happy path:
// list the ClusterCredential by spec.cellID (MEET-053 B9 — NOT metadata.name),
// read its SecretRef kubeconfig, build a kubernetes.Interface.
func TestSecretCellClientProvider_ClientFor_ResolvesFromSecret(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(secretCellScheme(t)).
		WithObjects(
			kubeconfigSecret("cc-secret", map[string][]byte{"kubeconfig": []byte(minimalKubeconfig)}),
		).
		Build()
	p := NewSecretCellClientProvider(resolverWith("cell-0", "cc-secret"), c)

	client, err := p.ClientFor(context.Background(), "cell-0")
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}
	if client == nil {
		t.Fatal("ClientFor returned a nil kubernetes.Interface")
	}
}

// TestSecretCellClientProvider_ClientFor_CachesClient — a second lookup returns the
// same cached client (no re-fetch / re-build).
func TestSecretCellClientProvider_ClientFor_CachesClient(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(secretCellScheme(t)).
		WithObjects(
			kubeconfigSecret("cc-secret", map[string][]byte{"kubeconfig": []byte(minimalKubeconfig)}),
		).
		Build()
	p := NewSecretCellClientProvider(resolverWith("cell-0", "cc-secret"), c)

	first, err := p.ClientFor(context.Background(), "cell-0")
	if err != nil {
		t.Fatalf("first ClientFor: %v", err)
	}
	second, err := p.ClientFor(context.Background(), "cell-0")
	if err != nil {
		t.Fatalf("second ClientFor: %v", err)
	}
	if first != second {
		t.Error("ClientFor did not return the cached client on the second call")
	}
}

// TestSecretCellClientProvider_ClientFor_UnknownCell_Errors — no ClusterCredential
// with the requested spec.cellID → typed error (never a silent nil client).
func TestSecretCellClientProvider_ClientFor_UnknownCell_Errors(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(secretCellScheme(t)).Build()
	p := NewSecretCellClientProvider(fakeCellResolver{}, c)

	if _, err := p.ClientFor(context.Background(), "cell-nope"); err == nil {
		t.Fatal("ClientFor(unknown cell) must return an error")
	}
}

// TestSecretCellClientProvider_ClientFor_SecretMissingKubeconfig_Errors — the
// referenced Secret exists but has no data.kubeconfig → typed error.
func TestSecretCellClientProvider_ClientFor_SecretMissingKubeconfig_Errors(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(secretCellScheme(t)).
		WithObjects(
			kubeconfigSecret("cc-secret", map[string][]byte{"other": []byte("x")}),
		).
		Build()
	p := NewSecretCellClientProvider(resolverWith("cell-0", "cc-secret"), c)

	if _, err := p.ClientFor(context.Background(), "cell-0"); err == nil {
		t.Fatal("ClientFor must error when the Secret has no data.kubeconfig")
	}
}

// A rotated kubeconfig reaches the next call once the cached client is
// invalidated — the cache must not pin a dead credential for the process life.
func TestSecretCellClientProvider_InvalidateRebuildsTheClient(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(secretCellScheme(t)).
		WithObjects(kubeconfigSecret("cc-secret", map[string][]byte{"kubeconfig": []byte(minimalKubeconfig)})).
		Build()
	p := NewSecretCellClientProvider(resolverWith("cell-0", "cc-secret"), c)
	first, err := p.ClientFor(context.Background(), "cell-0")
	if err != nil {
		t.Fatalf("ClientFor: %v", err)
	}

	p.Invalidate("cell-0")
	second, err := p.ClientFor(context.Background(), "cell-0")

	if err != nil {
		t.Fatalf("ClientFor after Invalidate: %v", err)
	}
	if first == second {
		t.Error("Invalidate did not drop the cached client")
	}
}
