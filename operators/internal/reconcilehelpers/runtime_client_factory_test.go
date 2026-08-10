// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Unit tests for CellAwareRuntimeClientFactory + StaticRuntimeClientFactory.
// cell-2-activation-plan iter 4 BUG-FIX. These tests use fake clients (no envtest).

package reconcilehelpers

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
)

func newSchemeForTest(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("add loopzerov1 to scheme: %v", err)
	}
	return scheme
}

func TestCellAwareFactory_DefaultClientForEmptyOrCellDefault(t *testing.T) {
	scheme := newSchemeForTest(t)
	mgmt := fake.NewClientBuilder().WithScheme(scheme).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()

	f := NewCellAwareRuntimeClientFactory(mgmt, def, "")
	for _, cellID := range []string{"", "cell-default"} {
		c, err := f.For(context.Background(), cellID)
		if err != nil {
			t.Fatalf("For(%q): unexpected error: %v", cellID, err)
		}
		if c != def {
			t.Errorf("For(%q): expected default client, got different", cellID)
		}
	}
}

func TestCellAwareFactory_ErrorWhenNoCCFound(t *testing.T) {
	scheme := newSchemeForTest(t)
	mgmt := fake.NewClientBuilder().WithScheme(scheme).Build() // empty
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	_, err := f.For(context.Background(), "cell-2")
	if err == nil {
		t.Fatal("expected error for missing ClusterCredential, got nil")
	}
}

func TestCellAwareFactory_ErrorWhenSecretMissing(t *testing.T) {
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-2", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1",
			APIServer:   "https://172.18.0.10:6443",
			CellID:      "cell-2",
			SecretRef: loopzerov1.SecretReference{
				Name:      "cell-2-kubeconfig",
				Namespace: "loopzero-system",
			},
		},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	_, err := f.For(context.Background(), "cell-2")
	if err == nil {
		t.Fatal("expected error for missing kubeconfig Secret, got nil")
	}
}

func TestCellAwareFactory_ErrorWhenSecretLacksKubeconfig(t *testing.T) {
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-2", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-1",
			APIServer:   "https://172.18.0.10:6443",
			CellID:      "cell-2",
			SecretRef: loopzerov1.SecretReference{
				Name:      "cell-2-kubeconfig",
				Namespace: "loopzero-system",
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cell-2-kubeconfig", Namespace: "loopzero-system"},
		Data:       map[string][]byte{"other": []byte("not-kubeconfig")},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc, secret).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	_, err := f.For(context.Background(), "cell-2")
	if err == nil {
		t.Fatal("expected error when Secret has no data.kubeconfig, got nil")
	}
}

func TestCellAwareFactory_LookupByCellIDNotCRName(t *testing.T) {
	// Per MEET-053 B9: lookup by spec.cellID, NOT metadata.name.
	// The CR name might be "workload" with cellID="cell-1", or "cell-2"
	// with cellID="cell-2"; either should be found.
	scheme := newSchemeForTest(t)
	cc := &loopzerov1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{Name: "workload-not-matching-cellid", Namespace: "loopzero-system"},
		Spec: loopzerov1.ClusterCredentialSpec{
			ClusterName: "loopzero-cell-0",
			APIServer:   "https://172.18.0.7:6443",
			CellID:      "cell-1",
			SecretRef: loopzerov1.SecretReference{
				Name:      "workload-not-matching-cellid-kubeconfig",
				Namespace: "loopzero-system",
			},
		},
	}
	mgmt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cc).Build()
	def := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewCellAwareRuntimeClientFactory(mgmt, def, "loopzero-system")

	// Lookup by spec.cellID, NOT by metadata.name.
	_, err := f.For(context.Background(), "cell-1")
	// It should find the CR but fail on Secret missing — which proves it
	// matched by cellID not name.
	if err == nil {
		t.Fatal("expected Secret-missing error (proving CR matched by cellID), got nil")
	}
	// Drift guard: should NOT report "no ClusterCredential found" because
	// the CR exists, just under a different name.
	want := "no ClusterCredential found"
	if got := err.Error(); contains(got, want) {
		t.Errorf("factory looked up by metadata.name (drift!): %s", got)
	}
}

func TestStaticFactory_ReturnsSameClient(t *testing.T) {
	scheme := newSchemeForTest(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	f := NewStaticRuntimeClientFactory(c)

	for _, cellID := range []string{"", "cell-default", "cell-1", "cell-2"} {
		got, err := f.For(context.Background(), cellID)
		if err != nil {
			t.Fatalf("For(%q) unexpected error: %v", cellID, err)
		}
		if got != c {
			t.Errorf("For(%q): expected static client, got different", cellID)
		}
	}
}

func TestStaticFactory_NilErrors(t *testing.T) {
	f := NewStaticRuntimeClientFactory(nil)
	_, err := f.For(context.Background(), "cell-2")
	if err == nil {
		t.Fatal("expected error for nil static client, got nil")
	}
}

// _ is a sentinel ensuring CellAwareRuntimeClientFactory implements
// RuntimeClientFactory. Compile-time guard.
var _ RuntimeClientFactory = (*CellAwareRuntimeClientFactory)(nil)
var _ client.Client = (client.Client)(nil)

func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
